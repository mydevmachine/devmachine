package sessions

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/mydevmachine/devmachine/internal/config"
	"github.com/mydevmachine/devmachine/internal/remote"
)

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

func TestCollectAgainstTheTestMachine(t *testing.T) {
	ctx := context.Background()
	client, _, err := remote.Dial(ctx, testMachine(t), "")
	if err != nil {
		t.Fatalf("Dial returned %v", err)
	}
	defer client.Close()

	if _, err := client.Run(ctx, "command -v tmux >/dev/null 2>&1"); err != nil {
		t.Skip("tmux not on the test machine")
	}
	cleanup := "tmux kill-session -t dm-sess-test 2>/dev/null; rm -rf /tmp/dm-sess-repo"
	t.Cleanup(func() { _, _ = client.Run(context.Background(), cleanup+"; true") })

	setup := "rm -rf /tmp/dm-sess-repo && git init -q -b feature/x /tmp/dm-sess-repo && " +
		"git -C /tmp/dm-sess-repo -c user.name=alice -c user.email=alice@example.com commit -q --allow-empty -m init && " +
		"{ tmux kill-session -t dm-sess-test 2>/dev/null; true; } && " +
		"tmux new-session -d -s dm-sess-test -c /tmp/dm-sess-repo && " +
		"tmux new-window -d -t dm-sess-test && " +
		`tmux send-keys -t dm-sess-test:1 "printf '\\a'" Enter`
	if _, err := client.Run(ctx, setup); err != nil {
		t.Fatalf("setting up the tmux session: %v", err)
	}

	var found *Session
	deadline := time.Now().Add(3 * time.Second)
	for {
		all, err := Collect(ctx, client)
		if err != nil {
			t.Fatalf("Collect returned %v", err)
		}
		found = nil
		for i := range all {
			if all[i].Name == "dm-sess-test" {
				found = &all[i]
			}
		}
		if found == nil {
			t.Fatal("Collect did not list dm-sess-test")
		}
		if found.Attention || time.Now().After(deadline) {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}

	if found.Path != "/tmp/dm-sess-repo" {
		t.Errorf("Path = %q, want /tmp/dm-sess-repo", found.Path)
	}
	if found.Branch != "feature/x" {
		t.Errorf("Branch = %q, want feature/x", found.Branch)
	}
	if found.Windows != 2 {
		t.Errorf("Windows = %d, want 2", found.Windows)
	}
	if found.AgeSeconds < 0 {
		t.Errorf("AgeSeconds = %d, want >= 0", found.AgeSeconds)
	}
	if !found.Attention {
		t.Errorf("Attention = false, want true after a bell in a background window")
	}
}
