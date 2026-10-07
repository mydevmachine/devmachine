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
	path, err := second.Replace(configDir, time.Now())
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

func TestStageChecksLinksBeforeReadingPackageYML(t *testing.T) {
	r := aliceToolsRepo(t)
	outside := filepath.Join(t.TempDir(), "package.yml")
	if err := os.WriteFile(outside, []byte("not: [yaml\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r.Remove("package.yml")
	r.Link("package.yml", outside)
	r.Commit("package.yml leads outside")
	configDir := t.TempDir()
	_, err := Stage(context.Background(), configDir, r.URL(), "")
	if err == nil || !strings.Contains(err.Error(), "package.yml is a link leading outside the package") {
		t.Fatalf("got %v", err)
	}
	if debris := installDebris(t, configDir); len(debris) != 0 {
		t.Fatalf("left %v", debris)
	}
}

func TestStageRefusesARelativeLinkLeadingOutside(t *testing.T) {
	r := aliceToolsRepo(t)
	r.Link("files/id", "../../../../secret")
	r.Commit("a relative link")
	configDir := t.TempDir()
	write(t, filepath.Join(configDir, "secret"), "secret\n")
	_, err := Stage(context.Background(), configDir, r.URL(), "")
	if err == nil || !strings.Contains(err.Error(), "files/id is a link leading outside the package") {
		t.Fatalf("got %v", err)
	}
}

func TestStageRefusesALinkChainThatLeavesThePackage(t *testing.T) {
	r := aliceToolsRepo(t)
	outside := filepath.Join(t.TempDir(), "id_ed25519")
	if err := os.WriteFile(outside, []byte("secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r.Link("files/a", "b")
	r.Link("files/b", outside)
	r.Commit("a chain")
	_, err := Stage(context.Background(), t.TempDir(), r.URL(), "")
	if err == nil || !strings.Contains(err.Error(), "is a link leading outside the package") {
		t.Fatalf("got %v", err)
	}
}

func namedRepo(t *testing.T, name string) *gittest.Repo {
	t.Helper()
	r := gittest.New(t)
	r.Write("package.yml", "format: 1\nname: "+name+"\nscope: machine\nsummary: A package.\n", 0o644)
	r.Write("tasks/main.yml", "---\n[]\n", 0o644)
	r.Commit("first")
	return r
}

func TestReplaceWorksForAPackageNamedLikeAWorkingFolder(t *testing.T) {
	for _, name := range []string{"previous", "clone"} {
		r := namedRepo(t, name)
		configDir := t.TempDir()
		first, err := Stage(context.Background(), configDir, r.URL(), "")
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if _, err := first.Install(configDir, time.Now()); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		first.Discard()
		r.Write("tasks/main.yml", "---\n- debug: {msg: hi}\n", 0o644)
		r.Commit("second")
		second, err := Stage(context.Background(), configDir, r.URL(), "")
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		path, err := second.Replace(configDir, time.Now())
		second.Discard()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if origin, _, _ := ReadOrigin(path); origin.Commit != r.Head() {
			t.Fatalf("%s: got %+v", name, origin)
		}
	}
}

func TestInstallRefusesWhenTheFolderAppearedSinceStage(t *testing.T) {
	r := aliceToolsRepo(t)
	configDir := t.TempDir()
	staged, err := Stage(context.Background(), configDir, r.URL(), "")
	if err != nil {
		t.Fatal(err)
	}
	defer staged.Discard()
	write(t, filepath.Join(LocalDir(configDir), "alice-tools", FileName), "format: 1\nname: alice-tools\n")
	if _, err := staged.Install(configDir, time.Now()); err == nil || !strings.Contains(err.Error(), "appeared while alice-tools was being fetched") {
		t.Fatalf("got %v", err)
	}
	if got := readFile(t, filepath.Join(LocalDir(configDir), "alice-tools", FileName)); got != "format: 1\nname: alice-tools\n" {
		t.Fatalf("your package changed: %q", got)
	}
}

func TestReplaceKeepsTheOldCopyWhenPuttingItBackFails(t *testing.T) {
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
	orig := rename
	calls := 0
	rename = func(from, to string) error {
		calls++
		if calls > 1 {
			return errors.New("disk full")
		}
		return orig(from, to)
	}
	t.Cleanup(func() { rename = orig })
	_, err = second.Replace(configDir, time.Now())
	second.Discard()
	if err == nil || !strings.Contains(err.Error(), "putting the old copy back failed") {
		t.Fatalf("got %v", err)
	}
	kept := filepath.Join(second.temp, ".previous")
	if !strings.Contains(err.Error(), kept) {
		t.Fatalf("the error does not say where the old copy is: %v", err)
	}
	if _, err := os.Stat(filepath.Join(kept, "widgets", "health", "widget.yml")); err != nil {
		t.Fatal("the old copy was deleted")
	}
	sweepStale(LocalDir(configDir), time.Now().Add(2*staleAfter))
	if _, err := os.Stat(kept); err != nil {
		t.Fatal("the sweep deleted the old copy")
	}
}

func TestStageSweepsStaleFetchFolders(t *testing.T) {
	configDir := t.TempDir()
	local := LocalDir(configDir)
	stale := filepath.Join(local, ".install-stale")
	fresh := filepath.Join(local, ".install-fresh")
	write(t, filepath.Join(stale, ".clone", "x"), "x")
	write(t, filepath.Join(fresh, ".clone", "x"), "x")
	old := time.Now().Add(-2 * staleAfter)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}
	_, _ = Stage(context.Background(), configDir, "file://"+filepath.Join(t.TempDir(), "missing"), "")
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatal("the stale folder is still there")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatal("a folder another command may be using was deleted")
	}
}

func comparePackage(t *testing.T, manifest string, files map[string]string, scripts map[string]string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "alice-tools")
	write(t, ManifestPath(dir), manifest)
	for name, body := range files {
		write(t, filepath.Join(dir, name), body)
	}
	for name, body := range scripts {
		writeMode(t, filepath.Join(dir, name), body, 0o755)
	}
	return dir
}

func TestCompareNamesWhatWasAddedRemovedAndChanged(t *testing.T) {
	beforeManifest := strings.Replace(aliceToolsManifest, "env: ALICE_TOKEN}\n",
		"env: ALICE_TOKEN}\n  - {name: bob-token, kind: secret, scope: machine, env: BOB_TOKEN}\n", 1)
	before := comparePackage(t, beforeManifest,
		map[string]string{"tasks/main.yml": "---\n[]\n", "tasks/old.yml": "---\n[]\n",
			"widgets/disk/widget.yml": aliceDiskWidget, "widgets/health/widget.yml": aliceHealthWidget},
		map[string]string{"bin/alice-tools": "#!/usr/bin/env python3\n", "bin/old": "#!/bin/sh\n"})
	afterManifest := strings.NewReplacer(
		"commands: [disk, help]", "commands: [disk, load]",
		"    min_every: 10s\n", "    min_every: 10s\n  load:\n    returns: {avg: number}\n    min_every: 10s\n",
		"env: ALICE_TOKEN}\n", "env: ALICE_KEY}\n  - {name: carol-token, kind: secret, scope: machine, env: CAROL_TOKEN}\n",
	).Replace(aliceToolsManifest)
	after := comparePackage(t, afterManifest,
		map[string]string{"tasks/main.yml": "---\n- debug: {msg: hi}\n", "tasks/new.yml": "---\n[]\n",
			"widgets/disk/widget.yml": aliceDiskWidget},
		map[string]string{"bin/alice-tools": "#!/usr/bin/env python3\nprint(1)\n", "bin/new": "#!/bin/sh\n"})

	got, err := Compare(before, after)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		field     string
		got, want []string
	}{
		{"widgets added", got.WidgetsAdded, []string{}},
		{"widgets removed", got.WidgetsRemoved, []string{"alice-tools/health"}},
		{"commands added", got.CommandsAdded, []string{"load"}},
		{"commands removed", got.CommandsRemoved, []string{"help"}},
		{"providers added", got.ProvidersAdded, []string{"load"}},
		{"providers removed", got.ProvidersRemoved, []string{}},
		{"credentials added", got.CredentialsAdded, []string{"carol-token"}},
		{"credentials removed", got.CredentialsRemoved, []string{"bob-token"}},
		{"credentials changed", got.CredentialsChanged, []string{"alice-token"}},
		{"scripts added", got.ScriptsAdded, []string{"bin/new"}},
		{"scripts removed", got.ScriptsRemoved, []string{"bin/old"}},
		{"scripts changed", got.ScriptsChanged, []string{"bin/alice-tools"}},
		{"tasks added", got.TasksAdded, []string{"tasks/new.yml"}},
		{"tasks removed", got.TasksRemoved, []string{"tasks/old.yml"}},
		{"tasks changed", got.TasksChanged, []string{"tasks/main.yml"}},
	} {
		if !slices.Equal(c.got, c.want) {
			t.Errorf("%s: got %q, want %q", c.field, c.got, c.want)
		}
	}
	if got.Empty() {
		t.Fatal("a changed package reads as unchanged")
	}
	same, err := Compare(before, before)
	if err != nil || !same.Empty() {
		t.Fatalf("the same package changed: %+v %v", same, err)
	}
}

func TestUninstallRefusesAPackageWithoutAnOrigin(t *testing.T) {
	configDir := t.TempDir()
	write(t, filepath.Join(LocalDir(configDir), "mine", FileName), "format: 1\nname: mine\n")
	_, err := Uninstall(configDir, "mine")
	if err == nil || !strings.Contains(err.Error(), "mine is your own package, not one installed from a git address") {
		t.Fatalf("got %v", err)
	}
	if _, err := os.Stat(filepath.Join(LocalDir(configDir), "mine")); err != nil {
		t.Fatal("your own package was deleted")
	}
	for _, name := range []string{"../mine", "a/b", ""} {
		if _, err := Uninstall(configDir, name); err == nil || !strings.Contains(err.Error(), "is not a package name") {
			t.Errorf("%q: got %v", name, err)
		}
	}
}

func TestTouchKeepsAStagedFolderFromTheSweep(t *testing.T) {
	r := aliceToolsRepo(t)
	configDir := t.TempDir()
	staged, err := Stage(context.Background(), configDir, r.URL(), "v1")
	if err != nil {
		t.Fatal(err)
	}
	defer staged.Discard()
	now := time.Now()
	old := now.Add(-2 * staleAfter)
	if err := os.Chtimes(staged.temp, old, old); err != nil {
		t.Fatal(err)
	}
	if err := staged.Touch(now); err != nil {
		t.Fatal(err)
	}
	sweepStale(LocalDir(configDir), now)
	if _, err := os.Stat(staged.Dir); err != nil {
		t.Fatal("the sweep removed a folder in use")
	}
	_ = os.RemoveAll(staged.temp)
	if err := staged.Touch(now); err == nil || !strings.Contains(err.Error(), "swept away while waiting for your answer") {
		t.Fatalf("got %v", err)
	}
}

func TestStageRefusesControlCharacters(t *testing.T) {
	manifest := func(from, to string) func(r *gittest.Repo) {
		return func(r *gittest.Repo) {
			r.Write("package.yml", strings.Replace(aliceToolsManifest, from, to, 1), 0o644)
		}
	}
	cases := map[string]func(r *gittest.Repo){
		"escape in a task name":         func(r *gittest.Repo) { r.Write("tasks/x\r\x1b[2K.yml", "---\n[]\n", 0o644) },
		"escape in a file name":         func(r *gittest.Repo) { r.Write("templates/a\x1b[1A", "x", 0o644) },
		"escape in a script path":       func(r *gittest.Repo) { r.Write("bin/run\x1b[2J", "#!/bin/sh\n", 0o755) },
		"bidi override in a file name":  func(r *gittest.Repo) { r.Write("files/evil\u202ecod.yml", "x", 0o644) },
		"bidi isolate in a file name":   func(r *gittest.Repo) { r.Write("files/evil\u2066x", "x", 0o644) },
		"delete in a file name":         func(r *gittest.Repo) { r.Write("files/a\x7fb", "x", 0o644) },
		"C1 control in a file name":     func(r *gittest.Repo) { r.Write("files/a\u009bb", "x", 0o644) },
		"escape in the summary":         manifest("summary: Tools from alice.", `summary: "Tools\e[2J from alice."`),
		"newline in the summary":        manifest("summary: Tools from alice.", `summary: "Tools\nfrom alice."`),
		"bidi override in the summary":  manifest("summary: Tools from alice.", `summary: "Tools \u202efrom alice."`),
		"escape in a command name":      manifest("commands: [disk, help]", `commands: [disk, "help\e[1A"]`),
		"escape in a credential name":   manifest("{name: alice-token,", `{name: "alice-token\e[2K",`),
		"bidi isolate in a credential":  manifest("{name: alice-token,", `{name: "alice-token\u2069",`),
		"escape in a provider name":     manifest("providers:\n  disk:", "providers:\n  \"disk\\e[1A\":"),
		"escape in a needed package":    manifest("needs: [base]", `needs: ["base\e[2K"]`),
		"escape in a widget folder":     func(r *gittest.Repo) { r.Write("widgets/x\x1b[2K/widget.yml", aliceHealthWidget, 0o644) },
		"left-to-right mark in a name":  func(r *gittest.Repo) { r.Write("files/a\u200eb", "x", 0o644) },
		"right-to-left mark in summary": manifest("summary: Tools from alice.", `summary: "Tools \u200ffrom alice."`),
		"arabic letter mark in command": manifest("commands: [disk, help]", `commands: [disk, "help\u061c"]`),
	}
	for name, change := range cases {
		r := aliceToolsRepo(t)
		change(r)
		r.Commit("change")
		configDir := t.TempDir()
		_, err := Stage(context.Background(), configDir, r.URL(), "")
		if err == nil || !strings.Contains(err.Error(), "a character that moves or hides text in a terminal") {
			t.Errorf("%s: got %v", name, err)
			continue
		}
		if strings.ContainsAny(err.Error(), "\x1b\r\x7f\u009b\u202e\u2066\u2069") {
			t.Errorf("%s: the error itself holds the character: %q", name, err)
		}
		if debris := installDebris(t, configDir); len(debris) != 0 {
			t.Errorf("%s: left %v", name, debris)
		}
	}
}

func TestEscapeControl(t *testing.T) {
	for in, want := range map[string]string{
		"plain text, ünïcode":    "plain text, ünïcode",
		"a\x1b[2Jb":              `a\x1b[2Jb`,
		"a\r\nb\tc":              `a\x0d\x0ab\x09c`,
		"a\x7fb\u009bc":          `a\x7fb\x9bc`,
		"evil\u202ecod":          `evil\u202ecod`,
		"x\u2066y\u2069":         `x\u2066y\u2069`,
		"a\u200eb\u200fc\u061cd": `a\u200eb\u200fc\u061cd`,
		"a\xffb\xc3":             `a\xffb\xc3`,
	} {
		if got := EscapeControl(in); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
}

func TestSummaryListsTheRestOfTheRole(t *testing.T) {
	dir := comparePackage(t, aliceToolsManifest, map[string]string{
		"tasks/main.yml": "---\n[]\n", "handlers/main.yml": "---\n[]\n", "meta/main.yml": "---\n",
		"library/mod.py": "x", "module_utils/u.py": "x", "filter_plugins/f.py": "x", "lookup_plugins/l.py": "x",
		"templates/a.j2": "x", "files/a": "x", "vars/main.yml": "x", "defaults/main.yml": "x",
		"widgets/disk/widget.yml": aliceDiskWidget, "README.md": "x",
	}, nil)
	s, err := Summarize(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"defaults/main.yml", "files/a", "filter_plugins/f.py", "library/mod.py", "lookup_plugins/l.py",
		"meta/main.yml", "module_utils/u.py", "templates/a.j2", "vars/main.yml"}
	if !slices.Equal(s.RoleFiles, want) {
		t.Fatalf("got %q, want %q", s.RoleFiles, want)
	}
	if !slices.Equal(s.Tasks, []string{"handlers/main.yml", "tasks/main.yml"}) {
		t.Fatalf("got %q", s.Tasks)
	}
}

func TestCompareNamesChangedRoleFiles(t *testing.T) {
	before := comparePackage(t, aliceToolsManifest, map[string]string{
		"tasks/main.yml": "---\n[]\n", "library/mod.py": "old", "templates/old.j2": "x", "vars/main.yml": "same",
	}, nil)
	after := comparePackage(t, aliceToolsManifest, map[string]string{
		"tasks/main.yml": "---\n[]\n", "library/mod.py": "new", "meta/main.yml": "dependencies: [x]", "vars/main.yml": "same",
	}, nil)
	got, err := Compare(before, after)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got.RoleFilesAdded, []string{"meta/main.yml"}) || !slices.Equal(got.RoleFilesRemoved, []string{"templates/old.j2"}) ||
		!slices.Equal(got.RoleFilesChanged, []string{"library/mod.py"}) {
		t.Fatalf("got %+v", got)
	}
	if got.Empty() {
		t.Fatal("a changed module reads as unchanged")
	}
}

func TestReplaceLeavesNoOldCopyAndTheSweepTellsAFinishedSwapFromAFailedOne(t *testing.T) {
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
	r.Write("tasks/main.yml", "---\n- debug: {msg: hi}\n", 0o644)
	r.Commit("second")
	second, err := Stage(context.Background(), configDir, r.URL(), "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := second.Replace(configDir, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(second.temp, ".previous")); !os.IsNotExist(err) {
		t.Fatal("the old copy is still beside the new one after a swap that worked")
	}
	second.Discard()

	local := LocalDir(configDir)
	finished := filepath.Join(local, ".install-finished")
	failed := filepath.Join(local, ".install-failed")
	write(t, filepath.Join(finished, ".replaced", "package.yml"), "x")
	write(t, filepath.Join(failed, ".previous", "package.yml"), "x")
	old := time.Now().Add(-2 * staleAfter)
	for _, dir := range []string{finished, failed} {
		if err := os.Chtimes(dir, old, old); err != nil {
			t.Fatal(err)
		}
	}
	sweepStale(local, time.Now())
	if _, err := os.Stat(finished); !os.IsNotExist(err) {
		t.Fatal("the sweep kept what a finished swap left")
	}
	if _, err := os.Stat(failed); err != nil {
		t.Fatal("the sweep deleted an old copy that could not be put back")
	}
}

func TestHiddenTextRefusesInvalidUTF8(t *testing.T) {
	for in, want := range map[string]bool{"tasks/main.yml": false, "files/a\xffb": true, "files/\xc3": true, "files/ünï": false} {
		if got := hiddenText(in); got != want {
			t.Errorf("%q: got %v, want %v", in, got, want)
		}
	}
}

func TestStageRefusesAFileNameThatIsNotUTF8(t *testing.T) {
	probe := filepath.Join(t.TempDir(), "a\xffb")
	if err := os.WriteFile(probe, []byte("x"), 0o644); err != nil {
		t.Skipf("this file system refuses names that are not UTF-8: %v", err)
	}
	r := aliceToolsRepo(t)
	r.Write("files/a\xffb", "x", 0o644)
	r.Commit("a name that is not UTF-8")
	_, err := Stage(context.Background(), t.TempDir(), r.URL(), "")
	if err == nil || !strings.Contains(err.Error(), `files/a\xffb`) {
		t.Fatalf("got %v", err)
	}
}

func assertEscaped(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatal("no error")
	}
	if strings.ContainsFunc(err.Error(), func(r rune) bool { return r != '\n' && hidesText(r) }) {
		t.Fatalf("the error holds a control character: %q", err)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("the error lacks %q: %q", want, err)
	}
}

func TestInvalidPackageErrorEscapesEachLine(t *testing.T) {
	err := &InvalidPackageError{Where: "https://example.com/a\x1b", Problems: []Problem{
		{File: "tasks/x\x1b[2K.yml", What: "bad"}, {File: "package.yml", Line: 2, What: "summary \x1b[1A\r"},
	}}
	assertEscaped(t, err, `tasks/x\x1b[2K.yml: bad`)
	if lines := strings.Split(err.Error(), "\n"); len(lines) != 3 || !strings.Contains(lines[2], `summary \x1b[1A\x0d`) {
		t.Fatalf("got %q", lines)
	}
}

func TestStageEscapesALinkNameInItsError(t *testing.T) {
	r := aliceToolsRepo(t)
	r.Link("files/id\x1b[2K", filepath.Join(t.TempDir(), "secret"))
	r.Commit("a link outside")
	_, err := Stage(context.Background(), t.TempDir(), r.URL(), "")
	assertEscaped(t, err, `files/id\x1b[2K is a link`)
}

func TestStageEscapesAYAMLError(t *testing.T) {
	r := aliceToolsRepo(t)
	r.Write("package.yml", strings.Replace(aliceToolsManifest, "format: 1", `format: "1\e[2J"`, 1), 0o644)
	r.Commit("a format that is not a number")
	_, err := Stage(context.Background(), t.TempDir(), r.URL(), "")
	assertEscaped(t, err, "package.yml")
}

func TestStageEscapesWhatGitSays(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "no\x1b[2Jrepo")
	_, err := Stage(context.Background(), t.TempDir(), "file://"+missing, "")
	assertEscaped(t, err, `no\x1b[2Jrepo`)
}
