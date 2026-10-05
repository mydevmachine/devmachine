package packages

import "path/filepath"

// The package manager packages a Mac chooses between. Each declares a
// bootstrap that installs its manager and Ansible through it.
const (
	MacBrew  = "mac-brew"
	MacPorts = "mac-ports"
)

// BootstrapPath is the bootstrap script in the package's own directory, empty
// when the package has none.
func (m Manifest) BootstrapPath() string {
	if m.Bootstrap == "" {
		return ""
	}
	return filepath.Join(m.Path, filepath.FromSlash(m.Bootstrap))
}

// Bootstrapping returns the packages among names that declare a bootstrap. A
// name the store does not have is passed over: sync is where a missing
// package is reported, with the list of what exists.
func (s *Store) Bootstrapping(names []string) ([]Found, error) {
	var out []Found
	for _, name := range names {
		if !s.Has(name) {
			continue
		}
		found, err := s.Get(name)
		if err != nil {
			return nil, err
		}
		if found.Manifest.Bootstrap != "" {
			out = append(out, found)
		}
	}
	return out, nil
}
