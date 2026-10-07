package commands

import (
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
			cfg, err := loadConfigOrEmpty(dir)
			if err != nil {
				return err
			}
			names := targetNamesOf(cfg)
			targets := args
			problems := []widgets.Problem{}
			warnings := []widgets.Problem{}
			if len(targets) == 0 {
				var broken []widgets.Problem
				if targets, broken, err = defaultValidateTargets(dir); err != nil {
					return err
				}
				problems = append(problems, broken...)
			}

			fits := []widgetFit{}
			for _, target := range targets {
				valid, found, warned, err := validateTarget(target, catalog, names)
				if err != nil {
					return err
				}
				problems = append(problems, found...)
				warnings = append(warnings, warned...)
				fits = append(fits, valid...)
			}

			if opts.format == formatJSON {
				if err := writeJSON(cmd.OutOrStdout(), struct {
					OK       bool              `json:"ok"`
					Checked  []string          `json:"checked"`
					Widgets  []widgetFit       `json:"widgets"`
					Problems []widgets.Problem `json:"problems"`
					Warnings []widgets.Problem `json:"warnings"`
				}{len(problems) == 0, nonNil(targets), fits, problems, warnings}); err != nil {
					return err
				}
			} else {
				if len(targets) == 0 && len(problems) == 0 {
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
				for _, w := range warnings {
					cmd.Printf("warning: %s\n", w.Error())
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

// targetNamesOf is what a widget may name as its target.
func targetNamesOf(cfg config.Config) widgets.TargetNames {
	var names widgets.TargetNames
	for _, m := range cfg.Machines {
		names.Machines = append(names.Machines, m.Name)
	}
	for _, w := range cfg.Workspaces {
		names.Workspaces = append(names.Workspaces, w.Name)
	}
	return names
}

// targetWarnings names, for each package widget, a machine or workspace it
// names literally that this configuration lacks. A warning, not a problem:
// a published widget is written for many configurations, not only yours.
func targetWarnings(found []widgets.Widget, names widgets.TargetNames) []widgets.Problem {
	warnings := []widgets.Problem{}
	for _, w := range found {
		if message := widgets.UnknownTarget(w.Source, names); message != "" {
			warnings = append(warnings, widgets.Problem{Path: filepath.Join(w.Dir, widgets.FileName), Message: message})
		}
	}
	return warnings
}

// validateTarget checks one path, whichever of the four things it is, and
// returns the widgets that passed beside the problems and the warnings.
func validateTarget(path string, catalog widgets.Catalog, names widgets.TargetNames) ([]widgetFit, []widgets.Problem, []widgets.Problem, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("reading %s: %w", path, err)
	}
	if !info.IsDir() {
		if filepath.Base(path) == widgets.FileName {
			return loadOne(filepath.Dir(path), names)
		}
		b, _, problems, err := boardProblems(path, catalog.Find)
		if err != nil {
			return nil, nil, nil, err
		}
		problems = append(problems, widgets.BoardTargetProblems(b, path, names)...)
		problems = append(problems, widgets.BoardProviderProblems(b, path, catalog.KnownProviders(), catalog.PackagesRelease != "")...)
		return nil, problems, widgets.BoardWarnings(b, path), nil
	}
	if fileExists(filepath.Join(path, widgets.FileName)) {
		return loadOne(path, names)
	}
	if manifest := packages.ManifestPath(path); fileExists(manifest) {
		m, err := packages.ParseManifest(path)
		if err != nil {
			return nil, []widgets.Problem{{Path: manifest, Message: err.Error()}}, nil, nil
		}
		root, ok := packages.WidgetsDir(m)
		if !ok {
			return nil, []widgets.Problem{{Path: manifest, Message: fmt.Sprintf("package %s declares no `widgets:` folder", m.Name)}}, nil, nil
		}
		valid, problems := widgets.LoadAll(packages.WidgetOwner(m), root)
		return fitsOf(m.Name, valid), problems, targetWarnings(valid, names), nil
	}
	return nil, nil, nil, fmt.Errorf("%s is neither a widget, a package nor a board", path)
}

// boardProblems reads the board at path and returns it, the bytes it was read
// from, and everything wrong with it.
func boardProblems(path string, lookup widgets.Lookup) (widgets.Board, []byte, []widgets.Problem, error) {
	b, read, problems, err := widgets.ReadBoard(path)
	if err != nil {
		return widgets.Board{}, nil, nil, err
	}
	// ParseBoard hands back an empty board with its one problem when the file
	// is not a board at all; checking that empty board would only add noise.
	if len(problems) > 0 && reflect.ValueOf(b).IsZero() {
		return b, read, problems, nil
	}
	return b, read, append(problems, widgets.ValidateBoard(b, path, lookup)...), nil
}

func loadOne(dir string, names widgets.TargetNames) ([]widgetFit, []widgets.Problem, []widgets.Problem, error) {
	owner := ownerOf(dir)
	w, problems := widgets.LoadIn(owner, dir)
	if len(problems) > 0 {
		return nil, problems, nil, nil
	}
	found := []widgets.Widget{w}
	return fitsOf(owner.Name, found), nil, targetWarnings(found, names), nil
}

func fitsOf(pkg string, valid []widgets.Widget) []widgetFit {
	fits := make([]widgetFit, 0, len(valid))
	for _, w := range valid {
		name := w.Name
		if pkg != "" {
			name = pkg + "/" + w.Name
		}
		fits = append(fits, widgetFit{Path: w.Dir, Name: name, Surfaces: nonNil(widgets.Surfaces(w))})
	}
	return fits
}

// ownerOf is the package whose widgets folder holds the widget folder dir, or
// the zero owner when no package claims it.
func ownerOf(dir string) widgets.Owner {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return widgets.Owner{}
	}
	folder := filepath.Dir(abs)
	for at := folder; ; at = filepath.Dir(at) {
		if fileExists(packages.ManifestPath(at)) {
			m, err := packages.ParseManifest(at)
			if err != nil {
				return widgets.Owner{}
			}
			if root, ok := packages.WidgetsDir(m); ok && filepath.Clean(root) == folder {
				return packages.WidgetOwner(m)
			}
			return widgets.Owner{}
		}
		if filepath.Dir(at) == at {
			return widgets.Owner{}
		}
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// defaultValidateTargets is every board, and every own package with widgets.
// An own package whose package.yml does not read is a problem, never skipped.
func defaultValidateTargets(dir string) ([]string, []widgets.Problem, error) {
	targets, err := filepath.Glob(filepath.Join(widgets.BoardsDir(dir), "*.yml"))
	if err != nil {
		return nil, nil, fmt.Errorf("listing the boards: %w", err)
	}
	found, broken := packages.OpenCached(dir, "").Scan()
	for _, f := range found {
		if _, ok := packages.WidgetsDir(f.Manifest); ok {
			targets = append(targets, f.Manifest.Path)
		}
	}
	problems := make([]widgets.Problem, 0, len(broken))
	for _, b := range broken {
		problems = append(problems, widgets.Problem{Path: b.Path, Message: b.Err.Error()})
	}
	return targets, problems, nil
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
