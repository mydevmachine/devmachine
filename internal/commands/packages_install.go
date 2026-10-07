package commands

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mydevmachine/devmachine/internal/config"
	"github.com/mydevmachine/devmachine/internal/packages"
	"github.com/mydevmachine/devmachine/internal/repo"
	"github.com/spf13/cobra"
)

// parseGitAddress is the seam a test replaces to fetch from a bare
// repository in a temporary folder; the real one refuses file:// addresses.
var parseGitAddress = packages.ParseGitAddress

func newPackagesInstallCmd(opts *options) *cobra.Command {
	var check, yes bool
	c := &cobra.Command{
		Use:   "install <git-address>[@<ref>]",
		Short: "Install a package somebody published in a git repository",
		Long: "Fetches one commit of the repository (a tag, a branch or a commit after @; the " +
			"default branch without one), checks it, shows what it brings and asks before " +
			"writing <config>/packages/<name>/. Only https:// and git@ addresses. Its tasks run " +
			"as root on every machine you add it to, and its widgets that run code ask before " +
			"they run. `packages add` then puts it on a machine, as with any package.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return installPackage(cmd, opts, args[0], check, yes)
		},
	}
	c.Flags().BoolVar(&check, "check", false, "show what it brings, and install nothing")
	c.Flags().BoolVar(&yes, "yes", false, "do not ask")
	return c
}

// installReport is what install prints with --format json.
type installReport struct {
	packages.Summary
	URL       string `json:"url"`
	Ref       string `json:"ref"`
	Commit    string `json:"commit"`
	Path      string `json:"path"`
	Installed bool   `json:"installed"`
}

func installPackage(cmd *cobra.Command, opts *options, arg string, check, yes bool) error {
	address, ref, err := parseGitAddress(arg)
	if err != nil {
		return err
	}
	if opts.format == formatJSON && !check && !yes {
		return errors.New("--format json cannot ask: add --check to see what it brings, or --yes to install")
	}
	dir, err := absConfigDir(opts)
	if err != nil {
		return err
	}
	cfg, err := loadConfigOrEmpty(dir)
	if err != nil {
		return err
	}
	staged, err := packages.Stage(cmd.Context(), dir, address, ref)
	if err != nil {
		return err
	}
	defer staged.Discard()

	target := filepath.Join(packages.LocalDir(dir), staged.Name)
	if err := refuseCollision(cmd.Context(), dir, cfg, staged.Name); err != nil {
		return err
	}
	if _, err := os.Lstat(target); err == nil {
		origin, found, err := packages.ReadOrigin(target)
		if err != nil {
			return fmt.Errorf("%s is already installed from a git address (%w): update it with devmachine packages update %s", staged.Name, err, staged.Name)
		}
		if found {
			return fmt.Errorf("%s is already installed from %s: update it with devmachine packages update %s", staged.Name, origin.URL, staged.Name)
		}
		return fmt.Errorf("you already have your own package %s in %s: a package from a git address cannot replace it", staged.Name, target)
	}

	report := installReport{Summary: staged.Summary, URL: address, Ref: ref, Commit: staged.Origin.Commit, Path: target}
	if check {
		return printInstall(cmd, opts, report)
	}
	if !yes {
		printSummary(cmd, staged.Summary, staged.Origin)
		ok, err := confirm(cmd.InOrStdin(), cmd.OutOrStdout(), fmt.Sprintf("Install %s into %s?", staged.Name, target))
		if err != nil {
			return err
		}
		if !ok {
			return errDeclined
		}
	}
	if _, err := staged.Install(dir, time.Now()); err != nil {
		return err
	}
	repo.AutoCommit(cmd.Context(), dir, "chore(config): install package "+staged.Name)
	report.Installed = true
	return printInstall(cmd, opts, report)
}

func printInstall(cmd *cobra.Command, opts *options, report installReport) error {
	if opts.format == formatJSON {
		return writeJSON(cmd.OutOrStdout(), report)
	}
	if !report.Installed {
		printSummary(cmd, report.Summary, packages.Origin{URL: report.URL, Ref: report.Ref, Commit: report.Commit})
		return nil
	}
	cmd.Printf("installed %s into %s; add it with devmachine packages add %s, then sync\n", report.Name, report.Path, report.Name)
	return nil
}

// printSummary shows what a package brings before anything is written.
func printSummary(cmd *cobra.Command, s packages.Summary, origin packages.Origin) {
	at := origin.Ref
	if at == "" {
		at = "its default branch"
	}
	cmd.Printf("%s (%s) — %s\n", s.Name, s.Scope, s.Summary)
	cmd.Printf("from %s at %s (commit %s)\n", origin.URL, at, shortCommit(origin.Commit))
	widgetLines := make([]string, 0, len(s.Widgets))
	for _, w := range s.Widgets {
		detail := w.Source
		if w.RunsCode {
			detail += ", runs code"
		}
		widgetLines = append(widgetLines, fmt.Sprintf("%s (%s)", w.Name, detail))
	}
	for _, row := range []struct {
		label string
		items []string
	}{
		{"needs", s.Needs}, {"widgets", widgetLines}, {"commands", s.Commands}, {"providers", s.Providers},
		{"scripts", s.Scripts}, {"tasks", s.Tasks}, {"credentials", s.Credentials},
	} {
		if len(row.items) > 0 {
			cmd.Printf("%s: %s\n", row.label, strings.Join(row.items, ", "))
		}
	}
	cmd.Println("Its tasks run as root on every machine you add it to; its widgets that run code ask before they run.")
}

func shortCommit(commit string) string {
	if len(commit) > 12 {
		return commit[:12]
	}
	return commit
}

// refuseCollision keeps a package from a git address off an official name:
// your own package of a name always wins over the release's, so it would
// replace the official one on every machine without a word.
func refuseCollision(ctx context.Context, dir string, cfg config.Config, name string) error {
	version := cfg.Packages
	if version == "" {
		latest, err := latestPackagesRelease(ctx)
		if err != nil {
			return err
		}
		version = latest
	}
	store, err := openStore(ctx, dir, version)
	if err != nil {
		return err
	}
	if _, err := store.GetRelease(name); err == nil {
		return fmt.Errorf("%s is an official package: a package from a git address cannot take its name, because it would replace "+
			"the official one everywhere. Ask its author to rename it", name)
	}
	return nil
}

// absConfigDir is the configuration folder as an absolute path, so a path
// printed or recorded stays right whatever folder the command ran from.
func absConfigDir(opts *options) (string, error) {
	dir, _, err := config.Dir(opts.configDir)
	if err != nil {
		return "", err
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("finding the configuration directory: %w", err)
	}
	return abs, nil
}
