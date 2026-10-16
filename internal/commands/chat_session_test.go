package commands

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mydevmachine/devmachine/internal/chat"
)

type liveEvent struct {
	Type   string       `json:"type"`
	Chat   *chat.Record `json:"chat"`
	Delta  string       `json:"delta"`
	Kind   string       `json:"kind"`
	Reason string       `json:"reason"`
	raw    string
}

type liveSession struct {
	in     *io.PipeWriter
	events chan liveEvent
	done   chan error
}

func startLive(t *testing.T, env chatEnv, args ...string) *liveSession {
	t.Helper()
	cmd := NewRootCmd()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	cmd.SetIn(inR)
	cmd.SetOut(outW)
	cmd.SetErr(io.Discard)
	cmd.SetArgs(append([]string{"--config", env.dir}, args...))
	l := &liveSession{in: inW, events: make(chan liveEvent, 100), done: make(chan error, 1)}
	go func() {
		err := cmd.Execute()
		_ = outW.Close()
		l.done <- err
	}()
	go func() {
		scanner := bufio.NewScanner(outR)
		scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
		for scanner.Scan() {
			var e liveEvent
			if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
				t.Errorf("not NDJSON: %q", scanner.Text())
				continue
			}
			e.raw = scanner.Text()
			l.events <- e
		}
		close(l.events)
	}()
	t.Cleanup(func() {
		_ = inW.Close()
		for range l.events {
		}
		select {
		case <-l.done:
		case <-time.After(15 * time.Second):
			t.Error("the session did not exit")
		}
	})
	return l
}

func (l *liveSession) write(t *testing.T, v any) {
	t.Helper()
	line, _ := json.Marshal(v)
	if _, err := l.in.Write(append(line, '\n')); err != nil {
		t.Fatal(err)
	}
}

func (l *liveSession) send(t *testing.T, text string) {
	t.Helper()
	l.write(t, map[string]string{"type": "send", "text": text})
}

// next is the next event that is not a status line.
func (l *liveSession) next(t *testing.T) liveEvent {
	t.Helper()
	for {
		if e := l.nextAny(t); e.Type != chat.EventStatus {
			return e
		}
	}
}

func (l *liveSession) nextAny(t *testing.T) liveEvent {
	t.Helper()
	select {
	case e, ok := <-l.events:
		if !ok {
			t.Fatal("the session ended its output early")
		}
		return e
	case <-time.After(15 * time.Second):
		t.Fatal("no event in 15s")
	}
	return liveEvent{}
}

// turn reads one turn's events, from started to done or error.
func (l *liveSession) turn(t *testing.T) (started liveEvent, text string, last liveEvent) {
	t.Helper()
	started = l.next(t)
	if started.Type != chat.EventStarted || started.Chat == nil {
		t.Fatalf("expected started, got %s", started.raw)
	}
	for {
		e := l.next(t)
		switch e.Type {
		case chat.EventText:
			text += e.Delta
		case chat.EventDone, chat.EventError:
			return started, text, e
		}
	}
}

func (l *liveSession) end(t *testing.T) error {
	t.Helper()
	select {
	case err := <-l.done:
		l.done <- err
		return err
	case <-time.After(15 * time.Second):
		t.Fatal("the session did not exit")
	}
	return nil
}

func TestChatSessionAnswersSeveralMessagesInOneProcess(t *testing.T) {
	env := newChatEnv(t, "")
	l := startLive(t, env, "chat", "session", "--local")

	ready := l.next(t)
	if ready.Type != chat.EventReady || !strings.Contains(ready.raw, `"chat":null`) {
		t.Fatalf("first line = %s", ready.raw)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(env.fake.Dir, "args")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the harness was not started before the first message")
		}
		time.Sleep(50 * time.Millisecond)
	}

	l.send(t, "one")
	started, text, last := l.turn(t)
	if started.Chat.Title != "one" || text != "turn 1: one" || last.Type != chat.EventDone {
		t.Fatalf("first turn: %s %q %s", started.raw, text, last.raw)
	}
	id := started.Chat.ID
	l.send(t, "two")
	started, text, last = l.turn(t)
	if started.Chat.ID != id || text != "turn 2: two" || last.Chat.HarnessSessionID == "" || last.Chat.MCP {
		t.Fatalf("second turn: %s %q %s", started.raw, text, last.raw)
	}
	args := env.fake.Read(t, "args")
	if strings.Count(args, "--input-format") != 1 || !strings.Contains(args, "--strict-mcp-config\n") {
		t.Fatalf("args = %q", args)
	}

	_ = l.in.Close()
	if closed := l.next(t); closed.Type != chat.EventClosed || closed.Reason != chat.ClosedEOF {
		t.Fatalf("last line = %s", closed.raw)
	}
	if err := l.end(t); err != nil {
		t.Fatal(err)
	}
	out, _ := env.run(t, "chat", "show", id, "--json")
	if strings.Count(out, `"role": "assistant"`) != 2 {
		t.Fatalf("the local copy misses a turn:\n%s", out)
	}
}

func TestChatSessionCancelKeepsTheSessionUsable(t *testing.T) {
	env := newChatEnv(t, "")
	l := startLive(t, env, "chat", "session", "--local")
	l.next(t)
	l.send(t, "first")
	if _, _, last := l.turn(t); last.Type != chat.EventDone {
		t.Fatalf("got %s", last.raw)
	}
	env.fake.Mode(t, "slow")
	l.send(t, "a long one")
	if e := l.next(t); e.Type != chat.EventStarted {
		t.Fatalf("got %s", e.raw)
	}
	time.Sleep(500 * time.Millisecond)
	l.write(t, map[string]string{"type": "cancel"})
	if e := l.next(t); e.Type != chat.EventError || e.Kind != chat.KindCancelled {
		t.Fatalf("got %s", e.raw)
	}

	env.fake.Mode(t, "")
	l.send(t, "after the stop")
	_, text, last := l.turn(t)
	if last.Type != chat.EventDone || !strings.HasSuffix(text, ": after the stop") {
		t.Fatalf("after cancel: %q %s", text, last.raw)
	}
	if args := env.fake.Read(t, "args"); !strings.Contains(args, "--resume\n22222222-aaaa-4bbb-8ccc-000000000001\n") {
		t.Fatalf("the restarted harness did not resume: %q", args)
	}
	_ = l.in.Close()
	l.next(t)
}

func TestChatSessionClosesWhenIdle(t *testing.T) {
	env := newChatEnv(t, "")
	orig := chatIdle
	chatIdle = 300 * time.Millisecond
	t.Cleanup(func() { chatIdle = orig })
	l := startLive(t, env, "chat", "session", "--local")
	l.next(t)
	if closed := l.next(t); closed.Type != chat.EventClosed || closed.Reason != chat.ClosedIdle {
		t.Fatalf("got %s", closed.raw)
	}
	if err := l.end(t); err != nil {
		t.Fatal(err)
	}
}

func TestChatSessionContinuesAChatWithItsMCPChoice(t *testing.T) {
	env := newChatEnv(t, "")
	out, err := env.run(t, "chat", "send", "--mcp", "--", "first")
	if err != nil {
		t.Fatal(err, out)
	}
	rec := events(t, out)[0].Chat

	l := startLive(t, env, "chat", "session", "--chat", rec.ID)
	ready := l.next(t)
	if ready.Chat == nil || ready.Chat.ID != rec.ID || !ready.Chat.MCP {
		t.Fatalf("ready = %s", ready.raw)
	}
	l.send(t, "more")
	if _, _, last := l.turn(t); last.Type != chat.EventDone {
		t.Fatalf("got %s", last.raw)
	}
	args := env.fake.Read(t, "args")
	if strings.Contains(args, "--strict-mcp-config") || !strings.Contains(args, "--resume\n22222222-aaaa-4bbb-8ccc-000000000001\n") {
		t.Fatalf("args = %q", args)
	}
	_ = l.in.Close()
	l.next(t)
}

func TestChatSessionFailsBeforeReadyWhenTheHarnessIsMissing(t *testing.T) {
	env := newChatEnv(t, "")
	t.Setenv("PATH", "/usr/bin:/bin")
	l := startLive(t, env, "chat", "session", "--local")
	if e := l.next(t); e.Type != chat.EventError || e.Kind != chat.KindHarnessMissing {
		t.Fatalf("got %s", e.raw)
	}
	if e := l.next(t); e.Type != chat.EventClosed || e.Reason != chat.ClosedError {
		t.Fatalf("got %s", e.raw)
	}
	if err := l.end(t); err == nil {
		t.Fatal("a session that never started exited 0")
	}
}

func TestChatSessionRunsCodexOneProcessPerMessage(t *testing.T) {
	env := newChatEnv(t, "")
	l := startLive(t, env, "chat", "session", "--local", "--harness", "codex")
	if e := l.next(t); e.Type != chat.EventReady {
		t.Fatalf("got %s", e.raw)
	}
	l.send(t, "one")
	if _, text, last := l.turn(t); text != "codex says hi" || last.Type != chat.EventDone {
		t.Fatalf("got %q %s", text, last.raw)
	}
	l.send(t, "two")
	l.turn(t)
	if args := env.fake.Read(t, "args"); !strings.Contains(args, "\nresume\n0199aaaa-bbbb-7ccc-8ddd-000000000009\n") || !strings.Contains(args, "--ignore-user-config\n") {
		t.Fatalf("args = %q", args)
	}
	_ = l.in.Close()
	l.next(t)
}

func TestChatSessionRefusesAnAsk(t *testing.T) {
	env := newChatEnv(t, "")
	store, _ := chat.Open(env.dir)
	ask, _ := store.Create(chat.Record{Title: "q", Kind: chat.KindAsk, Session: "s"})
	if _, err := env.run(t, "chat", "session", "--chat", ask.ID); err == nil || !strings.Contains(err.Error(), "is an ask") {
		t.Fatalf("err = %v", err)
	}
}

func TestChatAskServeKeepsOneLeanProcessAndReadsTheSessionEachTime(t *testing.T) {
	env := newChatEnv(t, "")
	project, _ := filepath.EvalSymlinks(t.TempDir())
	transcript := filepath.Join(os.Getenv("HOME"), ".claude/projects", chat.ClaudeSlug(project), "44444444-aaaa-4bbb-8ccc-000000000001.jsonl")
	writeAged(t, transcript, claudeLine(project, "user", "change ChargeService"), time.Minute)

	l := startLive(t, env, "chat", "ask", "--serve", "--local", "--session", "checkout", "--folder", project)
	if ready := l.next(t); ready.Type != chat.EventReady || !strings.Contains(ready.raw, `"chat":null`) {
		t.Fatalf("got %s", ready.raw)
	}
	l.send(t, "why ChargeService?")
	started, _, last := l.turn(t)
	if started.Chat.Kind != chat.KindAsk || last.Type != chat.EventDone || last.Chat.Mode != chat.ModeTranscript || last.Chat.Excerpts != 1 {
		t.Fatalf("first: %s %s", started.raw, last.raw)
	}
	appendTo(t, transcript, claudeLine(project, "user", "remember KESTREL"))
	l.send(t, "what new word?")
	if _, _, last := l.turn(t); last.Type != chat.EventDone || last.Chat.Excerpts != 2 {
		t.Fatalf("second: %s", last.raw)
	}

	args, stdin := env.fake.Read(t, "args"), env.fake.Read(t, "stdin")
	if strings.Count(args, "--input-format") != 1 || !strings.Contains(args, "--no-session-persistence\n") {
		t.Fatalf("args = %q", args)
	}
	lines := strings.Split(strings.TrimSpace(stdin), "\n")
	if len(lines) != 2 || !strings.Contains(lines[1], "remember KESTREL") || !strings.Contains(lines[1], "Earlier in this side conversation") {
		t.Fatalf("stdin = %q", stdin)
	}
	_ = l.in.Close()
	if e := l.next(t); e.Type != chat.EventClosed {
		t.Fatalf("got %s", e.raw)
	}
}
