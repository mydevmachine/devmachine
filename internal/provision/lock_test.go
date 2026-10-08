package provision

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
