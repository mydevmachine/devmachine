package packages

import (
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/mydevmachine/devmachine/internal/config"
)

// Target is one place packages are installed: a machine, or one workspace on
// it.
type Target struct {
	// Kind is ScopeMachine or ScopeWorkspace.
	Kind string `json:"kind"`
	Name string `json:"name"`
	// LinuxUser is empty for a machine.
	LinuxUser string   `json:"linux_user,omitempty"`
	Packages  []string `json:"packages,omitempty"`
	// Settings are this target's overrides, keyed `<package>.<name>`.
	Settings map[string]any `json:"settings,omitempty"`
	// Credentials is the operator's answer about each credential, by name,
	// with this target's own choice already over the configuration's.
	Credentials map[string]string `json:"credentials,omitempty"`
}

// Resolved is everything a target gets, in the order it has to run.
type Resolved struct {
	Target  Target
	Ordered []Found
}

// Extension is one package's contribution to a place another package opened.
type Extension struct {
	// From is the extending package, and Target is the machine or workspace
	// that asked for it.
	From   string `json:"from"`
	Target string `json:"target"`
	// Point is written <package>.<place>, such as caddy.sites.d.
	Point string `json:"point"`
	// Source is a path inside the extending package.
	Source string `json:"source"`
	// Into is the absolute path on the machine the extended package provides.
	Into string `json:"into"`
	// Scope is the extending package's own scope, ScopeMachine or
	// ScopeWorkspace, carried here so Path can name the file without looking
	// the package back up.
	Scope string `json:"scope"`
}

// Path is the absolute path this extension is written to on the machine: the
// package's name and the file's own name, and for a workspace package the
// workspace too, so two packages — or the same package for two workspaces —
// never collide on one destination.
func (e Extension) Path() string {
	name := e.From + "-" + path.Base(e.Source)
	if e.Scope == ScopeWorkspace {
		name = e.Target + "-" + name
	}
	return path.Join(e.Into, name)
}

// MachinePlan is everything that has to happen on one machine.
type MachinePlan struct {
	Machine config.Machine
	// OnMachine is what the machine itself gets; Workspaces is one entry per
	// workspace that lives on it.
	OnMachine  Resolved
	Workspaces []Resolved
	Extensions []Extension
	// Routes is every route of every workspace on this machine, in workspace
	// order, and SitesDir is where caddy takes them. SitesDir is empty when
	// caddy is not on the machine, and then Routes is empty too, because
	// ResolveMachine refuses a route with nowhere to go.
	Routes   []Route
	SitesDir string
	// OtherWorkspaces names every workspace that lives on another machine,
	// when caddy is here. This machine owns their routes file too: it is
	// written for the routes sent here with `via` and removed otherwise.
	OtherWorkspaces []string
	// PreviousExtensions is the absolute path of every extension file the
	// last successful sync of this machine wrote, from the lock. It is empty
	// on the first run after upgrading, and then Generate removes nothing —
	// ResolveMachine never sets it; the caller reads the lock and carries it
	// over, which keeps Generate pure.
	PreviousExtensions []string
}

// Route is a workspace's port answering to a public host.
type Route struct {
	Workspace string `json:"workspace"`
	Host      string `json:"host"`
	Port      int    `json:"port"`
	// From is the machine the port is on, when it is not this one.
	From string `json:"from,omitempty"`
	// Upstream is From's address as this machine reaches it. ResolveMachine
	// never sets it, which keeps it pure; the caller resolves it before
	// Generate, the way it carries PreviousExtensions over.
	Upstream string `json:"upstream,omitempty"`
}

// ResolveMachine works out the machine's own packages, each of its
// workspaces' packages, the order `needs` puts them in, and where every
// declared extension writes.
func ResolveMachine(store *Store, cfg config.Config, machine config.Machine, cliVersion string) (MachinePlan, error) {
	plan := MachinePlan{Machine: machine}

	resolved, err := resolveTarget(store, Target{
		Kind: ScopeMachine, Name: machine.Name, Packages: machine.Packages,
		Settings: machine.Settings,
	}, cliVersion)
	if err != nil {
		return plan, err
	}
	plan.OnMachine = resolved

	for _, w := range cfg.WorkspacesOn(machine.Name) {
		r, err := resolveTarget(store, Target{
			Kind: ScopeWorkspace, Name: w.Name, LinuxUser: w.LinuxUser(), Packages: w.Packages,
			Settings: w.Settings, Credentials: cfg.CredentialsFor(w),
		}, cliVersion)
		if err != nil {
			return plan, err
		}
		plan.Workspaces = append(plan.Workspaces, r)
	}

	extensions, err := wireExtensions(plan)
	if err != nil {
		return plan, err
	}
	plan.Extensions = extensions

	plan.SitesDir = sitesDirOf(plan)
	for _, served := range cfg.RoutesServedBy(machine.Name) {
		w, r := served.Workspace, served.Route
		if plan.SitesDir == "" {
			return MachinePlan{}, fmt.Errorf(
				"workspace %q publishes %s, but caddy is not on machine %q: "+
					"add it with `devmachine packages add caddy --machine %s`",
				w.Name, r.Host, machine.Name, machine.Name)
		}
		route := Route{Workspace: w.Name, Host: r.Host, Port: r.Port}
		if r.Via != "" {
			route.From = w.Machine
		}
		plan.Routes = append(plan.Routes, route)
	}
	if plan.SitesDir != "" {
		for _, w := range cfg.Workspaces {
			if !slices.ContainsFunc(cfg.WorkspacesOn(machine.Name), func(here config.Workspace) bool { return here.Name == w.Name }) {
				plan.OtherWorkspaces = append(plan.OtherWorkspaces, w.Name)
			}
		}
	}
	return plan, nil
}

// sitesDirOf finds the absolute path caddy provides for sites, empty when
// caddy is not on the machine.
func sitesDirOf(plan MachinePlan) string {
	for _, f := range plan.OnMachine.Ordered {
		if f.Manifest.Name == "caddy" {
			return f.Manifest.Provides["sites.d"]
		}
	}
	return ""
}

// resolveTarget expands one target's list through `needs` and orders it.
//
// The roots are visited in name order, so two configurations that list the
// same packages differently produce the same run: `needs` is the only thing
// that may decide an order.
func resolveTarget(store *Store, target Target, cliVersion string) (Resolved, error) {
	out := Resolved{Target: target}

	var (
		done     = map[string]bool{}
		visiting = map[string]bool{}
		visit    func(name string, chain []string) error
	)
	visit = func(name string, chain []string) error {
		if done[name] {
			return nil
		}
		if visiting[name] {
			return fmt.Errorf("the packages %s need each other in a circle",
				strings.Join(append(chain, name), " -> "))
		}
		visiting[name] = true

		found, err := store.Get(name)
		if err != nil {
			return err
		}
		if found.Manifest.Scope != target.Kind {
			return scopeError(found.Manifest, target)
		}
		if c := found.Manifest.Requires.CLI; c != "" {
			constraint, err := ParseConstraint(c)
			if err != nil {
				return fmt.Errorf("package %q: requires.cli %q: %w", name, c, err)
			}
			if !constraint.Allows(cliVersion) {
				return fmt.Errorf(
					"package %q needs a CLI %s, and this one is %s: upgrade with `brew upgrade devmachine`",
					name, constraint, cliVersion)
			}
		}

		for _, need := range found.Manifest.Needs {
			if err := visit(need, append(chain, name)); err != nil {
				return err
			}
		}

		visiting[name] = false
		done[name] = true
		out.Ordered = append(out.Ordered, found)
		return nil
	}

	roots := slices.Clone(target.Packages)
	slices.Sort(roots)
	if target.Kind == ScopeWorkspace && store.Has(AccountPackage) {
		roots = append([]string{AccountPackage}, roots...)
	}
	for _, name := range roots {
		if err := visit(name, nil); err != nil {
			return out, err
		}
	}
	return out, nil
}

// AccountPackage creates a workspace's account. Every workspace gets it,
// listed or not, and before anything else: a package that forgets to say it
// needs the account would otherwise run before the account exists. Every
// release carries it; only a set of packages without one goes without.
const AccountPackage = "workspace"

// scopeError says what to do, not just what was wrong. Naming the wrong kind
// of target is a mistake anybody makes once.
func scopeError(m Manifest, target Target) error {
	if m.Scope == ScopeWorkspace {
		return fmt.Errorf(
			"package %q is a workspace package, and %q is a machine: "+
				"add it with `devmachine packages add %s --workspace <name>`",
			m.Name, target.Name, m.Name)
	}
	return fmt.Errorf(
		"package %q is a machine package, and %q is a workspace: "+
			"add it with `devmachine packages add %s --machine <name>`",
		m.Name, target.Name, m.Name)
}

// wireExtensions resolves each `extends` onto the absolute path the extended
// package declared, and refuses one whose provider is not on this machine.
func wireExtensions(plan MachinePlan) ([]Extension, error) {
	provided := map[string]string{}
	for _, f := range plan.OnMachine.Ordered {
		for place, path := range f.Manifest.Provides {
			provided[f.Manifest.Name+"."+place] = path
		}
	}

	var out []Extension
	for _, resolved := range append([]Resolved{plan.OnMachine}, plan.Workspaces...) {
		for _, f := range resolved.Ordered {
			for _, point := range sortedKeys(f.Manifest.Extends) {
				into, ok := provided[point]
				if !ok {
					owner, _, _ := strings.Cut(point, ".")
					return nil, fmt.Errorf(
						"package %q extends %q, but %s is not installed on machine %q, or it does not provide %q",
						f.Manifest.Name, point, owner, plan.Machine.Name, point)
				}
				out = append(out, Extension{
					From:   f.Manifest.Name,
					Target: resolved.Target.Name,
					Point:  point,
					Source: f.Manifest.Extends[point],
					Into:   into,
					Scope:  f.Manifest.Scope,
				})
			}
		}
	}
	return out, nil
}
