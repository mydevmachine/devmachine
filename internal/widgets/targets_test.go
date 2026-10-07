package widgets

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestUnknownTarget(t *testing.T) {
	names := TargetNames{Machines: []string{"main"}, Workspaces: []string{"alice"}}
	cases := []struct {
		target Target
		want   string
	}{
		{Target{}, ""},
		{Target{Local: true}, ""},
		{Target{Machine: "main"}, ""},
		{Target{Workspace: "alice"}, ""},
		{Target{Machine: "{{inputs.machine}}"}, ""},
		{Target{Machine: "ghost"}, `source.target names machine "ghost", which config.yml does not have`},
		{Target{Workspace: "bob"}, `source.target names workspace "bob", which config.yml does not have`},
	}
	for _, tc := range cases {
		if got := UnknownTarget(Source{Kind: SourceCommand, Target: tc.target}, names); got != tc.want {
			t.Errorf("%+v: got %q, want %q", tc.target, got, tc.want)
		}
	}
}

func TestBoardTargetProblemsPointAtTheTarget(t *testing.T) {
	path := filepath.Join(t.TempDir(), "home.yml")
	b, problems := ParseBoard(path, []byte(inlineBoard))
	if len(problems) != 0 {
		t.Fatal(problems)
	}
	got := BoardTargetProblems(b, path, TargetNames{Machines: []string{"main"}})
	if len(got) != 1 || got[0].Line != 10 || !strings.Contains(got[0].Message, `disk: source.target names workspace "alice"`) {
		t.Fatalf("got %v", got)
	}
	if got := BoardTargetProblems(b, path, TargetNames{Workspaces: []string{"alice"}}); len(got) != 0 {
		t.Fatalf("got %v", got)
	}
}
