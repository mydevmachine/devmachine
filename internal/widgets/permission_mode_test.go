package widgets

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func modeWidget(t *testing.T, mode string, edits ...string) string {
	t.Helper()
	body := edited(t, digestWidget, `">= 1.1"`, `">= 1.6"`, "  harness: claude\n", "  harness: claude\n  permission_mode: "+mode+"\n")
	return edited(t, body, edits...)
}

func TestAPermissionModeLoads(t *testing.T) {
	c := CurrentContract()
	for harness, modes := range c.Sources[SourcePrompt].Fields["permission_mode"].ByHarness {
		for _, mode := range modes {
			body := modeWidget(t, mode, "harness: claude", "harness: "+harness)
			w, problems := Load(writeWidget(t, t.TempDir(), "digest", body))
			if len(problems) != 0 {
				t.Errorf("%s %s: %v", harness, mode, problems)
				continue
			}
			if w.Source.PermissionMode != mode {
				t.Errorf("%s %s read as %q", harness, mode, w.Source.PermissionMode)
			}
		}
	}
	for name, body := range map[string]string{
		"dangerous, every left out": modeWidget(t, "bypassPermissions"),
		"dangerous, every manual":   modeWidget(t, "bypassPermissions", "today.\n", "today.\n  every: manual\n"),
		"safe mode on a timer":      modeWidget(t, "auto", "today.\n", "today.\n  every: 30m\n"),
		"codex flag on a timer":     modeWidget(t, "approve-for-me", "harness: claude", "harness: codex", "today.\n", "today.\n  every: 1h\n"),
		"empty is not written":      modeWidget(t, `""`),
	} {
		if _, problems := Load(writeWidget(t, t.TempDir(), "digest", body)); len(problems) != 0 {
			t.Errorf("%s: %v", name, problems)
		}
	}
}

func TestEachPermissionModeRuleReportsItsLine(t *testing.T) {
	const claudeList = "manual, dontAsk, plan, acceptEdits, auto, bypassPermissions"
	const codexList = "read-only, workspace-write, danger-full-access, approve-for-me, dangerously-bypass-approvals-and-sandbox"
	cases := []struct {
		name, folder, body, want string
		line                     int
	}{
		{"not a mode", "digest", modeWidget(t, "yolo"),
			`source.permission_mode "yolo": a claude prompt takes ` + claudeList, 9},
		{"codex mode on claude", "digest", modeWidget(t, "read-only"),
			`source.permission_mode "read-only": a claude prompt takes ` + claudeList, 9},
		{"claude mode on codex", "digest", modeWidget(t, "auto", "harness: claude", "harness: codex"),
			`source.permission_mode "auto": a codex prompt takes ` + codexList, 9},
		{"a template", "digest", modeWidget(t, `"{{inputs.mode}}"`),
			`source.permission_mode "{{inputs.mode}}": a claude prompt takes ` + claudeList, 9},
		{"dangerous on a timer", "digest", modeWidget(t, "bypassPermissions", "today.\n", "today.\n  every: 30m\n"),
			"permission_mode bypassPermissions runs without any check, so it runs only when you press refresh: write every: manual", 9},
		{"codex dangerous on a timer", "digest", modeWidget(t, "danger-full-access", "harness: claude", "harness: codex", "today.\n", "today.\n  every: 1h\n"),
			"permission_mode danger-full-access runs without any check, so it runs only when you press refresh: write every: manual", 9},
		{"dangerous with an unreadable every", "digest", modeWidget(t, "bypassPermissions", "today.\n", "today.\n  every: often\n"),
			`source.every "often" is neither a duration nor manual`, 11},
		{"engine admits 1.5", "digest", modeWidget(t, "auto", `">= 1.6"`, `">= 1.1"`),
			`a widget with source.permission_mode needs requires.engine ">= 1.6": an app on engine 1.5 cannot run its prompt in that mode`, 4},
		{"harness unknown", "digest", modeWidget(t, "auto", "harness: claude", "harness: gpt"),
			`source.harness "gpt": a prompt source takes claude, codex`, 8},
		{"on a command", "disk", edited(t, diskWidget, "  parse: text\n", "  parse: text\n  permission_mode: auto\n"),
			"source.permission_mode is not a field of a command source", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, problems := Load(writeWidget(t, t.TempDir(), tc.folder, tc.body))
			if len(problems) != 1 || !strings.Contains(problems[0].Message, tc.want) {
				t.Fatalf("want one problem containing %q, got %v", tc.want, problems)
			}
			if problems[0].Line == 0 || (tc.line != 0 && problems[0].Line != tc.line) {
				t.Fatalf("want line %d, got %+v", tc.line, problems[0])
			}
		})
	}
}

const askBoard = `format: 1
surface: home
widgets:
  - id: ask
    title: Web search
    source:
      kind: prompt
      harness: claude
      permission_mode: bypassPermissions
      prompt: Search the web for this week's Go release notes.
      every: manual
    view: {kind: markdown}
    frame: {x: 24, y: 24, w: 320, h: 320}
    size: large
    minimized: false
    z: 1
`

func TestAnInlinePromptRunsADangerousModeOnlyByHand(t *testing.T) {
	if problems := boardProblemsOf(t, askBoard); len(problems) != 0 {
		t.Fatal(problems)
	}
	problems := boardProblemsOf(t, edited(t, askBoard, "every: manual", "every: 30m"))
	want := "ask: permission_mode bypassPermissions runs without any check, so it runs only when you press refresh: write every: manual"
	if len(problems) != 1 || problems[0].Message != want || problems[0].Line != 9 {
		t.Fatalf("got %+v", problems)
	}
	problems = boardProblemsOf(t, edited(t, askBoard, "bypassPermissions", "yolo"))
	if len(problems) != 1 || !strings.HasPrefix(problems[0].Message, `ask: source.permission_mode "yolo": a claude prompt takes`) || problems[0].Line != 9 {
		t.Fatalf("got %+v", problems)
	}
	path := filepath.Join(t.TempDir(), "home.yml")
	b, parsed := ParseBoard(path, []byte(askBoard))
	if len(parsed) != 0 {
		t.Fatal(parsed)
	}
	body, err := EncodeBoard(b)
	if err != nil || string(body) != askBoard {
		t.Fatalf("%v\ngot\n%s", err, body)
	}
}

func TestAPermissionModeRoundTripsThroughJSON(t *testing.T) {
	body, err := json.Marshal(Source{Kind: SourcePrompt, Harness: "codex", PermissionMode: "workspace-write", Prompt: "Hi"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"permission_mode":"workspace-write"`) {
		t.Fatalf("got %s", body)
	}
	plain, err := json.Marshal(Source{Kind: SourcePrompt, Harness: "codex", Prompt: "Hi"})
	if err != nil || strings.Contains(string(plain), "permission_mode") {
		t.Fatalf("%v: %s", err, plain)
	}
}
