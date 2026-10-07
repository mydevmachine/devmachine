package remote

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mydevmachine/devmachine/internal/config"
)

func muxTestMachine(t *testing.T) config.Machine {
	t.Helper()
	m := config.Machine{
		Name:           "main",
		Hosts:          []config.Host{{Address: "203.0.113.10"}},
		Port:           2222,
		Key:            throwawayKey(t),
		KnownHostsFile: filepath.Join(t.TempDir(), "known_hosts"),
	}
	trustMachine(t, m, newHostKey(t).PublicKey())
	return m
}

// fakeCacheDir is short on purpose: t.TempDir() on macOS is long enough that
// the sockets would move to the /tmp fallback.
func fakeCacheDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "dmc")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	orig := userCacheDir
	userCacheDir = func() (string, error) { return dir, nil }
	t.Cleanup(func() { userCacheDir = orig })
	return dir
}

func TestDialMuxBuildsTheSameHostKeyPinningAsTheSystemSSHBuilder(t *testing.T) {
	cache := fakeCacheDir(t)
	m := muxTestMachine(t)

	client, address, err := DialMux(context.Background(), m, "")
	if err != nil {
		t.Fatalf("DialMux returned %v", err)
	}
	if address != m.Hosts[0].Address {
		t.Fatalf("address = %q, want %q", address, m.Hosts[0].Address)
	}
	mux, ok := client.(*muxClient)
	if !ok {
		t.Fatalf("got %T, want *muxClient", client)
	}

	want := StrictSSHArgs(m)
	joined := strings.Join(mux.args, " ")
	for _, arg := range want {
		if !strings.Contains(joined, arg) {
			t.Fatalf("missing pinning option %q in %#v", arg, mux.args)
		}
	}

	argsFile := filepath.Join(t.TempDir(), "args")
	fakeSSH := filepath.Join(t.TempDir(), "ssh")
	if err := os.WriteFile(fakeSSH, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > "+argsFile+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	mux.sshPath = fakeSSH
	if _, err := client.Run(context.Background(), "true"); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	controlDir := filepath.Join(cache, "devmachine", "cm")
	for _, want := range []string{
		"ControlMaster=auto",
		"ControlPath=" + controlSocketPath(controlDir, m.User, m.Hosts[0].Address, m.Port),
		"ControlPersist=5m",
		"BatchMode=yes",
	} {
		if !strings.Contains(string(body), want+"\n") {
			t.Fatalf("missing multiplexing option %q in %s", want, body)
		}
	}
}

func TestDialMuxMovesTheSocketsToAShortDirectoryUnderAVeryLongHome(t *testing.T) {
	long := filepath.Join(t.TempDir(), strings.Repeat("a", 100))
	orig := userCacheDir
	userCacheDir = func() (string, error) { return long, nil }
	t.Cleanup(func() { userCacheDir = orig })

	dir, err := controlPathDir()
	if err != nil {
		t.Fatal(err)
	}
	if want := "/tmp/dm-" + strconv.Itoa(os.Getuid()); dir != want {
		t.Fatalf("dir = %q, want %q", dir, want)
	}
	info, err := os.Lstat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Fatalf("mode = %o, want 0700", perm)
	}
}

func TestMuxClientExplainsASocketPathTooLong(t *testing.T) {
	fakeCacheDir(t)
	m := muxTestMachine(t)
	fakeSSH := filepath.Join(t.TempDir(), "ssh")
	script := "#!/bin/sh\necho 'unix_listener: path \"/x/cm/abc.CCjnMgl2FZ5Geq6z\" too long for Unix domain socket' >&2\nexit 255\n"
	if err := os.WriteFile(fakeSSH, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	origLookPath := lookPath
	lookPath = func(string) (string, error) { return fakeSSH, nil }
	t.Cleanup(func() { lookPath = origLookPath })

	client, _, err := DialMux(context.Background(), m, "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Run(context.Background(), "true")
	if err == nil || !strings.Contains(err.Error(), "longer than this system allows for a socket") {
		t.Fatalf("got %v, want an error that says what the long path means", err)
	}
}

func TestDialMuxCreatesTheControlPathDirectoryMode0700(t *testing.T) {
	cache := fakeCacheDir(t)
	m := muxTestMachine(t)

	if _, _, err := DialMux(context.Background(), m, ""); err != nil {
		t.Fatalf("DialMux returned %v", err)
	}

	info, err := os.Stat(filepath.Join(cache, "devmachine", "cm"))
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() {
		t.Fatal("the control path is not a directory")
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Fatalf("mode = %o, want 0700", perm)
	}
}

func TestDialMuxFallsBackToDialWhenSSHIsMissing(t *testing.T) {
	fakeCacheDir(t)
	m := muxTestMachine(t)

	origLookPath := lookPath
	lookPath = func(string) (string, error) { return "", errors.New("not found") }
	t.Cleanup(func() { lookPath = origLookPath })

	origFallback := dialFallback
	called := false
	dialFallback = func(_ context.Context, gotMachine config.Machine, gotUser string) (Client, string, error) {
		called = true
		if gotMachine.Name != m.Name || gotUser != "alice" {
			t.Fatalf("fallback got (%q, %q)", gotMachine.Name, gotUser)
		}
		return &localClient{}, "fell back", nil
	}
	t.Cleanup(func() { dialFallback = origFallback })

	client, address, err := DialMux(context.Background(), m, "alice")
	if err != nil {
		t.Fatalf("DialMux returned %v", err)
	}
	if !called {
		t.Fatal("DialMux did not fall back to Dial when ssh is missing")
	}
	if address != "fell back" {
		t.Fatalf("address = %q", address)
	}
	if _, ok := client.(*localClient); !ok {
		t.Fatalf("got %T, want the fallback's client", client)
	}
}

func TestDialMuxKeepsUsingTheLocalClientForASelfMachine(t *testing.T) {
	fakeCacheDir(t)
	m := config.Machine{Name: "mac", Self: true}

	client, address, err := DialMux(context.Background(), m, "")
	if err != nil {
		t.Fatalf("DialMux returned %v", err)
	}
	if address != SelfAddress {
		t.Fatalf("address = %q, want %q", address, SelfAddress)
	}
	if _, ok := client.(*localClient); !ok {
		t.Fatalf("got %T, want *localClient", client)
	}
}

// fakeUnreachableSSH is a script that always exits 255: OpenSSH's own code
// for "never reached the machine", which is what tells muxClient.exec to try
// the next address instead of reporting the remote command's own failure.
func fakeUnreachableSSH(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ssh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho 'ssh: connect to host 203.0.113.10 port 22: Connection refused' >&2\nexit 255\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestMuxClientRunReturnsAnErrorTheSameShapeAsTheProgrammaticClient(t *testing.T) {
	fakeCacheDir(t)
	m := muxTestMachine(t)
	fakeSSH := fakeUnreachableSSH(t)

	origLookPath := lookPath
	lookPath = func(name string) (string, error) {
		if name == "ssh" {
			return fakeSSH, nil
		}
		return "", errors.New("not found")
	}
	t.Cleanup(func() { lookPath = origLookPath })

	client, _, err := DialMux(context.Background(), m, "")
	if err != nil {
		t.Fatalf("DialMux returned %v", err)
	}

	_, err = client.Run(context.Background(), "true")
	if err == nil {
		t.Fatal("expected an error: no address can ever answer in this test")
	}
	if !strings.Contains(err.Error(), m.Name) {
		t.Fatalf("error does not name the machine: %v", err)
	}
}

// TestMuxClientRunSaysWhyTheCommandFailed: a script explains a refusal on
// stderr, and that sentence is the error somebody can act on.
func TestMuxClientRunSaysWhyTheCommandFailed(t *testing.T) {
	fakeCacheDir(t)
	m := muxTestMachine(t)
	fakeSSH := filepath.Join(t.TempDir(), "ssh")
	if err := os.WriteFile(fakeSSH, []byte("#!/bin/sh\necho 'escape/leak.env reaches outside the home' >&2\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}

	origLookPath := lookPath
	lookPath = func(name string) (string, error) {
		if name == "ssh" {
			return fakeSSH, nil
		}
		return "", errors.New("not found")
	}
	t.Cleanup(func() { lookPath = origLookPath })

	client, _, err := DialMux(context.Background(), m, "")
	if err != nil {
		t.Fatalf("DialMux returned %v", err)
	}
	for name, run := range map[string]func() error{
		"Run": func() error { _, err := client.Run(context.Background(), "true"); return err },
		"RunInput": func() error {
			_, err := client.RunInput(context.Background(), "true", strings.NewReader(""))
			return err
		},
	} {
		err := run()
		if err == nil || !strings.Contains(err.Error(), "escape/leak.env reaches outside the home") {
			t.Fatalf("%s: the reason is missing: %v", name, err)
		}
	}
}

// TestDialMuxReusesTheControlSocket runs `run` twice against the disposable
// Lima VM (see scripts/fake-vps.sh) and proves the second call finds the
// first one's control socket still alive, which is the whole point of
// DialMux: `ssh -O check` against the very same ControlPath only succeeds
// when a master connection from an earlier process is still there.
func TestDialMuxReusesTheControlSocket(t *testing.T) {
	// The real cache directory, not a t.TempDir(): macOS temp paths run long,
	// and choosing a directory that fits the Unix socket limit is part of
	// what this proves.
	m := testMachine(t)

	client, _, err := DialMux(context.Background(), m, "")
	if err != nil {
		t.Fatalf("DialMux returned %v", err)
	}
	if _, err := client.Run(context.Background(), "true"); err != nil {
		t.Fatalf("the first run returned %v", err)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("Close returned %v", err)
	}

	client2, _, err := DialMux(context.Background(), m, "")
	if err != nil {
		t.Fatalf("DialMux returned %v", err)
	}
	if _, err := client2.Run(context.Background(), "true"); err != nil {
		t.Fatalf("the second run returned %v", err)
	}
	t.Cleanup(func() { _ = client2.Close() })

	controlDir, err := controlPathDir()
	if err != nil {
		t.Fatal(err)
	}
	user := m.User
	args := append(StrictSSHArgs(m),
		"-o", "ControlPath="+controlSocketPath(controlDir, user, m.Hosts[0].Address, m.Port),
		"-O", "check", user+"@"+m.Hosts[0].Address)
	check := exec.CommandContext(context.Background(), "ssh", args...)
	out, err := check.CombinedOutput()
	if err != nil {
		t.Fatalf("ssh -O check found no live master: %v: %s", err, out)
	}
}

// TestDialMuxConnectsUnderAVeryLongHome is the bug a long home directory hit:
// ssh refused to create the master socket because its path was over the
// Unix socket limit, so no address ever answered.
func TestDialMuxConnectsUnderAVeryLongHome(t *testing.T) {
	m := testMachine(t)
	long := filepath.Join(t.TempDir(), strings.Repeat("a", 60), "Library", "Caches")
	orig := userCacheDir
	userCacheDir = func() (string, error) { return long, nil }
	t.Cleanup(func() { userCacheDir = orig })

	client, _, err := DialMux(context.Background(), m, "")
	if err != nil {
		t.Fatalf("DialMux returned %v", err)
	}
	if _, err := client.Run(context.Background(), "true"); err != nil {
		t.Fatalf("run under a long home returned %v", err)
	}

	controlDir, err := controlPathDir()
	if err != nil {
		t.Fatal(err)
	}
	socket := controlSocketPath(controlDir, m.User, m.Hosts[0].Address, m.Port)
	t.Cleanup(func() {
		_ = exec.Command("ssh", "-o", "ControlPath="+socket, "-O", "exit", m.User+"@"+m.Hosts[0].Address).Run()
	})
	args := append(StrictSSHArgs(m), "-o", "ControlPath="+socket, "-O", "check", m.User+"@"+m.Hosts[0].Address)
	if out, err := exec.CommandContext(context.Background(), "ssh", args...).CombinedOutput(); err != nil {
		t.Fatalf("ssh -O check found no live master at %s: %v: %s", socket, err, out)
	}
}

func TestMuxClientUploadReturnsAClearError(t *testing.T) {
	fakeCacheDir(t)
	m := muxTestMachine(t)

	client, _, err := DialMux(context.Background(), m, "")
	if err != nil {
		t.Fatalf("DialMux returned %v", err)
	}

	if err := client.Upload(context.Background(), "/tmp/x", strings.NewReader("")); err == nil {
		t.Fatal("expected Upload to refuse")
	}
}

func TestMuxClientStopsAtARefusedHostKeyAndSaysSo(t *testing.T) {
	fakeCacheDir(t)
	m := muxTestMachine(t)
	m.Hosts = append(m.Hosts, config.Host{Address: "203.0.113.11"})
	calls := filepath.Join(t.TempDir(), "calls")
	fakeSSH := filepath.Join(t.TempDir(), "ssh")
	script := "#!/bin/sh\necho x >> " + calls + "\necho 'Host key verification failed.' >&2\nexit 255\n"
	if err := os.WriteFile(fakeSSH, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	origLookPath := lookPath
	lookPath = func(string) (string, error) { return fakeSSH, nil }
	t.Cleanup(func() { lookPath = origLookPath })

	client, _, err := DialMux(context.Background(), m, "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Run(context.Background(), "true")
	if !errors.Is(err, ErrHostKeyRejected) {
		t.Fatalf("got %v, want ErrHostKeyRejected", err)
	}
	body, err := os.ReadFile(calls)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(body), "x"); n != 1 {
		t.Fatalf("ssh ran %d times, want 1: a refused key must not fall through to the next address", n)
	}
}

func TestMuxClientDoesNotRetryAnotherAddressAfterSendingPartOfTheInput(t *testing.T) {
	fakeCacheDir(t)
	m := muxTestMachine(t)
	m.Hosts = append(m.Hosts, config.Host{Address: "203.0.113.11"})
	calls := filepath.Join(t.TempDir(), "calls")
	fakeSSH := filepath.Join(t.TempDir(), "ssh")
	script := "#!/bin/sh\necho x >> " + calls + "\nhead -c 4 >/dev/null\nexit 255\n"
	if err := os.WriteFile(fakeSSH, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	origLookPath := lookPath
	lookPath = func(string) (string, error) { return fakeSSH, nil }
	t.Cleanup(func() { lookPath = origLookPath })

	client, _, err := DialMux(context.Background(), m, "")
	if err != nil {
		t.Fatal(err)
	}
	runner, ok := client.(interface {
		RunInput(context.Context, string, io.Reader) (string, error)
	})
	if !ok {
		t.Fatal("the mux client takes no input")
	}
	_, err = runner.RunInput(context.Background(), "cat > f", strings.NewReader("0123456789"))
	if err == nil || !strings.Contains(err.Error(), "part of the input") {
		t.Fatalf("got %v, want an error saying part of the input was already sent", err)
	}
	body, err := os.ReadFile(calls)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(body), "x"); n != 1 {
		t.Fatalf("ssh ran %d times, want 1: a half-sent input must not go to the next address", n)
	}
}

func TestMuxClientDoesNotRetryAnotherAddressAfterReceivingPartOfTheOutput(t *testing.T) {
	fakeCacheDir(t)
	m := muxTestMachine(t)
	m.Hosts = append(m.Hosts, config.Host{Address: "203.0.113.11"})
	calls := filepath.Join(t.TempDir(), "calls")
	fakeSSH := filepath.Join(t.TempDir(), "ssh")
	script := "#!/bin/sh\necho x >> " + calls + "\nprintf abcd\nexit 255\n"
	if err := os.WriteFile(fakeSSH, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	origLookPath := lookPath
	lookPath = func(string) (string, error) { return fakeSSH, nil }
	t.Cleanup(func() { lookPath = origLookPath })

	client, _, err := DialMux(context.Background(), m, "")
	if err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	err = client.Stream(context.Background(), "cat f", &out, nil)
	if err == nil || !strings.Contains(err.Error(), "part of the output") {
		t.Fatalf("got %v, want an error saying part of the output already arrived", err)
	}
	if out.String() != "abcd" {
		t.Fatalf("output %q, want only the first address's bytes", out.String())
	}
	body, err := os.ReadFile(calls)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(body), "x"); n != 1 {
		t.Fatalf("ssh ran %d times, want 1: a half-received output must not be appended to by the next address", n)
	}
}

func countingSSH(t *testing.T, body string) (string, string) {
	t.Helper()
	calls := filepath.Join(t.TempDir(), "calls")
	fakeSSH := filepath.Join(t.TempDir(), "ssh")
	script := "#!/bin/sh\necho x >> " + calls + "\n" + body + "\n"
	if err := os.WriteFile(fakeSSH, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	origLookPath := lookPath
	lookPath = func(string) (string, error) { return fakeSSH, nil }
	t.Cleanup(func() { lookPath = origLookPath })
	return fakeSSH, calls
}

func sshRuns(t *testing.T, calls string) int {
	t.Helper()
	body, err := os.ReadFile(calls)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Count(string(body), "x")
}

func TestMuxClientTriesTheNextAddressWhenOneDoesNotConnect(t *testing.T) {
	for _, reason := range []string{
		"ssh: connect to host 203.0.113.10 port 22: Connection refused",
		"ssh: connect to host 203.0.113.10 port 22: Operation timed out",
		"ssh: Could not resolve hostname main.example.com: nodename nor servname provided",
		"Connection closed by 203.0.113.10 port 22",
	} {
		fakeCacheDir(t)
		m := muxTestMachine(t)
		m.Hosts = append(m.Hosts, config.Host{Address: "203.0.113.11"})
		_, calls := countingSSH(t, "echo '"+reason+"' >&2\nexit 255")

		client, _, err := DialMux(context.Background(), m, "")
		if err != nil {
			t.Fatal(err)
		}
		_, err = client.Run(context.Background(), "true")
		if err == nil || !strings.Contains(err.Error(), "no address answered") {
			t.Errorf("%s: got %v", reason, err)
		}
		if n := sshRuns(t, calls); n != 2 {
			t.Errorf("%s: ssh ran %d times, want 2", reason, n)
		}
	}
}

func TestMuxClientReturnsARemoteExit255AsTheCommandsOwn(t *testing.T) {
	fakeCacheDir(t)
	m := muxTestMachine(t)
	m.Hosts = append(m.Hosts, config.Host{Address: "203.0.113.11"})
	_, calls := countingSSH(t, "echo 'the check failed' >&2\nexit 255")

	client, _, err := DialMux(context.Background(), m, "")
	if err != nil {
		t.Fatal(err)
	}
	err = client.Stream(context.Background(), "exit 255", io.Discard, io.Discard)
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 255 {
		t.Fatalf("got %v, want the command's exit status 255", err)
	}
	if n := sshRuns(t, calls); n != 1 {
		t.Fatalf("ssh ran %d times, want 1: the command ran, and must not run again on another address", n)
	}
}
