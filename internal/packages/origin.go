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

// WriteOrigin records where the package in dir came from.
func WriteOrigin(dir string, o Origin) error {
	body, err := yaml.Marshal(o)
	if err != nil {
		return fmt.Errorf("writing the origin of %s: %w", dir, err)
	}
	path := filepath.Join(dir, OriginFile)
	if err := os.WriteFile(path, body, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}
