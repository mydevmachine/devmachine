package packages

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// gitRun runs git in dir. GIT_TERMINAL_PROMPT=0 makes a private repository
// fail with git's own message instead of waiting for a password nobody types.
func gitRun(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
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
// folder, and returns that commit. It does not check the address:
// ParseGitAddress does, before anything calls this.
func Clone(ctx context.Context, address, ref, into string) (string, error) {
	where := address
	if ref != "" {
		where += "@" + ref
	}
	if err := os.MkdirAll(into, 0o755); err != nil {
		return "", fmt.Errorf("making %s: %w", into, err)
	}
	want := ref
	if want == "" {
		want = "HEAD"
	}
	for _, args := range [][]string{
		{"init", "-q"},
		{"fetch", "-q", "--depth", "1", "--no-tags", address, want},
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
