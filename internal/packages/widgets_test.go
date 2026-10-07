package packages

import (
	"path/filepath"
	"strings"
	"testing"
)

const clockWidget = `format: 1
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

func writeWidgetPackage(t *testing.T, manifestTail, widget string) string {
	t.Helper()
	dir := writePackage(t, "devmachine-app", "format: 1\nname: devmachine-app\nscope: machine\nsummary: The macOS app.\n"+manifestTail)
	write(t, filepath.Join(dir, "tasks", "main.yml"), "---\n[]\n")
	if widget != "" {
		write(t, filepath.Join(dir, "widgets", "clock", "widget.yml"), widget)
	}
	return dir
}

func TestValidateAcceptsAPackageWithWidgets(t *testing.T) {
	dir := writeWidgetPackage(t, "requires: {cli: \">= 0.9.0\"}\nwidgets: widgets\n", clockWidget)
	problems, err := Validate(dir)
	if err != nil || len(problems) != 0 {
		t.Fatalf("%v %#v", err, problems)
	}
}

func TestValidateReportsAWidgetProblemAtItsFileAndLine(t *testing.T) {
	dir := writeWidgetPackage(t, "requires: {cli: \">= 0.9.0\"}\nwidgets: widgets\n",
		strings.Replace(clockWidget, "every: 5s", "every: 1s", 1))
	problems, err := Validate(dir)
	if err != nil {
		t.Fatal(err)
	}
	p := problemAbout(t, problems, "below the app/clock minimum of 5s")
	if p.File != filepath.Join("widgets", "clock", "widget.yml") || p.Line != 6 {
		t.Fatalf("got %#v", p)
	}
}

func TestValidateNeedsACLIThatKnowsWidgets(t *testing.T) {
	for _, requires := range []string{"", "requires: {cli: \">= 0.8.0\"}\n"} {
		dir := writeWidgetPackage(t, requires+"widgets: widgets\n", clockWidget)
		problems, err := Validate(dir)
		if err != nil {
			t.Fatal(err)
		}
		problemAbout(t, problems, `write requires: {cli: ">= 0.9.0"}`)
	}
}

func TestValidateRefusesAWidgetsFolderOutsideThePackage(t *testing.T) {
	dir := writeWidgetPackage(t, "requires: {cli: \">= 0.9.0\"}\nwidgets: ../widgets\n", "")
	problems, err := Validate(dir)
	if err != nil {
		t.Fatal(err)
	}
	problemAbout(t, problems, `widgets "../widgets" must stay inside the package`)
}

func TestValidateSaysWhenTheWidgetsFolderIsMissing(t *testing.T) {
	dir := writeWidgetPackage(t, "requires: {cli: \">= 0.9.0\"}\nwidgets: widgets\n", "")
	problems, err := Validate(dir)
	if err != nil {
		t.Fatal(err)
	}
	problemAbout(t, problems, "the widgets folder is not there")
}

func TestWidgetsDir(t *testing.T) {
	if _, ok := WidgetsDir(Manifest{}); ok {
		t.Fatal("a package with no widgets: has no widgets folder")
	}
	if got, ok := WidgetsDir(Manifest{Path: "/p/devmachine-app", Widgets: "widgets"}); !ok || got != filepath.Join("/p/devmachine-app", "widgets") {
		t.Fatalf("got %q %v", got, ok)
	}
}

func TestValidateAcceptsAWidgetReadingItsOwnProvider(t *testing.T) {
	dir := writeWidgetPackage(t, "requires: {cli: \">= 0.9.0\"}\nwidgets: widgets\nentrypoint: bin/devmachine-app\n"+
		"commands: [stats]\nproviders:\n  stats: {returns: {disk: object}, min_every: 10s}\n", "")
	writeMode(t, filepath.Join(dir, "bin", "devmachine-app"), "#!/usr/bin/env python3\n", 0o755)
	write(t, filepath.Join(dir, "widgets", "disk", "widget.yml"), `format: 1
name: disk
summary: How full the disk is.
requires: {engine: ">= 1.3"}
fits: [canvas]
source: {kind: provider, name: devmachine-app/stats, target: {machine: main}, every: 60s}
view: {kind: gauge, value: "{{json.disk.used_percent}}"}
sizes: [small]
default_size: small
`)
	problems, err := Validate(dir)
	if err != nil || len(problems) != 0 {
		t.Fatalf("%v %#v", err, problems)
	}
	write(t, filepath.Join(dir, "widgets", "disk", "widget.yml"), strings.Replace(readFile(t,
		filepath.Join(dir, "widgets", "disk", "widget.yml")), "devmachine-app/stats", "devmachine-app/load", 1))
	problems, err = Validate(dir)
	if err != nil {
		t.Fatal(err)
	}
	problemAbout(t, problems, "devmachine-app has no provider load; its providers: stats")
}
