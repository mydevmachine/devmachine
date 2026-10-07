package commands

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/mydevmachine/devmachine/internal/config"
	"github.com/mydevmachine/devmachine/internal/remote"
)

type exitStatus int

func (e exitStatus) Error() string { return fmt.Sprintf("exit status %d", int(e)) }
func (e exitStatus) ExitCode() int { return int(e) }

type sshExitStatus int

func (e sshExitStatus) Error() string   { return fmt.Sprintf("Process exited with status %d", int(e)) }
func (e sshExitStatus) ExitStatus() int { return int(e) }

type waitingRemote struct{ fakeRemote }

func (waitingRemote) Stream(ctx context.Context, command string, _, _ io.Writer) error {
	<-ctx.Done()
	return fmt.Errorf("running %q: %w", command, ctx.Err())
}

func TestExitCode(t *testing.T) {
	cases := []struct {
		err  error
		want int
	}{
		{errors.New("plain"), 1},
		{&exitError{code: 7, err: errors.New("seven")}, 7},
		{fmt.Errorf("wrapped: %w", &exitError{code: 9, err: errors.New("nine")}), 9},
	}
	for _, tc := range cases {
		if got := exitCode(tc.err); got != tc.want {
			t.Errorf("%v: got %d, want %d", tc.err, got, tc.want)
		}
	}
}

func TestRunExitsWithTheCommandsOwnCode(t *testing.T) {
	for _, failure := range []error{exitStatus(3), sshExitStatus(3)} {
		dialing(t, fakeRemote{err: fmt.Errorf("running %q: %w", "false", failure)})
		dir := configWith(t, twoMachineConfig)
		_, err := execute(t, "--config", dir, "--machine", "main", "run", "false")
		if got := exitCode(err); got != 3 {
			t.Errorf("%T: exit %d from %v", failure, got, err)
		}
	}
}

func TestRunAddsNoErrorLineToTheCommandsOwnFailure(t *testing.T) {
	dialing(t, fakeRemote{err: fmt.Errorf("running %q: %w", "false", exitStatus(3))})
	dir := configWith(t, twoMachineConfig)
	_, err := execute(t, "--config", dir, "--machine", "main", "run", "false")
	if line, ok := errorLine(err); ok {
		t.Errorf("the command's own failure printed %q", line)
	}
}

func TestRunStillSaysWhyWhenTheCommandNeverRan(t *testing.T) {
	dir := configWith(t, twoMachineConfig)
	_, err := execute(t, "--config", dir, "--machine", "nowhere", "run", "true")
	line, ok := errorLine(err)
	if !ok || !strings.Contains(line, "nowhere") {
		t.Errorf("got %q, %v", line, ok)
	}
}

func TestErrorLine(t *testing.T) {
	if line, ok := errorLine(errors.New("plain")); !ok || line != "error: plain" {
		t.Errorf("plain: %q, %v", line, ok)
	}
	if _, ok := errorLine(&exitError{code: 3, err: errors.New("exit status 3"), quiet: true}); ok {
		t.Error("a quiet failure printed a line")
	}
}

func TestRunExits255WhenTheCommandNeverRan(t *testing.T) {
	dir := configWith(t, twoMachineConfig)

	_, err := execute(t, "--config", dir, "--machine", "nowhere", "run", "true")
	if got := exitCode(err); got != unreachedExit {
		t.Errorf("unknown machine: exit %d from %v", got, err)
	}

	dialMux = func(context.Context, config.Machine, string) (remote.Client, string, error) {
		return nil, "", errors.New("no route to host")
	}
	t.Cleanup(func() { dialMux = remote.DialMux })
	_, err = execute(t, "--config", dir, "--machine", "main", "run", "true")
	if got := exitCode(err); got != unreachedExit {
		t.Errorf("unreachable: exit %d from %v", got, err)
	}

	dialing(t, fakeRemote{})
	_, err = execute(t, "--config", configDirWithProviderAccepting(t, "cloudflare", []string{"zones"}),
		"run", "--package", "cloudflare", "--", "drop-everything")
	if got := exitCode(err); got != unreachedExit {
		t.Errorf("refused package command: exit %d from %v", got, err)
	}
}

func TestRunStopsWhenItsContextEnds(t *testing.T) {
	dialing(t, waitingRemote{})
	stopSignals = func(parent context.Context) (context.Context, context.CancelFunc) {
		return context.WithTimeout(parent, 20*time.Millisecond)
	}
	t.Cleanup(func() { stopSignals = notifyStop })
	dir := configWith(t, twoMachineConfig)

	done := make(chan error, 1)
	go func() {
		_, err := execute(t, "--config", dir, "--machine", "main", "run", "tail -f /var/log/syslog")
		done <- err
	}()
	select {
	case err := <-done:
		if got := exitCode(err); got != unreachedExit {
			t.Fatalf("exit %d from %v", got, err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("run kept going after its context ended")
	}
}
