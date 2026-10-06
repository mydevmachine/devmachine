package commands

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mydevmachine/devmachine/internal/aliases"
	"github.com/mydevmachine/devmachine/internal/config"
	"github.com/mydevmachine/devmachine/internal/keys"
)

// testAgentKey and testAgentKey2 are real authorized_keys lines, so
// ParseAuthorizedKey has something valid to parse.
const (
	testAgentKey  = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAICIwq96VZXRs174bKzGUdytXjlvG0uIYHCTPTPplRnmv main"
	testAgentKey2 = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIBd3EtiUGoL0YW/tZAOFOH+SlRRh0PJ1fDGC7CkvrWve other"
)

// withKey writes the public half of the machine's key, which is the thing a
// new workspace is reachable by, and points the configuration at it.
//
// PublicFor reads the .pub beside the key, so no real pair is needed to prove
// that the CLI has a line to copy.
func withKey(t *testing.T, dir, machine string) string {
	t.Helper()
	keyDir := filepath.Join(dir, "keys")
	if err := os.MkdirAll(keyDir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(keyDir, machine)
	if err := os.WriteFile(path+".pub", []byte("ssh-ed25519 AAAAC3Nza devmachine-"+machine+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// configWithKey is one machine the CLI can already get into, which is the
// state every workspace command assumes.
func configWithKey(t *testing.T, extra string) string {
	t.Helper()
	dir := t.TempDir()
	key := withKey(t, dir, "main")
	body := "machines:\n  - name: main\n    hosts: [203.0.113.10]\n    key: " + key + "\n" + extra
	if err := os.WriteFile(filepath.Join(dir, "config.yml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestWorkspacesNewUsesTheConfiguredDefault(t *testing.T) {
	dir := configWithKey(t, "defaults:\n  workspace: [workspace, dev, zsh]\n")

	if _, err := execute(t, "--config", dir, "workspaces", "new", "alice", "--yes"); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	w, err := cfg.Workspace("alice")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(w.Packages, []string{"workspace", "dev", "zsh"}) {
		t.Fatalf("got %#v", w.Packages)
	}
}

func TestWorkspacesNewRefreshesSSHAliasesWhenEnabled(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := configWithTrustedKey(t, "ssh_aliases: true\n")

	out, err := execute(t, "--config", dir, "workspaces", "new", "alice", "--yes")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "updated the SSH aliases") {
		t.Fatalf("did not say the aliases were refreshed: %q", out)
	}
	if !strings.Contains(out, "devmachine ssh alice") {
		t.Fatalf("did not say how to reach it: %q", out)
	}

	path, err := aliases.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "alice-devmachine") {
		t.Fatalf("the alias was not written: %s", body)
	}
}

func TestWorkspacesNewLeavesAliasesAloneWhenNotEnabled(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := configWithTrustedKey(t, "")

	out, err := execute(t, "--config", dir, "workspaces", "new", "alice", "--yes")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "updated the SSH aliases") {
		t.Fatalf("refreshed the aliases without being told to: %q", out)
	}
	if _, err := os.Stat(filepath.Join(home, ".ssh", "config")); err == nil {
		t.Fatal("aliases were written even though ssh_aliases was not set")
	}
}

func TestWorkspacesNewRefusesWhenTheMachineHasNoKey(t *testing.T) {
	// A workspace is reachable because the administrative key is copied into
	// it. With no key there is nothing to copy, and the account would be
	// created with no way in.
	dir := writeConfigDir(t, "machines:\n  - name: main\n    hosts: [203.0.113.10]\n    key: /nowhere/at/all\n")

	_, err := execute(t, "--config", dir, "workspaces", "new", "alice", "--yes")
	if err == nil {
		t.Fatal("it made a workspace nobody could reach")
	}
	if !strings.Contains(err.Error(), "devmachine setup") {
		t.Fatalf("the error does not say how to fix it: %v", err)
	}
}

func TestWorkspacesNewRefusesWhenNothingHoldsTheKeyEither(t *testing.T) {
	// No `key:` means the SSH agent serves it. An agent holding nothing is
	// the same situation: there is no line to copy into the account.
	t.Setenv("SSH_AUTH_SOCK", "")
	dir := writeConfigDir(t, "machines:\n  - name: main\n    hosts: [203.0.113.10]\n")

	_, err := execute(t, "--config", dir, "workspaces", "new", "alice", "--yes")
	if err == nil {
		t.Fatal("it made a workspace nobody could reach")
	}
	if !strings.Contains(err.Error(), "devmachine setup") {
		t.Fatalf("the error does not say how to fix it: %v", err)
	}
}

func TestWorkspacesNewRefusesWhenTheAgentDoesNotHoldTheRecordedKey(t *testing.T) {
	// A workspace is reachable through the key the machine actually logs in
	// with. An agent holding some other key is no better than holding none.
	t.Cleanup(swap(&agentKeys, func() ([]keys.Offered, error) {
		return []keys.Offered{{Fingerprint: "SHA256:other", Comment: "other", PublicKey: testAgentKey2}}, nil
	}))
	dir := writeConfigDir(t, "machines:\n  - name: main\n    hosts: [203.0.113.10]\n    agent_key: "+testAgentKey+"\n")

	_, err := execute(t, "--config", dir, "workspaces", "new", "alice", "--yes")
	if err == nil {
		t.Fatal("it made a workspace nobody could reach")
	}
	if !strings.Contains(err.Error(), "SHA256:") {
		t.Fatalf("the error does not name the fingerprint: %v", err)
	}
}

func TestWorkspacesNewAcceptsTheRecordedKeyWhenTheAgentHoldsIt(t *testing.T) {
	t.Cleanup(swap(&agentKeys, func() ([]keys.Offered, error) {
		return []keys.Offered{{Fingerprint: "SHA256:xxxx", Comment: "main", PublicKey: testAgentKey}}, nil
	}))
	dir := writeConfigDir(t, "machines:\n  - name: main\n    hosts: [203.0.113.10]\n    agent_key: "+testAgentKey+"\n")

	if _, err := execute(t, "--config", dir, "workspaces", "new", "alice", "--yes"); err != nil {
		t.Fatal(err)
	}
}

func TestWorkspacesNewRefusesToGuessBetweenMachines(t *testing.T) {
	dir := configWithKey(t, "  - name: sandbox\n    hosts: [198.51.100.7]\n")

	_, err := execute(t, "--config", dir, "workspaces", "new", "alice", "--yes")
	if err == nil {
		t.Fatal("it picked a machine nobody named")
	}
	if !strings.Contains(err.Error(), "--machine") {
		t.Fatalf("the error does not say how to choose: %v", err)
	}
}

func TestWorkspacesNewCopiesOnlyThePackages(t *testing.T) {
	dir := configWithKey(t, "workspaces:\n  - name: bob\n    machine: main\n    user: robert\n    packages: [workspace, dev]\n")

	if _, err := execute(t, "--config", dir, "workspaces", "new", "alice", "--like", "bob", "--yes"); err != nil {
		t.Fatal(err)
	}

	cfg, _ := config.Load(dir)
	w, err := cfg.Workspace("alice")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(w.Packages, []string{"workspace", "dev"}) {
		t.Fatalf("the packages were not copied: %#v", w.Packages)
	}
	// A copied account name would collide.
	if w.User != "" {
		t.Fatalf("it copied the account: %q", w.User)
	}
}

func TestWorkspacesNewLikeSaysWhichWorkspacesExist(t *testing.T) {
	dir := configWithKey(t, "workspaces:\n  - name: bob\n    machine: main\n")

	_, err := execute(t, "--config", dir, "workspaces", "new", "alice", "--like", "carol", "--yes")
	if err == nil {
		t.Fatal("it copied a workspace that does not exist")
	}
	if !strings.Contains(err.Error(), "bob") {
		t.Fatalf("the error does not say what is there: %v", err)
	}
}

func TestWorkspacesNewSaysTheMachineIsUntouchedUntilSync(t *testing.T) {
	dir := configWithKey(t, "")

	out, err := execute(t, "--config", dir, "workspaces", "new", "alice", "--yes")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "sync") {
		t.Fatalf("it did not say what makes the account exist: %q", out)
	}
}

func TestWorkspacesNewTakesThePackagesFromTheFlag(t *testing.T) {
	dir := configWithKey(t, "defaults:\n  workspace: [workspace, dev]\n")

	if _, err := execute(t, "--config", dir, "workspaces", "new", "alice",
		"--packages", "workspace,zsh", "--user", "alice2", "--yes"); err != nil {
		t.Fatal(err)
	}

	cfg, _ := config.Load(dir)
	w, _ := cfg.Workspace("alice")
	if !slices.Equal(w.Packages, []string{"workspace", "zsh"}) {
		t.Fatalf("got %#v", w.Packages)
	}
	if w.User != "alice2" {
		t.Fatalf("the account was not taken from the flag: %q", w.User)
	}
}

func TestWorkspacesNewRefusesANameThatIsTaken(t *testing.T) {
	dir := configWithKey(t, "workspaces:\n  - name: alice\n    machine: main\n")

	if _, err := execute(t, "--config", dir, "workspaces", "new", "alice", "--yes"); err == nil {
		t.Fatal("it added a second alice")
	}
}

func TestWorkspacesNewCheckWritesNothing(t *testing.T) {
	dir := configWithKey(t, "")

	out, err := execute(t, "--config", dir, "workspaces", "new", "alice", "--check")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "would") {
		t.Fatalf("a dry run should say what it would do: %q", out)
	}
	cfg, _ := config.Load(dir)
	if len(cfg.Workspaces) != 0 {
		t.Fatalf("a dry run wrote: %#v", cfg.Workspaces)
	}
}

func TestWorkspacesNewAsksFirst(t *testing.T) {
	dir := configWithKey(t, "")

	out, err := executeWithInput(t, "n\n", "--config", dir, "workspaces", "new", "alice")
	if !errors.Is(err, errDeclined) {
		t.Fatalf("got %v", err)
	}
	if !strings.Contains(out, "alice") {
		t.Fatalf("the question does not name the workspace: %q", out)
	}
}

func TestWorkspacesRmLeavesTheAccountOnTheMachine(t *testing.T) {
	dir := configWithKey(t, "workspaces:\n  - name: alice\n    machine: main\n")

	out, err := execute(t, "--config", dir, "workspaces", "rm", "alice", "--yes")
	if err != nil {
		t.Fatal(err)
	}
	// Deleting a home is not something a configuration edit should do, and
	// `sync` could not undo it.
	for _, want := range []string{"account", "home", "machine"} {
		if !strings.Contains(out, want) {
			t.Fatalf("it did not say what survives (%q): %q", want, out)
		}
	}

	cfg, _ := config.Load(dir)
	if len(cfg.Workspaces) != 0 {
		t.Fatalf("it is still configured: %#v", cfg.Workspaces)
	}
}

func TestWorkspacesRmAsksFirst(t *testing.T) {
	dir := configWithKey(t, "workspaces:\n  - name: alice\n    machine: main\n")

	if _, err := executeWithInput(t, "n\n", "--config", dir, "workspaces", "rm", "alice"); !errors.Is(err, errDeclined) {
		t.Fatalf("got %v", err)
	}
	cfg, _ := config.Load(dir)
	if len(cfg.Workspaces) != 1 {
		t.Fatalf("a no still removed it: %#v", cfg.Workspaces)
	}
}

func TestWorkspacesRmSaysWhichOnesThereAre(t *testing.T) {
	dir := configWithKey(t, "workspaces:\n  - name: bob\n    machine: main\n")

	_, err := execute(t, "--config", dir, "workspaces", "rm", "alice", "--yes")
	if err == nil {
		t.Fatal("it removed a workspace that does not exist")
	}
	if !strings.Contains(err.Error(), "bob") {
		t.Fatalf("the error does not say what is there: %v", err)
	}
}

func TestWorkspacesListShowsEachOneAndItsMachine(t *testing.T) {
	dir := configWithKey(t, "workspaces:\n  - name: alice\n    machine: main\n    packages: [workspace]\n")

	out, err := execute(t, "--config", dir, "workspaces", "list")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"alice", "main", "workspace"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output is missing %q: %q", want, out)
		}
	}
}

func TestWorkspacesListAsJSON(t *testing.T) {
	dir := configWithKey(t, "workspaces:\n  - name: alice\n    machine: main\n    user: alice2\n")

	out, err := execute(t, "--config", dir, "--format", "json", "workspaces", "list")
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Workspaces []workspaceRow `json:"workspaces"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output was not JSON: %v (%q)", err, out)
	}
	if len(got.Workspaces) != 1 {
		t.Fatalf("got %#v", got.Workspaces)
	}
	if got.Workspaces[0].User != "alice2" || got.Workspaces[0].Machine != "main" {
		t.Fatalf("got %#v", got.Workspaces[0])
	}
}

func TestWorkspacesListSaysWhenThereAreNone(t *testing.T) {
	dir := configWithKey(t, "")

	out, err := execute(t, "--config", dir, "workspaces", "list")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "workspaces new") {
		t.Fatalf("an empty list should say how to make one: %q", out)
	}
}

func TestWorkspacesEditKeepsAWorkspacesOwnLogin(t *testing.T) {
	dir := configWithKey(t, "workspaces:\n  - name: alice\n    machine: main\n    packages: [dev]\n")

	if _, err := execute(t, "--config", dir, "workspaces", "edit", "alice", "--share", "gh=own", "--yes"); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	w, _ := cfg.Workspace("alice")
	// Without this, a workspace signed in to a different account is
	// overwritten by the shared login on the next sync, and loses it with
	// nothing saying why.
	if w.Credentials["gh"] != config.CredentialOwn {
		t.Fatalf("got %#v", w.Credentials)
	}
}

func TestWorkspacesEditRefusesAChoiceThatIsNeither(t *testing.T) {
	dir := configWithKey(t, "workspaces:\n  - name: alice\n    machine: main\n    packages: [dev]\n")

	_, err := execute(t, "--config", dir, "workspaces", "edit", "alice", "--share", "gh=sometimes", "--yes")
	if err == nil {
		t.Fatal("a choice that is neither was accepted")
	}
	if !strings.Contains(err.Error(), "machine") || !strings.Contains(err.Error(), "own") {
		t.Fatalf("the error does not say what it takes: %v", err)
	}
}

func TestWorkspacesEditWithAnEmptyShareFallsBackToTheConfiguration(t *testing.T) {
	dir := configWithKey(t, "workspaces:\n  - name: alice\n    machine: main\n    packages: [dev]\n")
	if _, err := execute(t, "--config", dir, "workspaces", "edit", "alice", "--share", "gh=own", "--yes"); err != nil {
		t.Fatal(err)
	}

	if _, err := execute(t, "--config", dir, "workspaces", "edit", "alice", "--share", "gh=", "--yes"); err != nil {
		t.Fatal(err)
	}
	cfg, _ := config.Load(dir)
	w, _ := cfg.Workspace("alice")
	if _, held := w.Credentials["gh"]; held {
		t.Fatalf("the decision survived being taken back: %#v", w.Credentials)
	}
}

func TestWorkspacesEditWarnsThatChangingTheMachineMovesNothing(t *testing.T) {
	dir := configWithKey(t, "  - name: sandbox\n    hosts: [198.51.100.7]\nworkspaces:\n  - name: alice\n    machine: main\n")

	out, err := execute(t, "--config", dir, "workspaces", "edit", "alice", "--machine", "sandbox", "--yes")
	if err != nil {
		t.Fatal(err)
	}
	// The next sync creates the account on the new machine; the old one keeps
	// everything it had, and nothing is copied across.
	for _, want := range []string{"sandbox", "main", "sync"} {
		if !strings.Contains(out, want) {
			t.Fatalf("the warning is missing %q: %q", want, out)
		}
	}

	cfg, _ := config.Load(dir)
	w, _ := cfg.Workspace("alice")
	if w.Machine != "sandbox" {
		t.Fatalf("got %#v", w)
	}
}

func TestWorkspacesEditWritesASettingIntoTheWorkspace(t *testing.T) {
	dir := configWithKey(t, "workspaces:\n  - name: alice\n    machine: main\n    packages: [claude-plugins]\n")

	_, err := execute(t, "--config", dir, "workspaces", "edit", "alice",
		"--set", "claude-plugins.marketplace=github.com/somebody/their-plugins", "--yes")
	if err != nil {
		t.Fatal(err)
	}

	cfg, _ := config.Load(dir)
	w, _ := cfg.Workspace("alice")
	if w.Settings["claude-plugins.marketplace"] != "github.com/somebody/their-plugins" {
		t.Fatalf("got %#v", w.Settings)
	}
}

func TestWorkspacesEditRefusesASettingForAPackageItDoesNotHave(t *testing.T) {
	// A setting that reaches nothing is worse than an error: the recipe keeps
	// its default and the machine is not what the configuration says it is.
	dir := configWithKey(t, "workspaces:\n  - name: alice\n    machine: main\n    packages: [zsh]\n")

	_, err := execute(t, "--config", dir, "workspaces", "edit", "alice", "--set", "caddy.email=a@example.com", "--yes")
	if err == nil {
		t.Fatal("it wrote a setting nothing would read")
	}
	if !strings.Contains(err.Error(), "caddy") {
		t.Fatalf("the error does not name it: %v", err)
	}

	cfg, _ := config.Load(dir)
	w, _ := cfg.Workspace("alice")
	if len(w.Settings) != 0 {
		t.Fatalf("it wrote anyway: %#v", w.Settings)
	}
}

func TestWorkspacesEditTakesASettingOutWithAnEmptyValue(t *testing.T) {
	dir := configWithKey(t, `workspaces:
  - name: alice
    machine: main
    packages: [zsh]
    settings:
      zsh.theme: plain
`)

	if _, err := execute(t, "--config", dir, "workspaces", "edit", "alice", "--set", "zsh.theme=", "--yes"); err != nil {
		t.Fatal(err)
	}

	cfg, _ := config.Load(dir)
	w, _ := cfg.Workspace("alice")
	if len(w.Settings) != 0 {
		t.Fatalf("the setting survived: %#v", w.Settings)
	}
}

func TestWorkspacesEditReadsASettingsValueAsYAML(t *testing.T) {
	// A package declaring a list variable has to be settable, and the file it
	// lands in is YAML.
	dir := configWithKey(t, "workspaces:\n  - name: alice\n    machine: main\n    packages: [claude-plugins]\n")

	_, err := execute(t, "--config", dir, "workspaces", "edit", "alice",
		"--set", "claude-plugins.plugins=[one, two]", "--yes")
	if err != nil {
		t.Fatal(err)
	}

	cfg, _ := config.Load(dir)
	w, _ := cfg.Workspace("alice")
	list, ok := w.Settings["claude-plugins.plugins"].([]any)
	if !ok || len(list) != 2 || list[0] != "one" {
		t.Fatalf("got %#v", w.Settings["claude-plugins.plugins"])
	}
}

func TestWorkspacesEditRefusesASetWithNoValue(t *testing.T) {
	dir := configWithKey(t, "workspaces:\n  - name: alice\n    machine: main\n")

	_, err := execute(t, "--config", dir, "workspaces", "edit", "alice", "--set", "zsh.theme", "--yes")
	if err == nil {
		t.Fatal("it accepted a setting with no value")
	}
	if !strings.Contains(err.Error(), "=") {
		t.Fatalf("the error does not give the shape: %v", err)
	}
}

func TestWorkspacesEditAddsAndRemovesPackages(t *testing.T) {
	dir := configWithKey(t, "workspaces:\n  - name: alice\n    machine: main\n    packages: [workspace, dev]\n")

	_, err := execute(t, "--config", dir, "workspaces", "edit", "alice",
		"--add", "zsh", "--rm", "dev", "--yes")
	if err != nil {
		t.Fatal(err)
	}

	cfg, _ := config.Load(dir)
	w, _ := cfg.Workspace("alice")
	if !slices.Equal(w.Packages, []string{"workspace", "zsh"}) {
		t.Fatalf("got %#v", w.Packages)
	}
}

func TestWorkspacesEditRefusesToAddAndRemoveTheSamePackage(t *testing.T) {
	dir := configWithKey(t, "workspaces:\n  - name: alice\n    machine: main\n    packages: [workspace]\n")

	_, err := execute(t, "--config", dir, "workspaces", "edit", "alice", "--add", "zsh", "--rm", "zsh", "--yes")
	if err == nil {
		t.Fatal("it accepted two orders that contradict each other")
	}
}

func TestWorkspacesEditWithNothingToChangeSaysSo(t *testing.T) {
	dir := configWithKey(t, "workspaces:\n  - name: alice\n    machine: main\n")

	_, err := execute(t, "--config", dir, "workspaces", "edit", "alice", "--yes")
	if err == nil {
		t.Fatal("it ran with no instruction")
	}
	if !strings.Contains(err.Error(), "--set") {
		t.Fatalf("the error does not say what to pass: %v", err)
	}
}

func TestWorkspacesEditCheckWritesNothing(t *testing.T) {
	dir := configWithKey(t, "workspaces:\n  - name: alice\n    machine: main\n    packages: [workspace]\n")

	out, err := execute(t, "--config", dir, "workspaces", "edit", "alice", "--add", "zsh", "--check")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "would") {
		t.Fatalf("a dry run should say what it would do: %q", out)
	}

	cfg, _ := config.Load(dir)
	w, _ := cfg.Workspace("alice")
	if slices.Contains(w.Packages, "zsh") {
		t.Fatalf("a dry run wrote: %#v", w.Packages)
	}
}

func TestWorkspacesEditAsksFirst(t *testing.T) {
	dir := configWithKey(t, "workspaces:\n  - name: alice\n    machine: main\n")

	if _, err := executeWithInput(t, "n\n", "--config", dir, "workspaces", "edit", "alice",
		"--user", "alice2"); !errors.Is(err, errDeclined) {
		t.Fatalf("got %v", err)
	}
	cfg, _ := config.Load(dir)
	w, _ := cfg.Workspace("alice")
	if w.User != "" {
		t.Fatalf("a no still wrote: %#v", w)
	}
}

func TestWorkspacesEditRefusesAMachineThatIsNotConfigured(t *testing.T) {
	dir := configWithKey(t, "workspaces:\n  - name: alice\n    machine: main\n")

	if _, err := execute(t, "--config", dir, "workspaces", "edit", "alice",
		"--machine", "nowhere", "--yes"); err == nil {
		t.Fatal("it pointed a workspace at a machine that is not there")
	}
}

func TestWorkspaceDefaultsAddAPackageWithoutTouchingAMachine(t *testing.T) {
	dir := configWithKey(t, "defaults:\n  workspace: [workspace, dev]\n")
	forbidDial(t)

	out, err := execute(t, "--config", dir, "workspaces", "defaults", "--add", "global-skills", "--yes")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(cfg.Defaults.Workspace, "global-skills") {
		t.Fatalf("got %#v", cfg.Defaults.Workspace)
	}
	if !strings.Contains(out, "future workspaces") || !strings.Contains(out, "existing workspaces are unchanged") {
		t.Fatalf("got %q", out)
	}
}

func TestWorkspaceDefaultsCheckWritesNothing(t *testing.T) {
	dir := configWithKey(t, "# keep this\ndefaults:\n  workspace: [workspace, dev]\n")
	path := filepath.Join(dir, config.FileName)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	out, err := execute(t, "--config", dir, "workspaces", "defaults", "--add", "global-skills", "--check")
	if err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(before, after) {
		t.Fatalf("--check rewrote config.yml:\n%s", after)
	}
	if !strings.Contains(out, "would add the package global-skills") || !strings.Contains(out, "Nothing was written") {
		t.Fatalf("got %q", out)
	}
}

func TestWorkspaceDefaultsRefusesContradictoryFlags(t *testing.T) {
	dir := configWithKey(t, "defaults:\n  workspace: [workspace]\n")
	_, err := execute(t, "--config", dir, "workspaces", "defaults", "--add", "zsh", "--rm", "zsh", "--yes")
	if err == nil || !strings.Contains(err.Error(), "two different things") {
		t.Fatalf("got %v", err)
	}
}

func TestWorkspacesEditRefusesToRemoveTheAccount(t *testing.T) {
	dir := configWithKey(t, "workspaces:\n  - name: alice\n    machine: main\n    packages: [workspace, dev]\n")

	_, err := execute(t, "--config", dir, "workspaces", "edit", "alice", "--rm", "workspace", "--yes")
	if err == nil || !strings.Contains(err.Error(), "creates the account") {
		t.Fatalf("got %v, want a refusal that says why", err)
	}
}

func TestWorkspacesEditUnsetTakesASettingOut(t *testing.T) {
	dir := configWithKey(t, "workspaces:\n  - name: alice\n    machine: main\n    packages: [zsh]\n    settings:\n      zsh.theme: plain\n")

	if _, err := execute(t, "--config", dir, "workspaces", "edit", "alice", "--unset", "zsh.theme", "--yes"); err != nil {
		t.Fatal(err)
	}
	cfg, _ := config.Load(dir)
	w, _ := cfg.Workspace("alice")
	if len(w.Settings) != 0 {
		t.Fatalf("the setting survived: %#v", w.Settings)
	}
}

func TestWorkspaceDefaultsWithNoFlagPrintsTheList(t *testing.T) {
	dir := configWithKey(t, "defaults:\n  workspace: [workspace, dev]\n")
	path := filepath.Join(dir, config.FileName)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	out, err := execute(t, "--config", dir, "workspaces", "defaults")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "workspace") || !strings.Contains(out, "dev") {
		t.Fatalf("got %q", out)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(before, after) {
		t.Fatalf("reading the defaults rewrote config.yml:\n%s", after)
	}
}

func TestWorkspaceDefaultsWithNoFlagAsJSON(t *testing.T) {
	dir := configWithKey(t, "defaults:\n  workspace: [workspace, dev]\n")

	out, err := execute(t, "--config", dir, "--format", "json", "workspaces", "defaults")
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Packages []string `json:"packages"`
		Changed  bool     `json:"changed"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not JSON: %v (%q)", err, out)
	}
	if !slices.Equal(got.Packages, []string{"workspace", "dev"}) || got.Changed {
		t.Fatalf("got %s", out)
	}
}

func TestWorkspaceDefaultsEmptyIsAnEmptyList(t *testing.T) {
	dir := configWithKey(t, "")

	out, err := execute(t, "--config", dir, "--format", "json", "workspaces", "defaults")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"packages": []`) {
		t.Fatalf("an empty default list is not []: %s", out)
	}

	out, err = execute(t, "--config", dir, "workspaces", "defaults")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "no package") {
		t.Fatalf("an empty list should say so: %q", out)
	}
}

func TestWorkspacesListAsJSONSaysWhichLoginsAreShared(t *testing.T) {
	dir := configWithKey(t, "workspaces:\n  - name: alice\n    machine: main\n"+
		"    credentials:\n      claude: own\n  - name: bob\n    machine: main\n")

	out, err := execute(t, "--config", dir, "--format", "json", "workspaces", "list")
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Workspaces []map[string]any `json:"workspaces"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not JSON: %v (%q)", err, out)
	}
	alice, bob := got.Workspaces[0], got.Workspaces[1]
	if alice["user"] != "alice" {
		t.Fatalf("user is missing: %s", out)
	}
	if creds, ok := alice["credentials"].(map[string]any); !ok || creds["claude"] != "own" {
		t.Fatalf("alice's own login is not reported: %s", out)
	}
	if creds, ok := bob["credentials"].(map[string]any); !ok || len(creds) != 0 {
		t.Fatalf("a workspace with no answer should report {}: %s", out)
	}
}
