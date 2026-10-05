package config

import (
	"fmt"
	"regexp"
	"strings"
)

// The locations a machine has when its configuration names none.
const (
	LocationExternal = "external"
	LocationLocal    = "local"
)

const maxLocationLength = 40

var locationPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9 ._-]{0,39}$`)

// NormalizeLocation trims and lowercases a location, and refuses one outside
// the allowed set. An empty location stays empty: it means the default.
func NormalizeLocation(location string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(location))
	if normalized == "" {
		return "", nil
	}
	if !locationPattern.MatchString(normalized) {
		return "", fmt.Errorf("%q is not a usable location: use at most %d lowercase letters, numbers, "+
			"spaces, dots, underscores and hyphens, starting with a letter or number", location, maxLocationLength)
	}
	return normalized, nil
}

// EffectiveLocation is where the machine is, never empty: what the
// configuration says, or, when it says nothing, local for your own computer
// and external for any other machine.
func (m Machine) EffectiveLocation() string {
	if location, err := NormalizeLocation(m.Location); err == nil && location != "" {
		return location
	}
	if m.Self {
		return LocationLocal
	}
	return LocationExternal
}
