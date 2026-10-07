package widgets

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
)

// PackageProvider is one command of a package that widgets may read: the
// fields of its JSON answer, and how often it may run at most.
type PackageProvider struct {
	Returns  map[string]string `json:"returns"`
	MinEvery string            `json:"min_every"`
}

// ProviderSet maps each package this configuration reaches onto the
// providers it declares, an empty map for a package with none, so a provider
// name tells "no package P" apart from "P has no provider C".
type ProviderSet map[string]map[string]PackageProvider

// Owner is the package a widget ships in: its folder, its name, and the
// providers it declares, which are the only package providers its widgets
// may read.
type Owner struct {
	Dir       string
	Name      string
	Providers map[string]PackageProvider
}

var (
	withKey = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)
	// providerPart mirrors the package-name and provider-name rules of
	// package.yml, which this package cannot import.
	providerPart = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)
)

func wellFormedProvider(pkg, command string) bool {
	return providerPart.MatchString(pkg) && providerPart.MatchString(command)
}

// SplitProviderName cuts a package provider's name, <package>/<command>. ok
// is false for an app provider and for any other shape.
func SplitProviderName(name string) (pkg, command string, ok bool) {
	pkg, command, found := strings.Cut(name, "/")
	if !found || pkg == "" || command == "" || strings.Contains(command, "/") ||
		slices.Contains(CurrentContract().PackageProvider.Reserved, pkg) {
		return "", "", false
	}
	return pkg, command, true
}

// IsPackageProvider says whether name is a package's command rather than one
// of the app's own providers.
func IsPackageProvider(name string) bool {
	_, _, ok := SplitProviderName(name)
	return ok
}

// RunsCode says whether a source runs something on a computer, a machine or a
// workspace, which is what makes a widget from a third-party package ask
// before it runs.
func RunsCode(s Source) bool {
	switch s.Kind {
	case SourceCommand, SourcePrompt, SourceSession:
		return true
	case SourceProvider:
		return IsPackageProvider(s.Name)
	}
	return false
}

func (o Owner) scope() ProviderSet {
	providers := o.Providers
	if providers == nil {
		providers = map[string]PackageProvider{}
	}
	return ProviderSet{o.Name: providers}
}

func providerList(declared map[string]PackageProvider) string {
	if len(declared) == 0 {
		return "none"
	}
	return strings.Join(sortedKeys(declared), ", ")
}

// checkPackageProvider applies the rules of a <package>/<command> source. A
// widget written in a board has no providers in its scope: whether its
// package is there is checked by BoardProviderProblems, so a package removed
// since never locks a board.
func checkPackageProvider(s Source, pkg, command string, scope sourceScope, c Contract, at reporter) {
	if !wellFormedProvider(pkg, command) {
		at("source.name", "source.name %s: is written <package>/<command>, each in lower case letters, digits, dashes and underscores, starting with a letter", s.Name)
		return
	}
	minimum := c.PackageProvider.MinEvery
	if !scope.inline || scope.providers != nil {
		if provider, found := findPackageProvider(s.Name, pkg, command, scope, at); found {
			minimum = orDefault(provider.MinEvery, minimum)
		}
	}
	switch {
	case s.Target.problem != "":
		at("source.target", "%s", s.Target.problem)
	case s.Target.IsZero() || s.Target.Local:
		field := "source"
		if s.Target.Local {
			field = "source.target"
		}
		at(field, "source.name %s: a package provider runs on a machine or workspace: write source.target: {machine: <name>} or {workspace: <name>}", s.Name)
	default:
		checkTarget(s.Target, scope, at)
	}
	for _, key := range sortedKeys(s.With) {
		if !withKey.MatchString(key) {
			at("source.with", "source.with.%s: %s gets it as --%s, so write the key in lower case letters, digits, dashes and underscores, starting with a letter",
				key, s.Name, key)
		}
		checkTemplates(s.With[key], "source.with."+key, "source.with", scope, at)
	}
	checkTimeout(s, c.Sources[SourceProvider], at)
	floor, _ := time.ParseDuration(minimum)
	every, err := time.ParseDuration(s.Every)
	switch {
	case s.Every == "":
		at("source", "every widget needs source.every, at least %s for %s, or manual", minimum, s.Name)
	case s.Every == EveryManual:
	case err != nil:
		at("source.every", "source.every %q is neither a duration nor manual: write it like 60s, 5m or manual", s.Every)
	case every < floor:
		at("source.every", "source.every %s is below the %s minimum of %s", s.Every, s.Name, minimum)
	}
}

func findPackageProvider(name, pkg, command string, scope sourceScope, at reporter) (PackageProvider, bool) {
	if !scope.inline && pkg != scope.owner {
		if scope.owner == "" {
			at("source.name", "source.name %s: only a widget in a package, or one written in a board, reads a package provider", name)
		} else {
			at("source.name", "source.name %s: a package widget reads only its own package's providers, written %s/<command>", name, scope.owner)
		}
		return PackageProvider{}, false
	}
	declared, known := scope.providers[pkg]
	if !known {
		at("source.name", "source.name %s: no package %s in the release or your own packages", name, pkg)
		return PackageProvider{}, false
	}
	provider, ok := declared[command]
	if !ok {
		at("source.name", "source.name %s: %s has no provider %s; its providers: %s", name, pkg, command, providerList(declared))
		return PackageProvider{}, false
	}
	return provider, true
}

// PackageSource is where a package installed from a git address came from.
type PackageSource struct {
	URL    string `json:"url"`
	Ref    string `json:"ref"`
	Commit string `json:"commit"`
}

// ProviderEntry is one package provider, as `widgets list --format json`
// prints it under providers.
type ProviderEntry struct {
	Package       string            `json:"package"`
	Command       string            `json:"command"`
	Scope         string            `json:"scope"`
	Trust         string            `json:"trust"`
	PackageSource *PackageSource    `json:"package_source,omitempty"`
	Returns       map[string]string `json:"returns"`
	MinEvery      string            `json:"min_every"`
}

// BoardProviderProblems checks every widget written in board b that reads a
// package provider against the packages this configuration reaches: the
// package is there, it declares that provider, and every is not below its
// minimum. It stays apart from ValidateBoard for the reason
// BoardTargetProblems does: removing a package must not lock a board.
// complete is false when no release was read, so a missing package may only
// be one the release has.
func BoardProviderProblems(b Board, path string, providers ProviderSet, complete bool) []Problem {
	floor, _ := time.ParseDuration(CurrentContract().PackageProvider.MinEvery)
	var problems []Problem
	for _, w := range b.Widgets {
		if !w.Inline || w.Source == nil || w.Source.Kind != SourceProvider {
			continue
		}
		pkg, command, ok := SplitProviderName(w.Source.Name)
		if !ok || !wellFormedProvider(pkg, command) {
			continue
		}
		at := func(field, format string, args ...any) {
			problems = append(problems, Problem{Path: path, Line: w.lineOf(field), Message: w.ID + ": " + fmt.Sprintf(format, args...)})
		}
		declared, known := providers[pkg]
		if !known {
			if complete {
				at("source.name", "source.name %s: no package %s in the release or your own packages", w.Source.Name, pkg)
			}
			continue
		}
		provider, found := declared[command]
		if !found {
			at("source.name", "source.name %s: %s has no provider %s; its providers: %s", w.Source.Name, pkg, command, providerList(declared))
			continue
		}
		minimum, err := time.ParseDuration(provider.MinEvery)
		if err != nil {
			continue
		}
		if every, err := time.ParseDuration(w.Source.Every); err == nil && every >= floor && every < minimum {
			at("source.every", "source.every %s is below the %s minimum of %s", w.Source.Every, w.Source.Name, provider.MinEvery)
		}
	}
	return problems
}
