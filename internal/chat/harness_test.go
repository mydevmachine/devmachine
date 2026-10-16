package chat

import (
	"bufio"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func feedFixture(t *testing.T, p parser, name string) []Event {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	var events []Event
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		events = append(events, p.feed(scanner.Bytes())...)
	}
	return events
}

func texts(events []Event) string {
	var b strings.Builder
	for _, e := range events {
		if e.Type == EventText {
			b.WriteString(e.Delta)
		}
	}
	return b.String()
}

func tools(events []Event) []Tool {
	var out []Tool
	for _, e := range events {
		if e.Type == EventTool {
			out = append(out, Tool{Name: e.Name, Summary: e.Summary})
		}
	}
	return out
}

func TestClaudeArgs(t *testing.T) {
	base := []string{"-p", "--output-format", "stream-json", "--verbose", "--include-partial-messages", "--permission-mode", "plan"}
	for _, c := range []struct {
		resume string
		fork   bool
		want   []string
	}{
		{"", false, append(append([]string{}, base...), "--", "hi")},
		{"sid", false, append(append([]string{}, base...), "--resume", "sid", "--", "hi")},
		{"sid", true, append(append([]string{}, base...), "--resume", "sid", "--fork-session", "--", "hi")},
	} {
		if got := Claude.Args("hi", c.resume, c.fork); !reflect.DeepEqual(got, c.want) {
			t.Errorf("Args(%q, %v) = %v", c.resume, c.fork, got)
		}
	}
}

func TestCodexArgs(t *testing.T) {
	base := []string{"exec", "--json", "--skip-git-repo-check", "--sandbox", "read-only"}
	if got := Codex.Args("hi", "", false); !reflect.DeepEqual(got, append(append([]string{}, base...), "--", "hi")) {
		t.Errorf("new = %v", got)
	}
	if got := Codex.Args("hi", "tid", false); !reflect.DeepEqual(got, append(append([]string{}, base...), "resume", "tid", "--", "hi")) {
		t.Errorf("resume = %v", got)
	}
}

func TestAskArgsAreLeanAndTakeThePromptOnStdin(t *testing.T) {
	wantClaude := []string{"-p", "--output-format", "stream-json", "--verbose", "--include-partial-messages",
		"--strict-mcp-config", "--setting-sources", "project", "--disable-slash-commands", "--tools", "",
		"--no-session-persistence", "--model", "haiku"}
	if !reflect.DeepEqual(Claude.AskArgs, wantClaude) {
		t.Errorf("claude = %q", Claude.AskArgs)
	}
	wantCodex := []string{"exec", "--json", "--ephemeral", "--ignore-user-config", "--skip-git-repo-check", "--sandbox", "read-only", "-"}
	if !reflect.DeepEqual(Codex.AskArgs, wantCodex) {
		t.Errorf("codex = %q", Codex.AskArgs)
	}
}

func TestClaudeAnswer(t *testing.T) {
	p := Claude.parser()
	events := feedFixture(t, p, "claude-answer.jsonl")
	if got := texts(events); got != "Let me look.\n\nIt charges in cents." {
		t.Fatalf("text = %q", got)
	}
	if got := tools(events); !reflect.DeepEqual(got, []Tool{{Name: "Read", Summary: "acme/billing.go"}}) {
		t.Fatalf("tools = %+v", got)
	}
	id, ended, failure := p.result()
	if id != "11111111-aaaa-4bbb-8ccc-000000000001" || !ended || failure != nil {
		t.Fatalf("result = %q %v %v", id, ended, failure)
	}
}

func TestClaudeReportsTheToolsItWasRefused(t *testing.T) {
	p := Claude.parser()
	events := feedFixture(t, p, "claude-denied.jsonl")
	var denied []Tool
	for _, e := range events {
		if e.Type == EventDenied {
			denied = append(denied, Tool{Name: e.Name, Summary: e.Summary})
		}
	}
	want := []Tool{{Name: "Edit", Summary: "acme/billing.go"}, {Name: "Bash", Summary: "rm -rf build"}}
	if !reflect.DeepEqual(denied, want) {
		t.Fatalf("denied = %+v", denied)
	}
	if events[len(events)-1].Type != EventDenied {
		t.Fatalf("the denials do not come last: %+v", events)
	}
	if _, ended, failure := p.result(); !ended || failure != nil {
		t.Fatalf("result = %v %+v", ended, failure)
	}
}

func TestClaudeLoggedOut(t *testing.T) {
	p := Claude.parser()
	feedFixture(t, p, "claude-logged-out.jsonl")
	_, ended, failure := p.result()
	if !ended || failure == nil || failure.Kind != KindHarnessLoggedOut || !strings.Contains(failure.Message, "/login") {
		t.Fatalf("failure = %+v", failure)
	}
}

func TestClaudeFailedPassesTheMessageThrough(t *testing.T) {
	p := Claude.parser()
	feedFixture(t, p, "claude-failed.jsonl")
	_, _, failure := p.result()
	if failure == nil || failure.Kind != KindHarnessFailed || failure.Message != "API Error: 529 overloaded" {
		t.Fatalf("failure = %+v", failure)
	}
}

func TestCodexAnswer(t *testing.T) {
	p := Codex.parser()
	events := feedFixture(t, p, "codex-answer.jsonl")
	if got := texts(events); got != "It charges in cents.\n\nAnything else?" {
		t.Fatalf("text = %q", got)
	}
	if got := tools(events); !reflect.DeepEqual(got, []Tool{{Name: "shell", Summary: "rg -n charge acme"}}) {
		t.Fatalf("tools = %+v", got)
	}
	id, ended, failure := p.result()
	if id != "0199aaaa-bbbb-7ccc-8ddd-000000000001" || !ended || failure != nil {
		t.Fatalf("result = %q %v %+v", id, ended, failure)
	}
}

func TestCodexFailures(t *testing.T) {
	for name, kind := range map[string]string{
		"codex-failed.jsonl":     KindHarnessFailed,
		"codex-logged-out.jsonl": KindHarnessLoggedOut,
	} {
		p := Codex.parser()
		feedFixture(t, p, name)
		_, ended, failure := p.result()
		if !ended || failure == nil || failure.Kind != kind {
			t.Errorf("%s: failure = %+v", name, failure)
		}
	}
}

func TestNonJSONLinesAreKeptForTheExplanation(t *testing.T) {
	p := Claude.parser()
	if events := p.feed([]byte("Error: something broke")); len(events) != 0 {
		t.Fatalf("events = %+v", events)
	}
	if got := p.noise(); got != "Error: something broke" {
		t.Fatalf("noise = %q", got)
	}
}

func TestLookup(t *testing.T) {
	if h, err := Lookup("codex"); err != nil || h.Name != "codex" {
		t.Fatalf("got %v %v", h, err)
	}
	if _, err := Lookup("pi"); err == nil || !strings.Contains(err.Error(), "claude or codex") {
		t.Fatalf("err = %v", err)
	}
}
