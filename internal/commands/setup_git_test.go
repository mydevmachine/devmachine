package commands

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mydevmachine/devmachine/internal/config"
)

// fakeGhClient stands in for the `gh` binary, so a test proves what the
// command decided rather than what a real `gh` happened to do.
type fakeGhClient struct {
	calls         [][]string
	authenticated bool
}

func (g *fakeGhClient) run(_ context.Context, _ string, args ...string) (string, error) {
	g.calls = append(g.calls, args)
	if args[0] == "auth" && !g.authenticated {
		return "", errTestGhNotLoggedIn
	}
	return "", nil
}

func (g *fakeGhClient) lastCall(prefix ...string) []string {
	for i := len(g.calls) - 1; i >= 0; i-- {
		if len(g.calls[i]) < len(prefix) {
			continue
		}
		if slices.Equal(g.calls[i][:len(prefix)], prefix) {
			return g.calls[i]
		}
	}
	return nil
}

var errTestGhNotLoggedIn = &ghNotLoggedInError{}

type ghNotLoggedInError struct{}

func (*ghNotLoggedInError) Error() string { return "not logged in" }

// installFakeGh makes `gh` look present, authenticated, and driven by the
// fake instead of a real process.
func installFakeGh(t *testing.T, authenticated bool) *fakeGhClient {
	t.Helper()
	g := &fakeGhClient{authenticated: authenticated}

	t.Cleanup(swap(&ghOnPath, func() bool { return true }))
	t.Cleanup(swap(&runGh, g.run))
	return g
}

// withoutGh makes `gh` look absent, whatever is really on this machine's PATH.
func withoutGh(t *testing.T) {
	t.Helper()
	t.Cleanup(swap(&ghOnPath, func() bool { return false }))
}

// configDirWithSecrets is a configuration directory with everything that must
// never reach a commit already sitting beside the two files that may.
func configDirWithSecrets(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "config.yml"), "machines: {}\n")
	mustWrite(t, filepath.Join(dir, "packages.lock"), "{}\n")
	mustWrite(t, filepath.Join(dir, config.KnownHostsFileName), "main-devmachine ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITest\n")
	if err := os.MkdirAll(filepath.Join(dir, "keys"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(dir, "keys", "id_ed25519"), "PRIVATE KEY\n")
	mustWrite(t, filepath.Join(dir, "secrets.json"), `{"t":"s3cret"}`+"\n")
	return dir
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// git runs a real git command against dir, for a test to inspect what the
// command actually did on disk, rather than what it printed.
func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append([]string{"-C", dir}, args...)
	out, err := exec.Command("git", full...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

// trackedFiles is what git actually follows in dir.
func trackedFiles(t *testing.T, dir string) []string {
	t.Helper()
	out := strings.TrimSpace(git(t, dir, "ls-files"))
	if out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

// gitFirstCommitFiles is what the repository's very first commit carries.
func gitFirstCommitFiles(t *testing.T, dir string) []string {
	t.Helper()
	first := strings.TrimSpace(git(t, dir, "rev-list", "--max-parents=0", "HEAD"))
	out := strings.TrimSpace(git(t, dir, "show", "--name-only", "--format=", first))
	if out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

// repoAlreadyTracking is a directory that became a git repository by hand,
// before `setup git` existed, and already tracks the given path.
func repoAlreadyTracking(t *testing.T, path string) string {
	t.Helper()
	dir := configDirWithSecrets(t)
	buf := &bytes.Buffer{}
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"add", "-A"},
		{"-c", "commit.gpgsign=false", "-c", "user.email=test@example.com", "-c", "user.name=test",
			"commit", "-q", "-m", "by hand"},
	} {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Stdout, cmd.Stderr = buf, buf
		if err := cmd.Run(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, buf.String())
		}
	}
	if !slices.Contains(trackedFiles(t, dir), path) {
		t.Fatalf("fixture does not track %q: %#v", path, trackedFiles(t, dir))
	}
	return dir
}

func TestSetupGitWritesTheIgnoreFileBeforeInit(t *testing.T) {
	// Written after, `git add -A` has a window in which it picks up the
	// private key. The window is short and the consequence is permanent.
	dir := configDirWithSecrets(t)
	withoutGh(t)

	if _, err := execute(t, "--config", dir, "setup", "git", "--yes"); err != nil {
		t.Fatal(err)
	}

	files := gitFirstCommitFiles(t, dir)
	if !slices.Contains(files, ".gitignore") {
		t.Fatalf("the very first commit does not carry .gitignore: %#v", files)
	}
}

func TestSetupGitCommitsThePublicTrustStoreButNotThePrivateKey(t *testing.T) {
	dir := configDirWithSecrets(t)
	withoutGh(t)

	if _, err := execute(t, "--config", dir, "setup", "git", "--yes"); err != nil {
		t.Fatal(err)
	}

	got := trackedFiles(t, dir)
	want := []string{".gitignore", "config.yml", "known_hosts", "packages.lock"}
	if !slices.Equal(got, want) {
		t.Fatalf("tracked %#v, want %#v", got, want)
	}
}

func TestSetupGitCommitsAgentsMd(t *testing.T) {
	dir := configDirWithSecrets(t)
	mustWrite(t, filepath.Join(dir, agentsFileName), agentsTemplate)
	withoutGh(t)

	if _, err := execute(t, "--config", dir, "setup", "git", "--yes"); err != nil {
		t.Fatal(err)
	}

	if !slices.Contains(trackedFiles(t, dir), agentsFileName) {
		t.Fatalf("the first commit does not carry %s: %#v", agentsFileName, trackedFiles(t, dir))
	}
}

func TestSetupGitStopsOnADirectoryThatAlreadyTracksAKey(t *testing.T) {
	dir := repoAlreadyTracking(t, "keys/id_ed25519")
	withoutGh(t)

	_, err := execute(t, "--config", dir, "setup", "git", "--yes")
	if err == nil {
		t.Fatal("it carried on with a tracked private key")
	}
	if !strings.Contains(err.Error(), "git rm --cached") {
		t.Fatalf("the error does not say how to fix it: %v", err)
	}
	if !strings.Contains(err.Error(), "rotate") {
		// Untracking does not remove it from the history that already has it.
		t.Fatalf("the error does not say the key is still compromised: %v", err)
	}
}

func TestSetupGitCreatesAPrivateRepository(t *testing.T) {
	dir := configDirWithSecrets(t)
	gh := installFakeGh(t, true)

	// Answered, not assumed: creating a repository always asks, so the yes
	// has to come from a person at a terminal.
	t.Cleanup(swap(&fromATerminal, func(io.Reader) bool { return true }))

	if _, err := executeWithInput(t, "y\n", "--config", dir, "setup", "git"); err != nil {
		t.Fatal(err)
	}

	args := gh.lastCall("repo", "create")
	if args == nil {
		t.Fatal("gh repo create was never called")
	}
	if !slices.Contains(args, "--private") {
		t.Fatalf("it would have created a public repository: %#v", args)
	}
}

func TestSetupGitSaysWhatToRunWithoutGh(t *testing.T) {
	dir := configDirWithSecrets(t)
	withoutGh(t)

	out, err := execute(t, "--config", dir, "setup", "git", "--yes")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "git remote add origin") {
		t.Fatalf("got %q", out)
	}
}

func TestSetupGitAsksBeforeCreatingARepository(t *testing.T) {
	dir := configDirWithSecrets(t)
	gh := installFakeGh(t, true)

	out, err := executeWithInput(t, "n\n", "--config", dir, "setup", "git")
	if err != nil {
		t.Fatal(err)
	}
	if gh.lastCall("repo", "create") != nil {
		t.Fatal("it created a repository after being told no")
	}
	if !strings.Contains(out, "private") {
		t.Fatalf("got %q", out)
	}
}

func TestSetupGitWritesNothingUnderCheck(t *testing.T) {
	dir := configDirWithSecrets(t)
	withoutGh(t)

	if _, err := execute(t, "--config", dir, "setup", "git", "--check"); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
		t.Fatal("it created a repository under --check")
	}
	if _, err := os.Stat(filepath.Join(dir, ".gitignore")); err == nil {
		t.Fatal("it wrote .gitignore under --check")
	}
}

func TestSetupGitSkipsInitWhenAlreadyARepository(t *testing.T) {
	dir := configDirWithSecrets(t)
	withoutGh(t)

	if _, err := execute(t, "--config", dir, "setup", "git", "--yes"); err != nil {
		t.Fatal(err)
	}
	// Running it again must not fail on an existing repository, and must not
	// recreate .gitignore from scratch.
	if _, err := execute(t, "--config", dir, "setup", "git", "--yes"); err != nil {
		t.Fatal(err)
	}
}

func TestSetupGitNeverCreatesARepositoryUnderYes(t *testing.T) {
	// --yes means "do not ask me about the writes in this directory". It has
	// never meant "publish". Creating a repository on somebody's account is
	// not the same class of action as writing a local file, and one flag
	// covering both is how a repository gets created by a script nobody was
	// watching.
	dir := configDirWithSecrets(t)
	gh := installFakeGh(t, true)

	out, err := execute(t, "--config", dir, "setup", "git", "--yes")
	if err != nil {
		t.Fatal(err)
	}
	if gh.lastCall("repo", "create") != nil {
		t.Fatalf("--yes created a repository:\n%s", out)
	}
	if len(gh.calls) != 0 {
		t.Fatalf("--yes invoked gh before declining to publish: %#v", gh.calls)
	}
	if !strings.Contains(out, "git remote add origin") {
		t.Fatalf("it neither created one nor said how to:\n%s", out)
	}
}

func TestSetupGitYesHelpDoesNotPromiseToPublish(t *testing.T) {
	out, err := execute(t, "setup", "git", "--help")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "create the private remote") || strings.Contains(out, "push without asking") {
		t.Fatalf("--yes promises a remote the command deliberately never creates:\n%s", out)
	}
	if !strings.Contains(out, "local writes") || !strings.Contains(out, "never create or push a remote") {
		t.Fatalf("--yes does not state its boundary:\n%s", out)
	}
}

func TestSetupGitWritesTheIgnoreFileIntoADirectoryThatIsAlreadyARepository(t *testing.T) {
	// The conversion case: the configuration directory has been a git
	// repository for months. Skipping the ignore file there leaves the CLI
	// writing keys/ and secrets.json into a tree that tracks everything.
	dir := configDirWithSecrets(t)
	withoutGh(t)
	git(t, dir, "init", "-q", "-b", "main")

	if _, err := execute(t, "--config", dir, "setup", "git", "--yes"); err != nil {
		t.Fatal(err)
	}

	body, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatalf("no .gitignore in a directory that was already a repository: %v", err)
	}
	for _, want := range []string{"keys/", "secrets.json", "cache/", "history.log", "*.env", "state/", "packages/.install-*"} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("the ignore file leaves out %q:\n%s", want, body)
		}
	}
}

func TestSetupGitKeepsTheOperatorsOwnIgnoreRules(t *testing.T) {
	// A rule somebody wrote is not ours to drop.
	dir := configDirWithSecrets(t)
	withoutGh(t)
	git(t, dir, "init", "-q", "-b", "main")
	mustWrite(t, filepath.Join(dir, ".gitignore"), "notes.md\n")

	if _, err := execute(t, "--config", dir, "setup", "git", "--yes"); err != nil {
		t.Fatal(err)
	}

	body, _ := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if !strings.Contains(string(body), "notes.md") {
		t.Fatalf("it dropped a rule it did not write:\n%s", body)
	}
	if !strings.Contains(string(body), "secrets.json") {
		t.Fatalf("it did not add the rules that were missing:\n%s", body)
	}
}

func TestSetupGitDoesNotAskWhereNobodyCanAnswer(t *testing.T) {
	// Always asking is right; blocking on the answer is not. With stdin not a
	// terminal — a script, CI, a background job — nobody can type y, and the
	// question waits for an answer that never comes. `setup git --yes` hung
	// exactly like this the first time this rule was written.
	gh := installFakeGh(t, true)
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")

	out := &bytes.Buffer{}
	if err := ensureRemote(t.Context(), dir, strings.NewReader("y\n"), out, false); err != nil {
		t.Fatal(err)
	}

	if gh.lastCall("repo", "create") != nil {
		t.Fatalf("it created a repository with nobody at a terminal:\n%s", out)
	}
	if !strings.Contains(out.String(), "git remote add origin") {
		t.Fatalf("it did not say how to add a remote by hand:\n%s", out)
	}
}
