package commands

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"syscall"
)

// unreachedExit is what run exits with when the command never ran — no such
// machine, no connection, a changed host key, a package not installed. It is
// the code ssh itself uses, so a program can tell "never got there" from the
// command's own failure.
const unreachedExit = 255

// runResult turns what a run returned into what it should cost: the
// command's own exit code when it ran and failed, 255 when it never ran.
func runResult(err error) error {
	if err == nil {
		return nil
	}
	if code, ok := commandExitCode(err); ok {
		return &exitError{code: code, err: err, quiet: true}
	}
	return &exitError{code: unreachedExit, err: err}
}

// commandExitCode finds the remote command's exit status inside err. The
// system ssh and a local shell report it as ExitCode, the Go SSH client as
// ExitStatus; a command killed by a signal has none.
func commandExitCode(err error) (int, bool) {
	var exit interface{ ExitCode() int }
	if errors.As(err, &exit) && exit.ExitCode() > 0 {
		return exit.ExitCode(), true
	}
	var status interface{ ExitStatus() int }
	if errors.As(err, &status) && status.ExitStatus() > 0 {
		return status.ExitStatus(), true
	}
	return 0, false
}

// stopSignals is the context a run lives in. SIGINT or SIGTERM cancels it,
// which kills the ssh or shell child: when the app stops a widget's stream,
// nothing is left running on this computer. A seam, so a test can end a run
// without signalling its own process.
var stopSignals = notifyStop

func notifyStop(parent context.Context) (context.Context, context.CancelFunc) {
	return signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
}
