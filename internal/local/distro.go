package local

import (
	_ "embed"
	"fmt"
)

// Distro is the system a local machine is created with.
type Distro string

// The systems create-local makes.
const (
	Ubuntu Distro = "ubuntu"
	Arch   Distro = "arch"
)

//go:embed machine-arch.yaml
var archTemplate []byte

var templates = map[Distro][]byte{
	Ubuntu: machineTemplate,
	Arch:   archTemplate,
}

// ParseDistro reads the --distro flag. Empty is Ubuntu, the default.
func ParseDistro(s string) (Distro, error) {
	if s == "" {
		return Ubuntu, nil
	}
	d := Distro(s)
	if _, ok := templates[d]; !ok {
		return "", fmt.Errorf("--distro %q: use ubuntu or arch", s)
	}
	return d, nil
}
