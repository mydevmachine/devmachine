package commands

import (
	"testing"

	"github.com/mydevmachine/devmachine/internal/facts"
)

func TestRunPutsTheMachinesPathPrefixFirst(t *testing.T) {
	var runs []string
	dialing(t, factsRemote{runs: &runs})
	dir := configWith(t, twoMachineConfig)
	saveFacts(t, dir, "sandbox", observedMac)

	if _, err := execute(t, "--config", dir, "--machine", "sandbox", "run", "port installed"); err != nil {
		t.Fatal(err)
	}
	want := `PATH='/opt/local/bin:/opt/local/sbin':"$PATH"; export PATH; port installed`
	if len(runs) != 1 || runs[0] != want {
		t.Fatalf("ran %q, want %q", runs, want)
	}
}

func TestRunSendsTheCommandAsItIsWithoutAPathPrefix(t *testing.T) {
	for name, observed := range map[string]*facts.Facts{
		"never read": nil,
		"linux":      {System: "Linux", Distribution: "Debian"},
	} {
		var runs []string
		dialing(t, factsRemote{runs: &runs})
		dir := configWith(t, twoMachineConfig)
		if observed != nil {
			saveFacts(t, dir, "sandbox", *observed)
		}

		if _, err := execute(t, "--config", dir, "--machine", "sandbox", "run", "apt list --installed"); err != nil {
			t.Fatal(err)
		}
		if len(runs) != 1 || runs[0] != "apt list --installed" {
			t.Fatalf("%s: ran %q", name, runs)
		}
	}
}

func TestPathPrefixIsQuotedForTheShell(t *testing.T) {
	got := withPathPrefix([]string{"/opt/it's here/bin"}, "true")
	want := `PATH='/opt/it'\''s here/bin':"$PATH"; export PATH; true`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
