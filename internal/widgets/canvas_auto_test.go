package widgets

import (
	"path/filepath"
	"strings"
	"testing"
)

const autoUsage = "frame: {x: 352, y: 24, w: 320, h: 160}\n    size: medium"

func homeProblemsOf(t *testing.T, body string) []Problem {
	t.Helper()
	path := filepath.Join(t.TempDir(), "home.yml")
	b, problems := ParseBoard(path, []byte(body))
	return append(problems, ValidateBoard(b, path, stackLookup)...)
}

func TestAGrowingViewOnHomeTakesAutoWithOrWithoutH(t *testing.T) {
	for name, to := range map[string]string{
		"no h":            "frame: {x: 352, y: 24, w: 320}\n    size: auto",
		"an h it ignores": "frame: {x: 352, y: 24, w: 320, h: 400}\n    size: auto",
		"a short custom":  "frame: {x: 352, y: 24, w: 320, h: 40}\n    size: custom",
	} {
		t.Run(name, func(t *testing.T) {
			if problems := homeProblemsOf(t, edited(t, homeBoard, autoUsage, to)); len(problems) != 0 {
				t.Fatal(problems)
			}
		})
	}
}

func TestEachCanvasAutoRuleReportsItsOwnProblem(t *testing.T) {
	const clock = "frame: {x: 24, y: 24, w: 320, h: 160}\n    size: medium"
	cases := []struct{ name, from, to, want string }{
		{"auto on a view that does not grow", clock, "frame: {x: 24, y: 24, w: 320}\n    size: auto",
			"clock: size auto follows the content, and the app.clock view does not grow: use small, medium"},
		{"no h without auto", clock, "frame: {x: 24, y: 24, w: 320}\n    size: medium",
			"clock: frame needs h unless size is auto"},
		{"no h on a growing view without auto", autoUsage, "frame: {x: 352, y: 24, w: 320}\n    size: custom",
			"usage: frame needs h unless size is auto"},
		{"a growing view too narrow", autoUsage, "frame: {x: 352, y: 24, w: 100}\n    size: auto",
			"usage: frame width 100 is narrower than claude-code/usage's minimum of 160"},
		{"a growing view with h of zero", autoUsage, "frame: {x: 352, y: 24, w: 320, h: 0}\n    size: custom",
			"usage: frame h must be above zero"},
		{"a view that does not grow, too small", clock, "frame: {x: 24, y: 24, w: 320, h: 80}\n    size: custom",
			"clock: frame 320x80 is smaller than devmachine-app/clock's minimum of 160x160"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			problems := homeProblemsOf(t, edited(t, homeBoard, tc.from, tc.to))
			if len(problems) != 1 || !strings.Contains(problems[0].Message, tc.want) {
				t.Fatalf("want one problem containing %q, got %v", tc.want, problems)
			}
			if problems[0].Line == 0 {
				t.Fatal("the problem has no line")
			}
		})
	}
}

func TestAnUnknownTypeOnHomeNeedsWidthAndAutoOrHeight(t *testing.T) {
	body := strings.Replace(homeBoard, "type: devmachine-app/clock", "type: gone/widget", 1)
	if problems := homeProblemsOf(t, edited(t, body, "w: 320, h: 160}\n    size: medium\n    minimized: false\n    z: 1", "w: 320}\n    size: auto\n    minimized: false\n    z: 1")); len(problems) != 0 {
		t.Fatal(problems)
	}
	problems := homeProblemsOf(t, edited(t, body, "w: 320, h: 160}\n    size: medium\n    minimized: false\n    z: 1", "w: 320}\n    size: medium\n    minimized: false\n    z: 1"))
	if len(problems) != 1 || !strings.Contains(problems[0].Message, "clock: frame needs h unless size is auto") {
		t.Fatalf("got %v", problems)
	}
}

func TestEachInlineCanvasAutoRuleReportsItsOwnProblem(t *testing.T) {
	const frame = "frame: {x: 24, y: 24, w: 320, h: 160}\n    size: medium"
	cases := []struct {
		name  string
		edits []string
		want  string
	}{
		{"auto on a view that does not grow", []string{"every: 60s", "every: 60s\n      parse: number", "view: {kind: text, tail: 20}", "view: {kind: gauge}", frame, "frame: {x: 24, y: 24, w: 320}\n    size: auto"},
			"disk: size auto follows the content, and the gauge view does not grow: use medium, wide"},
		{"no h without auto", []string{frame, "frame: {x: 24, y: 24, w: 320}\n    size: medium"},
			"disk: frame needs h unless size is auto"},
		{"too narrow", []string{frame, "frame: {x: 24, y: 24, w: 100}\n    size: auto"},
			"disk: frame width 100 is narrower than its minimum of 320"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			problems := boardProblemsOf(t, edited(t, inlineBoard, tc.edits...))
			if len(problems) != 1 || !strings.Contains(problems[0].Message, tc.want) {
				t.Fatalf("want one problem containing %q, got %v", tc.want, problems)
			}
		})
	}
}

func TestAnInlineGrowingViewTakesAutoWithoutH(t *testing.T) {
	body := edited(t, inlineBoard, "frame: {x: 24, y: 24, w: 320, h: 160}\n    size: medium", "frame: {x: 24, y: 24, w: 320}\n    size: auto")
	if problems := boardProblemsOf(t, body); len(problems) != 0 {
		t.Fatal(problems)
	}
}

func TestAnAutoEntryIsWrittenWithoutH(t *testing.T) {
	body := edited(t, homeBoard, autoUsage, "frame: {x: 352, y: 24, w: 320}\n    size: auto")
	path := filepath.Join(t.TempDir(), "home.yml")
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
	b.Widgets[1].Frame.H = 400
	encoded, _ = EncodeBoard(b)
	if string(encoded) != body {
		t.Fatalf("an h on an auto entry was written:\n%s", encoded)
	}
}

func TestFreeSpotCountsAnAutoNeighbourAtItsDefaultSizeHeight(t *testing.T) {
	lookup := func(name string) (Entry, bool) {
		return Entry{Name: name, DefaultSize: "large"}, name == "claude-code/summary"
	}
	auto := Instance{Type: "claude-code/summary", Size: SizeAuto, Frame: Frame{X: 24, Y: 24, W: 1280}}
	inline := Instance{Inline: true, Size: SizeAuto, Frame: Frame{X: 24, Y: 24, W: 1280}}
	cases := []struct {
		name   string
		widget Instance
		want   Frame
	}{
		{"typed, below the default size height", auto, Frame{X: 24, Y: 352, W: 320, H: 160}},
		{"inline, below 160", inline, Frame{X: 24, Y: 192, W: 320, H: 160}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := FreeSpot(EstimateHeights(Board{Widgets: []Instance{tc.widget}}, lookup), 320, 160)
			if got != tc.want {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}
