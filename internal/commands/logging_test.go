package commands

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mydevmachine/devmachine/internal/logfile"
)

// runLogged runs the CLI the way Execute does, log included, and returns
// stdout, stderr and the exit code.
func runLogged(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	root, opts := newRoot()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	code := run(root, opts, args)
	return stdout.String(), stderr.String(), code
}

func logLines(t *testing.T, dir string) []string {
	t.Helper()
	body, err := os.ReadFile(logfile.Path(dir))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimRight(string(body), "\n"), "\n")
}

func TestALoggedCommandThatFailsIsLoggedWholeAndPointsAtTheLog(t *testing.T) {
	dir := t.TempDir()
	SetVersion("1.2.3")
	t.Cleanup(func() { SetVersion("dev") })

	_, _, code := runLogged(t, "--config", dir, "doctor")
	if code != 1 {
		t.Fatalf("exit %d", code)
	}

	lines := logLines(t, dir)
	all := strings.Join(lines, "\n")
	if !strings.Contains(lines[0], " start  devmachine 1.2.3 ") || !strings.HasSuffix(lines[0], ": --config "+dir+" doctor") {
		t.Fatalf("first line is not the redacted start line: %q", lines[0])
	}

	last := lines[len(lines)-1]
	if !strings.Contains(last, " end    failed (exit 1) after ") || !strings.Contains(last, "some checks failed") {
		t.Fatalf("last line is not the end line: %q", last)
	}
	if !strings.Contains(all, " out    fail  configuration ") {
		t.Fatalf("the command's output is not in the log:\n%s", all)
	}

	_, stderr, _ := runLogged(t, "--config", dir, "doctor")
	stderrLines := strings.Split(strings.TrimRight(stderr, "\n"), "\n")
	abs, _ := filepath.Abs(logfile.Path(dir))
	if got, want := stderrLines[len(stderrLines)-1], "log: "+abs; got != want {
		t.Fatalf("last stderr line %q, want %q", got, want)
	}
	if got := stderrLines[len(stderrLines)-2]; got != "error: some checks failed" {
		t.Fatalf("the error line moved: %q", got)
	}
}

func TestALoggedCommandThatSucceedsEndsOK(t *testing.T) {
	dir := t.TempDir()

	_, stderr, code := runLogged(t, "--config", dir, "secrets", "set", "OPENAI", "fake-value-1")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if strings.Contains(stderr, "log: ") {
		t.Fatalf("a success points at the log: %q", stderr)
	}

	lines := logLines(t, dir)
	all := strings.Join(lines, "\n")
	if strings.Contains(all, "fake-value-1") {
		t.Fatalf("the log holds the secret:\n%s", all)
	}
	if !strings.Contains(lines[0], "secrets set OPENAI ***") {
		t.Fatalf("start line: %q", lines[0])
	}
	if last := lines[len(lines)-1]; !strings.Contains(last, " end    ok after ") {
		t.Fatalf("end line: %q", last)
	}
}

func TestAPolledReadWritesNothingWhenItWorks(t *testing.T) {
	dir := t.TempDir()

	stdout, _, code := runLogged(t, "--config", dir, "config", "path", "--format", "json")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	var got struct{ Path string }
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is no longer plain JSON: %v\n%s", err, stdout)
	}
	if lines := logLines(t, dir); len(lines) != 0 {
		t.Fatalf("a polled read logged its success:\n%s", strings.Join(lines, "\n"))
	}
}

func TestAPolledReadThatFailsWritesOneLineAndNoPointer(t *testing.T) {
	dir := t.TempDir()

	_, stderr, code := runLogged(t, "--config", dir, "sessions", "--format", "json")
	if code == 0 {
		t.Fatal("sessions worked without a config.yml; the test needs a failure")
	}
	if strings.Contains(stderr, "log: ") {
		t.Fatalf("a polled read points at the log: %q", stderr)
	}

	lines := logLines(t, dir)
	if len(lines) != 1 {
		t.Fatalf("want one line, got %d:\n%s", len(lines), strings.Join(lines, "\n"))
	}
	if !strings.Contains(lines[0], " failed --config "+dir+" sessions --format json (exit 1) after ") ||
		!strings.Contains(lines[0], "config.yml") {
		t.Fatalf("line: %q", lines[0])
	}
}

func TestAPackagePollFromTheAppIsNotLoggedWhole(t *testing.T) {
	for _, args := range [][]string{
		{"run", "--package", appPackage, "--", "stats"},
		{"run", "--no-log", "--package", "github", "--", "status"},
	} {
		root, _ := newRoot()
		cmd, _, err := root.Find(args)
		if err != nil {
			t.Fatal(err)
		}
		if err := cmd.ParseFlags(args[1:]); err != nil {
			t.Fatal(err)
		}
		if got := logModeFor(cmd); got != logFailures {
			t.Fatalf("%q: mode %v, want failures only", args, got)
		}
	}

	root, _ := newRoot()
	args := []string{"run", "--package", "github", "--", "login"}
	cmd, _, _ := root.Find(args)
	_ = cmd.ParseFlags(args[1:])
	if got := logModeFor(cmd); got != logEverything {
		t.Fatalf("run --package github: mode %v, want everything", got)
	}
}

func TestEveryCommandThatChangesAMachineIsLoggedWhole(t *testing.T) {
	for _, path := range []string{
		"setup", "machines add", "sync", "update", "doctor", "packages add",
		"packages remove", "login", "credentials push",
	} {
		root, _ := newRoot()
		cmd, _, err := root.Find(strings.Fields(path))
		if err != nil {
			t.Fatal(err)
		}
		if got := logModeFor(cmd); got != logEverything {
			t.Fatalf("%s: mode %v, want everything", path, got)
		}
	}
}

func TestALogThatCannotBeWrittenChangesNothing(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, logfile.Folder), 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(dir, logfile.Folder), 0o700) })

	_, stderr, code := runLogged(t, "--config", dir, "secrets", "set", "OPENAI", "fake-value-2")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}

	_, stderr, code = runLogged(t, "--config", dir, "doctor")
	if code != 1 {
		t.Fatalf("exit %d", code)
	}
	if strings.Contains(stderr, "log: ") {
		t.Fatalf("points at a log that was never written: %q", stderr)
	}
	if !strings.HasSuffix(stderr, "error: some checks failed\n") {
		t.Fatalf("stderr: %q", stderr)
	}
}

func TestLogsPathPrintsTheLog(t *testing.T) {
	dir := t.TempDir()

	out, err := execute(t, "--config", dir, "logs", "path")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(out); got != logfile.Path(dir) {
		t.Fatalf("got %q", got)
	}

	out, err = execute(t, "--config", dir, "logs", "path", "--format", "json")
	if err != nil {
		t.Fatal(err)
	}
	var got struct{ Path string }
	if err := json.Unmarshal([]byte(out), &got); err != nil || got.Path != logfile.Path(dir) {
		t.Fatalf("got %q (%v)", out, err)
	}
}

func TestALoggedCommandStillCommitsInARepositoryWhoseIgnoreFileLacksTheLogs(t *testing.T) {
	dir := gitRepoDir(t, configWithKey(t, "defaults:\n  workspace: [workspace]\n"))
	mustWrite(t, filepath.Join(dir, ".gitignore"), "keys/\nsecrets.json\ncache/\nhistory.log\n")

	_, stderr, code := runLogged(t, "--config", dir, "workspaces", "new", "alice", "--yes")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}

	if got := lastCommitMessage(t, dir); got != "chore(config): add workspace alice" {
		t.Fatalf("got %q; stderr: %s", got, stderr)
	}
	for _, f := range trackedFiles(t, dir) {
		if strings.HasPrefix(f, "logs/") {
			t.Fatalf("the log is tracked: %s", f)
		}
	}
	if len(logLines(t, dir)) < 2 {
		t.Fatal("the run was not logged")
	}
}
