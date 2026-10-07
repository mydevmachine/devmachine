package packages

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

// Where a recipe came from. A local package and a published one are the same
// kind of thing; this only says which copy was used.
const (
	SourceLocal   = "local"
	SourceRelease = "release"
)

// Found is a package and the copy of it that won.
type Found struct {
	Manifest Manifest
	Source   string
}

// Store is every package this run can reach.
type Store struct {
	localDir   string
	releaseDir string
	version    string
	checksum   string
}

// LocalDir is where the operator's own packages live. There is no separate
// overlay mechanism: this directory holds packages in the same format, and a
// name here beats the same name in the release.
func LocalDir(configDir string) string { return filepath.Join(configDir, "packages") }

// Open makes the pinned release available and returns the store over it.
//
// An empty version means no release is pinned, and the operator's own
// packages are all there is.
func Open(ctx context.Context, configDir, version string) (*Store, error) {
	s := &Store{localDir: LocalDir(configDir)}
	if version == "" {
		return s, nil
	}

	dir, checksum, err := Fetch(ctx, configDir, version)
	if err != nil {
		return nil, err
	}
	s.releaseDir, s.version, s.checksum = dir, version, checksum
	return s, nil
}

// OpenCached is Open without the network: the pinned release is used only
// when it is already in the cache, which a sync always leaves behind.
//
// It is for work done on every connection, such as resolving an address,
// where a download would stall the connection it is meant to help.
func OpenCached(configDir, version string) *Store {
	s := &Store{localDir: LocalDir(configDir)}
	if version == "" {
		return s
	}
	dir := CacheDir(configDir, version)
	sum, err := os.ReadFile(filepath.Join(dir, checksumFile))
	if err != nil {
		return s
	}
	s.releaseDir, s.version, s.checksum = filepath.Join(dir, "packages"), version, strings.TrimSpace(string(sum))
	return s
}

// Version is the release pin this store was opened at, empty when there is none.
func (s *Store) Version() string { return s.version }

// Checksum is the release tarball's checksum, empty when there is no release.
func (s *Store) Checksum() string { return s.checksum }

// sources are looked at in order, so the first that has a name wins.
func (s *Store) sources() []struct{ dir, source string } {
	return []struct{ dir, source string }{
		{s.localDir, SourceLocal},
		{s.releaseDir, SourceRelease},
	}
}

// Get returns one package by name.
func (s *Store) Get(name string) (Found, error) {
	for _, candidate := range s.sources() {
		if candidate.dir == "" {
			continue
		}
		path := filepath.Join(candidate.dir, name)
		if _, err := os.Stat(ManifestPath(path)); err != nil {
			continue
		}
		m, err := ParseManifest(path)
		if err != nil {
			return Found{}, err
		}
		return Found{Manifest: m, Source: candidate.source}, nil
	}

	available, err := s.All()
	if err != nil {
		return Found{}, err
	}
	names := make([]string, 0, len(available))
	for _, f := range available {
		names = append(names, f.Manifest.Name)
	}
	sort.Strings(names)
	if len(names) == 0 {
		return Found{}, fmt.Errorf("no package named %q, and none is available", name)
	}
	return Found{}, fmt.Errorf("no package named %q. Available: %s", name, strings.Join(names, ", "))
}

// Has says whether any source holds a package of this name.
func (s *Store) Has(name string) bool {
	for _, candidate := range s.sources() {
		if candidate.dir == "" {
			continue
		}
		if _, err := os.Stat(ManifestPath(filepath.Join(candidate.dir, name))); err == nil {
			return true
		}
	}
	return false
}

// GetRelease returns the pinned published copy even when a local package has
// the same name. It is used for reserved first-party content whose identity
// must not be changed by an overlay.
func (s *Store) GetRelease(name string) (Found, error) {
	if s.releaseDir == "" {
		return Found{}, errors.New("no package release is pinned")
	}
	path := filepath.Join(s.releaseDir, name)
	if _, err := os.Stat(ManifestPath(path)); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Found{}, &NotInReleaseError{Version: s.version, Name: name}
		}
		return Found{}, err
	}
	m, err := ParseManifest(path)
	if err != nil {
		return Found{}, err
	}
	return Found{Manifest: m, Source: SourceRelease}, nil
}

// NotInReleaseError says the pinned release has no package of that name.
type NotInReleaseError struct {
	Version string
	Name    string
}

func (e *NotInReleaseError) Error() string {
	return fmt.Sprintf("package release %s has no package named %q", e.Version, e.Name)
}

// All lists every package, counting an overridden name once. It refuses the
// whole listing when one manifest cannot be read.
func (s *Store) All() ([]Found, error) {
	found, broken := s.Scan()
	if len(broken) > 0 {
		return nil, broken[0].Err
	}
	return found, nil
}

// Broken is a package, or a packages folder, that could not be read.
type Broken struct {
	Path string
	Err  error
}

// Scan is All that keeps going: every package that reads, and every one that
// does not. A broken package still claims its name, so a broken package of
// yours never falls back to the release's copy without a word.
func (s *Store) Scan() ([]Found, []Broken) {
	seen := map[string]bool{}
	var out []Found
	var broken []Broken

	for _, candidate := range s.sources() {
		if candidate.dir == "" {
			continue
		}
		entries, err := os.ReadDir(candidate.dir)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			broken = append(broken, Broken{Path: candidate.dir, Err: err})
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() || seen[entry.Name()] {
				continue
			}
			path := filepath.Join(candidate.dir, entry.Name())
			if _, err := os.Stat(ManifestPath(path)); err != nil {
				continue
			}
			seen[entry.Name()] = true
			m, err := ParseManifest(path)
			if err != nil {
				broken = append(broken, Broken{Path: ManifestPath(path), Err: err})
				continue
			}
			out = append(out, Found{Manifest: m, Source: candidate.source})
		}
	}

	slices.SortFunc(out, func(a, b Found) int { return strings.Compare(a.Manifest.Name, b.Manifest.Name) })
	return out, broken
}
