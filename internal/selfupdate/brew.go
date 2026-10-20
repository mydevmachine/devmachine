package selfupdate

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/mydevmachine/devmachine/internal/tty"
)

// Brew runs Homebrew. Path is the brew binary; Env, when set, is the whole
// environment it runs with.
type Brew struct {
	Path   string
	Env    []string
	Stdout io.Writer
	Stderr io.Writer
}

// Upgrade upgrades the CLI through the tap, the way install.sh installs it.
//
// `brew update` comes first so the tap knows the new release. When it fails
// (offline mirror, a tap nobody can fetch) the upgrade is still tried: it
// may already know. `brew trust` exists only on newer Homebrew, which
// refuses an untrusted third-party tap without it.
func (b Brew) Upgrade(ctx context.Context) error {
	if err := b.run(ctx, b.Stdout, "update"); err != nil {
		fmt.Fprintf(b.Stderr, "brew update failed (%v); trying the upgrade anyway\n", err)
	}
	if b.run(ctx, io.Discard, "trust", "--help") == nil {
		if err := b.run(ctx, b.Stdout, "trust", "--formula", Formula); err != nil {
			fmt.Fprintf(b.Stderr, "brew trust failed (%v); trying the upgrade anyway\n", err)
		}
	}
	if err := b.run(ctx, b.Stdout, "upgrade", Formula); err != nil {
		return fmt.Errorf("brew upgrade %s: %w", Formula, err)
	}
	return nil
}

// Prefix is what `brew --prefix` says.
func (b Brew) Prefix(ctx context.Context) (string, error) {
	out := &strings.Builder{}
	if err := b.run(ctx, out, "--prefix"); err != nil {
		return "", fmt.Errorf("brew --prefix: %w", err)
	}
	return strings.TrimSpace(out.String()), nil
}

func (b Brew) run(ctx context.Context, stdout io.Writer, args ...string) error {
	cmd := exec.CommandContext(ctx, b.Path, args...)
	if b.Env != nil {
		cmd.Env = b.Env
	}
	cmd.Stdout = stdout
	cmd.Stderr = b.Stderr
	if cmd.Stderr == nil {
		cmd.Stderr = io.Discard
	}
	return tty.Run(cmd)
}
