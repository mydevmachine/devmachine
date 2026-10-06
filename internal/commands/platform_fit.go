package commands

import (
	"fmt"
	"slices"
	"strings"

	"github.com/mydevmachine/devmachine/internal/config"
	"github.com/mydevmachine/devmachine/internal/packages"
)

// foreignPackage is a package that cannot run on a machine's system, either
// itself or through something it needs.
type foreignPackage struct {
	name      string
	because   string
	platforms []string
}

// clause says why the package cannot run, ready to be followed by "and
// <machine> is <system>".
func (f foreignPackage) clause() string {
	runs := "runs only on " + strings.Join(f.platforms, " and ")
	if f.because == f.name {
		return runs
	}
	return fmt.Sprintf("needs %s, which %s,", f.because, runs)
}

// foreignPackages names the packages in list that would not run on platform.
//
// It reads only what is already on disk, so a command that edits the
// configuration never waits on the network for it. A name the store does not
// hold is left alone: sync is what reports a package that does not exist.
func foreignPackages(dir, release string, list []string, platform string) []foreignPackage {
	if platform == "" {
		return nil
	}
	store := packages.OpenCached(dir, release)
	var out []foreignPackage
	for _, name := range list {
		if blocker, platforms, ok := blockingPackage(store, name, platform, map[string]bool{}); ok {
			out = append(out, foreignPackage{name: name, because: blocker, platforms: platforms})
		}
	}
	return out
}

// blockingPackage walks a package and what it needs, and returns the first one
// whose platforms leave out platform.
func blockingPackage(store *packages.Store, name, platform string, seen map[string]bool) (string, []string, bool) {
	if seen[name] {
		return "", nil, false
	}
	seen[name] = true
	found, err := store.Get(name)
	if err != nil {
		return "", nil, false
	}
	m := found.Manifest
	if len(m.Platforms) > 0 && !slices.Contains(m.Platforms, platform) {
		return m.Name, m.Platforms, true
	}
	for _, need := range m.Needs {
		if blocker, platforms, ok := blockingPackage(store, need, platform, seen); ok {
			return blocker, platforms, true
		}
	}
	return "", nil, false
}

// fitNewWorkspace drops from a default or copied list what cannot run on the
// machine, and says so. A list somebody typed out is refused instead: a name
// they asked for by hand silently going missing would only surface later, as
// a workspace without it.
func fitNewWorkspace(out func(format string, a ...any), dir string, cfg config.Config, machine config.Machine,
	name string, list []string, typed bool) ([]string, error) {
	platform := knownPlatform(dir, machine)
	foreign := slices.DeleteFunc(foreignPackages(dir, cfg.Packages, list, platform),
		func(f foreignPackage) bool { return f.name == packages.AccountPackage })
	if len(foreign) == 0 {
		return list, nil
	}
	if typed {
		f := foreign[0]
		return nil, fmt.Errorf("package %q %s and machine %q is %s: leave it out of --packages",
			f.name, f.clause(), machine.Name, platform)
	}
	kept := slices.Clone(list)
	for _, f := range foreign {
		kept = slices.DeleteFunc(kept, func(n string) bool { return n == f.name })
		out("%s %s and %s is %s, so %s starts without it.\n", f.name, f.clause(), machine.Name, platform, name)
	}
	return kept, nil
}

// refuseForeignAdd stops `packages add` before it writes a package the
// target's machine cannot run, rather than leaving sync to find out.
func refuseForeignAdd(dir string, cfg config.Config, target packageTarget, name string) error {
	machineName := target.name
	if target.kind == packages.ScopeWorkspace {
		w, err := cfg.Workspace(target.name)
		if err != nil {
			return err
		}
		machineName = machineNameOf(cfg, w)
	}
	machine, err := cfg.Machine(machineName)
	if err != nil {
		return nil
	}
	platform := knownPlatform(dir, machine)
	foreign := foreignPackages(dir, cfg.Packages, []string{name}, platform)
	if len(foreign) == 0 {
		return nil
	}
	return fmt.Errorf("package %q %s and machine %q is %s, so it was not added",
		name, foreign[0].clause(), machine.Name, platform)
}

func workspacePackageTarget(name string) packageTarget {
	return packageTarget{kind: packages.ScopeWorkspace, name: name}
}
