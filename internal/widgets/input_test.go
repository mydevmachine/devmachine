package widgets

import (
	"strings"
	"testing"
)

const machinesWidget = `format: 1
name: machines
summary: Each machine, online or not.
requires: {engine: ">= 1.5"}
fits: [canvas, stack]
context: {}
inputs:
  machines: {type: choice, from: machines, many: true, summary: Which machines; none shows all.}
source:
  kind: provider
  name: app/machines
  with: {machines: "{{inputs.machines}}"}
  every: 90s
view: {kind: app.machines}
sizes: [medium, large, wide, tall]
default_size: large
places: [home]
`

func TestAChoiceInputLoads(t *testing.T) {
	w, problems := Load(writeWidget(t, t.TempDir(), "machines", machinesWidget))
	if len(problems) != 0 {
		t.Fatalf("got %v", problems)
	}
	if in := w.Inputs["machines"]; in.Type != InputChoice || in.From != OptionMachines || !in.Many {
		t.Fatalf("got %+v", in)
	}
	for _, body := range []string{
		strings.Replace(machinesWidget, "many: true,", "many: true, default: [],", 1),
		strings.Replace(machinesWidget, "many: true,", "many: true, default: [main, backup],", 1),
		strings.Replace(machinesWidget, "from: machines, many: true,", "from: harnesses, default: claude,", 1),
	} {
		if _, problems := Load(writeWidget(t, t.TempDir(), "machines", body)); len(problems) != 0 {
			t.Errorf("got %v for\n%s", problems, body)
		}
	}
}

func TestEachChoiceRuleReportsItsOwnProblemAtItsLine(t *testing.T) {
	cases := []struct {
		name, from, to, want string
		line                 int
	}{
		{"from missing", "from: machines, ", "", `input "machines" is a choice, and needs from: one of harnesses, machines, workspaces`, 8},
		{"from unknown", "from: machines", "from: hosts", `input "machines" takes its options from "hosts", which is not a source: the sources are harnesses, machines, workspaces`, 8},
		{"many not a bool", "many: true", "many: maybe", `input "machines": many is maybe: write true or false`, 8},
		{"many default not a list", "many: true,", "many: true, default: main,", `input "machines" is a list of choices, and its default main is not: write [a, b], or [] for all`, 8},
		{"one default is a list", "many: true,", "many: false, default: [main],", `input "machines" is one choice, and its default [main] is not a name`, 8},
		{"choice keys on a string", "type: choice, from: machines, many: true", "type: string, from: machines", `input "machines" is a string: from and many belong to a choice`, 8},
		{"engine admits 1.4", `">= 1.5"`, `">= 1.0"`, `a widget with a choice input needs requires.engine ">= 1.5": an app on engine 1.4 cannot show its choices`, 4},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !strings.Contains(machinesWidget, tc.from) {
				t.Fatalf("fixture has no %q", tc.from)
			}
			body := strings.Replace(machinesWidget, tc.from, tc.to, 1)
			_, problems := Load(writeWidget(t, t.TempDir(), "machines", body))
			if len(problems) != 1 || problems[0].Message != tc.want || problems[0].Line != tc.line {
				t.Fatalf("want %q at line %d, got %+v", tc.want, tc.line, problems)
			}
		})
	}
}

func TestInputValueFits(t *testing.T) {
	many := Input{Type: InputChoice, From: OptionMachines, Many: true}
	one := Input{Type: InputChoice, From: OptionHarnesses}
	for _, tc := range []struct {
		input Input
		value any
		want  bool
	}{
		{many, []any{"main", "backup"}, true}, {many, []string{"main"}, true}, {many, []any{}, true},
		{many, "main", false}, {many, []any{"main", 3}, false},
		{one, "claude", true}, {one, []any{"claude"}, false}, {one, 3, false},
		{Input{Type: "number"}, 3, true}, {Input{Type: "string"}, []any{"a"}, false},
	} {
		if got := InputValueFits(tc.input, tc.value); got != tc.want {
			t.Errorf("InputValueFits(%+v, %#v) = %v", tc.input, tc.value, got)
		}
	}
	if got := shownValue([]any{"claude", "codex"}); got != "[claude, codex]" {
		t.Fatalf("got %q", got)
	}
}
