package widgets

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const homeBoard = `format: 1
surface: home
widgets:
  - id: clock
    type: devmachine-app/clock
    frame: {x: 24, y: 24, w: 320, h: 160}
    size: medium
    minimized: false
    z: 1
  - id: usage
    type: claude-code/usage
    with: {harness: claude}
    frame: {x: 352, y: 24, w: 320, h: 160}
    size: medium
    minimized: false
    z: 2
`

func usageLookup(name string) (Entry, bool) {
	if name != "claude-code/usage" {
		return Entry{}, false
	}
	return Entry{
		Name:   name,
		Fits:   []string{LayoutCanvas, LayoutStack, LayoutSlot},
		View:   ViewRef{Kind: "app.harness-usage"},
		Sizes:  []string{"small", "medium", "wide"},
		Inputs: map[string]Input{"harness": {Type: "string", Default: "claude"}},
	}, true
}

func TestBoardRoundTripsInTheAgreedKeyOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "home.yml")
	b, problems := ParseBoard(path, []byte(homeBoard))
	if len(problems) != 0 {
		t.Fatal(problems)
	}
	if got := ValidateBoard(b, path, usageLookup); len(got) != 0 {
		t.Fatal(got)
	}
	body, err := EncodeBoard(b)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != homeBoard {
		t.Fatalf("got\n%s\nwant\n%s", body, homeBoard)
	}
}

func TestAnEmptyBoardEncodesAnEmptyList(t *testing.T) {
	body, err := EncodeBoard(NewBoard("home"))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "format: 1\nsurface: home\nwidgets: []\n" {
		t.Fatalf("got %q", body)
	}
}

func TestEachBoardRuleReportsItsOwnProblem(t *testing.T) {
	cases := []struct{ name, from, to, want string }{
		{"format unreadable", "format: 1", "format: 2", "board format 2, and this CLI reads board format 1"},
		{"surface unknown", "surface: home", "surface: desk", `surface "desk": the surfaces are`},
		{"id malformed", "id: clock", "id: Clock", `id "Clock": use lower case letters`},
		{"id used twice", "id: usage", "id: clock", `id "clock" is used twice`},
		{"type missing", "    type: devmachine-app/clock\n", "", "every widget needs a type"},
		{"type malformed", "type: devmachine-app/clock", "type: clock", `type "clock" is written <package>/<widget>`},
		{"inline widget", "    type: devmachine-app/clock\n", "    title: Clock\n    source: {kind: provider, name: app/clock}\n    view: {kind: app.clock}\n", "clock: every widget needs source.every, at least 5s for app/clock"},
		{"negative position", "x: 24, y: 24, w: 320", "x: -8, y: 24, w: 320", "frame x and y cannot be negative"},
		{"size unknown", "size: medium\n    minimized: false\n    z: 1", "size: huge\n    minimized: false\n    z: 1", `size "huge" is a preset`},
		{"zero frame on an unknown type", "w: 320, h: 160}\n    size: medium\n    minimized: false\n    z: 1", "w: 0, h: 160}\n    size: medium\n    minimized: false\n    z: 1", "frame w and h must be above zero"},
		{"below the widget's minimum", "x: 352, y: 24, w: 320, h: 160", "x: 352, y: 24, w: 100, h: 160", "smaller than claude-code/usage's minimum of 160x160"},
		{"a preset the widget does not take", "z: 1\n  - id: usage\n    type: claude-code/usage\n    with: {harness: claude}\n    frame: {x: 352, y: 24, w: 320, h: 160}\n    size: medium", "z: 1\n  - id: usage\n    type: claude-code/usage\n    with: {harness: claude}\n    frame: {x: 352, y: 24, w: 320, h: 160}\n    size: large", "does not come in size large"},
		{"an input the widget does not have", "with: {harness: claude}", "with: {colour: red}", `has no input "colour"`},
		{"an input of the wrong type", "with: {harness: claude}", "with: {harness: 3}", `input "harness" is a string, and 3 is not`},
		{"unknown key in the board", "surface: home\n", "surface: home\nx: 1\n", `unknown key "x" in the board`},
		{"unknown key in a widget", "minimized: false\n    z: 1", "minimised: false\n    z: 1", `unknown key "minimised" in a widget`},
		{"unknown key in a frame", "w: 320, h: 160}\n    size: medium\n    minimized: false\n    z: 1", "w: 320, h: 160, d: 1}\n    size: medium\n    minimized: false\n    z: 1", `unknown key "d" in a frame`},
		{"type and an inline source", "    type: devmachine-app/clock\n", "    type: devmachine-app/clock\n    source: {kind: provider, name: app/clock}\n", "a widget has either a type or a source and a view, not both"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !strings.Contains(homeBoard, tc.from) {
				t.Fatalf("fixture has no %q", tc.from)
			}
			body := strings.Replace(homeBoard, tc.from, tc.to, 1)
			path := filepath.Join(t.TempDir(), "home.yml")
			if tc.name == "surface unknown" {
				path = filepath.Join(t.TempDir(), "desk.yml")
			}
			b, problems := ParseBoard(path, []byte(body))
			problems = append(problems, ValidateBoard(b, path, usageLookup)...)
			if len(problems) != 1 || !strings.Contains(problems[0].Message, tc.want) {
				t.Fatalf("want one problem containing %q, got %v", tc.want, problems)
			}
		})
	}
}

func TestTheSidebarsTakeABoard(t *testing.T) {
	for _, surface := range []string{"sidebar", "context-sidebar"} {
		path := filepath.Join(t.TempDir(), surface+".yml")
		if problems := ValidateBoard(Board{Format: 1, Surface: surface}, path, usageLookup); len(problems) != 0 {
			t.Errorf("%s: %v", surface, problems)
		}
	}
}

func TestABoardWhoseSurfaceIsNotItsFileNameIsAProblem(t *testing.T) {
	path := filepath.Join(t.TempDir(), "work.yml")
	problems := ValidateBoard(Board{Format: 1, Surface: "home"}, path, usageLookup)
	if len(problems) != 1 || !strings.Contains(problems[0].Message, `surface is "home" but the file is work.yml`) {
		t.Fatalf("got %v", problems)
	}
}

func TestAnUnknownTypeIsNotABoardProblem(t *testing.T) {
	path := filepath.Join(t.TempDir(), "home.yml")
	body := strings.Replace(homeBoard, "type: devmachine-app/clock", "type: gone/widget", 1)
	b, _ := ParseBoard(path, []byte(body))
	if problems := ValidateBoard(b, path, usageLookup); len(problems) != 0 {
		t.Fatalf("got %v", problems)
	}
}

func TestBrokenBoardYAMLPointsAtTheLine(t *testing.T) {
	_, problems := ParseBoard("home.yml", []byte("format: 1\nsurface: home\nwidgets: [\n"))
	if len(problems) != 1 || problems[0].Line == 0 {
		t.Fatalf("got %v", problems)
	}
}

func TestEveryBoardProblemIsReportedAtOnce(t *testing.T) {
	body := strings.NewReplacer("id: usage", "id: clock", "with: {harness: claude}", "with: {colour: red}").Replace(homeBoard)
	path := filepath.Join(t.TempDir(), "home.yml")
	b, _ := ParseBoard(path, []byte(body))
	if problems := ValidateBoard(b, path, usageLookup); len(problems) != 2 {
		t.Fatalf("want 2 problems, got %v", problems)
	}
}

func TestWriteBoardCreatesAMissingBoard(t *testing.T) {
	path := filepath.Join(t.TempDir(), "boards", "home.yml")
	if err := WriteBoard(path, nil, NewBoard("home")); err != nil {
		t.Fatal(err)
	}
	b, _, problems, err := ReadBoard(path)
	if err != nil || len(problems) != 0 || b.Surface != "home" {
		t.Fatalf("got %+v %v %v", b, problems, err)
	}
}

func TestWriteBoardRefusesWhenTheFileChangedSinceItWasRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "home.yml")
	if err := os.WriteFile(path, []byte(homeBoard), 0o600); err != nil {
		t.Fatal(err)
	}
	b, read, _, err := ReadBoard(path)
	if err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(homeBoard, "z: 2", "z: 9", 1)
	if err := os.WriteFile(path, []byte(edited), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteBoard(path, read, b); !errors.Is(err, ErrBoardChanged) {
		t.Fatalf("got %v", err)
	}
	body, _ := os.ReadFile(path)
	if string(body) != edited {
		t.Fatal("the other writer's change was lost")
	}
}

func TestWriteBoardRefusesWhenABoardAppearedSinceItWasRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "home.yml")
	if err := os.WriteFile(path, []byte(homeBoard), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteBoard(path, nil, NewBoard("home")); !errors.Is(err, ErrBoardChanged) {
		t.Fatalf("got %v", err)
	}
}

func TestWriteBoardLeavesNoTemporaryFileBehind(t *testing.T) {
	dir := t.TempDir()
	if err := WriteBoard(filepath.Join(dir, "home.yml"), nil, NewBoard("home")); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("got %v", entries)
	}
}

func TestFreeSpot(t *testing.T) {
	medium := func(x, y int) Instance { return Instance{Frame: Frame{X: x, Y: y, W: 320, H: 160}} }
	cases := []struct {
		name    string
		widgets []Instance
		want    Frame
	}{
		{"empty board", nil, Frame{X: 24, Y: 24, W: 320, H: 160}},
		{"beside the first", []Instance{medium(24, 24)}, Frame{X: 352, Y: 24, W: 320, H: 160}},
		{"next row when the band is full", []Instance{medium(24, 24), medium(352, 24), medium(680, 24), medium(1008, 24)}, Frame{X: 24, Y: 192, W: 320, H: 160}},
		{"into a gap", []Instance{medium(24, 24), medium(1008, 24)}, Frame{X: 352, Y: 24, W: 320, H: 160}},
	}
	for _, tc := range cases {
		got := FreeSpot(Board{Widgets: tc.widgets}, 320, 160)
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: got %+v, want %+v", tc.name, got, tc.want)
		}
	}
}

func TestFreeSpotPlacesAWidgetWiderThanTheBand(t *testing.T) {
	if got := FreeSpot(Board{}, 2000, 160); got.X != 24 || got.Y != 24 {
		t.Fatalf("got %+v", got)
	}
}

func TestSizesAndMinimums(t *testing.T) {
	if w, h, ok := FrameFor("wide"); !ok || w != 640 || h != 160 {
		t.Fatalf("got %d %d %v", w, h, ok)
	}
	if w, h := MinFrame([]string{"medium", "tall"}); w != 160 || h != 160 {
		t.Fatalf("got %d %d", w, h)
	}
}

func TestIDsZAndRemove(t *testing.T) {
	b := Board{Surface: "home", Widgets: []Instance{{ID: "usage", Z: 3}, {ID: "usage-2", Z: 1}}}
	if got := NextID(b, "usage"); got != "usage-3" {
		t.Fatalf("got %q", got)
	}
	if got := NextID(b, "clock"); got != "clock" {
		t.Fatalf("got %q", got)
	}
	if got := NextZ(b); got != 4 {
		t.Fatalf("got %d", got)
	}
	if err := b.Remove("usage"); err != nil || b.HasID("usage") || len(b.Widgets) != 1 {
		t.Fatalf("got %v %+v", err, b)
	}
	if err := b.Remove("nope"); err == nil || !strings.Contains(err.Error(), `no widget with id "nope" on the home board`) {
		t.Fatalf("got %v", err)
	}
}

func TestSnapAndCoerce(t *testing.T) {
	if Snap(29) != 32 || Snap(27) != 24 || Snap(0) != 0 {
		t.Fatalf("got %d %d %d", Snap(29), Snap(27), Snap(0))
	}
	if v, err := CoerceInput("n", Input{Type: "number"}, "3"); err != nil || v != 3 {
		t.Fatalf("got %v %v", v, err)
	}
	if v, err := CoerceInput("b", Input{Type: "boolean"}, "true"); err != nil || v != true {
		t.Fatalf("got %v %v", v, err)
	}
	if _, err := CoerceInput("n", Input{Type: "number"}, "three"); err == nil {
		t.Fatal("want an error")
	}
	if v, _ := CoerceInput("s", Input{Type: "string"}, "codex"); v != "codex" {
		t.Fatalf("got %v", v)
	}
}

func TestAWidgetWithoutASizeIsCustom(t *testing.T) {
	path := filepath.Join(t.TempDir(), "home.yml")
	body := strings.Replace(homeBoard, "    size: medium\n", "", 1)
	b, problems := ParseBoard(path, []byte(body))
	if len(problems) != 0 {
		t.Fatal(problems)
	}
	if b.Widgets[0].Size != SizeCustom {
		t.Fatalf("got %q", b.Widgets[0].Size)
	}
	if got := ValidateBoard(b, path, usageLookup); len(got) != 0 {
		t.Fatal(got)
	}
	encoded, err := EncodeBoard(b)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), "    size: custom\n") {
		t.Fatalf("got\n%s", encoded)
	}
}

func TestUnknownBoardKeysPointAtTheirLine(t *testing.T) {
	body := strings.Replace(homeBoard, "minimized: false\n    z: 1", "minimised: false\n    z: 1", 1)
	_, problems := ParseBoard("home.yml", []byte(body))
	if len(problems) != 1 || problems[0].Line != 8 {
		t.Fatalf("got %v", problems)
	}
}

const choiceBoard = `format: 1
surface: home
widgets:
  - id: machines
    type: mine/machines
    with: {machines: [main, backup]}
    frame: {x: 24, y: 24, w: 320, h: 320}
    size: large
    minimized: false
    z: 1
  - id: usage
    type: mine/usage
    with: {harness: codex}
    frame: {x: 352, y: 24, w: 320, h: 160}
    size: medium
    minimized: false
    z: 2
`

func choiceLookup(name string) (Entry, bool) {
	switch name {
	case "mine/machines":
		return Entry{Name: name, Fits: []string{LayoutCanvas, LayoutStack}, View: ViewRef{Kind: "app.machines"},
			Sizes: []string{"medium", "large"}, Source: Source{Kind: SourceProvider, Name: "app/machines", Every: "90s"},
			Inputs: map[string]Input{"machines": {Type: InputChoice, From: OptionMachines, Many: true}}}, true
	case "mine/usage":
		return Entry{Name: name, Fits: []string{LayoutCanvas, LayoutStack}, View: ViewRef{Kind: "app.harness-usage"},
			Sizes: []string{"small", "medium"}, Source: Source{Kind: SourceProvider, Name: "app/harness-usage", Every: "60s"},
			Inputs: map[string]Input{"harness": {Type: InputChoice, From: OptionHarnesses}}}, true
	}
	return Entry{}, false
}

func TestAChoiceListRoundTripsAsAFlowList(t *testing.T) {
	path := filepath.Join(t.TempDir(), "home.yml")
	b, problems := ParseBoard(path, []byte(choiceBoard))
	if len(problems) != 0 {
		t.Fatal(problems)
	}
	if got := ValidateBoard(b, path, choiceLookup); len(got) != 0 {
		t.Fatal(got)
	}
	body, err := EncodeBoard(b)
	if err != nil || string(body) != choiceBoard {
		t.Fatalf("%v\ngot\n%s", err, body)
	}
	b.Widgets[0].With["machines"] = []string{}
	body, err = EncodeBoard(b)
	if err != nil || !strings.Contains(string(body), "    with: {machines: []}\n") {
		t.Fatalf("%v\ngot\n%s", err, body)
	}
}

func TestEachChoiceValueRuleReportsItsLine(t *testing.T) {
	for _, tc := range []struct {
		from, to, want string
		line           int
	}{
		{"{machines: [main, backup]}", "{machines: main}", `machines: input "machines" takes a list of names, written [a, b], and main is not one`, 6},
		{"{machines: [main, backup]}", "{machines: [main, 3]}", `machines: input "machines" takes a list of names, written [a, b], and [main 3] is not one`, 6},
		{"{harness: codex}", "{harness: [claude, codex]}", `usage: input "harness" takes one name, and [claude, codex] is not one`, 13},
	} {
		t.Run(tc.to, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "home.yml")
			b, _ := ParseBoard(path, []byte(strings.Replace(choiceBoard, tc.from, tc.to, 1)))
			problems := ValidateBoard(b, path, choiceLookup)
			if len(problems) != 1 || problems[0].Message != tc.want || problems[0].Line != tc.line {
				t.Fatalf("want %q at %d, got %+v", tc.want, tc.line, problems)
			}
		})
	}
}

func TestCoerceInputSplitsAManyChoice(t *testing.T) {
	many := Input{Type: InputChoice, From: OptionMachines, Many: true}
	for raw, want := range map[string][]string{"main, backup,": {"main", "backup"}, "main": {"main"}, "": {}} {
		got, err := CoerceInput("machines", many, raw)
		if names, ok := got.([]string); err != nil || !ok || !reflect.DeepEqual(names, want) {
			t.Errorf("%q: got %#v, %v", raw, got, err)
		}
	}
	one := Input{Type: InputChoice, From: OptionHarnesses}
	if got, err := CoerceInput("harness", one, "codex"); err != nil || got != "codex" {
		t.Fatalf("got %#v, %v", got, err)
	}
	if _, err := CoerceInput("harness", one, "claude,codex"); err == nil || err.Error() != `input harness takes one name, and "claude,codex" is a list` {
		t.Fatalf("got %v", err)
	}
}
