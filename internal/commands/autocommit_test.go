package commands

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mydevmachine/devmachine/internal/repo"
)

// gitRepoDir turns a freshly written configuration directory into a real git
// repository, the state every command's AutoCommit call assumes once
// `devmachine setup git` has run — including the .gitignore that command
// always writes before anything else, so a key `machines add` generates on
// the way is ignored rather than refused.
func gitRepoDir(t *testing.T, dir string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(repo.GitIgnore()), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", dir, "init", "-q", "-b", "main").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	// A machine with no git identity cannot commit at all, and AutoCommit
	// swallows that by design — so without this the test reads an empty log
	// and blames the message.
	for _, kv := range [][2]string{{"user.email", "test@example.com"}, {"user.name", "test"}} {
		if out, err := exec.Command("git", "-C", dir, "config", kv[0], kv[1]).CombinedOutput(); err != nil {
			t.Fatalf("git config %s: %v\n%s", kv[0], err, out)
		}
	}
	return dir
}

// lastCommitMessage is the subject line of the most recent commit in dir.
func lastCommitMessage(t *testing.T, dir string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "log", "-1", "--format=%s").CombinedOutput()
	if err != nil {
		t.Fatalf("git log: %v\n%s", err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestWorkspacesNewCommitsWithItsOwnMessage(t *testing.T) {
	dir := gitRepoDir(t, configWithKey(t, "defaults:\n  workspace: [workspace]\n"))

	if _, err := execute(t, "--config", dir, "workspaces", "new", "alice", "--yes"); err != nil {
		t.Fatal(err)
	}

	if got := lastCommitMessage(t, dir); got != "chore(config): add workspace alice" {
		t.Fatalf("got %q", got)
	}
}

func TestWorkspacesRmCommitsWithItsOwnMessage(t *testing.T) {
	dir := gitRepoDir(t, configWithKey(t, "workspaces:\n  - name: alice\n    machine: main\n"))

	if _, err := execute(t, "--config", dir, "workspaces", "rm", "alice", "--yes"); err != nil {
		t.Fatal(err)
	}

	if got := lastCommitMessage(t, dir); got != "chore(config): remove workspace alice" {
		t.Fatalf("got %q", got)
	}
}

func TestMachinesAddCommitsWithItsOwnMessage(t *testing.T) {
	dir := gitRepoDir(t, writeConfigDir(t, "machines:\n  - name: main\n    hosts: [203.0.113.10]\n"))
	stubBootstrap(t, bootstrapStubs{keyWorks: true})

	out, err := executeWithInput(t, "sandbox\n198.51.100.7\nroot\n22\n\n1\n", "--config", dir, "machines", "add")
	if err != nil {
		t.Fatalf("machines add returned %v (%s)", err, out)
	}

	if got := lastCommitMessage(t, dir); got != "chore(config): add machine sandbox" {
		t.Fatalf("got %q", got)
	}
}

func TestMachinesRmCommitsWithItsOwnMessage(t *testing.T) {
	dir := gitRepoDir(t, writeConfigDir(t,
		"machines:\n  - name: main\n    hosts: [203.0.113.10]\n  - name: sandbox\n    hosts: [198.51.100.7]\n"))

	if _, err := execute(t, "--config", dir, "machines", "rm", "sandbox", "--yes"); err != nil {
		t.Fatal(err)
	}

	if got := lastCommitMessage(t, dir); got != "chore(config): remove machine sandbox" {
		t.Fatalf("got %q", got)
	}
}

func TestPackagesAddCommitsWithItsOwnMessage(t *testing.T) {
	dir := gitRepoDir(t, writeConfigDir(t, "machines:\n  - name: main\n    hosts: [203.0.113.10]\n"))

	if _, err := execute(t, "--config", dir, "packages", "add", "docker", "--machine", "main", "--yes"); err != nil {
		t.Fatal(err)
	}

	if got := lastCommitMessage(t, dir); got != "chore(config): add package docker" {
		t.Fatalf("got %q", got)
	}
}

func TestSyncCommitsTheLockWithItsOwnMessage(t *testing.T) {
	stubSync(t)
	dir := gitRepoDir(t, configWithPackages(t))

	if _, err := execute(t, "--config", dir, "sync", "--yes"); err != nil {
		t.Fatal(err)
	}

	if got := lastCommitMessage(t, dir); got != "chore(config): lock packages for main" {
		t.Fatalf("got %q", got)
	}
}

func TestAutoCommitDoesNotTouchAConfigurationThatIsNotARepository(t *testing.T) {
	// Most people never run `setup git`. It must cost them nothing.
	dir := writeConfigDir(t, "machines:\n  - name: main\n    hosts: [203.0.113.10]\n")

	if _, err := execute(t, "--config", dir, "packages", "add", "docker", "--machine", "main", "--yes"); err != nil {
		t.Fatal(err)
	}

	if _, err := exec.Command("git", "-C", dir, "rev-parse", "--git-dir").CombinedOutput(); err == nil {
		t.Fatal("it made the directory a repository on its own")
	}
}
