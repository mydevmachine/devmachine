package widgets

import (
	"path/filepath"
	"strings"
	"testing"
)

const inlineBoard = `format: 1
surface: home
widgets:
  - id: disk
    title: Disk on alice
    source:
      kind: command
      run: df
      args: [-h, /]
      target: {workspace: alice}
      every: 60s
    view: {kind: text, tail: 20}
    sizes: [medium, wide]
    frame: {x: 24, y: 24, w: 320, h: 160}
    size: medium
    minimized: false
    z: 1
  - id: clock
    type: devmachine-app/clock
    frame: {x: 352, y: 24, w: 320, h: 160}
    size: medium
    minimized: false
    z: 2
`

func boardProblemsOf(t *testing.T, body string) []Problem {
	t.Helper()
	path := filepath.Join(t.TempDir(), "home.yml")
	b, problems := ParseBoard(path, []byte(body))
	return append(problems, ValidateBoard(b, path, usageLookup)...)
}

func TestAnInlineWidgetIsFineWhenWrittenRight(t *testing.T) {
	if problems := boardProblemsOf(t, inlineBoard); len(problems) != 0 {
		t.Fatal(problems)
	}
}

func TestEachInlineRuleReportsItsOwnProblem(t *testing.T) {
	cases := []struct {
		name  string
		edits []string
		want  string
	}{
		{"title missing", []string{"    title: Disk on alice\n", ""}, "disk: a widget written in the board needs a title"},
		{"view missing", []string{"    view: {kind: text, tail: 20}\n", ""}, "disk: a widget written in the board needs both a source and a view"},
		{"inputs given", []string{"    title: Disk on alice\n", "    title: Disk on alice\n    with: {path: /}\n"}, "disk: a widget written in the board has no inputs, so it takes no with"},
		{"a template naming an input", []string{"args: [-h, /]", `args: [-h, "{{inputs.path}}"]`}, "disk: template {{inputs.path}} in source.args[1]: a widget written in a board has no inputs"},
		{"a context key home does not give", []string{"args: [-h, /]", `args: [-h, "{{context.repo}}"]`}, "needs context.repo to be declared"},
		{"a relative script", []string{"      run: df\n      args: [-h, /]\n", "      script: bin/df\n"}, `disk: source.script "bin/df": a widget written in a board names an absolute path on the target`},
		{"a source rule", []string{"every: 60s", "every: 1s"}, "disk: source.every 1s is below the command minimum of 5s"},
		{"a view rule", []string{"tail: 20", "tail: 9000"}, "disk: view.tail 9000 is outside 1 to 2000 lines"},
		{"a size it does not take", []string{"sizes: [medium, wide]", "sizes: [small, wide]"}, "disk: size medium is not one of its sizes: small, wide"},
		{"below its smallest size", []string{"sizes: [medium, wide]\n    frame: {x: 24, y: 24, w: 320", "sizes: [medium, wide]\n    frame: {x: 24, y: 24, w: 100"}, "disk: frame width 100 is narrower than its minimum of 320"},
		{"fits another layout", []string{"    sizes: [medium, wide]\n", "    sizes: [medium, wide]\n    fits: [stack]\n"}, "disk: fits stack, and this board's area is laid out as canvas"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			problems := boardProblemsOf(t, edited(t, inlineBoard, tc.edits...))
			if len(problems) != 1 || !strings.Contains(problems[0].Message, tc.want) {
				t.Fatalf("want one problem containing %q, got %v", tc.want, problems)
			}
			if problems[0].Line < 4 {
				t.Fatalf("the problem does not point into the entry: %v", problems[0])
			}
		})
	}
}

func TestAnInlineProblemPointsAtItsOwnLine(t *testing.T) {
	problems := boardProblemsOf(t, edited(t, inlineBoard, "every: 60s", "every: 1s"))
	if len(problems) != 1 || problems[0].Line != 11 {
		t.Fatalf("want line 11, got %v", problems)
	}
}

func TestAnInlineWidgetSurvivesARewrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "home.yml")
	b, problems := ParseBoard(path, []byte(inlineBoard))
	if len(problems) != 0 {
		t.Fatal(problems)
	}
	body, err := EncodeBoard(b)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != inlineBoard {
		t.Fatalf("got\n%s\nwant\n%s", body, inlineBoard)
	}

	if err := b.Remove("clock"); err != nil {
		t.Fatal(err)
	}
	body, err = EncodeBoard(b)
	if err != nil {
		t.Fatal(err)
	}
	want := inlineBoard[:strings.Index(inlineBoard, "  - id: clock\n")]
	if string(body) != want {
		t.Fatalf("got\n%s\nwant\n%s", body, want)
	}
}

func TestAnInlineWidgetBuiltInCodeEncodesItsFields(t *testing.T) {
	b := NewBoard("home")
	b.Widgets = append(b.Widgets, Instance{
		ID: "health", Title: "Site", Inline: true,
		Source: &Source{Kind: SourceURL, URL: "https://example.com/health", Every: "30s"},
		View:   &ViewRef{Kind: "status", OK: "< 300"},
		Frame:  Frame{X: 24, Y: 24, W: 160, H: 160}, Size: "small", Z: 1,
	})
	body, err := EncodeBoard(b)
	if err != nil {
		t.Fatal(err)
	}
	back, problems := ParseBoard("home.yml", body)
	if len(problems) != 0 || back.Widgets[0].Source.URL != "https://example.com/health" || back.Widgets[0].View.OK != "< 300" {
		t.Fatalf("got %s / %v", body, problems)
	}
}
