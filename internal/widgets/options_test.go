package widgets

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestOptionWarningsNameWhatTheSourceLacks(t *testing.T) {
	names := TargetNames{Machines: []string{"main"}, Workspaces: []string{"alice"}}
	machines, _ := choiceLookup("mine/machines")
	got := OptionWarnings("machines", machines, map[string]any{"machines": []any{"main", "ghost"}}, names)
	want := []string{`machines: input "machines" names machine "ghost", which config.yml does not have: the app leaves it out`}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}
	usage, _ := choiceLookup("mine/usage")
	got = OptionWarnings("usage", usage, map[string]any{"harness": "gemini"}, names)
	want = []string{`usage: input "harness" names harness "gemini", which is not one of claude, codex: the app leaves it out`}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}
	who := Entry{Name: "mine/who", Inputs: map[string]Input{"who": {Type: InputChoice, From: OptionWorkspaces}}}
	got = OptionWarnings("who", who, map[string]any{"who": "bob"}, names)
	want = []string{`who: input "who" names workspace "bob", which config.yml does not have: the app leaves it out`}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}
	if got := OptionWarnings("usage", usage, map[string]any{"harness": "claude"}, names); len(got) != 0 {
		t.Fatalf("got %v", got)
	}
}

func TestBoardOptionWarningsPointAtTheWithLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "home.yml")
	b, _ := ParseBoard(path, []byte(strings.Replace(choiceBoard, "{harness: codex}", "{harness: gemini}", 1)))
	got := BoardOptionWarnings(b, path, choiceLookup, TargetNames{Machines: []string{"main", "backup"}})
	if len(got) != 1 || got[0].Line != 13 || !strings.HasPrefix(got[0].Message, `usage: input "harness" names harness "gemini"`) {
		t.Fatalf("got %+v", got)
	}
}
