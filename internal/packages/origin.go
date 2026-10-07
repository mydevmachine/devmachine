package packages

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// OriginFile marks a package installed from a git address and says where it
// came from. A package folder without it is one you wrote yourself.
const OriginFile = ".devmachine-source.yml"

// Origin is what OriginFile holds: the address and ref it was fetched from,
// the commit that ref pointed at, and when.
type Origin struct {
	URL         string `yaml:"url" json:"url"`
	Ref         string `yaml:"ref" json:"ref"`
	Commit      string `yaml:"commit" json:"commit"`
	InstalledAt string `yaml:"installed_at" json:"installed_at"`
}

// ReadOrigin reads the origin of the package in dir. ok is false when it has
// none: you wrote it yourself.
func ReadOrigin(dir string) (Origin, bool, error) {
	path := filepath.Join(dir, OriginFile)
	body, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Origin{}, false, nil
	}
	if err != nil {
		return Origin{}, false, fmt.Errorf("reading %s: %w", path, err)
	}
	var o Origin
	if err := yaml.Unmarshal(body, &o); err != nil {
		return Origin{}, false, fmt.Errorf("parsing %s: %w", path, err)
	}
	return o, true, nil
}

// WriteOrigin records where the package in dir came from. It replaces
// whatever is at that name and never writes through a link: a fetched
// repository may ship one there pointing at one of its own task files.
func WriteOrigin(dir string, o Origin) error {
	body, err := yaml.Marshal(o)
	if err != nil {
		return fmt.Errorf("writing the origin of %s: %w", dir, err)
	}
	path := filepath.Join(dir, OriginFile)
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("replacing %s: %w", path, err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	if _, err := f.Write(body); err != nil {
		_ = f.Close()
		return fmt.Errorf("writing %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}
