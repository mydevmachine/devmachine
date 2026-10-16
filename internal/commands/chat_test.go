package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mydevmachine/devmachine/internal/chat"
	"github.com/mydevmachine/devmachine/internal/chat/chattest"
	"github.com/mydevmachine/devmachine/internal/config"
	"github.com/mydevmachine/devmachine/internal/remote"
)

const chatConfig = "machines:\n  - name: main\n    hosts: [203.0.113.10]\n" +
	"workspaces:\n  - name: alice\n    machine: main\n  - name: bob\n    machine: main\n"

type chatEnv struct {
	dir  string
	fake chattest.Bin
}

// newChatEnv runs the harness on this computer, with the fakes first on
// PATH, and makes every workspace dial land here too.
func newChatEnv(t *testing.T, configBody string) chatEnv {
	t.Helper()
	chattest.Isolate(t)
	fake := chattest.Install(t)
	t.Setenv("PATH", fake.Dir+":"+os.Getenv("PATH"))
	dir := t.TempDir()
	if configBody != "" {
		if err := os.WriteFile(filepath.Join(dir, "config.yml"), []byte(configBody), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return chatEnv{dir: dir, fake: fake}
}

func dialHere(t *testing.T, users *[]string) {
	t.Helper()
	orig := dialMux
	dialMux = func(ctx context.Context, _ config.Machine, user string) (remote.Client, string, error) {
		if users != nil {
			*users = append(*users, user)
		}
		return remote.Dial(ctx, config.Machine{Name: "here", Self: true}, "")
	}
	t.Cleanup(func() { dialMux = orig })
}

func events(t *testing.T, out string) []chat.Event {
	t.Helper()
	var all []chat.Event
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		var e chat.Event
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatalf("not NDJSON: %q in\n%s", line, out)
		}
		all = append(all, e)
	}
	return all
}

func (e chatEnv) run(t *testing.T, args ...string) (string, error) {
	t.Helper()
	return execute(t, append([]string{"--config", e.dir}, args...)...)
}

func TestChatSendStartsAndContinuesAConversation(t *testing.T) {
	env := newChatEnv(t, "")
	out, err := env.run(t, "chat", "send", "--", "it's \"$HOME\"")
	if err != nil {
		t.Fatal(err, out)
	}
	got := events(t, out)
	if got[0].Type != chat.EventStarted || got[0].Chat == nil || got[0].Chat.Workspace != "local" || got[0].Chat.Harness != "claude" || got[0].Chat.Folder != "~/chats" {
		t.Fatalf("first = %+v", got[0])
	}
	last := got[len(got)-1]
	if last.Type != chat.EventDone || last.Chat.HarnessSessionID != "22222222-aaaa-4bbb-8ccc-000000000001" {
		t.Fatalf("last = %+v", last)
	}
	id := last.Chat.ID

	out, err = env.run(t, "chat", "send", "--chat", id, "--", "again")
	if err != nil {
		t.Fatal(err, out)
	}
	if args := env.fake.Read(t, "args"); !strings.Contains(args, "--resume\n22222222-aaaa-4bbb-8ccc-000000000001\n") {
		t.Fatalf("args = %q", args)
	}

	out, err = env.run(t, "chat", "show", id, "--json")
	if err != nil {
		t.Fatal(err, out)
	}
	var shown struct {
		Chat     chat.Record    `json:"chat"`
		Messages []chat.Message `json:"messages"`
	}
	if err := json.Unmarshal([]byte(out), &shown); err != nil {
		t.Fatal(err, out)
	}
	if shown.Chat.Title != `it's "$HOME"` || len(shown.Messages) != 4 || shown.Messages[1].Text != `echo: it's "$HOME"` || shown.Messages[1].Tools[0].Name != "Read" {
		t.Fatalf("shown = %+v", shown)
	}
}

func TestChatSendRefusesToSwitchAConversation(t *testing.T) {
	env := newChatEnv(t, "")
	out, err := env.run(t, "chat", "send", "--", "hi")
	if err != nil {
		t.Fatal(err, out)
	}
	id := events(t, out)[0].Chat.ID
	out, err = env.run(t, "chat", "send", "--chat", id, "--harness", "codex", "--", "hi")
	if err == nil || !strings.Contains(err.Error(), "--harness codex") || strings.Contains(out, `"type"`) {
		t.Fatalf("err = %v, out = %s", err, out)
	}
}

func TestChatSendNeedsTheMessageAfterTheDash(t *testing.T) {
	env := newChatEnv(t, "")
	if _, err := env.run(t, "chat", "send", "hi"); err == nil || !strings.Contains(err.Error(), "follows `--`") {
		t.Fatalf("err = %v", err)
	}
}

func TestChatSendUsesTheConfiguredDefaults(t *testing.T) {
	env := newChatEnv(t, chatConfig)
	var users []string
	dialHere(t, &users)
	for key, value := range map[string]string{"chat.default_workspace": "bob", "chat.harness": "codex", "chat.folder": t.TempDir()} {
		if out, err := env.run(t, "config", "set", key, value); err != nil {
			t.Fatal(err, out)
		}
	}
	out, err := env.run(t, "chat", "send", "--", "hi")
	if err != nil {
		t.Fatal(err, out)
	}
	got := events(t, out)
	if c := got[0].Chat; c.Workspace != "bob" || c.Harness != "codex" {
		t.Fatalf("record = %+v", c)
	}
	if len(users) != 1 || users[0] != "bob" || got[len(got)-1].Delta != "" || got[1].Delta != "codex says hi" {
		t.Fatalf("users = %v, events = %+v", users, got)
	}

	out, err = env.run(t, "config", "show", "--format", "json")
	if err != nil || !strings.Contains(out, `"harness": "codex"`) {
		t.Fatalf("config show = %s, %v", out, err)
	}
	if out, err := env.run(t, "config", "unset", "chat.default_workspace"); err != nil {
		t.Fatal(err, out)
	}
	out, _ = env.run(t, "config", "show", "--format", "json")
	if !strings.Contains(out, `"default_workspace": "alice"`) {
		t.Fatalf("the first workspace is not the default again:\n%s", out)
	}
}

func TestChatSendFirstWorkspaceByDefault(t *testing.T) {
	env := newChatEnv(t, chatConfig)
	var users []string
	dialHere(t, &users)
	if out, err := env.run(t, "chat", "send", "--folder", t.TempDir(), "--", "hi"); err != nil {
		t.Fatal(err, out)
	}
	if len(users) != 1 || users[0] != "alice" {
		t.Fatalf("users = %v", users)
	}
	if _, err := env.run(t, "chat", "send", "--workspace", "carol", "--", "hi"); err == nil {
		t.Fatal("an unknown workspace was accepted")
	}
}

func TestChatSendErrorKinds(t *testing.T) {
	env := newChatEnv(t, chatConfig)
	for name, c := range map[string]struct {
		mode, kind string
		dialErr    error
	}{
		"logged out":    {mode: "missing-login", kind: chat.KindHarnessLoggedOut},
		"harness fails": {mode: "fail", kind: chat.KindHarnessFailed},
		"host key":      {kind: chat.KindHostKeyRejected, dialErr: fmt.Errorf("machine main: %w", remote.ErrHostKeyRejected)},
		"login refused": {kind: chat.KindLoginRefused, dialErr: fmt.Errorf("%w: machine main answered", remote.ErrAuthRefused)},
		"unreachable":   {kind: chat.KindUnreachable, dialErr: fmt.Errorf(`machine "main": no address answered: 203.0.113.10`)},
	} {
		t.Run(name, func(t *testing.T) {
			env.fake.Mode(t, c.mode)
			args := []string{"chat", "send", "--local", "--", "hi"}
			if c.dialErr != nil {
				orig := dialMux
				dialMux = func(context.Context, config.Machine, string) (remote.Client, string, error) {
					return nil, "", c.dialErr
				}
				t.Cleanup(func() { dialMux = orig })
				args = []string{"chat", "send", "--workspace", "alice", "--", "hi"}
			}
			out, err := env.run(t, args...)
			if err == nil || exitCode(err) != 1 {
				t.Fatalf("err = %v", err)
			}
			got := events(t, out)
			last := got[len(got)-1]
			if got[0].Type != chat.EventStarted || last.Type != chat.EventError || last.Kind != c.kind {
				t.Fatalf("events = %+v", got)
			}
		})
	}
}

func TestChatSendHarnessMissing(t *testing.T) {
	env := newChatEnv(t, "")
	t.Setenv("PATH", "/usr/bin:/bin")
	out, _ := env.run(t, "chat", "send", "--harness", "codex", "--", "hi")
	got := events(t, out)
	if last := got[len(got)-1]; last.Kind != chat.KindHarnessMissing {
		t.Fatalf("events = %+v", got)
	}
}

func TestChatSendCancelled(t *testing.T) {
	env := newChatEnv(t, "")
	env.fake.Mode(t, "slow")
	orig := stopSignals
	stopSignals = func(parent context.Context) (context.Context, context.CancelFunc) {
		return context.WithTimeout(parent, time.Second)
	}
	t.Cleanup(func() { stopSignals = orig })
	out, err := env.run(t, "chat", "send", "--", "hi")
	if err == nil {
		t.Fatal("a cancelled send succeeded")
	}
	got := events(t, out)
	last := got[len(got)-1]
	if last.Kind != chat.KindCancelled {
		t.Fatalf("events = %+v", got)
	}
	out, _ = env.run(t, "chat", "list", "--json")
	if !strings.Contains(out, "22222222-aaaa-4bbb-8ccc-000000000001") {
		t.Fatalf("the session id seen before the stop was not kept:\n%s", out)
	}
}

func TestChatListRenameContinueDelete(t *testing.T) {
	env := newChatEnv(t, "")
	out, _ := env.run(t, "chat", "send", "--", "first question")
	id := events(t, out)[0].Chat.ID
	store, err := chat.Open(env.dir)
	if err != nil {
		t.Fatal(err)
	}
	ask, err := store.Create(chat.Record{Title: "an ask", Kind: chat.KindAsk, Session: "checkout"})
	if err != nil {
		t.Fatal(err)
	}

	out, err = env.run(t, "chat", "list")
	if err != nil || !strings.Contains(out, "first question") || strings.Contains(out, "an ask") {
		t.Fatalf("list = %s, %v", out, err)
	}
	if out, _ = env.run(t, "chat", "list", "--all", "--json"); !strings.Contains(out, "an ask") || !strings.Contains(out, `"chats": [`) {
		t.Fatalf("list --all = %s", out)
	}
	if out, err = env.run(t, "chat", "rename", id, "Billing", "notes"); err != nil {
		t.Fatal(err, out)
	}
	if out, err = env.run(t, "chat", "continue", ask.ID, "--json"); err != nil || !strings.Contains(out, `"kind": "chat"`) {
		t.Fatalf("continue = %s, %v", out, err)
	}
	out, _ = env.run(t, "chat", "list")
	if !strings.Contains(out, "Billing notes") || !strings.Contains(out, "an ask") {
		t.Fatalf("list = %s", out)
	}
	if out, err = env.run(t, "chat", "delete", id); err != nil {
		t.Fatal(err, out)
	}
	if out, _ = env.run(t, "chat", "list"); strings.Contains(out, "Billing") {
		t.Fatalf("deleted chat still listed:\n%s", out)
	}
	if _, err := env.run(t, "chat", "show", id); err == nil {
		t.Fatal("a deleted chat was shown")
	}
}

func writeAged(t *testing.T, path, body string, age time.Duration) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	when := time.Now().Add(-age)
	if err := os.Chtimes(path, when, when); err != nil {
		t.Fatal(err)
	}
}

func TestChatAskForksAClaudeSessionAndLeavesItAlone(t *testing.T) {
	env := newChatEnv(t, "")
	project, _ := filepath.EvalSymlinks(t.TempDir())
	transcript := filepath.Join(os.Getenv("HOME"), ".claude/projects", chat.ClaudeSlug(project), "44444444-aaaa-4bbb-8ccc-000000000001.jsonl")
	body := `{"type":"user","cwd":"` + project + `","message":{"content":"change ChargeService"}}` + "\n"
	writeAged(t, transcript, body, time.Minute)

	out, err := env.run(t, "chat", "ask", "--local", "--session", "checkout", "--folder", project, "--", "why?")
	if err != nil {
		t.Fatal(err, out)
	}
	got := events(t, out)
	done := got[len(got)-1].Chat
	if done == nil || done.Kind != chat.KindAsk || done.Mode != chat.ModeFork || done.Session != "checkout" ||
		done.SourceSessionID != "44444444-aaaa-4bbb-8ccc-000000000001" || done.HarnessSessionID != "33333333-aaaa-4bbb-8ccc-000000000001" || done.Harness != "claude" {
		t.Fatalf("done = %+v", done)
	}
	args := env.fake.Read(t, "args")
	if !strings.Contains(args, "--resume\n44444444-aaaa-4bbb-8ccc-000000000001\n--fork-session\n") || !strings.Contains(args, "--permission-mode\nplan\n") {
		t.Fatalf("args = %q", args)
	}
	if cwd := strings.TrimSpace(env.fake.Read(t, "cwd")); cwd != project {
		t.Fatalf("fork ran in %q, want %q", cwd, project)
	}
	after, _ := os.ReadFile(transcript)
	if string(after) != body {
		t.Fatal("the session's transcript changed")
	}

	if out, err = env.run(t, "chat", "ask", "--chat", done.ID, "--", "and then?"); err != nil {
		t.Fatal(err, out)
	}
	args = env.fake.Read(t, "args")
	if !strings.Contains(args, "--resume\n33333333-aaaa-4bbb-8ccc-000000000001\n") || strings.Contains(args, "--fork-session") {
		t.Fatalf("follow-up args = %q", args)
	}
	if out, _ = env.run(t, "chat", "list"); strings.Contains(out, "why?") {
		t.Fatalf("an ask is listed:\n%s", out)
	}
}

func TestChatAskReadsACodexTranscript(t *testing.T) {
	env := newChatEnv(t, chatConfig)
	dialHere(t, nil)
	project, _ := filepath.EvalSymlinks(t.TempDir())
	rollout := `{"type":"session_meta","payload":{"id":"0199-source","cwd":"` + project + `"}}` + "\n" +
		`{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"refactor ChargeService"}]}}` + "\n"
	writeAged(t, filepath.Join(os.Getenv("HOME"), ".codex/sessions/2026/10/15/rollout-x.jsonl"), rollout, time.Minute)

	out, err := env.run(t, "chat", "ask", "--workspace", "alice", "--session", "checkout", "--folder", project, "--harness", "codex", "--", "why ChargeService?")
	if err != nil {
		t.Fatal(err, out)
	}
	got := events(t, out)
	done := got[len(got)-1].Chat
	if done.Mode != chat.ModeTranscript || done.Excerpts != 1 || done.SourceSessionID != "0199-source" || done.Workspace != "alice" {
		t.Fatalf("done = %+v", done)
	}
	args := env.fake.Read(t, "args")
	if !strings.Contains(args, "[user] refactor ChargeService") || !strings.Contains(args, "Question: why ChargeService?") || !strings.Contains(args, "read-only") {
		t.Fatalf("args = %q", args)
	}
	out, _ = env.run(t, "chat", "show", done.ID, "--json")
	if strings.Contains(out, "<transcript>") {
		t.Fatalf("the local copy holds the excerpt, not the question:\n%s", out)
	}
}

func TestChatAskErrors(t *testing.T) {
	env := newChatEnv(t, "")
	project := t.TempDir()
	if _, err := env.run(t, "chat", "ask", "--local", "--", "q"); err == nil || !strings.Contains(err.Error(), "--session") {
		t.Fatalf("no session: %v", err)
	}
	if _, err := env.run(t, "chat", "ask", "--session", "s", "--", "q"); err == nil || !strings.Contains(err.Error(), "--local") {
		t.Fatalf("no target: %v", err)
	}
	for harness, kind := range map[string]string{"pi": chat.KindHarnessUnsupported, "claude": chat.KindTranscriptNotFound} {
		out, _ := env.run(t, "chat", "ask", "--local", "--session", "s", "--folder", project, "--harness", harness, "--", "q")
		got := events(t, out)
		if got[0].Type != chat.EventStarted || got[len(got)-1].Kind != kind {
			t.Errorf("%s: events = %+v", harness, got)
		}
	}
}
