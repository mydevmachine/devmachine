package commands

import (
	"bytes"
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

const diskWidgetYAML = `format: 1
name: disk
summary: Free space on a machine.
requires: {engine: ">= 1.1"}
fits: [canvas]
source: {kind: command, run: df, args: [-h, /], target: {machine: main}, every: 60s, parse: text}
view: {kind: text}
sizes: [medium]
default_size: medium
`

const healthWidgetYAML = `format: 1
name: health
summary: Whether the site answers.
requires: {engine: ">= 1.1"}
fits: [canvas]
source: {kind: url, url: "https://example.com/health", every: 30s}
view: {kind: status}
sizes: [small]
default_size: small
`

const workspacesWidgetYAML = `format: 1
name: workspaces
summary: Your machines and workspaces with their sessions.
requires: {engine: ">= 1.2"}
fits: [stack]
context: {}
source: {kind: provider, name: app/workspaces, every: 5s}
view: {kind: app.workspaces}
sizes: [medium, large]
default_size: large
single: true
places: [sidebar]
`

const todoWidgetYAML = `format: 1
name: todo
summary: The plan of the selected session.
requires: {engine: ">= 1.2"}
fits: [stack]
context: {session: required}
source: {kind: provider, name: app/session-context, every: 5s}
view: {kind: app.todo}
sizes: [medium, large]
default_size: large
places: [context-sidebar]
`

// stackConfig is widgetConfig plus the app's workspaces and todo widgets in
// release v40, for the boards of the two sidebars.
func stackConfig(t *testing.T) string {
	t.Helper()
	dir := widgetConfig(t)
	app := filepath.Join(packages.CacheDir(dir, "v40"), "packages", "devmachine-app", "widgets")
	writeCommandFile(t, filepath.Join(app, "workspaces", "widget.yml"), workspacesWidgetYAML)
	writeCommandFile(t, filepath.Join(app, "todo", "widget.yml"), todoWidgetYAML)
	return dir
}

func TestWidgetsListAndHelpSayASingleWidget(t *testing.T) {
	dir := stackConfig(t)
	catalog := listCatalog(t, dir)
	workspaces, ok := catalog.Find("devmachine-app/workspaces")
	todo, _ := catalog.Find("devmachine-app/todo")
	if !ok || !workspaces.Single || todo.Single || !reflect.DeepEqual(workspaces.Surfaces, []string{"context-sidebar", "sidebar"}) {
		t.Fatalf("got %+v", catalog.Widgets)
	}
	out, err := execute(t, "--config", dir, "widgets", "help", "devmachine-app/workspaces")
	if err != nil || !strings.Contains(out, "once per board\n") {
		t.Fatalf("%v\n%s", err, out)
	}
}

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
	if catalog.Engine != "1.3" || catalog.PackagesRelease != "v40" || len(catalog.Problems) != 0 {
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
	if len(catalog.Widgets) != 2 || len(catalog.Problems) != 1 || catalog.Problems[0].Message != `view.kind "gauge" draws number, json, not app/clock` {
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
		if path != board && !strings.Contains(out, "mine/clock fits home") {
			t.Errorf("%s: the surfaces it fits are missing from\n%s", path, out)
		}
		if path == board && !strings.Contains(out, "1 checked, all fine") {
			t.Errorf("%s: the board's result is missing from\n%s", path, out)
		}
	}
}

func TestWidgetsValidateNamesEachWidgetByItsPackage(t *testing.T) {
	dir := widgetConfig(t)
	local := packages.LocalDir(dir)
	writeWidgetPackageAt(t, local, "mine", "machine", map[string]string{"usage": usageWidgetYAML})
	writeWidgetPackageAt(t, local, "theirs", "machine", map[string]string{"usage": usageWidgetYAML})

	out, err := execute(t, "--config", dir, "widgets", "validate")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, want := range []string{"mine/usage fits ", "theirs/usage fits "} {
		if !strings.Contains(out, want) {
			t.Errorf("%q is missing from\n%s", want, out)
		}
	}

	out, err = execute(t, "--config", dir, "--format", "json", "widgets", "validate", filepath.Join(local, "theirs", "widgets", "usage"))
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	var result struct {
		Widgets []widgetFit `json:"widgets"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Widgets) != 1 || result.Widgets[0].Name != "theirs/usage" {
		t.Fatalf("got %+v", result.Widgets)
	}
}

func TestWidgetsValidateNamesAWidgetOutsideAPackageByItsName(t *testing.T) {
	dir := widgetConfig(t)
	folder := filepath.Join(t.TempDir(), "clock")
	writeCommandFile(t, filepath.Join(folder, "widget.yml"), clockWidgetYAML)

	out, err := execute(t, "--config", dir, "widgets", "validate", folder)
	if err != nil || !strings.HasPrefix(out, "clock fits home") {
		t.Fatalf("got %v\n%s", err, out)
	}
}

func TestWidgetsUnknownSubcommandFails(t *testing.T) {
	_, err := execute(t, "--config", t.TempDir(), "widgets", "frobnicate")
	if err == nil || !strings.Contains(err.Error(), `unknown command "frobnicate" for "devmachine widgets"`) {
		t.Fatalf("got %v", err)
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
	if !strings.Contains(out, filepath.Join(local, "broken", "package.yml")) || !strings.Contains(out, "mine/clock fits home") {
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
		Warnings []widgets.Problem `json:"warnings"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatal(err)
	}
	if result.OK || len(result.Problems) != 2 || result.Warnings == nil {
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

func TestWidgetsAddAtPlacesExactlyThereEvenOverAnotherWidget(t *testing.T) {
	dir := widgetConfig(t)
	if _, err := execute(t, "--config", dir, "widgets", "add", "devmachine-app/clock"); err != nil {
		t.Fatal(err)
	}
	if _, err := execute(t, "--config", dir, "widgets", "add", "claude-code/usage", "--at", "27,21"); err != nil {
		t.Fatal(err)
	}
	b, _, _, err := widgets.ReadBoard(widgets.BoardPath(dir, "home"))
	if err != nil {
		t.Fatal(err)
	}
	if got := b.Widgets[1].Frame; got != (widgets.Frame{X: 24, Y: 24, W: 320, H: 160}) {
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

func typedInputConfig(t *testing.T) string {
	t.Helper()
	dir := widgetConfig(t)
	dial := strings.Replace(clockWidgetYAML, "name: clock", "name: dial", 1)
	dial = strings.Replace(dial, "fits: [canvas]\n", "fits: [canvas]\ninputs:\n  n: {type: number, default: 1, summary: A count.}\n  flag: {type: boolean, default: false, summary: A switch.}\n", 1)
	writeWidgetPackageAt(t, packages.LocalDir(dir), "mine", "machine", map[string]string{"dial": dial})
	return dir
}

func TestWidgetsAddWritesTypedInputs(t *testing.T) {
	dir := typedInputConfig(t)
	if out, err := execute(t, "--config", dir, "widgets", "add", "mine/dial", "--set", "n=5", "--set", "flag=true"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	b, _, problems, err := widgets.ReadBoard(widgets.BoardPath(dir, "home"))
	if err != nil || len(problems) > 0 {
		t.Fatalf("%v %v", err, problems)
	}
	with := b.Widgets[0].With
	if n, ok := with["n"].(int); !ok || n != 5 {
		t.Fatalf("n: got %#v", with["n"])
	}
	if flag, ok := with["flag"].(bool); !ok || !flag {
		t.Fatalf("flag: got %#v", with["flag"])
	}
}

func TestWidgetsAddRefusesAValueOfTheWrongType(t *testing.T) {
	cases := map[string]string{
		"n=five":   `input n is a number, and "five" is not`,
		"flag=yes": `input flag is true or false, and "yes" is neither`,
	}
	for set, want := range cases {
		t.Run(set, func(t *testing.T) {
			dir := typedInputConfig(t)
			_, err := execute(t, "--config", dir, "widgets", "add", "mine/dial", "--set", set)
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("want %q, got %v", want, err)
			}
			if _, statErr := os.Stat(widgets.BoardPath(dir, "home")); statErr == nil {
				t.Fatal("a refused add wrote a board")
			}
		})
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

func TestWidgetsListKeepsTheEngine10ShapeForAProviderWidget(t *testing.T) {
	out, err := execute(t, "--config", widgetConfig(t), "--format", "json", "widgets", "list")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	var raw struct {
		Widgets []map[string]json.RawMessage `json:"widgets"`
	}
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		t.Fatal(err)
	}
	compact := func(m json.RawMessage) string {
		var b bytes.Buffer
		if err := json.Compact(&b, m); err != nil {
			t.Fatal(err)
		}
		return b.String()
	}
	for _, w := range raw.Widgets {
		if compact(w["name"]) != `"claude-code/usage"` {
			continue
		}
		if got := compact(w["source"]); got != `{"kind":"provider","name":"app/harness-usage","with":{"harness":"{{inputs.harness}}"},"every":"60s"}` {
			t.Errorf("source is %s", got)
		}
		if got := compact(w["view"]); got != `{"kind":"app.harness-usage"}` {
			t.Errorf("view is %s", got)
		}
		return
	}
	t.Fatalf("claude-code/usage is missing from %s", out)
}

const inlineEntry = `  - id: disk
    title: Disk on alice
    source:
      kind: command
      run: df
      args: [-h, /]
      target: {workspace: alice}
      every: 60s
    view: {kind: text, tail: 20}
    frame: {x: 24, y: 400, w: 320, h: 160}
    size: medium
    minimized: false
    z: 9
`

func TestWidgetsRemoveKeepsAnInlineWidget(t *testing.T) {
	dir := widgetConfig(t)
	if _, err := execute(t, "--config", dir, "widgets", "add", "devmachine-app/clock"); err != nil {
		t.Fatal(err)
	}
	board := widgets.BoardPath(dir, "home")
	writeCommandFile(t, board, readCommandFile(t, board)+inlineEntry)

	if out, err := execute(t, "--config", dir, "widgets", "add", "claude-code/usage"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if out, err := execute(t, "--config", dir, "widgets", "remove", "clock"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	got := readCommandFile(t, board)
	if !strings.Contains(got, inlineEntry) {
		t.Fatalf("the inline widget did not survive:\n%s", got)
	}
}

func TestWidgetsListPassesANewSourceThrough(t *testing.T) {
	dir := widgetConfig(t)
	writeWidgetPackageAt(t, packages.LocalDir(dir), "mine", "machine",
		map[string]string{"disk": diskWidgetYAML, "health": healthWidgetYAML})
	catalog := listCatalog(t, dir)

	disk, ok := catalog.Find("mine/disk")
	if !ok {
		t.Fatalf("mine/disk is missing: %+v", catalog)
	}
	want := widgets.Source{Kind: "command", Run: "df", Args: []string{"-h", "/"},
		Target: widgets.Target{Machine: "main"}, Every: "60s", Parse: "text"}
	if !reflect.DeepEqual(disk.Source, want) {
		t.Errorf("got %+v", disk.Source)
	}
	if disk.PackagePath != filepath.Join(packages.LocalDir(dir), "mine") {
		t.Errorf("package_path is %q", disk.PackagePath)
	}
	if disk.Available || !strings.Contains(disk.UnavailableReason, "devmachine packages add mine --machine") {
		t.Errorf("a command widget of a package nobody added is available: %+v", disk)
	}
	if health, _ := catalog.Find("mine/health"); !health.Available {
		t.Errorf("a url widget needs no package: %+v", health)
	}
}

func TestWidgetsValidateRefusesAnInlineWidgetOnAnUnknownMachine(t *testing.T) {
	dir := configWith(t, "machines:\n  - name: main\n    hosts: [203.0.113.10]\npackages: v40\n")
	widgetRelease(t, dir)
	forbidDial(t)
	board := filepath.Join(dir, "boards", "home.yml")
	ghost := strings.Replace(inlineEntry, "target: {workspace: alice}", "target: {machine: ghost}", 1)
	writeCommandFile(t, board, "format: 1\nsurface: home\nwidgets:\n"+ghost)

	out, err := execute(t, "--config", dir, "widgets", "validate", board)
	if err == nil || !strings.Contains(out, `disk: source.target names machine "ghost", which config.yml does not have`) {
		t.Fatalf("got %v\n%s", err, out)
	}

	main := strings.Replace(inlineEntry, "target: {workspace: alice}", "target: {machine: main}", 1)
	writeCommandFile(t, board, "format: 1\nsurface: home\nwidgets:\n"+main)
	if out, err := execute(t, "--config", dir, "widgets", "validate", board); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}

func TestWidgetsValidateWarnsAboutAPackageWidgetsUnknownMachine(t *testing.T) {
	dir := widgetConfig(t)
	writeWidgetPackageAt(t, packages.LocalDir(dir), "mine", "machine", map[string]string{"disk": diskWidgetYAML})

	out, err := execute(t, "--config", dir, "--format", "json", "widgets", "validate", filepath.Join(packages.LocalDir(dir), "mine"))
	if err != nil {
		t.Fatalf("a warning failed the check: %v\n%s", err, out)
	}
	var result struct {
		OK       bool              `json:"ok"`
		Warnings []widgets.Problem `json:"warnings"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatal(err)
	}
	if !result.OK || len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0].Message, `machine "main"`) {
		t.Fatalf("got %+v", result)
	}

	text, err := execute(t, "--config", dir, "widgets", "validate", filepath.Join(packages.LocalDir(dir), "mine"))
	if err != nil || !strings.Contains(text, "warning: ") {
		t.Fatalf("got %v\n%s", err, text)
	}
}

func TestWidgetsAddIgnoresTargetNames(t *testing.T) {
	dir := widgetConfig(t)
	board := widgets.BoardPath(dir, "home")
	ghost := strings.Replace(inlineEntry, "target: {workspace: alice}", "target: {machine: ghost}", 1)
	writeCommandFile(t, board, "format: 1\nsurface: home\nwidgets:\n"+ghost)

	if out, err := execute(t, "--config", dir, "widgets", "add", "devmachine-app/clock"); err != nil {
		t.Fatalf("a machine missing from config.yml locked the board: %v\n%s", err, out)
	}
	if out, err := execute(t, "--config", dir, "widgets", "remove", "clock"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if got := readCommandFile(t, board); !strings.Contains(got, ghost) {
		t.Fatalf("the inline widget changed:\n%s", got)
	}
}

func TestWidgetsAddPutsAStackWidgetInOrder(t *testing.T) {
	dir := stackConfig(t)
	writeCommandFile(t, widgets.BoardPath(dir, "context-sidebar"), "format: 1\nsurface: context-sidebar\nwidgets: []\n")
	for _, args := range [][]string{
		{"claude-code/usage"},
		{"devmachine-app/todo", "--before", "usage"},
		{"claude-code/usage", "--id", "usage-2", "--after", "todo", "--set", "harness=codex"},
	} {
		if out, err := execute(t, append([]string{"--config", dir, "widgets", "add", "--board", "context-sidebar"}, args...)...); err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
	}
	want := "format: 1\nsurface: context-sidebar\nwidgets:\n" +
		"  - id: todo\n    type: devmachine-app/todo\n    size: auto\n" +
		"  - id: usage-2\n    type: claude-code/usage\n    with: {harness: codex}\n    size: medium\n" +
		"  - id: usage\n    type: claude-code/usage\n    size: medium\n"
	if got := readCommandFile(t, widgets.BoardPath(dir, "context-sidebar")); got != want {
		t.Fatalf("got\n%s", got)
	}
}

func TestWidgetsAddToAMissingStackStartsFromTheDefaultBoard(t *testing.T) {
	dir := stackConfig(t)
	out, err := execute(t, "--config", dir, "widgets", "add", "claude-code/usage", "--board", "context-sidebar", "--after", "shortcuts")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if out != "added usage (claude-code/usage) to the context-sidebar board after shortcuts\n" {
		t.Fatalf("got %q", out)
	}
	b, _, _, err := widgets.ReadBoard(widgets.BoardPath(dir, "context-sidebar"))
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, w := range b.Widgets {
		ids = append(ids, w.ID)
	}
	want := "shortcuts,usage,publish-port,monitors,shells,sub-agents,todo,pull-requests,links"
	if strings.Join(ids, ",") != want {
		t.Fatalf("got %v", ids)
	}
}

func TestWidgetsAddRefusesASecondSingleWidget(t *testing.T) {
	dir := stackConfig(t)
	_, err := execute(t, "--config", dir, "widgets", "add", "devmachine-app/workspaces", "--board", "sidebar")
	if err == nil || !strings.Contains(err.Error(), "devmachine-app/workspaces goes on a board once, and the sidebar board has it as workspaces") {
		t.Fatalf("got %v", err)
	}
	if _, statErr := os.Stat(widgets.BoardPath(dir, "sidebar")); statErr == nil {
		t.Fatal("a refused add wrote the sidebar board")
	}
	writeCommandFile(t, widgets.BoardPath(dir, "sidebar"), "format: 1\nsurface: sidebar\nwidgets:\n  - id: workspaces\n    type: devmachine-app/workspaces\n    size: auto\n")
	if out, err := execute(t, "--config", dir, "widgets", "remove", "workspaces", "--board", "sidebar"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if out, err := execute(t, "--config", dir, "widgets", "add", "devmachine-app/workspaces", "--board", "sidebar"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if got := readCommandFile(t, widgets.BoardPath(dir, "sidebar")); got != "format: 1\nsurface: sidebar\nwidgets:\n  - id: workspaces\n    type: devmachine-app/workspaces\n    size: auto\n" {
		t.Fatalf("got\n%s", got)
	}
}

func TestWidgetsAddRefusesTheWrongPlacementFlag(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"--at on a stack", []string{"claude-code/usage", "--board", "sidebar", "--at", "0,0"}, "--at places a widget on Home's canvas; in the sidebar area use --after or --before"},
		{"--after on Home", []string{"claude-code/usage", "--after", "clock"}, "--after and --before order a sidebar's list; the home area is a canvas, so use --at"},
		{"an unknown neighbour", []string{"claude-code/usage", "--board", "sidebar", "--after", "ghost"}, `no widget with id "ghost" on the sidebar board`},
		{"auto on a view that does not grow", []string{"claude-code/usage", "--board", "sidebar", "--size", "auto"}, "claude-code/usage does not grow with its content, so it takes no --size auto: it takes small, medium, wide"},
		{"a widget that does not fit a stack", []string{"devmachine-app/clock", "--board", "sidebar"}, "devmachine-app/clock does not fit the sidebar area"},
		{"a widget that needs a session", []string{"devmachine-app/todo", "--board", "sidebar"}, "devmachine-app/todo does not fit the sidebar area"},
		{"both neighbours", []string{"claude-code/usage", "--board", "sidebar", "--after", "a", "--before", "b"}, "[after before]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := stackConfig(t)
			_, err := execute(t, append([]string{"--config", dir, "widgets", "add"}, tc.args...)...)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q, got %v", tc.want, err)
			}
			for _, surface := range []string{"home", "sidebar"} {
				if _, statErr := os.Stat(widgets.BoardPath(dir, surface)); statErr == nil {
					t.Fatalf("a refused add wrote the %s board", surface)
				}
			}
		})
	}
}

const inlineStackEntry = `  - id: notes
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

func TestWidgetsAddKeepsAnInlineStackWidget(t *testing.T) {
	dir := stackConfig(t)
	board := widgets.BoardPath(dir, "context-sidebar")
	writeCommandFile(t, board, "format: 1\nsurface: context-sidebar\nwidgets:\n"+inlineStackEntry)
	if out, err := execute(t, "--config", dir, "widgets", "add", "claude-code/usage", "--board", "context-sidebar", "--before", "notes"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	want := "format: 1\nsurface: context-sidebar\nwidgets:\n  - id: usage\n    type: claude-code/usage\n    size: medium\n" + inlineStackEntry
	if got := readCommandFile(t, board); got != want {
		t.Fatalf("got\n%s", got)
	}
}

func TestWidgetsMoveReordersAStack(t *testing.T) {
	dir := stackConfig(t)
	board := widgets.BoardPath(dir, "sidebar")
	writeCommandFile(t, board, "format: 1\nsurface: sidebar\nwidgets:\n"+
		"  - id: workspaces\n    type: devmachine-app/workspaces\n    size: auto\n"+
		"  - id: usage\n    type: claude-code/usage\n    size: medium\n")
	out, err := execute(t, "--config", dir, "--format", "json", "widgets", "move", "usage", "--before", "workspaces", "--board", "sidebar")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	var change struct {
		Moved string   `json:"moved"`
		Order []string `json:"order"`
	}
	if err := json.Unmarshal([]byte(out), &change); err != nil {
		t.Fatal(err)
	}
	if change.Moved != "usage" || strings.Join(change.Order, ",") != "usage,workspaces" {
		t.Fatalf("got %+v", change)
	}
	text, err := execute(t, "--config", dir, "widgets", "move", "usage", "--after", "workspaces", "--board", "sidebar")
	if err != nil || text != "moved usage after workspaces on the sidebar board\n" {
		t.Fatalf("%v %q", err, text)
	}
	if got := readCommandFile(t, board); !strings.HasSuffix(got, "  - id: usage\n    type: claude-code/usage\n    size: medium\n") {
		t.Fatalf("got\n%s", got)
	}
}

func TestWidgetsMoveOnAMissingStackUsesTheDefaultBoard(t *testing.T) {
	dir := stackConfig(t)
	if out, err := execute(t, "--config", dir, "widgets", "move", "links", "--before", "shortcuts", "--board", "context-sidebar"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	b, _, _, err := widgets.ReadBoard(widgets.BoardPath(dir, "context-sidebar"))
	if err != nil || len(b.Widgets) != 8 || b.Widgets[0].ID != "links" || b.Widgets[1].ID != "shortcuts" {
		t.Fatalf("%v %+v", err, b.Widgets)
	}
}

func TestWidgetsMoveKeepsAnInlineStackWidget(t *testing.T) {
	dir := stackConfig(t)
	board := widgets.BoardPath(dir, "context-sidebar")
	writeCommandFile(t, board, "format: 1\nsurface: context-sidebar\nwidgets:\n"+inlineStackEntry+"  - id: usage\n    type: claude-code/usage\n    size: medium\n")
	if out, err := execute(t, "--config", dir, "widgets", "move", "notes", "--after", "usage", "--board", "context-sidebar"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	want := "format: 1\nsurface: context-sidebar\nwidgets:\n  - id: usage\n    type: claude-code/usage\n    size: medium\n" + inlineStackEntry
	if got := readCommandFile(t, board); got != want {
		t.Fatalf("got\n%s", got)
	}
}

const handWrittenStackSourceAndView = `    source:
      # poll every minute
      run: 'echo'
      kind: command
      args:
        - 'hi'
      every: 60s
    view: {kind: 'text', wrap: false} # flow
`

func TestAddMoveAndRemoveKeepAHandWrittenInlineStackWidget(t *testing.T) {
	dir := stackConfig(t)
	board := widgets.BoardPath(dir, "sidebar")
	before := "format: 1\nsurface: sidebar\nwidgets:\n  - id: hello\n    title: Hello\n" + handWrittenStackSourceAndView + "    size: medium\n"
	writeCommandFile(t, board, before)
	steps := [][]string{
		{"add", "claude-code/usage", "--board", "sidebar", "--before", "hello"},
		{"move", "usage", "--after", "hello", "--board", "sidebar"},
		{"remove", "usage", "--board", "sidebar"},
	}
	for _, step := range steps {
		if out, err := execute(t, append([]string{"--config", dir, "widgets"}, step...)...); err != nil {
			t.Fatalf("%v: %v\n%s", step, err, out)
		}
		if got := readCommandFile(t, board); !strings.Contains(got, handWrittenStackSourceAndView) {
			t.Fatalf("after %v the hand-written source and view changed:\n%s", step, got)
		}
	}
	if got := readCommandFile(t, board); got != before {
		t.Fatalf("got\n%s\nwant\n%s", got, before)
	}
}

func TestWidgetsMoveRefuses(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"Home", []string{"clock", "--after", "usage", "--board", "home"}, "the home area is a canvas: a widget there has a place, not a turn in a list"},
		{"no board flag", []string{"usage", "--after", "todo"}, `required flag(s) "board" not set`},
		{"no neighbour", []string{"usage", "--board", "sidebar"}, "[after before]"},
		{"an unknown id", []string{"ghost", "--after", "workspaces", "--board", "sidebar"}, `no widget with id "ghost" on the sidebar board`},
		{"next to itself", []string{"workspaces", "--after", "workspaces", "--board", "sidebar"}, "workspaces cannot move next to itself"},
		{"an unknown area", []string{"usage", "--after", "todo", "--board", "desk"}, "there is no desk area"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := stackConfig(t)
			_, err := execute(t, append([]string{"--config", dir, "widgets", "move"}, tc.args...)...)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q, got %v", tc.want, err)
			}
			if _, statErr := os.Stat(widgets.BoardPath(dir, "sidebar")); statErr == nil {
				t.Fatal("a refused move wrote a board")
			}
		})
	}
}

func TestWidgetsRemoveOnAMissingStackUsesTheDefaultBoard(t *testing.T) {
	dir := stackConfig(t)
	if out, err := execute(t, "--config", dir, "widgets", "remove", "publish-port", "--board", "context-sidebar"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	b, _, _, err := widgets.ReadBoard(widgets.BoardPath(dir, "context-sidebar"))
	if err != nil || len(b.Widgets) != 7 || b.HasID("publish-port") {
		t.Fatalf("%v %+v", err, b.Widgets)
	}
}

func TestWidgetsListBoardKeepsWhatFitsThatArea(t *testing.T) {
	dir := stackConfig(t)
	names := func(board string) string {
		out, err := execute(t, "--config", dir, "--format", "json", "widgets", "list", "--board", board)
		if err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
		var catalog widgets.Catalog
		if err := json.Unmarshal([]byte(out), &catalog); err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, e := range catalog.Widgets {
			got = append(got, e.Name)
		}
		return strings.Join(got, ",")
	}
	if got := names("sidebar"); got != "claude-code/usage,devmachine-app/workspaces" {
		t.Errorf("sidebar: %s", got)
	}
	if got := names("context-sidebar"); got != "claude-code/usage,devmachine-app/todo,devmachine-app/workspaces" {
		t.Errorf("context-sidebar: %s", got)
	}
	if got := names("home"); got != "claude-code/usage,devmachine-app/clock" {
		t.Errorf("home: %s", got)
	}
	if _, err := execute(t, "--config", dir, "widgets", "list", "--board", "desk"); err == nil || !strings.Contains(err.Error(), "there is no desk area") {
		t.Fatalf("got %v", err)
	}
}

func TestWidgetsAddAndMoveRefuseAnEmptyAnchor(t *testing.T) {
	dir := stackConfig(t)
	board := widgets.BoardPath(dir, "sidebar")
	writeCommandFile(t, board, "format: 1\nsurface: sidebar\nwidgets:\n"+
		"  - id: workspaces\n    type: devmachine-app/workspaces\n    size: auto\n")
	for _, flag := range []string{"--after", "--before"} {
		out, err := execute(t, "--config", dir, "widgets", "move", "workspaces", flag, "", "--board", "sidebar")
		if err == nil || !strings.Contains(err.Error(), flag+" needs an id") {
			t.Fatalf("move %s: %v\n%s", flag, err, out)
		}
		out, err = execute(t, "--config", dir, "widgets", "add", "claude-code/usage", "--board", "sidebar", flag, "")
		if err == nil || !strings.Contains(err.Error(), flag+" needs an id") {
			t.Fatalf("add %s: %v\n%s", flag, err, out)
		}
	}
}

func TestWidgetsMoveThatChangesNothingDoesNotRewriteTheFile(t *testing.T) {
	dir := stackConfig(t)
	board := widgets.BoardPath(dir, "sidebar")
	original := "format: 1\n# kept\nsurface: sidebar\nwidgets:\n" +
		"  - id: workspaces\n    type: devmachine-app/workspaces\n    size: auto\n" +
		"  - id: usage\n    type: claude-code/usage\n    size: medium\n"
	writeCommandFile(t, board, original)
	out, err := execute(t, "--config", dir, "widgets", "move", "usage", "--after", "workspaces", "--board", "sidebar")
	if err != nil || out != "moved usage after workspaces on the sidebar board\n" {
		t.Fatalf("%v %q", err, out)
	}
	if got := readCommandFile(t, board); got != original {
		t.Fatalf("file was rewritten:\n%s", got)
	}
}

func TestWidgetsValidateReadsTheOwnPackagesProviders(t *testing.T) {
	dir := widgetConfig(t)
	pkg := filepath.Join(packages.LocalDir(dir), "mine")
	writeCommandFile(t, filepath.Join(pkg, "package.yml"), "format: 1\nname: mine\nscope: machine\nsummary: Mine.\n"+
		"requires: {cli: \">= 0.9.0\"}\nwidgets: widgets\nentrypoint: bin/mine\ncommands: [disk]\n"+
		"providers:\n  disk: {returns: {used: number}, min_every: 10s}\n")
	writeCommandFile(t, filepath.Join(pkg, "tasks", "main.yml"), "---\n[]\n")
	writeCommandFile(t, filepath.Join(pkg, "widgets", "disk", "widget.yml"), `format: 1
name: disk
summary: Disk.
requires: {engine: ">= 1.3"}
fits: [canvas]
source: {kind: provider, name: mine/disk, target: {machine: main}, every: 60s}
view: {kind: number, value: "{{json.used}}"}
sizes: [small]
default_size: small
`)
	for _, path := range []string{pkg, filepath.Join(pkg, "widgets", "disk")} {
		out, err := execute(t, "--config", dir, "widgets", "validate", path)
		if err != nil || !strings.Contains(out, "mine/disk fits home") {
			t.Fatalf("%s: %v\n%s", path, err, out)
		}
	}
}

const statsWidgetYAML = `format: 1
name: disk
summary: Disk.
requires: {engine: ">= 1.3"}
fits: [canvas]
source: {kind: provider, name: PKG/disk, target: {machine: main}, every: 60s}
view: {kind: number, value: "{{json.used}}"}
sizes: [small]
default_size: small
`

func writeProviderPackage(t *testing.T, dir, name string) string {
	t.Helper()
	pkg := filepath.Join(packages.LocalDir(dir), name)
	writeCommandFile(t, filepath.Join(pkg, "package.yml"), "format: 1\nname: "+name+"\nscope: machine\nsummary: Disk.\n"+
		"requires: {cli: \">= 0.9.0\"}\nwidgets: widgets\nentrypoint: bin/"+name+"\ncommands: [disk]\n"+
		"providers:\n  disk: {returns: {used: number}, min_every: 10s}\n")
	writeCommandFile(t, filepath.Join(pkg, "tasks", "main.yml"), "---\n[]\n")
	writeCommandFile(t, filepath.Join(pkg, "widgets", "disk", "widget.yml"), strings.ReplaceAll(statsWidgetYAML, "PKG", name))
	return pkg
}

func TestWidgetsListSaysWhereEachWidgetComesFrom(t *testing.T) {
	dir := widgetConfig(t)
	writeProviderPackage(t, dir, "mine")
	tools := writeProviderPackage(t, dir, "alice-tools")
	if err := packages.WriteOrigin(tools, packages.Origin{URL: "https://example.com/alice/tools.git", Ref: "v1", Commit: "0123abc"}); err != nil {
		t.Fatal(err)
	}
	catalog := listCatalog(t, dir)
	clock, _ := catalog.Find("devmachine-app/clock")
	mine, _ := catalog.Find("mine/disk")
	alice, _ := catalog.Find("alice-tools/disk")
	if clock.Trust != widgets.TrustOfficial || mine.Trust != widgets.TrustLocal || alice.Trust != widgets.TrustThirdParty {
		t.Fatalf("got %s %s %s", clock.Trust, mine.Trust, alice.Trust)
	}
	if alice.PackageSource == nil || alice.PackageSource.Commit != "0123abc" || mine.PackageSource != nil {
		t.Fatalf("got %+v %+v", alice.PackageSource, mine.PackageSource)
	}
	if alice.Provider == nil || alice.Provider.MinEvery != "10s" || clock.Provider != nil {
		t.Fatalf("got %+v %+v", alice.Provider, clock.Provider)
	}
	if p := catalog.Providers["alice-tools/disk"]; p.Trust != widgets.TrustThirdParty || p.Returns["used"] != "number" {
		t.Fatalf("got %+v", catalog.Providers)
	}
	out, err := execute(t, "--config", dir, "widgets", "list")
	if err != nil || !strings.Contains(out, "alice-tools/disk  (third-party from https://example.com/alice/tools.git)") {
		t.Fatalf("%v\n%s", err, out)
	}
}

func TestABrokenOriginFileStillMeansThirdParty(t *testing.T) {
	dir := widgetConfig(t)
	tools := writeProviderPackage(t, dir, "alice-tools")
	writeCommandFile(t, filepath.Join(tools, packages.OriginFile), "url: [\n")
	catalog := listCatalog(t, dir)
	alice, ok := catalog.Find("alice-tools/disk")
	if !ok || alice.Trust != widgets.TrustThirdParty || alice.PackageSource != nil {
		t.Fatalf("got %+v", alice)
	}
	if len(catalog.Problems) != 1 || !strings.Contains(catalog.Problems[0].Message, "its widgets are treated as third-party") {
		t.Fatalf("got %+v", catalog.Problems)
	}
	out, err := execute(t, "--config", dir, "widgets", "list")
	if err != nil || !strings.Contains(out, "alice-tools/disk  (third-party)") || strings.Contains(out, "(local") {
		t.Fatalf("%v\n%s", err, out)
	}
}

func TestAThirdPartyPackageWithoutAURLSaysThirdParty(t *testing.T) {
	dir := widgetConfig(t)
	tools := writeProviderPackage(t, dir, "alice-tools")
	writeCommandFile(t, filepath.Join(tools, packages.OriginFile), "ref: v1\ncommit: 0123abc\n")
	out, err := execute(t, "--config", dir, "widgets", "list")
	if err != nil || !strings.Contains(out, "alice-tools/disk  (third-party)") || strings.Contains(out, "third-party from") {
		t.Fatalf("%v\n%s", err, out)
	}
}

const ghostStatsBoard = `format: 1
surface: home
widgets:
  - id: stats
    title: Disk on main
    source: {kind: provider, name: ghost/stats, target: {machine: main}, every: 30s}
    view: {kind: gauge, value: "{{json.used}}"}
    sizes: [small]
    frame: {x: 24, y: 24, w: 160, h: 160}
    size: small
    minimized: false
    z: 1
`

func TestWidgetsValidateChecksABoardWidgetsPackageProvider(t *testing.T) {
	dir := widgetConfig(t)
	board := widgets.BoardPath(dir, "home")
	writeCommandFile(t, board, ghostStatsBoard)
	out, err := execute(t, "--config", dir, "widgets", "validate", board)
	if err == nil || !strings.Contains(out, "stats: source.name ghost/stats: no package ghost in the release or your own packages") {
		t.Fatalf("%v\n%s", err, out)
	}
}

func TestWidgetsAddKeepsWorkingWhenABoardWidgetsPackageIsGone(t *testing.T) {
	dir := widgetConfig(t)
	writeCommandFile(t, widgets.BoardPath(dir, "home"), ghostStatsBoard)
	if out, err := execute(t, "--config", dir, "widgets", "add", "devmachine-app/clock"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if out, err := execute(t, "--config", dir, "widgets", "remove", "stats"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}

func TestWidgetsAddRefusesABoardWithAMalformedPackageProviderName(t *testing.T) {
	dir := widgetConfig(t)
	writeCommandFile(t, widgets.BoardPath(dir, "home"), strings.Replace(ghostStatsBoard, "name: ghost/stats", "name: ghost/-rf", 1))
	out, err := execute(t, "--config", dir, "widgets", "add", "devmachine-app/clock")
	if err == nil || !strings.Contains(err.Error()+out, "source.name ghost/-rf: is written <package>/<command>") {
		t.Fatalf("%v\n%s", err, out)
	}
}
