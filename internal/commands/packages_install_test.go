package commands

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mydevmachine/devmachine/internal/gittest"
	"github.com/mydevmachine/devmachine/internal/packages"
	"github.com/mydevmachine/devmachine/internal/widgets"
)

const aliceToolsManifest = `format: 1
name: alice-tools
scope: machine
summary: Tools from alice.
requires: {cli: ">= 0.9.0"}
entrypoint: bin/alice-tools
commands: [disk, help]
providers:
  disk:
    returns: {used_percent: number}
    min_every: 10s
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

func allowFileAddresses(t *testing.T) {
	t.Helper()
	orig := parseGitAddress
	parseGitAddress = func(arg string) (string, string, error) {
		if rest, ok := strings.CutPrefix(arg, "file://"); ok {
			address, ref, _ := strings.Cut(rest, "@")
			return "file://" + address, ref, nil
		}
		return orig(arg)
	}
	t.Cleanup(func() { parseGitAddress = orig })
}

func aliceToolsRepo(t *testing.T, name string) *gittest.Repo {
	t.Helper()
	r := gittest.New(t)
	r.Write("package.yml", strings.Replace(aliceToolsManifest, "name: alice-tools", "name: "+name, 1), 0o644)
	r.Write("tasks/main.yml", "---\n[]\n", 0o644)
	r.Write("bin/alice-tools", "#!/usr/bin/env python3\n", 0o755)
	r.Write("widgets/disk/widget.yml", strings.ReplaceAll(aliceDiskWidget, "alice-tools/", name+"/"), 0o644)
	r.Commit("first")
	r.Tag("v1")
	return r
}

func installConfig(t *testing.T) string {
	t.Helper()
	dir := widgetConfig(t)
	allowFileAddresses(t)
	return dir
}

func assertNoInstallDebris(t *testing.T, dir string, name string) {
	t.Helper()
	entries, _ := os.ReadDir(packages.LocalDir(dir))
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".install-") || e.Name() == name {
			t.Fatalf("left %s in %s", e.Name(), packages.LocalDir(dir))
		}
	}
}

func TestPackagesInstallFetchesValidatesAndRecordsTheSource(t *testing.T) {
	dir := installConfig(t)
	r := aliceToolsRepo(t, "alice-tools")
	out, err := executeWithInput(t, "y\n", "--config", dir, "packages", "install", r.URL()+"@v1")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, want := range []string{
		"alice-tools (machine) — Tools from alice.",
		"from " + r.URL() + " at v1 (commit " + r.Head()[:12] + ")",
		"widgets: alice-tools/disk (provider alice-tools/disk, runs code)",
		"commands: disk, help", "providers: disk", "scripts: bin/alice-tools", "tasks: tasks/main.yml",
		"Install alice-tools into " + filepath.Join(packages.LocalDir(dir), "alice-tools") + "? [y/N]",
		"installed alice-tools",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	pkg := filepath.Join(packages.LocalDir(dir), "alice-tools")
	origin, ok, err := packages.ReadOrigin(pkg)
	if err != nil || !ok || origin.URL != r.URL() || origin.Ref != "v1" || origin.Commit != r.Head() {
		t.Fatalf("%+v %v %v", origin, ok, err)
	}
	if _, err := time.Parse(time.RFC3339, origin.InstalledAt); err != nil {
		t.Fatalf("installed_at %q: %v", origin.InstalledAt, err)
	}
	if _, err := os.Stat(filepath.Join(pkg, ".git")); !os.IsNotExist(err) {
		t.Fatal("the package kept its .git folder")
	}
	assertNoInstallDebris(t, dir, "")
}

func TestPackagesInstallWithoutYesAsksAndANoChangesNothing(t *testing.T) {
	dir := installConfig(t)
	r := aliceToolsRepo(t, "alice-tools")
	for _, answer := range []string{"n\n", ""} {
		_, err := executeWithInput(t, answer, "--config", dir, "packages", "install", r.URL())
		if !errors.Is(err, errDeclined) {
			t.Fatalf("answer %q: got %v", answer, err)
		}
		assertNoInstallDebris(t, dir, "alice-tools")
	}
}

func TestPackagesInstallCheckChangesNothing(t *testing.T) {
	dir := installConfig(t)
	r := aliceToolsRepo(t, "alice-tools")
	out, err := execute(t, "--config", dir, "--format", "json", "packages", "install", r.URL(), "--check")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	var got struct {
		Name      string `json:"name"`
		URL       string `json:"url"`
		Commit    string `json:"commit"`
		Installed bool   `json:"installed"`
		Widgets   []struct {
			Name     string `json:"name"`
			RunsCode bool   `json:"runs_code"`
		} `json:"widgets"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if got.Name != "alice-tools" || got.Installed || got.Commit != r.Head() || len(got.Widgets) != 1 || !got.Widgets[0].RunsCode {
		t.Fatalf("got %+v", got)
	}
	assertNoInstallDebris(t, dir, "alice-tools")
}

func TestPackagesInstallJSONNeedsYesOrCheck(t *testing.T) {
	dir := installConfig(t)
	r := aliceToolsRepo(t, "alice-tools")
	_, err := execute(t, "--config", dir, "--format", "json", "packages", "install", r.URL())
	if err == nil || !strings.Contains(err.Error(), "--format json cannot ask: add --check to see what it brings, or --yes to install") {
		t.Fatalf("got %v", err)
	}
}

func TestPackagesInstallRefuses(t *testing.T) {
	cases := []struct {
		name    string
		repo    func(t *testing.T) string
		before  func(t *testing.T, dir string)
		leftOut string
		want    string
	}{
		{"official name", func(t *testing.T) string { return aliceToolsRepo(t, "claude-code").URL() }, nil, "",
			"claude-code is an official package: a package from a git address cannot take its name"},
		{"your own package", func(t *testing.T) string { return aliceToolsRepo(t, "mine").URL() },
			func(t *testing.T, dir string) { writeProviderPackage(t, dir, "mine") }, "",
			"you already have your own package mine in "},
		{"invalid package", func(t *testing.T) string {
			r := aliceToolsRepo(t, "alice-tools")
			r.Remove("tasks/main.yml")
			r.Commit("no tasks")
			return r.URL()
		}, nil, "alice-tools", "has 1 problem(s)"},
		{"no package at the top", func(t *testing.T) string {
			r := aliceToolsRepo(t, "alice-tools")
			r.Remove("package.yml")
			r.Commit("no manifest")
			return r.URL()
		}, nil, "alice-tools", "has no package.yml at its top"},
		{"link outside", func(t *testing.T) string {
			r := aliceToolsRepo(t, "alice-tools")
			outside := filepath.Join(t.TempDir(), "id_ed25519")
			writeCommandFile(t, outside, "secret\n")
			r.Link("files/id", outside)
			r.Commit("a link")
			return r.URL()
		}, nil, "alice-tools", "files/id is a link leading outside the package"},
		{"a repository that is not there", func(t *testing.T) string { return "file://" + filepath.Join(t.TempDir(), "missing.git") },
			nil, "alice-tools", "fetching file://"},
		{"a ref that is not there", func(t *testing.T) string { return aliceToolsRepo(t, "alice-tools").URL() + "@v9" },
			nil, "alice-tools", "fetching file://"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := installConfig(t)
			if tc.before != nil {
				tc.before(t, dir)
			}
			out, err := execute(t, "--config", dir, "packages", "install", tc.repo(t), "--yes")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q, got %v\n%s", tc.want, err, out)
			}
			assertNoInstallDebris(t, dir, tc.leftOut)
		})
	}
}

func TestPackagesInstallRefusesAPackageAlreadyInstalled(t *testing.T) {
	dir := installConfig(t)
	r := aliceToolsRepo(t, "alice-tools")
	if out, err := execute(t, "--config", dir, "packages", "install", r.URL(), "--yes"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	_, err := execute(t, "--config", dir, "packages", "install", r.URL(), "--yes")
	if err == nil || !strings.Contains(err.Error(), "alice-tools is already installed from "+r.URL()+": update it with devmachine packages update alice-tools") {
		t.Fatalf("got %v", err)
	}
}

func TestPackagesInstallRefusesAnInstalledPackageWithABrokenSourceFile(t *testing.T) {
	dir := installConfig(t)
	r := aliceToolsRepo(t, "alice-tools")
	if out, err := execute(t, "--config", dir, "packages", "install", r.URL(), "--yes"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	writeCommandFile(t, filepath.Join(packages.LocalDir(dir), "alice-tools", packages.OriginFile), "url: [\n")
	_, err := execute(t, "--config", dir, "packages", "install", r.URL(), "--yes")
	if err == nil || !strings.Contains(err.Error(), "alice-tools is already installed from a git address") ||
		!strings.Contains(err.Error(), "update it with devmachine packages update alice-tools") {
		t.Fatalf("got %v", err)
	}
}

func TestPackagesInstallRefusesAnAddressThatIsNeitherHTTPSNorGit(t *testing.T) {
	dir := widgetConfig(t)
	for _, arg := range []string{"http://example.com/a.git", "file:///tmp/a", "/tmp/a", "ext::sh -c id"} {
		_, err := execute(t, "--config", dir, "packages", "install", arg, "--yes")
		if err == nil || !strings.Contains(err.Error(), "an https:// or git@ address") {
			t.Errorf("%s: got %v", arg, err)
		}
	}
}

func TestAnInstalledPackagesWidgetIsThirdParty(t *testing.T) {
	dir := installConfig(t)
	r := aliceToolsRepo(t, "alice-tools")
	if out, err := execute(t, "--config", dir, "packages", "install", r.URL(), "--yes"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	catalog := listCatalog(t, dir)
	disk, ok := catalog.Find("alice-tools/disk")
	if !ok || disk.Trust != widgets.TrustThirdParty || disk.PackageSource == nil || disk.PackageSource.Commit != r.Head() {
		t.Fatalf("got %+v", disk)
	}
	if catalog.Providers["alice-tools/disk"].Trust != widgets.TrustThirdParty {
		t.Fatalf("got %+v", catalog.Providers)
	}
}

func TestPackagesInstallCheckWithYesInstallsNothing(t *testing.T) {
	dir := installConfig(t)
	r := aliceToolsRepo(t, "alice-tools")
	out, err := execute(t, "--config", dir, "packages", "install", r.URL(), "--check", "--yes")
	if err != nil || strings.Contains(out, "installed alice-tools") {
		t.Fatalf("%v\n%s", err, out)
	}
	assertNoInstallDebris(t, dir, "alice-tools")
}

func TestPackagesInstallRefusesWhenTheReleaseCannotBeRead(t *testing.T) {
	dir := installConfig(t)
	r := aliceToolsRepo(t, "alice-tools")
	if err := os.MkdirAll(filepath.Join(packages.CacheDir(dir, "v40"), "packages", "alice-tools", packages.FileName), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := execute(t, "--config", dir, "packages", "install", r.URL(), "--yes")
	if err == nil || !strings.Contains(err.Error(), "checking whether alice-tools is an official package: ") {
		t.Fatalf("got %v", err)
	}
	assertNoInstallDebris(t, dir, "alice-tools")
}
