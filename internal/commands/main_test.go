package commands

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/mydevmachine/devmachine/internal/config"
	"github.com/mydevmachine/devmachine/internal/remote"
	"github.com/mydevmachine/devmachine/internal/secrets"
	agentskills "github.com/mydevmachine/devmachine/internal/skills"
	"github.com/spf13/cobra"
	"golang.org/x/crypto/ssh"
)

// No test reaches the network by accident: a test that needs a release says
// which one with stubLatestPackagesRelease.
func TestMain(m *testing.M) {
	latestPackagesRelease = func(context.Context) (string, error) {
		return "", errors.New("tests do not reach GitHub")
	}
	latestCLIRelease = func(context.Context) (string, error) {
		return "", errors.New("tests do not reach GitHub")
	}
	releaseCacheDir = func() (string, error) {
		return "", errors.New("tests keep no cache of their own")
	}
	hintToTerminal = func(*cobra.Command) bool { return false }
	executablePath = func() (string, error) {
		return "", errors.New("tests never replace the binary running them")
	}
	findBrew = func() (string, error) { return "", errors.New("tests never run brew") }
	execBinary = func(string, []string) error { return errors.New("tests never start another binary") }
	localSkills = func() (agentskills.Installer, error) {
		return agentskills.Installer{}, errors.New("tests never read the real home")
	}
	os.Setenv(secrets.KeychainEnv, "off")
	aliasCLI = func() string { return "" }
	scanHostKeyMatching = func(context.Context, config.Machine, string, string) (ssh.PublicKey, error) {
		return nil, nil
	}
	// The test clients stand in for a root admin and run commands right here;
	// TestDialAdminRunsEverythingAsRoot puts the real one back.
	elevate = func(c remote.Client) remote.Client { return c }
	if err := isolateGitConfig(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

// isolateGitConfig points every `git` process this test binary spawns at a
// throwaway config, instead of the machine's own ~/.gitconfig.
//
// setup_git_test.go runs real `git init`/`git commit` against temporary
// directories, and the CLI's own repo.Commit deliberately leaves signing to
// the operator's config (see repo.Commit) — so without this, those commits
// pick up whatever the developer machine has set globally: a signing key
// that shells out to an external program (1Password's op-ssh-sign) and a
// trace2 target that hands every git invocation to a background daemon. Both
// add work a test repo never asked for, and under full-suite load that
// daemon can still be touching a repository's `.git` directory after the
// test function returns, racing `t.TempDir()`'s cleanup and failing it with
// "directory not empty" — intermittently, and only when other tests are
// running at the same time.
func isolateGitConfig() error {
	f, err := os.CreateTemp("", "devmachine-test-gitconfig")
	if err != nil {
		return fmt.Errorf("creating a test git config: %w", err)
	}
	defer f.Close()

	if _, err := f.WriteString("[user]\n\tname = devmachine tests\n\temail = tests@example.com\n"); err != nil {
		return fmt.Errorf("writing the test git config: %w", err)
	}

	os.Setenv("GIT_CONFIG_GLOBAL", f.Name())
	os.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
	os.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	return nil
}
