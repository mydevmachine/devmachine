package chat

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mydevmachine/devmachine/internal/chat/chattest"
	"github.com/mydevmachine/devmachine/internal/remote"
)

func writeFile(t *testing.T, path, body string, age time.Duration) {
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

func realDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestClaudeSlug(t *testing.T) {
	if got := ClaudeSlug("/home/alice/.config/my_app é"); got != "-home-alice--config-my-app--" {
		t.Fatalf("slug = %q", got)
	}
}

func TestLocateClaudePicksTheNewestTranscript(t *testing.T) {
	home := chattest.Isolate(t)
	project := realDir(t)
	slug := ClaudeSlug(project)
	writeFile(t, filepath.Join(home, ".claude/projects", slug, "old-session.jsonl"), `{"cwd":"`+project+`"}`+"\n", time.Hour)
	worktree := filepath.Join(project, ".claude/worktrees/x")
	writeFile(t, filepath.Join(home, ".claude/projects", slug+"--claude-worktrees-x", "new-session.jsonl"),
		`{"type":"user","cwd":"`+worktree+`"}`+"\n", time.Minute)

	loc, f := Locate(context.Background(), localClient(t), Target{Session: "checkout", Folder: project, Harness: "claude"})
	if f != nil {
		t.Fatal(f)
	}
	if loc.SessionID != "new-session" || loc.Folder != project || filepath.Base(filepath.Dir(loc.Transcript)) != slug+"--claude-worktrees-x" {
		t.Fatalf("loc = %+v", loc)
	}
}

func TestLocateReadsTheTailAndTheLinesThatMatch(t *testing.T) {
	home := chattest.Isolate(t)
	project := realDir(t)
	var body strings.Builder
	body.WriteString(`{"type":"user","message":{"content":"the ZEBRA plan"}}` + "\n")
	for i := range 50 {
		fmt.Fprintf(&body, `{"type":"user","message":{"content":"filler %02d"}}`+"\n", i)
	}
	body.WriteString(`{"type":"user","message":{"content":"zebra again, at the end"}}` + "\n")
	writeFile(t, filepath.Join(home, ".claude/projects", ClaudeSlug(project), "s.jsonl"), body.String(), time.Minute)
	orig := transcriptTail
	transcriptTail = 120
	t.Cleanup(func() { transcriptTail = orig })

	loc, f := Locate(context.Background(), localClient(t), Target{Session: "s", Folder: project, Harness: "claude", Question: "what about the zebra?"})
	if f != nil {
		t.Fatal(f)
	}
	got := string(loc.Body)
	if !strings.Contains(got, "the ZEBRA plan") || strings.Contains(got, "filler 10") || strings.Count(got, "zebra again") != 1 {
		t.Fatalf("body = %q", got)
	}
	if strings.Index(got, "the ZEBRA plan") > strings.Index(got, "zebra again") {
		t.Fatalf("the matches do not come before the tail: %q", got)
	}
}

type countingClient struct {
	remote.Client
	calls int
}

func (c *countingClient) RunInput(ctx context.Context, command string, stdin io.Reader) (string, error) {
	c.calls++
	return c.Client.RunInput(ctx, command, stdin)
}

func TestLocateTakesOneTripWhenTheHarnessIsKnown(t *testing.T) {
	home := chattest.Isolate(t)
	for name, c := range map[string]struct {
		folder string
		trips  int
	}{"plain path": {"plain", 1}, "accented path": {"café", 2}} {
		project := filepath.Join(realDir(t), c.folder)
		if err := os.MkdirAll(project, 0o700); err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(home, ".claude/projects", ClaudeSlug(project), name+".jsonl"), `{"type":"user","message":{"content":"hello"}}`+"\n", time.Minute)
		client := &countingClient{Client: localClient(t)}
		loc, f := Locate(context.Background(), client, Target{Session: "s", Folder: project, Harness: "claude", Question: "hello?"})
		if f != nil {
			t.Fatalf("%s: %+v", name, f)
		}
		if client.calls != c.trips || loc.SessionID != name || !strings.Contains(string(loc.Body), "hello") || loc.Folder != project {
			t.Errorf("%s: %d trips, loc = %+v", name, client.calls, loc)
		}
	}
}

func TestLocateFindsTheHarnessFromTheNewestTranscript(t *testing.T) {
	home := chattest.Isolate(t)
	project := realDir(t)
	writeFile(t, filepath.Join(home, ".claude/projects", ClaudeSlug(project), "s.jsonl"), "{}\n", time.Hour)
	meta := `{"type":"session_meta","payload":{"id":"0199-codex-id","cwd":"` + project + `"}}`
	writeFile(t, filepath.Join(home, ".codex/sessions/2026/10/15/rollout-2026-10-15T10-00-00-a.jsonl"), meta+"\n", time.Minute)
	writeFile(t, filepath.Join(home, ".codex/sessions/2026/10/15/rollout-2026-10-15T11-00-00-b.jsonl"),
		`{"type":"session_meta","payload":{"id":"other","cwd":"/elsewhere"}}`+"\n", 0)

	loc, f := Locate(context.Background(), localClient(t), Target{Session: "s", Folder: project})
	if f != nil {
		t.Fatal(f)
	}
	if loc.Harness != "codex" || loc.SessionID != "0199-codex-id" || filepath.Base(loc.Transcript) != "rollout-2026-10-15T10-00-00-a.jsonl" {
		t.Fatalf("loc = %+v", loc)
	}
}

func TestLocateFailures(t *testing.T) {
	chattest.Isolate(t)
	project := realDir(t)
	for name, c := range map[string]struct {
		target Target
		kind   string
	}{
		"no transcript":   {Target{Session: "s", Folder: project, Harness: "claude"}, KindTranscriptNotFound},
		"no folder":       {Target{Session: "s", Folder: filepath.Join(project, "gone")}, KindSessionNotFound},
		"pi":              {Target{Session: "s", Folder: project, Harness: "pi"}, KindHarnessUnsupported},
		"no tmux session": {Target{Session: "dm-chat-test-no-such-session"}, KindSessionNotFound},
	} {
		_, f := Locate(context.Background(), localClient(t), c.target)
		if f == nil || f.Kind != c.kind {
			t.Errorf("%s: failure = %+v, want %s", name, f, c.kind)
		}
	}
}

func TestLocateReadsTmuxInUTF8(t *testing.T) {
	script := locateScript(Target{Session: "checkout"})
	if !strings.Contains(script, "tmux -u display-message -p -t '=checkout:'") {
		t.Fatalf("without -u a Mac's ssh login prints each tab as _:\n%s", script)
	}
}

func TestPaneHarness(t *testing.T) {
	ps := "  100     1 -zsh\n  200   100 node /home/alice/.local/bin/cline --yolo\n  300   100 /home/alice/.local/share/claude/versions/2.1.301\n"
	for info, want := range map[string]string{
		"/w\tclaude\t100":  "claude",
		"/w\t2.1.301\t100": "claude",
		"/w\tkimi-code\t1": "kimi",
		"/w\tagy\t1":       "antigravity",
		"/w\tzsh\t100":     "cline",
		"/w\tzsh\t999":     "",
	} {
		if got := paneHarness(info, ps); got != want {
			t.Errorf("%q: got %q, want %q", info, got, want)
		}
	}
}
