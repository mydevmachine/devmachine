package commands

import (
	"strings"
	"testing"

	"github.com/mydevmachine/devmachine/internal/expose"
)

// TestReloadCaddyFallsBackToCaddyItself: a Mac has no systemctl, and its
// Caddy runs under launchd, which `caddy reload` reaches through Caddy's own
// admin endpoint.
func TestReloadCaddyFallsBackToCaddyItself(t *testing.T) {
	script := reloadCaddyScript()
	systemd := strings.Index(script, "systemctl reload caddy")
	own := strings.Index(script, "caddy reload --config '"+expose.Caddyfile+"' --adapter caddyfile")
	if systemd < 0 || own < systemd {
		t.Fatalf("got %q", script)
	}
	if !strings.HasSuffix(script, "|| true)") {
		t.Fatalf("a Caddy that does not answer stops the removal: %q", script)
	}
}

func TestDestroyNamesTheHomeTheMachineHas(t *testing.T) {
	if got := homeOf("alice", "Darwin"); got != "/Users/alice" {
		t.Fatalf("a Mac got %q", got)
	}
	for _, system := range []string{"Linux", ""} {
		if got := homeOf("alice", system); got != "/home/alice" {
			t.Fatalf("%q got %q", system, got)
		}
	}
}
