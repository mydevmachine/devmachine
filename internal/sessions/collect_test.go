package sessions

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/mydevmachine/devmachine/internal/remote"
)

type fakeClient struct {
	out     string
	err     error
	command string
}

func (f *fakeClient) Run(_ context.Context, command string) (string, error) {
	f.command = command
	return f.out, f.err
}

func (f *fakeClient) RunInput(ctx context.Context, command string, _ io.Reader) (string, error) {
	return f.Run(ctx, command)
}

func (f *fakeClient) Stream(_ context.Context, _ string, stdout, _ io.Writer) error {
	if _, err := io.WriteString(stdout, f.out); err != nil {
		return err
	}
	return f.err
}

func (f *fakeClient) StreamInput(ctx context.Context, command string, _ io.Reader, stdout, stderr io.Writer) error {
	return f.Stream(ctx, command, stdout, stderr)
}

func (f *fakeClient) Upload(context.Context, string, io.Reader) error { return nil }

func (f *fakeClient) Close() error { return nil }

var _ remote.Client = (*fakeClient)(nil)

func TestCollectReturnsTheSessions(t *testing.T) {
	client := &fakeClient{out: lines(
		"@@NOW@@", "1000000",
		"@@SESSIONS@@",
		"web\t1\t990000\t999998\t\tclaude\t/home/alice/acme-web",
		"@@PANES@@",
		"web\tclaude\t10",
		"@@PS@@",
		"   10     1 claude",
		"@@GIT@@",
		"/home/alice/acme-web\tmain",
	)}
	got, err := Collect(context.Background(), client)
	if err != nil {
		t.Fatalf("Collect returned %v", err)
	}
	if len(got) != 1 || got[0].Name != "web" || got[0].Branch != "main" || !got[0].Busy {
		t.Errorf("Collect = %#v, want one busy session web on main", got)
	}
	if client.command != Script() {
		t.Errorf("Run got %q, want Script()", client.command)
	}
}

func TestCollectReportsMissingTmux(t *testing.T) {
	_, err := Collect(context.Background(), &fakeClient{out: "@@NO_TMUX@@\n"})
	if !errors.Is(err, ErrTmuxMissing) {
		t.Errorf("Collect error = %v, want ErrTmuxMissing", err)
	}
}

func TestCollectWrapsTheRunError(t *testing.T) {
	_, err := Collect(context.Background(), &fakeClient{err: remote.ErrHostKeyRejected})
	if !errors.Is(err, remote.ErrHostKeyRejected) {
		t.Errorf("Collect error = %v, want it to wrap ErrHostKeyRejected", err)
	}
}

func TestCollectFailsOnOutputWithoutAClock(t *testing.T) {
	_, err := Collect(context.Background(), &fakeClient{out: "garbage\n"})
	if !errors.Is(err, errNoClock) {
		t.Errorf("Collect error = %v, want missing machine clock", err)
	}
}
