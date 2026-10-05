package provision

import (
	"errors"
	"flag"
	"fmt"
	"maps"
	"os"
	osuser "os/user"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/mydevmachine/devmachine/internal/config"
	"github.com/mydevmachine/devmachine/internal/packages"
	"gopkg.in/yaml.v3"
)

var update = flag.Bool("update", false, "rewrite the golden files")

func assertOrderInText(t *testing.T, text string, want ...string) {
	t.Helper()
	at := -1
	for _, s := range want {
		next := strings.Index(text, s)
		if next < 0 {
			t.Fatalf("%q is missing from:\n%s", s, text)
		}
		if next < at {
			t.Fatalf("%q comes too early in:\n%s", s, text)
		}
		at = next
	}
}

func TestGenerateWritesAnsibleCfgWithBothRolePaths(t *testing.T) {
	files, err := Generate(planWith(t, "main", []string{"base"}, nil))
	if err != nil {
		t.Fatal(err)
	}
	cfg := string(files["ansible.cfg"])
	// The operator's own packages come first. That is the whole overlay
	// mechanism; there is no separate concept.
	if !strings.Contains(cfg, "roles_path = /opt/devmachine/roles.local:/opt/devmachine/roles") {
		t.Fatalf("got:\n%s", cfg)
	}
}

func TestGenerateWritesALocalInventory(t *testing.T) {
	files, err := Generate(planWith(t, "main", []string{"base"}, nil))
	if err != nil {
		t.Fatal(err)
	}
	inventory := string(files["inventory.ini"])
	if !strings.Contains(inventory, "devmachine ansible_connection=local") {
		t.Fatalf("got:\n%s", inventory)
	}
	// A group named after the host makes Ansible warn on every single run.
	if strings.Contains(inventory, "[devmachine]") {
		t.Fatalf("the host has a group of its own name:\n%s", inventory)
	}
}

func TestGeneratePlaybookRunsMachinePackagesInOrder(t *testing.T) {
	plan := planWith(t, "main", []string{"caddy"}, nil) // caddy needs firewall needs base
	files, err := Generate(plan)
	if err != nil {
		t.Fatal(err)
	}
	assertOrderInText(t, string(files["site.yml"]), "base", "firewall", "caddy")
}

func TestGeneratePlaybookLoopsAWorkspacePackageOverItsWorkspacesOnly(t *testing.T) {
	plan := planWith(t, "main", nil, map[string][]string{
		"alice": {"claude-code"},
		"bob":   {},
	})
	files, err := Generate(plan)
	if err != nil {
		t.Fatal(err)
	}
	playbook := string(files["site.yml"])
	if !strings.Contains(playbook, "alice") {
		t.Fatalf("alice is missing:\n%s", playbook)
	}
	if strings.Contains(playbook, "bob") {
		t.Fatalf("bob declares nothing and should not appear:\n%s", playbook)
	}
}

func TestGenerateNamesTheLoopVariableRatherThanUsingItem(t *testing.T) {
	plan := planWith(t, "main", nil, map[string][]string{"alice": {"claude-code"}})
	files, err := Generate(plan)
	if err != nil {
		t.Fatal(err)
	}
	playbook := string(files["site.yml"])

	// A role is free to have loops of its own, and any of them rebinds `item`.
	// A workspace package written that way would silently act on whatever the
	// inner loop was iterating — it fails with "'AnsibleUnsafeText' object has
	// no attribute 'user'", which names nothing useful.
	if !strings.Contains(playbook, "loop_var: devmachine_workspace") {
		t.Fatalf("the loop variable is not named:\n%s", playbook)
	}
	if strings.Contains(playbook, `devmachine_workspace: "{{ item }}"`) {
		t.Fatalf("the workspace is still bound through item:\n%s", playbook)
	}
}

func TestGenerateTagsEveryPackageWithItsOwnName(t *testing.T) {
	files, err := Generate(planWith(t, "main", []string{"docker"}, nil))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(files["site.yml"]), "tags: [docker]") {
		t.Fatalf("got:\n%s", files["site.yml"])
	}
}

// Without the apply block the inner tasks of a dynamic include_role do not
// inherit the tag, so `--tags docker` includes the role and then skips
// everything in it.
func TestGenerateAppliesTheTagInsideEveryIncludedRole(t *testing.T) {
	plan := planWith(t, "main", []string{"caddy"}, map[string][]string{"alice": {"claude-code"}})
	files, err := Generate(plan)
	if err != nil {
		t.Fatal(err)
	}

	for _, task := range tasksIn(t, files["site.yml"]) {
		include, ok := task["include_role"].(map[string]any)
		if !ok {
			continue
		}
		apply, ok := include["apply"].(map[string]any)
		if !ok {
			t.Fatalf("the role %v is included with no apply block: %#v", include["name"], task)
		}
		if !reflect.DeepEqual(apply["tags"], task["tags"]) {
			t.Fatalf("the role %v applies %#v but is tagged %#v", include["name"], apply["tags"], task["tags"])
		}
	}
}

// Every shipped recipe reads ansible_facts, so a playbook that turns fact
// gathering off breaks all of them.
func TestGenerateLeavesFactGatheringOn(t *testing.T) {
	files, err := Generate(planWith(t, "main", []string{"base"}, nil))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(files["site.yml"]), "gather_facts") {
		t.Fatalf("the playbook says something about facts:\n%s", files["site.yml"])
	}
}

func TestGenerateWritesAnExtensionAsACopyIntoTheProvidedPath(t *testing.T) {
	plan := planWithExtension(t) // sharing extends caddy.sites.d
	files, err := Generate(plan)
	if err != nil {
		t.Fatal(err)
	}
	playbook := string(files["site.yml"])
	if !strings.Contains(playbook, "/etc/caddy/sites.d") {
		t.Fatalf("the extension does not write where caddy said:\n%s", playbook)
	}
	// An extension runs after the package it extends, or it writes into a
	// directory that does not exist yet.
	assertOrderInText(t, playbook, "caddy", "/etc/caddy/sites.d")
}

// Two workspaces installing the same package contribute the same file name,
// and one silently overwriting the other is the kind of defect nobody finds.
func TestGenerateKeepsTwoWorkspaceExtensionsApart(t *testing.T) {
	plan := planWith(t, "main", []string{"caddy"}, map[string][]string{
		"alice": {"sharing"},
		"bob":   {"sharing"},
	})
	files, err := Generate(plan)
	if err != nil {
		t.Fatal(err)
	}

	var destinations []string
	for _, task := range tasksIn(t, files["site.yml"]) {
		if copyTask, ok := task["copy"].(map[string]any); ok {
			destinations = append(destinations, copyTask["dest"].(string))
		}
	}
	if len(destinations) != 2 {
		t.Fatalf("got %v", destinations)
	}
	if destinations[0] == destinations[1] {
		t.Fatalf("both workspaces write to %s", destinations[0])
	}
}

// The source of an extension follows the copy that won, or a local override
// would run its own tasks and ship the published package's file.
func TestGenerateReadsAnExtensionFromTheCopyThatWon(t *testing.T) {
	store := storeWithCatalogue(t, "sharing")
	plan := planFor(t, store, "main", []string{"caddy"}, map[string][]string{"alice": {"sharing"}})

	files, err := Generate(plan)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(files["site.yml"]), "/opt/devmachine/roles.local/sharing/files/sharing.caddy") {
		t.Fatalf("got:\n%s", files["site.yml"])
	}
}

// A path the previous lock recorded and the current plan no longer writes is
// a file a package left behind when it left the configuration.
func TestGenerateRemovesAnExtensionThePlanNoLongerWrites(t *testing.T) {
	plan := planWithExtension(t) // sharing extends caddy.sites.d
	plan.PreviousExtensions = []string{"/etc/caddy/sites.d/orphan-old.caddy"}

	files, err := Generate(plan)
	if err != nil {
		t.Fatal(err)
	}

	var removed []string
	for _, task := range tasksIn(t, files["site.yml"]) {
		if task["name"] != "files a package left when it left the plan" {
			continue
		}
		for _, item := range task["loop"].([]any) {
			removed = append(removed, item.(string))
		}
	}
	if len(removed) != 1 || removed[0] != "/etc/caddy/sites.d/orphan-old.caddy" {
		t.Fatalf("got %#v", removed)
	}
}

// A path still written by the current plan is never removed, whatever the
// previous lock says.
func TestGenerateNeverRemovesAPathStillWritten(t *testing.T) {
	plan := planWithExtension(t) // sharing extends caddy.sites.d, for alice
	plan.PreviousExtensions = []string{"/etc/caddy/sites.d/alice-sharing-sharing.caddy"}

	files, err := Generate(plan)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(files["site.yml"]), "files a package left when it left the plan") {
		t.Fatalf("a path the plan still writes was removed:\n%s", files["site.yml"])
	}
}

// An empty previous list is the ordinary first-run state, and Generate must
// not invent a removal out of it.
func TestGenerateWithNoPreviousExtensionsRemovesNothing(t *testing.T) {
	plan := planWithExtension(t)
	files, err := Generate(plan)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(files["site.yml"]), "files a package left when it left the plan") {
		t.Fatalf("nothing was recorded before, and something was removed anyway:\n%s", files["site.yml"])
	}
}

// The removal reloads caddy when it changed something, sharing the reload
// that the routes already add rather than running Caddy twice.
func TestGenerateReloadsCaddyWhenAnExtensionIsRemoved(t *testing.T) {
	plan := planWithExtension(t)
	plan.SitesDir = "/etc/caddy/sites.d"
	plan.PreviousExtensions = []string{"/etc/caddy/sites.d/orphan-old.caddy"}

	files, err := Generate(plan)
	if err != nil {
		t.Fatal(err)
	}
	site := string(files["site.yml"])
	if !strings.Contains(site, "devmachine_extensions_removed") {
		t.Fatalf("the reload does not watch the removal:\n%s", site)
	}
	if strings.Count(site, "state: reloaded") != 1 {
		t.Fatalf("caddy is reloaded more than once:\n%s", site)
	}
}

// Without caddy on the machine there is nothing to reload, whatever an
// extension removal changed.
func TestGenerateWithoutCaddyReloadsNothingOnRemoval(t *testing.T) {
	store := storeWithCatalogue(t)
	plan := planFor(t, store, "main", nil, nil)
	plan.PreviousExtensions = []string{"/some/other/place.caddy"}
	plan.Extensions = []packages.Extension{
		{From: "sharing", Target: "alice", Point: "other.place", Source: "files/sharing.caddy",
			Into: "/some/other", Scope: packages.ScopeWorkspace},
	}

	files, err := Generate(plan)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(files["site.yml"]), "state: reloaded") {
		t.Fatalf("no caddy on the machine, and it reloaded anyway:\n%s", files["site.yml"])
	}
}

func TestGenerateHostVarsCarryTheWorkspaces(t *testing.T) {
	files, err := Generate(planWith(t, "main", nil, map[string][]string{"alice": {"claude-code"}}))
	if err != nil {
		t.Fatal(err)
	}
	vars := string(files["host_vars/devmachine.yml"])
	if !strings.Contains(vars, "alice") {
		t.Fatalf("got:\n%s", vars)
	}
}

// kind, entrypoint and commands are inert until v0.4. A generator that started
// treating them specially would be a surprise nobody asked for.
func TestGenerateTreatsACallablePackageAsAnOrdinaryRole(t *testing.T) {
	callable, err := Generate(planWith(t, "main", []string{"callable"}, nil))
	if err != nil {
		t.Fatal(err)
	}
	plain, err := Generate(planWith(t, "main", []string{"unused"}, nil))
	if err != nil {
		t.Fatal(err)
	}

	got := strings.ReplaceAll(string(callable["site.yml"]), "callable", "unused")
	if got != string(plain["site.yml"]) {
		t.Fatalf("a callable package generates something else:\n%s", callable["site.yml"])
	}
}

func TestGenerateIsDeterministic(t *testing.T) {
	plan := planWith(t, "main", []string{"caddy", "docker"}, map[string][]string{"alice": {"claude-code"}})
	first, err := Generate(plan)
	if err != nil {
		t.Fatal(err)
	}
	for range 5 {
		again, err := Generate(plan)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(first, again) {
			t.Fatal("two runs produced different files; a diff nobody can read is a diff nobody reads")
		}
	}
}

func TestGenerateCarriesAMachineSetting(t *testing.T) {
	files, err := Generate(planWithSettings(t, map[string]any{"base.timezone": "UTC"}, nil))
	if err != nil {
		t.Fatal(err)
	}
	vars := string(files["host_vars/devmachine.yml"])
	if !strings.Contains(vars, "devmachine_base_timezone: UTC") {
		t.Fatalf("got:\n%s", vars)
	}
}

func TestGenerateNamespacesASettingByItsPackage(t *testing.T) {
	// Two packages may both want `email`. Ansible has one variable namespace,
	// so the package name is what keeps them apart.
	files, err := Generate(planWithSettings(t, map[string]any{
		"caddy.email": "a@example.com", "acme.email": "b@example.com",
	}, nil))
	if err != nil {
		t.Fatal(err)
	}
	vars := string(files["host_vars/devmachine.yml"])
	for _, want := range []string{"devmachine_caddy_email:", "devmachine_acme_email:"} {
		if !strings.Contains(vars, want) {
			t.Fatalf("got:\n%s", vars)
		}
	}
}

func TestGenerateTurnsADashIntoAnUnderscore(t *testing.T) {
	// `claude-code` is a fine package name and an invalid Ansible variable.
	files, err := Generate(planWithSettings(t, nil,
		map[string]map[string]any{"alice": {"claude-code.model": "x"}}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(files["site.yml"]), "devmachine_claude_code_model: x") {
		t.Fatalf("got:\n%s", files["site.yml"])
	}
}

func TestGenerateKeepsOnlyTheFirstDotAsThePackageName(t *testing.T) {
	// A package is free to use a dotted name of its own, and the whole of it
	// is still one Ansible variable.
	files, err := Generate(planWithSettings(t, nil,
		map[string]map[string]any{"alice": {"claude-code.marketplace.url": "example.com"}}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(files["site.yml"]), "devmachine_claude_code_marketplace_url: example.com") {
		t.Fatalf("got:\n%s", files["site.yml"])
	}
}

func TestGenerateCarriesASettingThatIsNotAString(t *testing.T) {
	files, err := Generate(planWithSettings(t, map[string]any{
		"firewall.http":     false,
		"firewall.ports":    []string{"80", "443"},
		"firewall.maxretry": 5,
	}, nil))
	if err != nil {
		t.Fatal(err)
	}
	vars := string(files["host_vars/devmachine.yml"])
	for _, want := range []string{
		"devmachine_firewall_http: false",
		"devmachine_firewall_maxretry: 5",
		"- \"80\"",
	} {
		if !strings.Contains(vars, want) {
			t.Fatalf("%q is missing from:\n%s", want, vars)
		}
	}
}

func TestGenerateGivesEachWorkspaceItsOwnSettings(t *testing.T) {
	// alice and bob may install the same package with different values, and
	// the loop has to carry each one's.
	plan := planWithSettings(t, nil, map[string]map[string]any{
		"alice": {"claude-code.model": "one"},
		"bob":   {"claude-code.model": "two"},
	})
	files, err := Generate(plan)
	if err != nil {
		t.Fatal(err)
	}

	playbook := string(files["site.yml"])
	carried := 0
	for _, task := range tasksIn(t, files["site.yml"]) {
		vars, ok := task["vars"].(map[string]any)
		if !ok || vars["devmachine_claude_code_model"] == nil {
			continue
		}
		carried++
		loop, ok := task["loop"].([]any)
		if !ok || len(loop) != 1 {
			t.Fatalf("a setting reached %d workspaces at once:\n%s", len(loop), playbook)
		}
		who := loop[0].(map[string]any)["name"]
		want := map[any]string{"alice": "one", "bob": "two"}[who]
		if vars["devmachine_claude_code_model"] != want {
			t.Fatalf("%v got %v, not %q:\n%s", who, vars["devmachine_claude_code_model"], want, playbook)
		}
	}
	// Without this the loop above passes by never running, which is how the
	// settings went into include_role's own options and nobody noticed.
	if carried != 2 {
		t.Fatalf("%d tasks carried the setting, not 2:\n%s", carried, playbook)
	}
}

func TestGenerateLeavesAWorkspaceWithoutSettingsInTheSharedLoop(t *testing.T) {
	// Only a workspace that overrides something needs a task of its own;
	// everybody else keeps sharing one.
	plan := planWith(t, "main", nil, map[string][]string{
		"alice": {"claude-code"},
		"bob":   {"claude-code"},
	})
	plan.Workspaces[0].Target.Settings = map[string]any{"claude-code.model": "one"}

	files, err := Generate(plan)
	if err != nil {
		t.Fatal(err)
	}
	playbook := string(files["site.yml"])
	if !strings.Contains(playbook, "claude-code for each workspace that declares it") {
		t.Fatalf("bob lost the shared loop:\n%s", playbook)
	}
	if strings.Count(playbook, "include_role") != 2 {
		t.Fatalf("got:\n%s", playbook)
	}
}

func TestGenerateRefusesTwoSettingsWithTheSameVariableName(t *testing.T) {
	// `a-b` and `a.b` are two settings and one Ansible variable. Letting one
	// win silently is the failure this whole feature exists to prevent.
	_, err := Generate(planWithSettings(t, map[string]any{
		"base.a-b": "one",
		"base.a.b": "two",
	}, nil))
	if err == nil {
		t.Fatal("two settings collapsed into one variable without a word")
	}
	if !strings.Contains(err.Error(), "devmachine_base_a_b") {
		t.Fatalf("the error does not name the variable: %v", err)
	}
}

func TestGenerateMatchesTheGoldenFiles(t *testing.T) {
	plan := planWith(t, "main", []string{"caddy", "docker"}, map[string][]string{
		"alice": {"claude-code", "dev", "sharing"},
		"bob":   {"dev"},
	})
	plan.Machine.Settings = map[string]any{"caddy.email": "someone@example.com"}
	plan.Workspaces[0].Target.Settings = map[string]any{"claude-code.plugins": []string{"one", "two"}}
	plan.Workspaces[1].Target.Credentials = map[string]string{"gh": config.CredentialOwn}

	files, err := Generate(plan)
	if err != nil {
		t.Fatal(err)
	}

	for _, name := range slices.Sorted(maps.Keys(files)) {
		golden := filepath.Join("testdata", "generate", name)
		if *update {
			write(t, golden, string(files[name]))
			continue
		}
		want, err := os.ReadFile(golden)
		if err != nil {
			t.Fatal(err)
		}
		if string(want) != string(files[name]) {
			t.Errorf("%s changed:\n--- want ---\n%s\n--- got ---\n%s", name, want, files[name])
		}
	}
}

// tasksIn parses the generated playbook, which also proves it is YAML.
func tasksIn(t *testing.T, playbook []byte) []map[string]any {
	t.Helper()
	var plays []struct {
		Hosts  string           `yaml:"hosts"`
		Become bool             `yaml:"become"`
		Tasks  []map[string]any `yaml:"tasks"`
	}
	if err := yaml.Unmarshal(playbook, &plays); err != nil {
		t.Fatalf("the playbook is not YAML: %v\n%s", err, playbook)
	}
	if len(plays) != 1 {
		t.Fatalf("got %d plays", len(plays))
	}
	if plays[0].Hosts != "devmachine" || !plays[0].Become {
		t.Fatalf("got hosts %q, become %v", plays[0].Hosts, plays[0].Become)
	}
	return plays[0].Tasks
}

func TestGenerateKeepsASettingOutOfIncludeRolesOwnOptions(t *testing.T) {
	// `include_role` takes a fixed set of options and refuses the whole play
	// when it meets one it does not know: "Invalid options for include_role".
	// A setting is a variable for the role, so it belongs in the task's
	// `vars:`, not among the options of the thing that includes it.
	plan := planWithSettings(t, nil, map[string]map[string]any{
		"alice": {"claude-code.model": "one"},
	})
	files, err := Generate(plan)
	if err != nil {
		t.Fatal(err)
	}

	playbook := string(files["site.yml"])
	found := false
	for _, task := range tasksIn(t, files["site.yml"]) {
		options, ok := task["include_role"].(map[string]any)
		if !ok {
			continue
		}
		for key := range options {
			if key != "name" && key != "apply" {
				t.Fatalf("include_role was given %q, which it does not take:\n%s", key, playbook)
			}
		}
		if vars, ok := task["vars"].(map[string]any); ok && vars["devmachine_claude_code_model"] == "one" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the setting reached no task's vars:\n%s", playbook)
	}
}

func TestGenerateRunsTheWorkspacePackagesAgainAfterTheCopies(t *testing.T) {
	// The copies come last, because the account and the tool both have to
	// exist before a session is put where the tool looks for it. A package
	// that READS the copied session therefore sees nothing on the run that
	// delivered it, and only picks it up on the next one. `git-key` is the
	// first such package: without this second pass, `sync` leaves the machine
	// needing another `sync`, which is the one thing convergence must not do.
	plan := planSharing(t)
	files, err := Generate(plan)
	if err != nil {
		t.Fatal(err)
	}

	playbook := string(files["site.yml"])
	copies := strings.Index(playbook, "look for the shared")
	if copies < 0 {
		t.Fatalf("no copies were generated:\n%s", playbook)
	}
	if !strings.Contains(playbook[copies:], "now that the shared logins are in place") {
		t.Fatalf("the workspace packages do not run again after the copies:\n%s", playbook)
	}
}

func TestGenerateDoesNotRunTheWorkspacePackagesTwiceWithNothingToCopy(t *testing.T) {
	// A machine with no shared login has nothing to pick up, so the second
	// pass would be time spent proving what the first pass already proved.
	plan := planWith(t, "main", nil, map[string][]string{"alice": {"claude-code"}})
	files, err := Generate(plan)
	if err != nil {
		t.Fatal(err)
	}

	includes := strings.Count(string(files["site.yml"]), "        name: claude-code\n")
	if includes != 1 {
		t.Fatalf("claude-code is included %d times, want 1", includes)
	}
}

func TestRolePathFollowsTheOverlay(t *testing.T) {
	// Anything that needs to name a file inside a package on the machine has
	// to agree with roles_path. A second copy of this rule, in another
	// package, is a rule that has to be changed in two places and will not be.
	if got := RolePath(RemoteDir, packages.SourceLocal, "hostinger", "bin/provider"); got != "/opt/devmachine/roles.local/hostinger/bin/provider" {
		t.Fatalf("got %q", got)
	}
	if got := RolePath(RemoteDir, packages.SourceRelease, "hostinger", "bin/provider"); got != "/opt/devmachine/roles/hostinger/bin/provider" {
		t.Fatalf("got %q", got)
	}
}

func TestGenerateInstallsSkillsOnlyForWorkspacesSelectingThePackage(t *testing.T) {
	plan := planWith(t, "main", nil, map[string][]string{
		"alice": {"claude-code", "global-skills"},
		"bob":   {"claude-code"},
	})
	files, err := Generate(plan)
	if err != nil {
		t.Fatal(err)
	}
	playbook := string(files["site.yml"])
	for _, want := range []string{workspaceHome(skillAccountsVar("global-skills")) + "/.agents/skills/workflow", "global-skills", "remote_src: true"} {
		if !strings.Contains(playbook, want) {
			t.Fatalf("%q is missing:\n%s", want, playbook)
		}
	}
	for _, task := range tasksIn(t, files["site.yml"]) {
		if !taskHasTag(task, "global-skills") {
			continue
		}
		loop, ok := task["loop"].([]any)
		if !ok {
			continue
		}
		for _, raw := range loop {
			if raw.(map[string]any)["name"] == "bob" {
				t.Fatalf("bob received global-skills:\n%s", playbook)
			}
		}
	}
}

func TestGenerateCreatesClaudeLinksOnlyWhereClaudeCodeIsSelected(t *testing.T) {
	plan := planWith(t, "main", nil, map[string][]string{
		"alice": {"claude-code", "global-skills"},
		"bob":   {"global-skills"},
	})
	files, err := Generate(plan)
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range tasksIn(t, files["site.yml"]) {
		file, ok := task["file"].(map[string]any)
		if !ok || file["state"] != "link" {
			continue
		}
		loop := task["loop"].([]any)
		if len(loop) != 1 || loop[0].(map[string]any)["name"] != "alice" {
			t.Fatalf("Claude link loop = %#v", loop)
		}
		if file["src"] != "../../.agents/skills/workflow" {
			t.Fatalf("Claude link target = %#v", file["src"])
		}
		if file["force"] != "{{ ansible_check_mode }}" {
			t.Fatalf("Claude link must tolerate check mode's not-yet-copied target: %#v", file)
		}
		return
	}
	t.Fatal("no Claude skill link task was generated")
}

// TestGenerateReadsEachWorkspaceAccountBeforeItsSkills: a Mac's homes are in
// /Users with no group per user, so the home and gid come from the account
// itself, kept per package so one workspace never gets another's.
func TestGenerateReadsEachWorkspaceAccountBeforeItsSkills(t *testing.T) {
	plan := planWith(t, "main", nil, map[string][]string{
		"alice": {"claude-code", "global-skills"},
		"bob":   {"claude-code", "global-skills"},
	})
	files, err := Generate(plan)
	if err != nil {
		t.Fatal(err)
	}
	tasks := tasksIn(t, files["site.yml"])
	accounts := skillAccountsVar("global-skills")
	read := -1
	for i, task := range tasks {
		if task["register"] == accounts {
			read = i
			user := task["user"].(map[string]any)
			if user["name"] != "{{ devmachine_workspace.user }}" || task["check_mode"] != true || task["changed_when"] != false {
				t.Fatalf("the account read can change something: %#v", task)
			}
			if len(task["loop"].([]any)) != 2 {
				t.Fatalf("it does not read every workspace: %#v", task["loop"])
			}
		}
		if file, ok := task["file"].(map[string]any); ok && strings.Contains(fmt.Sprint(file["path"]), ".agents/skills") {
			if read < 0 || read > i {
				t.Fatalf("a skill task names a home before the account is read: %#v", task)
			}
			if !strings.Contains(fmt.Sprint(file["path"]), accounts+".results") {
				t.Fatalf("the home is not the account's: %#v", file)
			}
		}
	}
	if read < 0 {
		t.Fatalf("no task reads the accounts:\n%s", files["site.yml"])
	}
}

func TestGenerateLinksSkillsForAntigravityAndCline(t *testing.T) {
	plan := planWith(t, "main", nil, map[string][]string{
		"alice": {"antigravity", "global-skills"},
		"bob":   {"cline", "global-skills"},
	})
	files, err := Generate(plan)
	if err != nil {
		t.Fatal(err)
	}
	home := workspaceHome(skillAccountsVar("global-skills")) + "/"
	type want struct {
		workspace string
		dirs      []string
		src       string
	}
	cases := map[string]want{
		home + ".gemini/antigravity-cli/skills/workflow": {"alice", []string{".gemini", ".gemini/antigravity-cli", ".gemini/antigravity-cli/skills"}, "../../../.agents/skills/workflow"},
		home + ".cline/skills/workflow":                  {"bob", []string{".cline", ".cline/skills"}, "../../.agents/skills/workflow"},
	}
	tasks := tasksIn(t, files["site.yml"])
	position := map[string]int{}
	for index, task := range tasks {
		file, ok := task["file"].(map[string]any)
		if !ok {
			continue
		}
		path, _ := file["path"].(string)
		if dest, _ := file["dest"].(string); dest != "" {
			path = dest
		}
		position[path] = index
	}
	for dest, w := range cases {
		linkAt, ok := position[dest]
		if !ok {
			t.Fatalf("no link task for %s:\n%s", dest, files["site.yml"])
		}
		link := tasks[linkAt]
		file := link["file"].(map[string]any)
		if file["state"] != "link" || file["src"] != w.src || file["force"] != "{{ ansible_check_mode }}" {
			t.Fatalf("link for %s = %#v", dest, file)
		}
		assertOnlyWorkspace(t, link, w.workspace)
		previous := -1
		for _, dir := range w.dirs {
			at, ok := position[home+dir]
			if !ok {
				t.Fatalf("no task prepares %s:\n%s", dir, files["site.yml"])
			}
			if at <= previous || at >= linkAt {
				t.Fatalf("%s is prepared out of order", dir)
			}
			previous = at
			task := tasks[at]
			prepared := task["file"].(map[string]any)
			if prepared["state"] != "directory" || prepared["owner"] != "{{ devmachine_workspace.user }}" || prepared["group"] != workspaceGroup(skillAccountsVar("global-skills")) {
				t.Fatalf("%s is not a directory the workspace user owns: %#v", dir, prepared)
			}
			assertOnlyWorkspace(t, task, w.workspace)
		}
	}
	if strings.Contains(string(files["site.yml"]), ".claude/skills") {
		t.Fatalf("Claude links generated without claude-code:\n%s", files["site.yml"])
	}
}

func assertOnlyWorkspace(t *testing.T, task map[string]any, workspace string) {
	t.Helper()
	loop := task["loop"].([]any)
	if len(loop) != 1 || loop[0].(map[string]any)["name"] != workspace {
		t.Fatalf("%v loops over %#v, want only %s", task["name"], loop, workspace)
	}
}

func TestGenerateSkillTasksFailOnAnUnmanagedCollision(t *testing.T) {
	files, err := Generate(planWith(t, "main", nil, map[string][]string{"alice": {"global-skills"}}))
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range tasksIn(t, files["site.yml"]) {
		if _, ok := task["fail"]; ok && strings.Contains(task["name"].(string), "unmanaged") {
			if task["when"] == nil {
				t.Fatalf("collision failure is unconditional: %#v", task)
			}
			return
		}
	}
	t.Fatalf("no unmanaged collision failure:\n%s", files["site.yml"])
}

func TestGenerateSkillTasksNeverPruneAbsentSkills(t *testing.T) {
	files, err := Generate(planWith(t, "main", nil, map[string][]string{"alice": {"global-skills"}}))
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range tasksIn(t, files["site.yml"]) {
		file, ok := task["file"].(map[string]any)
		if ok && file["state"] == "absent" {
			t.Fatalf("generated a pruning task: %#v", task)
		}
	}
}

func TestGenerateSkillOwnershipSurvivesReleaseAndLocalOverlayChanges(t *testing.T) {
	plan := planFor(t, storeWithCatalogue(t, "global-skills"), "main", nil,
		map[string][]string{"alice": {"global-skills"}})
	files, err := Generate(plan)
	if err != nil {
		t.Fatal(err)
	}
	playbook := string(files["site.yml"])
	if !strings.Contains(playbook, `content: "global-skills\n"`) {
		t.Fatalf("ownership depends on local/release source:\n%s", playbook)
	}
}

func TestGenerateWritesOneRoutesFilePerWorkspace(t *testing.T) {
	plan := planWith(t, "main", []string{"caddy"}, map[string][]string{"alice": nil, "bob": nil})
	plan.SitesDir = "/etc/caddy/sites.d"
	plan.Routes = []packages.Route{
		{Workspace: "alice", Host: "app.example.com", Port: 8080},
		{Workspace: "alice", Host: "api.example.com", Port: 8081},
	}
	files, err := Generate(plan)
	if err != nil {
		t.Fatal(err)
	}
	body := string(files["routes/alice.caddy"])
	if !strings.Contains(body, "app.example.com {") || !strings.Contains(body, "api.example.com {") {
		t.Fatalf("alice's file:\n%s", body)
	}
	if _, ok := files["routes/bob.caddy"]; ok {
		t.Fatal("a workspace with no route gets no file")
	}
	site := string(files["site.yml"])
	for _, want := range []string{
		`dest: "/etc/caddy/sites.d/alice-routes.caddy"`,
		`path: "/etc/caddy/sites.d/app.example.com.caddy"`,
		`path: "/etc/caddy/sites.d/api.example.com.caddy"`,
		`path: "/etc/caddy/sites.d/bob-routes.caddy"`,
		"state: absent",
		"state: reloaded",
		"tags: [routes]",
	} {
		if !strings.Contains(site, want) {
			t.Fatalf("missing %q in the play:\n%s", want, site)
		}
	}
	if strings.Index(site, "name: caddy") > strings.Index(site, "alice-routes.caddy") {
		t.Fatal("routes must be written after caddy made sites.d")
	}
}

func TestGenerateReloadsCaddyForTheRoutesOnlyOnLinux(t *testing.T) {
	plan := planWith(t, "main", []string{"caddy"}, map[string][]string{"alice": nil})
	plan.SitesDir = "/etc/caddy/sites.d"
	plan.Routes = []packages.Route{{Workspace: "alice", Host: "app.example.com", Port: 8080}}
	files, err := Generate(plan)
	if err != nil {
		t.Fatal(err)
	}
	site := string(files["site.yml"])
	start := strings.Index(site, "- name: reload caddy for the routes")
	if start < 0 {
		t.Fatalf("no reload task:\n%s", site)
	}
	task := site[start:]
	task = task[:strings.Index(task, "tags: [routes]")]
	if !strings.Contains(task, "when: ansible_facts['system'] == 'Linux' and (") {
		t.Fatalf("the systemd reload must run only on Linux:\n%s", task)
	}
}

func TestGenerateWithoutCaddyWritesNoRouteTasks(t *testing.T) {
	plan := planWith(t, "main", []string{"base"}, nil)
	files, err := Generate(plan)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(files["site.yml"]), "routes") {
		t.Fatal("no caddy, no route tasks")
	}
}

func taskHasTag(task map[string]any, want string) bool {
	tags, ok := task["tags"].([]any)
	if !ok {
		return false
	}
	return slices.Contains(tags, any(want))
}

// planSelf resolves a plan for a self machine, so tests can prove the base
// directory and the macOS guard without touching a real machine.
func planSelf(t *testing.T, machinePackages []string) packages.MachinePlan {
	t.Helper()
	store := storeWithCatalogue(t)
	machine := config.Machine{Name: "mac", Self: true, Packages: machinePackages}
	cfg := config.Config{Machines: []config.Machine{machine}, Packages: pin}
	plan, err := packages.ResolveMachine(store, cfg, machine, "0.2.0")
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

// TestGenerateAtPinsAnsibleUserForASelfMachine covers the bug this fixes:
// Ansible's local connection plugin asks the shell who is logged in, and some
// shells leave LOGNAME set to root with USER empty — sending it looking for
// /var/root instead of the real home directory. Pinning ansible_user in the
// inventory is what makes it resolve to the real operator regardless.
func TestGenerateAtPinsAnsibleUserForASelfMachine(t *testing.T) {
	was := currentUser
	currentUser = func() (*osuser.User, error) { return &osuser.User{Username: "alice"}, nil }
	t.Cleanup(func() { currentUser = was })

	plan := planSelf(t, []string{"base"})
	files, err := GenerateAt(plan, "/Users/alice/.local/share/devmachine/bundle")
	if err != nil {
		t.Fatal(err)
	}
	inventory := string(files["inventory.ini"])
	if !strings.Contains(inventory, "devmachine ansible_connection=local ansible_user=alice") {
		t.Fatalf("got:\n%s", inventory)
	}
}

// TestGenerateWritesALocalInventory already covers a non-self machine's
// inventory having no ansible_user: there is nobody local to pin it to.
func TestGenerateAtNonSelfMachineHasNoAnsibleUser(t *testing.T) {
	files, err := Generate(planWith(t, "main", []string{"base"}, nil))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(files["inventory.ini"]), "ansible_user") {
		t.Fatalf("a non-self machine pinned ansible_user:\n%s", files["inventory.ini"])
	}
}

func TestGenerateAtFailsWhenTheCurrentUserCannotBeFound(t *testing.T) {
	was := currentUser
	currentUser = func() (*osuser.User, error) { return nil, errors.New("no such user") }
	t.Cleanup(func() { currentUser = was })

	if _, err := GenerateAt(planSelf(t, []string{"base"}), "/Users/alice/.local/share/devmachine/bundle"); err == nil {
		t.Fatal("expected an error when the current user cannot be found")
	}
}

func TestGenerateAtWritesTheBaseIntoAnsibleCfgAndThePlaybookCommand(t *testing.T) {
	plan := planSelf(t, []string{"base"})
	files, err := GenerateAt(plan, "/Users/alice/.local/share/devmachine/bundle")
	if err != nil {
		t.Fatal(err)
	}
	cfg := string(files["ansible.cfg"])
	want := "roles_path = /Users/alice/.local/share/devmachine/bundle/roles.local" +
		":/Users/alice/.local/share/devmachine/bundle/roles"
	if !strings.Contains(cfg, want) {
		t.Fatalf("got:\n%s", cfg)
	}
}

func TestGenerateAtSelfSetsBecomeFalse(t *testing.T) {
	plan := planSelf(t, []string{"base"})
	files, err := GenerateAt(plan, RemoteDir)
	if err != nil {
		t.Fatal(err)
	}

	var plays []struct {
		Hosts  string `yaml:"hosts"`
		Become bool   `yaml:"become"`
	}
	if err := yaml.Unmarshal(files["site.yml"], &plays); err != nil {
		t.Fatalf("the playbook is not YAML: %v\n%s", err, files["site.yml"])
	}
	if len(plays) != 1 {
		t.Fatalf("got %d plays", len(plays))
	}
	if plays[0].Become {
		t.Fatalf("a self machine's play still escalates: %v", plays[0])
	}
}

func TestGenerateAtSelfRefusesAnywhereButMacOS(t *testing.T) {
	plan := planSelf(t, []string{"base"})
	files, err := GenerateAt(plan, RemoteDir)
	if err != nil {
		t.Fatal(err)
	}

	site := string(files["site.yml"])
	for _, want := range []string{
		"ansible_facts['system'] != 'Darwin'",
		"a self machine is converged on macOS only",
	} {
		if !strings.Contains(site, want) {
			t.Fatalf("missing %q in:\n%s", want, site)
		}
	}
}

func TestGenerateAtRemoteMachineHasNoDarwinGuard(t *testing.T) {
	files, err := GenerateAt(planWith(t, "main", []string{"base"}, nil), RemoteDir)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(files["site.yml"]), "Darwin") {
		t.Fatalf("a remote machine's playbook mentions Darwin:\n%s", files["site.yml"])
	}
}

func TestGenerateReloadsCaddyWhenASiteFileChanges(t *testing.T) {
	plan := planWithExtension(t)
	plan.SitesDir = "/etc/caddy/sites.d"
	for i := range plan.Extensions {
		plan.Extensions[i].Into = plan.SitesDir
	}

	files, err := Generate(plan)
	if err != nil {
		t.Fatal(err)
	}
	site := string(files["site.yml"])
	if !strings.Contains(site, "register: devmachine_extension_0") {
		t.Fatalf("the site file's copy is not registered:\n%s", site)
	}
	reload := site[strings.Index(site, "state: reloaded"):]
	if !strings.Contains(reload, "devmachine_extension_0 is changed") {
		t.Fatalf("the reload does not watch the site file:\n%s", site)
	}
	if strings.Count(site, "state: reloaded") != 1 {
		t.Fatalf("caddy is reloaded more than once:\n%s", site)
	}
}
