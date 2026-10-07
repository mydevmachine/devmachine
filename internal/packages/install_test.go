package packages

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mydevmachine/devmachine/internal/gittest"
)

const aliceToolsManifest = `format: 1
name: alice-tools
scope: machine
summary: Tools from alice.
requires: {cli: ">= 0.9.0"}
needs: [base]
entrypoint: bin/alice-tools
commands: [disk, help]
providers:
  disk:
    returns: {used_percent: number}
    min_every: 10s
credentials:
  - {name: alice-token, kind: secret, scope: machine, env: ALICE_TOKEN}
widgets: widgets
`

const aliceDiskWidget = `format: 1
name: disk
summary: How full the disk is.
requires: {engine: ">= 1.3"}
fits: [canvas]
source: {kind: provider, name: alice-tools/disk, target: {machine: main}, every: 60s}
view: {kind: gauge, value: "{{json.used_percent}}"}
sizes: [small]
default_size: small
`

const aliceHealthWidget = `format: 1
name: health
summary: Whether the site answers.
requires: {engine: ">= 1.1"}
fits: [canvas]
source: {kind: url, url: "https://example.com/health", every: 30s}
view: {kind: status}
sizes: [small]
default_size: small
`

func aliceToolsRepo(t *testing.T) *gittest.Repo {
	t.Helper()
	r := gittest.New(t)
	r.Write("package.yml", aliceToolsManifest, 0o644)
	r.Write("tasks/main.yml", "---\n[]\n", 0o644)
	r.Write("bin/alice-tools", "#!/usr/bin/env python3\n", 0o755)
	r.Write("widgets/disk/widget.yml", aliceDiskWidget, 0o644)
	r.Write("widgets/health/widget.yml", aliceHealthWidget, 0o644)
	r.Commit("first")
	r.Tag("v1")
	return r
}

func installDebris(t *testing.T, configDir string) []string {
	t.Helper()
	entries, err := os.ReadDir(LocalDir(configDir))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".install-") {
			out = append(out, e.Name())
		}
	}
	return out
}

func TestStageChecksAndSummarizes(t *testing.T) {
	r := aliceToolsRepo(t)
	configDir := t.TempDir()
	staged, err := Stage(context.Background(), configDir, r.URL(), "v1")
	if err != nil {
		t.Fatal(err)
	}
	defer staged.Discard()
	want := Summary{
		Name: "alice-tools", Scope: "machine", Summary: "Tools from alice.", Needs: []string{"base"},
		Widgets: []SummaryWidget{
			{Name: "alice-tools/disk", Source: "provider alice-tools/disk", RunsCode: true},
			{Name: "alice-tools/health", Source: "url", RunsCode: false},
		},
		Commands: []string{"disk", "help"}, Providers: []string{"disk"},
		Scripts: []string{"bin/alice-tools"}, Tasks: []string{"tasks/main.yml"}, Credentials: []string{"alice-token (secret)"},
	}
	if staged.Name != "alice-tools" || staged.Origin.Commit != r.Head() || staged.Origin.Ref != "v1" || staged.Origin.URL != r.URL() {
		t.Fatalf("got %+v", staged)
	}
	if !summariesEqual(staged.Summary, want) {
		t.Fatalf("got %+v\nwant %+v", staged.Summary, want)
	}
	path, err := staged.Install(configDir, time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC))
	if err != nil || path != filepath.Join(LocalDir(configDir), "alice-tools") {
		t.Fatalf("%v %s", err, path)
	}
	origin, ok, err := ReadOrigin(path)
	if err != nil || !ok || origin.InstalledAt != "2026-10-07T12:00:00Z" || origin.Commit != r.Head() {
		t.Fatalf("%+v %v %v", origin, ok, err)
	}
	staged.Discard()
	if debris := installDebris(t, configDir); len(debris) != 0 {
		t.Fatalf("left %v", debris)
	}
}

func summariesEqual(a, b Summary) bool {
	return a.Name == b.Name && a.Scope == b.Scope && a.Summary == b.Summary && slices.Equal(a.Needs, b.Needs) &&
		slices.Equal(a.Widgets, b.Widgets) && slices.Equal(a.Commands, b.Commands) && slices.Equal(a.Providers, b.Providers) &&
		slices.Equal(a.Scripts, b.Scripts) && slices.Equal(a.Tasks, b.Tasks) && slices.Equal(a.Credentials, b.Credentials)
}

func TestStageRefusesALinkLeadingOutside(t *testing.T) {
	r := aliceToolsRepo(t)
	outside := filepath.Join(t.TempDir(), "id_ed25519")
	if err := os.WriteFile(outside, []byte("secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r.Link("files/id", outside)
	r.Commit("a link")
	configDir := t.TempDir()
	_, err := Stage(context.Background(), configDir, r.URL(), "")
	if err == nil || !strings.Contains(err.Error(), "files/id is a link leading outside the package") {
		t.Fatalf("got %v", err)
	}
	if debris := installDebris(t, configDir); len(debris) != 0 {
		t.Fatalf("left %v", debris)
	}
}

func TestStageAcceptsALinkInsideThePackage(t *testing.T) {
	r := aliceToolsRepo(t)
	r.Link("bin/tools", "alice-tools")
	r.Commit("a link inside")
	staged, err := Stage(context.Background(), t.TempDir(), r.URL(), "")
	if err != nil {
		t.Fatal(err)
	}
	staged.Discard()
}

func TestStageRefusesWhatIsNotOnePackage(t *testing.T) {
	cases := map[string]func(r *gittest.Repo){
		"has no package.yml at its top": func(r *gittest.Repo) {
			r.Remove("package.yml")
			r.Write("packages/alice-tools/package.yml", aliceToolsManifest, 0o644)
		},
		"has 1 problem(s)": func(r *gittest.Repo) { r.Remove("tasks/main.yml") },
		`is named "../x"`: func(r *gittest.Repo) {
			r.Write("package.yml", strings.Replace(aliceToolsManifest, "name: alice-tools", `name: "../x"`, 1), 0o644)
		},
	}
	for want, change := range cases {
		r := aliceToolsRepo(t)
		change(r)
		r.Commit("change")
		configDir := t.TempDir()
		_, err := Stage(context.Background(), configDir, r.URL(), "")
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("want %q, got %v", want, err)
		}
		if debris := installDebris(t, configDir); len(debris) != 0 {
			t.Errorf("%s: left %v", want, debris)
		}
	}
}

func TestInstallReplacesTheOldCopy(t *testing.T) {
	r := aliceToolsRepo(t)
	configDir := t.TempDir()
	first, err := Stage(context.Background(), configDir, r.URL(), "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.Install(configDir, time.Now()); err != nil {
		t.Fatal(err)
	}
	first.Discard()
	r.Remove("widgets/health/widget.yml")
	r.Commit("drop health")
	second, err := Stage(context.Background(), configDir, r.URL(), "")
	if err != nil {
		t.Fatal(err)
	}
	path, err := second.Install(configDir, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	second.Discard()
	if _, err := os.Stat(filepath.Join(path, "widgets", "health")); !os.IsNotExist(err) {
		t.Fatal("the old copy's files are still there")
	}
	if debris := installDebris(t, configDir); len(debris) != 0 {
		t.Fatalf("left %v", debris)
	}
}
