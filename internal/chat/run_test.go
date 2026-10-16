package chat

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mydevmachine/devmachine/internal/chat/chattest"
	"github.com/mydevmachine/devmachine/internal/config"
	"github.com/mydevmachine/devmachine/internal/remote"
)

func localClient(t *testing.T) remote.Client {
	t.Helper()
	c, _, err := remote.Dial(context.Background(), config.Machine{Name: "here", Self: true}, "")
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func collect(events *[]Event) func(Event) {
	return func(e Event) { *events = append(*events, e) }
}

func TestScriptKeepsTheMessageAwayFromAnyShellButSh(t *testing.T) {
	script := Script(Request{Harness: Claude, Folder: "~/chats", Message: "it's $HOME `x` \\n"})
	if !strings.Contains(script, `'it'\''s $HOME `+"`x`"+` \n'`) {
		t.Fatalf("message not quoted:\n%s", script)
	}
	if !strings.Contains(script, `"$HOME"/'chats'`) {
		t.Fatalf("folder not expanded:\n%s", script)
	}
	if !strings.Contains(script, "</dev/null") {
		t.Fatalf("stdin not closed:\n%s", script)
	}
}

func TestRunStreamsTheAnswer(t *testing.T) {
	home := chattest.Isolate(t)
	fake := chattest.Install(t)
	message := "it's \"quoted\" $HOME `x` back\\slash"
	var events []Event
	out := Run(context.Background(), localClient(t), Request{
		Harness: Claude, Folder: "~/chats", Message: message, PathPrefix: []string{fake.Dir},
	}, collect(&events))

	if out.Failure != nil {
		t.Fatalf("failure = %+v", out.Failure)
	}
	if out.SessionID != "22222222-aaaa-4bbb-8ccc-000000000001" {
		t.Fatalf("session = %q", out.SessionID)
	}
	if out.Text != "echo: "+message {
		t.Fatalf("text = %q", out.Text)
	}
	if len(out.Tools) != 1 || out.Tools[0].Summary != "notes.md" {
		t.Fatalf("tools = %+v", out.Tools)
	}
	args := strings.Split(strings.TrimSuffix(fake.Read(t, "args"), "\n"), "\n")
	if args[len(args)-1] != message || args[len(args)-2] != "--" {
		t.Fatalf("args = %q", args)
	}
	if got := strings.TrimSpace(fake.Read(t, "cwd")); got != filepath.Join(home, "chats") {
		real, _ := filepath.EvalSymlinks(filepath.Join(home, "chats"))
		if got != real {
			t.Fatalf("cwd = %q", got)
		}
	}
	if len(events) != 3 || events[2].Type != EventTool {
		t.Fatalf("events = %+v", events)
	}
}

func TestRunResumesAndForks(t *testing.T) {
	chattest.Isolate(t)
	fake := chattest.Install(t)
	out := Run(context.Background(), localClient(t), Request{
		Harness: Claude, Folder: t.TempDir(), Message: "q", Resume: "abc", Fork: true, PathPrefix: []string{fake.Dir},
	}, func(Event) {})
	if out.Failure != nil || out.SessionID != "33333333-aaaa-4bbb-8ccc-000000000001" {
		t.Fatalf("out = %+v", out)
	}
	if args := fake.Read(t, "args"); !strings.Contains(args, "--resume\nabc\n--fork-session\n") {
		t.Fatalf("args = %q", args)
	}
}

func TestRunCodexClosesStdin(t *testing.T) {
	chattest.Isolate(t)
	fake := chattest.Install(t)
	out := Run(context.Background(), localClient(t), Request{
		Harness: Codex, Folder: t.TempDir(), Message: "q", Resume: "tid-1", PathPrefix: []string{fake.Dir},
	}, func(Event) {})
	if out.Failure != nil || out.Text != "codex says hi" || out.SessionID != "tid-1" {
		t.Fatalf("out = %+v", out)
	}
}

func TestRunFailures(t *testing.T) {
	chattest.Isolate(t)
	fake := chattest.Install(t)
	for mode, kind := range map[string]string{
		"missing-login": KindHarnessLoggedOut,
		"crash":         KindHarnessFailed,
		"fail":          KindHarnessFailed,
	} {
		fake.Mode(t, mode)
		out := Run(context.Background(), localClient(t), Request{
			Harness: Claude, Folder: t.TempDir(), Message: "q", PathPrefix: []string{fake.Dir},
		}, func(Event) {})
		if out.Failure == nil || out.Failure.Kind != kind {
			t.Errorf("%s: failure = %+v", mode, out.Failure)
		}
	}
}

func TestRunHarnessMissing(t *testing.T) {
	chattest.Isolate(t)
	t.Setenv("PATH", "/usr/bin:/bin")
	out := Run(context.Background(), localClient(t), Request{Harness: Codex, Folder: t.TempDir(), Message: "q"}, func(Event) {})
	if out.Failure == nil || out.Failure.Kind != KindHarnessMissing || !strings.Contains(out.Failure.Message, "codex") {
		t.Fatalf("failure = %+v", out.Failure)
	}
}

func TestRunCancelledKillsTheHarness(t *testing.T) {
	chattest.Isolate(t)
	fake := chattest.Install(t)
	fake.Mode(t, "slow")
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(time.Second, cancel)
	started := time.Now()
	out := Run(ctx, localClient(t), Request{
		Harness: Claude, Folder: t.TempDir(), Message: "q", PathPrefix: []string{fake.Dir},
	}, func(Event) {})
	if out.Failure == nil || out.Failure.Kind != KindCancelled {
		t.Fatalf("failure = %+v", out.Failure)
	}
	if out.SessionID == "" {
		t.Fatal("the session id seen before the stop was lost")
	}
	if time.Since(started) > 10*time.Second {
		t.Fatalf("the harness outlived the cancel: %s", time.Since(started))
	}
}

func TestClassifyConnection(t *testing.T) {
	for want, err := range map[string]error{
		KindHostKeyRejected: fmt.Errorf("machine %q at 203.0.113.10: %w", "main", remote.ErrHostKeyRejected),
		KindLoginRefused:    fmt.Errorf("%w: machine main answered", remote.ErrAuthRefused),
		KindUnreachable:     errors.New(`machine "main": no address answered: 203.0.113.10 (timeout)`),
	} {
		if got := ClassifyConnection(err); got == nil || got.Kind != want {
			t.Errorf("%v: got %+v, want %s", err, got, want)
		}
	}
	if got := ClassifyConnection(errors.New("exit status 1")); got != nil {
		t.Errorf("a command failure classified as %+v", got)
	}
	if got := ClassifyConnection(context.DeadlineExceeded); got == nil || got.Kind != KindUnreachable {
		t.Errorf("deadline = %+v", got)
	}
}
