package commands

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/mydevmachine/devmachine/internal/config"
	"github.com/mydevmachine/devmachine/internal/packages"
	"github.com/mydevmachine/devmachine/internal/widgets"
	"github.com/spf13/cobra"
)

func newWidgetsValidateCmd(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "validate [path...]",
		Short: "Check widgets and boards, and say what is wrong and where",
		Long: "A path is a widget.yml, a widget folder, a package folder or a board " +
			"file. With none, every board in <config>/boards/ and every widget of " +
			"your own packages is checked. Every problem is reported at once.",
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, _, err := config.Dir(opts.configDir)
			if err != nil {
				return err
			}
			catalog, err := cachedCatalog(dir)
			if err != nil {
				return err
			}
			targets := args
			if len(targets) == 0 {
				if targets, err = defaultValidateTargets(dir); err != nil {
					return err
				}
			}

			problems := []widgets.Problem{}
			fits := []widgetFit{}
			for _, target := range targets {
				valid, found, err := validateTarget(target, catalog.Find)
				if err != nil {
					return err
				}
				problems = append(problems, found...)
				for _, w := range valid {
					fits = append(fits, widgetFit{Path: w.Dir, Name: w.Name, Surfaces: nonNil(widgets.Surfaces(w))})
				}
			}

			if opts.format == formatJSON {
				if err := writeJSON(cmd.OutOrStdout(), struct {
					OK       bool              `json:"ok"`
					Checked  []string          `json:"checked"`
					Widgets  []widgetFit       `json:"widgets"`
					Problems []widgets.Problem `json:"problems"`
				}{len(problems) == 0, nonNil(targets), fits, problems}); err != nil {
					return err
				}
			} else {
				if len(targets) == 0 {
					cmd.Printf("nothing to check: no board in %s and no widget in your own packages\n", widgets.BoardsDir(dir))
				}
				for _, f := range fits {
					if len(f.Surfaces) == 0 {
						cmd.Printf("%s fits no area yet\n", f.Name)
						continue
					}
					cmd.Printf("%s fits %s\n", f.Name, strings.Join(f.Surfaces, ", "))
				}
				for _, p := range problems {
					cmd.Println(p.Error())
				}
				if len(problems) == 0 && len(targets) > 0 {
					cmd.Printf("%d checked, all fine\n", len(targets))
				}
			}
			if len(problems) > 0 {
				return fmt.Errorf("%d problem(s)", len(problems))
			}
			return nil
		},
	}
}

// cachedCatalog is loadCatalog without the network, for checking boards: an
// unknown type is not a board problem, so a missing release only makes the
// check less strict.
func cachedCatalog(dir string) (widgets.Catalog, error) {
	cfg, err := loadConfigOrEmpty(dir)
	if err != nil {
		return widgets.Catalog{}, err
	}
	return catalogFrom(dir, cfg, packages.OpenCached(dir, cfg.Packages))
}

// widgetFit is a widget that passed, and the areas it can be placed on.
type widgetFit struct {
	Path     string   `json:"path"`
	Name     string   `json:"name"`
	Surfaces []string `json:"surfaces"`
}

// validateTarget checks one path, whichever of the four things it is, and
// returns the widgets that passed beside the problems.
func validateTarget(path string, lookup widgets.Lookup) ([]widgets.Widget, []widgets.Problem, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, nil, fmt.Errorf("reading %s: %w", path, err)
	}
	if !info.IsDir() {
		if filepath.Base(path) == widgets.FileName {
			return loadOne(filepath.Dir(path))
		}
		problems, err := validateBoardFile(path, lookup)
		return nil, problems, err
	}
	if _, err := os.Stat(filepath.Join(path, widgets.FileName)); err == nil {
		return loadOne(path)
	}
	if _, err := os.Stat(packages.ManifestPath(path)); err == nil {
		m, err := packages.ParseManifest(path)
		if err != nil {
			return nil, nil, err
		}
		root, ok := packages.WidgetsDir(m)
		if !ok {
			return nil, nil, fmt.Errorf("package %s declares no `widgets:` folder", m.Name)
		}
		valid, problems := widgets.LoadAll(root)
		return valid, problems, nil
	}
	return nil, nil, fmt.Errorf("%s is neither a widget, a package nor a board", path)
}

func validateBoardFile(path string, lookup widgets.Lookup) ([]widgets.Problem, error) {
	b, _, problems, err := widgets.ReadBoard(path)
	if err != nil {
		return nil, err
	}
	// ParseBoard hands back an empty board with its one problem when the file
	// is not a board at all; checking that empty board would only add noise.
	if len(problems) > 0 && reflect.ValueOf(b).IsZero() {
		return problems, nil
	}
	return append(problems, widgets.ValidateBoard(b, path, lookup)...), nil
}

func loadOne(dir string) ([]widgets.Widget, []widgets.Problem, error) {
	w, problems := widgets.Load(dir)
	if len(problems) > 0 {
		return nil, problems, nil
	}
	return []widgets.Widget{w}, nil, nil
}

// defaultValidateTargets is every board, and every own package with widgets.
func defaultValidateTargets(dir string) ([]string, error) {
	boards, err := filepath.Glob(filepath.Join(widgets.BoardsDir(dir), "*.yml"))
	if err != nil {
		return nil, fmt.Errorf("listing the boards: %w", err)
	}
	targets := boards
	entries, err := os.ReadDir(packages.LocalDir(dir))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("reading %s: %w", packages.LocalDir(dir), err)
	}
	for _, entry := range entries {
		pkgDir := filepath.Join(packages.LocalDir(dir), entry.Name())
		m, err := packages.ParseManifest(pkgDir)
		if err != nil {
			continue
		}
		if _, ok := packages.WidgetsDir(m); ok {
			targets = append(targets, pkgDir)
		}
	}
	return targets, nil
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
