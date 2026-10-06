package commands

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mydevmachine/devmachine/internal/config"
	"github.com/mydevmachine/devmachine/internal/facts"
	"github.com/mydevmachine/devmachine/internal/hostkeys"
	"github.com/mydevmachine/devmachine/internal/keys"
)

// withoutLima empties PATH, which is the only honest way to meet the machine
// that has no Lima on it from inside a test.
func withoutLima(t *testing.T) {
	t.Helper()
	t.Setenv("PATH", "")
}

func TestCreateLocalSaysHowToInstallLima(t *testing.T) {
	withoutLima(t)

	_, err := execute(t, "machines", "create-local", "alpha")
	if err == nil {
		t.Fatal("it reported a machine where there is no lima")
	}
	if !strings.Contains(err.Error(), "brew install lima") {
		t.Fatalf("got %v", err)
	}
}

func TestLocalMachineCommandsNeedAName(t *testing.T) {
	for _, command := range []string{"create-local", "start", "stop", "delete-local"} {
		if _, err := execute(t, "machines", command); err == nil {
			t.Fatalf("%s ran with no name", command)
		}
	}
}

func TestDeleteLocalAsksBeforeDestroying(t *testing.T) {
	withoutLima(t)

	// The question comes first, so a no costs nothing — not even the lima
	// that is not installed here.
	out, err := executeWithInput(t, "n\n", "machines", "delete-local", "alpha")
	if !errors.Is(err, errDeclined) {
		t.Fatalf("got %v", err)
	}
	if !strings.Contains(out, "alpha") {
		t.Fatalf("the question does not name the machine: %q", out)
	}
}

func TestDeleteLocalWithYesDoesNotAsk(t *testing.T) {
	withoutLima(t)

	_, err := executeWithInput(t, "", "machines", "delete-local", "--yes", "alpha")
	if err == nil || errors.Is(err, errDeclined) {
		t.Fatalf("it asked, or did nothing: %v", err)
	}
	if !strings.Contains(err.Error(), "brew install lima") {
		t.Fatalf("got %v", err)
	}
}

func TestMachinesAddBootstrapsAndWritesTheNewMachine(t *testing.T) {
	dir := writeConfigDir(t, "machines:\n  - name: main\n    hosts: [203.0.113.10]\ndomain: example.com\n")
	steps := stubBootstrap(t, bootstrapStubs{keyWorks: false})

	// The same questions as setup, minus the domain, then the same bootstrap.
	out, err := executeWithInput(t, "sandbox\n198.51.100.7\nroot\n2222\n\n1\ndevmachine\n",
		"--config", dir, "machines", "add")
	if err != nil {
		t.Fatalf("machines add returned %v (%s)", err, out)
	}

	if !steps.installedKey || !steps.proved || !steps.hardened || !steps.ansible {
		t.Fatalf("it did not run the bootstrap: %#v", steps)
	}

	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Machines) != 2 || cfg.Machines[1].Name != "sandbox" || cfg.Machines[1].Port != 2222 {
		t.Fatalf("machines = %#v", cfg.Machines)
	}
	if cfg.Domain != "example.com" {
		t.Fatalf("the rest of the configuration changed: %q", cfg.Domain)
	}
}

func TestMachinesAddRecordsTheChosenAgentKey(t *testing.T) {
	dir := writeConfigDir(t, "machines:\n  - name: main\n    hosts: [203.0.113.10]\n")
	stubBootstrap(t, bootstrapStubs{keyWorks: false, agent: []keys.Offered{
		{Fingerprint: "SHA256:bbbb", Comment: "bob key", PublicKey: "ssh-ed25519 AAAAagent bob key"},
	}})

	out, err := executeWithInput(t, "sandbox\n198.51.100.7\nroot\n2222\n\n3\ndevmachine\n",
		"--config", dir, "machines", "add")
	if err != nil {
		t.Fatalf("machines add returned %v (%s)", err, out)
	}

	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	added := cfg.Machines[1]
	if added.Key != "" {
		t.Fatalf("key = %q, want empty", added.Key)
	}
	if added.AgentKey != "ssh-ed25519 AAAAagent bob key" {
		t.Fatalf("agent_key = %q, want the chosen key", added.AgentKey)
	}
	if _, err := os.Stat(added.AgentKeyFile); err != nil {
		t.Fatalf("the agent key file was not written: %v", err)
	}
}

func TestMachinesAddHostTrustRefusalChangesNothing(t *testing.T) {
	dir := writeConfigDir(t, "machines:\n  - name: main\n    hosts: [203.0.113.10]\ndomain: example.com\n")
	before, err := os.ReadFile(filepath.Join(dir, config.FileName))
	if err != nil {
		t.Fatal(err)
	}
	steps := stubBootstrap(t, bootstrapStubs{keyWorks: false})
	t.Cleanup(swap(&confirmHostKey, func(_ io.Reader, _ io.Writer, _ string) (bool, error) {
		steps.events = append(steps.events, "ask trust")
		return false, nil
	}))

	_, err = executeWithInput(t, "sandbox\n198.51.100.7\nroot\n2222\n\n",
		"--config", dir, "machines", "add")
	if !errors.Is(err, errDeclined) {
		t.Fatalf("machines add error = %v, want declined", err)
	}
	after, err := os.ReadFile(filepath.Join(dir, config.FileName))
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("config changed after refusal:\n%s", after)
	}
	if _, statErr := os.Stat(filepath.Join(dir, config.KnownHostsFileName)); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatal("refusal created known_hosts")
	}
	if !slices.Equal(steps.events, []string{"scan host key", "ask trust"}) {
		t.Fatalf("refusal events = %#v", steps.events)
	}
	if steps.installedKey || steps.proved || steps.hardened || steps.ansible {
		t.Fatalf("refusal bootstrapped: %#v", steps)
	}
}

func TestMachinesAddRefusesANameThatIsTaken(t *testing.T) {
	dir := writeConfigDir(t, "machines:\n  - name: main\n    hosts: [203.0.113.10]\n")
	steps := stubBootstrap(t, bootstrapStubs{keyWorks: true})

	_, err := executeWithInput(t, "main\n198.51.100.7\nroot\n22\n1\n", "--config", dir, "machines", "add")
	if err == nil {
		t.Fatal("two machines were allowed the same name")
	}
	// The name is refused before anything is done to a server.
	if steps.hardened {
		t.Fatal("it hardened a machine it was never going to record")
	}
}

func TestMachinesRmForgetsTheMachineAndSaysTheServerIsUntouched(t *testing.T) {
	dir := writeConfigDir(t, `machines:
  - name: main
    hosts: [203.0.113.10]
  - name: sandbox
    hosts: [198.51.100.7]
`)

	out, err := executeWithInput(t, "", "--config", dir, "machines", "rm", "--yes", "sandbox")
	if err != nil {
		t.Fatalf("machines rm returned %v", err)
	}

	// `rm` and `delete-local` are one letter apart in a person's head. Saying
	// plainly that the server keeps running is what keeps them apart.
	for _, want := range []string{"still running", "delete-local"} {
		if !strings.Contains(out, want) {
			t.Fatalf("the output does not say what it did not do: %q", out)
		}
	}

	cfg, _ := config.Load(dir)
	if len(cfg.Machines) != 1 || cfg.Machines[0].Name != "main" {
		t.Fatalf("machines = %#v", cfg.Machines)
	}
}

func TestMachinesRmForgetsWhatTheCLIObservedAboutTheMachine(t *testing.T) {
	dir := writeConfigDir(t, `machines:
  - name: main
    hosts: [203.0.113.10]
  - name: sandbox
    hosts: [198.51.100.7]
`)
	saveFacts(t, dir, "sandbox", observedMac)
	saveFacts(t, dir, "main", observedMac)

	if out, err := executeWithInput(t, "", "--config", dir, "machines", "rm", "--yes", "sandbox"); err != nil {
		t.Fatalf("machines rm returned %v (%s)", err, out)
	}
	if _, err := os.Stat(facts.Path(dir, "sandbox")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the facts of sandbox are still there: %v", err)
	}
	if _, err := os.Stat(facts.Path(dir, "main")); err != nil {
		t.Fatalf("the facts of main went too: %v", err)
	}
}

func TestMachinesRmAsksFirst(t *testing.T) {
	dir := writeConfigDir(t, `machines:
  - name: main
    hosts: [203.0.113.10]
  - name: sandbox
    hosts: [198.51.100.7]
`)

	out, err := executeWithInput(t, "n\n", "--config", dir, "machines", "rm", "sandbox")
	if !errors.Is(err, errDeclined) {
		t.Fatalf("got %v", err)
	}
	if !strings.Contains(out, "sandbox") {
		t.Fatalf("the question does not name the machine: %q", out)
	}

	cfg, _ := config.Load(dir)
	if len(cfg.Machines) != 2 {
		t.Fatal("a no removed it anyway")
	}
}

func TestMachinesRmRefusesToOrphanAWorkspace(t *testing.T) {
	dir := writeConfigDir(t, `machines:
  - name: main
    hosts: [203.0.113.10]
  - name: sandbox
    hosts: [198.51.100.7]
workspaces:
  - name: alice
    machine: sandbox
`)

	_, err := executeWithInput(t, "", "--config", dir, "machines", "rm", "--yes", "sandbox")
	if err == nil {
		t.Fatal("it left a workspace pointing at nothing")
	}
	if !strings.Contains(err.Error(), "alice") {
		t.Fatalf("the error does not name the workspace: %v", err)
	}
}

func TestMachinesRmIsNotDeleteLocal(t *testing.T) {
	// Two commands whose names look alike, one of which destroys a machine.
	// The help of each has to say which is which.
	out, err := execute(t, "machines", "rm", "--help")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "delete-local") {
		t.Fatalf("rm's help does not point at the destructive one: %q", out)
	}

	out, err = execute(t, "machines", "delete-local", "--help")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "machines rm") {
		t.Fatalf("delete-local's help does not point at the harmless one: %q", out)
	}
}

func TestMachinesAddGivesTheNewMachineTheEssentials(t *testing.T) {
	dir := writeConfigDir(t, "packages: v14\nmachines:\n  - name: main\n    hosts: [203.0.113.10]\n")
	stubReleaseHas(t, true)
	stubBootstrap(t, bootstrapStubs{keyWorks: true})

	if out, err := executeWithInput(t, "sandbox\n198.51.100.7\nroot\n22\n\n1\n",
		"--config", dir, "machines", "add"); err != nil {
		t.Fatalf("machines add returned %v (%s)", err, out)
	}
	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(cfg.Machines[1].Packages, []string{"essentials"}) {
		t.Fatalf("new machine packages are %#v", cfg.Machines[1].Packages)
	}
}

// TestMachinesAddWithFlagsAsksNothing: an agent or a script adds a machine in
// one command. Every question has a flag, and none is asked.
func TestMachinesAddWithFlagsAsksNothing(t *testing.T) {
	dir := writeConfigDir(t, "machines:\n  - name: main\n    hosts: [203.0.113.10]\n")
	steps := stubBootstrap(t, bootstrapStubs{keyWorks: true})

	out, err := executeWithInput(t, "",
		"--config", dir, "machines", "add",
		"--name", "sandbox", "--address", "100.64.0.7", "--user", "alice", "--port", "2222",
		"--fingerprint", hostkeys.Fingerprint(steps.hostKey), "--no-aliases")
	if err != nil {
		t.Fatalf("machines add returned %v (%s)", err, out)
	}
	if slices.Contains(steps.events, "ask trust") {
		t.Fatalf("it asked to trust the host key: %q", steps.events)
	}
	if !steps.hardened || !steps.ansible {
		t.Fatalf("it did not run the bootstrap: %q", steps.events)
	}

	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	m, err := cfg.Machine("sandbox")
	if err != nil {
		t.Fatal(err)
	}
	if m.User != "alice" || m.Port != 2222 || m.Hosts[0].Address != "100.64.0.7" {
		t.Fatalf("machine = %#v", m)
	}
	if m.Key != filepath.Join(keys.Dir(dir), "sandbox") {
		t.Fatalf("it did not make a key of its own: %q", m.Key)
	}
}

// TestMachinesAddWithFlagsRefusesAnUncheckedHostKey: with nobody to ask,
// trusting whatever answered would be trust on first use with no one looking.
// The fingerprint has to come from the person, checked elsewhere.
func TestMachinesAddWithFlagsRefusesAnUncheckedHostKey(t *testing.T) {
	for _, c := range []struct{ name, fingerprint string }{
		{"none given", ""},
		{"a different one", "SHA256:AAAAnotthisone"},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := writeConfigDir(t, "machines:\n  - name: main\n    hosts: [203.0.113.10]\n")
			before, err := os.ReadFile(filepath.Join(dir, config.FileName))
			if err != nil {
				t.Fatal(err)
			}
			steps := stubBootstrap(t, bootstrapStubs{keyWorks: true})

			args := []string{"--config", dir, "machines", "add", "--name", "sandbox", "--address", "100.64.0.7"}
			if c.fingerprint != "" {
				args = append(args, "--fingerprint", c.fingerprint)
			}
			_, err = executeWithInput(t, "", args...)
			if err == nil || !strings.Contains(err.Error(), "--fingerprint") ||
				!strings.Contains(err.Error(), hostkeys.Fingerprint(steps.hostKey)) {
				t.Fatalf("got %v", err)
			}
			after, err := os.ReadFile(filepath.Join(dir, config.FileName))
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != string(before) || steps.installedKey || steps.hardened {
				t.Fatalf("it went on anyway: %q", steps.events)
			}
		})
	}
}

func TestMachinesAddWithFlagsAddsTailscaleOnlyWhenAsked(t *testing.T) {
	for _, want := range []bool{false, true} {
		dir := writeConfigDir(t, "machines:\n  - name: main\n    hosts: [203.0.113.10]\n")
		steps := stubBootstrap(t, bootstrapStubs{keyWorks: true})
		args := []string{"--config", dir, "machines", "add", "--name", "sandbox", "--address", "100.64.0.7",
			"--fingerprint", hostkeys.Fingerprint(steps.hostKey), "--no-aliases"}
		if want {
			args = append(args, "--tailscale")
		}
		if _, err := executeWithInput(t, "", args...); err != nil {
			t.Fatal(err)
		}
		cfg, err := config.Load(dir)
		if err != nil {
			t.Fatal(err)
		}
		m, _ := cfg.Machine("sandbox")
		if got := slices.Contains(m.Packages, tailscalePackage); got != want {
			t.Fatalf("--tailscale %v: packages %q", want, m.Packages)
		}
	}
}

func TestMachinesEditWritesASettingIntoTheMachine(t *testing.T) {
	dir := configWithKey(t, "    packages: [hostinger]\n")

	_, err := execute(t, "--config", dir, "machines", "edit", "main",
		"--set", "hostinger.zones=[example.com, example.org]", "--yes")
	if err != nil {
		t.Fatal(err)
	}

	cfg, _ := config.Load(dir)
	m, _ := cfg.Machine("main")
	zones, ok := m.Settings["hostinger.zones"].([]any)
	if !ok || len(zones) != 2 || zones[0] != "example.com" {
		t.Fatalf("got %#v", m.Settings)
	}
}

func TestMachinesEditRefusesASettingForAPackageItDoesNotHave(t *testing.T) {
	dir := configWithKey(t, "    packages: [caddy]\n")

	_, err := execute(t, "--config", dir, "machines", "edit", "main", "--set", "hostinger.zones=[example.com]", "--yes")
	if err == nil {
		t.Fatal("it wrote a setting nothing would read")
	}
	if !strings.Contains(err.Error(), "hostinger") {
		t.Fatalf("the error does not name it: %v", err)
	}
	cfg, _ := config.Load(dir)
	m, _ := cfg.Machine("main")
	if len(m.Settings) != 0 {
		t.Fatalf("it wrote anyway: %#v", m.Settings)
	}
}

func TestMachinesEditUnsetTakesASettingOut(t *testing.T) {
	dir := configWithKey(t, "    packages: [caddy, hostinger]\n    settings:\n      caddy.email: alice@example.com\n      hostinger.zones: [example.com]\n")

	if _, err := execute(t, "--config", dir, "machines", "edit", "main", "--unset", "hostinger.zones", "--yes"); err != nil {
		t.Fatal(err)
	}
	if _, err := execute(t, "--config", dir, "machines", "edit", "main", "--set", "caddy.email=", "--yes"); err != nil {
		t.Fatal(err)
	}

	cfg, _ := config.Load(dir)
	m, _ := cfg.Machine("main")
	if len(m.Settings) != 0 {
		t.Fatalf("a setting survived: %#v", m.Settings)
	}
}

func TestMachinesEditRefusesToSetAndUnsetTheSameSetting(t *testing.T) {
	dir := configWithKey(t, "    packages: [hostinger]\n")

	_, err := execute(t, "--config", dir, "machines", "edit", "main",
		"--set", "hostinger.zones=[example.com]", "--unset", "hostinger.zones", "--yes")
	if err == nil {
		t.Fatal("it accepted two orders that contradict each other")
	}
}

func TestMachinesEditRefusesASetWithNoValue(t *testing.T) {
	dir := configWithKey(t, "    packages: [hostinger]\n")

	_, err := execute(t, "--config", dir, "machines", "edit", "main", "--set", "hostinger.zones", "--yes")
	if err == nil || !strings.Contains(err.Error(), "=") {
		t.Fatalf("got %v", err)
	}
}

func TestMachinesEditWithNothingToChangeSaysSo(t *testing.T) {
	dir := configWithKey(t, "    packages: [hostinger]\n")

	_, err := execute(t, "--config", dir, "machines", "edit", "main", "--yes")
	if err == nil || !strings.Contains(err.Error(), "--set") {
		t.Fatalf("got %v", err)
	}
}

func TestMachinesEditRefusesAMachineThatIsNotConfigured(t *testing.T) {
	dir := configWithKey(t, "    packages: [hostinger]\n")

	_, err := execute(t, "--config", dir, "machines", "edit", "sandbox", "--set", "hostinger.zones=[example.com]", "--yes")
	if err == nil {
		t.Fatal("it edited a machine that is not configured")
	}
}

func TestMachinesEditCheckWritesNothing(t *testing.T) {
	dir := configWithKey(t, "    packages: [hostinger]\n")

	out, err := execute(t, "--config", dir, "machines", "edit", "main", "--set", "hostinger.zones=[example.com]", "--check")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "would") {
		t.Fatalf("a dry run should say what it would do: %q", out)
	}
	cfg, _ := config.Load(dir)
	m, _ := cfg.Machine("main")
	if len(m.Settings) != 0 {
		t.Fatalf("a dry run wrote: %#v", m.Settings)
	}
}

func TestMachinesEditAsksFirst(t *testing.T) {
	dir := configWithKey(t, "    packages: [hostinger]\n")

	if _, err := executeWithInput(t, "n\n", "--config", dir, "machines", "edit", "main",
		"--set", "hostinger.zones=[example.com]"); !errors.Is(err, errDeclined) {
		t.Fatalf("got %v", err)
	}
	cfg, _ := config.Load(dir)
	m, _ := cfg.Machine("main")
	if len(m.Settings) != 0 {
		t.Fatalf("a no still wrote: %#v", m.Settings)
	}
}
