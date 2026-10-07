package widgets

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const usageWidget = `format: 1
name: usage
summary: Coding-harness usage windows.
requires: {engine: ">= 1.0"}
fits: [canvas, stack, slot]
context: {}
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

func writeWidget(t *testing.T, parent, name, body string) string {
	t.Helper()
	dir := filepath.Join(parent, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestAValidWidgetLoadsWithNoProblem(t *testing.T) {
	w, problems := Load(writeWidget(t, t.TempDir(), "usage", usageWidget))
	if len(problems) != 0 {
		t.Fatalf("got %v", problems)
	}
	if w.Name != "usage" || w.Source.With["harness"] != "{{inputs.harness}}" || w.Inputs["harness"].Default != "claude" {
		t.Fatalf("got %+v", w)
	}
}

func TestEachRuleReportsItsOwnProblem(t *testing.T) {
	cases := []struct {
		name, folder, from, to, want string
	}{
		{"name malformed", "Usage", "name: usage", "name: Usage", `name "Usage": use lower case`},
		{"name is not the folder", "other", "", "", `name is "usage" but the folder is "other"`},
		{"summary missing", "usage", "summary: Coding-harness usage windows.", "summary: \"\"", "needs a one-line summary"},
		{"requires.engine missing", "usage", `requires: {engine: ">= 1.0"}`, "requires: {}", "needs requires.engine"},
		{"requires.engine unreadable", "usage", `">= 1.0"`, `"latest"`, `requires.engine "latest": write it as ">= 1.0"`},
		{"fits unknown", "usage", "fits: [canvas, stack, slot]", "fits: [grid]", `fits "grid": the layouts are canvas, stack, slot`},
		{"fits empty", "usage", "fits: [canvas, stack, slot]", "fits: []", "fits names the layouts"},
		{"size unknown", "usage", "sizes: [small, medium, wide]", "sizes: [small, medium, huge]", `size "huge": the presets are`},
		{"default_size not in sizes", "usage", "default_size: medium", "default_size: large", `default_size "large" is not one of sizes`},
		{"places unknown", "usage", "places: [home]", "places: [menubar]", `places "menubar": the surfaces are`},
		{"context key unknown", "usage", "context: {}", "context: {colour: required}", `context key "colour" is not given by any surface`},
		{"context value unknown", "usage", "context: {}", "context: {repo: maybe}", `context key "repo" is "maybe": write required or optional`},
		{"input type unknown", "usage", "type: string", "type: date", `input "harness" has type "date"`},
		{"input default of the wrong type", "usage", "default: claude", "default: 3", `input "harness" is a string, and its default 3 is not`},
		{"template names an unknown input", "usage", "{{inputs.harness}}", "{{inputs.agent}}", "needs inputs.agent"},
		{"template names an undeclared context key", "usage", "{{inputs.harness}}", "{{context.repo}}", "needs context.repo to be declared"},
		{"template names something else", "usage", "{{inputs.harness}}", "{{env.HOME}}", "names neither inputs.<name> nor context.<name>"},
		{"source.kind unknown", "usage", "kind: provider", "kind: magic", `source.kind "magic": engine 1.2 knows provider, command, url, prompt, session`},
		{"source.name unknown", "usage", "name: app/harness-usage", "name: app/weather", `source.name "app/weather" is not a provider`},
		{"source.with has an extra argument", "usage", `with: {harness: "{{inputs.harness}}"}`, `with: {harness: "{{inputs.harness}}", colour: red}`, "source.with.colour is not an argument of app/harness-usage"},
		{"source.with misses a required argument", "usage", `with: {harness: "{{inputs.harness}}"}`, "with: {}", "source.with.harness is required by app/harness-usage"},
		{"every missing", "usage", "  every: 60s\n", "", "needs source.every, at least 5s"},
		{"every unreadable", "usage", "every: 60s", "every: often", `source.every "often" is not a duration`},
		{"every below the minimum", "usage", "every: 60s", "every: 1s", "source.every 1s is below the app/harness-usage minimum of 5s"},
		{"view does not draw the provider", "usage", "view: {kind: app.harness-usage}", "view: {kind: gauge}", `view.kind "gauge" draws number, json, not app/harness-usage`},
		{"view unknown", "usage", "view: {kind: app.harness-usage}", "view: {kind: app.weather}", `view.kind "app.weather" is not a view`},
		{"view does not accept the provider", "usage", "view: {kind: app.harness-usage}", "view: {kind: app.clock}", `view.kind "app.clock" draws app/clock, not app/harness-usage`},
		{"unknown field", "usage", "places: [home]", "places: [home]\ncolour: red", `unknown field "colour"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := usageWidget
			if tc.from != "" {
				if !strings.Contains(body, tc.from) {
					t.Fatalf("fixture has no %q", tc.from)
				}
				body = strings.Replace(body, tc.from, tc.to, 1)
			}
			_, problems := Load(writeWidget(t, t.TempDir(), tc.folder, body))
			if len(problems) != 1 || !strings.Contains(problems[0].Message, tc.want) {
				t.Fatalf("want one problem containing %q, got %v", tc.want, problems)
			}
		})
	}
}

func TestEveryProblemIsReportedAtOnce(t *testing.T) {
	body := strings.NewReplacer(
		"summary: Coding-harness usage windows.", "summary: \"\"",
		"default_size: medium", "default_size: large",
		"every: 60s", "every: 1s",
	).Replace(usageWidget)
	_, problems := Load(writeWidget(t, t.TempDir(), "usage", body))
	if len(problems) != 3 {
		t.Fatalf("want 3 problems, got %v", problems)
	}
}

func TestAProblemPointsAtItsLine(t *testing.T) {
	body := strings.Replace(usageWidget, "every: 60s", "every: 1s", 1)
	_, problems := Load(writeWidget(t, t.TempDir(), "usage", body))
	if len(problems) != 1 || problems[0].Line != 13 {
		t.Fatalf("want line 13, got %v", problems)
	}
}

func TestAFormatMismatchStopsEveryOtherCheck(t *testing.T) {
	body := strings.Replace(usageWidget, "format: 1", "format: 2", 1)
	body = strings.Replace(body, "every: 60s", "every: 1s", 1)
	_, problems := Load(writeWidget(t, t.TempDir(), "usage", body))
	if len(problems) != 1 || !strings.Contains(problems[0].Message, "this CLI reads widget format 1") {
		t.Fatalf("got %v", problems)
	}
}

func TestANewerEngineStopsEveryOtherCheck(t *testing.T) {
	body := strings.Replace(usageWidget, `">= 1.0"`, `">= 1.3"`, 1)
	body = strings.Replace(body, "view: {kind: app.harness-usage}", "view: {kind: gauge}", 1)
	_, problems := Load(writeWidget(t, t.TempDir(), "usage", body))
	if len(problems) != 1 || !strings.Contains(problems[0].Message, "requires engine >= 1.3, and this CLI implements engine 1.2") {
		t.Fatalf("got %v", problems)
	}
}

func TestAMissingFileIsOneProblem(t *testing.T) {
	_, problems := Load(t.TempDir())
	if len(problems) != 1 || !strings.Contains(problems[0].Message, "reading widget.yml") {
		t.Fatalf("got %v", problems)
	}
}

func TestBrokenYAMLIsOneProblem(t *testing.T) {
	_, problems := Load(writeWidget(t, t.TempDir(), "usage", "format: [1\n"))
	if len(problems) != 1 || !strings.Contains(problems[0].Message, "parsing widget.yml") {
		t.Fatalf("got %v", problems)
	}
}

func TestSurfacesFollowFitsAndRequiredContext(t *testing.T) {
	cases := []struct {
		fits    []string
		context map[string]string
		want    []string
	}{
		{[]string{"canvas", "stack", "slot"}, nil, []string{"context-sidebar", "home", "sidebar"}},
		{[]string{"canvas"}, nil, []string{"home"}},
		{[]string{"canvas", "stack"}, map[string]string{"repo": ContextRequired}, []string{"context-sidebar"}},
		{[]string{"canvas", "stack"}, map[string]string{"repo": ContextOptional}, []string{"context-sidebar", "home", "sidebar"}},
	}
	for _, tc := range cases {
		got := Surfaces(Widget{Fits: tc.fits, Context: tc.context})
		if !slices.Equal(got, tc.want) {
			t.Errorf("fits %v context %v: got %v, want %v", tc.fits, tc.context, got, tc.want)
		}
	}
}

func TestEngineConstraints(t *testing.T) {
	cases := []struct {
		constraint string
		want       bool
	}{
		{">= 1.0", true}, {"> 1.0", false}, {"= 1.0", true}, {">= 1.1", false}, {">= 0.9", true}, {">=1.0", true},
	}
	for _, tc := range cases {
		c, err := parseEngineConstraint(tc.constraint)
		if err != nil {
			t.Fatalf("%s: %v", tc.constraint, err)
		}
		if got := c.allows("1.0"); got != tc.want {
			t.Errorf("%s allows 1.0: got %v", tc.constraint, got)
		}
	}
	if _, err := parseEngineConstraint(">= 1.0.0"); err == nil {
		t.Error("a three-part engine version should be refused")
	}
}
