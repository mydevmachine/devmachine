package commands

import (
	"strings"
	"testing"

	"github.com/mydevmachine/devmachine/internal/config"
	"github.com/mydevmachine/devmachine/internal/expose"
)

const macPathLine = `PATH='/opt/local/bin:/opt/local/sbin:/Applications/Tailscale.app/Contents/MacOS':"$PATH"; export PATH; `

func TestLoginOnAMacRunsTheCommandWithTheManagersPath(t *testing.T) {
	launched, _ := loggingIn(t)
	dir := configWithCredentials(t, "probe-cmd")
	saveFacts(t, dir, "main", observedMac)

	if _, err := execute(t, "--config", dir, "login", "gh"); err != nil {
		t.Fatalf("login returned %v", err)
	}
	line := strings.Join(*launched, " ")
	if !strings.Contains(line, macPathLine+"gh auth login") {
		t.Fatalf("gh from the package manager is not on the path: %q", line)
	}
}

func TestLoginOnLinuxRunsTheCommandAsItIs(t *testing.T) {
	launched, _ := loggingIn(t)
	dir := configWithCredentials(t, "probe-cmd")

	if _, err := execute(t, "--config", dir, "login", "gh"); err != nil {
		t.Fatalf("login returned %v", err)
	}
	line := strings.Join(*launched, " ")
	if strings.Contains(line, "PATH=") || !strings.HasSuffix(line, " gh auth login") {
		t.Fatalf("got %q", line)
	}
}

func TestLoginToANetworkOnAMacFindsItsTools(t *testing.T) {
	launched := captureInteractive(t)
	client := &recordingRemote{out: "main-abc\n"}
	dialing(t, client)
	dir := configWithNetwork(t, "")
	saveFacts(t, dir, "main", observedMac)

	if _, err := execute(t, "--config", dir, "login", "acme-net"); err != nil {
		t.Fatal(err)
	}
	if line := strings.Join(*launched, " "); !strings.Contains(line, macPathLine+"DEVMACHINE_SETTINGS=") {
		t.Fatalf("join runs without the Mac's tools on the path: %q", line)
	}
	if len(client.ran) != 1 || !strings.HasPrefix(client.ran[0], macPathLine) {
		t.Fatalf("self_name runs without the Mac's tools on the path: %#v", client.ran)
	}
}

func TestExposeOnAMacSetsThePathInsideTheScriptSudoRuns(t *testing.T) {
	dir := t.TempDir()
	saveFacts(t, dir, "main", observedMac)
	change := expose.FileChange{Path: "/etc/caddy/sites.d/alice-routes.caddy", Owner: "root", Group: "wheel", Mode: "0644"}

	got := routesScript(dir, config.Machine{Name: "main"}, change)
	if got != macPathLine+expose.ApplyScript(change, expose.Caddyfile) {
		t.Fatalf("got %q", got)
	}
}

func TestExposeOnLinuxSendsTheScriptAsItIs(t *testing.T) {
	change := expose.FileChange{Path: "/etc/caddy/sites.d/alice-routes.caddy", Owner: "root", Group: "root", Mode: "0644"}
	if got := routesScript(t.TempDir(), config.Machine{Name: "main"}, change); got != expose.ApplyScript(change, expose.Caddyfile) {
		t.Fatalf("got %q", got)
	}
}

func TestRemovingAWorkspacesSitesOnAMacFindsCaddy(t *testing.T) {
	dir := t.TempDir()
	saveFacts(t, dir, "main", observedMac)

	got := removeSitesScript(dir, config.Machine{Name: "main"}, "/etc/caddy/sites.d/alice-routes.caddy")
	if !strings.HasPrefix(got, macPathLine+"rm -f '/etc/caddy/sites.d/alice-routes.caddy' && ") ||
		!strings.HasSuffix(got, reloadCaddyScript()) {
		t.Fatalf("got %q", got)
	}
}
