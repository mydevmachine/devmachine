package commands

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
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
	if err := staged.Touch(time.Now()); err != nil {
		return err
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
	esc := packages.EscapeControl
	cmd.Printf("%s (%s) — %s\n", esc(s.Name), esc(s.Scope), esc(s.Summary))
	cmd.Printf("from %s at %s (commit %s)\n", esc(origin.URL), esc(at), esc(shortCommit(origin.Commit)))
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
		{"scripts", s.Scripts}, {"tasks", s.Tasks}, {"role files", s.RoleFiles}, {"credentials", s.Credentials},
	} {
		printRow(cmd, row.label, row.items)
	}
	cmd.Println("Its tasks run as root on every machine you add it to; its widgets that run code ask before they run.")
}

// printRow prints a labelled list, escaping what could move or hide text
// in a terminal: Stage already refuses it, and this is the second guard on
// the only prompt before tasks that run as root.
func printRow(cmd *cobra.Command, label string, items []string) {
	if len(items) == 0 {
		return
	}
	escaped := make([]string, len(items))
	for i, item := range items {
		escaped[i] = packages.EscapeControl(item)
	}
	cmd.Printf("%s: %s\n", label, strings.Join(escaped, ", "))
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
	_, err = store.GetRelease(name)
	var notInRelease *packages.NotInReleaseError
	switch {
	case errors.As(err, &notInRelease):
		return nil
	case err != nil:
		return fmt.Errorf("checking whether %s is an official package: %w", name, err)
	}
	return fmt.Errorf("%s is an official package: a package from a git address cannot take its name, because it would replace "+
		"the official one everywhere. Ask its author to rename it", name)
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

func newPackagesUpdateCmd(opts *options) *cobra.Command {
	var check, yes bool
	c := &cobra.Command{
		Use:   "update <name>",
		Short: "Fetch a package installed from a git address again",
		Long: "Fetches the address and ref recorded when it was installed, says what changed — the " +
			"commit; the widgets, commands and providers added or removed; and the credentials, " +
			"scripts, tasks and other role files added, removed or changed — and asks before replacing it. A new " +
			"commit means its widgets that run code ask again.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return updatePackage(cmd, opts, args[0], check, yes)
		},
	}
	c.Flags().BoolVar(&check, "check", false, "show what would change, and change nothing")
	c.Flags().BoolVar(&yes, "yes", false, "do not ask")
	return c
}

// updateReport is what update prints with --format json.
type updateReport struct {
	Package        string           `json:"package"`
	Path           string           `json:"path"`
	URL            string           `json:"url"`
	Ref            string           `json:"ref"`
	PreviousCommit string           `json:"previous_commit"`
	Commit         string           `json:"commit"`
	Changes        packages.Changes `json:"changes"`
	Updated        bool             `json:"updated"`
}

func updatePackage(cmd *cobra.Command, opts *options, name string, check, yes bool) error {
	if !packages.ValidName(name) {
		return fmt.Errorf("%q is not a package name", name)
	}
	if opts.format == formatJSON && !check && !yes {
		return errors.New("--format json cannot ask: add --check to see what would change, or --yes to update")
	}
	dir, err := absConfigDir(opts)
	if err != nil {
		return err
	}
	cfg, err := loadConfigOrEmpty(dir)
	if err != nil {
		return err
	}
	target := filepath.Join(packages.LocalDir(dir), name)
	if _, err := os.Stat(packages.ManifestPath(target)); err != nil {
		return fmt.Errorf("no package %s in %s: packages update works on a package installed with packages install", name, packages.LocalDir(dir))
	}
	origin, found, err := packages.ReadOrigin(target)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("%s is your own package, not one installed from a git address: there is nothing to fetch it from", name)
	}
	recorded := origin.URL
	if origin.Ref != "" {
		recorded += "@" + origin.Ref
	}
	address, ref, err := parseGitAddress(recorded)
	if err != nil {
		return fmt.Errorf("the address recorded in %s: %w", filepath.Join(target, packages.OriginFile), err)
	}
	staged, err := packages.Stage(cmd.Context(), dir, address, ref)
	if err != nil {
		return err
	}
	defer staged.Discard()
	if staged.Name != name {
		return fmt.Errorf("%s now holds a package named %s, not %s: remove %s with devmachine packages remove %s, then install the new one",
			address, staged.Name, name, name, name)
	}
	if err := refuseCollision(cmd.Context(), dir, cfg, name); err != nil {
		return err
	}

	changes, err := packages.Compare(target, staged.Dir)
	if err != nil {
		return err
	}
	report := updateReport{Package: name, Path: target, URL: address, Ref: ref, PreviousCommit: origin.Commit,
		Commit: staged.Origin.Commit, Changes: changes}
	if staged.Origin.Commit == origin.Commit {
		if opts.format == formatJSON {
			return writeJSON(cmd.OutOrStdout(), report)
		}
		cmd.Printf("%s is up to date at %s\n", name, shortCommit(origin.Commit))
		return nil
	}
	if opts.format != formatJSON {
		printChanges(cmd, report)
	}
	if check {
		if opts.format == formatJSON {
			return writeJSON(cmd.OutOrStdout(), report)
		}
		return nil
	}
	if !yes {
		ok, err := confirm(cmd.InOrStdin(), cmd.OutOrStdout(), fmt.Sprintf("Update %s to %s?", name, shortCommit(staged.Origin.Commit)))
		if err != nil {
			return err
		}
		if !ok {
			return errDeclined
		}
	}
	if err := staged.Touch(time.Now()); err != nil {
		return err
	}
	if _, err := staged.Replace(dir, time.Now()); err != nil {
		return err
	}
	repo.AutoCommit(cmd.Context(), dir, "chore(config): update package "+name)
	report.Updated = true
	if opts.format == formatJSON {
		return writeJSON(cmd.OutOrStdout(), report)
	}
	cmd.Printf("updated %s; sync to put it on your machines\n", name)
	return nil
}

func printChanges(cmd *cobra.Command, r updateReport) {
	cmd.Printf("%s: commit %s → %s\n", packages.EscapeControl(r.Package), packages.EscapeControl(shortCommit(r.PreviousCommit)),
		packages.EscapeControl(shortCommit(r.Commit)))
	c := r.Changes
	for _, row := range []struct {
		label string
		items []string
	}{
		{"widgets added", c.WidgetsAdded}, {"widgets removed", c.WidgetsRemoved},
		{"commands added", c.CommandsAdded}, {"commands removed", c.CommandsRemoved},
		{"providers added", c.ProvidersAdded}, {"providers removed", c.ProvidersRemoved},
		{"credentials added", c.CredentialsAdded}, {"credentials removed", c.CredentialsRemoved}, {"credentials changed", c.CredentialsChanged},
		{"scripts added", c.ScriptsAdded}, {"scripts removed", c.ScriptsRemoved}, {"scripts changed", c.ScriptsChanged},
		{"tasks added", c.TasksAdded}, {"tasks removed", c.TasksRemoved}, {"tasks changed", c.TasksChanged},
		{"role files added", c.RoleFilesAdded}, {"role files removed", c.RoleFilesRemoved}, {"role files changed", c.RoleFilesChanged},
	} {
		printRow(cmd, row.label, row.items)
	}
	if c.Empty() {
		cmd.Println("its widgets, commands, providers, credentials, scripts, tasks and role files are the same; its other files may have changed")
	}
	cmd.Println("A new commit means its widgets that run code ask again before they run.")
}

func newPackagesRemoveCmd(opts *options) *cobra.Command {
	var yes bool
	c := &cobra.Command{
		Use:   "remove <name>",
		Short: "Delete a package installed from a git address",
		Long: "Deletes <config>/packages/<name>/. Only a package installed with `packages install`: " +
			"one you wrote yourself is never deleted. To take a package off a machine or a " +
			"workspace instead, use `packages rm`.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return removeInstalledPackage(cmd, opts, args[0], yes)
		},
	}
	c.Flags().BoolVar(&yes, "yes", false, "do not ask")
	return c
}

func removeInstalledPackage(cmd *cobra.Command, opts *options, name string, yes bool) error {
	if !packages.ValidName(name) {
		return fmt.Errorf("%q is not a package name", name)
	}
	dir, err := absConfigDir(opts)
	if err != nil {
		return err
	}
	cfg, err := loadConfigOrEmpty(dir)
	if err != nil {
		return err
	}
	target := filepath.Join(packages.LocalDir(dir), name)
	if _, err := os.Stat(packages.ManifestPath(target)); err != nil {
		return fmt.Errorf("no package %s in %s", name, packages.LocalDir(dir))
	}
	origin, found, err := packages.ReadOrigin(target)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("%s is your own package, not one installed from a git address: packages remove never deletes your own; "+
			"delete %s yourself if you mean it", name, target)
	}
	if on := installedOn(cfg)[name]; len(on) > 0 {
		return fmt.Errorf("%s is still on %s: take it off first with devmachine packages rm %s --machine <name> (or --workspace <name>), then sync",
			name, strings.Join(on, ", "), name)
	}
	if slices.Contains(cfg.Defaults.Workspace, name) {
		return fmt.Errorf("%s is in the future workspace defaults, so the next workspace you create would need it: take it out first "+
			"with devmachine workspaces defaults --rm %s", name, name)
	}
	if !yes {
		ok, err := confirm(cmd.InOrStdin(), cmd.OutOrStdout(), fmt.Sprintf("Delete %s (installed from %s)?", target, origin.URL))
		if err != nil {
			return err
		}
		if !ok {
			return errDeclined
		}
	}
	path, err := packages.Uninstall(dir, name)
	if err != nil {
		return err
	}
	repo.AutoCommit(cmd.Context(), dir, "chore(config): remove package "+name)
	if opts.format == formatJSON {
		return writeJSON(cmd.OutOrStdout(), struct {
			Package string `json:"package"`
			Path    string `json:"path"`
			Removed bool   `json:"removed"`
		}{name, path, true})
	}
	cmd.Printf("removed %s\n", name)
	return nil
}
