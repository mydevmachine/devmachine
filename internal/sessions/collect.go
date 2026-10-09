// Package sessions reads a workspace's tmux sessions and what the sidebar
// shows about each: folder, branch, age, busy and attention.
package sessions

import (
	"context"
	"errors"
	"fmt"

	"github.com/mydevmachine/devmachine/internal/remote"
)

// ErrTmuxMissing means the machine has no tmux binary for this account.
var ErrTmuxMissing = errors.New("tmux is not installed on this machine")

// Collect runs Script on the workspace in one remote call and builds its sessions.
func Collect(ctx context.Context, client remote.Client) ([]Session, error) {
	out, err := client.Run(ctx, Script())
	if err != nil {
		return nil, fmt.Errorf("listing sessions: %w", err)
	}
	snap, err := Parse(out)
	if err != nil {
		return nil, fmt.Errorf("reading the session list: %w", err)
	}
	if snap.NoTmux {
		return nil, ErrTmuxMissing
	}
	return Build(snap), nil
}
