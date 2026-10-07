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
		"The menu bar holds at most 3 widgets",
		"| `menubar` | slot | available | none |",
		"| `menubar-panel` | tabs | available | none |",
		"| `app/open-pull-requests` | none | none | 5s | `count` number |",
		"| `app.brand` | `app/brand` | slot | none |",
		"| `gauge` | `number`, `json` | canvas, stack, tabs |",
		"| `number` | `number`, `json`, `app/open-pull-requests` | any layout |",
		"| `app/session-context` | none | `session` required | 5s |",
		"| `app.todo` | `app/session-context` | stack; grows with its content | none |",
		"| `text` | `text`, `lines`, `ansi` | any layout |",
		"| `sidebar` | stack | available |",
		"### Package providers",
		"`<package>/<command>`",
		"| `provider` | the provider's | no | `every` every, required; `name` provider, required; `target` target; `timeout` duration, default `30s`, max 10m; `with` args |",
		"### Inputs",
		"| `choice` | `from` enum, required, harnesses/machines/workspaces; `many` bool, default `false` | string, or a list of strings when many |",
		"| `boolean` | none | bool |",
		"A choice takes its options from `harnesses` (claude, codex), `machines` (the machines in config.yml), `workspaces` (the workspaces in config.yml).",
		"An entry with a `type` may also set `every` every; `title` string on a board: they change only that copy.",
		"| `app/machines` | `machines` list, optional | none | 5s | `list` machine_stats |",
		"| `app.pull-requests-panel` | `app/pull-requests-panel` | canvas, stack, tabs | none |",
		"`permission_mode` enum-by-harness, claude: manual/dontAsk/plan/acceptEdits/auto/bypassPermissions, " +
			"codex: read-only/workspace-write/danger-full-access/approve-for-me/dangerously-bypass-approvals-and-sandbox, " +
			"dangerous: bypassPermissions/danger-full-access/dangerously-bypass-approvals-and-sandbox",
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
