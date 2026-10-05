package commands

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mydevmachine/devmachine/internal/aliases"
	"github.com/mydevmachine/devmachine/internal/config"
	"github.com/mydevmachine/devmachine/internal/hostkeys"
	"github.com/mydevmachine/devmachine/internal/keys"
	"github.com/mydevmachine/devmachine/internal/remote"
	"golang.org/x/crypto/ssh"
)

// nopClient stands in for a machine. Every step of the bootstrap is stubbed,
// so no test reads what a command returned, except which system it runs: a
// Debian.
type nopClient struct{}

func (nopClient) Run(_ context.Context, command string) (string, error) {
	switch command {
	case remote.UnameCommand:
		return "Linux\n", nil
	case remote.OSReleaseCommand:
		return "ID=debian\n", nil
	}
	return "", nil
}
func (nopClient) RunInput(context.Context, string, io.Reader) (string, error) { return "", nil }
func (nopClient) Stream(context.Context, string, io.Writer, io.Writer) error  { return nil }
func (nopClient) Upload(context.Context, string, io.Reader) error             { return nil }
func (nopClient) Close() error                                                { return nil }

// swap replaces a seam and returns the function that puts it back.
func swap[T any](p *T, v T) func() {
	old := *p
	*p = v
	return func() { *p = old }
}

// bootstrapStubs says what the machine on the other end does.
type bootstrapStubs struct {
	keyWorks   bool
	proofFails bool
	tailscale  bool
	noRoot     bool
	agent      []keys.Offered
	// kernel and id are what the machine says it runs. Empty is Debian.
	kernel string
	id     string
}

// bootstrapSteps records the decisions the flow took, so a test reads those
// rather than guessing from the output.
type bootstrapSteps struct {
	events           []string
	askedForPassword bool
	password         string
	installedKey     bool
	publicKey        string
	proved           bool
	provedWith       remote.Auth
	hardened         bool
	ansible          bool
	checkedRoot      bool
	detected         bool
	hostKey          ssh.PublicKey
}

func stubBootstrap(t *testing.T, s bootstrapStubs) *bootstrapSteps {
	t.Helper()
	steps := &bootstrapSteps{}
	steps.hostKey = commandHostKey(t)
	trustRecorded := false

	t.Cleanup(swap(&scanHostKey, func(_ context.Context, m config.Machine) (ssh.PublicKey, string, error) {
		steps.events = append(steps.events, "scan host key")
		return steps.hostKey, m.Hosts[0].Address, nil
	}))
	t.Cleanup(swap(&confirmHostKey, func(_ io.Reader, _ io.Writer, _ string) (bool, error) {
		steps.events = append(steps.events, "ask trust")
		return true, nil
	}))

	t.Cleanup(swap(&dialWith, func(_ context.Context, m config.Machine, _ string, a remote.Auth) (remote.Client, string, error) {
		address := m.Hosts[0].Address
		if !trustRecorded {
			store, err := hostkeys.Open(m.KnownHostsFile)
			if err != nil {
				t.Errorf("opening trust before authentication: %v", err)
			} else if err := store.Check(m.Name, m.Port, steps.hostKey); err != nil {
				t.Errorf("host key was not written before authentication: %v", err)
			}
			steps.events = append(steps.events, "write trust")
			trustRecorded = true
		}
		if a.Password != "" {
			steps.events = append(steps.events, "optional password authentication")
			steps.askedForPassword = true
			steps.password = a.Password
			return nopClient{}, address, nil
		}
		steps.events = append(steps.events, "attempt key authentication")
		if !s.keyWorks {
			return nil, "", fmt.Errorf("%w: the machine refused the key", remote.ErrAuthRefused)
		}
		return nopClient{}, address, nil
	}))
	t.Cleanup(swap(&installKey, func(_ context.Context, _ remote.Client, publicKey string) error {
		steps.installedKey = true
		steps.publicKey = publicKey
		return nil
	}))
	t.Cleanup(swap(&proveAuth, func(_ context.Context, _ config.Machine, _ string, a remote.Auth) (remote.Client, error) {
		if s.proofFails {
			return nil, errors.New("the key is installed but does not log in: check authorized_keys and AuthorizedKeysFile")
		}
		steps.proved = true
		steps.events = append(steps.events, "prove key")
		steps.provedWith = a
		return nopClient{}, nil
	}))
	t.Cleanup(swap(&installAnsible, func(context.Context, remote.Client, io.Writer) error {
		steps.ansible = true
		steps.events = append(steps.events, "install ansible")
		return nil
	}))
	t.Cleanup(swap(&harden, func(context.Context, remote.Client) error {
		steps.hardened = true
		steps.events = append(steps.events, "harden")
		return nil
	}))
	t.Cleanup(swap(&agentKeys, func() ([]keys.Offered, error) { return s.agent, nil }))
	t.Cleanup(swap(&tailscaleSSH, func(remote.Client) bool { return s.tailscale }))
	t.Cleanup(swap(&detectSystem, func(context.Context, remote.Client) (remote.System, error) {
		steps.detected = true
		steps.events = append(steps.events, "detect system")
		system := remote.System{Kernel: "Linux", ID: "debian"}
		if s.kernel != "" {
			system = remote.System{Kernel: s.kernel, ID: s.id}
		}
		if system.Kernel == "Linux" && !remote.SupportedLinux(system.ID) || system.Kernel != "Linux" {
			return system, &remote.UnsupportedSystemError{System: system}
		}
		return system, nil
	}))
	t.Cleanup(swap(&checkRoot, func(_ context.Context, _ remote.Client, user string) error {
		steps.checkedRoot = true
		steps.events = append(steps.events, "check root")
		if s.noRoot {
			return fmt.Errorf("%w: %q", remote.ErrNoRoot, user)
		}
		return nil
	}))

	return steps
}

func commandHostKey(t *testing.T) ssh.PublicKey {
	t.Helper()
	public, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ssh.NewPublicKey(public)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

// answers replies to setup's questions, in order: machine name, address,
// administrative login, port, domain, then how to log in.
func answers(lines ...string) io.Reader {
	return strings.NewReader(strings.Join(lines, "\n") + "\n")
}

// runSetupIn runs the whole flow against a configuration directory.
func runSetupIn(t *testing.T, dir string, in io.Reader, o setupOptions) (string, error) {
	t.Helper()
	out := &strings.Builder{}
	err := runSetup(context.Background(), dir, in, out, o)
	return out.String(), err
}

func TestSetupWithExistingConfigurationOnlyPreparesTheSelectedMachine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, config.FileName)
	original := []byte("machines:\n  - name: main\n    hosts: [203.0.113.10]\n  - name: sandbox\n    hosts: [198.51.100.7]\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}

	var dialed string
	t.Cleanup(swap(&dial, func(_ context.Context, m config.Machine, user string) (remote.Client, string, error) {
		dialed = m.Name + ":" + user
		return nopClient{}, m.Hosts[0].Address, nil
	}))
	ansible := false
	t.Cleanup(swap(&installAnsible, func(context.Context, remote.Client, io.Writer) error {
		ansible = true
		return nil
	}))
	t.Cleanup(swap(&scanHostKey, func(context.Context, config.Machine) (ssh.PublicKey, string, error) {
		t.Fatal("setup scanned a new host key for an existing configuration")
		return nil, "", nil
	}))
	t.Cleanup(swap(&installKey, func(context.Context, remote.Client, string) error {
		t.Fatal("setup installed a key for an existing configuration")
		return nil
	}))
	t.Cleanup(swap(&harden, func(context.Context, remote.Client) error {
		t.Fatal("setup hardened an existing machine")
		return nil
	}))

	out, err := execute(t, "--config", dir, "--machine", "sandbox", "setup")
	if err != nil {
		t.Fatalf("runSetup returned %v", err)
	}
	if dialed != "sandbox:root" || !ansible {
		t.Fatalf("dialed %q, installed Ansible %v", dialed, ansible)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(after, original) {
		t.Fatalf("setup rewrote config.yml:\n%s", after)
	}
	if !strings.Contains(out, "without rewriting") || !strings.Contains(out, "Ansible") {
		t.Fatalf("setup did not explain the resume path: %q", out)
	}
}

func TestSetupWithoutAPackageReleasePrintsTheSkillsFollowUp(t *testing.T) {
	stubBootstrap(t, bootstrapStubs{keyWorks: true})

	out, err := runSetupIn(t, t.TempDir(),
		answers("main", "203.0.113.10", "root", "22", "", "1"), setupOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "devmachine skills add") || !strings.Contains(out, "package release") {
		t.Fatalf("missing skills follow-up: %q", out)
	}
}

func TestSetupWithAPackageReleaseOffersLocalSkills(t *testing.T) {
	dir := configWith(t, "machines:\n  - name: main\n    hosts: [203.0.113.10]\npackages: v9\n")
	t.Cleanup(swap(&dial, func(context.Context, config.Machine, string) (remote.Client, string, error) {
		return nopClient{}, "203.0.113.10", nil
	}))
	t.Cleanup(swap(&installAnsible, func(context.Context, remote.Client, io.Writer) error { return nil }))
	called := false
	t.Cleanup(swap(&setupSkills, func(_ context.Context, gotDir string, _ io.Reader, _ io.Writer) error {
		called = gotDir == dir
		return nil
	}))

	if _, err := runSetupIn(t, dir, strings.NewReader(""), setupOptions{}); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("setup did not offer local skills after preparing a pinned configuration")
	}
}

func TestSetupWithAWorkingKeySkipsThePassword(t *testing.T) {
	steps := stubBootstrap(t, bootstrapStubs{keyWorks: true})

	out, err := runSetupIn(t, t.TempDir(),
		answers("main", "203.0.113.10", "root", "22", "example.com", "1"), setupOptions{})
	if err != nil {
		t.Fatalf("runSetup returned %v", err)
	}

	if steps.askedForPassword {
		t.Fatal("it asked for a password it did not need")
	}
	if !steps.hardened {
		t.Fatal("it did not harden")
	}
	if !strings.Contains(out, "already") {
		t.Fatalf("it does not say the key already worked: %q", out)
	}
}

func TestSetupFallsBackToThePassword(t *testing.T) {
	steps := stubBootstrap(t, bootstrapStubs{keyWorks: false})

	if _, err := runSetupIn(t, t.TempDir(),
		answers("main", "203.0.113.10", "root", "22", "example.com", "1", "devmachine"),
		setupOptions{}); err != nil {
		t.Fatalf("runSetup returned %v", err)
	}

	if !steps.askedForPassword || !steps.installedKey || !steps.proved || !steps.hardened {
		t.Fatalf("got %#v", steps)
	}
	if steps.password != "devmachine" {
		t.Fatalf("password = %q", steps.password)
	}
}

func TestSetupAsksToWriteSSHAliasesAndWritesThemOnYes(t *testing.T) {
	stubBootstrap(t, bootstrapStubs{keyWorks: true})
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()

	out, err := runSetupIn(t, dir,
		answers("main", "203.0.113.10", "root", "22", "", "1", "y", "n"), setupOptions{})
	if err != nil {
		t.Fatalf("runSetup returned %v", err)
	}
	if !strings.Contains(out, "wrote the SSH aliases") {
		t.Fatalf("output does not say the aliases were written: %q", out)
	}

	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.SSHAliases {
		t.Fatal("ssh_aliases was not recorded")
	}
	path, err := aliases.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the ssh config was not written: %v", err)
	}
}

func TestSetupNoAliasesFlagSkipsTheQuestionAndWritesNothing(t *testing.T) {
	stubBootstrap(t, bootstrapStubs{keyWorks: true})
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := t.TempDir()

	if _, err := runSetupIn(t, dir,
		answers("main", "203.0.113.10", "root", "22", "", "1"),
		setupOptions{noAliases: true}); err != nil {
		t.Fatalf("runSetup returned %v", err)
	}

	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SSHAliases {
		t.Fatal("--no-aliases still recorded ssh_aliases")
	}
	if _, err := os.Stat(filepath.Join(home, ".ssh", "config")); err == nil {
		t.Fatal("--no-aliases still wrote ~/.ssh/config")
	}
}

func TestSetupYesFlagAnswersTheAliasesQuestionWithoutAsking(t *testing.T) {
	stubBootstrap(t, bootstrapStubs{keyWorks: true})
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()

	if _, err := runSetupIn(t, dir,
		answers("main", "203.0.113.10", "root", "22", "", "1"),
		setupOptions{yes: true}); err != nil {
		t.Fatalf("runSetup returned %v", err)
	}

	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.SSHAliases {
		t.Fatal("--yes did not record ssh_aliases: true")
	}
}

func TestSetupOffersTailscaleAndAddsThePackageOnYes(t *testing.T) {
	stubBootstrap(t, bootstrapStubs{keyWorks: true})
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()

	if _, err := runSetupIn(t, dir,
		answers("main", "203.0.113.10", "root", "22", "", "1", "n", "y"),
		setupOptions{}); err != nil {
		t.Fatalf("runSetup returned %v", err)
	}

	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	m, err := cfg.Machine("main")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(m.Packages, "tailscale") {
		t.Fatalf("packages = %#v, want tailscale added", m.Packages)
	}
}

func TestSetupDecliningTailscaleAddsNoPackage(t *testing.T) {
	stubBootstrap(t, bootstrapStubs{keyWorks: true})
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()

	if _, err := runSetupIn(t, dir,
		answers("main", "203.0.113.10", "root", "22", "", "1", "n", "n"),
		setupOptions{}); err != nil {
		t.Fatalf("runSetup returned %v", err)
	}

	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	m, err := cfg.Machine("main")
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(m.Packages, "tailscale") {
		t.Fatal("declining tailscale still added the package")
	}
}

func TestSetupTrustsTheHostBeforeAnyAuthentication(t *testing.T) {
	steps := stubBootstrap(t, bootstrapStubs{keyWorks: false})

	if _, err := runSetupIn(t, t.TempDir(),
		answers("main", "203.0.113.10", "root", "22", "", "1", "devmachine"),
		setupOptions{}); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"scan host key",
		"ask trust",
		"write trust",
		"attempt key authentication",
		"optional password authentication",
		"detect system",
		"prove key",
		"check root",
		"harden",
		"install ansible",
	}
	if !slices.Equal(steps.events, want) {
		t.Fatalf("events = %#v, want %#v", steps.events, want)
	}
}

func TestSetupHostTrustRefusalLeavesNoFilesOrAuthentication(t *testing.T) {
	dir := t.TempDir()
	steps := stubBootstrap(t, bootstrapStubs{keyWorks: false})
	t.Cleanup(swap(&confirmHostKey, func(_ io.Reader, _ io.Writer, _ string) (bool, error) {
		steps.events = append(steps.events, "ask trust")
		return false, nil
	}))

	_, err := runSetupIn(t, dir,
		answers("main", "203.0.113.10", "root", "22", ""), setupOptions{})
	if !errors.Is(err, errDeclined) {
		t.Fatalf("runSetup error = %v, want declined", err)
	}
	for _, path := range []string{
		filepath.Join(dir, config.FileName),
		filepath.Join(dir, config.KnownHostsFileName),
		keys.Dir(dir),
	} {
		if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("refusal left %s", path)
		}
	}
	if !slices.Equal(steps.events, []string{"scan host key", "ask trust"}) {
		t.Fatalf("refusal events = %#v", steps.events)
	}
	if steps.installedKey || steps.proved || steps.hardened || steps.ansible {
		t.Fatalf("refusal bootstrapped: %#v", steps)
	}
}

// TestSetupProvesTheKeyBeforeHardening: the proof is the only thing that says
// the door still opens once the password is gone.
func TestSetupProvesWithTheKeyAloneAndInstallsWhatItProves(t *testing.T) {
	dir := t.TempDir()
	steps := stubBootstrap(t, bootstrapStubs{keyWorks: false})

	if _, err := runSetupIn(t, dir,
		answers("main", "203.0.113.10", "root", "22", "", "1", "devmachine"),
		setupOptions{}); err != nil {
		t.Fatalf("runSetup returned %v", err)
	}

	generated := filepath.Join(keys.Dir(dir), "main")
	if steps.provedWith.KeyPath != generated || steps.provedWith.Password != "" {
		t.Fatalf("the proof did not use the key alone: %#v", steps.provedWith)
	}
	public, err := os.ReadFile(generated + ".pub")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(public)) != steps.publicKey {
		t.Fatalf("it installed %q, not the key it generated", steps.publicKey)
	}

	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Machines[0].Key != generated {
		t.Fatalf("the configuration does not point at the key it made: %q", cfg.Machines[0].Key)
	}
}

func TestSetupDoesNotHardenWhenTheProofFails(t *testing.T) {
	steps := stubBootstrap(t, bootstrapStubs{keyWorks: false, proofFails: true})

	out, err := runSetupIn(t, t.TempDir(),
		answers("main", "203.0.113.10", "root", "22", "", "1", "devmachine"), setupOptions{})
	if err == nil {
		t.Fatal("a failed proof was reported as success")
	}

	// Turning passwords off after a failed proof locks the door with the key
	// still inside.
	if steps.hardened {
		t.Fatal("it hardened without proving the key")
	}
	if !strings.Contains(out, "password login is still on") {
		t.Fatalf("it does not say the machine is still reachable: %q", out)
	}
}

func TestSetupNeverWritesThePasswordAnywhere(t *testing.T) {
	dir := t.TempDir()
	stubBootstrap(t, bootstrapStubs{keyWorks: false})

	if _, err := runSetupIn(t, dir,
		answers("main", "203.0.113.10", "root", "22", "example.com", "1", "hunter2"),
		setupOptions{}); err != nil {
		t.Fatalf("runSetup returned %v", err)
	}

	// Not in config.yml, not in the lock, not in the history log. The password
	// lives in memory, is used once, and is never the CLI's to keep.
	found := false
	filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		if strings.Contains(string(body), "hunter2") {
			t.Errorf("the password reached %s", path)
			found = true
		}
		return nil
	})
	if found {
		t.FailNow()
	}
}

func TestSetupOffersTheKeysTheAgentHolds(t *testing.T) {
	// Somebody whose key lives in a password manager has no file to point at,
	// and the CLI must not require one.
	dir := t.TempDir()
	steps := stubBootstrap(t, bootstrapStubs{keyWorks: false, agent: []keys.Offered{
		{Fingerprint: "SHA256:aaaa", Comment: "alice laptop", PublicKey: "ssh-ed25519 AAAAagent alice laptop"},
	}})

	out, err := runSetupIn(t, dir,
		answers("main", "203.0.113.10", "root", "22", "", "3", "devmachine"), setupOptions{})
	if err != nil {
		t.Fatalf("runSetup returned %v", err)
	}

	if steps.publicKey != "ssh-ed25519 AAAAagent alice laptop" {
		t.Fatalf("it installed %q, not the key the agent holds", steps.publicKey)
	}

	for _, want := range []string{"SHA256:aaaa", "alice laptop"} {
		if !strings.Contains(out, want) {
			t.Fatalf("the agent's key was not offered: %q", out)
		}
	}
	if !steps.provedWith.Agent || steps.provedWith.KeyPath != "" {
		t.Fatalf("it did not log in through the agent: %#v", steps.provedWith)
	}
	if steps.provedWith.AgentPublicKey != "ssh-ed25519 AAAAagent alice laptop" {
		t.Fatalf("it did not narrow the agent to the chosen key: %#v", steps.provedWith)
	}

	// An agent key has no path, so the configuration says nothing about a key
	// file: an empty `key` is what makes the CLI ask the agent later. The
	// chosen key is recorded instead, so only it gets offered from now on.
	cfg, _ := config.Load(dir)
	if cfg.Machines[0].Key != "" {
		t.Fatalf("key = %q, want empty", cfg.Machines[0].Key)
	}
	if cfg.Machines[0].AgentKey != "ssh-ed25519 AAAAagent alice laptop" {
		t.Fatalf("agent_key = %q, want the chosen key", cfg.Machines[0].AgentKey)
	}
	if body, err := os.ReadFile(cfg.Machines[0].AgentKeyFile); err != nil {
		t.Fatalf("the agent key's public file was not written: %v", err)
	} else if strings.TrimSpace(string(body)) != "ssh-ed25519 AAAAagent alice laptop" {
		t.Fatalf("got %q", body)
	}
}

func TestSetupWithNoAgentStillOffersTheOtherTwoWays(t *testing.T) {
	// No agent is one of the two ordinary cases, not a failure to report.
	stubBootstrap(t, bootstrapStubs{keyWorks: true})

	out, err := runSetupIn(t, t.TempDir(),
		answers("main", "203.0.113.10", "root", "22", "", "1"), setupOptions{})
	if err != nil {
		t.Fatalf("runSetup returned %v", err)
	}
	if strings.Contains(strings.ToLower(out), "no ssh agent") {
		t.Fatalf("an absent agent was reported as a problem: %q", out)
	}
}

func TestSetupUsesAKeyFileAlreadyOnDisk(t *testing.T) {
	dir := t.TempDir()
	elsewhere := t.TempDir()
	path, public, err := keys.Generate(elsewhere, "id_ed25519")
	if err != nil {
		t.Fatal(err)
	}
	steps := stubBootstrap(t, bootstrapStubs{keyWorks: false})

	if _, err := runSetupIn(t, dir,
		answers("main", "203.0.113.10", "root", "22", "", "2", path, "devmachine"),
		setupOptions{}); err != nil {
		t.Fatalf("runSetup returned %v", err)
	}

	if steps.publicKey != public {
		t.Fatalf("it installed %q, not the key it was pointed at", steps.publicKey)
	}
	cfg, _ := config.Load(dir)
	if cfg.Machines[0].Key != path {
		t.Fatalf("key = %q", cfg.Machines[0].Key)
	}
}

func TestSetupReusesTheKeyItAlreadyMadeForThatMachine(t *testing.T) {
	// Running setup again on a machine it already owns is the common case, and
	// generating over the old key would lock it out of that machine forever.
	dir := t.TempDir()
	path, public, err := keys.Generate(keys.Dir(dir), "main")
	if err != nil {
		t.Fatal(err)
	}
	steps := stubBootstrap(t, bootstrapStubs{keyWorks: false})

	if _, err := runSetupIn(t, dir,
		answers("main", "203.0.113.10", "root", "22", "", "1", "devmachine"),
		setupOptions{}); err != nil {
		t.Fatalf("runSetup returned %v", err)
	}

	if steps.publicKey != public {
		t.Fatalf("it did not reuse the key at %s: %q", path, steps.publicKey)
	}
}

func TestSetupNoHardenSkipsIt(t *testing.T) {
	steps := stubBootstrap(t, bootstrapStubs{keyWorks: false})

	out, err := runSetupIn(t, t.TempDir(),
		answers("main", "203.0.113.10", "root", "22", "", "1", "devmachine"),
		setupOptions{noHarden: true})
	if err != nil {
		t.Fatalf("runSetup returned %v", err)
	}

	if steps.hardened {
		t.Fatal("--no-harden hardened anyway")
	}
	if !steps.proved {
		t.Fatal("--no-harden also skipped the proof")
	}
	if !strings.Contains(out, "password login") {
		t.Fatalf("it does not say password login was left alone: %q", out)
	}
}

func TestSetupStopsWhenNothingAnswers(t *testing.T) {
	// A machine nobody can reach is not a machine whose password is worth
	// asking for.
	steps := &bootstrapSteps{}
	t.Cleanup(swap(&dialWith, func(context.Context, config.Machine, string, remote.Auth) (remote.Client, string, error) {
		return nil, "", errors.New("no address answered")
	}))
	t.Cleanup(swap(&agentKeys, func() ([]keys.Offered, error) { return nil, nil }))

	_, err := runSetupIn(t, t.TempDir(),
		answers("main", "203.0.113.10", "root", "22", "", "1"), setupOptions{})
	if err == nil {
		t.Fatal("an unreachable machine was reported as set up")
	}
	if steps.askedForPassword {
		t.Fatal("it asked for a password for a machine that never answered")
	}
}

func TestSetupWritesAConfigurationThatLoads(t *testing.T) {
	dir := t.TempDir()
	stubBootstrap(t, bootstrapStubs{keyWorks: true})

	if _, err := runSetupIn(t, dir,
		answers("sandbox", "198.51.100.7", "root", "2222", "example.com", "1"),
		setupOptions{}); err != nil {
		t.Fatalf("runSetup returned %v", err)
	}

	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatalf("the configuration it wrote does not load: %v", err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("the configuration it wrote is invalid: %v", err)
	}

	if len(cfg.Machines) != 1 {
		t.Fatalf("machines = %#v", cfg.Machines)
	}
	m := cfg.Machines[0]
	if m.Name != "sandbox" || m.Hosts[0].Address != "198.51.100.7" || m.Port != 2222 {
		t.Fatalf("machine = %#v", m)
	}
	if cfg.Domain != "example.com" {
		t.Fatalf("domain = %q", cfg.Domain)
	}
}

func TestSetupWritesAgentsMdForANewConfiguration(t *testing.T) {
	dir := t.TempDir()
	stubBootstrap(t, bootstrapStubs{keyWorks: true})

	if _, err := runSetupIn(t, dir,
		answers("sandbox", "198.51.100.7", "root", "2222", "", "1"),
		setupOptions{}); err != nil {
		t.Fatalf("runSetup returned %v", err)
	}

	body, err := os.ReadFile(filepath.Join(dir, agentsFileName))
	if err != nil {
		t.Fatalf("AGENTS.md was not written: %v", err)
	}
	if string(body) != agentsTemplate {
		t.Fatalf("AGENTS.md = %q, want the template", body)
	}
}

func TestSetupLeavesAnExistingAgentsMdUntouched(t *testing.T) {
	dir := t.TempDir()
	stubBootstrap(t, bootstrapStubs{keyWorks: true})

	custom := "# AGENTS.md\n\nmy own rules\n"
	if err := os.WriteFile(filepath.Join(dir, agentsFileName), []byte(custom), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := runSetupIn(t, dir,
		answers("sandbox", "198.51.100.7", "root", "2222", "", "1"),
		setupOptions{}); err != nil {
		t.Fatalf("runSetup returned %v", err)
	}

	body, err := os.ReadFile(filepath.Join(dir, agentsFileName))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != custom {
		t.Fatalf("AGENTS.md was overwritten: %q", body)
	}
}

func TestSetupResumingWritesNoAgentsMd(t *testing.T) {
	dir := t.TempDir()
	original := []byte("machines:\n  - name: main\n    hosts: [203.0.113.10]\n")
	if err := os.WriteFile(filepath.Join(dir, config.FileName), original, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(swap(&dial, func(_ context.Context, m config.Machine, user string) (remote.Client, string, error) {
		return nopClient{}, m.Hosts[0].Address, nil
	}))
	t.Cleanup(swap(&installAnsible, func(context.Context, remote.Client, io.Writer) error { return nil }))

	if _, err := runSetupIn(t, dir, answers(), setupOptions{machine: "main"}); err != nil {
		t.Fatalf("runSetup returned %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, agentsFileName)); err == nil {
		t.Fatal("resuming setup wrote AGENTS.md")
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func TestSetupTakesTheDefaultsOnEmptyAnswers(t *testing.T) {
	dir := t.TempDir()
	stubBootstrap(t, bootstrapStubs{keyWorks: true})

	// Only the address is typed; everything else is left blank.
	if _, err := runSetupIn(t, dir,
		answers("", "203.0.113.10", "", "", "", ""), setupOptions{}); err != nil {
		t.Fatalf("runSetup returned %v", err)
	}

	cfg, _ := config.Load(dir)
	m := cfg.Machines[0]
	if m.Name != "main" || m.User != config.DefaultAdminUser || m.Port != config.DefaultPort {
		t.Fatalf("the defaults were not applied: %#v", m)
	}
}

func TestSetupRefusesAnAddressThatWasNotGiven(t *testing.T) {
	dir := t.TempDir()

	if _, err := runSetupIn(t, dir, answers("main", "", "root", "22", ""), setupOptions{}); err == nil {
		t.Fatal("expected an error when no address was given")
	}
	if _, err := os.Stat(filepath.Join(dir, config.FileName)); err == nil {
		t.Fatal("it wrote a configuration it had already refused")
	}
}

func TestSetupRefusesAPortThatIsNotANumber(t *testing.T) {
	if _, err := runSetupIn(t, t.TempDir(),
		answers("main", "203.0.113.10", "root", "not-a-port", ""), setupOptions{}); err == nil {
		t.Fatal("expected an error for a port that is not a number")
	}
}

func TestSetupExistingDoesNotOverwriteWhenPreparationFails(t *testing.T) {
	dir := t.TempDir()
	existing := "machines:\n  - name: keep-me\n    hosts: [203.0.113.99]\n"
	if err := os.WriteFile(filepath.Join(dir, config.FileName), []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(swap(&dial, func(context.Context, config.Machine, string) (remote.Client, string, error) {
		return nil, "", errors.New("unreachable")
	}))

	_, err := runSetupIn(t, dir, answers("main", "203.0.113.10", "root", "22", ""), setupOptions{})
	if err == nil || !strings.Contains(err.Error(), "unreachable") {
		t.Fatalf("error = %v, want connection failure", err)
	}

	after, readErr := os.ReadFile(filepath.Join(dir, config.FileName))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(after) != existing {
		t.Fatal("the existing configuration was overwritten")
	}
}

func TestSetupOverwritesWithForce(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, config.FileName),
		[]byte("machines:\n  - name: old\n    hosts: [203.0.113.99]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	stubBootstrap(t, bootstrapStubs{keyWorks: true})

	if _, err := runSetupIn(t, dir,
		answers("new", "203.0.113.10", "root", "22", "", "1"), setupOptions{force: true}); err != nil {
		t.Fatalf("runSetup returned %v", err)
	}

	cfg, _ := config.Load(dir)
	if cfg.Machines[0].Name != "new" {
		t.Fatalf("machine = %q", cfg.Machines[0].Name)
	}
}

func TestSetupSaysWhatItChangedOnTheMachine(t *testing.T) {
	stubBootstrap(t, bootstrapStubs{keyWorks: true})

	out, err := runSetupIn(t, t.TempDir(),
		answers("main", "203.0.113.10", "", "", "", "1"), setupOptions{})
	if err != nil {
		t.Fatalf("runSetup returned %v", err)
	}

	// setup now touches the server, and saying which parts it changed is the
	// difference between a command people trust and one they run scared.
	for _, want := range []string{"password login", "Next"} {
		if !strings.Contains(out, want) {
			t.Fatalf("the output does not say what it did: %q", out)
		}
	}
}

func TestTheConfigurationFileIsNotReadableByOthers(t *testing.T) {
	dir := t.TempDir()
	stubBootstrap(t, bootstrapStubs{keyWorks: true})

	if _, err := runSetupIn(t, dir,
		answers("main", "203.0.113.10", "", "", "", "1"), setupOptions{}); err != nil {
		t.Fatalf("runSetup returned %v", err)
	}

	info, err := os.Stat(filepath.Join(dir, config.FileName))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("mode = %o, want 600", perm)
	}
}

func TestSetupInstallsAnsible(t *testing.T) {
	// The imperative shell surface ends here. Everything after it is a play,
	// so a machine without Ansible is a machine `sync` cannot reach.
	steps := stubBootstrap(t, bootstrapStubs{keyWorks: false})

	if _, err := runSetupIn(t, t.TempDir(),
		answers("main", "203.0.113.10", "root", "22", "", "1", "devmachine"),
		setupOptions{}); err != nil {
		t.Fatalf("runSetup returned %v", err)
	}
	if !steps.ansible {
		t.Fatal("it left the machine without Ansible")
	}
}

func TestSetupInstallsAnsibleEvenWithNoHarden(t *testing.T) {
	// --no-harden is about password login, not about leaving the job half
	// done.
	steps := stubBootstrap(t, bootstrapStubs{keyWorks: true})

	if _, err := runSetupIn(t, t.TempDir(),
		answers("main", "203.0.113.10", "root", "22", "", "1"),
		setupOptions{noHarden: true}); err != nil {
		t.Fatalf("runSetup returned %v", err)
	}
	if !steps.ansible {
		t.Fatal("--no-harden also skipped Ansible")
	}
}

func TestSetupInstallsNothingWhenTheProofFails(t *testing.T) {
	steps := stubBootstrap(t, bootstrapStubs{keyWorks: false, proofFails: true})

	if _, err := runSetupIn(t, t.TempDir(),
		answers("main", "203.0.113.10", "root", "22", "", "1", "devmachine"),
		setupOptions{}); err == nil {
		t.Fatal("a failed proof was reported as success")
	}
	if steps.ansible {
		t.Fatal("it carried on installing on a machine it could not prove it owns")
	}
}

func TestSetupSeedsTheDefaultPackagesForAWorkspace(t *testing.T) {
	dir := t.TempDir()
	stubBootstrap(t, bootstrapStubs{keyWorks: true})

	if _, err := runSetupIn(t, dir,
		answers("main", "203.0.113.10", "root", "22", "example.com", "1"),
		setupOptions{}); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	// The default lives in the person's own file, so changing one line
	// changes every workspace made afterwards.
	if !slices.Equal(cfg.Defaults.Workspace, config.DefaultWorkspacePackages) {
		t.Fatalf("got %#v", cfg.Defaults.Workspace)
	}
}

func TestSetupPinsTheLatestPackagesRelease(t *testing.T) {
	defer stubLatestPackagesRelease(t, "v8")()
	stubBootstrap(t, bootstrapStubs{keyWorks: true})
	dir := t.TempDir()

	if _, err := runSetupIn(t, dir,
		answers("main", "203.0.113.10", "root", "22", "example.com", "1"), setupOptions{}); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Packages != "v8" {
		t.Fatalf("a new configuration must pin the latest release, got %q", cfg.Packages)
	}
}

func TestSetupWithoutTheLatestReleaseSaysHowToPin(t *testing.T) {
	stubBootstrap(t, bootstrapStubs{keyWorks: true})

	out, err := runSetupIn(t, t.TempDir(),
		answers("main", "203.0.113.10", "root", "22", "example.com", "1"), setupOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "devmachine packages pin") {
		t.Fatalf("it does not say how to pin: %q", out)
	}
}

func stubReleaseHas(t *testing.T, has bool) {
	t.Helper()
	previous := releaseHasPackage
	releaseHasPackage = func(context.Context, string, string, string) bool { return has }
	t.Cleanup(func() { releaseHasPackage = previous })
}

func machinePackagesAfterSetup(t *testing.T, o setupOptions) ([]string, string) {
	t.Helper()
	dir := t.TempDir()
	out, err := runSetupIn(t, dir,
		answers("main", "203.0.113.10", "root", "22", "example.com", "1"), o)
	if err != nil {
		t.Fatalf("runSetup returned %v", err)
	}
	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return cfg.Machines[0].Packages, out
}

func TestSetupGivesTheMachineTheEssentials(t *testing.T) {
	defer stubLatestPackagesRelease(t, "v14")()
	stubReleaseHas(t, true)
	stubBootstrap(t, bootstrapStubs{keyWorks: true})

	got, out := machinePackagesAfterSetup(t, setupOptions{})
	if !slices.Equal(got, []string{"essentials"}) {
		t.Fatalf("machine packages are %#v, want [essentials]", got)
	}
	if !strings.Contains(out, "--no-essentials") {
		t.Fatalf("setup does not say how to go without them: %q", out)
	}
	if !strings.Contains(out, "what the macOS app reads") {
		t.Fatalf("setup does not say the essentials serve the macOS app: %q", out)
	}
}

func TestSetupWithNoEssentialsLeavesTheMachineBare(t *testing.T) {
	defer stubLatestPackagesRelease(t, "v14")()
	stubReleaseHas(t, true)
	stubBootstrap(t, bootstrapStubs{keyWorks: true})

	got, _ := machinePackagesAfterSetup(t, setupOptions{noEssentials: true})
	if len(got) != 0 {
		t.Fatalf("machine packages are %#v, want none", got)
	}
}

func TestSetupSkipsEssentialsAReleaseDoesNotHave(t *testing.T) {
	defer stubLatestPackagesRelease(t, "v13")()
	stubReleaseHas(t, false)
	stubBootstrap(t, bootstrapStubs{keyWorks: true})

	got, _ := machinePackagesAfterSetup(t, setupOptions{})
	if len(got) != 0 {
		t.Fatalf("machine packages are %#v, want none: v13 has no essentials", got)
	}
}

// TestSetupInstallsTheKeyWhenTailscaleSSHLetItIn: Tailscale SSH accepts any
// key, so a login over it is no proof the key was ever installed. Found on a
// real machine whose authorized_keys never got the key.
func TestSetupInstallsTheKeyWhenTailscaleSSHLetItIn(t *testing.T) {
	steps := stubBootstrap(t, bootstrapStubs{keyWorks: true, tailscale: true})

	out, err := runSetupIn(t, t.TempDir(),
		answers("main", "100.64.0.10", "alice", "22", "", "1"), setupOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !steps.installedKey {
		t.Fatal("the key was taken as proved by a Tailscale SSH login")
	}
	if steps.askedForPassword {
		t.Fatal("it asked for a password it does not need")
	}
	// Nothing proved the key past Tailscale SSH, so passwords stay on.
	if steps.hardened {
		t.Fatal("it turned password login off on an unproved key")
	}
	if !strings.Contains(out, "Tailscale SSH") || strings.Contains(out, "already logs in") {
		t.Fatalf("got %q", out)
	}
}

func TestSetupChecksRootBeforeHardening(t *testing.T) {
	steps := stubBootstrap(t, bootstrapStubs{keyWorks: true})

	if _, err := runSetupIn(t, t.TempDir(),
		answers("main", "203.0.113.10", "alice", "22", "", "1"), setupOptions{}); err != nil {
		t.Fatal(err)
	}
	check := slices.Index(steps.events, "check root")
	hardened := slices.Index(steps.events, "harden")
	if check < 0 || check > hardened {
		t.Fatalf("events: %q", steps.events)
	}
}

// TestSetupStopsBeforeChangingAnythingWhenTheAdminCannotBecomeRoot: a sudo
// that wants a password must not leave half a bootstrap behind.
func TestSetupStopsBeforeChangingAnythingWhenTheAdminCannotBecomeRoot(t *testing.T) {
	steps := stubBootstrap(t, bootstrapStubs{keyWorks: true, noRoot: true})

	_, err := runSetupIn(t, t.TempDir(),
		answers("main", "203.0.113.10", "alice", "22", "", "1"), setupOptions{})
	if !errors.Is(err, remote.ErrNoRoot) {
		t.Fatalf("got %v", err)
	}
	if steps.hardened || steps.ansible {
		t.Fatalf("it changed the machine anyway: %q", steps.events)
	}
}

// TestSetupAgainInstallsTheKeyOverTailscaleSSH repairs a machine an earlier
// run left without its key, because Tailscale SSH let that run in anyway.
func TestSetupAgainInstallsTheKeyOverTailscaleSSH(t *testing.T) {
	keyPath := filepath.Join(t.TempDir(), "main")
	if err := os.WriteFile(keyPath+".pub", []byte("ssh-ed25519 AAAAexample devmachine-main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := configWith(t, "machines:\n  - name: main\n    hosts: [100.64.0.10]\n    user: alice\n    key: "+keyPath+"\n")
	t.Cleanup(swap(&dial, func(context.Context, config.Machine, string) (remote.Client, string, error) {
		return nopClient{}, "100.64.0.10", nil
	}))
	t.Cleanup(swap(&tailscaleSSH, func(remote.Client) bool { return true }))
	t.Cleanup(swap(&checkRoot, func(context.Context, remote.Client, string) error { return nil }))
	t.Cleanup(swap(&installAnsible, func(context.Context, remote.Client, io.Writer) error { return nil }))
	var installed string
	t.Cleanup(swap(&installKey, func(_ context.Context, _ remote.Client, public string) error {
		installed = public
		return nil
	}))

	if _, err := runSetupIn(t, dir, strings.NewReader(""), setupOptions{}); err != nil {
		t.Fatal(err)
	}
	if installed != "ssh-ed25519 AAAAexample devmachine-main" {
		t.Fatalf("installed %q", installed)
	}
}

// TestSetupRefusesAnUnknownSystemBeforeTheKeyGoesIn: on a machine as it was
// bought, installing the key is the first change, so the refusal comes before.
func TestSetupRefusesAnUnknownSystemBeforeTheKeyGoesIn(t *testing.T) {
	steps := stubBootstrap(t, bootstrapStubs{keyWorks: false, kernel: "Linux", id: "fedora"})

	_, err := runSetupIn(t, t.TempDir(),
		answers("main", "203.0.113.10", "root", "22", "", "1", "secret"), setupOptions{})
	var unsupported *remote.UnsupportedSystemError
	if !errors.As(err, &unsupported) || !strings.Contains(err.Error(), `"fedora"`) {
		t.Fatalf("got %v", err)
	}
	if steps.installedKey || steps.hardened || steps.ansible {
		t.Fatalf("it changed the machine anyway: %q", steps.events)
	}
}

func TestSetupRefusesASystemThatIsNotLinuxWhenTheKeyAlreadyWorks(t *testing.T) {
	steps := stubBootstrap(t, bootstrapStubs{keyWorks: true, kernel: "Darwin"})

	_, err := runSetupIn(t, t.TempDir(),
		answers("main", "203.0.113.10", "root", "22", "", "1"), setupOptions{})
	if err == nil || !strings.Contains(err.Error(), `"Darwin" is not a system this CLI sets up`) {
		t.Fatalf("got %v", err)
	}
	if steps.checkedRoot || steps.hardened || steps.ansible {
		t.Fatalf("it went on past the refusal: %q", steps.events)
	}
}

func TestSetupRefusesAnUnknownSystemBeforeTheKeyGoesInOverTailscaleSSH(t *testing.T) {
	steps := stubBootstrap(t, bootstrapStubs{keyWorks: true, tailscale: true, kernel: "Linux", id: "alpine"})

	_, err := runSetupIn(t, t.TempDir(),
		answers("main", "100.64.0.10", "alice", "22", "", "1"), setupOptions{})
	if err == nil || !strings.Contains(err.Error(), `"alpine"`) {
		t.Fatalf("got %v", err)
	}
	if steps.installedKey {
		t.Fatalf("it installed the key on a machine it refuses: %q", steps.events)
	}
}

func TestSetupDetectsTheSystemFirst(t *testing.T) {
	steps := stubBootstrap(t, bootstrapStubs{keyWorks: false, kernel: "Linux", id: "arch"})

	out, err := runSetupIn(t, t.TempDir(),
		answers("main", "203.0.113.10", "root", "22", "", "1", "secret"), setupOptions{})
	if err != nil {
		t.Fatal(err)
	}
	detected := slices.Index(steps.events, "detect system")
	first := slices.IndexFunc(steps.events, func(e string) bool {
		return e == "prove key" || e == "check root" || e == "harden" || e == "install ansible"
	})
	if detected < 0 || detected > first || !steps.installedKey {
		t.Fatalf("events: %q", steps.events)
	}
	if !strings.Contains(out, "runs arch") {
		t.Fatalf("it does not say what it found: %q", out)
	}
}

func TestSetupAgainRefusesAnUnknownSystemBeforeInstallingAnsible(t *testing.T) {
	dir := configWith(t, "machines:\n  - name: main\n    hosts: [203.0.113.10]\n")
	steps := stubBootstrap(t, bootstrapStubs{kernel: "Linux", id: "fedora"})
	t.Cleanup(swap(&dial, func(context.Context, config.Machine, string) (remote.Client, string, error) {
		return nopClient{}, "203.0.113.10", nil
	}))

	_, err := runSetupIn(t, dir, strings.NewReader(""), setupOptions{})
	if err == nil || !strings.Contains(err.Error(), `"fedora"`) {
		t.Fatalf("got %v", err)
	}
	if steps.ansible || steps.checkedRoot {
		t.Fatalf("it went on past the refusal: %q", steps.events)
	}
}
