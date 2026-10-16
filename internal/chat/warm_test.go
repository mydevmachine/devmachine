package chat

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mydevmachine/devmachine/internal/chat/chattest"
)

func TestWarmScriptKeepsStdinForTheHarness(t *testing.T) {
	script := WarmScript(Request{Harness: Claude, Folder: "~/chats", Resume: "sid"})
	if strings.Contains(script, "</dev/null\n") || strings.Contains(script, "<<'") {
		t.Fatalf("the harness would not read the messages:\n%s", script)
	}
	last := script[strings.LastIndex(strings.TrimSuffix(script, "\n"), "\n")+1:]
	if !strings.HasPrefix(last, startedEcho+"; exec 'claude' ") || !strings.Contains(last, "'--input-format' 'stream-json' '--resume' 'sid'") {
		t.Fatalf("the ready line and exec must share the last line, so no shell reads a message:\n%s", last)
	}
}

func TestWarmAnswersSeveralTurnsInOneProcess(t *testing.T) {
	chattest.Isolate(t)
	fake := chattest.Install(t)
	var startup []Event
	w, f := StartWarm(context.Background(), localClient(t), Request{Harness: Claude, Folder: t.TempDir(), PathPrefix: []string{fake.Dir}}, collect(&startup))
	if f != nil {
		t.Fatal(f)
	}
	defer w.Close()
	if got := stages(startup); got != "starting=Starting Claude…" {
		t.Fatalf("start-up stages = %s", got)
	}

	var turn []Event
	first := w.Turn(`it's "one"`, nil, collect(&turn))
	if turn[0].Type != EventStatus || turn[0].Stage != StageWaiting || stages(turn) != "waiting=Claude is thinking…" {
		t.Fatalf("turn events = %+v", turn)
	}
	second := w.Turn("two\nlines", nil, func(Event) {})
	if first.Failure != nil || first.Text != `turn 1: it's "one"` || first.SessionID == "" {
		t.Fatalf("first = %+v", first)
	}
	if second.Failure != nil || second.Text != "turn 2: two\nlines" {
		t.Fatalf("second = %+v", second)
	}
	if args := fake.Read(t, "args"); strings.Count(args, "--input-format") != 1 {
		t.Fatalf("args = %q", args)
	}
	if got := fake.Read(t, "stdin"); strings.Count(got, `{"type":"user","message":{"role":"user","content":`) != 2 {
		t.Fatalf("stdin = %q", got)
	}
}

func TestWarmStopEndsTheTurnAndTheProcess(t *testing.T) {
	chattest.Isolate(t)
	fake := chattest.Install(t)
	fake.Mode(t, "slow")
	w, f := StartWarm(context.Background(), localClient(t), Request{Harness: Claude, Folder: t.TempDir(), PathPrefix: []string{fake.Dir}}, func(Event) {})
	if f != nil {
		t.Fatal(f)
	}
	stop := make(chan struct{})
	var once sync.Once
	started := time.Now()
	out := w.Turn("hi", stop, func(e Event) {
		if e.Type == EventStatus && e.Stage == StageWaiting {
			once.Do(func() { close(stop) })
		}
	})
	if out.Failure == nil || out.Failure.Kind != KindCancelled || out.SessionID == "" {
		t.Fatalf("out = %+v", out)
	}
	if time.Since(started) > 10*time.Second {
		t.Fatal("the stop did not end the turn")
	}
	if !w.Dead() {
		t.Fatal("a stopped process is still in use")
	}
}

func TestWarmFailsBeforeReadyWhenTheHarnessIsMissing(t *testing.T) {
	chattest.Isolate(t)
	t.Setenv("PATH", "/usr/bin:/bin")
	_, f := StartWarm(context.Background(), localClient(t), Request{Harness: Claude, Folder: t.TempDir()}, func(Event) {})
	if f == nil || f.Kind != KindHarnessMissing {
		t.Fatalf("failure = %+v", f)
	}
}

func TestWarmReportsAHarnessThatDiesMidTurn(t *testing.T) {
	chattest.Isolate(t)
	fake := chattest.Install(t)
	w, f := StartWarm(context.Background(), localClient(t), Request{Harness: Claude, Folder: t.TempDir(), PathPrefix: []string{fake.Dir}}, func(Event) {})
	if f != nil {
		t.Fatal(f)
	}
	defer w.Close()
	w.kill()
	out := w.Turn("hi", nil, func(Event) {})
	if out.Failure == nil || out.Failure.Kind != KindHarnessFailed || !w.Dead() {
		t.Fatalf("out = %+v", out)
	}
}

func TestWarmAskArgsAreLeanAndStream(t *testing.T) {
	script := WarmScript(Request{Harness: Claude, Folder: "~/chats", Ask: true})
	if !strings.Contains(script, "'--no-session-persistence' '--model' 'haiku' '--input-format' 'stream-json'") {
		t.Fatalf("script:\n%s", script)
	}
}
