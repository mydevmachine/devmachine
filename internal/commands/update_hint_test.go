package commands

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

const wantHint = "packages v33 is out (you pin v32): run devmachine update"

// hintWorld is a person at a terminal running 0.7.18, pinning v32, with v33
// published.
func hintWorld(t *testing.T) (*releaseWorld, string) {
	t.Helper()
	runningVersion(t, "0.7.18")
	t.Cleanup(swap(&hintToTerminal, func(*cobra.Command) bool { return true }))
	t.Setenv(noUpdateHintEnv, "")
	return newReleaseWorld(t, "v33"), pinConfig(t, "v32")
}

func hinted(t *testing.T, args ...string) bool {
	t.Helper()
	stdout, stderr, err := executeSplit(t, args...)
	if err != nil {
		t.Fatalf("%v\n%s", err, stderr)
	}
	if strings.Contains(stdout, wantHint) {
		t.Fatalf("the hint went to stdout:\n%s", stdout)
	}
	return strings.Contains(stderr, wantHint)
}

func TestUpdateHintAfterAListSaysANewerReleaseIsOut(t *testing.T) {
	for _, command := range [][]string{{"machines", "list"}, {"workspaces", "list"}} {
		t.Run(strings.Join(command, " "), func(t *testing.T) {
			_, dir := hintWorld(t)
			if !hinted(t, append([]string{"--config", dir}, command...)...) {
				t.Fatal("no hint")
			}
		})
	}
}

func TestUpdateHintAtMostOnceADay(t *testing.T) {
	w, dir := hintWorld(t)
	if !hinted(t, "--config", dir, "machines", "list") {
		t.Fatal("no hint the first time")
	}
	w.now = w.now.Add(23 * time.Hour)
	if hinted(t, "--config", dir, "machines", "list") {
		t.Fatal("hinted twice in a day")
	}
	w.now = w.now.Add(2 * time.Hour)
	if !hinted(t, "--config", dir, "machines", "list") {
		t.Fatal("no hint the next day")
	}
}

func TestUpdateHintOnTheFirstRunOfANewCLIVersion(t *testing.T) {
	w, dir := hintWorld(t)
	hinted(t, "--config", dir, "machines", "list")

	w.now = w.now.Add(time.Hour)
	runningVersion(t, "0.7.19")
	if !hinted(t, "--config", dir, "machines", "list") {
		t.Fatal("no hint after the CLI changed version")
	}
	if hinted(t, "--config", dir, "machines", "list") {
		t.Fatal("hinted again on the second run of the new version")
	}
}

func TestUpdateHintAsksGitHubAtMostOnceADay(t *testing.T) {
	w, dir := hintWorld(t)
	w.err = errors.New("no route to host")
	for i := range 3 {
		runningVersion(t, fmt.Sprintf("0.7.%d", 19+i))
		w.now = w.now.Add(time.Hour)
		if hinted(t, "--config", dir, "machines", "list") {
			t.Fatal("hinted with no answer")
		}
	}
	if w.calls != 1 {
		t.Fatalf("asked GitHub %d times in a day", w.calls)
	}
	w.err = nil
	w.now = w.now.Add(24 * time.Hour)
	if !hinted(t, "--config", dir, "machines", "list") || w.calls != 2 {
		t.Fatalf("no hint the next day, after %d calls", w.calls)
	}
}

func TestUpdateHintReadsARecentAnswerWithoutAsking(t *testing.T) {
	w, dir := hintWorld(t)
	if _, err := execute(t, "--config", dir, "packages", "outdated"); err != nil {
		t.Fatal(err)
	}
	if !hinted(t, "--config", dir, "machines", "list") || w.calls != 1 {
		t.Fatalf("asked GitHub %d times", w.calls)
	}
}

func TestUpdateHintStaysQuiet(t *testing.T) {
	cases := map[string]func(t *testing.T, dir string) []string{
		"json output": func(t *testing.T, dir string) []string {
			return []string{"--config", dir, "--format", "json", "machines", "list"}
		},
		"turned off": func(t *testing.T, dir string) []string {
			t.Setenv(noUpdateHintEnv, "1")
			return []string{"--config", dir, "machines", "list"}
		},
		"no terminal": func(t *testing.T, dir string) []string {
			t.Cleanup(swap(&hintToTerminal, func(*cobra.Command) bool { return false }))
			return []string{"--config", dir, "machines", "list"}
		},
		"another command": func(t *testing.T, dir string) []string {
			return []string{"--config", dir, "config", "path"}
		},
		"the pin is the latest": func(t *testing.T, _ string) []string {
			return []string{"--config", pinConfig(t, "v33"), "machines", "list"}
		},
		"nothing pinned": func(t *testing.T, _ string) []string {
			return []string{"--config", pinConfig(t, ""), "machines", "list"}
		},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			w, dir := hintWorld(t)
			stdout, stderr, err := executeSplit(t, args(t, dir)...)
			if err != nil {
				t.Fatalf("%v\n%s", err, stderr)
			}
			if strings.Contains(stdout+stderr, "is out") {
				t.Fatalf("hinted:\n%s%s", stdout, stderr)
			}
			if name != "the pin is the latest" && w.calls != 0 {
				t.Fatalf("asked GitHub %d times", w.calls)
			}
		})
	}
}

func TestUpdateHintIsOffInTests(t *testing.T) {
	runningVersion(t, "0.7.18")
	w := newReleaseWorld(t, "v33")
	if hinted(t, "--config", pinConfig(t, "v32"), "machines", "list") || w.calls != 0 {
		t.Fatal("a test without a terminal was hinted")
	}
}
