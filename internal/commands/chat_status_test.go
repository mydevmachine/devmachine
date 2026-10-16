package commands

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mydevmachine/devmachine/internal/chat"
)

// stagesOf lists the status stages in order, and the first other event
// that follows them.
func stagesOf(evs []chat.Event) (stages []string, then string) {
	for _, e := range evs[1:] {
		if e.Type != chat.EventStatus {
			return stages, e.Type
		}
		stages = append(stages, e.Stage+"="+e.Message)
	}
	return stages, ""
}

func TestChatSendStagesHereAndOnAWorkspace(t *testing.T) {
	env := newChatEnv(t, chatConfig)
	dialHere(t, nil)
	for name, c := range map[string]struct {
		args []string
		want string
	}{
		"this computer": {[]string{"--local"}, "starting=Starting Claude…,waiting=Claude is thinking…"},
		"a workspace":   {[]string{"--workspace", "bob", "--folder", t.TempDir()}, "connecting=Connecting to bob…,starting=Starting Claude…,waiting=Claude is thinking…"},
	} {
		out, err := env.run(t, append(append([]string{"chat", "send"}, c.args...), "--", "hi")...)
		if err != nil {
			t.Fatal(err, out)
		}
		evs := events(t, out)
		stages, then := stagesOf(evs)
		if evs[0].Type != chat.EventStarted || strings.Join(stages, ",") != c.want || then != chat.EventText {
			t.Errorf("%s: stages %v then %s", name, stages, then)
		}
		out, _ = env.run(t, "chat", "show", evs[0].Chat.ID, "--json")
		if strings.Contains(out, "status") || strings.Contains(out, "thinking") {
			t.Errorf("%s: a status reached the stored messages:\n%s", name, out)
		}
	}
}

func TestChatAskStages(t *testing.T) {
	env := newChatEnv(t, "")
	project, _ := filepath.EvalSymlinks(t.TempDir())
	writeAged(t, filepath.Join(os.Getenv("HOME"), ".claude/projects", chat.ClaudeSlug(project), "s.jsonl"), claudeLine(project, "user", "x"), time.Minute)
	out, err := env.run(t, "chat", "ask", "--local", "--session", "s", "--folder", project, "--", "why?")
	if err != nil {
		t.Fatal(err, out)
	}
	stages, then := stagesOf(events(t, out))
	want := "reading-transcript=Reading the session…,starting=Starting Claude…,waiting=Claude is thinking…"
	if strings.Join(stages, ",") != want || then != chat.EventText {
		t.Fatalf("stages %v then %s", stages, then)
	}
}

func TestChatSessionStagesBeforeReadyThenWaitingPerTurn(t *testing.T) {
	env := newChatEnv(t, chatConfig)
	dialHere(t, nil)
	for name, c := range map[string]struct {
		args []string
		want string
	}{
		"this computer": {[]string{"--local"}, "starting=Starting Claude…"},
		"a workspace":   {[]string{"--workspace", "bob", "--folder", t.TempDir()}, "connecting=Connecting to bob…,starting=Starting Claude…"},
	} {
		l := startLive(t, env, append([]string{"chat", "session"}, c.args...)...)
		var stages []string
		e := l.nextAny(t)
		for ; e.Type == chat.EventStatus; e = l.nextAny(t) {
			stages = append(stages, liveStage(e))
		}
		if e.Type != chat.EventReady || strings.Join(stages, ",") != c.want {
			t.Fatalf("%s: before ready %v, then %s", name, stages, e.raw)
		}
		for i := range 2 {
			l.send(t, "hi")
			if s := l.nextAny(t); s.Type != chat.EventStarted {
				t.Fatalf("%s turn %d: %s", name, i, s.raw)
			}
			if s := l.nextAny(t); s.Type != chat.EventStatus || liveStage(s) != "waiting=Claude is thinking…" {
				t.Fatalf("%s turn %d: %s", name, i, s.raw)
			}
			if s := l.nextAny(t); s.Type != chat.EventText {
				t.Fatalf("%s turn %d: %s", name, i, s.raw)
			}
			for s := l.next(t); s.Type != chat.EventDone; s = l.next(t) {
			}
		}
		_ = l.in.Close()
		l.next(t)
	}
}

func liveStage(e liveEvent) string {
	var v struct {
		Stage   string `json:"stage"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal([]byte(e.raw), &v)
	return v.Stage + "=" + v.Message
}
