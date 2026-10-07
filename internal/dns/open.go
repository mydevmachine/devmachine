package dns

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/mydevmachine/devmachine/internal/packages"
	"github.com/mydevmachine/devmachine/internal/provision"
	"github.com/mydevmachine/devmachine/internal/remote"
)

// KindDNS is what a package.yml writes in `kind` to be a DNS provider.
const KindDNS = "dns"

// ProviderManual is Manual's name, and what `--dns-provider manual` asks for.
const ProviderManual = "manual"

// buildExternal wraps a found DNS package as a Provider, sourced from the
// credential it declares. base is where the bundle lives on the machine the
// client is connected to: RemoteDir for one reached over SSH, or the self
// bundle directory for a self machine.
func buildExternal(found packages.Found, base string, client remote.Client) (*External, error) {
	m := found.Manifest

	credential := secretCredential(m)
	if credential == "" && m.Kind == KindDNS {
		return nil, fmt.Errorf(
			"the %s package declares no credential, so there is nothing to source before it runs", m.Name)
	}

	entrypoint := provision.RolePath(base, found.Source, m.Name, m.Entrypoint)
	return NewExternal(m.Name, client, entrypoint, credential, m.Commands), nil
}

// secretCredential is the first secret the package declares, the one sourced
// before its entrypoint or a script of it runs; "" when it declares none.
func secretCredential(m packages.Manifest) string {
	for _, c := range m.Credentials {
		if c.Kind == packages.KindSecret {
			return c.Name
		}
	}
	return ""
}

// Script builds a call to one file of an installed package, such as a
// widget's script: the same copy on the machine, the same account and the
// same credential as the entrypoint, without the entrypoint's command list,
// which limits only the entrypoint.
func Script(dir, machine, workspace, base, name, rel string, client remote.Client) (*External, error) {
	clean := path.Clean(rel)
	if rel == "" || path.IsAbs(rel) || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return nil, fmt.Errorf("--script %q names a file inside the package, relative to its package.yml", rel)
	}
	found, err := lookupInstalled(dir, machine, workspace, name)
	if err != nil {
		return nil, err
	}
	if info, err := os.Stat(filepath.Join(found.Manifest.Path, filepath.FromSlash(clean))); err != nil || info.IsDir() {
		return nil, fmt.Errorf("%s has no file %s", name, clean)
	}
	file := provision.RolePath(base, found.Source, name, clean)
	return NewExternal(name, client, file, secretCredential(found.Manifest), nil), nil
}

// Installed builds every package of kind: dns that the lock says is on this
// machine.
//
// The lock is what says a package is installed, not the cache: a recipe in
// the cache and not in the lock is the ordinary state for every package this
// machine never asked for, and asking one anyway fails in a way nobody can
// act on.
func Installed(dir, machine, base string, client remote.Client) ([]*External, error) {
	lock, err := packages.LoadLock(dir)
	if err != nil {
		return nil, err
	}
	store, err := packages.Open(context.Background(), dir, lock.Release)
	if err != nil {
		return nil, err
	}

	var out []*External
	for _, entry := range lock.Machines[machine] {
		found, err := store.Get(entry.Name)
		if err != nil {
			return nil, err
		}
		if found.Manifest.Kind != KindDNS {
			continue
		}
		ext, err := buildExternal(found, base, client)
		if err != nil {
			return nil, err
		}
		out = append(out, ext)
	}
	return out, nil
}

// lookupInstalled finds a single package the lock says is installed on this
// machine, or for this workspace, whatever the package's kind.
//
// Unlike Installed, it reports rather than skips: somebody who named a
// package wants to know why it did not work, not silence. workspace is
// empty for a lookup that only ever means the machine.
func lookupInstalled(dir, machine, workspace, name string) (packages.Found, error) {
	lock, err := packages.LoadLock(dir)
	if err != nil {
		return packages.Found{}, err
	}

	installed := lockHas(lock.Machines[machine], name)
	if !installed && workspace != "" {
		installed = lockHas(lock.Workspaces[workspace], name)
	}
	if !installed {
		where := machine
		if workspace != "" {
			where = fmt.Sprintf("%s or workspace %s", machine, workspace)
		}
		return packages.Found{}, fmt.Errorf(
			"%s is not installed on %s. Add it with `devmachine packages add %s` and apply with `devmachine sync`",
			name, where, name)
	}

	store, err := packages.Open(context.Background(), dir, lock.Release)
	if err != nil {
		return packages.Found{}, err
	}
	return store.Get(name)
}

// lockHas is whether the lock names this package among these entries.
func lockHas(entries []packages.LockEntry, name string) bool {
	for _, entry := range entries {
		if entry.Name == name {
			return true
		}
	}
	return false
}

// One builds a single installed DNS provider by name.
func One(dir, machine, base, name string, client remote.Client) (*External, error) {
	found, err := lookupInstalled(dir, machine, "", name)
	if err != nil {
		return nil, err
	}
	if found.Manifest.Kind != KindDNS {
		return nil, fmt.Errorf("%s is not a DNS provider (kind %q)", name, found.Manifest.Kind)
	}
	return buildExternal(found, base, client)
}

// Any builds a single installed package's entrypoint, whatever its kind.
//
// One refuses anything that is not a DNS provider, which is right for a
// command that only makes sense against one. `run --package` reaches any
// package that declares an entrypoint, and this is the door for that.
// workspace is empty for a machine-only lookup, or a workspace name so a
// package installed for that workspace alone is also found.
func Any(dir, machine, workspace, base, name string, client remote.Client) (*External, error) {
	found, err := lookupInstalled(dir, machine, workspace, name)
	if err != nil {
		return nil, err
	}
	if found.Manifest.Entrypoint == "" {
		return nil, fmt.Errorf("%s declares no entrypoint, so there is nothing to call", name)
	}
	return buildExternal(found, base, client)
}

// Choice is what a command decided to act on, and why.
type Choice struct {
	Provider Provider
	Name     string
	Zone     string
	Why      string
}

// Choose resolves a name to a provider and a zone.
//
// It returns an error only when --dns-provider names something unusable or
// when two installed providers claim the same zone: every other outcome has
// an answer, and the answer is manual. A nil client means nothing can be
// asked, and the answer is manual too.
func Choose(ctx context.Context, dir, machine, base, name, providerFlag string,
	client remote.Client, out io.Writer) (Choice, error) {
	return NewChooser(dir, machine, base, client, out).Choose(ctx, name, providerFlag)
}

// Chooser is Choose for several names on one machine. It reads which
// providers are installed once, and asks each provider for its zones once,
// however many names it is asked about.
type Chooser struct {
	dir, machine, base string
	client             remote.Client
	out                io.Writer

	loaded    bool
	providers []*External
	zoners    []Zoner
	loadErr   error
}

// NewChooser returns a Chooser for one machine's providers.
func NewChooser(dir, machine, base string, client remote.Client, out io.Writer) *Chooser {
	return &Chooser{dir: dir, machine: machine, base: base, client: client, out: out}
}

// Choose is the package-level Choose, with what it learns kept for the next
// name.
func (c *Chooser) Choose(ctx context.Context, name, providerFlag string) (Choice, error) {
	manual := Choice{Provider: NewManual(c.out), Name: ProviderManual, Zone: name}

	if providerFlag == ProviderManual {
		manual.Why = "the --dns-provider flag"
		manual.Provider = newManualBecause(c.out, "--dns-provider manual was given.")
		return manual, nil
	}
	if c.client == nil {
		// A provider runs on the machine. With no machine there is nothing
		// to ask and nothing to write, but there is still a record to print.
		manual.Why = "the machine could not be reached"
		manual.Provider = newManualBecause(c.out,
			fmt.Sprintf("%s could not be reached, so no DNS provider on it was asked.", c.machine))
		return manual, nil
	}
	if providerFlag != "" {
		p, err := One(c.dir, c.machine, c.base, providerFlag, c.client)
		if err != nil {
			return Choice{}, err
		}
		return Choice{Provider: p, Name: p.Name(), Zone: name, Why: "the --dns-provider flag"}, nil
	}

	if !c.loaded {
		c.loaded = true
		c.providers, c.loadErr = Installed(c.dir, c.machine, c.base, c.client)
		for _, p := range c.providers {
			c.zoners = append(c.zoners, &zonesOnce{External: p})
		}
	}
	if c.loadErr != nil {
		return Choice{}, c.loadErr
	}
	if len(c.providers) == 0 {
		manual.Why = "no DNS provider is installed"
		manual.Provider = newManualBecause(c.out, fmt.Sprintf("No DNS provider is installed on %s.", c.machine))
		return manual, nil
	}

	holder, err := WhoHolds(ctx, c.zoners, name)
	switch {
	case err == nil:
		for _, p := range c.providers {
			if p.Name() == holder.Provider {
				return Choice{Provider: p, Name: p.Name(), Zone: holder.Zone,
					Why: fmt.Sprintf("%s holds %s", holder.Provider, holder.Zone)}, nil
			}
		}
		return Choice{}, fmt.Errorf("provider %q answered and then vanished", holder.Provider)
	case errors.Is(err, ErrAmbiguous):
		return Choice{}, err
	default:
		// Nobody holds it. That is ordinary — but saying nothing about a
		// provider that could not be asked is not.
		fmt.Fprintf(c.out, "No installed provider holds %s: %v\n", name, err)
		manual.Why = "no installed provider holds this zone"
		return manual, nil
	}
}

// zonesOnce asks a provider for its zones the first time, and answers from
// that afterwards.
type zonesOnce struct {
	*External
	asked bool
	zones []string
	err   error
}

func (z *zonesOnce) Zones(ctx context.Context) ([]string, error) {
	if !z.asked {
		z.asked = true
		z.zones, z.err = z.External.Zones(ctx)
	}
	return z.zones, z.err
}
