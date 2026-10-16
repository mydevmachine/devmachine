package commands

import (
	"strings"
	"testing"

	"github.com/mydevmachine/devmachine/internal/packages"
)

func configWithTwoWorkspaces(t *testing.T) string {
	t.Helper()
	dir := configWith(t, `
machines:
  - name: main
    hosts: [203.0.113.10]
    packages: [base]
workspaces:
  - name: alice
    packages: [claude-code]
  - name: bob
    packages: [claude-code]
`)
	writeLocalPackage(t, dir, "base", packages.ScopeMachine)
	writeLocalPackage(t, dir, "claude-code", packages.ScopeWorkspace)
	return dir
}

func TestSyncForOneWorkspaceNarrowsThePlan(t *testing.T) {
	stub := stubSync(t)

	out, err := execute(t, "--config", configWithTwoWorkspaces(t), "sync", "--yes", "--workspace", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if stub.plan.OnlyWorkspace != "alice" {
		t.Fatalf("the run was not narrowed to alice: %q", stub.plan.OnlyWorkspace)
	}
	if !strings.Contains(out, "workspace alice") {
		t.Fatalf("the plan does not name alice:\n%s", out)
	}
	for _, unwanted := range []string{"machine main:", "workspace bob"} {
		if strings.Contains(out, unwanted) {
			t.Fatalf("the plan shows %q, which this run leaves alone:\n%s", unwanted, out)
		}
	}
}

func TestSyncForOneWorkspaceFindsItsMachine(t *testing.T) {
	stub := stubSync(t)
	dir := configWith(t, twoMachineConfig)

	if _, err := execute(t, "--config", dir, "sync", "--yes", "--workspace", "bob"); err != nil {
		t.Fatal(err)
	}
	if stub.plan.Machine.Name != "sandbox" {
		t.Fatalf("bob lives on sandbox, the run went to %q", stub.plan.Machine.Name)
	}
}

func TestSyncRefusesAWorkspaceOnAnotherMachine(t *testing.T) {
	stub := stubSync(t)
	dir := configWith(t, twoMachineConfig)

	_, err := execute(t, "--config", dir, "--machine", "main", "sync", "--yes", "--workspace", "bob")
	if err == nil || !strings.Contains(err.Error(), "sandbox") {
		t.Fatalf("got %v, want an error that says bob is on sandbox", err)
	}
	if stub.called {
		t.Fatal("it reached a machine anyway")
	}
}

func TestSyncRefusesAnUnknownWorkspace(t *testing.T) {
	stub := stubSync(t)

	_, err := execute(t, "--config", configWithTwoWorkspaces(t), "sync", "--yes", "--workspace", "carol")
	if err == nil || !strings.Contains(err.Error(), "carol") {
		t.Fatalf("got %v, want an error naming carol", err)
	}
	if stub.called {
		t.Fatal("it reached the machine anyway")
	}
}

func TestSyncForOneWorkspaceLocksOnlyThatWorkspace(t *testing.T) {
	stubSync(t)
	dir := configWithTwoWorkspaces(t)

	if _, err := execute(t, "--config", dir, "sync", "--yes", "--workspace", "alice"); err != nil {
		t.Fatal(err)
	}

	lock, err := packages.LoadLock(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(lock.Machines["main"]) != 0 {
		t.Fatalf("base is locked although it did not run: %#v", lock.Machines["main"])
	}
	if got := lock.Workspaces["alice"]; len(got) != 1 || got[0].Name != "claude-code" {
		t.Fatalf("alice's lock is %#v, want claude-code", got)
	}
	if _, ok := lock.Workspaces["bob"]; ok {
		t.Fatalf("bob is locked although nothing ran for him: %#v", lock.Workspaces["bob"])
	}
}

func TestSyncForOneWorkspaceIsInTheLog(t *testing.T) {
	stubSync(t)
	dir := configWithTwoWorkspaces(t)

	if _, err := execute(t, "--config", dir, "sync", "--check", "--workspace", "alice"); err != nil {
		t.Fatal(err)
	}
	if line := historyLines(t, dir)[0]; !strings.Contains(line, "sync --check --workspace alice") {
		t.Fatalf("the log line leaves out the workspace: %q", line)
	}
}
