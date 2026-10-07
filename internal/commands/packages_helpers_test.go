package commands

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mydevmachine/devmachine/internal/config"
	"github.com/mydevmachine/devmachine/internal/packages"
	"github.com/mydevmachine/devmachine/internal/remote"
)

// localClient runs a command on this machine instead of over SSH, so a test
// can see a real package entrypoint really execute — the only way to prove
// `run --package` and `packages help` reach it, rather than a mock of it.
//
// The command runs whole, credential sourcing included: this computer has no
// /etc/devmachine/<name>/env, which is the state of a machine before
// `credentials push`, and the entrypoint must still be reached.
type localClient struct {
	// root maps the machine's package directory onto where the test wrote
	// the package instead, since /opt/devmachine does not exist here.
	root string
}

// machineRolesLocalDir is dns.rolesLocalDir joined onto dns.remoteDir. It is
// unexported in internal/dns, so it is written out here rather than reached
// across the package boundary.
const machineRolesLocalDir = "/opt/devmachine/roles.local/"

func (c localClient) Run(ctx context.Context, command string) (string, error) {
	real := strings.ReplaceAll(command, machineRolesLocalDir, c.root+"/")
	out, err := exec.CommandContext(ctx, "sh", "-c", real).CombinedOutput()
	return string(out), err
}

func (c localClient) Stream(ctx context.Context, command string, stdout, _ io.Writer) error {
	out, err := c.Run(ctx, command)
	if _, writeErr := io.WriteString(stdout, out); writeErr != nil {
		return writeErr
	}
	return err
}

func (c localClient) StreamInput(ctx context.Context, command string, stdin io.Reader, stdout, _ io.Writer) error {
	script, err := io.ReadAll(stdin)
	if err != nil {
		return err
	}
	sh := exec.CommandContext(ctx, "sh", "-c", command)
	sh.Stdin = strings.NewReader(strings.ReplaceAll(string(script), machineRolesLocalDir, c.root+"/"))
	out, err := sh.CombinedOutput()
	if _, writeErr := stdout.Write(out); writeErr != nil {
		return writeErr
	}
	return err
}

func (c localClient) RunInput(ctx context.Context, command string, _ io.Reader) (string, error) {
	return c.Run(ctx, command)
}

func (localClient) Upload(context.Context, string, io.Reader) error { return nil }

func (localClient) Close() error { return nil }

// dialLocal makes `dial` hand out localClient for the rest of the test, with
// the package's entrypoint resolved under configDir's local package store.
// dialLocal stubs both dial and dialMux, since callers include `run
// --package` (which dials through dialMux) as well as commands that still
// use dial directly.
func dialLocal(t *testing.T, configDir string) {
	t.Helper()
	stub := func(context.Context, config.Machine, string) (remote.Client, string, error) {
		return localClient{root: packages.LocalDir(configDir)}, "203.0.113.10", nil
	}
	origDial, origDialMux := dial, dialMux
	dial, dialMux = stub, stub
	t.Cleanup(func() { dial, dialMux = origDial, origDialMux })
}

// dialCall is what one dial invocation was asked to reach.
type dialCall struct {
	machine string
	user    string
}

// dialLocalCapturing is dialLocal, but it also records the machine and user
// each call was made with, so a test can prove who a package's entrypoint
// ran as.
func dialLocalCapturing(t *testing.T, configDir string) *[]dialCall {
	t.Helper()
	var calls []dialCall
	stub := func(_ context.Context, m config.Machine, user string) (remote.Client, string, error) {
		calls = append(calls, dialCall{machine: m.Name, user: user})
		return localClient{root: packages.LocalDir(configDir)}, "203.0.113.10", nil
	}
	origDial, origDialMux := dial, dialMux
	dial, dialMux = stub, stub
	t.Cleanup(func() { dial, dialMux = origDial, origDialMux })
	return &calls
}

// lockOnto writes a lock that says these packages are installed on machine,
// the same state `devmachine sync` leaves behind.
func lockOnto(t *testing.T, configDir, machine string, names ...string) {
	t.Helper()
	entries := make([]packages.LockEntry, len(names))
	for i, n := range names {
		entries[i] = packages.LockEntry{Name: n, Source: packages.SourceLocal}
	}
	lock := packages.Lock{
		AppliedAt: time.Now().UTC().Format(time.RFC3339),
		Machines:  map[string][]packages.LockEntry{machine: entries},
	}
	if err := packages.SaveLock(configDir, lock); err != nil {
		t.Fatal(err)
	}
}

// lockOntoWorkspace writes a lock that says these packages are installed for
// a workspace specifically, rather than for the whole machine.
func lockOntoWorkspace(t *testing.T, configDir, workspace string, names ...string) {
	t.Helper()
	entries := make([]packages.LockEntry, len(names))
	for i, n := range names {
		entries[i] = packages.LockEntry{Name: n, Source: packages.SourceLocal}
	}
	lock := packages.Lock{
		AppliedAt:  time.Now().UTC().Format(time.RFC3339),
		Workspaces: map[string][]packages.LockEntry{workspace: entries},
	}
	if err := packages.SaveLock(configDir, lock); err != nil {
		t.Fatal(err)
	}
}

// writeDNSPackage writes a package of kind dns with a real bin/provider
// script, so a test can drive it through the real entrypoint contract.
func writeDNSPackage(t *testing.T, configDir, name string, commands []string, body string) {
	t.Helper()
	pkgDir := filepath.Join(packages.LocalDir(configDir), name)
	if err := os.MkdirAll(filepath.Join(pkgDir, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}

	if len(commands) == 0 {
		commands = []string{"*"}
	}
	quoted := make([]string, len(commands))
	for i, c := range commands {
		quoted[i] = fmt.Sprintf("%q", c)
	}

	manifest := fmt.Sprintf(
		"format: 1\nname: %s\nscope: machine\nsummary: A package written by a test.\n"+
			"kind: dns\nentrypoint: bin/provider\ncommands: [%s]\n"+
			"credentials:\n  - name: %s\n    kind: secret\n    env: %s_TOKEN\n",
		name, strings.Join(quoted, ", "), name, strings.ToUpper(name))
	if err := os.WriteFile(packages.ManifestPath(pkgDir), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	script := "#!/usr/bin/env python3\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(pkgDir, "bin", "provider"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

// configDirWithProvider is a one-machine configuration with one dns package
// installed, its bin/provider running exactly this Python body, accepting
// any command.
func configDirWithProvider(t *testing.T, name, body string) string {
	t.Helper()
	dir := configDir(t)
	writeDNSPackage(t, dir, name, nil, body)
	lockOnto(t, dir, "main", name)
	return dir
}

// configDirWithProviderAccepting is like configDirWithProvider, but the
// package only declares these commands rather than "*".
func configDirWithProviderAccepting(t *testing.T, name string, commands []string) string {
	t.Helper()
	dir := configDir(t)
	writeDNSPackage(t, dir, name, commands, "print('ok')")
	lockOnto(t, dir, "main", name)
	return dir
}

// configDirWithPlainPackage is a one-machine configuration with a package
// that declares no entrypoint: an ordinary machine package, not callable.
func configDirWithPlainPackage(t *testing.T, name string) string {
	t.Helper()
	dir := configDir(t)
	pkgDir := filepath.Join(packages.LocalDir(dir), name)
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := fmt.Sprintf("format: 1\nname: %s\nscope: machine\nsummary: A package written by a test.\n", name)
	if err := os.WriteFile(packages.ManifestPath(pkgDir), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	lockOnto(t, dir, "main", name)
	return dir
}
