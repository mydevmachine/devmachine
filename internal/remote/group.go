package remote

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"
)

type ownGroupKey struct{}

// OwnProcessGroup marks ctx so a command a self machine runs under it gets a
// process group of its own, and the end of ctx stops everything the command
// started, not only bash. Only `run` asks for it: a group of its own leaves
// the terminal's foreground group, so Ctrl-C no longer reaches it directly and
// a program reading the terminal stops.
func OwnProcessGroup(ctx context.Context) context.Context {
	return context.WithValue(ctx, ownGroupKey{}, true)
}

func wantsOwnGroup(ctx context.Context) bool {
	own, _ := ctx.Value(ownGroupKey{}).(bool)
	return own
}

// groupGrace is how long a stopped run's processes get to end on SIGTERM
// before SIGKILL. A variable so a test does not wait for it.
var groupGrace = 5 * time.Second

func localCommand(ctx context.Context, command string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "/bin/bash", "-c", command)
	if wantsOwnGroup(ctx) {
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		cmd.Cancel = func() error { return signalGroup(cmd.Process.Pid, syscall.SIGTERM) }
		cmd.WaitDelay = groupGrace
	}
	return cmd
}

func signalGroup(pgid int, sig syscall.Signal) error {
	err := syscall.Kill(-pgid, sig)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, syscall.ESRCH):
		return os.ErrProcessDone
	default:
		return fmt.Errorf("signalling process group %d: %w", pgid, err)
	}
}

// runLocal runs cmd and, when ctx ended it, waits for what is left of the
// command's own group — a child that outlived bash — and kills it when the
// grace that began with the SIGTERM is over, so WaitDelay and this share one
// grace. Polling is safe: a group id is not reused while one member is alive.
func runLocal(ctx context.Context, cmd *exec.Cmd, run func() error) error {
	if !wantsOwnGroup(ctx) {
		return run()
	}
	ended := make(chan time.Time, 1)
	stopWatching := context.AfterFunc(ctx, func() { ended <- time.Now() })
	defer stopWatching()

	err := run()
	if ctx.Err() == nil || cmd.Process == nil {
		return err
	}
	pgid := cmd.Process.Pid
	deadline := (<-ended).Add(groupGrace)
	for syscall.Kill(-pgid, 0) == nil {
		if time.Now().After(deadline) {
			_ = syscall.Kill(-pgid, syscall.SIGKILL)
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	return err
}
