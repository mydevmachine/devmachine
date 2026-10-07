package widgets

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const diskWidget = `format: 1
name: disk
summary: Free space on a machine.
requires: {engine: ">= 1.1"}
fits: [canvas]
inputs:
  machine: {type: string, summary: Which machine.}
source:
  kind: command
  run: df
  args: [-h, /]
  target: {machine: "{{inputs.machine}}"}
  every: 60s
  parse: text
view: {kind: text}
sizes: [medium, wide]
default_size: medium
`

const healthWidget = `format: 1
name: health
summary: Whether the site answers.
requires: {engine: ">= 1.1"}
fits: [canvas]
source:
  kind: url
  url: https://example.com/health
  every: 30s
view: {kind: status}
sizes: [small]
default_size: small
`

const digestWidget = `format: 1
name: digest
summary: A daily summary from Claude.
requires: {engine: ">= 1.1"}
fits: [canvas]
source:
  kind: prompt
  harness: claude
  prompt: Summarise what changed in the repository today.
view: {kind: markdown}
sizes: [large]
default_size: large
`

const screenWidget = `format: 1
name: screen
summary: What the main session shows.
requires: {engine: ">= 1.1"}
fits: [canvas]
source:
  kind: session
  session: main
  every: 2s
view: {kind: terminal}
sizes: [large]
default_size: large
`

func edited(t *testing.T, body string, edits ...string) string {
	t.Helper()
	for i := 0; i+1 < len(edits); i += 2 {
		if !strings.Contains(body, edits[i]) {
			t.Fatalf("fixture has no %q", edits[i])
		}
		body = strings.Replace(body, edits[i], edits[i+1], 1)
	}
	return body
}

func TestEveryNewSourceLoadsWhenWrittenRight(t *testing.T) {
	cases := map[string]struct{ folder, body string }{
		"command": {"disk", diskWidget},
		"url":     {"health", healthWidget},
		"prompt":  {"digest", digestWidget},
		"session": {"screen", screenWidget},
		"shell line": {"disk", edited(t, diskWidget,
			"  run: df\n  args: [-h, /]\n", "  run: df -h / | tail -1\n  shell: true\n")},
		"manual": {"disk", edited(t, diskWidget, "every: 60s", "every: manual")},
		"stream": {"disk", edited(t, diskWidget,
			"  every: 60s\n  parse: text\n", "  mode: stream\n  keep: 500\n  parse: lines\n")},
		"assignment in a shell line": {"disk", edited(t, diskWidget,
			"  run: df\n  args: [-h, /]\n", "  run: LC_ALL=C df -h /\n  shell: true\n")},
		"value read from the environment in a shell line": {"disk", edited(t, diskWidget,
			"  run: df\n  args: [-h, /]\n", "  run: 'df -h -- \"$DM_INPUT_MACHINE\"'\n  shell: true\n")},
		"templates in args": {"disk", edited(t, diskWidget,
			"args: [-h, /]", `args: ["{{inputs.machine}}", "-- {{ inputs.machine }}"]`)},
	}
	for name, tc := range cases {
		if _, problems := Load(writeWidget(t, t.TempDir(), tc.folder, tc.body)); len(problems) != 0 {
			t.Errorf("%s: %v", name, problems)
		}
	}
}

func TestEachSourceRuleReportsItsOwnProblem(t *testing.T) {
	cases := []struct {
		name, folder, fixture string
		edits                 []string
		want                  string
	}{
		{"run and script", "disk", diskWidget, []string{"  run: df\n", "  run: df\n  script: bin/df\n"}, "a command source has run or script, not both"},
		{"neither run nor script", "disk", diskWidget, []string{"  run: df\n  args: [-h, /]\n", ""}, "a command source needs run or script"},
		{"run with spaces", "disk", diskWidget, []string{"  run: df\n  args: [-h, /]\n", "  run: df -h /\n"}, `source.run "df -h /" has spaces`},
		{"template in a shell line", "disk", diskWidget, []string{"  run: df\n  args: [-h, /]\n", "  run: \"echo {{inputs.machine}}\"\n  shell: true\n"}, `source.run is a shell line, so it cannot hold {{inputs.machine}}: read it as "$DM_INPUT_MACHINE" instead`},
		{"context template in a shell line", "disk", diskWidget, []string{"  run: df\n  args: [-h, /]\n", "  run: \"echo '{{ context.workspace }}'\"\n  shell: true\n"}, `cannot hold {{ context.workspace }}: read it as "$DM_CONTEXT_WORKSPACE" instead`},
		{"item template in a shell line", "disk", diskWidget, []string{"  run: df\n  args: [-h, /]\n", "  run: \"echo {{item.name}}\"\n  shell: true\n"}, "cannot hold {{item.name}}: read values as $DM_INPUT_<NAME> or $DM_CONTEXT_<KEY> instead"},
		{"args with a shell line", "disk", diskWidget, []string{"  run: df\n", "  run: df\n  shell: true\n"}, "a shell line takes no source.args"},
		{"template in a run without a shell", "disk", diskWidget, []string{"  run: df\n", "  run: \"{{ inputs.machine }}\"\n"}, "source.run cannot hold {{ inputs.machine }}: a value must not pick the program"},
		{"template inside a run word", "disk", diskWidget, []string{"  run: df\n", "  run: \"/opt/{{inputs.machine}}/df\"\n"}, "source.run cannot hold {{inputs.machine}}"},
		{"run with a carriage return", "disk", diskWidget, []string{"  run: df\n  args: [-h, /]\n", "  run: \"df\\r-h\"\n"}, "has spaces"},
		{"run with a unicode space", "disk", diskWidget, []string{"  run: df\n  args: [-h, /]\n", "  run: \"df\\u2003-h\"\n"}, "has spaces"},
		{"run with a no-break space", "disk", diskWidget, []string{"  run: df\n  args: [-h, /]\n", "  run: \"df\\u00a0-h\"\n"}, "has spaces"},
		{"run starting with a dash", "disk", diskWidget, []string{"  run: df\n", "  run: -df\n"}, `source.run "-df" starts with -`},
		{"run with an equals sign", "disk", diskWidget, []string{"  run: df\n", "  run: LANG=C\n"}, `source.run "LANG=C" has =`},
		{"every below the minimum", "disk", diskWidget, []string{"every: 60s", "every: 1s"}, "source.every 1s is below the command minimum of 5s"},
		{"every missing on a poll", "disk", diskWidget, []string{"  every: 60s\n", ""}, "a command source needs source.every"},
		{"every unreadable", "disk", diskWidget, []string{"every: 60s", "every: often"}, `source.every "often" is neither a duration nor manual`},
		{"timeout above the maximum", "disk", diskWidget, []string{"  every: 60s\n", "  every: 60s\n  timeout: 11m\n"}, "source.timeout 11m is above the 10m maximum"},
		{"stream with every", "disk", diskWidget, []string{"  parse: text\n", "  parse: text\n  mode: stream\n"}, "a stream runs while the widget is on screen: remove source.every"},
		{"stream parsing numbers", "disk", diskWidget, []string{"  every: 60s\n  parse: text\n", "  mode: stream\n  parse: number\n", "view: {kind: text}", "view: {kind: number}"}, "a stream reads lines as they come"},
		{"keep without a stream", "disk", diskWidget, []string{"  parse: text\n", "  parse: text\n  keep: 10\n"}, "source.keep only applies to mode: stream"},
		{"keep too many", "disk", diskWidget, []string{"  every: 60s\n", "  mode: stream\n  keep: 5000\n"}, "source.keep 5000 is outside 1 to 2000 lines"},
		{"parse unknown", "disk", diskWidget, []string{"parse: text", "parse: yaml"}, `source.parse "yaml": a command source takes text, lines, number, json, ansi`},
		{"mode unknown", "disk", diskWidget, []string{"  parse: text\n", "  parse: text\n  mode: daily\n"}, `source.mode "daily": a command source takes poll, stream`},
		{"target is a word", "disk", diskWidget, []string{`target: {machine: "{{inputs.machine}}"}`, "target: server"}, `source.target "server": write local, {machine: <name>} or {workspace: <name>}`},
		{"target names both", "disk", diskWidget, []string{`target: {machine: "{{inputs.machine}}"}`, "target: {machine: main, workspace: alice}"}, "source.target names one machine or one workspace"},
		{"target has an unknown key", "disk", diskWidget, []string{`target: {machine: "{{inputs.machine}}"}`, "target: {host: main}"}, `source.target has an unknown key "host"`},
		{"template needs an input", "disk", diskWidget, []string{"args: [-h, /]", `args: [-h, "{{inputs.path}}"]`}, "template {{inputs.path}} in source.args[1] needs inputs.path"},
		{"item outside a list", "disk", diskWidget, []string{"args: [-h, /]", `args: [-h, "{{item.path}}"]`}, "item only works inside a list view's item"},
		{"a field of another kind", "disk", diskWidget, []string{"  parse: text\n", "  parse: text\n  url: https://example.com\n"}, "source.url is not a field of a command source"},
		{"script outside the package", "disk", diskWidget, []string{"  run: df\n  args: [-h, /]\n", "  script: ../other/df\n"}, "a package widget names a file inside its package"},
		{"script absolute in a package", "disk", diskWidget, []string{"  run: df\n  args: [-h, /]\n", "  script: /usr/bin/df\n"}, "a package widget names a file inside its package"},
		{"script with a template", "disk", diskWidget, []string{"  run: df\n  args: [-h, /]\n", "  script: \"{{inputs.machine}}\"\n"}, "source.script cannot hold a template"},
		{"view does not take the parse", "disk", diskWidget, []string{"view: {kind: text}", "view: {kind: gauge}"}, `view.kind "gauge" takes number, json, and this command source gives text`},
		{"web for a command", "disk", diskWidget, []string{"view: {kind: text}", "view: {kind: web}"}, `view.kind "web" takes kind:url, and this command source gives text`},
		{"url without a scheme", "health", healthWidget, []string{"url: https://example.com/health", "url: example.com/health"}, "write a full address starting with https:// or http://"},
		{"url without every", "health", healthWidget, []string{"  every: 30s\n", ""}, "a url source needs source.every"},
		{"url parsed as lines", "health", healthWidget, []string{"  every: 30s\n", "  every: 30s\n  parse: lines\n"}, `source.parse "lines": a url source takes status, text, json`},
		{"harness unknown", "digest", digestWidget, []string{"harness: claude", "harness: gpt"}, `source.harness "gpt": a prompt source takes claude, codex`},
		{"prompt too often", "digest", digestWidget, []string{"today.\n", "today.\n  every: 1m\n"}, "source.every 1m is below the prompt minimum of 15m"},
		{"prompt missing", "digest", digestWidget, []string{"  prompt: Summarise what changed in the repository today.\n", ""}, "a prompt source needs source.prompt"},
		{"session missing", "screen", screenWidget, []string{"  session: main\n", ""}, "a session source needs source.session"},
		{"session too often", "screen", screenWidget, []string{"every: 2s", "every: 1s"}, "source.every 1s is below the session minimum of 2s"},
		{"session in a text view", "screen", screenWidget, []string{"view: {kind: terminal}", "view: {kind: text}"}, `view.kind "text" takes text, lines, ansi, and this session source gives a screen`},
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

func TestEveryTemplateInAShellLineIsReportedOnItsLine(t *testing.T) {
	body := edited(t, diskWidget, "  run: df\n  args: [-h, /]\n",
		"  run: \"echo {{inputs.machine}} {{context.workspace}}\"\n  shell: true\n")
	_, problems := Load(writeWidget(t, t.TempDir(), "disk", body))
	want := []string{"$DM_INPUT_MACHINE", "$DM_CONTEXT_WORKSPACE"}
	if len(problems) != len(want) {
		t.Fatalf("want %d problems, got %v", len(want), problems)
	}
	for i, name := range want {
		if problems[i].Line != 10 || !strings.Contains(problems[i].Message, name) {
			t.Errorf("problem %d: want line 10 naming %s, got %v", i, name, problems[i])
		}
	}
}

func TestEnvNameNamesTheVariableAShellLineReads(t *testing.T) {
	cases := map[string]string{
		"inputs.machine":     "DM_INPUT_MACHINE",
		" inputs.max_lines ": "DM_INPUT_MAX_LINES",
		"context.workspace":  "DM_CONTEXT_WORKSPACE",
		"context.repo":       "DM_CONTEXT_REPO",
		"item.name":          "",
		"inputs":             "",
		"inputs.":            "",
	}
	for ref, want := range cases {
		if got := EnvName(ref); got != want {
			t.Errorf("EnvName(%q) = %q, want %q", ref, got, want)
		}
	}
}

func TestTwoInputsCannotShareAnEnvironmentVariable(t *testing.T) {
	body := edited(t, diskWidget, "  machine: {type: string, summary: Which machine.}\n",
		"  machine: {type: string, summary: Which machine.}\n  a-b: {type: string}\n  a_b: {type: string}\n")
	_, problems := Load(writeWidget(t, t.TempDir(), "disk", body))
	if len(problems) != 1 || !strings.Contains(problems[0].Message, `input "a-b": use lower case letters, digits and underscores`) || problems[0].Line != 6 {
		t.Fatalf("want one name problem about a-b on the inputs line, got %v", problems)
	}
}

func TestEveryContextKeyHasItsOwnEnvironmentVariable(t *testing.T) {
	seen := map[string]string{}
	for _, key := range contextKeys(CurrentContract()) {
		name := EnvName("context." + key)
		if other, ok := seen[name]; ok {
			t.Errorf("context keys %s and %s both become %s", other, key, name)
		}
		seen[name] = key
	}
}

func writeScriptPackage(t *testing.T, mode os.FileMode) (string, string) {
	t.Helper()
	pkgDir := t.TempDir()
	body := edited(t, diskWidget, "  run: df\n  args: [-h, /]\n", "  script: widgets/disk/check.sh\n")
	widgetDir := writeWidget(t, filepath.Join(pkgDir, "widgets"), "disk", body)
	if mode != 0 {
		script := filepath.Join(widgetDir, "check.sh")
		if err := os.WriteFile(script, []byte("#!/bin/sh\ndf -h /\n"), mode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(script, mode); err != nil {
			t.Fatal(err)
		}
	}
	return pkgDir, widgetDir
}

func TestAPackageScriptMustBeAFileEveryAccountCanRun(t *testing.T) {
	cases := []struct {
		name string
		mode os.FileMode
		want string
	}{
		{"missing", 0, "is not in the package"},
		{"not executable", 0o644, "is not executable: run chmod +x on it"},
		{"only its owner can run it", 0o700, "only its owner can run it"},
		{"fine", 0o755, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pkgDir, widgetDir := writeScriptPackage(t, tc.mode)
			_, problems := LoadIn(pkgDir, widgetDir)
			if tc.want == "" {
				if len(problems) != 0 {
					t.Fatalf("got %v", problems)
				}
				return
			}
			if len(problems) != 1 || !strings.Contains(problems[0].Message, tc.want) {
				t.Fatalf("want %q, got %v", tc.want, problems)
			}
		})
	}
}

func TestAPackageScriptCannotLeaveThePackageThroughALink(t *testing.T) {
	pkgDir, widgetDir := writeScriptPackage(t, 0)
	outside := filepath.Join(t.TempDir(), "check.sh")
	if err := os.WriteFile(outside, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(widgetDir, "check.sh")); err != nil {
		t.Fatal(err)
	}
	_, problems := LoadIn(pkgDir, widgetDir)
	if len(problems) != 1 || !strings.Contains(problems[0].Message, "leads outside the package through a link") {
		t.Fatalf("got %v", problems)
	}
}

func TestATargetRoundTripsThroughJSON(t *testing.T) {
	for _, target := range []Target{{Local: true}, {Machine: "main"}, {Workspace: "alice"}} {
		body, err := target.MarshalJSON()
		if err != nil {
			t.Fatal(err)
		}
		var back Target
		if err := back.UnmarshalJSON(body); err != nil {
			t.Fatal(err)
		}
		if back != target {
			t.Errorf("%s came back as %+v", body, back)
		}
	}
}

func TestAnAppProviderTakesNoTargetOrTimeout(t *testing.T) {
	cases := map[string]struct{ edit, want string }{
		"target":  {"  every: 60s\n", "  every: 60s\n  target: {machine: main}\n"},
		"timeout": {"  every: 60s\n", "  every: 60s\n  timeout: 20s\n"},
	}
	for name, tc := range cases {
		body := edited(t, usageWidget, tc.edit, tc.want)
		_, problems := Load(writeWidget(t, t.TempDir(), "usage", body))
		want := "source." + name + ": app/harness-usage is the app's own data, so it takes no " + name
		if len(problems) != 1 || problems[0].Message != want {
			t.Errorf("%s: want %q, got %v", name, want, problems)
		}
	}
}

func TestAnAppProviderRefusesManualAsItDidIn12(t *testing.T) {
	body := edited(t, usageWidget, "  every: 60s\n", "  every: manual\n")
	_, problems := Load(writeWidget(t, t.TempDir(), "usage", body))
	want := `source.every "manual" is not a duration: write it like 60s or 5m`
	if len(problems) != 1 || problems[0].Message != want {
		t.Fatalf("want %q, got %v", want, problems)
	}
}

func TestAProviderSourcePrintsItsTargetOnlyWhenItHasOne(t *testing.T) {
	plain, err := json.Marshal(Source{Kind: SourceProvider, Name: "app/clock", Every: "5s"})
	if err != nil {
		t.Fatal(err)
	}
	if string(plain) != `{"kind":"provider","name":"app/clock","with":{},"every":"5s"}` {
		t.Fatalf("got %s", plain)
	}
	full, err := json.Marshal(Source{Kind: SourceProvider, Name: "devmachine-app/stats", Every: "60s",
		Target: Target{Machine: "main"}, Timeout: "20s"})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"kind":"provider","name":"devmachine-app/stats","with":{},"every":"60s","target":{"machine":"main"},"timeout":"20s"}`
	if string(full) != want {
		t.Fatalf("got %s", full)
	}
}
