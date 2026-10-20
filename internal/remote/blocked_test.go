package remote

import (
	"context"
	"errors"
	"net"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mydevmachine/devmachine/internal/config"
	"golang.org/x/crypto/ssh"
)

const banCheck = "sudo fail2ban-client status sshd"

// closedPortMachine is a machine whose port refuses every connection, the way
// a server answers an address its firewall has banned.
func closedPortMachine(t *testing.T) config.Machine {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return config.Machine{
		Name:           "main",
		Hosts:          []config.Host{{Address: "127.0.0.1"}},
		User:           "root",
		Port:           port,
		KnownHostsFile: filepath.Join(t.TempDir(), "known_hosts"),
	}
}

// droppingMachine accepts each connection and closes it before the handshake.
func droppingMachine(t *testing.T) config.Machine {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()
	t.Cleanup(func() { _ = listener.Close() })
	m := closedPortMachine(t)
	m.Port = listener.Addr().(*net.TCPAddr).Port
	return m
}

func TestDialToAKnownMachineThatRefusesSuggestsABan(t *testing.T) {
	for name, machine := range map[string]config.Machine{
		"refused": closedPortMachine(t),
		"dropped": droppingMachine(t),
	} {
		t.Run(name, func(t *testing.T) {
			trustMachine(t, machine, newHostKey(t).PublicKey())

			_, _, err := DialWith(context.Background(), machine, "root", Auth{Password: "secret"})
			if err == nil {
				t.Fatal("a refused connection was reported as a login")
			}
			if !strings.HasPrefix(err.Error(), `machine "main": no address answered: 127.0.0.1 (`) {
				t.Fatalf("the original error is not first: %v", err)
			}
			if !strings.Contains(err.Error(), banCheck) || !strings.Contains(err.Error(), "fail2ban") {
				t.Fatalf("no hint about a ban: %v", err)
			}
		})
	}
}

func TestDialWithAnotherNetworkFailureSaysNothingOfABan(t *testing.T) {
	machine := closedPortMachine(t)
	trustMachine(t, machine, newHostKey(t).PublicKey())
	dialSSH = func(context.Context, string, *ssh.ClientConfig) (*ssh.Client, error) {
		return nil, errors.New("network unreachable")
	}
	t.Cleanup(func() { dialSSH = realDialContext })

	_, _, err := DialWith(context.Background(), machine, "root", Auth{Password: "secret"})
	if err == nil || strings.Contains(err.Error(), banCheck) {
		t.Fatalf("got %v", err)
	}
}

func TestScanHostKeyOfAKnownMachineThatRefusesSuggestsABan(t *testing.T) {
	machine := closedPortMachine(t)
	trustMachine(t, machine, newHostKey(t).PublicKey())

	_, _, err := ScanHostKey(context.Background(), machine)
	if err == nil || !strings.Contains(err.Error(), banCheck) {
		t.Fatalf("got %v", err)
	}
	if !strings.HasPrefix(err.Error(), `machine "main": no address answered while scanning`) {
		t.Fatalf("the original error is not first: %v", err)
	}
}

func TestAnAddressNeverSeenBeforeSaysNothingOfABan(t *testing.T) {
	machine := closedPortMachine(t)

	_, _, err := ScanHostKey(context.Background(), machine)
	if err == nil || strings.Contains(err.Error(), banCheck) {
		t.Fatalf("scan: got %v", err)
	}
	_, _, err = DialWith(context.Background(), machine, "root", Auth{Password: "secret"})
	if err == nil || strings.Contains(err.Error(), banCheck) {
		t.Fatalf("dial: got %v", err)
	}
}

func muxRunError(t *testing.T, m config.Machine) error {
	t.Helper()
	fakeCacheDir(t)
	fakeSSH := fakeUnreachableSSH(t)
	origLookPath := lookPath
	lookPath = func(string) (string, error) { return fakeSSH, nil }
	t.Cleanup(func() { lookPath = origLookPath })

	client, _, err := DialMux(context.Background(), m, "")
	if err != nil {
		t.Fatalf("DialMux returned %v", err)
	}
	_, err = client.Run(context.Background(), "true")
	if err == nil {
		t.Fatal("a refused connection was reported as a run")
	}
	return err
}

func TestMuxRunOnAKnownMachineThatRefusesSuggestsABan(t *testing.T) {
	err := muxRunError(t, muxTestMachine(t))
	if !strings.HasPrefix(err.Error(), `machine "main": no address answered: 203.0.113.10 (`) ||
		!strings.Contains(err.Error(), banCheck) {
		t.Fatalf("got %v", err)
	}
}

func TestMuxRunOnAnUnseenMachineSaysNothingOfABan(t *testing.T) {
	m := muxTestMachine(t)
	m.KnownHostsFile = filepath.Join(t.TempDir(), "known_hosts")
	if err := muxRunError(t, m); strings.Contains(err.Error(), banCheck) {
		t.Fatalf("got %v", err)
	}
}

func TestLoginToAKnownMachineThatDropsTheConnectionSuggestsABan(t *testing.T) {
	c, _ := fakeLoginSSH(t, 1,
		"echo 'kex_exchange_identification: read: Connection reset by peer' >&2\nexit 255")
	err := c.Login(context.Background())
	if err == nil || !strings.Contains(err.Error(), banCheck) {
		t.Fatalf("Login returned %v", err)
	}
}

func TestLoginThatTimesOutSaysNothingOfABan(t *testing.T) {
	c, _ := fakeLoginSSH(t, 1,
		"echo 'ssh: connect to host 203.0.113.10 port 2222: Operation timed out' >&2\nexit 255")
	err := c.Login(context.Background())
	if err == nil || strings.Contains(err.Error(), banCheck) {
		t.Fatalf("Login returned %v", err)
	}
}
