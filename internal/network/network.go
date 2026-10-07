// Package network finds the package that answers for a private network's
// host entries, and runs its scripts.
//
// A host entry written `<prefix>:<name>` belongs to the package whose
// manifest declares that prefix. The CLI knows the prefix and the scripts,
// never the product: Tailscale, Headscale or anything else is one package
// among others.
package network

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/mydevmachine/devmachine/internal/config"
	"github.com/mydevmachine/devmachine/internal/packages"
)

// SettingsEnv is the variable every network script reads the machine's
// settings for its package from, as one JSON object.
const SettingsEnv = "DEVMACHINE_SETTINGS"

// UnavailableExit is the exit status a resolve script gives when the network
// is not reachable from this computer: not installed, not running, not
// signed in. The entry is skipped, the way an unplugged cable is.
const UnavailableExit = 3

// ErrUnavailable says a network is not reachable from here, which is not a
// failure: the next address is what it exists for.
var ErrUnavailable = errors.New("not available here")

// unavailable carries the script's own words, which say more than "not
// available here" does, and still matches ErrUnavailable.
type unavailable struct{ reason string }

func (u unavailable) Error() string {
	if u.reason == "" {
		return ErrUnavailable.Error()
	}
	return u.reason
}

func (u unavailable) Is(target error) bool { return target == ErrUnavailable }

// ResolveTimeout bounds one resolve script. It is short because an address
// waits on it, and the next address may be the one that works.
var ResolveTimeout = 5 * time.Second

var prefixPattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// Split reads a host entry as `<prefix>:<name>`. ok is false for anything ssh
// can dial as written: an IP address, IPv6 included, or a DNS name.
func Split(entry string) (prefix, name string, ok bool) {
	if net.ParseIP(entry) != nil || strings.HasPrefix(entry, "[") {
		return "", "", false
	}
	prefix, name, found := strings.Cut(entry, ":")
	if !found || name == "" || !prefixPattern.MatchString(prefix) {
		return "", "", false
	}
	return prefix, name, true
}

// Provider is a package that answers for one prefix.
type Provider struct {
	Package string
	Source  string
	// Dir is the package's directory on this computer, in the pinned
	// release's cache or in the configuration's own packages/.
	Dir      string
	Network  packages.Network
	defaults map[string]any
}

// Load finds every network package the machine's configuration can reach,
// without the network: the pinned release counts only once a sync cached it.
func Load(m config.Machine) ([]Provider, error) {
	if m.ConfigDir == "" {
		return nil, nil
	}
	all, err := packages.OpenCached(m.ConfigDir, m.PackagesRelease).All()
	if err != nil {
		return nil, err
	}
	var out []Provider
	for _, found := range all {
		if p, ok := FromFound(found); ok {
			out = append(out, p)
		}
	}
	return out, nil
}

// FromFound reads a package as a Provider, when it declares a network.
func FromFound(found packages.Found) (Provider, bool) {
	manifest := found.Manifest
	if manifest.Network == nil || manifest.Network.Prefix == "" {
		return Provider{}, false
	}
	defaults := map[string]any{}
	for name, v := range manifest.Variables {
		defaults[name] = v.Default
	}
	return Provider{
		Package: manifest.Name, Source: found.Source, Dir: manifest.Path,
		Network: *manifest.Network, defaults: defaults,
	}, true
}

// Pick chooses the package for a prefix: the one the machine installs, in the
// order it lists them, and otherwise the first the store holds.
func Pick(providers []Provider, prefix string, installed []string) (Provider, bool) {
	var candidates []Provider
	for _, p := range providers {
		if p.Network.Prefix == prefix {
			candidates = append(candidates, p)
		}
	}
	if len(candidates) == 0 {
		return Provider{}, false
	}
	for _, name := range installed {
		for _, p := range candidates {
			if p.Package == name {
				return p, true
			}
		}
	}
	return candidates[0], true
}

// Settings is what the package's scripts see: its declared defaults, with the
// machine's own `<package>.<name>` settings on top.
func (p Provider) Settings(machineSettings map[string]any) map[string]any {
	out := maps.Clone(p.defaults)
	if out == nil {
		out = map[string]any{}
	}
	maps.Copy(out, config.SettingsFor(machineSettings, p.Package))
	return out
}

// SettingsJSON is Settings as the one JSON object SettingsEnv carries.
func SettingsJSON(settings map[string]any) (string, error) {
	if settings == nil {
		settings = map[string]any{}
	}
	body, err := json.Marshal(settings)
	if err != nil {
		return "", fmt.Errorf("writing the network settings: %w", err)
	}
	return string(body), nil
}

// TailscaleAppDir holds the CLI of Tailscale's own Mac app. It is the app's
// executable, and it answers to `tailscale` because the Mac's disk ignores
// case unless somebody formatted it otherwise.
const TailscaleAppDir = "/Applications/Tailscale.app/Contents/MacOS"

// goos is a seam, so the tests can be a Mac anywhere.
var goos = runtime.GOOS

// scriptEnviron is this computer's environment for a resolve script. A Mac
// that has Tailscale only as the app has no tailscale on PATH, so the app's
// directory goes last: a CLI installed on its own still wins.
func scriptEnviron() []string {
	env := os.Environ()
	if goos != "darwin" {
		return env
	}
	for i, kv := range env {
		if strings.HasPrefix(kv, "PATH=") {
			env[i] = kv + string(os.PathListSeparator) + TailscaleAppDir
			return env
		}
	}
	return append(env, "PATH="+TailscaleAppDir)
}

// Resolve runs the package's resolve script on this computer and returns the
// addresses it printed, in its order.
//
// The name is an argument, never part of a shell line: nothing in it is
// interpreted.
func (p Provider) Resolve(ctx context.Context, name string, settings map[string]any) ([]string, error) {
	encoded, err := SettingsJSON(settings)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, ResolveTimeout)
	defer cancel()

	path := filepath.Join(p.Dir, p.Network.Resolve)
	cmd := exec.CommandContext(ctx, path, name)
	cmd.Env = append(scriptEnviron(), SettingsEnv+"="+encoded)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	// A script that leaves a child holding its output open would otherwise
	// keep Wait blocked long after the timeout killed the script itself.
	cmd.WaitDelay = time.Second

	runErr := cmd.Run()
	reason := strings.TrimSpace(stderr.String())
	if ctx.Err() == context.DeadlineExceeded {
		return nil, fmt.Errorf("the %s package's resolve did not answer within %s", p.Package, ResolveTimeout)
	}
	if runErr != nil {
		var exit *exec.ExitError
		if errors.As(runErr, &exit) && exit.ExitCode() == UnavailableExit {
			return nil, unavailable{reason: reason}
		}
		if reason != "" {
			return nil, fmt.Errorf("the %s package's resolve failed: %s", p.Package, reason)
		}
		return nil, fmt.Errorf("the %s package's resolve failed: %w", p.Package, runErr)
	}

	var out []string
	for _, line := range strings.Split(stdout.String(), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if net.ParseIP(line) == nil {
			return nil, fmt.Errorf("the %s package's resolve printed %q, which is not an IP address", p.Package, line)
		}
		if !slices.Contains(out, line) {
			out = append(out, line)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("the %s package's resolve answered with no address for %q", p.Package, name)
	}
	return out, nil
}
