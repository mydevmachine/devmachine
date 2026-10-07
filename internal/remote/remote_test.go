package remote

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mydevmachine/devmachine/internal/config"
	"github.com/mydevmachine/devmachine/internal/hostkeys"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// throwawayKey writes a private key nothing will ever authenticate with.
//
// A test about which addresses were tried must not depend on whether the
// machine running it happens to have an SSH agent. Without this it passes on a
// developer's laptop and fails on a runner, for a reason that has nothing to do
// with what it is checking.
func throwawayKey(t *testing.T) string {
	t.Helper()

	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(private, "")
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), "id_ed25519")
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestResolveKeepsALiteralAddress(t *testing.T) {
	m := config.Machine{Name: "main", Hosts: []config.Host{{Address: "203.0.113.10"}}}

	got, err := Resolve(m)
	if err != nil {
		t.Fatalf("Resolve returned %v", err)
	}
	if len(got) != 1 || got[0] != "203.0.113.10" {
		t.Fatalf("got %#v", got)
	}
}

func TestResolveKeepsTheConfiguredOrder(t *testing.T) {
	m := config.Machine{Name: "main", Hosts: []config.Host{
		{Address: "100.64.0.5"},
		{Address: "203.0.113.10"},
	}}

	got, _ := Resolve(m)
	if len(got) != 2 || got[0] != "100.64.0.5" || got[1] != "203.0.113.10" {
		t.Fatalf("the order was not preserved: %#v", got)
	}
}

func TestResolveDropsTailscaleWhenTheBinaryIsMissing(t *testing.T) {
	lookPath = func(string) (string, error) { return "", errors.New("not found") }
	t.Cleanup(func() { lookPath = realLookPath })

	m := config.Machine{Name: "main", Hosts: []config.Host{
		{Address: "tailscale:vps"},
		{Address: "203.0.113.10"},
	}}

	got, err := Resolve(m)
	if err != nil {
		t.Fatalf("Resolve returned %v", err)
	}
	if len(got) != 1 || got[0] != "203.0.113.10" {
		t.Fatalf("the tailscale entry was not dropped: %#v", got)
	}
}

func TestResolveExpandsTailscaleWhenItIsAvailable(t *testing.T) {
	lookPath = func(string) (string, error) { return "/usr/bin/tailscale", nil }
	tailscaleIP = func(string, string) (string, error) { return "100.64.0.5", nil }
	t.Cleanup(func() { lookPath = realLookPath; tailscaleIP = realTailscaleIP })

	m := config.Machine{Name: "main", Hosts: []config.Host{{Address: "tailscale:vps"}}}

	got, _ := Resolve(m)
	if len(got) != 1 || got[0] != "100.64.0.5" {
		t.Fatalf("got %#v", got)
	}
}

func TestResolveDropsTailscaleWhenThePeerIsUnknown(t *testing.T) {
	lookPath = func(string) (string, error) { return "/usr/bin/tailscale", nil }
	tailscaleIP = func(string, string) (string, error) { return "", errors.New("no such peer") }
	t.Cleanup(func() { lookPath = realLookPath; tailscaleIP = realTailscaleIP })

	m := config.Machine{Name: "main", Hosts: []config.Host{
		{Address: "tailscale:vps"},
		{Address: "203.0.113.10"},
	}}

	got, _ := Resolve(m)
	if len(got) != 1 || got[0] != "203.0.113.10" {
		t.Fatalf("got %#v", got)
	}
}

func TestResolveFailsWhenEveryAddressWasDropped(t *testing.T) {
	lookPath = func(string) (string, error) { return "", errors.New("not found") }
	t.Cleanup(func() { lookPath = realLookPath })

	m := config.Machine{Name: "main", Hosts: []config.Host{{Address: "tailscale:vps"}}}

	_, err := Resolve(m)
	if err == nil {
		t.Fatal("expected an error when nothing is left to try")
	}
	if !strings.Contains(err.Error(), "tailscale") {
		t.Fatalf("the error does not say what was dropped or why: %v", err)
	}
}

// testMachine describes the throwaway VPS from scripts/fake-vps.sh. Without it
// the integration tests skip, so `go test ./...` still works where there is no
// VM.
func testMachine(t *testing.T) config.Machine {
	t.Helper()

	host := os.Getenv("DEVMACHINE_TEST_HOST")
	port := os.Getenv("DEVMACHINE_TEST_PORT")
	key := os.Getenv("DEVMACHINE_TEST_KEY")
	knownHosts := os.Getenv("DEVMACHINE_TEST_KNOWN_HOSTS")
	if host == "" || port == "" || key == "" || knownHosts == "" {
		t.Skip("no test VPS: run `eval \"$(scripts/fake-vps.sh env)\"` first")
	}
	n, err := strconv.Atoi(port)
	if err != nil {
		t.Fatalf("DEVMACHINE_TEST_PORT is not a number: %v", err)
	}

	user := os.Getenv("DEVMACHINE_TEST_USER")
	if user == "" {
		user = "root"
	}
	return config.Machine{
		Name:           "sandbox",
		Hosts:          []config.Host{{Address: host}},
		User:           user,
		Port:           n,
		Key:            key,
		KnownHostsFile: knownHosts,
	}
}

func TestDialReachesTheTestMachine(t *testing.T) {
	m := testMachine(t)

	client, address, err := Dial(context.Background(), m, "")
	if err != nil {
		t.Fatalf("Dial returned %v", err)
	}
	defer client.Close()

	if address != m.Hosts[0].Address {
		t.Fatalf("connected through %q, want %q", address, m.Hosts[0].Address)
	}
}

func TestRunReturnsTheCommandOutput(t *testing.T) {
	m := testMachine(t)

	client, _, err := Dial(context.Background(), m, "")
	if err != nil {
		t.Fatalf("Dial returned %v", err)
	}
	defer client.Close()

	out, err := client.Run(context.Background(), "id -un")
	if err != nil {
		t.Fatalf("Run returned %v", err)
	}
	if strings.TrimSpace(out) != m.User {
		t.Fatalf("got %q, want %q", strings.TrimSpace(out), m.User)
	}
}

func TestRunReportsACommandThatFailed(t *testing.T) {
	m := testMachine(t)

	client, _, err := Dial(context.Background(), m, "")
	if err != nil {
		t.Fatalf("Dial returned %v", err)
	}
	defer client.Close()

	if _, err := client.Run(context.Background(), "exit 3"); err == nil {
		t.Fatal("expected an error from a command that exited non-zero")
	}
}

func TestRunSaysWhyTheCommandFailed(t *testing.T) {
	m := testMachine(t)

	client, _, err := Dial(context.Background(), m, "")
	if err != nil {
		t.Fatalf("Dial returned %v", err)
	}
	defer client.Close()

	_, err = client.Run(context.Background(), "echo 'the reason' >&2; exit 3")
	if err == nil || !strings.Contains(err.Error(), ": the reason") {
		t.Fatalf("Run lost the reason: %v", err)
	}
	_, err = client.RunInput(context.Background(), "echo 'the reason' >&2; exit 3", strings.NewReader(""))
	if err == nil || !strings.Contains(err.Error(), ": the reason") {
		t.Fatalf("RunInput lost the reason: %v", err)
	}
}

func TestDialFallsBackToTheNextAddress(t *testing.T) {
	m := testMachine(t)
	// An address in the documentation range never answers, so the fallback is
	// the only way this can succeed.
	m.Hosts = append([]config.Host{{Address: "203.0.113.1"}}, m.Hosts...)

	client, address, err := Dial(context.Background(), m, "")
	if err != nil {
		t.Fatalf("Dial returned %v", err)
	}
	defer client.Close()

	if address == "203.0.113.1" {
		t.Fatal("Dial claimed to reach an address that cannot answer")
	}
}

func TestDialSaysEveryAddressFailed(t *testing.T) {
	m := config.Machine{
		Name:  "main",
		Hosts: []config.Host{{Address: "203.0.113.1"}, {Address: "203.0.113.2"}},
		User:  "root",
		Port:  22,
		Key:   throwawayKey(t),
	}
	m.KnownHostsFile = filepath.Join(t.TempDir(), "known_hosts")
	trustMachine(t, m, newHostKey(t).PublicKey())

	_, _, err := Dial(context.Background(), m, "")
	if err == nil {
		t.Fatal("expected an error when no address answers")
	}
	for _, want := range []string{"203.0.113.1", "203.0.113.2"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the error does not name %q: %v", want, err)
		}
	}
}

func TestDialLogsInAsTheUserItIsGiven(t *testing.T) {
	m := testMachine(t)

	// An account that does not exist cannot authenticate. Failing here is what
	// proves the override reached the server instead of being ignored in
	// favour of the machine's admin login, which would have succeeded.
	if _, _, err := Dial(context.Background(), m, "nobody-here"); err == nil {
		t.Fatal("Dial succeeded as a user that does not exist on the machine")
	}
}

func TestDialFallsBackToTheAdminUserWhenGivenNone(t *testing.T) {
	m := testMachine(t)

	client, _, err := Dial(context.Background(), m, "")
	if err != nil {
		t.Fatalf("Dial returned %v", err)
	}
	defer client.Close()

	out, err := client.Run(context.Background(), "id -un")
	if err != nil {
		t.Fatalf("Run returned %v", err)
	}
	if strings.TrimSpace(out) != m.User {
		t.Fatalf("logged in as %q, want the machine's admin user %q", strings.TrimSpace(out), m.User)
	}
}

func TestDialTellsARefusedLoginApartFromAnUnreachableMachine(t *testing.T) {
	m := testMachine(t)

	_, _, err := Dial(context.Background(), m, "nobody-here")
	if err == nil {
		t.Fatal("Dial succeeded as a user that does not exist")
	}
	// The machine answered; only the login failed. Saying "no address
	// answered" would send someone to check the network for nothing.
	if strings.Contains(err.Error(), "no address answered") {
		t.Fatalf("a refused login was reported as an unreachable machine: %v", err)
	}
	for _, want := range []string{"refused the login", "nobody-here"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the error does not say %q: %v", want, err)
		}
	}
}

func TestDialSaysWhatToDoWithNoKeyAndNoAgent(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")

	m := config.Machine{
		Name:  "main",
		Hosts: []config.Host{{Address: "203.0.113.1"}},
		User:  "root",
		Port:  22,
	}
	m.KnownHostsFile = filepath.Join(t.TempDir(), "known_hosts")
	trustMachine(t, m, newHostKey(t).PublicKey())

	_, _, err := Dial(context.Background(), m, "")
	if err == nil {
		t.Fatal("expected an error with nothing to authenticate with")
	}
	for _, want := range []string{"key", "agent"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the error does not say what is missing (%q): %v", want, err)
		}
	}
}

func TestDialRejectsAKeyItCannotRead(t *testing.T) {
	m := config.Machine{
		Name:  "main",
		Hosts: []config.Host{{Address: "203.0.113.1"}},
		User:  "root",
		Port:  22,
		Key:   filepath.Join(t.TempDir(), "absent"),
	}
	m.KnownHostsFile = filepath.Join(t.TempDir(), "known_hosts")
	trustMachine(t, m, newHostKey(t).PublicKey())

	_, _, err := Dial(context.Background(), m, "")
	if err == nil {
		t.Fatal("expected an error for a key file that does not exist")
	}
	if !strings.Contains(err.Error(), "absent") {
		t.Fatalf("the error does not name the file: %v", err)
	}
}

// tarballWith builds a gzipped tar in memory. The remote package cannot use
// provision.Tar for this: provision reaches for a client, so importing it back
// would be a cycle.
func tarballWith(t *testing.T, files map[string]string) []byte {
	t.Helper()

	var buf bytes.Buffer
	zipped := gzip.NewWriter(&buf)
	archive := tar.NewWriter(zipped)
	for name, body := range files {
		header := &tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}
		if err := archive.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if _, err := archive.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := zipped.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestUploadAndStreamAgainstTheThrowawayMachine(t *testing.T) {
	m := testMachine(t)

	client, _, err := Dial(context.Background(), m, "")
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	dir := "/tmp/devmachine-test-" + strconv.Itoa(os.Getpid())
	defer client.Run(context.Background(), "rm -rf "+dir)

	tarball := tarballWith(t, map[string]string{"hello.txt": "hi\n"})
	if err := client.Upload(context.Background(), dir, bytes.NewReader(tarball)); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := client.Stream(context.Background(), "cat "+dir+"/hello.txt", &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out.String()) != "hi" {
		t.Fatalf("got %q", out.String())
	}

	if err := client.Stream(context.Background(), "exit 3", io.Discard, io.Discard); err == nil {
		t.Fatal("a non-zero exit was reported as success")
	}
}

// TestStreamWritesWhileTheCommandIsStillRunning is the whole reason Stream
// exists: output collected and printed at the end makes a long run look stuck.
func TestStreamWritesWhileTheCommandIsStillRunning(t *testing.T) {
	m := testMachine(t)

	client, _, err := Dial(context.Background(), m, "")
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	first := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- client.Stream(context.Background(), "echo one; sleep 5; echo two", &firstWrite{ch: first}, io.Discard)
	}()

	select {
	case <-first:
	case err := <-done:
		t.Fatalf("the command finished before anything was written: %v", err)
	case <-time.After(4 * time.Second):
		t.Fatal("nothing was written in the first four seconds of a five second command")
	}

	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

// firstWrite closes its channel the first time anything is written to it.
type firstWrite struct {
	ch   chan struct{}
	once sync.Once
}

func (w *firstWrite) Write(p []byte) (int, error) {
	w.once.Do(func() { close(w.ch) })
	return len(p), nil
}

func TestShellQuoteSurvivesAQuoteInThePath(t *testing.T) {
	if got := shellQuote("/opt/dev'machine"); got != `'/opt/dev'\''machine'` {
		t.Fatalf("got %s", got)
	}
}

// fakeSSHServer is an SSH server in this process, so a test can assert on what
// the client offered and how many times it connected.
//
// Shelling out to `ssh` would make both unobservable, and those two facts are
// the whole point of the bootstrap: offer one method, and prove the key on a
// connection of its own.
type fakeSSHServer struct {
	addr string
	key  ssh.PublicKey

	mu          sync.Mutex
	methods     []string
	connections int
}

// methodsOffered returns the authentication methods the client actually chose.
//
// Every SSH client asks "none" first, because that is how the protocol says to
// find out what a server takes. It is not a method the CLI offered, so it is
// left out.
func (s *fakeSSHServer) methodsOffered() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.methods...)
}

func (s *fakeSSHServer) connectionCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.connections
}

func sshServerAccepting(t *testing.T, method string) *fakeSSHServer {
	return sshServer(t, method)
}

func sshServerRejecting(t *testing.T) *fakeSSHServer {
	return sshServer(t, "")
}

// sshServerAcceptingOnlyKey refuses every public key except one, so a test can
// tell a client that filtered the agent down to the right key apart from one
// that let the library try every key the agent holds.
func sshServerAcceptingOnlyKey(t *testing.T, want ssh.PublicKey) *fakeSSHServer {
	t.Helper()

	s := &fakeSSHServer{}
	cfg := &ssh.ServerConfig{
		AuthLogCallback: func(_ ssh.ConnMetadata, method string, _ error) {
			if method == "none" {
				return
			}
			s.mu.Lock()
			defer s.mu.Unlock()
			s.methods = append(s.methods, method)
		},
		PasswordCallback: func(ssh.ConnMetadata, []byte) (*ssh.Permissions, error) {
			return nil, errors.New("refused")
		},
		PublicKeyCallback: func(_ ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			if !bytes.Equal(key.Marshal(), want.Marshal()) {
				return nil, errors.New("refused")
			}
			return &ssh.Permissions{}, nil
		},
	}
	signer := serverHostKey(t)
	s.key = signer.PublicKey()
	cfg.AddHostKey(signer)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	s.addr = listener.Addr().String()

	go s.serve(listener, cfg)
	return s
}

// agentHolding runs a real SSH agent in this process, holding the given
// private keys under the given comments, and returns the socket to talk to
// it.
func agentHolding(t *testing.T, keys ...agent.AddedKey) string {
	t.Helper()

	keyring := agent.NewKeyring()
	for _, k := range keys {
		if err := keyring.Add(k); err != nil {
			t.Fatal(err)
		}
	}

	dir, err := os.MkdirTemp("", "ag")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "s")
	listener, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				_ = agent.ServeAgent(keyring, conn)
			}()
		}
	}()

	return sock
}

func addedKey(t *testing.T, comment string) (agent.AddedKey, ssh.PublicKey) {
	t.Helper()
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	return agent.AddedKey{PrivateKey: private, Comment: comment}, signer.PublicKey()
}

func sshServer(t *testing.T, accept string) *fakeSSHServer {
	t.Helper()

	s := &fakeSSHServer{}
	cfg := &ssh.ServerConfig{
		AuthLogCallback: func(_ ssh.ConnMetadata, method string, _ error) {
			if method == "none" {
				return
			}
			s.mu.Lock()
			defer s.mu.Unlock()
			s.methods = append(s.methods, method)
		},
		PasswordCallback: func(ssh.ConnMetadata, []byte) (*ssh.Permissions, error) {
			if accept != "password" {
				return nil, errors.New("refused")
			}
			return &ssh.Permissions{}, nil
		},
		PublicKeyCallback: func(ssh.ConnMetadata, ssh.PublicKey) (*ssh.Permissions, error) {
			if accept != "publickey" {
				return nil, errors.New("refused")
			}
			return &ssh.Permissions{}, nil
		},
	}
	signer := serverHostKey(t)
	s.key = signer.PublicKey()
	cfg.AddHostKey(signer)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	s.addr = listener.Addr().String()

	go s.serve(listener, cfg)
	return s
}

func (s *fakeSSHServer) serve(listener net.Listener, cfg *ssh.ServerConfig) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		// Counted here, before the handshake, so the count is already written
		// by the time the client's Dial returns.
		s.mu.Lock()
		s.connections++
		s.mu.Unlock()

		go func() {
			defer conn.Close()
			served, chans, reqs, err := ssh.NewServerConn(conn, cfg)
			if err != nil {
				return
			}
			defer served.Close()
			go ssh.DiscardRequests(reqs)
			for ch := range chans {
				ch.Reject(ssh.Prohibited, "this server only answers about authentication")
			}
		}()
	}
}

func serverHostKey(t *testing.T) ssh.Signer {
	t.Helper()

	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	return signer
}

func machineAt(t *testing.T, server *fakeSSHServer) config.Machine {
	t.Helper()

	host, port, err := net.SplitHostPort(server.addr)
	if err != nil {
		t.Fatal(err)
	}
	n, err := strconv.Atoi(port)
	if err != nil {
		t.Fatal(err)
	}
	machine := config.Machine{
		Name:           "sandbox",
		Hosts:          []config.Host{{Address: host}},
		User:           "root",
		Port:           n,
		KnownHostsFile: filepath.Join(t.TempDir(), "known_hosts"),
	}
	store, err := hostkeys.Open(machine.KnownHostsFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Put(machine.Name, machine.Port, server.key); err != nil {
		t.Fatal(err)
	}
	return machine
}

func TestDialWithOffersOnlyThePasswordWhenOneIsGiven(t *testing.T) {
	// An agent holding many keys makes a server cut the connection at
	// MaxAuthTries before the password is ever tried. Offering one method is
	// the whole reason this does not shell out to ssh.
	server := sshServerAccepting(t, "password")
	m := machineAt(t, server)

	client, _, err := DialWith(context.Background(), m, "root", Auth{Password: "devmachine"})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	if got := server.methodsOffered(); !slices.Equal(got, []string{"password"}) {
		t.Fatalf("offered %v", got)
	}
}

func TestDialWithOffersOnlyTheKeyWhenOneIsGiven(t *testing.T) {
	server := sshServerAccepting(t, "publickey")
	m := machineAt(t, server)

	client, _, err := DialWith(context.Background(), m, "root", Auth{KeyPath: throwawayKey(t)})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	if got := server.methodsOffered(); !slices.Equal(got, []string{"publickey"}) {
		t.Fatalf("offered %v", got)
	}
}

func TestDialWithOffersOnlyTheRecordedAgentKey(t *testing.T) {
	// The agent holds a key the server would refuse and the recorded key,
	// with the wrong one added first. Without filtering, the library tries
	// the wrong key before the right one, which is exactly the MaxAuthTries
	// problem this field exists to avoid.
	wrong, _ := addedKey(t, "wrong key")
	right, rightPublic := addedKey(t, "right key")
	sock := agentHolding(t, wrong, right)
	t.Setenv("SSH_AUTH_SOCK", sock)

	server := sshServerAcceptingOnlyKey(t, rightPublic)
	m := machineAt(t, server)
	recorded := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(rightPublic)))

	client, _, err := DialWith(context.Background(), m, "root", Auth{Agent: true, AgentPublicKey: recorded})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	if got := server.methodsOffered(); !slices.Equal(got, []string{"publickey"}) {
		t.Fatalf("offered %v, want exactly one publickey attempt", got)
	}
}

func TestDialWithSaysTheAgentDoesNotHoldTheRecordedKey(t *testing.T) {
	held, _ := addedKey(t, "held key")
	sock := agentHolding(t, held)
	t.Setenv("SSH_AUTH_SOCK", sock)

	_, missingPublic := addedKey(t, "not in the agent")
	recorded := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(missingPublic)))
	fingerprint := ssh.FingerprintSHA256(missingPublic)

	server := sshServerAcceptingOnlyKey(t, missingPublic)
	m := machineAt(t, server)

	_, _, err := DialWith(context.Background(), m, "root", Auth{Agent: true, AgentPublicKey: recorded})
	if err == nil {
		t.Fatal("expected an error: the agent does not hold the recorded key")
	}
	if !strings.Contains(err.Error(), fingerprint) {
		t.Fatalf("the error does not name the fingerprint: %v", err)
	}
	if !strings.Contains(strings.ToLower(err.Error()), "unlock") {
		t.Fatalf("the error does not say what to do: %v", err)
	}
}

func TestDialWithRefusesMoreThanOneMethod(t *testing.T) {
	machine := config.Machine{Name: "main", Port: 22, KnownHostsFile: filepath.Join(t.TempDir(), "known_hosts")}
	trustMachine(t, machine, newHostKey(t).PublicKey())
	_, _, err := DialWith(context.Background(), machine, "root",
		Auth{KeyPath: "/k", Password: "p"})
	if err == nil {
		t.Fatal("two methods were accepted")
	}
}

func TestDialWithSaysAPasswordWasRefused(t *testing.T) {
	server := sshServerRejecting(t)
	m := machineAt(t, server)

	_, _, err := DialWith(context.Background(), m, "root", Auth{Password: "wrong"})
	if !errors.Is(err, ErrAuthRefused) {
		t.Fatalf("got %v, want ErrAuthRefused", err)
	}
	// "no address answered" sends somebody to check the network when the
	// password is what is wrong.
	if !strings.Contains(err.Error(), "password") {
		t.Fatalf("the error does not say which method failed: %v", err)
	}
}

func TestDialWithSaysAKeyWasRefused(t *testing.T) {
	server := sshServerRejecting(t)
	m := machineAt(t, server)
	key := throwawayKey(t)

	_, _, err := DialWith(context.Background(), m, "root", Auth{KeyPath: key})
	if !errors.Is(err, ErrAuthRefused) {
		t.Fatalf("got %v, want ErrAuthRefused", err)
	}
	if !strings.Contains(err.Error(), key) {
		t.Fatalf("the error does not name the key that failed: %v", err)
	}
}

// TestDialWithLeavesAnUnreachableMachineApart guards the other half of the
// distinction: nothing answered, so no method was refused.
func TestDialWithLeavesAnUnreachableMachineApart(t *testing.T) {
	m := config.Machine{
		Name:  "main",
		Hosts: []config.Host{{Address: "203.0.113.1"}},
		User:  "root",
		Port:  22,
	}
	m.KnownHostsFile = filepath.Join(t.TempDir(), "known_hosts")
	trustMachine(t, m, newHostKey(t).PublicKey())

	_, _, err := DialWith(context.Background(), m, "root", Auth{Password: "devmachine"})
	if err == nil {
		t.Fatal("expected an error from an address that cannot answer")
	}
	if errors.Is(err, ErrAuthRefused) {
		t.Fatalf("an unreachable machine was reported as a refused login: %v", err)
	}
}

func TestLiteralTellsAnAddressFromANameToResolve(t *testing.T) {
	if !Literal("203.0.113.10") {
		t.Fatal("an address is literal")
	}
	if Literal("tailscale:main") {
		t.Fatal("a tailscale entry is not an address ssh can dial")
	}
}

type versionedClient struct {
	nopTestClient
	version string
}

func (c versionedClient) ServerVersion() string { return c.version }

func TestIsTailscaleSSHReadsTheServerVersion(t *testing.T) {
	for _, c := range []struct {
		version string
		want    bool
	}{
		{"SSH-2.0-Tailscale", true},
		{"SSH-2.0-OpenSSH_9.6p1 Ubuntu-3ubuntu13", false},
	} {
		if got := IsTailscaleSSH(versionedClient{version: c.version}); got != c.want {
			t.Fatalf("%s: got %v", c.version, got)
		}
	}
	if IsTailscaleSSH(nopTestClient{}) {
		t.Fatal("a client with no version claimed to be Tailscale")
	}
}

type nopTestClient struct{}

func (nopTestClient) Run(context.Context, string) (string, error)                 { return "", nil }
func (nopTestClient) RunInput(context.Context, string, io.Reader) (string, error) { return "", nil }
func (nopTestClient) Stream(context.Context, string, io.Writer, io.Writer) error  { return nil }
func (nopTestClient) Upload(context.Context, string, io.Reader) error             { return nil }
func (nopTestClient) Close() error                                                { return nil }
