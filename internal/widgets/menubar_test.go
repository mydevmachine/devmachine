package widgets

import (
	"path/filepath"
	"strings"
	"testing"
)

const menubarBoard = `format: 1
surface: menubar
widgets:
  - id: brand
    type: devmachine-app/brand
  - id: prs
    type: devmachine-app/open-pull-requests
  - id: load
    title: Load
    source:
      kind: command
      run: uptime
      every: 60s
    view: {kind: text}
`

const panelBoard = `format: 1
surface: menubar-panel
widgets:
  - id: pull-requests-panel
    type: devmachine-app/pull-requests-panel
  - id: usage-panel
    type: devmachine-app/usage-panel
    size: large
`

func menubarLookup(name string) (Entry, bool) {
	entries := map[string]Entry{
		"devmachine-app/brand": {
			Name: "devmachine-app/brand", Fits: []string{LayoutSlot}, View: ViewRef{Kind: "app.brand"}, Sizes: []string{"small"},
		},
		"devmachine-app/open-pull-requests": {
			Name: "devmachine-app/open-pull-requests", Fits: []string{LayoutSlot}, View: ViewRef{Kind: "number"}, Sizes: []string{"small"},
		},
		"devmachine-app/pull-requests-panel": {
			Name: "devmachine-app/pull-requests-panel", Fits: []string{LayoutTabs}, View: ViewRef{Kind: "app.pull-requests-panel"}, Sizes: []string{"large"},
		},
		"devmachine-app/usage-panel": {
			Name: "devmachine-app/usage-panel", Fits: []string{LayoutTabs}, View: ViewRef{Kind: "app.usage-panel"}, Sizes: []string{"large"},
		},
	}
	if e, ok := entries[name]; ok {
		return e, true
	}
	return stackLookup(name)
}

func menubarProblemsOf(t *testing.T, surface, body string) []Problem {
	t.Helper()
	path := filepath.Join(t.TempDir(), surface+".yml")
	b, problems := ParseBoard(path, []byte(body))
	return append(problems, ValidateBoard(b, path, menubarLookup)...)
}

func TestAMenubarBoardRoundTripsInTheAgreedKeyOrder(t *testing.T) {
	for surface, body := range map[string]string{"menubar": menubarBoard, "menubar-panel": panelBoard} {
		path := filepath.Join(t.TempDir(), surface+".yml")
		b, problems := ParseBoard(path, []byte(body))
		if len(problems) != 0 {
			t.Fatal(problems)
		}
		if got := ValidateBoard(b, path, menubarLookup); len(got) != 0 {
			t.Fatalf("%s: %v", surface, got)
		}
		if surface == "menubar" && b.Widgets[0].Size != "" {
			t.Fatalf("a menubar entry got size %q", b.Widgets[0].Size)
		}
		encoded, err := EncodeBoard(b)
		if err != nil {
			t.Fatal(err)
		}
		if string(encoded) != body {
			t.Fatalf("%s: got\n%s\nwant\n%s", surface, encoded, body)
		}
	}
}

func TestASizeOnATabIsAWarningNotAProblem(t *testing.T) {
	path := filepath.Join(t.TempDir(), "menubar-panel.yml")
	b, _ := ParseBoard(path, []byte(panelBoard))
	warnings := BoardWarnings(b, path)
	if len(warnings) != 1 || warnings[0].Line != 8 ||
		warnings[0].Message != "usage-panel: size is ignored in the menu bar popover: a tab fills it" {
		t.Fatalf("got %v", warnings)
	}
	menubar, _ := ParseBoard(filepath.Join(t.TempDir(), "menubar.yml"), []byte(menubarBoard))
	if got := BoardWarnings(menubar, path); len(got) != 0 {
		t.Fatalf("the menubar warned: %v", got)
	}
}

func TestEachMenubarRuleReportsItsLine(t *testing.T) {
	brand := "  - id: brand\n    type: devmachine-app/brand\n"
	panel := "  - id: usage-panel\n    type: devmachine-app/usage-panel\n"
	cases := []struct {
		name, surface, entries string
		line                   int
		want                   string
	}{
		{"a fourth widget", "menubar",
			"  - id: a\n    type: devmachine-app/brand\n  - id: b\n    type: devmachine-app/brand\n" +
				"  - id: c\n    type: devmachine-app/brand\n  - id: d\n    type: devmachine-app/brand\n",
			10, "the menubar holds 3 widgets: take one off"},
		{"a frame", "menubar", brand + "    frame: {x: 0, y: 0, w: 80, h: 40}\n",
			6, "brand: a widget in the menu bar has no frame; its place is its position in the list"},
		{"a z", "menubar", brand + "    z: 1\n",
			6, "brand: a widget in the menu bar has no z; its place is its position in the list"},
		{"a size", "menubar", brand + "    size: small\n",
			6, "brand: a widget in the menu bar has no size: it is one line of text"},
		{"collapsed", "menubar", brand + "    collapsed: true\n",
			6, "brand: a widget in the menu bar does not fold: take it off with widgets remove"},
		{"minimized", "menubar", brand + "    minimized: false\n",
			6, "brand: a widget in the menu bar does not fold: take it off with widgets remove"},
		{"a view no slot draws", "menubar", "  - id: usage\n    type: claude-code/usage\n",
			5, "usage: the app.harness-usage view cannot be drawn in the menu bar"},
		{"a widget with no slot", "menubar", "  - id: clock\n    type: devmachine-app/clock\n",
			5, "clock: devmachine-app/clock does not fit the menubar area: its fits has no slot"},
		{"an inline gauge", "menubar",
			"  - id: load\n    title: Load\n    source: {kind: command, run: uptime, every: 60s, parse: number}\n    view: {kind: gauge}\n",
			7, "load: the gauge view cannot be drawn in the menu bar"},
		{"a frame on a tab", "menubar-panel", panel + "    frame: {x: 0, y: 0, w: 320, h: 320}\n",
			6, "usage-panel: a tab in the menu bar popover has no frame; its place is its position in the list"},
		{"a z on a tab", "menubar-panel", panel + "    z: 2\n",
			6, "usage-panel: a tab in the menu bar popover has no z; its place is its position in the list"},
		{"a collapsed tab", "menubar-panel", panel + "    collapsed: true\n",
			6, "usage-panel: a tab in the menu bar popover does not fold: take it off with widgets remove"},
		{"a slot widget as a tab", "menubar-panel", brand,
			5, "brand: devmachine-app/brand does not fit the menubar-panel area: its fits has no tabs"},
		{"an inline slot view on a tab", "menubar-panel",
			"  - id: mark\n    title: Mark\n    source: {kind: provider, name: app/brand, every: 1h}\n    view: {kind: app.brand}\n",
			7, "mark: the app.brand view is drawn only in slot, and this board's area is laid out as tabs"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := "format: 1\nsurface: " + tc.surface + "\nwidgets:\n" + tc.entries
			problems := menubarProblemsOf(t, tc.surface, body)
			for _, p := range problems {
				if p.Line == tc.line && p.Message == tc.want {
					return
				}
			}
			t.Fatalf("want line %d %q, got %v", tc.line, tc.want, problems)
		})
	}
}

func TestAnUnknownTypeInTheMenubarIsKept(t *testing.T) {
	body := "format: 1\nsurface: menubar\nwidgets:\n  - id: gone\n    type: mine/gone\n"
	if problems := menubarProblemsOf(t, "menubar", body); len(problems) != 0 {
		t.Fatalf("got %v", problems)
	}
}

func TestTheMenubarCountIsReportedOnceForFiveWidgets(t *testing.T) {
	body := "format: 1\nsurface: menubar\nwidgets:\n"
	for _, id := range []string{"a", "b", "c", "d", "e"} {
		body += "  - id: " + id + "\n    type: devmachine-app/brand\n"
	}
	count := 0
	for _, p := range menubarProblemsOf(t, "menubar", body) {
		if strings.HasPrefix(p.Message, "the menubar holds 3 widgets") {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("the 3-widget message appeared %d times", count)
	}
}

func TestAnInlineTabKeepsItsSizeOnEncode(t *testing.T) {
	body := `format: 1
surface: menubar-panel
widgets:
  - id: notes
    title: Notes
    source: {kind: command, run: uptime, every: 60s}
    view: {kind: text}
    size: large
`
	path := filepath.Join(t.TempDir(), "menubar-panel.yml")
	b, problems := ParseBoard(path, []byte(body))
	if len(problems) != 0 {
		t.Fatal(problems)
	}
	encoded, err := EncodeBoard(b)
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != body {
		t.Fatalf("got\n%s\nwant\n%s", encoded, body)
	}
}

func TestAMenubarWidgetWithNoViewKindNamesNoEmptyView(t *testing.T) {
	lookup := func(name string) (Entry, bool) {
		if name == "mine/blank" {
			return Entry{Name: name, Fits: []string{LayoutSlot}, Sizes: []string{"small"}}, true
		}
		return menubarLookup(name)
	}
	path := filepath.Join(t.TempDir(), "menubar.yml")
	b, _ := ParseBoard(path, []byte("format: 1\nsurface: menubar\nwidgets:\n  - id: blank\n    type: mine/blank\n"))
	problems := ValidateBoard(b, path, lookup)
	if len(problems) != 1 || problems[0].Message != "blank: a widget with no view cannot be drawn in the menu bar" {
		t.Fatalf("got %v", problems)
	}
}
