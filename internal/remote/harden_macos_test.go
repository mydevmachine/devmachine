package remote

import (
	"context"
	"strings"
	"testing"
)

var (
	debian = System{Kernel: "Linux", ID: "debian"}
	macOS  = System{Kernel: KernelDarwin, Version: "15.7.9"}
)

// TestHardenOnAMacValidatesWithoutRunAndNeverReloads: a Mac has no /run and
// a read-only /, and launchd starts sshd per connection, so the next
// connection reads the drop-in without a reload.
func TestHardenOnAMacValidatesWithoutRunAndNeverReloads(t *testing.T) {
	c := hardenClient()
	if err := Harden(context.Background(), c, macOS); err != nil {
		t.Fatal(err)
	}
	joined := c.transcript()
	if !strings.Contains(c.everything(), hardeningDropInPath) || !strings.Contains(c.everything(), "PasswordAuthentication no") {
		t.Fatalf("the drop-in was not written: %s", c.everything())
	}
	if !strings.Contains(joined, "/usr/sbin/sshd -t") {
		t.Fatalf("it does not validate with /usr/sbin/sshd -t: %s", joined)
	}
	for _, unwanted := range []string{"/run", "reload", "systemctl"} {
		if strings.Contains(joined, unwanted) {
			t.Fatalf("it runs %q on a Mac: %s", unwanted, joined)
		}
	}
	if !strings.Contains(c.commands[len(c.commands)-1], "sshd -T") {
		t.Fatalf("the proof is not the last step: %s", joined)
	}
}

func TestHardenOnAMacTakesBackAConfigSshdRefused(t *testing.T) {
	c := &recordingClient{failOn: "sshd -t"}
	if err := Harden(context.Background(), c, macOS); err == nil {
		t.Fatal("it carried on past a bad config")
	}
	last := c.commands[len(c.commands)-1]
	if !strings.Contains(last, "rm -f "+hardeningDropInPath) {
		t.Fatalf("the refused drop-in was left behind: %s", c.transcript())
	}
}

func TestHardenOnAMacTakesTheDropInBackWithoutReloading(t *testing.T) {
	c := &recordingClient{output: map[string]string{AsRoot(effectiveConfigScript): sshdTWithPasswords("yes")}}
	err := Harden(context.Background(), c, macOS)
	want := "the drop-in was written but sshd still allows passwords: " + hardeningDropInPath +
		" is not read by this sshd"
	if err == nil || err.Error() != want {
		t.Fatalf("got %v, want %q", err, want)
	}
	joined := c.transcript()
	if !strings.Contains(c.commands[len(c.commands)-1], "rm -f "+hardeningDropInPath) {
		t.Fatalf("the drop-in was left behind: %s", joined)
	}
	if strings.Contains(joined, "reload") {
		t.Fatalf("it reloaded on a Mac: %s", joined)
	}
}
