package widgets

import (
	"fmt"
	"sort"
	"strings"
)

// The origins of a widget, which are the origins of its package.
const (
	OriginRelease = "release"
	OriginLocal   = "local"
)

// PackageWidgets is one package that declares a widgets folder, and the copy
// of it that won: yours over the release's, as everywhere else.
type PackageWidgets struct {
	Package string
	// Scope is the package's scope, "machine" or "workspace": it decides which
	// flag the "add the package" hint names.
	Scope   string
	Origin  string
	Version string
	Root    string
	// Dir is the package's own folder, the one holding package.yml.
	Dir string
}

// Installed says how far a package is from a machine.
type Installed struct {
	Added  bool
	Synced bool
}

// Entry is one usable widget, as `widgets list --format json` prints it.
type Entry struct {
	Name              string            `json:"name"`
	Package           string            `json:"package"`
	Widget            string            `json:"widget"`
	Origin            string            `json:"origin"`
	Version           string            `json:"version"`
	Path              string            `json:"path"`
	PackagePath       string            `json:"package_path"`
	Summary           string            `json:"summary"`
	RequiresEngine    string            `json:"requires_engine"`
	Fits              []string          `json:"fits"`
	Context           map[string]string `json:"context"`
	Inputs            map[string]Input  `json:"inputs"`
	Source            Source            `json:"source"`
	View              ViewRef           `json:"view"`
	Sizes             []string          `json:"sizes"`
	DefaultSize       string            `json:"default_size"`
	Places            []string          `json:"places"`
	Single            bool              `json:"single"`
	Surfaces          []string          `json:"surfaces"`
	Available         bool              `json:"available"`
	UnavailableReason string            `json:"unavailable_reason"`
}

// ListProblem is a widget left out of the catalog, and why.
type ListProblem struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

// Catalog is every widget this configuration can reach.
type Catalog struct {
	Engine          string        `json:"engine"`
	PackagesRelease string        `json:"packages_release"`
	Widgets         []Entry       `json:"widgets"`
	Problems        []ListProblem `json:"problems"`
}

// Resolve builds the catalog from the packages that declare widgets. installed
// answers how far one package is from a machine.
func Resolve(release string, pkgs []PackageWidgets, installed func(pkg string) Installed) Catalog {
	catalog := Catalog{Engine: Engine, PackagesRelease: release, Widgets: []Entry{}, Problems: []ListProblem{}}
	for _, pkg := range pkgs {
		found, problems := LoadAll(Owner{Dir: pkg.Dir, Name: pkg.Package}, pkg.Root)
		for _, p := range problems {
			catalog.Problems = append(catalog.Problems, ListProblem{Path: p.Path, Message: p.Message})
		}
		for _, w := range found {
			catalog.Widgets = append(catalog.Widgets, entryFor(w, pkg, installed(pkg.Package)))
		}
	}
	sort.Slice(catalog.Widgets, func(i, j int) bool { return catalog.Widgets[i].Name < catalog.Widgets[j].Name })
	return catalog
}

// Find returns the widget with this full name, <package>/<widget>.
func (c Catalog) Find(name string) (Entry, bool) {
	for _, e := range c.Widgets {
		if e.Name == name {
			return e, true
		}
	}
	return Entry{}, false
}

// Availability applies the rule: a widget fed only by the app's own
// providers, or by a url, works without its package; anything else runs
// what the package installs, so it needs the package added and synced.
func Availability(w Widget, pkg PackageWidgets, state Installed) (bool, string) {
	if !needsPackage(w.Source) {
		return true, ""
	}
	flag := "--machine"
	if pkg.Scope == "workspace" {
		flag = "--workspace"
	}
	switch {
	case !state.Added:
		return false, fmt.Sprintf("add the package: devmachine packages add %s %s <name>", pkg.Package, flag)
	case !state.Synced:
		return false, "sync to install the package: devmachine sync"
	}
	return true, ""
}

func entryFor(w Widget, pkg PackageWidgets, state Installed) Entry {
	available, reason := Availability(w, pkg, state)
	entry := Entry{
		Name:              pkg.Package + "/" + w.Name,
		Package:           pkg.Package,
		Widget:            w.Name,
		Origin:            pkg.Origin,
		Version:           pkg.Version,
		Path:              w.Dir,
		PackagePath:       pkg.Dir,
		Summary:           w.Summary,
		RequiresEngine:    w.Requires.Engine,
		Fits:              w.Fits,
		Context:           w.Context,
		Inputs:            w.Inputs,
		Source:            w.Source,
		View:              w.View,
		Sizes:             w.Sizes,
		DefaultSize:       w.DefaultSize,
		Places:            w.Places,
		Single:            w.Single,
		Surfaces:          Surfaces(w),
		Available:         available,
		UnavailableReason: reason,
	}
	if entry.Context == nil {
		entry.Context = map[string]string{}
	}
	if entry.Inputs == nil {
		entry.Inputs = map[string]Input{}
	}
	if entry.Places == nil {
		entry.Places = []string{}
	}
	if entry.Surfaces == nil {
		entry.Surfaces = []string{}
	}
	return entry
}

func needsPackage(s Source) bool {
	if s.Kind == SourceURL {
		return false
	}
	return !strings.HasPrefix(s.Name, "app/")
}
