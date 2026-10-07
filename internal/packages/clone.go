package packages

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

// repositoryVars point git at a repository other than the one in dir. A hook
// or an alias that runs the CLI sets them, and git would then fetch into
// that repository instead.
var repositoryVars = []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_OBJECT_DIRECTORY",
	"GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_COMMON_DIR"}

// droppedVar says whether git must not inherit the variable name: the
// repository ones, and the config ones, which can set core.sshCommand or a
// hook path and so run a program while fetching somebody else's repository.
func droppedVar(name string) bool {
	return slices.Contains(repositoryVars, name) || name == "GIT_SSH_COMMAND" ||
		name == "GIT_CONFIG_PARAMETERS" || name == "GIT_CONFIG_COUNT" ||
		strings.HasPrefix(name, "GIT_CONFIG_KEY_") || strings.HasPrefix(name, "GIT_CONFIG_VALUE_")
}

// gitRun runs git in dir. GIT_TERMINAL_PROMPT=0 and ssh's BatchMode make a
// private repository, an unknown host or a locked key fail with git's own
// message instead of waiting for an answer nobody types, the app included.
func gitRun(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = slices.DeleteFunc(os.Environ(), func(kv string) bool {
		name, _, _ := strings.Cut(kv, "=")
		return droppedVar(name)
	})
	cmd.Env = append(cmd.Env, "GIT_TERMINAL_PROMPT=0", "GIT_SSH_COMMAND=ssh -o BatchMode=yes")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return "", errors.New("git is not installed: installing a package from a git address needs it")
		}
		if message := strings.TrimSpace(stderr.String()); message != "" {
			return "", fmt.Errorf("%w: %s", err, message)
		}
		return "", err
	}
	return stdout.String(), nil
}

// Clone fetches one commit of address — ref, or its default branch when ref
// is empty — into the new folder into, without its history or its .git
// folder, and returns that commit. into must not exist yet, and is removed
// again when anything fails, so a caller never finds half a package there.
// It does not check the address: ParseGitAddress does, before anything calls
// this.
func Clone(ctx context.Context, address, ref, into string) (commit string, err error) {
	where := address
	if ref != "" {
		where += "@" + ref
	}
	if err := os.MkdirAll(filepath.Dir(into), 0o755); err != nil {
		return "", fmt.Errorf("making %s: %w", filepath.Dir(into), err)
	}
	if err := os.Mkdir(into, 0o755); err != nil {
		if errors.Is(err, os.ErrExist) {
			return "", fmt.Errorf("fetching %s: %s already exists", where, into)
		}
		return "", fmt.Errorf("making %s: %w", into, err)
	}
	defer func() {
		if err != nil {
			_ = os.RemoveAll(into)
		}
	}()
	return fetchInto(ctx, address, ref, where, into)
}

func fetchInto(ctx context.Context, address, ref, where, into string) (string, error) {
	want := ref
	if want == "" {
		want = "HEAD"
	}
	for _, args := range [][]string{
		{"init", "-q"},
		{"fetch", "-q", "--depth", "1", "--no-tags", "--", address, want},
		{"-c", "advice.detachedHead=false", "checkout", "-q", "--detach", "FETCH_HEAD"},
	} {
		if _, err := gitRun(ctx, into, args...); err != nil {
			return "", fmt.Errorf("fetching %s: %w", where, err)
		}
	}
	out, err := gitRun(ctx, into, "rev-parse", "HEAD")
	if err != nil {
		return "", fmt.Errorf("reading the commit of %s: %w", where, err)
	}
	if err := os.RemoveAll(filepath.Join(into, ".git")); err != nil {
		return "", fmt.Errorf("dropping the history of %s: %w", where, err)
	}
	return strings.TrimSpace(out), nil
}
