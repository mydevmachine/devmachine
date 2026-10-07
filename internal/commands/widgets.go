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
		newWidgetsMoveCmd(opts),
		newWidgetsSetCmd(opts),
	)
	return cmd
}

func newWidgetsListCmd(opts *options) *cobra.Command {
	var board string
	c := &cobra.Command{
		Use:   "list",
		Short: "List every widget the pinned release and your own packages offer",
		Long: "A widget with a problem is left out and listed under problems; the " +
			"command still succeeds, so one broken widget never hides the rest. " +
			"--board keeps only the widgets that fit that area.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if board != "" {
				if err := checkBoardSurface(board); err != nil {
					return err
				}
			}
			catalog, _, err := loadCatalog(cmd.Context(), opts)
			if err != nil {
				return err
			}
			if board != "" {
				catalog.Widgets = slices.DeleteFunc(catalog.Widgets, func(e widgets.Entry) bool {
					return !slices.Contains(e.Surfaces, board)
				})
			}
			if opts.format == formatJSON {
				return writeJSON(cmd.OutOrStdout(), catalog)
			}
			for _, e := range catalog.Widgets {
				origin := e.Origin
				if e.Version != "" {
					origin += " " + e.Version
				}
				if e.Trust == widgets.TrustThirdParty {
					origin = "third-party"
					if e.PackageSource != nil && e.PackageSource.URL != "" {
						origin += " from " + e.PackageSource.URL
					}
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
	c.Flags().StringVar(&board, "board", "", "keep only the widgets that fit this area: home, sidebar, context-sidebar, menubar or menubar-panel")
	return c
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
				cmd.Printf("input %s (%s): %s\n", name, inputDetail(in), in.Summary)
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

// inputDetail is how help names an input's type and default: a choice says
// where its options come from, and a missing default says what it means.
func inputDetail(in widgets.Input) string {
	detail := in.Type
	if in.Type == widgets.InputChoice {
		detail = "choice of " + in.From
		if in.Many {
			detail += ", many"
		}
	}
	list, isList := in.Default.([]any)
	emptyList := isList && len(list) == 0
	switch {
	case in.Default != nil && !emptyList:
		detail += fmt.Sprintf(", default %v", in.Default)
	case in.Type == widgets.InputChoice && in.Many:
		detail += ", default all"
	case in.Type == widgets.InputChoice:
		detail += ", default the first"
	}
	return detail
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
	var originProblems []widgets.ListProblem
	pkgs := make([]widgets.PackageWidgets, 0, len(all))
	for _, f := range all {
		owner := packages.WidgetOwner(f.Manifest)
		pkg := widgets.PackageWidgets{Package: f.Manifest.Name, Scope: f.Manifest.Scope, Origin: widgets.OriginLocal,
			Trust: widgets.TrustLocal, Dir: f.Manifest.Path, Providers: owner.Providers}
		if root, ok := packages.WidgetsDir(f.Manifest); ok {
			pkg.Root = root
		}
		if f.Source == packages.SourceRelease {
			pkg.Origin, pkg.Version, pkg.Trust = widgets.OriginRelease, store.Version(), widgets.TrustOfficial
		} else {
			origin, found, err := packages.ReadOrigin(f.Manifest.Path)
			switch {
			case err != nil:
				pkg.Trust = widgets.TrustThirdParty
				originProblems = append(originProblems, widgets.ListProblem{
					Path:    filepath.Join(f.Manifest.Path, packages.OriginFile),
					Message: err.Error() + ": its widgets are treated as third-party",
				})
			case found:
				pkg.Trust = widgets.TrustThirdParty
				pkg.Source = &widgets.PackageSource{URL: origin.URL, Ref: origin.Ref, Commit: origin.Commit}
			}
		}
		pkgs = append(pkgs, pkg)
	}
	catalog := widgets.Resolve(store.Version(), pkgs, installedState(cfg, lock))
	manifestProblems := make([]widgets.ListProblem, 0, len(broken)+len(originProblems))
	for _, b := range broken {
		manifestProblems = append(manifestProblems, widgets.ListProblem{Path: b.Path, Message: b.Err.Error()})
	}
	manifestProblems = append(manifestProblems, originProblems...)
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
