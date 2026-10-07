package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mydevmachine/devmachine/internal/config"
	"github.com/mydevmachine/devmachine/internal/packages"
)

// writeReposPackage is a local workspace package with one typed list
// variable, the shape a workspace's repositories take.
func writeReposPackage(t *testing.T, dir string) {
	t.Helper()
	writeLocalPackage(t, dir, "repos", packages.ScopeWorkspace)
	manifest := filepath.Join(packages.LocalDir(dir), "repos", "package.yml")
	body := `format: 1
name: repos
scope: workspace
summary: Clones repositories.
variables:
  list:
    summary: What to clone.
    type: list
    default: []
    items:
      fields:
        name: {summary: The folder., required: true}
        url: {summary: The source., required: true}
  depth:
    summary: How deep to clone.
    type: number
    default: 0
`
	if err := os.WriteFile(manifest, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestWorkspacesEditRefusesASettingThatDoesNotFitItsType(t *testing.T) {
	dir := configWithKey(t, "workspaces:\n  - name: alice\n    machine: main\n    packages: [repos]\n")
	writeReposPackage(t, dir)

	_, err := execute(t, "--config", dir, "workspaces", "edit", "alice",
		"--set", "repos.list=[{name: app}]", "--yes")
	if err == nil || !strings.Contains(err.Error(), `repos.list[0]: "url" is required`) {
		t.Fatalf("got %v", err)
	}

	cfg, _ := config.Load(dir)
	w, _ := cfg.Workspace("alice")
	if len(w.Settings) != 0 {
		t.Fatalf("it wrote anyway: %#v", w.Settings)
	}
}

func TestWorkspacesEditWritesASettingThatFitsItsType(t *testing.T) {
	dir := configWithKey(t, "workspaces:\n  - name: alice\n    machine: main\n    packages: [repos]\n")
	writeReposPackage(t, dir)

	_, err := execute(t, "--config", dir, "workspaces", "edit", "alice",
		"--set", "repos.list=[{name: app, url: git@example.com:alice/app.git}]", "--yes")
	if err != nil {
		t.Fatal(err)
	}

	cfg, _ := config.Load(dir)
	w, _ := cfg.Workspace("alice")
	list, ok := w.Settings["repos.list"].([]any)
	if !ok || len(list) != 1 {
		t.Fatalf("got %#v", w.Settings["repos.list"])
	}
}

func TestMachinesEditRefusesASettingThatDoesNotFitItsType(t *testing.T) {
	dir := configWith(t, "machines:\n  - name: main\n    hosts: [203.0.113.10]\n    packages: [tuner]\n")
	writeLocalPackage(t, dir, "tuner", packages.ScopeMachine)
	manifest := filepath.Join(packages.LocalDir(dir), "tuner", "package.yml")
	body := "format: 1\nname: tuner\nscope: machine\nsummary: Tunes.\nvariables:\n  port:\n    summary: A port.\n    type: number\n    default: 80\n"
	if err := os.WriteFile(manifest, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := execute(t, "--config", dir, "machines", "edit", "main", "--set", "tuner.port=eighty", "--yes")
	if err == nil || !strings.Contains(err.Error(), `tuner.port: a number, got the string "eighty"`) {
		t.Fatalf("got %v", err)
	}
}

// A setting written by hand, or before the package gave the variable a type,
// is stopped before the machine is reached rather than by Ansible on it.
func TestSyncRefusesASettingThatDoesNotFitItsTypeBeforeReachingTheMachine(t *testing.T) {
	stub := stubSync(t)
	dir := configWith(t, `
machines:
  - name: main
    hosts: [203.0.113.10]
workspaces:
  - name: alice
    packages: [repos]
    settings:
      repos.list: [{name: app, url: https://example.com/app.git}]
      repos.depth: deep
`)
	writeReposPackage(t, dir)

	_, err := execute(t, "--config", dir, "sync", "--check")
	if err == nil || !strings.Contains(err.Error(), `workspace "alice": repos.depth: a number, got the string "deep"`) {
		t.Fatalf("got %v", err)
	}
	if stub.called {
		t.Fatal("it reached the machine")
	}
}

// A setting for a variable the package does not declare is the recipe's own
// business, as it was before types existed.
func TestWorkspacesEditLeavesAnUndeclaredVariableAlone(t *testing.T) {
	dir := configWithKey(t, "workspaces:\n  - name: alice\n    machine: main\n    packages: [repos]\n")
	writeReposPackage(t, dir)

	if _, err := execute(t, "--config", dir, "workspaces", "edit", "alice", "--set", "repos.extra=anything", "--yes"); err != nil {
		t.Fatal(err)
	}
}
