package widgets

import (
	"path/filepath"
	"strings"
	"testing"
)

const machineStatsWidget = `format: 1
name: machine-stats
summary: How full one machine's disk is.
requires: {engine: ">= 1.3"}
fits: [canvas, stack]
inputs:
  machine: {type: string, summary: Which machine.}
source:
  kind: provider
  name: devmachine-app/stats
  with: {path: /}
  target: {machine: "{{inputs.machine}}"}
  every: 60s
view: {kind: gauge, value: "{{json.disk.used_percent}}", unit: "%", warn: 80, crit: 90}
sizes: [small, medium]
default_size: small
`

var statsOwner = Owner{Name: "devmachine-app", Providers: map[string]PackageProvider{
	"stats": {Returns: map[string]string{"disk": "object", "load": "object"}, MinEvery: "10s"},
}}

func loadOwned(t *testing.T, body string) []Problem {
	t.Helper()
	_, problems := LoadIn(statsOwner, writeWidget(t, t.TempDir(), "machine-stats", body))
	return problems
}

func TestAPackageProviderWidgetLoadsWhenWrittenRight(t *testing.T) {
	cases := map[string]string{
		"as written":         machineStatsWidget,
		"manual":             edited(t, machineStatsWidget, "every: 60s", "every: manual"),
		"workspace":          edited(t, machineStatsWidget, `target: {machine: "{{inputs.machine}}"}`, "target: {workspace: alice}"),
		"no with":            edited(t, machineStatsWidget, "  with: {path: /}\n", ""),
		"timeout":            edited(t, machineStatsWidget, "  every: 60s\n", "  every: 60s\n  timeout: 2m\n"),
		"a key with a dash":  edited(t, machineStatsWidget, "{path: /}", "{max-lines: 20}"),
		"a value with flags": edited(t, machineStatsWidget, "{path: /}", `{path: "--all; rm -rf ~"}`),
	}
	for name, body := range cases {
		if problems := loadOwned(t, body); len(problems) != 0 {
			t.Errorf("%s: %v", name, problems)
		}
	}
}

func TestEachPackageProviderRuleReportsItsOwnProblem(t *testing.T) {
	cases := []struct {
		name  string
		edits []string
		want  string
	}{
		{"another package's provider", []string{"name: devmachine-app/stats", "name: claude-code/usage"},
			"source.name claude-code/usage: a package widget reads only its own package's providers, written devmachine-app/<command>"},
		{"a provider its package lacks", []string{"name: devmachine-app/stats", "name: devmachine-app/context"},
			"source.name devmachine-app/context: devmachine-app has no provider context; its providers: stats"},
		{"local target", []string{`target: {machine: "{{inputs.machine}}"}`, "target: local"},
			"source.name devmachine-app/stats: a package provider runs on a machine or workspace: write source.target: {machine: <name>} or {workspace: <name>}"},
		{"no target", []string{"  target: {machine: \"{{inputs.machine}}\"}\n", ""},
			"source.name devmachine-app/stats: a package provider runs on a machine or workspace"},
		{"every below the provider's minimum", []string{"every: 60s", "every: 5s"},
			"source.every 5s is below the devmachine-app/stats minimum of 10s"},
		{"every unreadable", []string{"every: 60s", "every: often"},
			`source.every "often" is neither a duration nor manual: write it like 60s, 5m or manual`},
		{"a with key in capitals", []string{"{path: /}", "{Path: /}"},
			"source.with.Path: devmachine-app/stats gets it as --Path, so write the key in lower case letters, digits, dashes and underscores, starting with a letter"},
		{"a with key that is a flag", []string{"{path: /}", `{"-p": /}`},
			"source.with.-p: devmachine-app/stats gets it as ---p"},
		{"a with key with a shell word", []string{"{path: /}", `{"x; rm": /}`},
			"source.with.x; rm: devmachine-app/stats gets it as --x; rm"},
		{"an engine that cannot run it", []string{`">= 1.3"`, `">= 1.2"`},
			`a widget reading a package provider needs requires.engine ">= 1.3": an app on engine 1.2 cannot run it`},
		{"a view that does not draw JSON", []string{`view: {kind: gauge, value: "{{json.disk.used_percent}}", unit: "%", warn: 80, crit: 90}`, "view: {kind: text}"},
			`view.kind "text" takes text, lines, ansi, and this provider source gives json`},
		{"a timeout too long", []string{"  every: 60s\n", "  every: 60s\n  timeout: 11m\n"},
			"source.timeout 11m is above the 10m maximum"},
		{"a template it cannot fill", []string{"{path: /}", `{path: "{{inputs.dir}}"}`},
			"template {{inputs.dir}} in source.with.path needs inputs.dir"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			problems := loadOwned(t, edited(t, machineStatsWidget, tc.edits...))
			if len(problems) != 1 || !strings.Contains(problems[0].Message, tc.want) {
				t.Fatalf("want one problem containing %q, got %v", tc.want, problems)
			}
		})
	}
}

func TestAPackageProviderProblemPointsAtItsLine(t *testing.T) {
	problems := loadOwned(t, edited(t, machineStatsWidget, `target: {machine: "{{inputs.machine}}"}`, "target: local"))
	if len(problems) != 1 || problems[0].Line != 12 {
		t.Fatalf("got %v", problems)
	}
}

func TestAWidgetOutsideAPackageCannotReadAPackageProvider(t *testing.T) {
	_, problems := Load(writeWidget(t, t.TempDir(), "machine-stats", machineStatsWidget))
	if len(problems) != 1 || !strings.Contains(problems[0].Message,
		"source.name devmachine-app/stats: only a widget in a package, or one written in a board, reads a package provider") {
		t.Fatalf("got %v", problems)
	}
}

func TestSplitProviderName(t *testing.T) {
	cases := map[string]bool{
		"devmachine-app/stats": true, "app/clock": false, "stats": false, "a/b/c": false, "/stats": false, "pkg/": false,
	}
	for name, want := range cases {
		if _, _, ok := SplitProviderName(name); ok != want {
			t.Errorf("%s: got %v", name, ok)
		}
	}
}

func TestRunsCode(t *testing.T) {
	cases := map[string]struct {
		s    Source
		want bool
	}{
		"app provider":     {Source{Kind: SourceProvider, Name: "app/clock"}, false},
		"package provider": {Source{Kind: SourceProvider, Name: "devmachine-app/stats"}, true},
		"command":          {Source{Kind: SourceCommand}, true},
		"prompt":           {Source{Kind: SourcePrompt}, true},
		"session":          {Source{Kind: SourceSession}, true},
		"url":              {Source{Kind: SourceURL}, false},
	}
	for name, tc := range cases {
		if got := RunsCode(tc.s); got != tc.want {
			t.Errorf("%s: got %v", name, got)
		}
	}
}

const inlineStatsBoard = `format: 1
surface: home
widgets:
  - id: stats
    title: Disk on main
    source: {kind: provider, name: devmachine-app/stats, target: {machine: main}, every: 30s}
    view: {kind: gauge, value: "{{json.disk.used_percent}}"}
    sizes: [small]
    frame: {x: 24, y: 24, w: 160, h: 160}
    size: small
    minimized: false
    z: 1
`

func TestAnInlineWidgetTakesAPackageProviderShape(t *testing.T) {
	if problems := boardProblemsOf(t, inlineStatsBoard); len(problems) != 0 {
		t.Fatalf("a package the catalog may not know is not a board problem: %v", problems)
	}
	cases := []struct{ from, to, want string }{
		{"target: {machine: main}", "target: local", "stats: source.name devmachine-app/stats: a package provider runs on a machine or workspace"},
		{"every: 30s", "every: 2s", "stats: source.every 2s is below the devmachine-app/stats minimum of 5s"},
		{"every: 30s}", `every: 30s, with: {"-x": "1"}}`, "stats: source.with.-x: devmachine-app/stats gets it as ---x"},
	}
	for _, tc := range cases {
		problems := boardProblemsOf(t, strings.Replace(inlineStatsBoard, tc.from, tc.to, 1))
		if len(problems) != 1 || !strings.Contains(problems[0].Message, tc.want) {
			t.Errorf("%s: want %q, got %v", tc.to, tc.want, problems)
		}
	}
}

func TestBoardProviderProblems(t *testing.T) {
	path := filepath.Join(t.TempDir(), "home.yml")
	known := ProviderSet{"devmachine-app": {"stats": {MinEvery: "10s"}}, "mine": {}}
	cases := []struct {
		name, from, to string
		complete       bool
		want           string
	}{
		{"fine", "every: 30s", "every: 30s", true, ""},
		{"no package", "devmachine-app/stats", "ghost/stats", true, "stats: source.name ghost/stats: no package ghost in the release or your own packages"},
		{"no package, nothing read", "devmachine-app/stats", "ghost/stats", false, ""},
		{"no provider", "devmachine-app/stats", "mine/stats", false, "stats: source.name mine/stats: mine has no provider stats; its providers: none"},
		{"below its minimum", "every: 30s", "every: 6s", true, "stats: source.every 6s is below the devmachine-app/stats minimum of 10s"},
		{"below the floor is ValidateBoard's", "every: 30s", "every: 2s", true, ""},
	}
	for _, tc := range cases {
		b, problems := ParseBoard(path, []byte(strings.Replace(inlineStatsBoard, tc.from, tc.to, 1)))
		if len(problems) != 0 {
			t.Fatal(problems)
		}
		got := BoardProviderProblems(b, path, known, tc.complete)
		switch {
		case tc.want == "" && len(got) != 0:
			t.Errorf("%s: got %v", tc.name, got)
		case tc.want != "" && (len(got) != 1 || got[0].Message != tc.want || got[0].Line == 0):
			t.Errorf("%s: want %q, got %v", tc.name, tc.want, got)
		}
	}
}

func TestAnInlineWidgetsPackageProviderNameIsCheckedByItsShape(t *testing.T) {
	want := "is written <package>/<command>, each in lower case letters, digits, dashes and underscores, starting with a letter"
	for _, name := range []string{"Alice/stats", "devmachine-app/-rf", "devmachine-app/{{inputs.cmd}}", "{{inputs.pkg}}/stats", "alice tools/stats", "devmachine-app/Stats"} {
		board := strings.Replace(inlineStatsBoard, "name: devmachine-app/stats", `name: "`+name+`"`, 1)
		problems := boardProblemsOf(t, board)
		if len(problems) != 1 || !strings.Contains(problems[0].Message, "stats: source.name "+name+": "+want) || problems[0].Line == 0 {
			t.Errorf("%s: got %v", name, problems)
		}
		b, parsed := ParseBoard("home.yml", []byte(board))
		if len(parsed) != 0 {
			t.Fatal(parsed)
		}
		if got := BoardProviderProblems(b, "home.yml", ProviderSet{}, true); len(got) != 0 {
			t.Errorf("%s: the shape is ValidateBoard's to report, got %v", name, got)
		}
	}
}
