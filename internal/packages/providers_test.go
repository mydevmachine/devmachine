package packages

import (
	"path/filepath"
	"strings"
	"testing"
)

const statsManifest = `format: 1
name: devmachine-app
scope: machine
summary: The macOS app.
entrypoint: bin/devmachine-app
commands: [stats, context]
providers:
  stats:
    returns: {disk: object, load: object, errors: list, note: "string?"}
    min_every: 10s
`

func writeStatsPackage(t *testing.T, manifest string) string {
	t.Helper()
	dir := writePackage(t, "devmachine-app", manifest)
	write(t, filepath.Join(dir, "tasks", "main.yml"), "---\n[]\n")
	writeMode(t, filepath.Join(dir, "bin", "devmachine-app"), "#!/usr/bin/env python3\n", 0o755)
	return dir
}

func TestValidateAcceptsAPackageWithProviders(t *testing.T) {
	problems, err := Validate(writeStatsPackage(t, statsManifest))
	if err != nil || len(problems) != 0 {
		t.Fatalf("%v %#v", err, problems)
	}
	m, err := ParseManifest(writeStatsPackage(t, statsManifest))
	if err != nil {
		t.Fatal(err)
	}
	stats := m.Providers["stats"]
	if stats.MinEvery != "10s" || stats.Returns["note"] != "string?" || m.Lines["providers.stats.min_every"] != 10 {
		t.Fatalf("got %+v, lines %v", m.Providers, m.Lines)
	}
}

func TestEachProviderRuleReportsItsOwnProblem(t *testing.T) {
	cases := []struct {
		name, from, to, want string
		line                 int
	}{
		{"not one of commands", "commands: [stats, context]", "commands: [context]",
			"provider stats is not one of commands: add it to commands, or remove the provider", 8},
		{"name not lower case", "  stats:\n", "  Stats:\n",
			`provider "Stats": use lower case letters, digits, dashes and underscores, starting with a letter`, 8},
		{"returns type unknown", "errors: list", "errors: array",
			`provider stats returns.errors is "array": the types are string, number, bool, list, object, each with ? when optional`, 9},
		{"returns field name", `note: "string?"`, `2note: "string?"`,
			"provider stats returns.2note: a field name is letters, digits, dashes and underscores, starting with a letter", 9},
		{"returns empty", `returns: {disk: object, load: object, errors: list, note: "string?"}`, "returns: {}",
			"provider stats says what it returns: returns: {<field>: <type>}", 8},
		{"min_every missing", "    min_every: 10s\n", "",
			"provider stats needs min_every: how often a widget may run it at most, at least 5s", 8},
		{"min_every not a duration", "min_every: 10s", "min_every: often",
			`provider stats min_every "often" is not a duration: write it like 10s or 1m`, 10},
		{"min_every below the floor", "min_every: 10s", "min_every: 2s",
			"provider stats min_every 2s is below the 5s floor", 10},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !strings.Contains(statsManifest, tc.from) {
				t.Fatalf("fixture has no %q", tc.from)
			}
			dir := writeStatsPackage(t, strings.Replace(statsManifest, tc.from, tc.to, 1))
			problems, err := Validate(dir)
			if err != nil {
				t.Fatal(err)
			}
			if len(problems) != 1 || problems[0].What != tc.want || problems[0].Line != tc.line || problems[0].File != FileName {
				t.Fatalf("want one problem %q at line %d, got %#v", tc.want, tc.line, problems)
			}
		})
	}
}

func TestProvidersNeedAnEntrypoint(t *testing.T) {
	manifest := strings.Replace(strings.Replace(statsManifest, "entrypoint: bin/devmachine-app\n", "", 1),
		"commands: [stats, context]\n", "", 1)
	problems, err := Validate(writeStatsPackage(t, manifest))
	if err != nil {
		t.Fatal(err)
	}
	p := problemAbout(t, problems, "`providers` are commands of an `entrypoint`, and this package declares none")
	if p.Line != 5 || len(problems) != 1 {
		t.Fatalf("got %#v", problems)
	}
}

func TestAnyCommandTakesAnyProvider(t *testing.T) {
	manifest := strings.Replace(statsManifest, "commands: [stats, context]", `commands: ["*"]`, 1)
	if problems, err := Validate(writeStatsPackage(t, manifest)); err != nil || len(problems) != 0 {
		t.Fatalf("%v %#v", err, problems)
	}
}

func TestTheSchemaListsProviders(t *testing.T) {
	for _, f := range Schema().Fields {
		if f.Name == "providers" && !f.Required && strings.Contains(f.Summary, "min_every") {
			return
		}
	}
	t.Fatal("packages schema has no providers field")
}
