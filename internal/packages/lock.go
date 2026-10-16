package packages

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"time"

	"gopkg.in/yaml.v3"
)

// LockFile answers "what is actually on this machine" without logging in to
// look, and is what makes two machines reproducible from one configuration.
const LockFile = "packages.lock"

// LockEntry is one package, where its copy came from, and what pins it.
type LockEntry struct {
	Name   string `yaml:"name" json:"name"`
	Source string `yaml:"source" json:"source"`
	// Version and Checksum are empty for a local package: there is nothing to
	// pin, and pretending otherwise would be a lie in a file whose whole job
	// is being true.
	Version  string `yaml:"version,omitempty" json:"version,omitempty"`
	Checksum string `yaml:"checksum,omitempty" json:"checksum,omitempty"`
}

// Lock is what the last sync applied, per target.
type Lock struct {
	Release    string                 `yaml:"release,omitempty" json:"release,omitempty"`
	AppliedAt  string                 `yaml:"applied_at" json:"applied_at"`
	Machines   map[string][]LockEntry `yaml:"machines" json:"machines"`
	Workspaces map[string][]LockEntry `yaml:"workspaces" json:"workspaces"`
	// Extensions is, per machine, the absolute path of every extension file
	// the sync that locked it wrote. A lock from before this field existed
	// loads with it empty, and Generate then removes nothing on that first
	// run — the files an already-removed package left stay until `devmachine
	// run` removes them by hand.
	Extensions map[string][]string `yaml:"extensions,omitempty" json:"extensions,omitempty"`
}

// LockPath is where the lock lives.
func LockPath(configDir string) string { return filepath.Join(configDir, LockFile) }

// LoadLock reads the lock. A missing one is the ordinary first state, not an
// error.
func LoadLock(configDir string) (Lock, error) {
	lock := Lock{Machines: map[string][]LockEntry{}, Workspaces: map[string][]LockEntry{}, Extensions: map[string][]string{}}

	path := LockPath(configDir)
	body, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return lock, nil
	}
	if err != nil {
		return lock, fmt.Errorf("reading %s: %w", path, err)
	}
	if err := yaml.Unmarshal(body, &lock); err != nil {
		return lock, fmt.Errorf("parsing %s: %w", path, err)
	}
	if lock.Machines == nil {
		lock.Machines = map[string][]LockEntry{}
	}
	if lock.Workspaces == nil {
		lock.Workspaces = map[string][]LockEntry{}
	}
	if lock.Extensions == nil {
		lock.Extensions = map[string][]string{}
	}
	return lock, nil
}

// WithPlan returns the lock as it is after this plan was applied.
//
// Only the machine and the workspaces in the plan are replaced: syncing one
// machine says nothing about another, and the lock must not claim it does.
//
// A run narrowed by tags only ran the packages it named, so only those are
// recorded; every other entry, and every extension it did not write, stays
// as the last full run left it. A run narrowed to one workspace works the
// same way: the machine and every other workspace keep their entries.
func (l Lock) WithPlan(plan MachinePlan, store *Store, now time.Time, tags []string) Lock {
	out := Lock{
		Release:    store.Version(),
		AppliedAt:  now.UTC().Format(time.RFC3339),
		Machines:   maps.Clone(l.Machines),
		Workspaces: maps.Clone(l.Workspaces),
		Extensions: maps.Clone(l.Extensions),
	}
	if out.Machines == nil {
		out.Machines = map[string][]LockEntry{}
	}
	if out.Workspaces == nil {
		out.Workspaces = map[string][]LockEntry{}
	}
	if out.Extensions == nil {
		out.Extensions = map[string][]string{}
	}

	if len(tags) == 0 && plan.OnlyWorkspace == "" {
		out.Machines[plan.Machine.Name] = lockEntries(plan.OnMachine.Ordered, store)
		for _, w := range plan.Workspaces {
			out.Workspaces[w.Target.Name] = lockEntries(w.Ordered, store)
		}
		out.Extensions[plan.Machine.Name] = extensionPaths(plan.Extensions)
		return out
	}

	tagged := func(name string) bool { return len(tags) == 0 || slices.Contains(tags, name) }
	if plan.OnlyWorkspace == "" {
		if entries, ran := taggedEntries(l.Machines[plan.Machine.Name], plan.OnMachine.Ordered, store, tagged); ran {
			out.Machines[plan.Machine.Name] = entries
		}
	}
	for _, w := range plan.RunWorkspaces() {
		if len(tags) == 0 {
			out.Workspaces[w.Target.Name] = lockEntries(w.Ordered, store)
		} else if entries, ran := taggedEntries(l.Workspaces[w.Target.Name], w.Ordered, store, tagged); ran {
			out.Workspaces[w.Target.Name] = entries
		}
	}
	var written []Extension
	for _, e := range plan.RunExtensions() {
		if tagged(e.From) {
			written = append(written, e)
		}
	}
	if len(written) > 0 {
		paths := append(slices.Clone(l.Extensions[plan.Machine.Name]), extensionPaths(written)...)
		sort.Strings(paths)
		out.Extensions[plan.Machine.Name] = slices.Compact(paths)
	}
	return out
}

// taggedEntries is a target's earlier entries with the packages a tagged run
// applied put in or brought up to date, and whether any of them ran at all.
func taggedEntries(previous []LockEntry, found []Found, store *Store, tagged func(string) bool) ([]LockEntry, bool) {
	out := slices.Clone(previous)
	ran := false
	for _, entry := range lockEntries(found, store) {
		if !tagged(entry.Name) {
			continue
		}
		ran = true
		i := slices.IndexFunc(out, func(e LockEntry) bool { return e.Name == entry.Name })
		if i >= 0 {
			out[i] = entry
		} else {
			out = append(out, entry)
		}
	}
	return out, ran
}

// extensionPaths is every path a plan's extensions write, sorted so the lock
// does not churn when the same set is resolved in a different order.
func extensionPaths(extensions []Extension) []string {
	out := make([]string, 0, len(extensions))
	for _, e := range extensions {
		out = append(out, e.Path())
	}
	sort.Strings(out)
	return out
}

func lockEntries(found []Found, store *Store) []LockEntry {
	out := make([]LockEntry, 0, len(found))
	for _, f := range found {
		entry := LockEntry{Name: f.Manifest.Name, Source: f.Source}
		if f.Source == SourceRelease {
			entry.Version, entry.Checksum = store.Version(), store.Checksum()
		}
		out = append(out, entry)
	}
	return out
}

// SaveLock writes the lock atomically. A lock truncated by an interrupted
// write is worse than one that is out of date.
func SaveLock(configDir string, l Lock) error {
	body, err := yaml.Marshal(l)
	if err != nil {
		return fmt.Errorf("rendering %s: %w", LockFile, err)
	}
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return err
	}

	temp, err := os.CreateTemp(configDir, LockFile+".*")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())

	if _, err := temp.Write(body); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(temp.Name(), 0o644); err != nil {
		return err
	}
	return os.Rename(temp.Name(), LockPath(configDir))
}
