package commands

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mydevmachine/devmachine/internal/packages"
	"github.com/mydevmachine/devmachine/internal/widgets"
)

const clockWidgetYAML = `format: 1
name: clock
summary: The time where you are.
requires: {engine: ">= 1.0"}
fits: [canvas]
source: {kind: provider, name: app/clock, every: 5s}
view: {kind: app.clock}
sizes: [small, medium]
default_size: medium
places: [home]
`

const usageWidgetYAML = `format: 1
name: usage
summary: Coding-harness usage windows.
requires: {engine: ">= 1.0"}
fits: [canvas, stack, slot]
inputs:
  harness: {type: string, default: claude, summary: Which harness.}
source:
  kind: provider
  name: app/harness-usage
  with: {harness: "{{inputs.harness}}"}
  every: 60s
view: {kind: app.harness-usage}
sizes: [small, medium, wide]
default_size: medium
places: [home]
`

func writeWidgetPackageAt(t *testing.T, parent, pkg, scope string, widgetFiles map[string]string) {
	t.Helper()
	dir := filepath.Join(parent, pkg)
	writeCommandFile(t, filepath.Join(dir, "package.yml"),
		"format: 1\nname: "+pkg+"\nscope: "+scope+"\nsummary: Test widgets.\nrequires: {cli: \">= 0.9.0\"}\nwidgets: widgets\n")
	writeCommandFile(t, filepath.Join(dir, "tasks", "main.yml"), "---\n[]\n")
	for name, body := range widgetFiles {
		writeCommandFile(t, filepath.Join(dir, "widgets", name, "widget.yml"), body)
	}
}

// widgetRelease puts release v40 in the cache with the app's clock and
// Claude Code's usage widget, so nothing reaches the network.
func widgetRelease(t *testing.T, dir string) {
	t.Helper()
	writeCommandFile(t, filepath.Join(packages.CacheDir(dir, "v40"), ".checksum"), "fixture\n")
	release := filepath.Join(packages.CacheDir(dir, "v40"), "packages")
	writeWidgetPackageAt(t, release, "devmachine-app", "machine", map[string]string{"clock": clockWidgetYAML})
	writeWidgetPackageAt(t, release, "claude-code", "workspace", map[string]string{"usage": usageWidgetYAML})
}

func widgetConfig(t *testing.T) string {
	t.Helper()
	dir := configWith(t, "machines: []\npackages: v40\n")
	widgetRelease(t, dir)
	forbidDial(t)
	return dir
}

func listCatalog(t *testing.T, dir string) widgets.Catalog {
	t.Helper()
	out, err := execute(t, "--config", dir, "--format", "json", "widgets", "list")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	var catalog widgets.Catalog
	if err := json.Unmarshal([]byte(out), &catalog); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	return catalog
}

func TestWidgetsListReadsThePinnedRelease(t *testing.T) {
	catalog := listCatalog(t, widgetConfig(t))
	if catalog.Engine != "1.0" || catalog.PackagesRelease != "v40" || len(catalog.Problems) != 0 {
		t.Fatalf("got %+v", catalog)
	}
	var names []string
	for _, e := range catalog.Widgets {
		names = append(names, e.Name)
	}
	if !reflect.DeepEqual(names, []string{"claude-code/usage", "devmachine-app/clock"}) {
		t.Fatalf("got %v", names)
	}
	usage := catalog.Widgets[0]
	if usage.Origin != "release" || usage.Version != "v40" || !usage.Available || !reflect.DeepEqual(usage.Surfaces, []string{"context-sidebar", "home", "sidebar"}) {
		t.Fatalf("got %+v", usage)
	}
}

func TestWidgetsListPrefersYourOwnPackageOfTheSameName(t *testing.T) {
	dir := widgetConfig(t)
	writeWidgetPackageAt(t, packages.LocalDir(dir), "claude-code", "workspace",
		map[string]string{"usage": strings.Replace(usageWidgetYAML, "Coding-harness usage windows.", "Mine.", 1)})

	usage, ok := listCatalog(t, dir).Find("claude-code/usage")
	if !ok || usage.Origin != "local" || usage.Version != "" || usage.Summary != "Mine." {
		t.Fatalf("got %+v", usage)
	}
}

func TestWidgetsListWithoutAPinUsesTheLatestRelease(t *testing.T) {
	dir := configWith(t, "machines: []\n")
	widgetRelease(t, dir)
	forbidDial(t)
	t.Cleanup(stubLatestPackagesRelease(t, "v40"))

	if got := listCatalog(t, dir).PackagesRelease; got != "v40" {
		t.Fatalf("got %q", got)
	}
}

func TestWidgetsListWithNoConfigurationUsesTheLatestRelease(t *testing.T) {
	dir := t.TempDir()
	widgetRelease(t, dir)
	forbidDial(t)
	t.Cleanup(stubLatestPackagesRelease(t, "v40"))

	if got := len(listCatalog(t, dir).Widgets); got != 2 {
		t.Fatalf("got %d widgets", got)
	}
}

func TestWidgetsListFailsWhenTheLatestReleaseCannotBeFound(t *testing.T) {
	dir := t.TempDir()
	forbidDial(t)
	previous := latestPackagesRelease
	latestPackagesRelease = func(context.Context) (string, error) {
		return "", errors.New("finding the latest packages release: no network")
	}
	t.Cleanup(func() { latestPackagesRelease = previous })

	out, err := execute(t, "--config", dir, "--format", "json", "widgets", "list")
	if err == nil || !strings.Contains(err.Error(), "no network") || out != "" {
		t.Fatalf("got %v %q", err, out)
	}
}

func TestWidgetsListKeepsGoingPastABrokenWidget(t *testing.T) {
	dir := widgetConfig(t)
	writeWidgetPackageAt(t, packages.LocalDir(dir), "mine", "machine",
		map[string]string{"bad": strings.Replace(strings.Replace(clockWidgetYAML, "name: clock", "name: bad", 1), "view: {kind: app.clock}", "view: {kind: gauge}", 1)})

	catalog := listCatalog(t, dir)
	if len(catalog.Widgets) != 2 || len(catalog.Problems) != 1 || catalog.Problems[0].Message != `view.kind "gauge" needs engine 1.1` {
		t.Fatalf("got %+v", catalog)
	}
}

func TestWidgetsHelpDescribesOneWidget(t *testing.T) {
	out, err := execute(t, "--config", widgetConfig(t), "widgets", "help", "claude-code/usage")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"sizes: small, medium, wide (default medium)", "input harness (string, default claude)", "fits: context-sidebar, home, sidebar"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	if _, err := execute(t, "--config", widgetConfig(t), "widgets", "help", "nope/nope"); err == nil || !strings.Contains(err.Error(), `no widget named "nope/nope"`) {
		t.Fatalf("got %v", err)
	}
}

func TestWidgetsSchemaJSONIsTheGoldenContract(t *testing.T) {
	out, err := execute(t, "widgets", "schema", "--json")
	if err != nil {
		t.Fatal(err)
	}
	golden, err := os.ReadFile(filepath.Join("..", "widgets", "testdata", "contract.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got, want any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(golden, &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %s", out)
	}
}

func TestWidgetAvailabilityFollowsTheConfigurationAndTheLock(t *testing.T) {
	dir := configWith(t, "machines:\n  - name: main\n    hosts: [203.0.113.10]\n    packages: [github-prs]\n")
	writeCommandFile(t, packages.LockPath(dir), "applied_at: \"2026-10-06T00:00:00Z\"\nmachines:\n  main:\n    - {name: github-prs, source: release}\nworkspaces: {}\n")
	cfg, err := loadConfigOrEmpty(dir)
	if err != nil {
		t.Fatal(err)
	}
	lock, err := packages.LoadLock(dir)
	if err != nil {
		t.Fatal(err)
	}
	state := installedState(cfg, lock)
	if got := state("github-prs"); !got.Added || !got.Synced {
		t.Fatalf("got %+v", got)
	}
	if got := state("other"); got.Added || got.Synced {
		t.Fatalf("got %+v", got)
	}
}

func TestWidgetAvailabilityCountsAWorkspacePackage(t *testing.T) {
	cfg, err := loadConfigOrEmpty(configWith(t, "machines: []\nworkspaces:\n  - name: alice\n    machine: main\n    packages: [claude-code]\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got := installedState(cfg, packages.Lock{})("claude-code"); !got.Added || got.Synced {
		t.Fatalf("got %+v", got)
	}
}

func TestWidgetsListPrintsAbsolutePathsForARelativeConfigFlag(t *testing.T) {
	dir := widgetConfig(t)
	t.Chdir(filepath.Dir(dir))

	for _, e := range listCatalog(t, filepath.Base(dir)).Widgets {
		if !filepath.IsAbs(e.Path) {
			t.Fatalf("got %q", e.Path)
		}
	}
}

func TestWidgetsValidateChecksEachKindOfPath(t *testing.T) {
	dir := widgetConfig(t)
	local := packages.LocalDir(dir)
	writeWidgetPackageAt(t, local, "mine", "machine", map[string]string{"clock": clockWidgetYAML})
	board := filepath.Join(dir, "boards", "home.yml")
	writeCommandFile(t, board, "format: 1\nsurface: home\nwidgets:\n  - id: clock\n    type: devmachine-app/clock\n    frame: {x: 24, y: 24, w: 320, h: 160}\n    size: medium\n    minimized: false\n    z: 1\n")

	for _, path := range []string{
		filepath.Join(local, "mine", "widgets", "clock", "widget.yml"),
		filepath.Join(local, "mine", "widgets", "clock"),
		filepath.Join(local, "mine"),
		board,
	} {
		out, err := execute(t, "--config", dir, "widgets", "validate", path)
		if err != nil {
			t.Errorf("%s: %v\n%s", path, err, out)
		}
		if path != board && !strings.Contains(out, "clock fits home") {
			t.Errorf("%s: the surfaces it fits are missing from\n%s", path, out)
		}
		if path == board && !strings.Contains(out, "1 checked, all fine") {
			t.Errorf("%s: the board's result is missing from\n%s", path, out)
		}
	}
}

func TestWidgetsValidateWithNoPathReportsABrokenOwnPackage(t *testing.T) {
	dir := widgetConfig(t)
	local := packages.LocalDir(dir)
	writeWidgetPackageAt(t, local, "mine", "machine", map[string]string{"clock": clockWidgetYAML})
	writeCommandFile(t, filepath.Join(local, "broken", "package.yml"), "format: 1\nname: [broken\n")
	writeCommandFile(t, filepath.Join(local, "notes", "README.md"), "not a package\n")

	out, err := execute(t, "--config", dir, "widgets", "validate")
	if err == nil || !strings.Contains(err.Error(), "1 problem(s)") {
		t.Fatalf("got %v\n%s", err, out)
	}
	if !strings.Contains(out, filepath.Join(local, "broken", "package.yml")) || !strings.Contains(out, "clock fits home") {
		t.Fatalf("got\n%s", out)
	}
}

func TestWidgetsValidateReportsEveryPackageProblemAtOnce(t *testing.T) {
	dir := widgetConfig(t)
	local := packages.LocalDir(dir)
	writeCommandFile(t, filepath.Join(local, "broken", "package.yml"), "format: 1\nname: [broken\n")
	writeWidgetPackageAt(t, local, "mine", "machine", nil)
	writeCommandFile(t, filepath.Join(local, "plain", "package.yml"), "format: 1\nname: plain\nscope: machine\nsummary: No widgets.\n")
	writeCommandFile(t, filepath.Join(local, "plain", "tasks", "main.yml"), "---\n[]\n")

	out, err := execute(t, "--config", dir, "widgets", "validate",
		filepath.Join(local, "broken"), filepath.Join(local, "mine"), filepath.Join(local, "plain"))
	if err == nil || !strings.Contains(err.Error(), "3 problem(s)") {
		t.Fatalf("got %v\n%s", err, out)
	}
	for _, want := range []string{"broken", "the widgets folder is not there", "declares no `widgets:` folder"} {
		if !strings.Contains(out, want) {
			t.Errorf("%q is missing from\n%s", want, out)
		}
	}
}

func TestWidgetsValidateWithNoPathChecksBoardsAndYourPackages(t *testing.T) {
	dir := widgetConfig(t)
	writeWidgetPackageAt(t, packages.LocalDir(dir), "mine", "machine",
		map[string]string{"clock": strings.Replace(clockWidgetYAML, "every: 5s", "every: 1s", 1)})
	writeCommandFile(t, filepath.Join(dir, "boards", "home.yml"), "format: 1\nsurface: home\nwidgets:\n  - id: Clock\n    type: devmachine-app/clock\n    frame: {x: 24, y: 24, w: 320, h: 160}\n    size: medium\n    minimized: false\n    z: 1\n")

	out, err := execute(t, "--config", dir, "--format", "json", "widgets", "validate")
	if err == nil || !strings.Contains(err.Error(), "2 problem(s)") {
		t.Fatalf("got %v\n%s", err, out)
	}
	var result struct {
		OK       bool              `json:"ok"`
		Problems []widgets.Problem `json:"problems"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatal(err)
	}
	if result.OK || len(result.Problems) != 2 {
		t.Fatalf("got %+v", result)
	}
}

func TestWidgetsValidateReportsABoardsKeyAndRuleProblemsTogether(t *testing.T) {
	dir := widgetConfig(t)
	board := filepath.Join(dir, "boards", "home.yml")
	writeCommandFile(t, board, "format: 1\nsurface: home\ncolor: blue\nwidgets:\n  - id: Clock\n    type: devmachine-app/clock\n    frame: {x: 24, y: 24, w: 320, h: 160}\n    z: 1\n")

	out, err := execute(t, "--config", dir, "widgets", "validate", board)
	if err == nil || !strings.Contains(err.Error(), "2 problem(s)") {
		t.Fatalf("got %v\n%s", err, out)
	}
	for _, want := range []string{`unknown key "color"`, `id "Clock"`} {
		if !strings.Contains(out, want) {
			t.Errorf("%q is missing from\n%s", want, out)
		}
	}
}

func TestWidgetsValidateReportsABoardThatIsNotYAMLOnce(t *testing.T) {
	dir := widgetConfig(t)
	board := filepath.Join(dir, "boards", "home.yml")
	writeCommandFile(t, board, "format: 1\nsurface: [home\n")

	out, err := execute(t, "--config", dir, "widgets", "validate", board)
	if err == nil || !strings.Contains(err.Error(), "1 problem(s)") {
		t.Fatalf("got %v\n%s", err, out)
	}
}

func TestWidgetsListKeepsGoingPastABrokenPackageManifest(t *testing.T) {
	dir := widgetConfig(t)
	writeCommandFile(t, filepath.Join(packages.LocalDir(dir), "mine", "package.yml"), "format: [\n")

	catalog := listCatalog(t, dir)
	if len(catalog.Widgets) != 2 || len(catalog.Problems) != 1 || catalog.Problems[0].Path != filepath.Join(packages.LocalDir(dir), "mine", "package.yml") {
		t.Fatalf("got %+v", catalog)
	}
}

func TestWidgetsListNeverFallsBackFromABrokenLocalPackage(t *testing.T) {
	dir := widgetConfig(t)
	writeCommandFile(t, filepath.Join(packages.LocalDir(dir), "claude-code", "package.yml"), "format: [\n")

	catalog := listCatalog(t, dir)
	if _, ok := catalog.Find("claude-code/usage"); ok || len(catalog.Problems) != 1 {
		t.Fatalf("got %+v", catalog)
	}
}

func TestWidgetsHelpLeavesOutAMissingDefault(t *testing.T) {
	dir := widgetConfig(t)
	writeWidgetPackageAt(t, packages.LocalDir(dir), "mine", "machine",
		map[string]string{"usage": strings.Replace(usageWidgetYAML, "harness: {type: string, default: claude, summary: Which harness.}", "harness: {type: string, summary: Which harness.}", 1)})

	out, err := execute(t, "--config", dir, "widgets", "help", "mine/usage")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "input harness (string): Which harness.") || strings.Contains(out, "<nil>") {
		t.Fatalf("got\n%s", out)
	}
}

func TestWidgetsAddPlacesAtTheFirstFreeSpot(t *testing.T) {
	dir := widgetConfig(t)
	if _, err := execute(t, "--config", dir, "widgets", "add", "devmachine-app/clock"); err != nil {
		t.Fatal(err)
	}
	out, err := execute(t, "--config", dir, "--format", "json", "widgets", "add", "claude-code/usage", "--set", "harness=codex")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	var change struct {
		Widget widgets.Instance `json:"widget"`
	}
	if err := json.Unmarshal([]byte(out), &change); err != nil {
		t.Fatal(err)
	}
	if change.Widget.Frame != (widgets.Frame{X: 352, Y: 24, W: 320, H: 160}) || change.Widget.Z != 2 || change.Widget.With["harness"] != "codex" {
		t.Fatalf("got %+v", change.Widget)
	}

	body := readCommandFile(t, widgets.BoardPath(dir, "home"))
	want := "format: 1\nsurface: home\nwidgets:\n" +
		"  - id: clock\n    type: devmachine-app/clock\n    frame: {x: 24, y: 24, w: 320, h: 160}\n    size: medium\n    minimized: false\n    z: 1\n" +
		"  - id: usage\n    type: claude-code/usage\n    with: {harness: codex}\n    frame: {x: 352, y: 24, w: 320, h: 160}\n    size: medium\n    minimized: false\n    z: 2\n"
	if body != want {
		t.Fatalf("got\n%s", body)
	}
}

func TestWidgetsAddHonoursIDSizeAndPosition(t *testing.T) {
	dir := widgetConfig(t)
	if _, err := execute(t, "--config", dir, "widgets", "add", "claude-code/usage", "--id", "codex-usage", "--size", "wide", "--at", "101,203"); err != nil {
		t.Fatal(err)
	}
	b, _, _, err := widgets.ReadBoard(widgets.BoardPath(dir, "home"))
	if err != nil {
		t.Fatal(err)
	}
	got := b.Widgets[0]
	if got.ID != "codex-usage" || got.Size != "wide" || got.Frame != (widgets.Frame{X: 104, Y: 200, W: 640, H: 160}) {
		t.Fatalf("got %+v", got)
	}
}

func TestWidgetsAddRefuses(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"an unknown widget", []string{"nope/nope"}, `no widget named "nope/nope"`},
		{"a planned area", []string{"claude-code/usage", "--board", "sidebar"}, "the sidebar area arrives in a later version"},
		{"an unknown area", []string{"claude-code/usage", "--board", "desk"}, "there is no desk area"},
		{"a size the widget does not take", []string{"devmachine-app/clock", "--size", "wide"}, `does not come in size "wide"`},
		{"an unknown input", []string{"devmachine-app/clock", "--set", "colour=red"}, `has no input "colour"`},
		{"a malformed --set", []string{"claude-code/usage", "--set", "harness"}, "write it as name=value"},
		{"a malformed --at", []string{"devmachine-app/clock", "--at", "-1,4"}, "write it as x,y"},
		{"a malformed id", []string{"devmachine-app/clock", "--id", "Clock"}, `id "Clock": use lower case`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := widgetConfig(t)
			_, err := execute(t, append([]string{"--config", dir, "widgets", "add"}, tc.args...)...)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q, got %v", tc.want, err)
			}
			if _, statErr := os.Stat(widgets.BoardPath(dir, "home")); statErr == nil {
				t.Fatal("a refused add wrote a board")
			}
		})
	}
}

func TestWidgetsAddRefusesATakenID(t *testing.T) {
	dir := widgetConfig(t)
	if _, err := execute(t, "--config", dir, "widgets", "add", "devmachine-app/clock"); err != nil {
		t.Fatal(err)
	}
	if _, err := execute(t, "--config", dir, "widgets", "add", "claude-code/usage", "--id", "clock"); err == nil || !strings.Contains(err.Error(), `id "clock" is already on the home board`) {
		t.Fatalf("got %v", err)
	}
}

func TestWidgetsAddAndRemoveNeverRewriteAnInvalidBoard(t *testing.T) {
	dir := widgetConfig(t)
	broken := "format: 1\nsurface: home\nwidgets:\n  - id: Clock # keep this comment\n    type: devmachine-app/clock\n    frame: {x: 24, y: 24, w: 320, h: 160}\n    size: medium\n    minimized: false\n    z: 1\n"
	path := widgets.BoardPath(dir, "home")
	writeCommandFile(t, path, broken)

	for _, args := range [][]string{{"add", "claude-code/usage"}, {"remove", "Clock"}} {
		_, err := execute(t, append([]string{"--config", dir, "widgets"}, args...)...)
		if err == nil || !strings.Contains(err.Error(), "the board has a problem, so it was not changed") {
			t.Fatalf("%v: got %v", args, err)
		}
		if readCommandFile(t, path) != broken {
			t.Fatalf("%v rewrote an invalid board", args)
		}
	}
}

func TestWidgetsRemoveTakesOneOff(t *testing.T) {
	dir := widgetConfig(t)
	for _, name := range []string{"devmachine-app/clock", "claude-code/usage"} {
		if _, err := execute(t, "--config", dir, "widgets", "add", name); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := execute(t, "--config", dir, "widgets", "remove", "clock"); err != nil {
		t.Fatal(err)
	}
	b, _, _, _ := widgets.ReadBoard(widgets.BoardPath(dir, "home"))
	if len(b.Widgets) != 1 || b.Widgets[0].ID != "usage" {
		t.Fatalf("got %+v", b.Widgets)
	}
	if _, err := execute(t, "--config", dir, "widgets", "remove", "clock"); err == nil || !strings.Contains(err.Error(), `no widget with id "clock"`) {
		t.Fatalf("got %v", err)
	}
}

func TestWidgetsRemoveWithoutABoardSaysSo(t *testing.T) {
	dir := widgetConfig(t)
	if _, err := execute(t, "--config", dir, "widgets", "remove", "clock"); err == nil || !strings.Contains(err.Error(), "there is no home board") {
		t.Fatalf("got %v", err)
	}
}

func TestWidgetsAddRefusesABoardWithAnUnknownKey(t *testing.T) {
	dir := widgetConfig(t)
	broken := "format: 1\nsurface: home\nwidgets:\n  - id: clock\n    type: devmachine-app/clock\n    frame: {x: 24, y: 24, w: 320, h: 160}\n    colour: red\n    size: medium\n    minimized: false\n    z: 1\n"
	path := widgets.BoardPath(dir, "home")
	writeCommandFile(t, path, broken)

	_, err := execute(t, "--config", dir, "widgets", "add", "claude-code/usage")
	if err == nil || !strings.Contains(err.Error(), "the board has a problem, so it was not changed") || !strings.Contains(err.Error(), "colour") {
		t.Fatalf("got %v", err)
	}
	if readCommandFile(t, path) != broken {
		t.Fatal("add rewrote a board with an unknown key")
	}
}

func readCommandFile(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}
