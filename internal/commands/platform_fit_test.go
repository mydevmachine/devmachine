package commands

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mydevmachine/devmachine/internal/config"
	"github.com/mydevmachine/devmachine/internal/facts"
	"github.com/mydevmachine/devmachine/internal/packages"
)

func needing(t *testing.T, configDir, name, needs string) {
	t.Helper()
	path := filepath.Join(packages.LocalDir(configDir), name, packages.FileName)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString("needs: " + needs + "\n"); err != nil {
		t.Fatal(err)
	}
}

// macWithWorkspacePackages is a machine last seen running macOS, a default
// workspace list, and the packages it names, one of them only for Linux.
func macWithWorkspacePackages(t *testing.T) string {
	t.Helper()
	dir := configWithKey(t, "defaults:\n  workspace: [workspace, zsh, remote-control]\n")
	for _, name := range []string{"workspace", "zsh", "remote-control"} {
		writeLocalPackage(t, dir, name, packages.ScopeWorkspace)
	}
	onlyOn(t, dir, "remote-control", "[linux]")
	saveFacts(t, dir, "main", observedMac)
	return dir
}

func workspacePackages(t *testing.T, dir, name string) []string {
	t.Helper()
	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	w, err := cfg.Workspace(name)
	if err != nil {
		t.Fatal(err)
	}
	return w.Packages
}

func TestWorkspacesNewLeavesOutADefaultPackageForAnotherSystem(t *testing.T) {
	dir := macWithWorkspacePackages(t)

	out, err := execute(t, "--config", dir, "workspaces", "new", "alice", "--yes")
	if err != nil {
		t.Fatal(err)
	}
	if got := workspacePackages(t, dir, "alice"); !slices.Equal(got, []string{"workspace", "zsh"}) {
		t.Fatalf("got %#v", got)
	}
	want := "remote-control runs only on linux and main is macos, so alice starts without it.\n"
	if !strings.Contains(out, want) {
		t.Fatalf("did not say why: %q", out)
	}
}

func TestWorkspacesNewLeavesOutAPackageWhoseNeedIsForAnotherSystem(t *testing.T) {
	dir := configWithKey(t, "defaults:\n  workspace: [workspace, agent]\n")
	writeLocalPackage(t, dir, "workspace", packages.ScopeWorkspace)
	writeLocalPackage(t, dir, "agent", packages.ScopeWorkspace)
	writeLocalPackage(t, dir, "systemd-unit", packages.ScopeWorkspace)
	needing(t, dir, "agent", "[systemd-unit]")
	onlyOn(t, dir, "systemd-unit", "[linux]")
	saveFacts(t, dir, "main", observedMac)

	out, err := execute(t, "--config", dir, "workspaces", "new", "alice", "--yes")
	if err != nil {
		t.Fatal(err)
	}
	if got := workspacePackages(t, dir, "alice"); !slices.Equal(got, []string{"workspace"}) {
		t.Fatalf("got %#v", got)
	}
	want := "agent needs systemd-unit, which runs only on linux, and main is macos, so alice starts without it.\n"
	if !strings.Contains(out, want) {
		t.Fatalf("did not say why: %q", out)
	}
}

func TestWorkspacesNewLikeLeavesOutAPackageForAnotherSystem(t *testing.T) {
	dir := configWithKey(t, "workspaces:\n  - name: bob\n    packages: [zsh, remote-control]\n")
	writeLocalPackage(t, dir, "zsh", packages.ScopeWorkspace)
	writeLocalPackage(t, dir, "remote-control", packages.ScopeWorkspace)
	onlyOn(t, dir, "remote-control", "[linux]")
	saveFacts(t, dir, "main", observedMac)

	if _, err := execute(t, "--config", dir, "workspaces", "new", "alice", "--like", "bob", "--yes"); err != nil {
		t.Fatal(err)
	}
	if got := workspacePackages(t, dir, "alice"); !slices.Equal(got, []string{"zsh"}) {
		t.Fatalf("got %#v", got)
	}
}

func TestWorkspacesNewRefusesAnAskedForPackageForAnotherSystem(t *testing.T) {
	dir := macWithWorkspacePackages(t)

	_, err := execute(t, "--config", dir, "workspaces", "new", "alice", "--packages", "zsh,remote-control", "--yes")
	want := `package "remote-control" runs only on linux and machine "main" is macos: leave it out of --packages`
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("got %v, want %q", err, want)
	}
	if cfg, _ := config.Load(dir); len(cfg.Workspaces) != 0 {
		t.Fatalf("the workspace was written: %#v", cfg.Workspaces)
	}
}

func TestWorkspacesNewKeepsEveryPackageWhenTheSystemIsUnknown(t *testing.T) {
	dir := configWithKey(t, "defaults:\n  workspace: [workspace, remote-control]\n")
	writeLocalPackage(t, dir, "remote-control", packages.ScopeWorkspace)
	onlyOn(t, dir, "remote-control", "[linux]")

	out, err := execute(t, "--config", dir, "workspaces", "new", "alice", "--yes")
	if err != nil {
		t.Fatal(err)
	}
	if got := workspacePackages(t, dir, "alice"); !slices.Equal(got, []string{"workspace", "remote-control"}) {
		t.Fatalf("got %#v", got)
	}
	if strings.Contains(out, "starts without") {
		t.Fatalf("dropped a package on a guess: %q", out)
	}
}

func TestWorkspacesNewKeepsAPackageForTheMachinesSystem(t *testing.T) {
	dir := macWithWorkspacePackages(t)
	saveFacts(t, dir, "main", facts.Facts{System: "Linux", Distribution: "Debian"})

	if _, err := execute(t, "--config", dir, "workspaces", "new", "alice", "--yes"); err != nil {
		t.Fatal(err)
	}
	if got := workspacePackages(t, dir, "alice"); !slices.Equal(got, []string{"workspace", "zsh", "remote-control"}) {
		t.Fatalf("got %#v", got)
	}
}

func TestPackagesAddRefusesAMachinePackageForAnotherSystem(t *testing.T) {
	dir := configWithMachineAndWorkspace(t)
	writeLocalPackage(t, dir, "docker", packages.ScopeMachine)
	onlyOn(t, dir, "docker", "[linux]")
	saveFacts(t, dir, "main", observedMac)

	_, err := execute(t, "--config", dir, "packages", "add", "docker", "--machine", "main", "--yes")
	want := `package "docker" runs only on linux and machine "main" is macos, so it was not added`
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("got %v, want %q", err, want)
	}
	if cfg, _ := config.Load(dir); len(cfg.Machines[0].Packages) != 0 {
		t.Fatalf("it was added: %#v", cfg.Machines[0].Packages)
	}
}

func TestPackagesAddRefusesAWorkspacePackageForItsMachinesSystem(t *testing.T) {
	dir := configWithMachineAndWorkspace(t)
	writeLocalPackage(t, dir, "remote-control", packages.ScopeWorkspace)
	onlyOn(t, dir, "remote-control", "[linux]")
	saveFacts(t, dir, "main", observedMac)

	_, err := execute(t, "--config", dir, "packages", "add", "remote-control", "--workspace", "alice", "--yes")
	want := `package "remote-control" runs only on linux and machine "main" is macos, so it was not added`
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("got %v, want %q", err, want)
	}
}

func TestPackagesAddTakesAPackageWhenTheSystemIsUnknown(t *testing.T) {
	dir := configWithMachineAndWorkspace(t)
	writeLocalPackage(t, dir, "docker", packages.ScopeMachine)
	onlyOn(t, dir, "docker", "[linux]")

	if _, err := execute(t, "--config", dir, "packages", "add", "docker", "--machine", "main", "--yes"); err != nil {
		t.Fatal(err)
	}
}

func TestPackagesRmTakesOffAPackageForAnotherSystem(t *testing.T) {
	dir := configWith(t, "machines:\n  - name: main\n    hosts: [203.0.113.10]\n    packages: [docker]\n")
	writeLocalPackage(t, dir, "docker", packages.ScopeMachine)
	onlyOn(t, dir, "docker", "[linux]")
	saveFacts(t, dir, "main", observedMac)

	if _, err := execute(t, "--config", dir, "packages", "rm", "docker", "--machine", "main", "--yes"); err != nil {
		t.Fatal(err)
	}
}

func TestWorkspacesEditRefusesToAddAPackageForAnotherSystem(t *testing.T) {
	dir := configWithKey(t, "workspaces:\n  - name: alice\n")
	writeLocalPackage(t, dir, "remote-control", packages.ScopeWorkspace)
	onlyOn(t, dir, "remote-control", "[linux]")
	saveFacts(t, dir, "main", observedMac)

	_, err := execute(t, "--config", dir, "workspaces", "edit", "alice", "--add", "remote-control", "--yes")
	want := `package "remote-control" runs only on linux and machine "main" is macos, so it was not added`
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("got %v, want %q", err, want)
	}
}
