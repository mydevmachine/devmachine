package widgets

import (
	"path/filepath"
	"strings"
	"testing"
)

const contextBoard = `format: 1
surface: context-sidebar
widgets:
  - id: todo
    type: devmachine-app/todo
    size: auto
  - id: usage
    type: claude-code/usage
    with: {harness: claude}
    size: medium
    collapsed: true
  - id: notes
    title: Files in the session folder
    source:
      kind: command
      run: ls
      args: ["{{context.path}}"]
      every: 30s
      parse: lines
    view: {kind: list}
    size: medium
`

func stackLookup(name string) (Entry, bool) {
	entries := map[string]Entry{
		"devmachine-app/todo": {
			Name: "devmachine-app/todo", Fits: []string{LayoutStack}, View: ViewRef{Kind: "app.todo"},
			Context: map[string]string{"session": ContextRequired}, Sizes: []string{"medium", "large"},
		},
		"devmachine-app/workspaces": {
			Name: "devmachine-app/workspaces", Fits: []string{LayoutStack}, View: ViewRef{Kind: "app.workspaces"},
			Sizes: []string{"medium", "large"}, Single: true,
		},
		"devmachine-app/clock": {
			Name: "devmachine-app/clock", Fits: []string{LayoutCanvas}, View: ViewRef{Kind: "app.clock"},
			Sizes: []string{"small", "medium"},
		},
	}
	if e, ok := entries[name]; ok {
		return e, true
	}
	return usageLookup(name)
}

func stackProblemsOf(t *testing.T, surface, body string) []Problem {
	t.Helper()
	path := filepath.Join(t.TempDir(), surface+".yml")
	b, problems := ParseBoard(path, []byte(body))
	return append(problems, ValidateBoard(b, path, stackLookup)...)
}

func TestAStackBoardRoundTripsInTheAgreedKeyOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "context-sidebar.yml")
	b, problems := ParseBoard(path, []byte(contextBoard))
	if len(problems) != 0 {
		t.Fatal(problems)
	}
	if got := ValidateBoard(b, path, stackLookup); len(got) != 0 {
		t.Fatal(got)
	}
	if !b.Widgets[1].Collapsed || b.Widgets[0].Size != SizeAuto {
		t.Fatalf("got %+v", b.Widgets)
	}
	body, err := EncodeBoard(b)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != contextBoard {
		t.Fatalf("got\n%s\nwant\n%s", body, contextBoard)
	}
}

func TestAStackEntryWithoutASizeStaysWithout(t *testing.T) {
	body := strings.Replace(contextBoard, "    type: devmachine-app/todo\n    size: auto\n", "    type: devmachine-app/todo\n", 1)
	path := filepath.Join(t.TempDir(), "context-sidebar.yml")
	b, problems := ParseBoard(path, []byte(body))
	problems = append(problems, ValidateBoard(b, path, stackLookup)...)
	if len(problems) != 0 || b.Widgets[0].Size != "" {
		t.Fatalf("got %v %q", problems, b.Widgets[0].Size)
	}
	encoded, err := EncodeBoard(b)
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != body {
		t.Fatalf("got\n%s", encoded)
	}
}

func TestEachStackEntryRuleReportsItsOwnProblem(t *testing.T) {
	cases := []struct{ name, from, to, want string }{
		{"a frame", "    type: devmachine-app/todo\n    size: auto\n", "    type: devmachine-app/todo\n    frame: {x: 0, y: 0, w: 320, h: 160}\n    size: auto\n",
			"todo: a widget in a sidebar has no frame; its place is its position in the list"},
		{"a z", "    type: devmachine-app/todo\n    size: auto\n", "    type: devmachine-app/todo\n    size: auto\n    z: 3\n",
			"todo: a widget in a sidebar has no z; its place is its position in the list"},
		{"minimized", "collapsed: true", "minimized: true",
			"usage: a widget in a sidebar folds with collapsed: true, not minimized"},
		{"custom", "    size: auto\n", "    size: custom\n",
			`todo: size "custom" in a sidebar is auto or one of medium, large`},
		{"a preset it does not take", "    size: auto\n", "    size: wide\n",
			`todo: size "wide" in a sidebar is auto or one of medium, large`},
		{"auto on a view that does not grow", "size: medium\n    collapsed", "size: auto\n    collapsed",
			"usage: size auto follows the content, and the app.harness-usage view does not grow: use small, medium, wide"},
		{"auto on an inline view that does not grow", "    view: {kind: list}\n    size: medium\n", "    view: {kind: list}\n    size: auto\n",
			"notes: size auto follows the content, and the list view does not grow"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !strings.Contains(contextBoard, tc.from) {
				t.Fatalf("fixture has no %q", tc.from)
			}
			problems := stackProblemsOf(t, "context-sidebar", strings.Replace(contextBoard, tc.from, tc.to, 1))
			if len(problems) != 1 || !strings.Contains(problems[0].Message, tc.want) {
				t.Fatalf("want one problem containing %q, got %v", tc.want, problems)
			}
		})
	}
}

func TestAStackProblemPointsAtItsKey(t *testing.T) {
	body := strings.Replace(contextBoard, "    type: devmachine-app/todo\n    size: auto\n", "    type: devmachine-app/todo\n    size: auto\n    z: 3\n", 1)
	problems := stackProblemsOf(t, "context-sidebar", body)
	if len(problems) != 1 || problems[0].Line != 7 {
		t.Fatalf("want line 7, got %v", problems)
	}
}

func TestAnUnknownTypeInAStackNeedsNoFrame(t *testing.T) {
	body := strings.Replace(contextBoard, "type: devmachine-app/todo", "type: gone/widget", 1)
	if problems := stackProblemsOf(t, "context-sidebar", body); len(problems) != 0 {
		t.Fatal(problems)
	}
}

func TestCollapsedIsNotAHomeKey(t *testing.T) {
	body := strings.Replace(homeBoard, "minimized: false\n    z: 1", "minimized: false\n    collapsed: true\n    z: 1", 1)
	problems := stackProblemsOf(t, "home", body)
	if len(problems) != 1 || !strings.Contains(problems[0].Message, "clock: a widget on a canvas folds with minimized: true, not collapsed") {
		t.Fatalf("got %v", problems)
	}
}
