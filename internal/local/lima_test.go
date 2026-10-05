package local

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// recorder stands in for limactl, so every branch is driven without a VM.
type recorder struct {
	calls   [][]string
	replies map[string]string
	fails   map[string]error
	paths   []string
	bodies  []string
}

func (r *recorder) run(_ context.Context, _ io.Writer, args ...string) (string, error) {
	r.calls = append(r.calls, args)

	// A template only exists while limactl is running, so what it held has to
	// be read here or not at all.
	for _, a := range args {
		if strings.HasSuffix(a, ".yaml") {
			r.paths = append(r.paths, a)
			body, err := os.ReadFile(a)
			if err != nil {
				return "", fmt.Errorf("the template was not there when limactl ran: %w", err)
			}
			r.bodies = append(r.bodies, string(body))
		}
	}

	joined := strings.Join(args, " ")
	for pattern, err := range r.fails {
		if strings.Contains(joined, pattern) {
			return "", err
		}
	}
	for pattern, reply := range r.replies {
		if strings.Contains(joined, pattern) {
			return reply, nil
		}
	}
	return "", nil
}

func (r *recorder) joined() string {
	var lines []string
	for _, c := range r.calls {
		lines = append(lines, strings.Join(c, " "))
	}
	return strings.Join(lines, "\n")
}

// stub puts a recorder in place of limactl and reports that lima is installed.
func stub(t *testing.T, r *recorder) *recorder {
	t.Helper()
	if r == nil {
		r = &recorder{}
	}
	if r.replies == nil {
		r.replies = map[string]string{"SSHLocalPort": "57151"}
	}
	if r.fails == nil {
		// An unknown instance is what limactl reports for a name nothing uses.
		r.fails = map[string]error{"{{.Status}}": errors.New("unmatched instances")}
	}
	lookPath = func(string) (string, error) { return "/usr/local/bin/limactl", nil }
	run = r.run
	t.Cleanup(func() { lookPath, run = realLookPath, realRun })
	stubHost(t, 8, 16*gib)
	return r
}

func TestAvailableSaysHowToInstallLima(t *testing.T) {
	lookPath = func(string) (string, error) { return "", errors.New("not found") }
	t.Cleanup(func() { lookPath = realLookPath })

	err := Available()
	if err == nil {
		t.Fatal("it reported lima where there is none")
	}
	if !strings.Contains(err.Error(), "brew install lima") {
		t.Fatalf("got %v", err)
	}
}

func TestAvailableIsQuietWhenLimaIsInstalled(t *testing.T) {
	stub(t, nil)

	if err := Available(); err != nil {
		t.Fatalf("got %v", err)
	}
}

func TestCreateReturnsAMachineTheCLICanAlreadyUse(t *testing.T) {
	stub(t, nil)

	m, err := Create(context.Background(), "alpha", Ubuntu, DefaultSize, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if m.Name != "alpha" || m.User != "root" || m.Port != 57151 {
		t.Fatalf("got %#v", m)
	}
	if len(m.Hosts) != 1 || m.Hosts[0].Address != "127.0.0.1" {
		t.Fatalf("got %#v", m.Hosts)
	}
}

func TestCreateLeavesNoKeyOnTheMachine(t *testing.T) {
	r := stub(t, nil)

	m, err := Create(context.Background(), "alpha", Ubuntu, DefaultSize, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	// A local machine is created the way a bought one arrives: a password and
	// no key. Otherwise `setup` is never exercised by the thing that runs on
	// every test.
	if m.Key != "" {
		t.Fatalf("the machine came back with a key: %q", m.Key)
	}
	for _, forbidden := range []string{"authorized_keys", "shell", "copy"} {
		if strings.Contains(r.joined(), forbidden) {
			t.Fatalf("it touched the machine's keys: %s", r.joined())
		}
	}
	if strings.Contains(string(Template(Ubuntu)), "authorizedKeys") ||
		strings.Contains(string(Template(Ubuntu)), "ssh-ed25519") {
		t.Fatal("the template installs a key")
	}
}

func TestCreateRefusesToTakeOverAnExistingMachine(t *testing.T) {
	r := stub(t, &recorder{replies: map[string]string{"{{.Status}}": "Running"}, fails: map[string]error{}})

	_, err := Create(context.Background(), "alpha", Ubuntu, DefaultSize, io.Discard)
	if err == nil {
		t.Fatal("it created over a machine that was already there")
	}
	if !strings.Contains(err.Error(), "alpha") || !strings.Contains(err.Error(), "delete-local") {
		t.Fatalf("the error does not say what to do: %v", err)
	}
	if strings.Contains(r.joined(), "start --name") {
		t.Fatalf("it started anyway: %s", r.joined())
	}
}

func TestCreateGivesLimaTheTemplateAsAFileItCanRead(t *testing.T) {
	r := stub(t, nil)

	if _, err := Create(context.Background(), "alpha", Ubuntu, DefaultSize, io.Discard); err != nil {
		t.Fatal(err)
	}
	if len(r.paths) != 1 {
		t.Fatalf("limactl was given %d templates: %s", len(r.paths), r.joined())
	}
	// limactl reads the file, so an embedded template has to reach the disk,
	// and the extension is how limactl knows what it is.
	if filepath.Ext(r.paths[0]) != ".yaml" {
		t.Fatalf("got %q", r.paths[0])
	}
	if r.bodies[0] != string(Template(Ubuntu)) {
		t.Fatal("what reached the disk is not the embedded template")
	}
	if _, err := os.Stat(r.paths[0]); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the template was left behind at %s", r.paths[0])
	}
}

func TestCreateRejectsANameLimaWillNotTake(t *testing.T) {
	stub(t, nil)

	for _, name := range []string{"", "--tty=false", "Alpha One", "../escape"} {
		if _, err := Create(context.Background(), name, Ubuntu, DefaultSize, io.Discard); err == nil {
			t.Fatalf("it accepted %q", name)
		}
	}
}

func TestMachineSaysSoWhenThePortIsNotThere(t *testing.T) {
	// limactl reports port 0 for a machine that is not running, and an
	// address with port 0 sends somebody to read a connection error instead.
	stub(t, &recorder{replies: map[string]string{"SSHLocalPort": "0"}, fails: map[string]error{}})

	_, err := Machine(context.Background(), "alpha")
	if err == nil {
		t.Fatal("port 0 was reported as a working machine")
	}
	if !strings.Contains(err.Error(), "not running") {
		t.Fatalf("got %v", err)
	}
}

func TestMachineSaysSoWhenLimactlReportsNothing(t *testing.T) {
	stub(t, &recorder{replies: map[string]string{"SSHLocalPort": "  \n"}, fails: map[string]error{}})

	if _, err := Machine(context.Background(), "alpha"); err == nil {
		t.Fatal("an empty port was accepted")
	}
}

func TestStartStopAndDelete(t *testing.T) {
	for _, c := range []struct {
		name string
		call func(context.Context, string) error
		want string
	}{
		{"start", Start, "start alpha"},
		{"stop", Stop, "stop alpha"},
		// Without -f, delete asks, and nothing is there to answer it.
		{"delete", Delete, "delete -f alpha"},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := stub(t, nil)
			if err := c.call(context.Background(), "alpha"); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(r.joined(), c.want) {
				t.Fatalf("got %s, want %s", r.joined(), c.want)
			}
		})
	}
}

func TestEveryCommandSaysHowToInstallLima(t *testing.T) {
	lookPath = func(string) (string, error) { return "", errors.New("not found") }
	t.Cleanup(func() { lookPath = realLookPath })

	for _, call := range []func() error{
		func() error {
			_, err := Create(context.Background(), "alpha", Ubuntu, DefaultSize, io.Discard)
			return err
		},
		func() error { return Start(context.Background(), "alpha") },
		func() error { return Stop(context.Background(), "alpha") },
		func() error { return Delete(context.Background(), "alpha") },
	} {
		if err := call(); err == nil || !strings.Contains(err.Error(), "brew install lima") {
			t.Fatalf("got %v", err)
		}
	}
}

func TestTemplateEnablesPasswordLoginWhereSshdReadsItFirst(t *testing.T) {
	body := string(Template(Ubuntu))

	// sshd uses the FIRST value it finds for each directive, and the cloud
	// image ships 60-cloudimg-settings.conf. A 99- prefix silently does
	// nothing.
	if !strings.Contains(body, "sshd_config.d/01-") {
		t.Fatalf("the drop-in does not sort first:\n%s", body)
	}
	for _, want := range []string{"PermitRootLogin yes", "PasswordAuthentication yes", "chpasswd"} {
		if !strings.Contains(body, want) {
			t.Fatalf("the template does not carry %q", want)
		}
	}
	// The next person to read this has to know why a password is in a public
	// repository before they decide it is a mistake.
	if !strings.Contains(body, "public on purpose") {
		t.Fatal("the template does not say why the password is public")
	}
}

func TestTemplateMountsNothingFromThisComputer(t *testing.T) {
	// A real server has no shared folder, and a mount lets a test pass for the
	// wrong reason.
	if !strings.Contains(string(Template(Ubuntu)), "mounts: []") {
		t.Fatal("the template shares a folder with the host")
	}
}

// TestCreateReallyMakesAMachineReachableByPasswordOnly is the claim the whole
// feature rests on, and only a running VM can settle it.
func TestCreateReallyMakesAMachineReachableByPasswordOnly(t *testing.T) {
	if testing.Short() {
		t.Skip("creating a VM takes minutes")
	}
	if _, err := exec.LookPath(limactl); err != nil {
		t.Skip("lima is not installed")
	}

	// A name of its own, so a run of this suite never fights the throwaway VPS
	// or another run beside it.
	suffix := make([]byte, 4)
	if _, err := rand.Read(suffix); err != nil {
		t.Fatal(err)
	}
	name := "devmachine-selftest-" + hex.EncodeToString(suffix)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	t.Cleanup(func() {
		if err := Delete(context.Background(), name); err != nil {
			t.Errorf("the test VM was left behind: %v", err)
		}
	})

	m, err := Create(ctx, name, Ubuntu, DefaultSize, os.Stderr)
	if err != nil {
		t.Fatal(err)
	}

	address := net.JoinHostPort(m.Hosts[0].Address, fmt.Sprint(m.Port))
	settings := func(auth ...ssh.AuthMethod) *ssh.ClientConfig {
		return &ssh.ClientConfig{
			User: m.User, Auth: auth,
			HostKeyCallback: ssh.InsecureIgnoreHostKey(),
			Timeout:         15 * time.Second,
		}
	}

	c, err := ssh.Dial("tcp", address, settings(ssh.Password(Password)))
	if err != nil {
		t.Fatalf("root could not log in with the password: %v", err)
	}
	_ = c.Close()

	// The point of the feature: no key works yet, so `setup` has something to
	// do and gets exercised on every test run.
	if _, err := ssh.Dial("tcp", address, settings()); err == nil {
		t.Fatal("the machine accepted a login without a password")
	}
}
