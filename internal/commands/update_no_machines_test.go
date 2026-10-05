package commands

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
)

type noMachinesJSON struct {
	CLI struct {
		From    string `json:"from"`
		To      string `json:"to"`
		Updated bool   `json:"updated"`
	} `json:"cli"`
	Packages struct {
		From   string `json:"from"`
		To     string `json:"to"`
		Pinned bool   `json:"pinned"`
	} `json:"packages"`
	Skills struct {
		Updated bool `json:"updated"`
	} `json:"skills"`
}

func decodeNoMachines(t *testing.T, stdout string) noMachinesJSON {
	t.Helper()
	var got noMachinesJSON
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not one JSON object: %v\n%s", err, stdout)
	}
	return got
}

func assertNoMachineWasReached(t *testing.T, w updateWorld, out string) {
	t.Helper()
	for _, other := range []string{"Doctor", "Sync check", "[y/N]", "sync"} {
		if strings.Contains(out, other) {
			t.Fatalf("--no-machines went past the skills (%q):\n%s", other, out)
		}
	}
	if len(w.runs.runs) != 0 {
		t.Fatalf("--no-machines reached a machine: %+v", w.runs.runs)
	}
}

func TestUpdateNoMachinesRunsTheFirstThreeStepsOnly(t *testing.T) {
	w := newUpdateWorld(t, "v16", "v17", 2)

	out, err := executeWithInput(t, "y\n", "--config", w.dir, "update", "--no-machines")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	last := -1
	for _, header := range []string{"==> 1/3 CLI", "==> 2/3 Packages", "==> 3/3 Skills"} {
		at := strings.Index(out, header)
		if at <= last {
			t.Fatalf("%q is missing or out of order:\n%s", header, out)
		}
		last = at
	}
	assertSummary(t, out, "packages", "v16 → v17")
	assertNoMachineWasReached(t, w, out)
	if got := pinnedIn(t, w.dir); got != "v17" {
		t.Fatalf("pinned %q", got)
	}
}

func TestUpdateNoMachinesJSONReportsEachStep(t *testing.T) {
	w := newUpdateWorld(t, "v16", "v17", 2)

	stdout, stderr, err := executeSplit(t, "--config", w.dir, "--format", "json", "update", "--no-machines")
	if err != nil {
		t.Fatalf("%v\n%s", err, stderr)
	}
	got := decodeNoMachines(t, stdout)
	var want noMachinesJSON
	want.CLI.From, want.CLI.To = "0.7.18", "0.7.18"
	want.Packages.From, want.Packages.To, want.Packages.Pinned = "v16", "v17", true
	if got != want {
		t.Fatalf("got %+v; want %+v", got, want)
	}
	if !strings.Contains(stderr, "==> 2/3 Packages") {
		t.Fatalf("the log is not on stderr:\n%s", stderr)
	}
	assertNoMachineWasReached(t, w, stdout+stderr)
}

func TestUpdateNoMachinesJSONAfterTheHandOver(t *testing.T) {
	w := newUpdateWorld(t, "v17", "v17", 0)

	stdout, stderr, err := executeSplit(t, "--config", w.dir, "--format", "json",
		"update", "--no-machines", "--continue-after-self-update=0.7.17")
	if err != nil {
		t.Fatalf("%v\n%s", err, stderr)
	}
	got := decodeNoMachines(t, stdout)
	if got.CLI.From != "0.7.17" || got.CLI.To != "0.7.18" || !got.CLI.Updated {
		t.Fatalf("cli: %+v", got.CLI)
	}
	if got.Packages.From != "v17" || got.Packages.To != "v17" || got.Packages.Pinned {
		t.Fatalf("packages: %+v", got.Packages)
	}
}

func TestUpdateNoMachinesHandsOverWithNoMachines(t *testing.T) {
	w := newSelfUpdateWorld(t, "new binary", false)
	t.Cleanup(swap(&commandLineArgs, func() []string {
		return []string{"--config", w.dir, "--format", "json", "update", "--no-machines"}
	}))

	stdout, _, err := executeSplit(t, "--config", w.dir, "--format", "json", "update", "--no-machines")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{w.binary, w.binary, "--config", w.dir, "--format", "json", "update", "--no-machines",
		"--continue-after-self-update=0.7.17"}
	if !slices.Equal(*w.execed, want) {
		t.Fatalf("started %q; want %q", *w.execed, want)
	}
	if stdout != "" {
		t.Fatalf("the old binary printed a result before handing over: %s", stdout)
	}
}

func TestUpdateNoMachinesWithNothingPinnedPinsNothing(t *testing.T) {
	w := newUpdateWorld(t, "v16", "v17", 0)
	dir := pinConfig(t, "")

	stdout, stderr, err := executeSplit(t, "--config", dir, "--format", "json", "update", "--no-machines")
	if err != nil {
		t.Fatalf("%v\n%s", err, stderr)
	}
	got := decodeNoMachines(t, stdout)
	if got.Packages.From != "" || got.Packages.To != "" || got.Packages.Pinned {
		t.Fatalf("packages: %+v", got.Packages)
	}
	if pinned := pinnedIn(t, dir); pinned != "" {
		t.Fatalf("pinned %q", pinned)
	}
	assertNoMachineWasReached(t, w, stdout+stderr)
}

func TestUpdateNoMachinesSkipPackagesKeepsThePin(t *testing.T) {
	w := newUpdateWorld(t, "v16", "v17", 0)

	stdout, stderr, err := executeSplit(t, "--config", w.dir, "--format", "json",
		"update", "--no-machines", "--skip-packages", "--skip-cli")
	if err != nil {
		t.Fatalf("%v\n%s", err, stderr)
	}
	got := decodeNoMachines(t, stdout)
	if got.Packages.From != "v16" || got.Packages.To != "v16" || got.Packages.Pinned {
		t.Fatalf("packages: %+v", got.Packages)
	}
	if *w.cli != 0 {
		t.Fatal("--skip-cli still asked GitHub about the CLI")
	}
}

func TestUpdateNoMachinesPrintsWhatWasDoneThenFails(t *testing.T) {
	w := newUpdateWorld(t, "v16", "v17", 0)
	stubLatestCLI(t, "", errors.New("no route to host"))

	stdout, _, err := executeSplit(t, "--config", w.dir, "--format", "json", "update", "--no-machines")
	if err == nil || !strings.Contains(err.Error(), "cli") {
		t.Fatalf("a failed step must fail the command: %v", err)
	}
	got := decodeNoMachines(t, stdout)
	if got.CLI.Updated || got.CLI.From != "0.7.18" || got.CLI.To != "0.7.18" || !got.Packages.Pinned {
		t.Fatalf("got %+v", got)
	}
}

func TestUpdateNoMachinesRefusesWhatReachesAMachine(t *testing.T) {
	w := newUpdateWorld(t, "v16", "v17", 0)
	for _, args := range [][]string{
		{"--config", w.dir, "--machine", "main", "update", "--no-machines"},
		{"--config", w.dir, "update", "--no-machines", "--cli-only"},
		{"--config", w.dir, "update", "--no-machines", "--yes"},
	} {
		_, err := execute(t, args...)
		if err == nil || !strings.Contains(err.Error(), "--no-machines") {
			t.Fatalf("%q: got %v", args, err)
		}
	}
	if *w.cli != 0 || len(w.runs.runs) != 0 || pinnedIn(t, w.dir) != "v16" {
		t.Fatal("a refused combination still did something")
	}
}
