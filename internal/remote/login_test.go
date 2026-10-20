package remote

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// fakeLoginSSH stands in for ssh: `-O check` answers with checkExit, and a
// login runs loginBody. Every login appends a line to the returned file.
func fakeLoginSSH(t *testing.T, checkExit int, loginBody string) (*muxClient, string) {
	t.Helper()
	dir := t.TempDir()
	logins := filepath.Join(dir, "logins")
	script := "#!/bin/sh\n" +
		"for a in \"$@\"; do if [ \"$a\" = \"-O\" ]; then exit " + strconv.Itoa(checkExit) + "; fi; done\n" +
		"echo login >> " + logins + "\n" + loginBody + "\n"
	fakeSSH := filepath.Join(dir, "ssh")
	if err := os.WriteFile(fakeSSH, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	m := muxTestMachine(t)
	fakeCacheDir(t)
	client, _, err := DialMux(context.Background(), m, "")
	if err != nil {
		t.Fatalf("DialMux returned %v", err)
	}
	mux := client.(*muxClient)
	mux.sshPath = fakeSSH
	t.Cleanup(func() { killPendingLogins(t, mux) })
	return mux, logins
}

func killPendingLogins(t *testing.T, c *muxClient) {
	t.Helper()
	for _, address := range c.addresses {
		if pid, _, err := readPendingLogin(loginStatePath(c.controlPath(address))); err == nil {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	}
}

func loginCount(t *testing.T, logins string) int {
	t.Helper()
	data, err := os.ReadFile(logins)
	if errors.Is(err, os.ErrNotExist) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Count(string(data), "login")
}

const waitingForTheAgent = "echo 'debug1: Connection established.' >&2\n" +
	"echo 'debug1: Server accepts key: alice ED25519 SHA256:abc agent' >&2\n" +
	"sleep 60\n"

func TestLoginWithALiveSharedConnectionStartsNoLogin(t *testing.T) {
	c, logins := fakeLoginSSH(t, 0, "exit 0")
	if err := c.Login(context.Background()); err != nil {
		t.Fatalf("Login returned %v", err)
	}
	if n := loginCount(t, logins); n != 0 {
		t.Fatalf("logins = %d, want 0", n)
	}
}

func TestLoginOpensTheSharedConnection(t *testing.T) {
	c, logins := fakeLoginSSH(t, 1, "exit 0")
	if err := c.Login(context.Background()); err != nil {
		t.Fatalf("Login returned %v", err)
	}
	if n := loginCount(t, logins); n != 1 {
		t.Fatalf("logins = %d, want 1", n)
	}
}

func TestLoginSaysTheAgentIsWaitingAndKeepsTheLoginOpen(t *testing.T) {
	c, logins := fakeLoginSSH(t, 1, waitingForTheAgent)
	start := time.Now()
	err := c.Login(context.Background())
	if !errors.Is(err, ErrLoginWaiting) {
		t.Fatalf("Login returned %v, want ErrLoginWaiting", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("Login took %v; it should answer as soon as the key is accepted", elapsed)
	}
	pid, _, err := readPendingLogin(loginStatePath(c.controlPath(c.addresses[0])))
	if err != nil {
		t.Fatalf("no pending login recorded: %v", err)
	}
	if err := syscall.Kill(pid, 0); err != nil {
		t.Fatalf("the waiting login is not running: %v", err)
	}
	if n := loginCount(t, logins); n != 1 {
		t.Fatalf("logins = %d, want 1", n)
	}
}

func TestLoginWhileOneWaitsStartsNoOther(t *testing.T) {
	c, logins := fakeLoginSSH(t, 1, waitingForTheAgent)
	if err := c.Login(context.Background()); !errors.Is(err, ErrLoginWaiting) {
		t.Fatalf("first Login returned %v", err)
	}
	if err := c.Login(context.Background()); !errors.Is(err, ErrLoginWaiting) {
		t.Fatalf("second Login returned %v", err)
	}
	if n := loginCount(t, logins); n != 1 {
		t.Fatalf("logins = %d, want 1: a second login would ask the agent again and knock on the machine again", n)
	}
}

func TestLoginRefusedIsAuthRefused(t *testing.T) {
	c, _ := fakeLoginSSH(t, 1,
		"echo 'debug1: Connection established.' >&2\n"+
			"echo 'alice@203.0.113.10: Permission denied (publickey).' >&2\nexit 255")
	if err := c.Login(context.Background()); !errors.Is(err, ErrAuthRefused) {
		t.Fatalf("Login returned %v, want ErrAuthRefused", err)
	}
}

func TestLoginToNoAddressSaysNoAddressAnswered(t *testing.T) {
	c, _ := fakeLoginSSH(t, 1,
		"echo 'ssh: connect to host 203.0.113.10 port 2222: Operation timed out' >&2\nexit 255")
	err := c.Login(context.Background())
	if err == nil || !strings.Contains(err.Error(), "no address answered") {
		t.Fatalf("Login returned %v, want no address answered", err)
	}
}

func TestLoginReportsAWaitingLoginTheAgentRefusedOnce(t *testing.T) {
	c, logins := fakeLoginSSH(t, 1, waitingForTheAgent)
	if err := c.Login(context.Background()); !errors.Is(err, ErrLoginWaiting) {
		t.Fatalf("first Login returned %v", err)
	}
	state := loginStatePath(c.controlPath(c.addresses[0]))
	pid, _, err := readPendingLogin(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(loginLogPath(state),
		[]byte("debug1: Server accepts key: alice\nsign_and_send_pubkey: signing failed for ED25519 \"alice\" from agent: agent refused operation\nalice@203.0.113.10: Permission denied (publickey).\n"),
		0o600); err != nil {
		t.Fatal(err)
	}
	_ = syscall.Kill(pid, syscall.SIGKILL)
	waitGone(t, pid)

	if err := c.Login(context.Background()); !errors.Is(err, ErrAuthRefused) {
		t.Fatalf("Login after the refusal returned %v, want ErrAuthRefused", err)
	}
	if n := loginCount(t, logins); n != 1 {
		t.Fatalf("logins = %d, want 1: the refusal is the answer, not a reason to ask again", n)
	}
}

func TestLoginEndsAWaitingLoginLeftTooLong(t *testing.T) {
	c, logins := fakeLoginSSH(t, 1, waitingForTheAgent)
	if err := c.Login(context.Background()); !errors.Is(err, ErrLoginWaiting) {
		t.Fatalf("first Login returned %v", err)
	}
	state := loginStatePath(c.controlPath(c.addresses[0]))
	pid, _, err := readPendingLogin(state)
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-loginLife - time.Minute)
	if err := writePendingLogin(state, pid, old); err != nil {
		t.Fatal(err)
	}

	if err := c.Login(context.Background()); !errors.Is(err, ErrLoginWaiting) {
		t.Fatalf("Login after the old one returned %v", err)
	}
	waitGone(t, pid)
	if n := loginCount(t, logins); n != 2 {
		t.Fatalf("logins = %d, want 2", n)
	}
}

func TestLoginIgnoresAPendingRecordWhoseProcessIsSomethingElse(t *testing.T) {
	c, logins := fakeLoginSSH(t, 1, "exit 0")
	other := exec.Command("sleep", "60")
	if err := other.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = other.Process.Kill(); _ = other.Wait() })
	state := loginStatePath(c.controlPath(c.addresses[0]))
	for _, started := range []time.Time{time.Now(), time.Now().Add(-loginLife - time.Minute)} {
		if err := writePendingLogin(state, other.Process.Pid, started); err != nil {
			t.Fatal(err)
		}
		if err := c.Login(context.Background()); err != nil {
			t.Fatalf("Login returned %v; the recorded process is not a login", err)
		}
		if err := syscall.Kill(other.Process.Pid, 0); err != nil {
			t.Fatalf("Login ended a process that was not its login: %v", err)
		}
	}
	if n := loginCount(t, logins); n != 2 {
		t.Fatalf("logins = %d, want 2", n)
	}
}

func waitGone(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if syscall.Kill(pid, 0) != nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("process %d is still running", pid)
}
