package widgets

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTheWidgetFormatPageMatchesTheContract(t *testing.T) {
	path := filepath.Join("..", "..", "docs", "reference", "widget-format.md")
	page, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want, err := ReplaceReference(string(page))
	if err != nil {
		t.Fatal(err)
	}
	if string(page) != want {
		t.Fatalf("%s is out of date with the engine contract: run `make widget-format`", path)
	}
}

func TestReferenceTablesNameEveryProviderAndView(t *testing.T) {
	tables := ReferenceTables()
	for _, want := range []string{
		"`app/harness-usage`", "`app.clock`", "`context-sidebar`", "| `wide` | 8×2 | 640×160 |",
		"### Source kinds", "| `command` | 5s | yes |", "`kind:url`", "`timeout` duration, default `30s`, max 10m",
		"One preset row in a sidebar is 40pt",
		"| `app/session-context` | none | `session` required | 5s |",
		"| `app.todo` | `app/session-context` | stack; grows with its content | none |",
		"| `text` | `text`, `lines`, `ansi` | any layout |",
		"| `sidebar` | stack | available |",
		"### Package providers",
		"`<package>/<command>`",
		"| `provider` | the provider's | no | `every` every, required; `name` provider, required; `target` target; `timeout` duration, default `30s`, max 10m; `with` args |",
	} {
		if !strings.Contains(tables, want) {
			t.Errorf("tables lack %s", want)
		}
	}
}

func TestReplaceReferenceNeedsBothMarkers(t *testing.T) {
	if _, err := ReplaceReference("no markers"); err == nil {
		t.Fatal("want an error")
	}
	if _, err := ReplaceReference(ReferenceStart + " but no end"); err == nil {
		t.Fatal("want an error")
	}
}
