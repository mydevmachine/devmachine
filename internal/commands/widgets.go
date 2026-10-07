package commands

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mydevmachine/devmachine/internal/config"
	"github.com/mydevmachine/devmachine/internal/packages"
	"github.com/mydevmachine/devmachine/internal/widgets"
	"github.com/spf13/cobra"
)

func newWidgetsCmd(opts *options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "widgets",
		Short: "The widgets the app can draw, and the boards they sit on",
		Long: "Widgets come from packages: a release's and your own. Boards live " +
			"in <config>/boards/. Local only: these commands never connect to a machine.\n\n" +
			"`widgets help <package/widget>` describes a widget; `--help` shows how to use a command.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error { return cmd.Help() },
	}
	cmd.AddCommand(
		newWidgetsListCmd(opts),
		newWidgetsHelpCmd(opts),
		newWidgetsValidateCmd(opts),
		newWidgetsSchemaCmd(opts),
		newWidgetsAddCmd(opts),
		newWidgetsRemoveCmd(opts),
	)
	return cmd
}

func newWidgetsListCmd(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List every widget the pinned release and your own packages offer",
		Long: "A widget with a problem is left out and listed under problems; the " +
			"command still succeeds, so one broken widget never hides the rest.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			catalog, _, err := loadCatalog(cmd.Context(), opts)
			if err != nil {
				return err
			}
			if opts.format == formatJSON {
				return writeJSON(cmd.OutOrStdout(), catalog)
			}
			for _, e := range catalog.Widgets {
				origin := e.Origin
				if e.Version != "" {
					origin += " " + e.Version
				}
				state := "available"
				if !e.Available {
					state = "unavailable: " + e.UnavailableReason
				}
				cmd.Printf("%s  (%s)  %s\n  sizes: %s  fits: %s  %s\n",
					e.Name, origin, e.Summary, strings.Join(e.Sizes, ", "), strings.Join(e.Surfaces, ", "), state)
			}
			for _, p := range catalog.Problems {
				cmd.Printf("problem: %s: %s\n", p.Path, p.Message)
			}
			return nil
		},
	}
}

func newWidgetsHelpCmd(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "help <package/widget>",
		Short: "Say what one widget takes: inputs, context, sizes, and where it fits",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			catalog, _, err := loadCatalog(cmd.Context(), opts)
			if err != nil {
				return err
			}
			e, ok := catalog.Find(args[0])
			if !ok {
				return fmt.Errorf("no widget named %q: `devmachine widgets list` shows every widget there is", args[0])
			}
			if opts.format == formatJSON {
				return writeJSON(cmd.OutOrStdout(), e)
			}
			cmd.Printf("%s — %s\n", e.Name, e.Summary)
			cmd.Printf("sizes: %s (default %s)\n", strings.Join(e.Sizes, ", "), e.DefaultSize)
			cmd.Printf("fits: %s\n", strings.Join(e.Surfaces, ", "))
			if e.Single {
				cmd.Println("once per board")
			}
			for _, name := range sortedNames(e.Inputs) {
				in := e.Inputs[name]
				detail := in.Type
				if in.Default != nil {
					detail += fmt.Sprintf(", default %v", in.Default)
				}
				cmd.Printf("input %s (%s): %s\n", name, detail, in.Summary)
			}
			for _, key := range sortedNames(e.Context) {
				cmd.Printf("context %s: %s\n", key, e.Context[key])
			}
			if !e.Available {
				cmd.Printf("unavailable: %s\n", e.UnavailableReason)
			}
			return nil
		},
	}
}

func newWidgetsSchemaCmd(opts *options) *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "schema",
		Short: "Print the engine contract: surfaces, providers, views and sizes",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if asJSON || opts.format == formatJSON {
				return writeJSON(cmd.OutOrStdout(), widgets.CurrentContract())
			}
			cmd.Print(widgets.ReferenceTables())
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "print the contract as JSON")
	return c
}

// loadCatalog resolves the catalog from the pinned release, or from the
// latest one when nothing is pinned yet, plus your own packages.
func loadCatalog(ctx context.Context, opts *options) (widgets.Catalog, string, error) {
	dir, _, err := config.Dir(opts.configDir)
	if err != nil {
		return widgets.Catalog{}, "", err
	}
	if dir, err = filepath.Abs(dir); err != nil {
		return widgets.Catalog{}, "", fmt.Errorf("finding the configuration directory: %w", err)
	}
	cfg, err := loadConfigOrEmpty(dir)
	if err != nil {
		return widgets.Catalog{}, "", err
	}
	version := cfg.Packages
	if version == "" {
		if version, err = latestPackagesRelease(ctx); err != nil {
			return widgets.Catalog{}, "", err
		}
	}
	store, err := openStore(ctx, dir, version)
	if err != nil {
		return widgets.Catalog{}, "", err
	}
	catalog, err := catalogFrom(dir, cfg, store)
	return catalog, dir, err
}

func loadConfigOrEmpty(dir string) (config.Config, error) {
	cfg, err := config.Load(dir)
	if errors.Is(err, os.ErrNotExist) {
		return config.Config{}, nil
	}
	return cfg, err
}

func catalogFrom(dir string, cfg config.Config, store *packages.Store) (widgets.Catalog, error) {
	all, broken := store.Scan()
	lock, err := packages.LoadLock(dir)
	if err != nil {
		return widgets.Catalog{}, err
	}
	var pkgs []widgets.PackageWidgets
	for _, f := range all {
		root, ok := packages.WidgetsDir(f.Manifest)
		if !ok {
			continue
		}
		pkg := widgets.PackageWidgets{Package: f.Manifest.Name, Scope: f.Manifest.Scope, Origin: widgets.OriginLocal, Root: root, Dir: f.Manifest.Path}
		if f.Source == packages.SourceRelease {
			pkg.Origin, pkg.Version = widgets.OriginRelease, store.Version()
		}
		pkgs = append(pkgs, pkg)
	}
	catalog := widgets.Resolve(store.Version(), pkgs, installedState(cfg, lock))
	manifestProblems := make([]widgets.ListProblem, 0, len(broken))
	for _, b := range broken {
		manifestProblems = append(manifestProblems, widgets.ListProblem{Path: b.Path, Message: b.Err.Error()})
	}
	catalog.Problems = append(manifestProblems, catalog.Problems...)
	return catalog, nil
}

func installedState(cfg config.Config, lock packages.Lock) func(string) widgets.Installed {
	return func(pkg string) widgets.Installed {
		var state widgets.Installed
		for _, m := range cfg.Machines {
			state.Added = state.Added || slices.Contains(m.Packages, pkg)
		}
		for _, w := range cfg.Workspaces {
			state.Added = state.Added || slices.Contains(w.Packages, pkg)
		}
		for _, entries := range []map[string][]packages.LockEntry{lock.Machines, lock.Workspaces} {
			for _, list := range entries {
				for _, entry := range list {
					state.Synced = state.Synced || entry.Name == pkg
				}
			}
		}
		return state
	}
}

func sortedNames[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}
