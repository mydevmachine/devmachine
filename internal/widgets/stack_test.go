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
		"claude-code/meter": {
			Name: "claude-code/meter", Fits: []string{LayoutStack}, View: ViewRef{Kind: "gauge"},
			Sizes: []string{"small", "medium"},
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
		{"auto on a view that does not grow", "type: claude-code/usage\n    with: {harness: claude}\n    size: medium", "type: claude-code/meter\n    size: auto",
			"usage: size auto follows the content, and the gauge view does not grow: use small, medium"},
		{"auto on an inline view that does not grow", "parse: lines\n    view: {kind: list}\n    size: medium\n", "parse: number\n    view: {kind: gauge}\n    size: auto\n",
			"notes: size auto follows the content, and the gauge view does not grow: use small, medium, tall, large, wide"},
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

const sidebarBoard = `format: 1
surface: sidebar
widgets:
  - id: workspaces
    type: devmachine-app/workspaces
    size: auto
`

func TestEachFitRuleReportsItsOwnProblem(t *testing.T) {
	cases := []struct{ name, surface, body, want string }{
		{"a widget that does not fit a stack", "context-sidebar",
			strings.Replace(contextBoard, "type: devmachine-app/todo\n    size: auto", "type: devmachine-app/clock\n    size: medium", 1),
			"todo: devmachine-app/clock does not fit the context-sidebar area: its fits has no stack"},
		{"a context key the area does not give", "sidebar",
			sidebarBoard + "  - id: todo\n    type: devmachine-app/todo\n    size: auto\n",
			"todo: devmachine-app/todo needs context.session, which the sidebar area does not give"},
		{"a single widget twice", "sidebar",
			sidebarBoard + "  - id: workspaces-2\n    type: devmachine-app/workspaces\n",
			"workspaces-2: devmachine-app/workspaces goes on a board once, and workspaces already has it"},
		{"an inline provider that needs a session", "sidebar",
			sidebarBoard + "  - id: keys\n    title: Shortcuts\n    source: {kind: provider, name: app/shortcuts, every: 60s}\n    view: {kind: app.shortcuts}\n    size: auto\n",
			"keys: source.name app/shortcuts needs context.session, which this board's area does not give"},
		{"an inline sidebar view on Home", "home",
			homeBoard + "  - id: port\n    title: Publish\n    source: {kind: provider, name: app/publish-port, every: 60s}\n    view: {kind: app.publish-port}\n    frame: {x: 24, y: 400, w: 320, h: 160}\n    size: medium\n    minimized: false\n    z: 3\n",
			"port: the app.publish-port view is drawn only in stack, and this board's area is laid out as canvas"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			problems := stackProblemsOf(t, tc.surface, tc.body)
			if len(problems) != 1 || !strings.Contains(problems[0].Message, tc.want) {
				t.Fatalf("want one problem containing %q, got %v", tc.want, problems)
			}
		})
	}
}

func TestTheSidebarBoardWithItsWorkspacesIsFine(t *testing.T) {
	if problems := stackProblemsOf(t, "sidebar", sidebarBoard); len(problems) != 0 {
		t.Fatal(problems)
	}
	if problems := stackProblemsOf(t, "context-sidebar", contextBoard); len(problems) != 0 {
		t.Fatal(problems)
	}
}

func TestAutoOnAnInlineEntryWithNoViewNamesNoBlankView(t *testing.T) {
	body := strings.Replace(contextBoard, "    view: {kind: list}\n    size: medium\n", "    size: auto\n", 1)
	problems := stackProblemsOf(t, "context-sidebar", body)
	if len(problems) != 2 || !strings.Contains(problems[0].Message, "notes: size auto follows the content, and a widget with no view does not grow") {
		t.Fatalf("got %v", problems)
	}
	if strings.Contains(problems[0].Message, "  ") {
		t.Fatalf("double space in %q", problems[0].Message)
	}
}

const defaultContextSidebar = `format: 1
surface: context-sidebar
widgets:
  - id: shortcuts
    type: devmachine-app/shortcuts
    size: auto
  - id: publish-port
    type: devmachine-app/publish-port
    size: auto
  - id: monitors
    type: devmachine-app/monitors
    size: auto
  - id: shells
    type: devmachine-app/shells
    size: auto
  - id: sub-agents
    type: devmachine-app/sub-agents
    size: auto
  - id: todo
    type: devmachine-app/todo
    size: auto
  - id: pull-requests
    type: devmachine-app/pull-requests
    size: auto
  - id: links
    type: devmachine-app/links
    size: auto
`

const defaultMenubar = `format: 1
surface: menubar
widgets:
  - id: brand
    type: devmachine-app/brand
  - id: open-pull-requests
    type: devmachine-app/open-pull-requests
`

const defaultMenubarPanel = `format: 1
surface: menubar-panel
widgets:
  - id: pull-requests-panel
    type: devmachine-app/pull-requests-panel
  - id: usage-panel
    type: devmachine-app/usage-panel
`

func TestTheDefaultBoardsDrawWhatTheAppAlwaysShowed(t *testing.T) {
	for surface, want := range map[string]string{
		"sidebar":         "format: 1\nsurface: sidebar\nwidgets:\n  - id: workspaces\n    type: devmachine-app/workspaces\n    size: auto\n",
		"context-sidebar": defaultContextSidebar,
		"home":            "format: 1\nsurface: home\nwidgets: []\n",
		"menubar":         defaultMenubar,
		"menubar-panel":   defaultMenubarPanel,
	} {
		body, err := EncodeBoard(DefaultBoard(surface))
		if err != nil {
			t.Fatal(err)
		}
		if string(body) != want {
			t.Errorf("%s: got\n%s", surface, body)
		}
	}
	for _, surface := range []string{"menubar", "menubar-panel"} {
		path := filepath.Join(t.TempDir(), surface+".yml")
		if problems := ValidateBoard(DefaultBoard(surface), path, menubarLookup); len(problems) != 0 {
			t.Errorf("%s: %v", surface, problems)
		}
	}
}

func TestInsertPutsAWidgetInOrder(t *testing.T) {
	b := Board{Surface: "sidebar", Widgets: []Instance{{ID: "a"}, {ID: "b"}}}
	for _, step := range []struct{ id, after, before string }{{"end", "", ""}, {"first", "", "a"}, {"middle", "a", ""}} {
		if err := b.Insert(Instance{ID: step.id}, step.after, step.before); err != nil {
			t.Fatal(err)
		}
	}
	if got := idsOf(b); got != "first,a,middle,b,end" {
		t.Fatalf("got %s", got)
	}
	if err := b.Insert(Instance{ID: "x"}, "ghost", ""); err == nil || !strings.Contains(err.Error(), `no widget with id "ghost" on the sidebar board`) {
		t.Fatalf("got %v", err)
	}
	if id, ok := b.IDOfType(""); !ok || id != "first" {
		t.Fatalf("got %q %v", id, ok)
	}
}

func idsOf(b Board) string {
	ids := make([]string, len(b.Widgets))
	for i, w := range b.Widgets {
		ids[i] = w.ID
	}
	return strings.Join(ids, ",")
}

func TestMoveReordersTheList(t *testing.T) {
	b := Board{Surface: "sidebar", Widgets: []Instance{{ID: "a"}, {ID: "b"}, {ID: "c"}}}
	if err := b.Move("c", "", "a"); err != nil || idsOf(b) != "c,a,b" {
		t.Fatalf("got %v %s", err, idsOf(b))
	}
	if err := b.Move("c", "b", ""); err != nil || idsOf(b) != "a,b,c" {
		t.Fatalf("got %v %s", err, idsOf(b))
	}
	for _, tc := range []struct{ id, after, before, want string }{
		{"a", "a", "", "a cannot move next to itself"},
		{"ghost", "a", "", `no widget with id "ghost" on the sidebar board`},
		{"a", "", "ghost", `no widget with id "ghost" on the sidebar board`},
		{"a", "", "", "move needs exactly one of after or before"},
	} {
		if err := b.Move(tc.id, tc.after, tc.before); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("want %q, got %v", tc.want, err)
		}
	}
	if idsOf(b) != "a,b,c" {
		t.Fatalf("a refused move changed the list: %s", idsOf(b))
	}
}
