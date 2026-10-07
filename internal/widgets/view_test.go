package widgets

import (
	"encoding/json"
	"strings"
	"testing"
)

const gaugeView = `view:
  kind: gauge
  value: "{{json.used}}"
  min: 0
  max: 100
  unit: "%"
  warn: 80
  crit: 95
`

const gaugeWidget = `format: 1
name: load
summary: Disk use as a gauge.
requires: {engine: ">= 1.1"}
fits: [canvas]
source:
  kind: command
  run: disk-usage
  every: 60s
  parse: json
` + gaugeView + `sizes: [small, medium]
default_size: small
`

const statusWidget = `format: 1
name: health
summary: Whether the site answers.
requires: {engine: ">= 1.1"}
fits: [canvas]
source:
  kind: url
  url: https://example.com/health
  every: 30s
view: {kind: status, ok: "< 300", warn: "< 500"}
sizes: [small]
default_size: small
`

const branchesView = `view:
  kind: list
  item: {title: "{{item}}", link: "https://example.com/branches/{{item}}"}
`

const branchesWidget = `format: 1
name: branches
summary: The branches of a repository.
requires: {engine: ">= 1.1"}
fits: [canvas]
source: {kind: command, run: git-branches, every: 60s, parse: lines}
` + branchesView + `sizes: [medium]
default_size: medium
`

func TestEveryViewLoadsWhenWrittenRight(t *testing.T) {
	cases := map[string]struct{ folder, body string }{
		"gauge":     {"load", gaugeWidget},
		"status":    {"health", statusWidget},
		"list":      {"branches", branchesWidget},
		"number":    {"load", edited(t, gaugeWidget, gaugeView, "view: {kind: number, value: \"{{json.disk.used}}\", format: bytes}\n")},
		"sparkline": {"load", edited(t, gaugeWidget, gaugeView, "view: {kind: sparkline, value: \"{{json.used}}\", max: 100}\n")},
		"text":      {"branches", edited(t, branchesWidget, branchesView, "view: {kind: text, wrap: false, tail: 50}\n")},
		"web":       {"health", edited(t, statusWidget, `view: {kind: status, ok: "< 300", warn: "< 500"}`, "view: {kind: web, zoom: 1.5}")},
		"json list": {"branches", edited(t, branchesWidget,
			"parse: lines}", "parse: json}",
			`title: "{{item}}"`, `title: "{{item.name}}", status: "{{item.state}}"`)},
		"status text": {"health", edited(t, statusWidget,
			"  every: 30s\n", "  every: 30s\n  parse: text\n",
			`ok: "< 300", warn: "< 500"`, `ok: '== "up"'`)},
	}
	for name, tc := range cases {
		if _, problems := Load(writeWidget(t, t.TempDir(), tc.folder, tc.body)); len(problems) != 0 {
			t.Errorf("%s: %v", name, problems)
		}
	}
}

func TestEachViewRuleReportsItsOwnProblem(t *testing.T) {
	cases := []struct {
		name, folder, fixture string
		edits                 []string
		want                  string
	}{
		{"value missing for JSON", "load", gaugeWidget, []string{"  value: \"{{json.used}}\"\n", ""}, "view.value picks what to show out of the JSON"},
		{"value names something else", "load", gaugeWidget, []string{`"{{json.used}}"`, `"{{inputs.used}}"`}, "template {{inputs.used}} in view.value names json.<path>"},
		{"value without JSON", "load", gaugeWidget, []string{"parse: json", "parse: number"}, "view.value only applies when source.parse is json"},
		{"min not below max", "load", gaugeWidget, []string{"  max: 100\n", "  max: 0\n"}, "view.max 0 must be above view.min 0"},
		{"warn not a number", "load", gaugeWidget, []string{"warn: 80", "warn: high"}, "view.warn is high, and it has to be a number"},
		{"unknown view key", "load", gaugeWidget, []string{"  crit: 95\n", "  crit: 95\n  colour: red\n"}, "view.colour is not a field of the gauge view"},
		{"format unknown", "load", gaugeWidget, []string{gaugeView, "view: {kind: number, value: \"{{json.used}}\", format: hex}\n"}, `view.format "hex": the number view takes plain, percent, bytes, duration`},
		{"status rule unreadable", "health", statusWidget, []string{`ok: "< 300"`, `ok: "fine"`}, "view.ok fine is not a rule"},
		{"status compares text by size", "health", statusWidget, []string{`ok: "< 300"`, `ok: '< "up"'`}, `view.ok < "up" is not a rule`},
		{"web with a parse", "health", statusWidget, []string{"  every: 30s\n", "  every: 30s\n  parse: text\n", `view: {kind: status, ok: "< 300", warn: "< 500"}`, "view: {kind: web}"}, "the web view loads the page itself: remove source.parse"},
		{"zoom too large", "health", statusWidget, []string{`view: {kind: status, ok: "< 300", warn: "< 500"}`, "view: {kind: web, zoom: 3}"}, "view.zoom 3 is above 2"},
		{"item field on lines", "branches", branchesWidget, []string{`title: "{{item}}"`, `title: "{{item.name}}"`}, "only JSON items have fields"},
		{"item key unknown", "branches", branchesWidget, []string{`{{item}}"}`, `{{item}}", colour: red}`}, "view.item.colour is not a field of a list item"},
		{"item template needs an input", "branches", branchesWidget, []string{`title: "{{item}}"`, `title: "{{inputs.repo}}"`}, "template {{inputs.repo}} in view.item.title needs inputs.repo"},
		{"tail too long", "branches", branchesWidget, []string{branchesView, "view: {kind: text, tail: 5000}\n"}, "view.tail 5000 is outside 1 to 2000 lines"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := edited(t, tc.fixture, tc.edits...)
			_, problems := Load(writeWidget(t, t.TempDir(), tc.folder, body))
			if len(problems) != 1 || !strings.Contains(problems[0].Message, tc.want) {
				t.Fatalf("want one problem containing %q, got %v", tc.want, problems)
			}
			if problems[0].Line == 0 {
				t.Fatalf("the problem names no line: %v", problems[0])
			}
		})
	}
}

func TestAViewKeepsItsFieldsInJSON(t *testing.T) {
	w, problems := Load(writeWidget(t, t.TempDir(), "load", gaugeWidget))
	if len(problems) != 0 {
		t.Fatal(problems)
	}
	body, err := json.Marshal(w.View)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"kind":"gauge","unit":"%","value":"{{json.used}}","min":0,"max":100,"warn":80,"crit":95}`
	if string(body) != want {
		t.Fatalf("got  %s\nwant %s", body, want)
	}
}
