package commands

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/mydevmachine/devmachine/internal/packages"
)

// settingProblem says why one setting does not fit the type its package
// declares, prefixed with the setting so the person sees which one, or nil.
// A package the lookup cannot find, or a variable it does not declare or did
// not type, is left alone: sync is what reports a package that does not exist.
func settingProblem(key string, value any, manifest func(string) (packages.Manifest, bool)) error {
	pkg, name, ok := strings.Cut(key, ".")
	if !ok {
		return nil
	}
	m, found := manifest(pkg)
	if !found {
		return nil
	}
	v, declared := m.Variables[name]
	if !declared {
		return nil
	}
	err := v.Check(value)
	if err == nil {
		return nil
	}
	if strings.HasPrefix(err.Error(), "[") {
		return fmt.Errorf("%s%w", key, err)
	}
	return fmt.Errorf("%s: %w", key, err)
}

// refuseMistypedSettings checks the settings an edit writes against what is
// already on disk, so a command that edits the configuration never waits on
// the network for it.
func refuseMistypedSettings(dir, release string, settings map[string]any, set []string) error {
	store := packages.OpenCached(dir, release)
	lookup := func(name string) (packages.Manifest, bool) {
		found, err := store.Get(name)
		return found.Manifest, err == nil
	}
	for _, raw := range set {
		key, _, _ := strings.Cut(raw, "=")
		value, held := settings[key]
		if !held {
			continue
		}
		if err := settingProblem(key, value, lookup); err != nil {
			return err
		}
	}
	return nil
}

// refuseMistypedPlan stops a sync, before the machine is changed, on a
// setting that does not fit its type, whoever wrote it.
func refuseMistypedPlan(plan packages.MachinePlan) error {
	var problems []error
	for _, resolved := range append([]packages.Resolved{plan.OnMachine}, plan.Workspaces...) {
		manifests := map[string]packages.Manifest{}
		for _, found := range resolved.Ordered {
			manifests[found.Manifest.Name] = found.Manifest
		}
		lookup := func(name string) (packages.Manifest, bool) {
			m, ok := manifests[name]
			return m, ok
		}
		for _, key := range slices.Sorted(maps.Keys(resolved.Target.Settings)) {
			if err := settingProblem(key, resolved.Target.Settings[key], lookup); err != nil {
				problems = append(problems, fmt.Errorf("%s %q: %w", resolved.Target.Kind, resolved.Target.Name, err))
			}
		}
	}
	return errors.Join(problems...)
}
