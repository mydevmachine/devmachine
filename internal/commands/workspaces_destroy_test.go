package commands

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/mydevmachine/devmachine/internal/config"
	"github.com/mydevmachine/devmachine/internal/dns"
	"github.com/mydevmachine/devmachine/internal/remote"
)

// destroyClient is a remote.Client a test drives by hand: it plays the shell
// script `workspaces destroy` sends, without touching a real machine.
type destroyClient struct {
	userExists bool
	lastScript string
	runErr     error
}

func (c *destroyClient) Run(_ context.Context, script string) (string, error) {
	c.lastScript = script
	if c.runErr != nil {
		return "", c.runErr
	}
	if c.userExists {
		return "removed", nil
	}
	return "absent", nil
}

func (c *destroyClient) RunInput(ctx context.Context, command string, _ io.Reader) (string, error) {
	return c.Run(ctx, command)
}

func (c *destroyClient) Stream(ctx context.Context, command string, stdout, _ io.Writer) error {
	out, err := c.Run(ctx, command)
	if _, werr := io.WriteString(stdout, out); werr != nil {
		return werr
	}
	return err
}

func (c *destroyClient) Upload(context.Context, string, io.Reader) error { return nil }
func (c *destroyClient) Close() error                                    { return nil }

func dialDestroy(t *testing.T, client *destroyClient) {
	t.Helper()
	orig := dial
	dial = func(context.Context, config.Machine, string) (remote.Client, string, error) {
		return client, "203.0.113.10", nil
	}
	t.Cleanup(func() { dial = orig })
}

func TestWorkspacesDestroyDeclinesOnTheWrongTypedName(t *testing.T) {
	client := &destroyClient{userExists: true}
	dialDestroy(t, client)
	dir := configWithKey(t, "workspaces:\n  - name: alice\n    machine: main\n")

	_, err := executeWithInput(t, "not-alice\n", "--config", dir, "workspaces", "destroy", "alice")
	if !errors.Is(err, errDeclined) {
		t.Fatalf("got %v", err)
	}
	if client.lastScript != "" {
		t.Fatalf("it ran something on the machine: %q", client.lastScript)
	}
	cfg, _ := config.Load(dir)
	if len(cfg.Workspaces) != 1 {
		t.Fatalf("config changed: %#v", cfg.Workspaces)
	}
}

func TestWorkspacesDestroyConfirmRunsOneScriptAndRemovesTheRoutesFile(t *testing.T) {
	client := &destroyClient{userExists: true}
	dialDestroy(t, client)
	dir := configWithCaddy(t)
	// configWithCaddy already declares workspace alice; add a route for it.
	if err := config.AddRoute(dir, "alice", config.Route{Host: "app.example.com", Port: 8080}); err != nil {
		t.Fatal(err)
	}

	out, err := execute(t, "--config", dir, "workspaces", "destroy", "alice", "--confirm", "alice")
	if err != nil {
		t.Fatalf("got %v (%s)", err, out)
	}
	for _, want := range []string{"loginctl disable-linger", "terminate-user", "userdel --force --remove", "'alice'", "alice-routes.caddy"} {
		if !strings.Contains(client.lastScript, want) {
			t.Fatalf("script does not contain %q: %s", want, client.lastScript)
		}
	}
	cfg, _ := config.Load(dir)
	if _, err := cfg.Workspace("alice"); err == nil {
		t.Fatal("alice is still configured")
	}
}

func TestWorkspacesDestroyOnAMacDeletesTheAccountWithSysadminctl(t *testing.T) {
	client := &destroyClient{userExists: true}
	dialDestroy(t, client)
	dir := configWithKey(t, "workspaces:\n  - name: alice\n    machine: main\n")
	saveFacts(t, dir, "main", observedMac)

	out, err := execute(t, "--config", dir, "workspaces", "destroy", "alice", "--confirm", "alice")
	if err != nil {
		t.Fatalf("got %v (%s)", err, out)
	}
	if !strings.Contains(client.lastScript, "sysadminctl -deleteUser 'alice'") {
		t.Fatalf("script does not delete the account with sysadminctl: %s", client.lastScript)
	}
	for _, unwanted := range []string{"loginctl", "userdel"} {
		if strings.Contains(client.lastScript, unwanted) {
			t.Fatalf("a Mac has no %s: %s", unwanted, client.lastScript)
		}
	}
	if !strings.Contains(out, "/Users/alice") {
		t.Fatalf("it did not name the Mac home: %q", out)
	}
}

func TestWorkspacesDestroyOnAMacTakesTheAccountOutOfTheRemoteLoginGroupFirst(t *testing.T) {
	client := &destroyClient{userExists: true}
	dialDestroy(t, client)
	dir := configWithKey(t, "workspaces:\n  - name: alice\n    machine: main\n")
	saveFacts(t, dir, "main", observedMac)

	if out, err := execute(t, "--config", dir, "workspaces", "destroy", "alice", "--confirm", "alice"); err != nil {
		t.Fatalf("got %v (%s)", err, out)
	}
	remove := "dseditgroup -o edit -d 'alice' -t user com.apple.access_ssh"
	removeAt := strings.Index(client.lastScript, remove)
	if removeAt < 0 {
		t.Fatalf("script does not take alice out of com.apple.access_ssh: %s", client.lastScript)
	}
	if removeAt > strings.Index(client.lastScript, "sysadminctl -deleteUser") {
		t.Fatalf("the group edit must come before the account is deleted: %s", client.lastScript)
	}
	if !strings.Contains(client.lastScript, "dscl . -read /Groups/com.apple.access_ssh") {
		t.Fatalf("the group edit must be skipped when the group does not exist: %s", client.lastScript)
	}
}

func TestWorkspacesDestroyListsEachRouteBeforeAsking(t *testing.T) {
	client := &destroyClient{userExists: true}
	dialDestroy(t, client)
	dir := configWithKey(t, "workspaces:\n  - name: alice\n    machine: main\n"+
		"    routes: [{host: app.example.com, port: 8080}, {host: api.example.com, port: 9090}]\n")

	out, _ := executeWithInput(t, "\n", "--config", dir, "workspaces", "destroy", "alice")
	for _, want := range []string{"https://app.example.com", "https://api.example.com"} {
		if !strings.Contains(out, want) {
			t.Fatalf("it did not list the route %q: %q", want, out)
		}
	}
}

func TestWorkspacesDestroyCheckRunsNothing(t *testing.T) {
	client := &destroyClient{userExists: true}
	dialDestroy(t, client)
	dir := configWithKey(t, "workspaces:\n  - name: alice\n    machine: main\n")

	out, err := execute(t, "--config", dir, "workspaces", "destroy", "alice", "--check")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "would destroy alice") {
		t.Fatalf("got %q", out)
	}
	if client.lastScript != "" {
		t.Fatalf("--check ran something on the machine: %q", client.lastScript)
	}
	cfg, _ := config.Load(dir)
	if len(cfg.Workspaces) != 1 {
		t.Fatalf("--check changed the configuration: %#v", cfg.Workspaces)
	}
}

func TestWorkspacesDestroyConfirmMustMatchTheName(t *testing.T) {
	client := &destroyClient{userExists: true}
	dialDestroy(t, client)
	dir := configWithKey(t, "workspaces:\n  - name: alice\n    machine: main\n")

	_, err := execute(t, "--config", dir, "workspaces", "destroy", "alice", "--confirm", "bob")
	if err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("got %v", err)
	}
	if client.lastScript != "" {
		t.Fatal("it dialed the machine despite the mismatch")
	}
}

func TestWorkspacesDestroyDialFailureLeavesConfigAlone(t *testing.T) {
	orig := dial
	dial = func(context.Context, config.Machine, string) (remote.Client, string, error) {
		return nil, "", errors.New("no route to host")
	}
	t.Cleanup(func() { dial = orig })
	dir := configWithKey(t, "workspaces:\n  - name: alice\n    machine: main\n")

	_, err := execute(t, "--config", dir, "workspaces", "destroy", "alice", "--confirm", "alice")
	if err == nil || !strings.Contains(err.Error(), "workspaces rm") {
		t.Fatalf("got %v", err)
	}
	cfg, _ := config.Load(dir)
	if len(cfg.Workspaces) != 1 {
		t.Fatalf("config changed: %#v", cfg.Workspaces)
	}
}

func TestWorkspacesDestroyScriptFailureLeavesConfigAlone(t *testing.T) {
	client := &destroyClient{userExists: true, runErr: errors.New("permission denied")}
	dialDestroy(t, client)
	dir := configWithKey(t, "workspaces:\n  - name: alice\n    machine: main\n")

	_, err := execute(t, "--config", dir, "workspaces", "destroy", "alice", "--confirm", "alice")
	if err == nil {
		t.Fatal("expected the script failure to surface")
	}
	cfg, _ := config.Load(dir)
	if len(cfg.Workspaces) != 1 {
		t.Fatalf("config changed: %#v", cfg.Workspaces)
	}
}

func TestWorkspacesDestroyAbsentAccountStillSucceeds(t *testing.T) {
	client := &destroyClient{userExists: false}
	dialDestroy(t, client)
	dir := configWithKey(t, "workspaces:\n  - name: alice\n    machine: main\n")

	out, err := execute(t, "--config", dir, "workspaces", "destroy", "alice", "--confirm", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "had no account") {
		t.Fatalf("got %q", out)
	}
	cfg, _ := config.Load(dir)
	if _, err := cfg.Workspace("alice"); err == nil {
		t.Fatal("alice is still configured")
	}
}

func TestWorkspacesDestroyRefusesTheAdminAccount(t *testing.T) {
	client := &destroyClient{}
	dialDestroy(t, client)
	dir := configWithKey(t, "workspaces:\n  - name: alice\n    machine: main\n    user: root\n")

	_, err := execute(t, "--config", dir, "workspaces", "destroy", "alice", "--confirm", "alice")
	if err == nil || !strings.Contains(err.Error(), "administrative account") {
		t.Fatalf("got %v", err)
	}
	if client.lastScript != "" {
		t.Fatal("it dialed the machine for the admin account")
	}
}

func TestWorkspacesDestroyRefusesWhenItCannotFindWhereItsRoutesLive(t *testing.T) {
	client := &destroyClient{userExists: true}
	dialDestroy(t, client)
	dir := configWithKey(t, "workspaces:\n  - name: alice\n    machine: main\n"+
		"    routes: [{host: app.example.com, port: 8080}]\n")

	_, err := execute(t, "--config", dir, "workspaces", "destroy", "alice", "--confirm", "alice")
	if err == nil || !strings.Contains(err.Error(), "app.example.com") || !strings.Contains(err.Error(), "expose rm") {
		t.Fatalf("got %v", err)
	}
	if client.lastScript != "" {
		t.Fatalf("ran on the machine: %q", client.lastScript)
	}
	cfg, _ := config.Load(dir)
	if len(cfg.Workspaces) != 1 {
		t.Fatal("config changed")
	}
}

func configDestroying(t *testing.T) string {
	t.Helper()
	dir := configWith(t, "machines:\n  - name: main\n    hosts: [203.0.113.10]\n    packages: [caddy]\n"+
		"workspaces:\n  - name: alice\n    routes: [{host: app.example.com, port: 8080}, {host: api.example.com, port: 8081}]\n")
	writeCaddyPackage(t, dir)
	writeDNSPackage(t, dir, "hostinger", nil, "print('ok')")
	lockOnto(t, dir, "main", "hostinger")
	return dir
}

func TestWorkspacesDestroyRemovesTheRecordsThatStillPointAtTheMachine(t *testing.T) {
	client := &exposeClient{zones: []string{"example.com"}, records: []dns.Record{
		{Name: "app", Type: "A", Value: "203.0.113.10"},
		{Name: "api", Type: "A", Value: "198.51.100.7"},
	}}
	dialExpose(t, client)
	dir := configDestroying(t)

	out, err := execute(t, "--config", dir, "workspaces", "destroy", "alice", "--confirm", "alice")
	if err != nil {
		t.Fatal(err, out)
	}
	if len(client.deleted) != 1 || !strings.Contains(client.deleted[0], `{"name":"app","type":"A","value":"203.0.113.10"}`) {
		t.Fatalf("only app's record pointing at main goes: %q", client.deleted)
	}
	if !strings.Contains(out, "api.example.com points at 198.51.100.7") {
		t.Fatalf("it does not say why api's record stays:\n%s", out)
	}
}

func TestWorkspacesDestroyIsNotStoppedByTheDNSProvider(t *testing.T) {
	client := &exposeClient{zones: []string{"example.com"},
		records:   []dns.Record{{Name: "app", Type: "A", Value: "203.0.113.10"}},
		deleteErr: errors.New("exit status 1")}
	dialExpose(t, client)
	dir := configDestroying(t)

	out, err := execute(t, "--config", dir, "workspaces", "destroy", "alice", "--confirm", "alice")
	if err != nil {
		t.Fatalf("a refused record must not stop the destroy: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Remove this record by hand") || !strings.Contains(out, "app (in example.com)\tA\t203.0.113.10") {
		t.Fatalf("%s", out)
	}
	cfg, _ := config.Load(dir)
	if _, err := cfg.Workspace("alice"); err == nil {
		t.Fatal("alice is still configured")
	}
}

func TestWorkspacesDestroyWithNoProviderPrintsTheRecordsToRemove(t *testing.T) {
	dialExpose(t, &exposeClient{})
	dir := configWith(t, "machines:\n  - name: main\n    hosts: [203.0.113.10]\n    packages: [caddy]\n"+
		"workspaces:\n  - name: alice\n    routes: [{host: app.example.com, port: 8080}]\n")
	writeCaddyPackage(t, dir)

	out, err := execute(t, "--config", dir, "workspaces", "destroy", "alice", "--confirm", "alice")
	if err != nil {
		t.Fatal(err, out)
	}
	if !strings.Contains(out, "Remove this record by hand") || !strings.Contains(out, "app.example.com\tA\t203.0.113.10") {
		t.Fatalf("%s", out)
	}
}

func TestWorkspacesDestroyCheckShowsTheDNSRemovalAndDeletesNothing(t *testing.T) {
	client := &exposeClient{zones: []string{"example.com"},
		records: []dns.Record{{Name: "app", Type: "A", Value: "203.0.113.10"}}}
	dialExpose(t, client)
	dir := configDestroying(t)

	out, err := execute(t, "--config", dir, "workspaces", "destroy", "alice", "--check")
	if err != nil {
		t.Fatal(err, out)
	}
	if !strings.Contains(out, "would remove app A 203.0.113.10 from example.com (provider hostinger)") {
		t.Fatalf("%s", out)
	}
	if len(client.deleted) != 0 {
		t.Fatalf("--check deleted %q", client.deleted)
	}
}
