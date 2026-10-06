package commands

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mydevmachine/devmachine/internal/config"
	"github.com/mydevmachine/devmachine/internal/history"
	"github.com/mydevmachine/devmachine/internal/remote"
)

// captureInteractive records what would have been launched instead of
// launching it, so these tests never open a session.
func captureInteractive(t *testing.T) *[]string {
	t.Helper()
	var got []string
	runInteractive = func(name string, args ...string) error {
		got = append([]string{name}, args...)
		return nil
	}
	lookPath = func(string) (string, error) { return "/usr/bin/fake", nil }
	verifySystemHost = func(context.Context, config.Machine) error { return nil }
	t.Cleanup(func() {
		runInteractive = realExecCommand
		lookPath = realLookPath
		verifySystemHost = realVerifySystemHost
	})
	return &got
}

func configWith(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.yml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestSSHUsesTheConfiguredUserPortAndKey(t *testing.T) {
	got := captureInteractive(t)
	dir := configWith(t, "machines:\n  - name: main\n    hosts: [203.0.113.10]\n    user: alice\n    port: 2222\n    key: /keys/id_ed25519\n")

	if _, err := execute(t, "--config", dir, "ssh"); err != nil {
		t.Fatalf("ssh returned %v", err)
	}

	line := strings.Join(*got, " ")
	for _, want := range []string{"ssh", "-p 2222", "-i /keys/id_ed25519", "alice@203.0.113.10"} {
		if !strings.Contains(line, want) {
			t.Fatalf("the command is missing %q: %q", want, line)
		}
	}
}

func TestSSHTakesAUserFromTheArgument(t *testing.T) {
	got := captureInteractive(t)
	dir := configWith(t, "machines:\n  - name: main\n    hosts: [203.0.113.10]\n    user: root\nworkspaces:\n  - name: bob\n")

	if _, err := execute(t, "--config", dir, "ssh", "bob"); err != nil {
		t.Fatalf("ssh returned %v", err)
	}

	line := strings.Join(*got, " ")
	if !strings.Contains(line, "bob@203.0.113.10") {
		t.Fatalf("the argument did not override the configured user: %q", line)
	}
}

func TestMoshPassesThePortThroughItsSSHOption(t *testing.T) {
	got := captureInteractive(t)
	dir := configWith(t, "machines:\n  - name: main\n    hosts: [203.0.113.10]\n    port: 2222\n    key: /keys/id_ed25519\n")

	if _, err := execute(t, "--config", dir, "mosh"); err != nil {
		t.Fatalf("mosh returned %v", err)
	}

	line := strings.Join(*got, " ")
	// mosh does not take -p or -i itself; they have to travel inside --ssh.
	if !strings.Contains(line, "'-p' '2222'") || !strings.Contains(line, "'-i' '/keys/id_ed25519'") {
		t.Fatalf("the port and key did not reach mosh's ssh command: %q", line)
	}
	if !strings.Contains(line, "root@203.0.113.10") {
		t.Fatalf("the target is missing: %q", line)
	}
}

func TestAnInteractiveCommandSaysWhenTheBinaryIsMissing(t *testing.T) {
	runInteractive = func(string, ...string) error { return nil }
	lookPath = func(string) (string, error) { return "", errors.New("not found") }
	t.Cleanup(func() { runInteractive = realExecCommand; lookPath = realLookPath })

	dir := configWith(t, "machines:\n  - name: main\n    hosts: [203.0.113.10]\n")

	_, err := execute(t, "--config", dir, "mosh")
	if err == nil {
		t.Fatal("expected an error when mosh is not installed")
	}
	if !strings.Contains(err.Error(), "mosh") {
		t.Fatalf("the error does not name the missing binary: %v", err)
	}
}

func TestAnInteractiveCommandRefusesAnInvalidConfig(t *testing.T) {
	got := captureInteractive(t)
	dir := configWith(t, "domain: example.com\n")

	if _, err := execute(t, "--config", dir, "ssh"); err == nil {
		t.Fatal("expected an error when there is no host")
	}
	if len(*got) != 0 {
		t.Fatalf("it launched a session with an invalid configuration: %v", *got)
	}
}

func TestHumanBytesReadsLikeAPersonWouldWriteIt(t *testing.T) {
	cases := map[uint64]string{
		0:          "0B",
		512:        "512B",
		1024:       "1.0K",
		1536:       "1.5K",
		1073741824: "1.0G",
	}
	for in, want := range cases {
		if got := humanBytes(in); got != want {
			t.Fatalf("humanBytes(%d) = %q, want %q", in, got, want)
		}
	}
}

const twoMachineConfig = `
machines:
  - name: main
    hosts: [203.0.113.10]
    port: 22
  - name: sandbox
    hosts: [198.51.100.7]
    port: 2222
    key: /keys/sandbox
workspaces:
  - name: alice
    machine: main
  - name: bob
    machine: sandbox
    user: bob-dev
`

func TestSSHSendsAWorkspaceToItsOwnMachine(t *testing.T) {
	got := captureInteractive(t)
	dir := configWith(t, twoMachineConfig)

	if _, err := execute(t, "--config", dir, "ssh", "bob"); err != nil {
		t.Fatalf("ssh returned %v", err)
	}

	line := strings.Join(*got, " ")
	// bob lives on sandbox, so its address, port, key and account are
	// the ones that have to appear — never main's.
	for _, want := range []string{"-p 2222", "-i /keys/sandbox", "bob-dev@198.51.100.7"} {
		if !strings.Contains(line, want) {
			t.Fatalf("the command is missing %q: %q", want, line)
		}
	}
	if strings.Contains(line, "203.0.113.10") {
		t.Fatalf("it reached the wrong machine: %q", line)
	}
}

func TestTwoWorkspacesReachDifferentMachines(t *testing.T) {
	dir := configWith(t, twoMachineConfig)

	got := captureInteractive(t)
	if _, err := execute(t, "--config", dir, "ssh", "alice"); err != nil {
		t.Fatalf("ssh alice returned %v", err)
	}
	alice := strings.Join(*got, " ")

	got = captureInteractive(t)
	if _, err := execute(t, "--config", dir, "ssh", "bob"); err != nil {
		t.Fatalf("ssh bob returned %v", err)
	}
	bob := strings.Join(*got, " ")

	if !strings.Contains(alice, "alice@203.0.113.10") {
		t.Fatalf("alice did not land on main: %q", alice)
	}
	if !strings.Contains(bob, "bob-dev@198.51.100.7") {
		t.Fatalf("bob did not land on sandbox: %q", bob)
	}
}

func TestSSHWithNoWorkspaceNeedsAMachineWhenThereAreSeveral(t *testing.T) {
	got := captureInteractive(t)
	dir := configWith(t, twoMachineConfig)

	_, err := execute(t, "--config", dir, "ssh")
	if err == nil {
		t.Fatal("expected an error rather than a guess between two machines")
	}
	if !strings.Contains(err.Error(), "--machine") {
		t.Fatalf("the error does not say how to choose: %v", err)
	}
	if len(*got) != 0 {
		t.Fatalf("it opened a session on a machine nobody named: %v", *got)
	}
}

func TestSSHWithNoWorkspaceUsesTheMachineFlag(t *testing.T) {
	got := captureInteractive(t)
	dir := configWith(t, twoMachineConfig)

	if _, err := execute(t, "--config", dir, "--machine", "sandbox", "ssh"); err != nil {
		t.Fatalf("ssh returned %v", err)
	}

	line := strings.Join(*got, " ")
	if !strings.Contains(line, "root@198.51.100.7") {
		t.Fatalf("it did not use the named machine's admin login: %q", line)
	}
}

func TestAnUnknownWorkspaceListsTheOnesThatExist(t *testing.T) {
	got := captureInteractive(t)
	dir := configWith(t, twoMachineConfig)

	_, err := execute(t, "--config", dir, "ssh", "carol")
	if err == nil {
		t.Fatal("expected an error for an unknown workspace")
	}
	for _, want := range []string{"alice", "bob"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the error does not list %q: %v", want, err)
		}
	}
	if len(*got) != 0 {
		t.Fatalf("it opened a session for a workspace that does not exist: %v", *got)
	}
}

func TestAWorkspaceUsesItsNameAsTheLinuxAccount(t *testing.T) {
	got := captureInteractive(t)
	dir := configWith(t, twoMachineConfig)

	if _, err := execute(t, "--config", dir, "ssh", "alice"); err != nil {
		t.Fatalf("ssh returned %v", err)
	}

	if line := strings.Join(*got, " "); !strings.Contains(line, "alice@") {
		t.Fatalf("the workspace name was not used as the account: %q", line)
	}
}

// fakeRemote answers one command, so a test can drive `run` without a machine.
type fakeRemote struct {
	out string
	err error
}

func (f fakeRemote) Run(context.Context, string) (string, error) { return f.out, f.err }

func (f fakeRemote) Stream(_ context.Context, _ string, stdout, _ io.Writer) error {
	if _, err := io.WriteString(stdout, f.out); err != nil {
		return err
	}
	return f.err
}

func (f fakeRemote) RunInput(ctx context.Context, command string, _ io.Reader) (string, error) {
	return f.Run(ctx, command)
}

func (f fakeRemote) Upload(context.Context, string, io.Reader) error { return nil }

func (f fakeRemote) Close() error { return nil }

// dialing answers every dial with this client, and reports nothing was reached
// for real. `run` dials through dialMux rather than dial, so both are stubbed.
func dialing(t *testing.T, client remote.Client) {
	t.Helper()
	dial = func(context.Context, config.Machine, string) (remote.Client, string, error) {
		return client, "203.0.113.10", nil
	}
	dialMux = func(context.Context, config.Machine, string) (remote.Client, string, error) {
		return client, "203.0.113.10", nil
	}
	t.Cleanup(func() { dial = remote.Dial; dialMux = remote.DialMux })
}

func historyLines(t *testing.T, dir string) []string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(dir, history.FileName))
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(body)), "\n")
}

func TestRunDialsThroughTheMultiplexedClientNotTheProgrammaticOne(t *testing.T) {
	dialing(t, fakeRemote{out: "ok\n"})
	dial = func(context.Context, config.Machine, string) (remote.Client, string, error) {
		return nil, "", errors.New("run must not dial through the programmatic client")
	}
	dir := configWith(t, twoMachineConfig)

	out, err := execute(t, "--config", dir, "--machine", "sandbox", "run", "true")
	if err != nil {
		t.Fatalf("run returned %v", err)
	}
	if !strings.Contains(out, "ok") {
		t.Fatalf("got %q", out)
	}
}

func TestRunPackageDialsThroughTheMultiplexedClientNotTheProgrammaticOne(t *testing.T) {
	dir := configDirWithProvider(t, "cloudflare", `print("hello from the package")`)
	dialLocal(t, dir)
	dial = func(context.Context, config.Machine, string) (remote.Client, string, error) {
		return nil, "", errors.New("run --package must not dial through the programmatic client")
	}

	out, err := execute(t, "--config", dir, "run", "--package", "cloudflare", "--", "zones")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "hello from the package") {
		t.Fatalf("got %q", out)
	}
}

func TestRunRecordsTheCommandAndTheWorkspaceItRanIn(t *testing.T) {
	dialing(t, fakeRemote{out: "CONTAINER ID\n"})
	dir := configWith(t, twoMachineConfig)

	if _, err := execute(t, "--config", dir, "run", "docker ps", "--workspace", "alice"); err != nil {
		t.Fatalf("run returned %v", err)
	}

	line := historyLines(t, dir)[0]
	for _, want := range []string{"workspace alice", "docker ps", "ok"} {
		if !strings.Contains(line, want) {
			t.Fatalf("the log line leaves out %q: %q", want, line)
		}
	}
}

func TestRunRecordsAFailureAgainstTheMachineItRanOn(t *testing.T) {
	dialing(t, fakeRemote{err: errors.New("exit status 1")})
	dir := configWith(t, twoMachineConfig)

	if _, err := execute(t, "--config", dir, "--machine", "sandbox", "run", "false"); err == nil {
		t.Fatal("expected the command's own failure")
	}

	line := historyLines(t, dir)[0]
	for _, want := range []string{"machine sandbox", "false", "failed"} {
		if !strings.Contains(line, want) {
			t.Fatalf("the log line leaves out %q: %q", want, line)
		}
	}
}

func TestRunPackageReachesTheEntrypoint(t *testing.T) {
	dir := configDirWithProvider(t, "cloudflare", `print("hello from the package")`)
	dialLocal(t, dir)

	out, err := execute(t, "--config", dir, "run", "--package", "cloudflare", "--", "zones")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "hello from the package") {
		t.Fatalf("got %q", out)
	}
}

func TestRunPackageWithWorkspaceDialsAsTheWorkspacesAccount(t *testing.T) {
	dir := configWith(t, "machines:\n  - name: main\n    hosts: [203.0.113.10]\n"+
		"workspaces:\n  - name: alice\n    machine: main\n")
	writeDNSPackage(t, dir, "cloudflare", nil, `import sys; print("saw " + " ".join(sys.argv[1:]))`)
	lockOnto(t, dir, "main", "cloudflare")
	calls := dialLocalCapturing(t, dir)

	out, err := execute(t, "--config", dir, "run", "--package", "cloudflare", "--workspace", "alice", "--", "zones", "list")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "saw zones list") {
		t.Fatalf("the entrypoint did not get the args after --: %q", out)
	}

	if len(*calls) != 1 {
		t.Fatalf("expected one dial, got %#v", *calls)
	}
	if got := (*calls)[0]; got.machine != "main" || got.user != "alice" {
		t.Fatalf("dialed %#v, want main as alice", got)
	}
}

func TestRunPackageFindsOnePackageInstalledForTheWorkspace(t *testing.T) {
	dir := configWith(t, "machines:\n  - name: main\n    hosts: [203.0.113.10]\n"+
		"workspaces:\n  - name: alice\n    machine: main\n")
	writeDNSPackage(t, dir, "cloudflare", nil, `print("hello from the workspace package")`)
	lockOntoWorkspace(t, dir, "alice", "cloudflare")
	dialLocal(t, dir)

	out, err := execute(t, "--config", dir, "run", "--package", "cloudflare", "--workspace", "alice", "--", "zones")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "hello from the workspace package") {
		t.Fatalf("got %q", out)
	}
}

func TestRunPackageWithoutWorkspaceDialsAsTheMachinesAdmin(t *testing.T) {
	dir := configDirWithProvider(t, "cloudflare", `print("hello from the package")`)
	calls := dialLocalCapturing(t, dir)

	if _, err := execute(t, "--config", dir, "run", "--package", "cloudflare", "--", "zones"); err != nil {
		t.Fatal(err)
	}

	if len(*calls) != 1 {
		t.Fatalf("expected one dial, got %#v", *calls)
	}
	if got := (*calls)[0]; got.machine != "main" || got.user != "" {
		t.Fatalf("dialed %#v, want main as the admin (empty user)", got)
	}
}

func TestRunPackageWithAnUnknownWorkspaceErrorsWithoutDialing(t *testing.T) {
	dialed := false
	orig := dialMux
	dialMux = func(context.Context, config.Machine, string) (remote.Client, string, error) {
		dialed = true
		return nil, "", nil
	}
	t.Cleanup(func() { dialMux = orig })

	dir := configWith(t, "machines:\n  - name: main\n    hosts: [203.0.113.10]\n"+
		"workspaces:\n  - name: alice\n    machine: main\n")

	_, err := execute(t, "--config", dir, "run", "--package", "cloudflare", "--workspace", "carol", "--", "zones")
	if err == nil {
		t.Fatal("expected an error for an unknown workspace")
	}
	if dialed {
		t.Fatal("it dialed before resolving the workspace")
	}
}

func TestRunPackageRefusesAnUndeclaredCommand(t *testing.T) {
	dialing(t, fakeRemote{})
	dir := configDirWithProviderAccepting(t, "cloudflare", []string{"zones", "list"})

	_, err := execute(t, "--config", dir, "run", "--package", "cloudflare", "--", "drop-everything")
	if err == nil {
		t.Fatal("an undeclared command ran")
	}
	if !strings.Contains(err.Error(), "zones, list") {
		t.Fatalf("the error does not say what it accepts: %v", err)
	}
}

func TestExecCommandContextKillsTheChildItStarted(t *testing.T) {
	// `tunnel` execs ssh and holds it. Stopping devmachine without killing
	// that ssh leaves a port forwarded into the machine with nothing on this
	// computer saying so — found running an hour after the command that
	// opened it had gone.
	ctx, cancel := context.WithCancel(t.Context())

	started := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		close(started)
		done <- execCommandContext(ctx, "sleep", "60")
	}()

	<-started
	time.Sleep(200 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("a cancelled session reported a failure: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the child outlived the context")
	}
}

func TestRunExplainsARefusedHostKey(t *testing.T) {
	dialing(t, fakeRemote{err: fmt.Errorf("machine %q: %w", "main", remote.ErrHostKeyRejected)})
	verifySystemHost = func(context.Context, config.Machine) error {
		return fmt.Errorf("%w for machine %q: run `devmachine machines trust main --replace`", remote.ErrHostKeyChanged, "main")
	}
	t.Cleanup(func() { verifySystemHost = realVerifySystemHost })
	dir := configWith(t, twoMachineConfig)

	_, err := execute(t, "--config", dir, "--machine", "main", "run", "true")
	if !errors.Is(err, remote.ErrHostKeyChanged) || !strings.Contains(err.Error(), "--replace") {
		t.Fatalf("got %v, want the explained host key change", err)
	}
}

func TestRunPackageExplainsARefusedHostKey(t *testing.T) {
	dir := configDirWithProvider(t, "cloudflare", `print("unused")`)
	dialing(t, fakeRemote{err: fmt.Errorf("machine %q: %w", "main", remote.ErrHostKeyRejected)})
	verifySystemHost = func(context.Context, config.Machine) error {
		return fmt.Errorf("%w for machine %q: run `devmachine machines trust main --replace`", remote.ErrHostKeyChanged, "main")
	}
	t.Cleanup(func() { verifySystemHost = realVerifySystemHost })

	_, err := execute(t, "--config", dir, "run", "--package", "cloudflare", "--", "zones")
	if !errors.Is(err, remote.ErrHostKeyChanged) {
		t.Fatalf("got %v, want the explained host key change", err)
	}
}

type recordingAdmin struct{ nopClient }

var adminCommands []string

func (recordingAdmin) Run(_ context.Context, command string) (string, error) {
	adminCommands = append(adminCommands, command)
	return "", nil
}

// TestDialAdminRunsEverythingAsRoot: an admin that is not root reaches root
// through sudo for every command on an admin connection.
func TestDialAdminRunsEverythingAsRoot(t *testing.T) {
	t.Cleanup(swap(&elevate, remote.Elevated))
	t.Cleanup(swap(&dial, func(context.Context, config.Machine, string) (remote.Client, string, error) {
		return recordingAdmin{}, "203.0.113.10", nil
	}))
	adminCommands = nil

	client, _, err := dialAdmin(context.Background(), config.Machine{Name: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Run(context.Background(), "userdel bob"); err != nil {
		t.Fatal(err)
	}
	if len(adminCommands) != 1 || adminCommands[0] != remote.AsRoot("userdel bob") {
		t.Fatalf("got %q", adminCommands)
	}
}

type exitCodeError int

func (e exitCodeError) Error() string { return fmt.Sprintf("exit status %d", int(e)) }

func (e exitCodeError) ExitCode() int { return int(e) }

func TestSSHToAMacThatRefusesTheLoginPointsAtRemoteLogin(t *testing.T) {
	captureInteractive(t)
	runInteractive = func(string, ...string) error { return exitCodeError(255) }
	dir := configWith(t, twoMachineConfig)
	saveFacts(t, dir, "sandbox", observedMac)

	_, err := execute(t, "--config", dir, "ssh", "bob")
	if err == nil {
		t.Fatal("ssh succeeded")
	}
	for _, want := range []string{"Remote Login", "bob-dev", "devmachine doctor"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the error does not say %q: %v", want, err)
		}
	}
}

func TestSSHToAMachineNotKnownAsAMacDoesNotGuessAtRemoteLogin(t *testing.T) {
	captureInteractive(t)
	runInteractive = func(string, ...string) error { return exitCodeError(255) }
	dir := configWith(t, twoMachineConfig)

	_, err := execute(t, "--config", dir, "ssh", "bob")
	if err == nil || strings.Contains(err.Error(), "Remote Login") {
		t.Fatalf("got %v", err)
	}
}

func TestSSHToAMacThatFailsForAnotherReasonSaysNothingAboutRemoteLogin(t *testing.T) {
	captureInteractive(t)
	runInteractive = func(string, ...string) error { return exitCodeError(1) }
	dir := configWith(t, twoMachineConfig)
	saveFacts(t, dir, "sandbox", observedMac)

	_, err := execute(t, "--config", dir, "ssh", "bob")
	if err == nil || strings.Contains(err.Error(), "Remote Login") {
		t.Fatalf("got %v", err)
	}
}
