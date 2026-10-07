package commands

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mydevmachine/devmachine/internal/widgets"
)

const setClockEntry = `  - id: clock
    type: devmachine-app/clock
    frame: {x: 24, y: 24, w: 320, h: 160}
    size: medium
    minimized: false
    z: 1
`

const setUsageEntry = `  - id: usage
    type: claude-code/usage
    with: {harness: claude}
    frame: {x: 352, y: 24, w: 320, h: 160}
    size: medium
    minimized: false
    z: 2
`

const setHomeBoard = "format: 1\nsurface: home\nwidgets:\n" + setClockEntry + setUsageEntry + inlineEntry

func setConfig(t *testing.T) (string, string) {
	t.Helper()
	dir := widgetConfig(t)
	board := widgets.BoardPath(dir, "home")
	writeCommandFile(t, board, setHomeBoard)
	return dir, board
}

func TestWidgetsSetChangesOnlyThatEntry(t *testing.T) {
	dir, board := setConfig(t)
	out, err := execute(t, "--config", dir, "widgets", "set", "usage", "--board", "home",
		"--title", "Codex", "--every", "2m", "--set", "harness=codex")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if out != "changed usage on the home board: title, every, harness\n" {
		t.Fatalf("got %q", out)
	}
	usage := strings.Replace(setUsageEntry, "    with: {harness: claude}\n",
		"    title: Codex\n    with: {harness: codex}\n    every: 2m\n", 1)
	want := "format: 1\nsurface: home\nwidgets:\n" + setClockEntry + usage + inlineEntry
	if got := readCommandFile(t, board); got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}

func TestWidgetsSetClearsAnOverride(t *testing.T) {
	dir, board := setConfig(t)
	if out, err := execute(t, "--config", dir, "widgets", "set", "usage", "--board", "home", "--title", "Codex", "--every", "2m"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if out, err := execute(t, "--config", dir, "widgets", "set", "usage", "--board", "home", "--title", "", "--every", ""); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if got := readCommandFile(t, board); got != setHomeBoard {
		t.Fatalf("got\n%s", got)
	}
}

func TestWidgetsSetWritesAListForAManyChoice(t *testing.T) {
	dir := choiceConfig(t)
	if out, err := execute(t, "--config", dir, "widgets", "add", "mine/machines"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	board := widgets.BoardPath(dir, "home")
	if out, err := execute(t, "--config", dir, "widgets", "set", "machines", "--board", "home", "--set", "machines=main"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if got := readCommandFile(t, board); !strings.Contains(got, "    with: {machines: [main]}\n") {
		t.Fatalf("got\n%s", got)
	}
	if out, err := execute(t, "--config", dir, "widgets", "set", "machines", "--board", "home", "--set", "machines="); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if got := readCommandFile(t, board); !strings.Contains(got, "    with: {machines: []}\n") {
		t.Fatalf("got\n%s", got)
	}
}

func TestWidgetsSetKeepsWorkingWithAMachineTheConfigLacks(t *testing.T) {
	dir := choiceConfig(t)
	if out, err := execute(t, "--config", dir, "widgets", "add", "mine/machines", "--set", "machines=ghost"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	out, err := execute(t, "--config", dir, "widgets", "set", "machines", "--board", "home", "--title", "Mine", "--set", "machines=main,ghost")
	if err != nil {
		t.Fatalf("a machine missing from config.yml locked the board: %v\n%s", err, out)
	}
	if !strings.Contains(out, `warning: machines: input "machines" names machine "ghost", which config.yml does not have: the app leaves it out`) {
		t.Fatalf("got\n%s", out)
	}
}

func TestWidgetsSetRefuses(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"nothing given", []string{"usage"}, "nothing to change: give --title, --every or --set"},
		{"every below the minimum", []string{"usage", "--every", "1s"}, "usage: every 1s is below claude-code/usage's minimum of 5s"},
		{"every not a duration", []string{"usage", "--every", "soon"}, `usage: every "soon" is not a duration: write it like 60s or 5m`},
		{"every manual on a provider", []string{"usage", "--every", "manual"}, "usage: every manual: claude-code/usage reads a provider, which runs on a schedule: write a duration like 60s"},
		{"title only spaces", []string{"usage", "--title", "  "}, `--title is only spaces: write a title, or --title "" to show the widget's own`},
		{"unknown input", []string{"usage", "--set", "colour=red"}, `claude-code/usage has no input "colour": it takes harness`},
		{"unknown id", []string{"ghost", "--title", "x"}, `no widget with id "ghost" on the home board`},
		{"inline every", []string{"disk", "--every", "2m"}, "disk is written in the board: change its source in the board file, and the app asks for approval again"},
		{"inline set", []string{"disk", "--set", "x=1"}, "disk is written in the board: change its source in the board file, and the app asks for approval again"},
		{"inline empty title", []string{"disk", "--title", ""}, "disk is written in the board and needs a title, so --title cannot be empty"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir, board := setConfig(t)
			args := append([]string{"--config", dir, "widgets", "set", "--board", "home"}, tc.args...)
			_, err := execute(t, args...)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q, got %v", tc.want, err)
			}
			if got := readCommandFile(t, board); got != setHomeBoard {
				t.Fatalf("a refused set changed the board:\n%s", got)
			}
		})
	}
	dir := widgetConfig(t)
	if _, err := execute(t, "--config", dir, "widgets", "set", "clock", "--board", "home", "--title", "x"); err == nil ||
		!strings.Contains(err.Error(), "there is no home board at") {
		t.Fatalf("got %v", err)
	}
	if _, err := execute(t, "--config", dir, "widgets", "set", "clock", "--title", "x"); err == nil ||
		!strings.Contains(err.Error(), `required flag(s) "board" not set`) {
		t.Fatalf("got %v", err)
	}
}

func TestWidgetsSetRenamesAnInlineWidget(t *testing.T) {
	dir, board := setConfig(t)
	if out, err := execute(t, "--config", dir, "widgets", "set", "disk", "--board", "home", "--title", "Disk"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	want := strings.Replace(setHomeBoard, "    title: Disk on alice\n", "    title: Disk\n", 1)
	if got := readCommandFile(t, board); got != want {
		t.Fatalf("got\n%s", got)
	}
}

func TestWidgetsSetOnAMissingStackUsesTheDefaultBoard(t *testing.T) {
	dir := stackConfig(t)
	if out, err := execute(t, "--config", dir, "widgets", "set", "workspaces", "--board", "sidebar", "--title", "Places"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	want := "format: 1\nsurface: sidebar\nwidgets:\n  - id: workspaces\n    type: devmachine-app/workspaces\n    title: Places\n    size: auto\n"
	if got := readCommandFile(t, widgets.BoardPath(dir, "sidebar")); got != want {
		t.Fatalf("got\n%s", got)
	}
}

func TestWidgetsSetPrintsJSON(t *testing.T) {
	dir, board := setConfig(t)
	out, err := execute(t, "--config", dir, "--format", "json", "widgets", "set", "usage", "--board", "home", "--title", "Codex")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	var change struct {
		Board  string           `json:"board"`
		Path   string           `json:"path"`
		Widget widgets.Instance `json:"widget"`
	}
	if err := json.Unmarshal([]byte(out), &change); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if change.Board != "home" || change.Path != board || change.Widget.ID != "usage" || change.Widget.Title != "Codex" {
		t.Fatalf("got %+v", change)
	}
}

func TestWidgetsSetRefusesAnInputOnATypeTheCatalogLacks(t *testing.T) {
	dir, board := setConfig(t)
	gone := "  - id: gone\n    type: mine/gone\n    frame: {x: 24, y: 400, w: 320, h: 160}\n    size: medium\n    minimized: false\n    z: 3\n"
	body := setHomeBoard + gone
	writeCommandFile(t, board, body)
	_, err := execute(t, "--config", dir, "widgets", "set", "gone", "--board", "home", "--set", "x=1")
	if err == nil || !strings.Contains(err.Error(), "mine/gone is not in the catalog, so its inputs are unknown") {
		t.Fatalf("got %v", err)
	}
	if got := readCommandFile(t, board); got != body {
		t.Fatalf("a refused set changed the board:\n%s", got)
	}
}

func TestWidgetsSetThatChangesNothingDoesNotRewriteTheBoard(t *testing.T) {
	dir, board := setConfig(t)
	body := "# my board\n" + setHomeBoard
	writeCommandFile(t, board, body)
	out, err := execute(t, "--config", dir, "widgets", "set", "usage", "--board", "home", "--every", "", "--title", "", "--set", "harness=claude")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if out != "changed usage on the home board: title, every, harness\n" {
		t.Fatalf("got %q", out)
	}
	if got := readCommandFile(t, board); got != body {
		t.Fatalf("a set that changes nothing rewrote the board:\n%s", got)
	}
}
