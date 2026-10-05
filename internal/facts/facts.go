// Package facts keeps what a command last read about a machine: which system
// it runs, its package manager, where Ansible is.
//
// It is an observation, not an intent, so it lives under state/ in the
// configuration directory and never in config.yml. The next command that
// reaches the machine reads it again.
package facts

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/mydevmachine/devmachine/internal/remote"
)

// The platforms, as package.yml names them.
const (
	platformLinux = "linux"
	platformMacOS = "macos"
)

// Facts is one machine's state file. The names follow Ansible's facts, so a
// person or an agent who knows one reads the other.
type Facts struct {
	ObservedAt          time.Time `json:"observed_at"`
	System              string    `json:"system"`
	OSFamily            string    `json:"os_family"`
	Distribution        string    `json:"distribution"`
	DistributionVersion string    `json:"distribution_version"`
	PkgMgr              string    `json:"pkg_mgr"`
	ServiceMgr          string    `json:"service_mgr"`
	Architecture        string    `json:"architecture"`
	AnsiblePlaybook     string    `json:"ansible_playbook"`
	// PathPrefix is what a plain SSH command needs in front of PATH to find
	// the tools the package manager installed. Empty where it needs nothing.
	PathPrefix []string `json:"path_prefix"`
}

// Platform is the system as package.yml's `platforms` names it, or "" for one
// this CLI does not know.
func (f Facts) Platform() string {
	switch f.System {
	case "Linux":
		return platformLinux
	case "Darwin":
		return platformMacOS
	}
	return ""
}

// Same reports whether two observations say the same thing, whenever they
// were made.
func (f Facts) Same(other Facts) bool {
	f.PathPrefix, other.PathPrefix = orEmpty(f.PathPrefix), orEmpty(other.PathPrefix)
	return f.System == other.System && f.OSFamily == other.OSFamily &&
		f.Distribution == other.Distribution && f.DistributionVersion == other.DistributionVersion &&
		f.PkgMgr == other.PkgMgr && f.ServiceMgr == other.ServiceMgr &&
		f.Architecture == other.Architecture && f.AnsiblePlaybook == other.AnsiblePlaybook &&
		slices.Equal(f.PathPrefix, other.PathPrefix)
}

func orEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// Path is where a machine's facts are kept. Machine names are unique only
// within one configuration, so the file lives in it.
func Path(dir, machine string) string {
	return filepath.Join(dir, "state", "machines", machine+".json")
}

// Load reads a machine's facts. found is false when nothing was ever observed.
func Load(dir, machine string) (Facts, bool, error) {
	var f Facts
	body, err := os.ReadFile(Path(dir, machine))
	if errors.Is(err, os.ErrNotExist) {
		return f, false, nil
	}
	if err != nil {
		return f, false, fmt.Errorf("reading the facts of %s: %w", machine, err)
	}
	if err := json.Unmarshal(body, &f); err != nil {
		return f, false, fmt.Errorf("reading %s: %w", Path(dir, machine), err)
	}
	f.PathPrefix = orEmpty(f.PathPrefix)
	return f, true, nil
}

// Save writes a machine's facts, and reports whether it wrote. The macOS app
// watches the configuration directory and reloads on every change, so an
// unchanged observation is not written and the file never appears half-written.
func Save(dir, machine string, f Facts) (bool, error) {
	// observed_at is when the machine was first seen as the file describes it.
	if old, found, err := Load(dir, machine); err == nil && found && old.Same(f) {
		return false, nil
	}

	f.PathPrefix = orEmpty(f.PathPrefix)
	body, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return false, fmt.Errorf("encoding the facts of %s: %w", machine, err)
	}
	body = append(body, '\n')

	path := Path(dir, machine)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, fmt.Errorf("creating %s: %w", filepath.Dir(path), err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+machine+".*.tmp")
	if err != nil {
		return false, fmt.Errorf("writing the facts of %s: %w", machine, err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		return false, fmt.Errorf("writing %s: %w", tmp.Name(), err)
	}
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		return false, fmt.Errorf("writing %s: %w", tmp.Name(), err)
	}
	if err := tmp.Close(); err != nil {
		return false, fmt.Errorf("writing %s: %w", tmp.Name(), err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return false, fmt.Errorf("writing %s: %w", path, err)
	}
	return true, nil
}

// ObserveCommand reads everything in one round trip. On a Mac it also looks
// where Homebrew and MacPorts install, since a plain SSH command does not.
const ObserveCommand = `kernel=$(uname -s)
echo "kernel=$kernel"
echo "machine=$(uname -m)"
if [ "$kernel" = Darwin ]; then
	PATH="$PATH:/opt/homebrew/bin:/usr/local/bin:/opt/local/bin"
	echo "version=$(sw_vers -productVersion)"
fi
echo "ansible_playbook=$(command -v ansible-playbook)"
sed 's/^/os-release./' /etc/os-release 2>/dev/null
true`

// Observe reads the machine's facts. It only reads.
func Observe(ctx context.Context, c remote.Client, now time.Time) (Facts, error) {
	out, err := c.Run(ctx, ObserveCommand)
	if err != nil {
		return Facts{}, fmt.Errorf("reading what the machine runs: %w", err)
	}
	values := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if ok {
			values[key] = strings.Trim(strings.TrimSpace(value), `"'`)
		}
	}

	f := Facts{
		ObservedAt:      now.Truncate(time.Second),
		System:          values["kernel"],
		Architecture:    values["machine"],
		AnsiblePlaybook: values["ansible_playbook"],
		PathPrefix:      []string{},
	}
	switch f.System {
	case "Linux":
		linuxFacts(&f, values["os-release.ID"], values["os-release.VERSION_ID"])
	case "Darwin":
		darwinFacts(&f, values["version"])
	}
	return f, nil
}

// linuxDistributions maps os-release's ID onto Ansible's names for it, and the
// package manager and init system that come with it.
var linuxDistributions = map[string]struct{ family, name, pkgMgr string }{
	"debian":  {"Debian", "Debian", "apt"},
	"ubuntu":  {"Debian", "Ubuntu", "apt"},
	"arch":    {"Archlinux", "Archlinux", "pacman"},
	"archarm": {"Archlinux", "Archlinux", "pacman"},
}

func linuxFacts(f *Facts, id, version string) {
	f.DistributionVersion = version
	d, ok := linuxDistributions[id]
	if !ok {
		f.Distribution = id
		return
	}
	f.OSFamily, f.Distribution, f.PkgMgr, f.ServiceMgr = d.family, d.name, d.pkgMgr, "systemd"
}

// macPrefixes are where each Mac package manager installs, and so what a
// plain SSH command has to put in front of PATH.
var macPrefixes = []struct{ root, pkgMgr string }{
	{"/opt/local", "macports"},
	{"/opt/homebrew", "homebrew"},
	{"/usr/local", "homebrew"},
}

func darwinFacts(f *Facts, version string) {
	f.OSFamily, f.Distribution, f.DistributionVersion, f.ServiceMgr = "Darwin", "MacOSX", version, "launchd"
	for _, p := range macPrefixes {
		if strings.HasPrefix(f.AnsiblePlaybook, p.root+"/") {
			f.PkgMgr = p.pkgMgr
			f.PathPrefix = []string{p.root + "/bin", p.root + "/sbin"}
			return
		}
	}
}

// Record observes the machine and saves what it found. It never fails the
// command that called it: the facts are a by-product of a read that already
// succeeded, and ok is false when nothing usable was read.
func Record(ctx context.Context, dir, machine string, c remote.Client, now time.Time) (Facts, bool) {
	f, err := Observe(ctx, c, now)
	if err != nil || f.System == "" {
		return Facts{}, false
	}
	if old, found, err := Load(dir, machine); err == nil && found {
		f = keepReported(old, f)
	}
	_, _ = Save(dir, machine, f)
	return f, true
}

// keepReported keeps where the Mac bootstrap said Ansible is when this read
// could not find it: a plain SSH command on a Mac may not, and a MacPorts
// install can carry a Python suffix in its name.
func keepReported(old, now Facts) Facts {
	if now.System != "Darwin" || old.System != "Darwin" || now.AnsiblePlaybook != "" {
		return now
	}
	now.AnsiblePlaybook, now.PkgMgr, now.PathPrefix = old.AnsiblePlaybook, old.PkgMgr, old.PathPrefix
	return now
}

// String is the facts as a person reads them in one line.
func (f Facts) String() string {
	var b bytes.Buffer
	b.WriteString(f.System)
	if f.Distribution != "" {
		fmt.Fprintf(&b, ", %s", strings.TrimSpace(f.Distribution+" "+f.DistributionVersion))
	}
	if f.Architecture != "" {
		fmt.Fprintf(&b, ", %s", f.Architecture)
	}
	return b.String()
}
