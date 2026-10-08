package provision

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mydevmachine/devmachine/internal/config"
	"github.com/mydevmachine/devmachine/internal/remote"
)

func localClient(t *testing.T) remote.Client {
	t.Helper()
	client, _, err := remote.Dial(context.Background(), config.Machine{Name: "mac", Self: true}, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })
	return client
}

func TestASecondHolderOfTheSameMachineIsRefusedUntilTheFirstLetsGo(t *testing.T) {
	client := localClient(t)
	lock := filepath.Join(t.TempDir(), "bundle.lock")
	ctx := context.Background()

	release, err := holdMachine(ctx, client, "main", lock, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := holdMachine(ctx, client, "main", lock, true); !errors.Is(err, ErrSyncRunning) {
		release()
		t.Fatalf("a second sync got %v", err)
	}
	release()
	if _, err := os.Stat(lock); !os.IsNotExist(err) {
		t.Fatalf("letting go left the lock behind: %v", err)
	}
	again, err := holdMachine(ctx, client, "main", lock, true)
	if err != nil {
		t.Fatalf("the lock was not free after the first sync: %v", err)
	}
	again()
}

// TestALockWhoseHolderIsGoneIsTakenOver: a sync killed outright, or a machine
// that rebooted mid-run, leaves the lock behind with nobody holding it.
func TestALockWhoseHolderIsGoneIsTakenOver(t *testing.T) {
	client := localClient(t)
	ctx := context.Background()
	for name, holder := range map[string]string{
		"dead process":   "999999 " + bootID(t, client),
		"earlier boot":   "1 an-earlier-boot",
		"never recorded": "",
	} {
		t.Run(name, func(t *testing.T) {
			lock := filepath.Join(t.TempDir(), "bundle.lock")
			if err := os.Mkdir(lock, 0o755); err != nil {
				t.Fatal(err)
			}
			if holder != "" {
				if err := os.WriteFile(filepath.Join(lock, "holder"), []byte(holder+"\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			release, err := holdMachine(ctx, client, "main", lock, true)
			if err != nil {
				t.Fatalf("a lock nobody holds was refused: %v", err)
			}
			release()
		})
	}
}

func bootID(t *testing.T, client remote.Client) string {
	t.Helper()
	out, err := client.Run(context.Background(), bootIDCommand)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(out)
}

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
	return config.Machine{Name: "sandbox", Hosts: []config.Host{{Address: host}}, User: user, Port: n, Key: key, KnownHostsFile: knownHosts}
}

func dialTestMachine(t *testing.T, m config.Machine) remote.Client {
	t.Helper()
	client, _, err := remote.Dial(context.Background(), m, m.User)
	if err != nil {
		t.Fatal(err)
	}
	return client
}

// TestADroppedConnectionLetsGoOfTheLockOnARealMachine: a sync whose
// connection dies never calls release, and the next sync must not be locked
// out by it.
func TestADroppedConnectionLetsGoOfTheLockOnARealMachine(t *testing.T) {
	m := testMachine(t)
	ctx := context.Background()
	lock := fmt.Sprintf("/tmp/devmachine-lock-test-%d/bundle.lock", time.Now().UnixNano())

	first := dialTestMachine(t, m)
	if _, err := holdMachine(ctx, first, m.Name, lock, false); err != nil {
		first.Close()
		t.Fatal(err)
	}
	second := dialTestMachine(t, m)
	defer second.Close()
	if _, err := holdMachine(ctx, second, m.Name, lock, false); !errors.Is(err, ErrSyncRunning) {
		first.Close()
		t.Fatalf("a second sync got %v", err)
	}

	first.Close()
	deadline := time.Now().Add(10 * time.Second)
	for {
		release, err := holdMachine(ctx, second, m.Name, lock, false)
		if err == nil {
			release()
			break
		}
		if !errors.Is(err, ErrSyncRunning) || time.Now().After(deadline) {
			t.Fatalf("the lock outlived its connection: %v", err)
		}
		time.Sleep(500 * time.Millisecond)
	}
	if _, err := second.Run(ctx, remote.AsRoot("rm -rf "+path.Dir(lock))); err != nil {
		t.Fatal(err)
	}
}
