package widgets

import (
	"path/filepath"
	"strings"
	"testing"
)

const overrideBoard = `format: 1
surface: home
widgets:
  - id: usage
    type: claude-code/usage
    title: Claude
    every: 2m
    frame: {x: 24, y: 24, w: 320, h: 160}
    size: medium
    minimized: false
    z: 1
`

func overrideLookup(name string) (Entry, bool) {
	entries := map[string]Entry{
		"claude-code/usage": {Source: Source{Kind: SourceProvider, Name: "app/harness-usage", Every: "60s"}},
		"mine/tail":         {Source: Source{Kind: SourceCommand, Run: "journalctl", Mode: ModeStream}},
		"mine/ask":          {Source: Source{Kind: SourcePrompt, Harness: "claude", Prompt: "Summarize."}},
		"mine/page":         {Source: Source{Kind: SourceURL, URL: "https://example.com", Every: "60s"}},
		"mine/shell":        {Source: Source{Kind: SourceSession, Session: "main", Every: "10s"}},
		"mine/stats":        {Source: Source{Kind: SourceProvider, Name: "mine/stats", Every: "60s"}, Provider: &PackageProvider{MinEvery: "10s"}},
	}
	e, ok := entries[name]
	e.Name, e.Fits, e.View, e.Sizes = name, []string{LayoutCanvas}, ViewRef{Kind: "text"}, []string{"medium"}
	return e, ok
}

func overrideProblemsOf(t *testing.T, body string) []Problem {
	t.Helper()
	path := filepath.Join(t.TempDir(), "home.yml")
	b, problems := ParseBoard(path, []byte(body))
	if len(problems) != 0 {
		t.Fatal(problems)
	}
	return ValidateBoard(b, path, overrideLookup)
}

func TestABoardEntryMayOverrideTitleAndEvery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "home.yml")
	b, problems := ParseBoard(path, []byte(overrideBoard))
	if len(problems) != 0 {
		t.Fatal(problems)
	}
	if got := ValidateBoard(b, path, overrideLookup); len(got) != 0 {
		t.Fatal(got)
	}
	if w := b.Widgets[0]; w.Title != "Claude" || w.Every != "2m" {
		t.Fatalf("got %+v", w)
	}
	body, err := EncodeBoard(b)
	if err != nil || string(body) != overrideBoard {
		t.Fatalf("%v\ngot\n%s", err, body)
	}
}

func TestEachOverrideRuleReportsItsLine(t *testing.T) {
	for _, tc := range []struct {
		name    string
		replace []string
		want    string
		line    int
	}{
		{"empty title", []string{"title: Claude", `title: ""`}, "usage: title is empty: write one, or take the key off to show the widget's own", 6},
		{"below the provider", []string{"every: 2m", "every: 1s"}, "usage: every 1s is below claude-code/usage's minimum of 5s", 7},
		{"not a duration", []string{"every: 2m", "every: often"}, `usage: every "often" is not a duration: write it like 60s or 5m`, 7},
		{"manual on a provider", []string{"every: 2m", "every: manual"}, "usage: every manual: claude-code/usage reads a provider, which runs on a schedule: write a duration like 60s", 7},
		{"a stream", []string{"claude-code/usage", "mine/tail"}, "usage: a stream runs while the widget is on screen, so it takes no every", 7},
		{"below the kind", []string{"claude-code/usage", "mine/ask", "every: 2m", "every: 5m"}, "usage: every 5m is below mine/ask's minimum of 15m", 7},
		{"below a package provider", []string{"claude-code/usage", "mine/stats", "every: 2m", "every: 5s"}, "usage: every 5s is below mine/stats's minimum of 10s", 7},
		{"unknown type, bad shape", []string{"claude-code/usage", "mine/gone", "every: 2m", "every: soon"}, `usage: every "soon" is neither a duration nor manual: write it like 60s, 5m or manual`, 7},
	} {
		t.Run(tc.name, func(t *testing.T) {
			problems := overrideProblemsOf(t, strings.NewReplacer(tc.replace...).Replace(overrideBoard))
			if len(problems) != 1 || problems[0].Message != tc.want || problems[0].Line != tc.line {
				t.Fatalf("want %q at %d, got %+v", tc.want, tc.line, problems)
			}
		})
	}
	for _, ok := range [][]string{
		{"claude-code/usage", "mine/ask", "every: 2m", "every: manual"},
		{"claude-code/usage", "mine/gone", "every: 2m", "every: 1s"},
		{"every: 2m", "every: 5s"},
	} {
		if problems := overrideProblemsOf(t, strings.NewReplacer(ok...).Replace(overrideBoard)); len(problems) != 0 {
			t.Errorf("%v: got %v", ok, problems)
		}
	}
}

func TestAnInlineWidgetSetsEveryInItsSource(t *testing.T) {
	body := `format: 1
surface: home
widgets:
  - id: disk
    title: Disk
    source: {kind: command, run: uptime, every: 60s}
    view: {kind: text}
    every: 2m
    frame: {x: 24, y: 24, w: 320, h: 160}
    size: medium
    minimized: false
    z: 1
`
	problems := overrideProblemsOf(t, body)
	if len(problems) != 1 || problems[0].Message != "disk: a widget written in the board sets how often in source.every, not every" || problems[0].Line != 8 {
		t.Fatalf("got %+v", problems)
	}
}

func TestEveryOverrideOnAURLOrSessionSourceFollowsTheKindMinimum(t *testing.T) {
	for _, tc := range []struct {
		name, typ, every, want string
	}{
		{"url below", "mine/page", "1s", "usage: every 1s is below mine/page's minimum of 5s"},
		{"url at the minimum", "mine/page", "5s", ""},
		{"url manual", "mine/page", "manual", ""},
		{"session below", "mine/shell", "1s", "usage: every 1s is below mine/shell's minimum of 2s"},
		{"session at the minimum", "mine/shell", "2s", ""},
		{"session manual", "mine/shell", "manual", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := strings.NewReplacer("claude-code/usage", tc.typ, "every: 2m", "every: "+tc.every).Replace(overrideBoard)
			problems := overrideProblemsOf(t, body)
			switch {
			case tc.want == "" && len(problems) != 0:
				t.Fatalf("got %+v", problems)
			case tc.want != "" && (len(problems) != 1 || problems[0].Message != tc.want):
				t.Fatalf("want %q, got %+v", tc.want, problems)
			}
		})
	}
}

func TestATitleOfOnlySpacesAndAnEmptyEveryAreRefused(t *testing.T) {
	for _, tc := range []struct {
		name    string
		replace []string
		want    string
		line    int
	}{
		{"spaces title", []string{"title: Claude", `title: "   "`}, "usage: title is empty: write one, or take the key off to show the widget's own", 6},
		{"empty every", []string{"every: 2m", `every: ""`}, `usage: every "" is not a duration: write it like 60s or 5m`, 7},
	} {
		t.Run(tc.name, func(t *testing.T) {
			problems := overrideProblemsOf(t, strings.NewReplacer(tc.replace...).Replace(overrideBoard))
			if len(problems) != 1 || problems[0].Message != tc.want || problems[0].Line != tc.line {
				t.Fatalf("want %q at %d, got %+v", tc.want, tc.line, problems)
			}
		})
	}
}
