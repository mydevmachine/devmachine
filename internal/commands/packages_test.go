package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mydevmachine/devmachine/internal/config"
	"github.com/mydevmachine/devmachine/internal/packages"
)

func writeBrokenPackage(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "git")
	if err := os.MkdirAll(filepath.Join(dir, "tasks"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "package.yml"), []byte("format: 1\nname: git\nscope: user\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	body := "---\n- name: Install git\n  apt:\n    name: git\n"
	if err := os.WriteFile(filepath.Join(dir, "tasks", "main.yml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestPackagesValidatePrintsEveryProblemAtOnce(t *testing.T) {
	dir := writeBrokenPackage(t)

	out, err := execute(t, "packages", "validate", dir)
	if err == nil {
		t.Fatal("a broken package passed")
	}
	for _, want := range []string{"scope", "summary", "apt"} {
		if !strings.Contains(out, want) {
			t.Fatalf("the output leaves out %q: %s", want, out)
		}
	}
}

func TestPackagesValidateAsJSONCarriesEachProblemWithItsPlace(t *testing.T) {
	dir := writeBrokenPackage(t)

	out, _ := execute(t, "--format", "json", "packages", "validate", dir)
	var got struct {
		OK       bool `json:"ok"`
		Problems []struct {
			File string `json:"file"`
			Line int    `json:"line"`
			What string `json:"what"`
		} `json:"problems"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not JSON: %v (%q)", err, out)
	}
	if got.OK || len(got.Problems) == 0 {
		t.Fatalf("got %#v", got)
	}
}

func writePackageBorrowingAnAccount(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "notes")
	if err := packages.WriteSkeleton(dir, "notes", packages.ScopeWorkspace, ""); err != nil {
		t.Fatal(err)
	}
	body := "---\n- name: Write the notes folder\n  ansible.builtin.file:\n    path: \"{{ devmachine_account.home }}/notes\"\n    state: directory\n"
	if err := os.WriteFile(filepath.Join(dir, "tasks", "main.yml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestPackagesValidateWarnsButPassesOnABorrowedAccount(t *testing.T) {
	dir := writePackageBorrowingAnAccount(t)

	out, err := execute(t, "packages", "validate", dir)
	if err != nil {
		t.Fatalf("a warning failed validation: %v\n%s", err, out)
	}
	if !strings.Contains(out, "warning:") || !strings.Contains(out, "devmachine_account") {
		t.Fatalf("no warning about devmachine_account: %s", out)
	}
}

func TestPackagesValidateAsJSONCarriesWarningsApartFromProblems(t *testing.T) {
	dir := writePackageBorrowingAnAccount(t)

	out, err := execute(t, "--format", "json", "packages", "validate", dir)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		OK       bool               `json:"ok"`
		Problems []packages.Problem `json:"problems"`
		Warnings []packages.Problem `json:"warnings"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not JSON: %v (%q)", err, out)
	}
	if !got.OK || len(got.Problems) != 0 || len(got.Warnings) != 1 {
		t.Fatalf("got %#v", got)
	}
}

func TestPackagesValidateAcceptsAGoodPackage(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sharing")
	if err := packages.WriteSkeleton(dir, "sharing", packages.ScopeWorkspace, ""); err != nil {
		t.Fatal(err)
	}

	if _, err := execute(t, "packages", "validate", dir); err != nil {
		t.Fatalf("the skeleton did not pass: %v", err)
	}
}

func TestPackagesNewWritesSomethingValidateAccepts(t *testing.T) {
	into := t.TempDir()

	if _, err := execute(t, "packages", "new", "sharing", "--scope", "workspace", "--into", into); err != nil {
		t.Fatal(err)
	}
	if _, err := execute(t, "packages", "validate", filepath.Join(into, "sharing")); err != nil {
		t.Fatalf("what `packages new` wrote does not validate: %v", err)
	}
}

func TestPackagesNewRefusesToOverwrite(t *testing.T) {
	into := t.TempDir()

	if _, err := execute(t, "packages", "new", "sharing", "--scope", "workspace", "--into", into); err != nil {
		t.Fatal(err)
	}
	if _, err := execute(t, "packages", "new", "sharing", "--scope", "workspace", "--into", into); err == nil {
		t.Fatal("it overwrote a package that was already there")
	}
}

func TestPackagesSchemaJSONNamesEveryField(t *testing.T) {
	out, err := execute(t, "packages", "schema", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Formats []int `json:"formats"`
		Fields  []struct {
			Name     string `json:"name"`
			Required bool   `json:"required"`
			Summary  string `json:"summary"`
		} `json:"fields"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	for _, want := range []string{"format", "name", "scope", "summary", "needs", "provides", "extends", "credentials"} {
		found := false
		for _, f := range got.Fields {
			if f.Name == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("the schema leaves out %q", want)
		}
	}
	// Whoever writes a recipe has to know which format number to put at the
	// top, and asking the binary is the only answer that cannot drift.
	if !slices.Equal(got.Formats, packages.ReadableFormats) {
		t.Fatalf("the schema does not publish the formats it reads: %#v", got.Formats)
	}
}

func TestPackagesSchemaAsATablePrintsTheFormatAndTheFields(t *testing.T) {
	out, err := execute(t, "packages", "schema")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"format", "scope", "required"} {
		if !strings.Contains(out, want) {
			t.Fatalf("the table leaves out %q: %s", want, out)
		}
	}
}

func configWithMachineAndWorkspace(t *testing.T) string {
	t.Helper()
	return configWith(t, "machines:\n  - name: main\n    hosts: [203.0.113.10]\nworkspaces:\n  - name: alice\n")
}

// writeLocalPackage puts a package in the operator's own directory, which is
// where a configuration with no release pin finds everything it has.
func writeLocalPackage(t *testing.T, configDir, name, scope string) {
	t.Helper()
	if err := packages.WriteSkeleton(filepath.Join(packages.LocalDir(configDir), name), name, scope, ""); err != nil {
		t.Fatal(err)
	}
}

func configWithPackagesInstalled(t *testing.T) string {
	t.Helper()
	dir := configWith(t, `
machines:
  - name: main
    hosts: [203.0.113.10]
    packages: [docker]
workspaces:
  - name: alice
    packages: [claude-code]
`)
	writeLocalPackage(t, dir, "docker", packages.ScopeMachine)
	writeLocalPackage(t, dir, "claude-code", packages.ScopeWorkspace)
	writeLocalPackage(t, dir, "caddy", packages.ScopeMachine)
	return dir
}

func TestPackagesAddPutsItOnTheNamedWorkspace(t *testing.T) {
	dir := configWithMachineAndWorkspace(t)

	if _, err := execute(t, "--config", dir, "packages", "add", "claude-code", "--workspace", "alice", "--yes"); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Workspaces[0].Packages) != 1 || cfg.Workspaces[0].Packages[0] != "claude-code" {
		t.Fatalf("got %#v", cfg.Workspaces[0].Packages)
	}
	if len(cfg.Machines[0].Packages) != 0 {
		t.Fatalf("it also touched the machine: %#v", cfg.Machines[0].Packages)
	}
}

func TestPackagesAddRefusesTwoTargets(t *testing.T) {
	dir := configWithMachineAndWorkspace(t)

	_, err := execute(t, "--config", dir, "packages", "add", "docker", "--workspace", "alice", "--machine", "main", "--yes")
	if err == nil {
		t.Fatal("two targets were accepted")
	}
	if !strings.Contains(err.Error(), "--workspace") || !strings.Contains(err.Error(), "--machine") {
		t.Fatalf("the error does not say what to choose between: %v", err)
	}
}

func TestPackagesAddDefaultsToTheOnlyMachine(t *testing.T) {
	dir := configWithMachineAndWorkspace(t)

	if _, err := execute(t, "--config", dir, "packages", "add", "docker", "--yes"); err != nil {
		t.Fatal(err)
	}

	cfg, _ := config.Load(dir)
	if len(cfg.Machines[0].Packages) != 1 || cfg.Machines[0].Packages[0] != "docker" {
		t.Fatalf("got %#v", cfg.Machines[0].Packages)
	}
}

func TestPackagesAddSaysNothingChangedWhenItIsAlreadyThere(t *testing.T) {
	dir := configWithMachineAndWorkspace(t)

	if _, err := execute(t, "--config", dir, "packages", "add", "docker", "--machine", "main", "--yes"); err != nil {
		t.Fatal(err)
	}
	out, err := execute(t, "--config", dir, "packages", "add", "docker", "--machine", "main", "--yes")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "already") {
		t.Fatalf("got %q", out)
	}

	cfg, _ := config.Load(dir)
	if len(cfg.Machines[0].Packages) != 1 {
		t.Fatalf("it was added twice: %#v", cfg.Machines[0].Packages)
	}
}

func TestPackagesAddWithCheckChangesNothing(t *testing.T) {
	dir := configWithMachineAndWorkspace(t)

	if _, err := execute(t, "--config", dir, "packages", "add", "docker", "--machine", "main", "--check"); err != nil {
		t.Fatal(err)
	}

	cfg, _ := config.Load(dir)
	if len(cfg.Machines[0].Packages) != 0 {
		t.Fatalf("a dry run wrote to the configuration: %#v", cfg.Machines[0].Packages)
	}
}

func TestPackagesAddKeepsTheCommentsAroundIt(t *testing.T) {
	dir := configWith(t, "# The main server.\nmachines:\n  - name: main\n    hosts: [203.0.113.10]\n")

	if _, err := execute(t, "--config", dir, "packages", "add", "docker", "--yes"); err != nil {
		t.Fatal(err)
	}

	body, err := os.ReadFile(filepath.Join(dir, config.FileName))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "# The main server.") {
		t.Fatalf("the comment was lost:\n%s", body)
	}
}

func TestPackagesRmTakesItOffTheTarget(t *testing.T) {
	dir := configWithPackagesInstalled(t)

	if _, err := execute(t, "--config", dir, "packages", "rm", "claude-code", "--workspace", "alice", "--yes"); err != nil {
		t.Fatal(err)
	}

	cfg, _ := config.Load(dir)
	if len(cfg.Workspaces[0].Packages) != 0 {
		t.Fatalf("got %#v", cfg.Workspaces[0].Packages)
	}
}

func TestPackagesRmSaysWhenItWasNotThere(t *testing.T) {
	dir := configWithMachineAndWorkspace(t)

	out, err := execute(t, "--config", dir, "packages", "rm", "docker", "--machine", "main", "--yes")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "not") {
		t.Fatalf("got %q", out)
	}
}

func TestPackagesAddNamesTheWorkspacesThatExist(t *testing.T) {
	dir := configWithMachineAndWorkspace(t)

	_, err := execute(t, "--config", dir, "packages", "add", "docker", "--workspace", "carol", "--yes")
	if err == nil {
		t.Fatal("an unknown workspace was accepted")
	}
	if !strings.Contains(err.Error(), "alice") {
		t.Fatalf("the error does not list the ones that exist: %v", err)
	}
}

func TestPackagesListShowsWhereEachOneIsInstalled(t *testing.T) {
	dir := configWithPackagesInstalled(t)

	out, err := execute(t, "--config", dir, "--format", "json", "packages", "list")
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Packages []struct {
			Name      string   `json:"name"`
			Scope     string   `json:"scope"`
			Source    string   `json:"source"`
			Installed []string `json:"installed_on"`
		} `json:"packages"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not JSON: %v (%q)", err, out)
	}
	if len(got.Packages) != 3 {
		t.Fatalf("got %#v", got.Packages)
	}

	where := map[string][]string{}
	for _, p := range got.Packages {
		where[p.Name] = p.Installed
	}
	if len(where["docker"]) != 1 || where["docker"][0] != "machine main" {
		t.Fatalf("docker is installed on %#v", where["docker"])
	}
	if len(where["claude-code"]) != 1 || where["claude-code"][0] != "workspace alice" {
		t.Fatalf("claude-code is installed on %#v", where["claude-code"])
	}
	if len(where["caddy"]) != 0 {
		t.Fatalf("caddy is on nothing, and the list says %#v", where["caddy"])
	}
}

func TestPackagesListAsATableNamesEachPackageOnce(t *testing.T) {
	dir := configWithPackagesInstalled(t)

	out, err := execute(t, "--config", dir, "packages", "list")
	if err != nil {
		t.Fatal(err)
	}
	rows := 0
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "claude-code") {
			rows++
		}
	}
	if rows != 1 {
		t.Fatalf("claude-code is on %d rows:\n%s", rows, out)
	}
	for _, want := range []string{"docker", "machine main", "workspace alice", "local"} {
		if !strings.Contains(out, want) {
			t.Fatalf("the table leaves out %q:\n%s", want, out)
		}
	}
}

func TestPackagesListMarksAConfiguredPackageThatDoesNotExist(t *testing.T) {
	dir := configWith(t, "machines:\n  - name: main\n    hosts: [203.0.113.10]\n    packages: [ghost]\n")

	out, err := execute(t, "--config", dir, "packages", "list")
	if err != nil {
		t.Fatal(err)
	}
	// Dropping it silently would leave `sync` to be the thing that finds out.
	if !strings.Contains(out, "ghost") || !strings.Contains(out, "missing") {
		t.Fatalf("a configured package nothing provides is not reported:\n%s", out)
	}
}

func TestPackagesHelpAsksThePackage(t *testing.T) {
	dir := configDirWithProvider(t, "cloudflare", `
import json
print(json.dumps({"commands": [{"name": "zones", "summary": "The zones this token can see."}]}))
`)
	dialLocal(t, dir)

	out, err := execute(t, "--config", dir, "packages", "help", "cloudflare")
	if err != nil {
		t.Fatal(err)
	}
	// The package is the source of truth about itself. Nothing here is
	// written in the CLI or in a document somebody has to keep in sync.
	if !strings.Contains(out, "The zones this token can see.") {
		t.Fatalf("got %q", out)
	}
}

func TestPackagesHelpOnAPackageThatCannotBeAskedSaysSo(t *testing.T) {
	dialing(t, fakeRemote{})
	dir := configDirWithPlainPackage(t, "docker")

	_, err := execute(t, "--config", dir, "packages", "help", "docker")
	if err == nil {
		t.Fatal("a machine package answered help")
	}
	// A package that declares no entrypoint has nothing to ask.
	if !strings.Contains(err.Error(), "entrypoint") {
		t.Fatalf("the error does not say why: %v", err)
	}
}

func TestPackagesHelpJSONIsTheStableContract(t *testing.T) {
	dir := configDirWithProvider(t, "cloudflare", `
import json
print(json.dumps({"commands": [{"name": "zones", "summary": "x", "args": ""}]}))
`)
	dialLocal(t, dir)

	out, err := execute(t, "--config", dir, "packages", "help", "cloudflare", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Name     string `json:"name"`
		Commands []struct {
			Name    string `json:"name"`
			Summary string `json:"summary"`
		} `json:"commands"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if got.Name != "cloudflare" || len(got.Commands) != 1 {
		t.Fatalf("got %#v", got)
	}
}

func TestPackagesRmRefusesToRemoveTheAccount(t *testing.T) {
	dir := configWithPackagesInstalled(t)

	_, err := execute(t, "--config", dir, "packages", "rm", "workspace", "--workspace", "alice", "--yes")
	if err == nil || !strings.Contains(err.Error(), "creates the account") {
		t.Fatalf("got %v, want a refusal that says why", err)
	}
}

func TestPackagesListSaysWhichPackageIsADNSProviderAndWhatItNeeds(t *testing.T) {
	dir := configDir(t)
	writeDNSPackage(t, dir, "hostinger", nil, "print('ok')")
	pkgDir := filepath.Join(packages.LocalDir(dir), "caddy")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := "format: 1\nname: caddy\nscope: machine\nsummary: A web server.\ncategory: web\n"
	if err := os.WriteFile(packages.ManifestPath(pkgDir), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := execute(t, "--config", dir, "--format", "json", "packages", "list")
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Packages []map[string]any `json:"packages"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not JSON: %v (%q)", err, out)
	}
	byName := map[string]map[string]any{}
	for _, p := range got.Packages {
		byName[p["name"].(string)] = p
	}

	dns := byName["hostinger"]
	if dns["kind"] != "dns" {
		t.Fatalf("the provider does not say it is one: %#v", dns)
	}
	creds, ok := dns["credentials"].([]any)
	if !ok || len(creds) != 1 {
		t.Fatalf("the credentials it declares are not listed: %#v", dns)
	}
	want := map[string]any{"name": "hostinger", "kind": "secret", "env": "HOSTINGER_TOKEN"}
	cred := creds[0].(map[string]any)
	for key, value := range want {
		if cred[key] != value {
			t.Fatalf("credential %s: got %#v, want %q", key, cred[key], value)
		}
	}
	if _, leaked := cred["value"]; leaked {
		t.Fatalf("a credential carries a value: %#v", cred)
	}

	web := byName["caddy"]
	if web["category"] != "web" {
		t.Fatalf("the category is missing: %#v", web)
	}
	if _, ok := web["kind"]; ok {
		t.Fatalf("a package with no kind reports one: %#v", web)
	}
	if creds, ok := web["credentials"].([]any); !ok || len(creds) != 0 {
		t.Fatalf("a package with no credentials should list an empty array: %#v", web["credentials"])
	}
}

func TestPackagesListSaysWhereAPackageRunsAndWhatItBringsIn(t *testing.T) {
	dir := configDir(t)
	for name, manifest := range map[string]string{
		"mac-brew":   "format: 1\nname: mac-brew\nscope: machine\nsummary: Homebrew.\nplatforms: [macos]\n",
		"essentials": "format: 1\nname: essentials\nscope: machine\nsummary: The basics.\nneeds: [base, git]\n",
	} {
		pkgDir := filepath.Join(packages.LocalDir(dir), name)
		if err := os.MkdirAll(pkgDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(packages.ManifestPath(pkgDir), []byte(manifest), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	out, err := execute(t, "--config", dir, "--format", "json", "packages", "list")
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Packages []struct {
			Name      string   `json:"name"`
			Platforms []string `json:"platforms"`
			Needs     []string `json:"needs"`
		} `json:"packages"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not JSON: %v (%q)", err, out)
	}
	byName := map[string]int{}
	for i, p := range got.Packages {
		byName[p.Name] = i
	}
	mac := got.Packages[byName["mac-brew"]]
	if !slices.Equal(mac.Platforms, []string{"macos"}) || mac.Needs == nil || len(mac.Needs) != 0 {
		t.Fatalf("mac-brew: %s", out)
	}
	essentials := got.Packages[byName["essentials"]]
	if !slices.Equal(essentials.Needs, []string{"base", "git"}) ||
		essentials.Platforms == nil || len(essentials.Platforms) != 0 {
		t.Fatalf("essentials: %s", out)
	}
}

func TestPackagesListReportsEachVariable(t *testing.T) {
	dir := configDir(t)
	pkgDir := filepath.Join(packages.LocalDir(dir), "zsh")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := "format: 1\nname: zsh\nscope: workspace\nsummary: A shell.\nvariables:\n" +
		"  theme:\n    summary: The prompt theme.\n    default: plain\n" +
		"  plugins:\n    summary: Plugins to load.\n    default: [git]\n" +
		"  history:\n    summary: Lines kept.\n    default: 1000\n" +
		"  vi_mode:\n    summary: Vi keys.\n    default: false\n" +
		"  token:\n    summary: No default.\n"
	if err := os.WriteFile(packages.ManifestPath(pkgDir), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	writeLocalPackage(t, dir, "docker", packages.ScopeMachine)

	out, err := execute(t, "--config", dir, "--format", "json", "packages", "list")
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Packages []struct {
			Name      string           `json:"name"`
			Variables []map[string]any `json:"variables"`
		} `json:"packages"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not JSON: %v (%q)", err, out)
	}
	byName := map[string][]map[string]any{}
	for _, p := range got.Packages {
		byName[p.Name] = p.Variables
	}
	if v := byName["docker"]; v == nil || len(v) != 0 {
		t.Fatalf("a package with no variables should list []: %s", out)
	}
	vars := byName["zsh"]
	names := make([]string, 0, len(vars))
	for _, v := range vars {
		names = append(names, v["name"].(string))
	}
	if !slices.Equal(names, []string{"history", "plugins", "theme", "token", "vi_mode"}) {
		t.Fatalf("variables are not listed by name: %v", names)
	}
	want := map[string]struct {
		def  any
		kind string
	}{
		"history": {float64(1000), "number"},
		"plugins": {[]any{"git"}, "list"},
		"theme":   {"plain", "string"},
		"token":   {nil, ""},
		"vi_mode": {false, "boolean"},
	}
	for _, v := range vars {
		w := want[v["name"].(string)]
		if fmt.Sprint(v["default"]) != fmt.Sprint(w.def) {
			t.Fatalf("%s default = %#v, want %#v", v["name"], v["default"], w.def)
		}
		kind, _ := v["type"].(string)
		if kind != w.kind {
			t.Fatalf("%s type = %q, want %q", v["name"], kind, w.kind)
		}
		if v["summary"] == "" {
			t.Fatalf("%s has no summary", v["name"])
		}
	}
}

func TestPackagesListSaysWhichLoginCanBeSharedFromTheMachine(t *testing.T) {
	dir := configDir(t)
	pkgDir := filepath.Join(packages.LocalDir(dir), "agent")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := "format: 1\nname: agent\nscope: workspace\nsummary: A coding agent.\ncredentials:\n" +
		"  - name: copies\n    kind: manual\n    scope: machine\n    shareable: true\n" +
		"    command: agent login\n    stored_at: .agent/session.json\n" +
		"  - name: bound\n    kind: manual\n    scope: workspace\n" +
		"    command: other login\n    stored_at: .other/token\n" +
		"  - name: key\n    kind: secret\n    scope: workspace\n    env: AGENT_KEY\n"
	if err := os.WriteFile(packages.ManifestPath(pkgDir), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := execute(t, "--config", dir, "--format", "json", "packages", "list")
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Packages []struct {
			Name        string           `json:"name"`
			Credentials []map[string]any `json:"credentials"`
		} `json:"packages"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not JSON: %v (%q)", err, out)
	}
	shareable := map[string]any{}
	for _, p := range got.Packages {
		if p.Name != "agent" {
			continue
		}
		for _, c := range p.Credentials {
			shareable[c["name"].(string)] = c["shareable"]
		}
	}
	want := map[string]any{"copies": true, "bound": false, "key": false}
	for name, value := range want {
		if shareable[name] != value {
			t.Fatalf("%s shareable = %#v, want %v: %s", name, shareable[name], value, out)
		}
	}
}
