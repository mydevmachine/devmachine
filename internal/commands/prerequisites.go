package commands

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/mydevmachine/devmachine/internal/config"
	"github.com/mydevmachine/devmachine/internal/facts"
	"github.com/mydevmachine/devmachine/internal/packages"
	"github.com/mydevmachine/devmachine/internal/provision"
	"github.com/mydevmachine/devmachine/internal/remote"
	"golang.org/x/term"
)

// The answers --package-manager takes, and the package each one means.
const (
	managerBrew  = "brew"
	managerPorts = "ports"
)

var managerPackages = map[string]string{
	managerBrew:  packages.MacBrew,
	managerPorts: packages.MacPorts,
}

// errPrerequisitesDeclined is the person saying no to installing what a Mac
// lacks. Nothing was installed, and setup goes no further.
var errPrerequisitesDeclined = errors.New("nothing was installed; setup stops here")

// hasTerminal says whether a person is there to answer a question. Without
// one, a question that needs a real answer becomes a flag in the error.
var hasTerminal = func(in io.Reader) bool {
	f, ok := in.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

// ansiblePrep is what setup and machines add need to get Ansible onto a
// machine: on Linux nothing beyond the connection, on a Mac the package
// manager package and the person's consent.
type ansiblePrep struct {
	dir     string
	release string
	in      *bufio.Reader
	// canAsk is a person at a terminal, who can choose and agree.
	canAsk               bool
	packageManager       string
	installPrerequisites bool
	// addPackage records a package setup added to the machine.
	addPackage func(name string) error
	// replaceStarting swaps a new machine's starting packages for the Mac's.
	// It is nil where the machine's packages are already the person's.
	replaceStarting func(names []string) error
}

// newAnsiblePrep reads the answers a run was given.
func newAnsiblePrep(dir, release string, r *bufio.Reader, in io.Reader, opts setupOptions,
	addPackage func(string) error) (ansiblePrep, error) {
	if err := checkPackageManager(opts.packageManager); err != nil {
		return ansiblePrep{}, err
	}
	return ansiblePrep{
		dir: dir, release: release, in: r,
		canAsk:               hasTerminal(in),
		packageManager:       opts.packageManager,
		installPrerequisites: opts.installPrerequisites,
		addPackage:           addPackage,
	}, nil
}

func checkPackageManager(answer string) error {
	if answer == "" {
		return nil
	}
	if _, ok := managerPackages[answer]; !ok {
		return fmt.Errorf("--package-manager %q: use brew (Homebrew) or ports (MacPorts)", answer)
	}
	return nil
}

// observed is what a machine's bootstrap reported about it.
type observed struct {
	AnsiblePlaybook string
	PathPrefix      []string
}

// recordObserved keeps what the bootstrap reported in the machine's facts, so
// sync calls Ansible by that path and run puts the prefix on PATH. A later
// read that cannot find ansible-playbook keeps this one.
var recordObserved = func(dir, machine string, o observed) error {
	f, found, err := facts.Load(dir, machine)
	if err != nil {
		return err
	}
	if !found {
		f.ObservedAt = time.Now().Truncate(time.Second)
	}
	_, err = facts.Save(dir, machine, facts.Reported(f, o.AnsiblePlaybook, o.PathPrefix))
	return err
}

// prepareAnsible installs Ansible: from the CLI's own table on Linux, through
// the package manager package's bootstrap on a Mac.
func prepareAnsible(ctx context.Context, client remote.Client, out io.Writer, system remote.System,
	m config.Machine, prep ansiblePrep) error {
	if !system.MacOS() {
		return installAnsible(ctx, client, out)
	}
	if prep.replaceStarting != nil {
		if starting, ok := startingPackagesOnMac(ctx, prep.dir, prep.release, m.Name, m.Packages, out); ok {
			if err := prep.replaceStarting(starting); err != nil {
				return err
			}
			m.Packages = starting
		}
	}
	return prepareMac(ctx, client, out, m, prep)
}

// prepareMac copies the machine's package manager package to the Mac and runs
// its bootstrap there, as the admin login.
func prepareMac(ctx context.Context, client remote.Client, out io.Writer, m config.Machine, prep ansiblePrep) error {
	store, err := openStore(ctx, prep.dir, prep.release)
	if err != nil {
		return err
	}
	found, err := chooseMacPackage(ctx, client, out, store, m, prep)
	if err != nil {
		return err
	}
	name := found.Manifest.Name

	target := path.Join(remote.MacBootstrapDir, name)
	tarball, err := provision.Tar(nil, map[string]string{name: found.Manifest.Path})
	if err != nil {
		return err
	}
	// Unpacking never deletes: a file an older copy left would stay.
	if _, err := client.Run(ctx, remote.AsRoot("rm -rf "+quoteForShell(target))); err != nil {
		return fmt.Errorf("emptying %s: %w", target, err)
	}
	if err := client.Upload(ctx, remote.MacBootstrapDir, tarball); err != nil {
		return err
	}
	b := remote.Bootstrap{Path: path.Join(target, filepath.ToSlash(found.Manifest.Bootstrap))}
	return runBootstrap(ctx, client, out, m.Name, b, prep)
}

// chooseMacPackage is the one package with a bootstrap among the machine's
// packages. With none, it adds the one for the package manager the Mac
// already has, or the one the person chooses.
func chooseMacPackage(ctx context.Context, client remote.Client, out io.Writer, store *packages.Store,
	m config.Machine, prep ansiblePrep) (packages.Found, error) {
	listed, err := listedBootstrap(store, m)
	if err != nil || listed != nil {
		return derefFound(listed), err
	}

	answer := prep.packageManager
	if answer == "" {
		have, err := remote.MacManagers(ctx, client)
		if err != nil {
			return packages.Found{}, err
		}
		answer, err = pickManager(out, m.Name, have, prep)
		if err != nil {
			return packages.Found{}, err
		}
	}
	return addManagerPackage(out, store, m, managerPackages[answer], prep)
}

func derefFound(f *packages.Found) packages.Found {
	if f == nil {
		return packages.Found{}
	}
	return *f
}

// listedBootstrap is the machine's own choice, nil when it made none.
func listedBootstrap(store *packages.Store, m config.Machine) (*packages.Found, error) {
	listed, err := store.Bootstrapping(m.Packages)
	if err != nil {
		return nil, err
	}
	switch len(listed) {
	case 0:
		return nil, nil
	case 1:
		return &listed[0], nil
	}
	names := make([]string, 0, len(listed))
	for _, f := range listed {
		names = append(names, f.Manifest.Name)
	}
	return nil, fmt.Errorf("machine %q lists %s: keep one", m.Name, joinAnd(names))
}

// pickManager follows what is already on the Mac, and asks only when that
// says nothing: neither manager, or both.
func pickManager(out io.Writer, machine string, have remote.Managers, prep ansiblePrep) (string, error) {
	switch {
	case have.Brew && !have.Ports:
		fmt.Fprintf(out, "%s has Homebrew, so it uses the %s package.\n", machine, packages.MacBrew)
		return managerBrew, nil
	case have.Ports && !have.Brew:
		fmt.Fprintf(out, "%s has MacPorts, so it uses the %s package.\n", machine, packages.MacPorts)
		return managerPorts, nil
	}
	found := "neither Homebrew nor MacPorts"
	if have.Brew {
		found = "both Homebrew and MacPorts"
	}
	if !prep.canAsk {
		return "", fmt.Errorf("the Mac has %s: run again with --package-manager brew or --package-manager ports", found)
	}
	fmt.Fprintf(out, "\n%s has %s. Which package manager should it use?\n", machine, found)
	fmt.Fprintf(out, "  1) Homebrew (%s)\n  2) MacPorts (%s)\n", packages.MacBrew, packages.MacPorts)
	choice, err := ask(prep.in, out, "choice", "1")
	if err != nil {
		return "", err
	}
	switch choice {
	case "1", managerBrew:
		return managerBrew, nil
	case "2", managerPorts:
		return managerPorts, nil
	}
	return "", fmt.Errorf("%q is not one of the choices", choice)
}

func addManagerPackage(out io.Writer, store *packages.Store, m config.Machine, name string,
	prep ansiblePrep) (packages.Found, error) {
	found, err := store.Get(name)
	if err != nil {
		return packages.Found{}, fmt.Errorf("%w: pin a packages release that has %s with `devmachine packages pin`", err, name)
	}
	if found.Manifest.Bootstrap == "" {
		return packages.Found{}, fmt.Errorf("package %q has no bootstrap, so it cannot prepare a Mac: "+
			"pin a newer packages release with `devmachine packages pin`", name)
	}
	if prep.addPackage != nil && !slices.Contains(m.Packages, name) {
		if err := prep.addPackage(name); err != nil {
			return packages.Found{}, err
		}
		fmt.Fprintf(out, "added the %s package to %s.\n", name, m.Name)
	}
	return found, nil
}

// runBootstrap asks what is missing, installs it once the person agrees, and
// keeps the ansible-playbook the bootstrap reports.
func runBootstrap(ctx context.Context, client remote.Client, out io.Writer, machine string,
	b remote.Bootstrap, prep ansiblePrep) error {
	missing, err := b.Check(ctx, client)
	if err != nil {
		return err
	}

	progress := io.Discard
	if len(missing) > 0 {
		if err := agreeToInstall(out, machine, missing, prep); err != nil {
			return err
		}
		progress = out
	}
	prepared, err := b.Apply(ctx, client, progress)
	if err != nil {
		return err
	}
	if err := recordObserved(prep.dir, machine, observed{
		AnsiblePlaybook: prepared.AnsiblePlaybook, PathPrefix: prepared.PathPrefix,
	}); err != nil {
		return err
	}
	if len(missing) == 0 {
		fmt.Fprintf(out, "%s is already prepared: ansible-playbook is %s.\n", machine, prepared.AnsiblePlaybook)
	} else {
		fmt.Fprintf(out, "%s is prepared: ansible-playbook is %s.\n", machine, prepared.AnsiblePlaybook)
	}
	return nil
}

// agreeToInstall shows what would be installed and how long it takes, and
// gets a yes for it. Only --install-prerequisites or a person's answer is a
// yes: --yes means other things, and the macOS app passes it with nobody
// watching.
func agreeToInstall(out io.Writer, machine string, missing []remote.Prerequisite, prep ansiblePrep) error {
	fmt.Fprintf(out, "\n%s needs these before Ansible can run on it:\n", machine)
	names := make([]string, 0, len(missing))
	for _, p := range missing {
		fmt.Fprintf(out, "  - %s (about %d minutes)\n", p.Name, p.Minutes)
		names = append(names, p.Name)
	}
	switch {
	case prep.installPrerequisites:
		fmt.Fprintln(out, "--install-prerequisites: installing them now.")
		return nil
	case !prep.canAsk:
		return fmt.Errorf("nothing was installed: run again with --install-prerequisites to install %s",
			joinAnd(names))
	}
	ok, err := confirm(prep.in, out, "Install them now?")
	if err != nil {
		return err
	}
	if !ok {
		return errPrerequisitesDeclined
	}
	return nil
}

// joinAnd lists names the way a sentence does: "a", "a and b", "a, b and c".
func joinAnd(names []string) string {
	if len(names) < 2 {
		return strings.Join(names, "")
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}

// prepareExistingSelf is `setup`'s whole job for a self machine: no host key
// to trust, no key to install, no password, no hardening. It runs the
// machine's package manager package's bootstrap on your computer, from the
// package cache: a copy under /opt would need sudo on your own Mac.
func prepareExistingSelf(ctx context.Context, out io.Writer, m config.Machine, prep ansiblePrep) error {
	store, err := openStore(ctx, prep.dir, prep.release)
	if err != nil {
		return err
	}
	listed, err := listedBootstrap(store, m)
	if err != nil {
		return err
	}
	found := derefFound(listed)
	if listed == nil {
		name := packages.MacBrew
		if prep.packageManager != "" {
			name = managerPackages[prep.packageManager]
		}
		if found, err = store.Get(name); err != nil || found.Manifest.Bootstrap == "" {
			return fmt.Errorf("no %s package with a bootstrap is available here: run `devmachine packages pin`, "+
				"then `devmachine setup --machine %s` again", name, m.Name)
		}
	}

	client, _, err := dial(ctx, m, "")
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()
	return runBootstrap(ctx, client, out, m.Name, remote.Bootstrap{Path: found.Manifest.BootstrapPath()}, prep)
}
