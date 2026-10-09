package sessions

import (
	"context"
	"errors"
	"fmt"

	"github.com/mydevmachine/devmachine/internal/remote"
)

var ErrTmuxMissing = errors.New("tmux is not installed on this machine")

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
