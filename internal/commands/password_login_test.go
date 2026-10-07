package commands

import (
	"bufio"
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/mydevmachine/devmachine/internal/config"
	"github.com/mydevmachine/devmachine/internal/hostkeys"
	"github.com/mydevmachine/devmachine/internal/remote"
)

func TestSetupKeepsPasswordLoginForTheAccountsNamed(t *testing.T) {
	steps := stubBootstrap(t, bootstrapStubs{keyWorks: true})
	dir := t.TempDir()

	out, err := runSetupIn(t, dir, answers("main", "203.0.113.10", "root", "22", "", "1", "2", "alice, bob"),
		setupOptions{noAliases: true})
	if err != nil {
		t.Fatalf("runSetup returned %v (%s)", err, out)
	}
	if !steps.hardened || !slices.Equal(steps.hardenKeep, []string{"alice", "bob"}) {
		t.Fatalf("hardened %v keeping %v", steps.hardened, steps.hardenKeep)
	}
	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	m, _ := cfg.Machine("main")
	if !slices.Equal(m.PasswordLoginKeep, []string{"alice", "bob"}) {
		t.Fatalf("password_login_keep = %v", m.PasswordLoginKeep)
	}
	if !strings.Contains(out, "except for alice and bob") {
		t.Fatalf("got:\n%s", out)
	}
}

func TestSetupLeavesPasswordLoginAsItWasWhenAsked(t *testing.T) {
	steps := stubBootstrap(t, bootstrapStubs{keyWorks: true})
	if _, err := runSetupIn(t, t.TempDir(), answers("main", "203.0.113.10", "root", "22", "", "1", "3"),
		setupOptions{noAliases: true}); err != nil {
		t.Fatal(err)
	}
	if steps.hardened {
		t.Fatal("it turned password login off after being told to leave it")
	}
}

func TestSetupTurnsPasswordLoginOffForEverybodyByDefault(t *testing.T) {
	steps := stubBootstrap(t, bootstrapStubs{keyWorks: true})
	if _, err := runSetupIn(t, t.TempDir(), answers("main", "203.0.113.10", "root", "22", "", "1", ""),
		setupOptions{noAliases: true}); err != nil {
		t.Fatal(err)
	}
	if !steps.hardened || len(steps.hardenKeep) != 0 {
		t.Fatalf("hardened %v keeping %v", steps.hardened, steps.hardenKeep)
	}
}

func TestMachinesAddKeepPasswordLoginAsksNothingAndRecordsTheAccounts(t *testing.T) {
	dir := writeConfigDir(t, oneMachine)
	steps := stubBootstrap(t, bootstrapStubs{keyWorks: true})

	out, err := executeWithInput(t, "",
		unattendedAdd(dir, hostkeys.Fingerprint(steps.hostKey), "--keep-password-login", "alice")...)
	if err != nil {
		t.Fatalf("machines add returned %v (%s)", err, out)
	}
	if !slices.Equal(steps.hardenKeep, []string{"alice"}) {
		t.Fatalf("kept %v", steps.hardenKeep)
	}
	if !strings.Contains(readConfigFile(t, dir), "password_login_keep: [alice]") {
		t.Fatalf("got:\n%s", readConfigFile(t, dir))
	}
}

func TestMachinesAddUnattendedTurnsPasswordLoginOffWithoutAsking(t *testing.T) {
	dir := writeConfigDir(t, oneMachine)
	steps := stubBootstrap(t, bootstrapStubs{keyWorks: true})
	if out, err := executeWithInput(t, "", unattendedAdd(dir, hostkeys.Fingerprint(steps.hostKey))...); err != nil {
		t.Fatalf("machines add returned %v (%s)", err, out)
	}
	if !steps.hardened || len(steps.hardenKeep) != 0 {
		t.Fatalf("hardened %v keeping %v", steps.hardened, steps.hardenKeep)
	}
}

func TestKeepPasswordLoginAndNoHardenAreRefusedTogether(t *testing.T) {
	dir := writeConfigDir(t, oneMachine)
	steps := stubBootstrap(t, bootstrapStubs{keyWorks: true})
	_, err := executeWithInput(t, "",
		unattendedAdd(dir, hostkeys.Fingerprint(steps.hostKey), "--keep-password-login", "alice", "--no-harden")...)
	if err == nil || !strings.Contains(err.Error(), "pick one") {
		t.Fatalf("got %v", err)
	}
	if len(steps.events) > 0 {
		t.Fatalf("it touched the machine first: %v", steps.events)
	}
}

func TestKeepPasswordLoginRefusesANameSshdWouldReadAsAPattern(t *testing.T) {
	dir := writeConfigDir(t, oneMachine)
	steps := stubBootstrap(t, bootstrapStubs{keyWorks: true})
	_, err := executeWithInput(t, "",
		unattendedAdd(dir, hostkeys.Fingerprint(steps.hostKey), "--keep-password-login", "*")...)
	if err == nil || !strings.Contains(err.Error(), "--keep-password-login") {
		t.Fatalf("got %v", err)
	}
}

func TestAskPasswordLoginOnAMacSaysItIsEveryAccount(t *testing.T) {
	out := &strings.Builder{}
	keep, leaveOn, err := askPasswordLogin(bufio.NewReader(strings.NewReader("2\n\n")), out,
		[]string{"alice", "bob"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if leaveOn || !slices.Equal(keep, []string{"alice", "bob"}) {
		t.Fatalf("kept %v, leave on %v", keep, leaveOn)
	}
	for _, want := range []string{"every account on it", "alice, bob", "login window"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("no %q in:\n%s", want, out)
		}
	}
}

func TestAskPasswordLoginRefusesAnAnswerThatIsNotAChoice(t *testing.T) {
	if _, _, err := askPasswordLogin(bufio.NewReader(strings.NewReader("y\n")), &strings.Builder{}, nil, false); err == nil {
		t.Fatal("y was taken as a choice")
	}
}

// stubPasswordLogin answers machines password-login without a machine.
type passwordLoginCalls struct {
	hardened bool
	keep     []string
	on       bool
}

func stubPasswordLogin(t *testing.T, system remote.System, state remote.PasswordLoginState) *passwordLoginCalls {
	t.Helper()
	calls := &passwordLoginCalls{}
	t.Cleanup(swap(&dial, func(_ context.Context, m config.Machine, _ string) (remote.Client, string, error) {
		return nopClient{}, m.Hosts[0].Address, nil
	}))
	t.Cleanup(swap(&detectSystem, func(context.Context, remote.Client) (remote.System, error) { return system, nil }))
	t.Cleanup(swap(&harden, func(_ context.Context, _ remote.Client, _ remote.System, keep []string) error {
		calls.hardened, calls.keep = true, keep
		return nil
	}))
	t.Cleanup(swap(&passwordLoginOn, func(context.Context, remote.Client, remote.System) error {
		calls.on = true
		return nil
	}))
	t.Cleanup(swap(&readPasswordLogin, func(context.Context, remote.Client, remote.System) (remote.PasswordLoginState, error) {
		return state, nil
	}))
	return calls
}

var debianSystem = remote.System{Kernel: "Linux", ID: "debian"}

func TestPasswordLoginShowsWhatSshdSaysAsJSON(t *testing.T) {
	dir := writeConfigDir(t, oneMachine)
	stubPasswordLogin(t, debianSystem, remote.PasswordLoginState{
		DropIn: true, Accounts: []remote.AccountPasswordLogin{{User: "alice", Allowed: true}},
	})
	out, err := execute(t, "--config", dir, "--format", "json", "machines", "password-login", "main")
	if err != nil {
		t.Fatalf("%v (%s)", err, out)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if got["machine"] != "main" || got["default"] != false || got["drop_in"] != true {
		t.Fatalf("got %s", out)
	}
	if accounts := got["accounts"].([]any); accounts[0].(map[string]any)["password_login"] != true {
		t.Fatalf("got %s", out)
	}
}

func TestPasswordLoginKeepChangesTheMachineAndTheConfiguration(t *testing.T) {
	dir := writeConfigDir(t, oneMachine)
	calls := stubPasswordLogin(t, debianSystem, remote.PasswordLoginState{})
	if out, err := execute(t, "--config", dir, "machines", "password-login", "main", "--keep", "alice", "--yes"); err != nil {
		t.Fatalf("%v (%s)", err, out)
	}
	if !calls.hardened || !slices.Equal(calls.keep, []string{"alice"}) {
		t.Fatalf("got %+v", calls)
	}
	if !strings.Contains(readConfigFile(t, dir), "password_login_keep: [alice]") {
		t.Fatalf("got:\n%s", readConfigFile(t, dir))
	}

	if out, err := execute(t, "--config", dir, "machines", "password-login", "main", "--on", "--yes"); err != nil {
		t.Fatalf("%v (%s)", err, out)
	}
	if !calls.on || strings.Contains(readConfigFile(t, dir), "password_login_keep") {
		t.Fatalf("got %+v:\n%s", calls, readConfigFile(t, dir))
	}
}

func TestPasswordLoginCheckChangesNothing(t *testing.T) {
	dir := writeConfigDir(t, oneMachine)
	calls := stubPasswordLogin(t, debianSystem, remote.PasswordLoginState{})
	out, err := execute(t, "--config", dir, "machines", "password-login", "main", "--off", "--check")
	if err != nil || !strings.Contains(out, "nothing was changed") {
		t.Fatalf("%v (%s)", err, out)
	}
	if calls.hardened {
		t.Fatal("--check changed the machine")
	}
}

func TestPasswordLoginChangesNothingUnlessConfirmed(t *testing.T) {
	dir := writeConfigDir(t, oneMachine)
	calls := stubPasswordLogin(t, remote.System{Kernel: remote.KernelDarwin}, remote.PasswordLoginState{})
	out, err := executeWithInput(t, "n\n", "--config", dir, "machines", "password-login", "main", "--off")
	if err == nil || calls.hardened {
		t.Fatalf("%v hardened %v", err, calls.hardened)
	}
	if !strings.Contains(out, "every account on it") {
		t.Fatalf("a Mac was not warned:\n%s", out)
	}
}

func TestPasswordLoginRefusesFlagsThatContradictEachOther(t *testing.T) {
	dir := writeConfigDir(t, oneMachine)
	stubPasswordLogin(t, debianSystem, remote.PasswordLoginState{})
	_, err := execute(t, "--config", dir, "machines", "password-login", "main", "--on", "--keep", "alice")
	if err == nil || !strings.Contains(err.Error(), "pick one") {
		t.Fatalf("got %v", err)
	}
}

func TestPasswordLoginOnIsRefusedWhileSshHardeningWouldTurnItBackOff(t *testing.T) {
	dir := writeConfigDir(t, "machines:\n  - name: main\n    hosts: [203.0.113.10]\n    packages: [ssh_hardening]\n")
	calls := stubPasswordLogin(t, debianSystem, remote.PasswordLoginState{})
	_, err := execute(t, "--config", dir, "machines", "password-login", "main", "--on", "--yes")
	if err == nil || !strings.Contains(err.Error(), "ssh_hardening") || calls.on {
		t.Fatalf("got %v, on %v", err, calls.on)
	}
}

func TestMachinesAddWithNoConfigurationRecordsTheAccountsThatKeepPasswordLogin(t *testing.T) {
	dir := t.TempDir() + "/devmachine"
	t.Cleanup(swap(&latestPackagesRelease, func(context.Context) (string, error) { return "v9", nil }))
	stubReleaseHas(t, true)
	steps := stubBootstrap(t, bootstrapStubs{keyWorks: true})

	if out, err := executeWithInput(t, "",
		unattendedAdd(dir, hostkeys.Fingerprint(steps.hostKey), "--keep-password-login", "alice,bob")...); err != nil {
		t.Fatalf("machines add returned %v (%s)", err, out)
	}
	if body := readConfigFile(t, dir); !strings.Contains(body, "password_login_keep: [alice, bob]") {
		t.Fatalf("got:\n%s", body)
	}
}

func TestSetupWithNoAccountKeptLeavesTheFileAsItWroteIt(t *testing.T) {
	dir := t.TempDir()
	stubBootstrap(t, bootstrapStubs{keyWorks: true})
	if _, err := runSetupIn(t, dir, answers("main", "203.0.113.10", "root", "22", "", "1", ""),
		setupOptions{noAliases: true}); err != nil {
		t.Fatal(err)
	}
	body := readConfigFile(t, dir)
	if strings.Contains(body, "password_login_keep") || !strings.Contains(body, "\n      key: ") {
		t.Fatalf("got:\n%s", body)
	}
}
