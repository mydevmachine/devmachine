package commands

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mydevmachine/devmachine/internal/config"
	"github.com/mydevmachine/devmachine/internal/hostkeys"
	"github.com/mydevmachine/devmachine/internal/keys"
	"github.com/mydevmachine/devmachine/internal/remote"
)

const oneMachine = "machines:\n  - name: main\n    hosts: [203.0.113.10]\n"

func readConfigFile(t *testing.T, dir string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(dir, config.FileName))
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

// A machine is written only once the key is proved, so a failed run leaves
// nothing behind that a second run would trip over.
func TestMachinesAddWithFlagsWritesNothingUntilTheKeyIsProved(t *testing.T) {
	dir := writeConfigDir(t, oneMachine)
	before := readConfigFile(t, dir)
	steps := stubBootstrap(t, bootstrapStubs{keyWorks: false})
	args := []string{"--config", dir, "machines", "add", "--name", "sandbox", "--address", "100.64.0.7",
		"--fingerprint", hostkeys.Fingerprint(steps.hostKey), "--no-aliases"}

	if _, err := executeWithInput(t, "", args...); err == nil {
		t.Fatal("a key that does not log in, and no password, was accepted")
	}
	if after := readConfigFile(t, dir); after != before {
		t.Fatalf("a failed add changed config.yml:\n%s", after)
	}

	if _, err := os.Stat(filepath.Join(dir, config.KnownHostsFileName)); !os.IsNotExist(err) {
		t.Fatal("a failed add left a trusted host key behind")
	}

	// The server answered with another key the second time: a corrected
	// address, or a rebuild. The first run's key must not stand in the way.
	steps = stubBootstrap(t, bootstrapStubs{keyWorks: true})
	args[len(args)-2] = hostkeys.Fingerprint(steps.hostKey)
	if out, err := executeWithInput(t, "", args...); err != nil {
		t.Fatalf("the second run failed: %v (%s)", err, out)
	}
	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cfg.Machine("sandbox"); err != nil {
		t.Fatalf("the second run did not add it: %v", err)
	}
}

func TestMachinesAddWritesNothingWhenTheProofFails(t *testing.T) {
	dir := writeConfigDir(t, oneMachine)
	before := readConfigFile(t, dir)
	stubBootstrap(t, bootstrapStubs{keyWorks: false, proofFails: true})

	_, err := executeWithInput(t, "sandbox\n198.51.100.7\nroot\n22\n\n1\ndevmachine\n",
		"--config", dir, "machines", "add")
	if err == nil {
		t.Fatal("an unproved key was accepted")
	}
	if after := readConfigFile(t, dir); after != before {
		t.Fatalf("a failed add changed config.yml:\n%s", after)
	}
}

func unattendedAdd(dir, fingerprint string, extra ...string) []string {
	args := []string{"--config", dir, "machines", "add", "--name", "sandbox", "--address", "203.0.113.20",
		"--fingerprint", fingerprint, "--no-aliases"}
	return append(args, extra...)
}

func TestMachinesAddWithPasswordStdinInstallsTheKeyWithIt(t *testing.T) {
	dir := writeConfigDir(t, oneMachine)
	steps := stubBootstrap(t, bootstrapStubs{keyWorks: false})

	out, err := executeWithInput(t, "pass word with spaces\n",
		unattendedAdd(dir, hostkeys.Fingerprint(steps.hostKey), "--password-stdin")...)
	if err != nil {
		t.Fatalf("machines add returned %v (%s)", err, out)
	}
	if steps.password != "pass word with spaces" {
		t.Fatalf("it logged in with %q", steps.password)
	}
	if !steps.installedKey || !steps.proved || !steps.hardened {
		t.Fatalf("it did not install and prove the key: %q", steps.events)
	}
	if strings.Contains(out, "pass word") || strings.Contains(readConfigFile(t, dir), "pass word") {
		t.Fatalf("the password was written somewhere:\n%s", out)
	}
}

func TestMachinesAddWithPasswordStdinDoesNotUseItWhenTheKeyWorks(t *testing.T) {
	dir := writeConfigDir(t, oneMachine)
	steps := stubBootstrap(t, bootstrapStubs{keyWorks: true})

	if out, err := executeWithInput(t, "secret\n",
		unattendedAdd(dir, hostkeys.Fingerprint(steps.hostKey), "--password-stdin")...); err != nil {
		t.Fatalf("machines add returned %v (%s)", err, out)
	}
	if steps.askedForPassword {
		t.Fatal("it logged in with the password although the key works")
	}
}

func TestMachinesAddWithPasswordStdinRefusesAnEmptyOne(t *testing.T) {
	dir := writeConfigDir(t, oneMachine)
	steps := stubBootstrap(t, bootstrapStubs{keyWorks: false})

	_, err := executeWithInput(t, "\n", unattendedAdd(dir, hostkeys.Fingerprint(steps.hostKey), "--password-stdin")...)
	if err == nil || !strings.Contains(err.Error(), "--password-stdin") {
		t.Fatalf("got %v", err)
	}
	if len(steps.events) != 0 {
		t.Fatalf("it went on with no password: %q", steps.events)
	}
}

func TestMachinesAddPasswordStdinNeedsAddress(t *testing.T) {
	dir := writeConfigDir(t, oneMachine)
	stubBootstrap(t, bootstrapStubs{})

	_, err := executeWithInput(t, "secret\n", "--config", dir, "machines", "add", "--password-stdin")
	if err == nil || !strings.Contains(err.Error(), "--address") {
		t.Fatalf("got %v", err)
	}
}

func TestMachinesAddWithFlagsAndNoPasswordSaysHowToGiveOne(t *testing.T) {
	dir := writeConfigDir(t, oneMachine)
	steps := stubBootstrap(t, bootstrapStubs{keyWorks: false})

	_, err := executeWithInput(t, "", unattendedAdd(dir, hostkeys.Fingerprint(steps.hostKey))...)
	if err == nil || !strings.Contains(err.Error(), "--password-stdin") {
		t.Fatalf("got %v", err)
	}
}

func TestMachinesAddWithFlagsUsesAKeyTheAgentHolds(t *testing.T) {
	dir := writeConfigDir(t, oneMachine)
	steps := stubBootstrap(t, bootstrapStubs{keyWorks: true, agent: []keys.Offered{
		{Fingerprint: "SHA256:aaaa", Comment: "alice key", PublicKey: "ssh-ed25519 AAAAalice alice key"},
		{Fingerprint: "SHA256:bbbb", Comment: "bob key", PublicKey: "ssh-ed25519 AAAAbob bob key"},
	}})

	if out, err := executeWithInput(t, "",
		unattendedAdd(dir, hostkeys.Fingerprint(steps.hostKey), "--key", "agent:SHA256:bbbb")...); err != nil {
		t.Fatalf("machines add returned %v (%s)", err, out)
	}
	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	m, err := cfg.Machine("sandbox")
	if err != nil {
		t.Fatal(err)
	}
	if m.Key != "" || m.AgentKey != "ssh-ed25519 AAAAbob bob key" {
		t.Fatalf("key = %q, agent_key = %q", m.Key, m.AgentKey)
	}
	if _, err := os.Stat(m.AgentKeyFile); err != nil {
		t.Fatalf("the agent key file was not written: %v", err)
	}
	if _, err := os.Stat(filepath.Join(keys.Dir(dir), "sandbox")); err == nil {
		t.Fatal("it made a key of its own instead")
	}
}

func TestMachinesAddWithFlagsRefusesAnAgentKeyItDoesNotHold(t *testing.T) {
	dir := writeConfigDir(t, oneMachine)
	before := readConfigFile(t, dir)
	steps := stubBootstrap(t, bootstrapStubs{keyWorks: true, agent: []keys.Offered{
		{Fingerprint: "SHA256:aaaa", Comment: "alice key", PublicKey: "ssh-ed25519 AAAAalice alice key"},
	}})

	_, err := executeWithInput(t, "",
		unattendedAdd(dir, hostkeys.Fingerprint(steps.hostKey), "--key", "agent:SHA256:zzzz")...)
	if err == nil || !strings.Contains(err.Error(), "SHA256:zzzz") || !strings.Contains(err.Error(), "SHA256:aaaa") {
		t.Fatalf("got %v", err)
	}
	if readConfigFile(t, dir) != before || steps.installedKey {
		t.Fatalf("it went on anyway: %q", steps.events)
	}
}

func TestMachinesAddWithFlagsSaysWhenTheAgentHoldsNothing(t *testing.T) {
	dir := writeConfigDir(t, oneMachine)
	steps := stubBootstrap(t, bootstrapStubs{keyWorks: true})

	_, err := executeWithInput(t, "",
		unattendedAdd(dir, hostkeys.Fingerprint(steps.hostKey), "--key", "agent:SHA256:zzzz")...)
	if err == nil || !strings.Contains(err.Error(), "SSH_AUTH_SOCK") {
		t.Fatalf("got %v", err)
	}
}

// With no config.yml yet, `machines add --address` is setup without a
// terminal: it writes the configuration setup would.
func TestMachinesAddWithFlagsAndNoConfigurationWritesOne(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "devmachine")
	t.Cleanup(swap(&latestPackagesRelease, func(context.Context) (string, error) { return "v9", nil }))
	stubReleaseHas(t, true)
	steps := stubBootstrap(t, bootstrapStubs{keyWorks: true})

	out, err := executeWithInput(t, "",
		unattendedAdd(dir, hostkeys.Fingerprint(steps.hostKey), "--domain", "example.com")...)
	if err != nil {
		t.Fatalf("machines add returned %v (%s)", err, out)
	}
	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatalf("it wrote no configuration that loads: %v", err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	if cfg.Packages != "v9" || cfg.Domain != "example.com" ||
		!slices.Equal(cfg.Defaults.Workspace, config.DefaultWorkspacePackages) {
		t.Fatalf("configuration = %#v", cfg)
	}
	if len(cfg.Machines) != 1 || cfg.Machines[0].Name != "sandbox" ||
		!slices.Equal(cfg.Machines[0].Packages, []string{essentials}) {
		t.Fatalf("machines = %#v", cfg.Machines)
	}
	if _, err := os.Stat(filepath.Join(dir, agentsFileName)); err != nil {
		t.Fatalf("no AGENTS.md: %v", err)
	}
}

func TestMachinesAddWithNoConfigurationWritesNoneWhenItFails(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "devmachine")
	steps := stubBootstrap(t, bootstrapStubs{keyWorks: false})

	if _, err := executeWithInput(t, "", unattendedAdd(dir, hostkeys.Fingerprint(steps.hostKey))...); err == nil {
		t.Fatal("a key that does not log in was accepted")
	}
	for _, name := range []string{config.FileName, agentsFileName} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Fatalf("a failed add left %s behind", name)
		}
	}
}

func TestMachinesAddRefusesDomainOnAnExistingConfiguration(t *testing.T) {
	dir := writeConfigDir(t, oneMachine)
	steps := stubBootstrap(t, bootstrapStubs{keyWorks: true})

	_, err := executeWithInput(t, "",
		unattendedAdd(dir, hostkeys.Fingerprint(steps.hostKey), "--domain", "example.com")...)
	if err == nil || !strings.Contains(err.Error(), "--domain") {
		t.Fatalf("got %v", err)
	}
	if len(steps.events) != 0 {
		t.Fatalf("it reached the server first: %q", steps.events)
	}
}

func TestMachinesAddReplacesAStaleHostKeyForANameNotConfigured(t *testing.T) {
	dir := writeConfigDir(t, oneMachine)
	store, err := hostkeys.Open(filepath.Join(dir, config.KnownHostsFileName))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Put("sandbox", 22, commandHostKey(t)); err != nil {
		t.Fatal(err)
	}
	steps := stubBootstrap(t, bootstrapStubs{keyWorks: true})

	if out, err := executeWithInput(t, "", unattendedAdd(dir, hostkeys.Fingerprint(steps.hostKey))...); err != nil {
		t.Fatalf("machines add returned %v (%s)", err, out)
	}
	if err := store.Check("sandbox", 22, steps.hostKey); err != nil {
		t.Fatalf("the new host key is not the trusted one: %v", err)
	}
}

func TestMachinesAddWithQuestionsAndNoConfigurationSendsToSetup(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "devmachine")
	steps := stubBootstrap(t, bootstrapStubs{keyWorks: true})

	_, err := executeWithInput(t, "sandbox\n198.51.100.7\nroot\n22\n1\n", "--config", dir, "machines", "add")
	if err == nil || !strings.Contains(err.Error(), "devmachine setup") || !strings.Contains(err.Error(), "--address") {
		t.Fatalf("got %v", err)
	}
	if len(steps.events) != 0 {
		t.Fatalf("it reached a server first: %q", steps.events)
	}
}

// Two adds into an empty folder at once: the one that finishes second must
// add its machine to the configuration the first wrote, not overwrite it.
func TestMachinesAddIntoAConfigurationWrittenMeanwhileAddsToIt(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "devmachine")
	steps := stubBootstrap(t, bootstrapStubs{keyWorks: true})
	t.Cleanup(swap(&installAnsible, func(context.Context, remote.Client, io.Writer) error {
		return os.WriteFile(filepath.Join(dir, config.FileName),
			[]byte("domain: example.org\nmachines:\n  - name: other\n    hosts: [203.0.113.30]\n"), 0o600)
	}))

	out, err := executeWithInput(t, "",
		unattendedAdd(dir, hostkeys.Fingerprint(steps.hostKey), "--domain", "example.com")...)
	if err != nil {
		t.Fatalf("machines add returned %v (%s)", err, out)
	}
	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Machines) != 2 || cfg.Machines[0].Name != "other" || cfg.Machines[1].Name != "sandbox" {
		t.Fatalf("machines = %#v", cfg.Machines)
	}
	if cfg.Domain != "example.org" || !strings.Contains(out, "--domain") {
		t.Fatalf("domain %q, and the output does not say --domain was not written: %s", cfg.Domain, out)
	}
}
