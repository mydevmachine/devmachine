package commands

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// releaseWorld is the network and the clock a latest-release lookup sees: a
// fake GitHub that counts its calls, a cache folder of its own and a clock a
// test moves by hand.
type releaseWorld struct {
	latest string
	err    error
	calls  int
	now    time.Time
	cache  string
}

func newReleaseWorld(t *testing.T, latest string) *releaseWorld {
	t.Helper()
	w := &releaseWorld{latest: latest, now: time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC), cache: t.TempDir()}
	t.Cleanup(swap(&latestPackagesRelease, func(context.Context) (string, error) {
		w.calls++
		return w.latest, w.err
	}))
	t.Cleanup(swap(&releaseCacheDir, func() (string, error) { return w.cache, nil }))
	t.Cleanup(swap(&releaseClock, func() time.Time { return w.now }))
	return w
}

func pinConfig(t *testing.T, pinned string) string {
	t.Helper()
	body := "machines:\n  - name: main\n    hosts: [203.0.113.10]\n"
	if pinned != "" {
		body += "packages: " + pinned + "\n"
	}
	return configWith(t, body)
}

type outdatedJSON struct {
	Pinned   string `json:"pinned"`
	Latest   string `json:"latest"`
	Newer    bool   `json:"newer"`
	NotesURL string `json:"notes_url"`
}

func outdated(t *testing.T, dir string) outdatedJSON {
	t.Helper()
	stdout, stderr, err := executeSplit(t, "--config", dir, "--format", "json", "packages", "outdated")
	if err != nil {
		t.Fatalf("%v\n%s", err, stderr)
	}
	var got outdatedJSON
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not one JSON object: %v\n%s", err, stdout)
	}
	return got
}

func TestPackagesOutdatedSaysANewerReleaseIsOut(t *testing.T) {
	newReleaseWorld(t, "v33")
	dir := pinConfig(t, "v32")

	got := outdated(t, dir)
	want := outdatedJSON{Pinned: "v32", Latest: "v33", Newer: true,
		NotesURL: "https://github.com/mydevmachine/packages/releases/tag/v33"}
	if got != want {
		t.Fatalf("got %+v; want %+v", got, want)
	}

	out, err := execute(t, "--config", dir, "packages", "outdated")
	if err != nil || !strings.Contains(out, "packages v33 is out (you pin v32): run devmachine update") {
		t.Fatalf("%v\n%s", err, out)
	}
}

func TestPackagesOutdatedWhenThePinIsTheLatest(t *testing.T) {
	newReleaseWorld(t, "v33")
	for _, pinned := range []string{"v33", "v34"} {
		got := outdated(t, pinConfig(t, pinned))
		if got.Newer || got.Pinned != pinned || got.Latest != "v33" {
			t.Fatalf("%s: got %+v", pinned, got)
		}
	}
}

func TestPackagesOutdatedWithNothingPinned(t *testing.T) {
	newReleaseWorld(t, "v33")
	for name, dir := range map[string]string{
		"no pin":           pinConfig(t, ""),
		"no configuration": filepath.Join(t.TempDir(), "missing"),
	} {
		got := outdated(t, dir)
		if got.Pinned != "" || got.Newer || got.Latest != "v33" {
			t.Fatalf("%s: got %+v", name, got)
		}
	}
}

func TestPackagesOutdatedAsksGitHubAtMostOnceADay(t *testing.T) {
	w := newReleaseWorld(t, "v33")
	dir := pinConfig(t, "v32")

	outdated(t, dir)
	w.now = w.now.Add(23 * time.Hour)
	w.latest = "v34"
	if got := outdated(t, dir); got.Latest != "v33" || w.calls != 1 {
		t.Fatalf("within a day: got %+v after %d calls", got, w.calls)
	}
	w.now = w.now.Add(2 * time.Hour)
	if got := outdated(t, dir); got.Latest != "v34" || w.calls != 2 {
		t.Fatalf("after a day: got %+v after %d calls", got, w.calls)
	}
}

func TestPackagesOutdatedOfflineUsesAnOldAnswer(t *testing.T) {
	w := newReleaseWorld(t, "v33")
	dir := pinConfig(t, "v32")
	outdated(t, dir)

	w.now = w.now.Add(72 * time.Hour)
	w.err = errors.New("no route to host")
	if got := outdated(t, dir); got.Latest != "v33" || !got.Newer {
		t.Fatalf("got %+v", got)
	}
}

func TestPackagesOutdatedOfflineWithNoAnswerFails(t *testing.T) {
	w := newReleaseWorld(t, "")
	w.err = errors.New("no route to host")
	dir := pinConfig(t, "v32")

	stdout, _, err := executeSplit(t, "--config", dir, "--format", "json", "packages", "outdated")
	if err == nil || !strings.Contains(err.Error(), "no route to host") || !strings.Contains(err.Error(), "latest packages release") {
		t.Fatalf("got %v", err)
	}
	if stdout != "" {
		t.Fatalf("a failure printed JSON: %s", stdout)
	}
}

func TestPackagesPinRemembersTheLatestRelease(t *testing.T) {
	w := newReleaseWorld(t, "v33")
	dir := pinConfig(t, "v32")

	if _, err := execute(t, "--config", dir, "packages", "pin"); err != nil {
		t.Fatal(err)
	}
	if got := outdated(t, dir); got.Latest != "v33" || got.Newer || w.calls != 1 {
		t.Fatalf("got %+v after %d calls", got, w.calls)
	}
}
