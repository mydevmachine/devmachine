package provision

import (
	"strings"
	"testing"

	"github.com/mydevmachine/devmachine/internal/packages"
)

func planForAliceOnly(t *testing.T) packages.MachinePlan {
	t.Helper()
	plan := planWith(t, "main", []string{"caddy"}, map[string][]string{
		"alice": {"sharing", "dev"},
		"bob":   {"sharing", "dev"},
	})
	plan.SitesDir = "/etc/caddy/sites.d"
	plan.Routes = []packages.Route{
		{Workspace: "alice", Host: "app.example.com", Port: 8080},
		{Workspace: "bob", Host: "bob.example.com", Port: 8081},
	}
	plan.OtherWorkspaces = []string{"carol"}
	plan.PreviousExtensions = []string{"/etc/caddy/sites.d/orphan-old.caddy", "/etc/caddy/sites.d/bob-sharing-sharing.caddy"}
	plan.OnlyWorkspace = "alice"
	return plan
}

func TestGenerateForOneWorkspaceRunsNoMachinePackage(t *testing.T) {
	files, err := Generate(planForAliceOnly(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range tasksIn(t, files["site.yml"]) {
		switch task["name"] {
		case "base", "firewall", "caddy":
			t.Fatalf("the machine package %s ran in a one-workspace sync:\n%s", task["name"], files["site.yml"])
		}
	}
}

func TestGenerateForOneWorkspaceTouchesNoOtherWorkspace(t *testing.T) {
	files, err := Generate(planForAliceOnly(t))
	if err != nil {
		t.Fatal(err)
	}
	site := string(files["site.yml"])
	for _, unwanted := range []string{`"bob"`, "bob-routes.caddy", "bob.example.com", "bob-sharing", "carol"} {
		if strings.Contains(site, unwanted) {
			t.Fatalf("%q is in a play for alice only:\n%s", unwanted, site)
		}
	}
	if _, ok := files["routes/bob.caddy"]; ok {
		t.Fatal("bob's routes file was sent with a play for alice only")
	}
	for _, wanted := range []string{"name: sharing", "name: dev", `"alice"`, "alice-routes.caddy", "alice-sharing-sharing.caddy",
		"the shared gh login"} {
		if !strings.Contains(site, wanted) {
			t.Fatalf("%q is missing from alice's play:\n%s", wanted, site)
		}
	}
}

// Which extension file belonged to which workspace is only known from its
// name, and a workspace named alice-dev shares alice's prefix: a narrowed run
// leaves removal to a full one rather than guess.
func TestGenerateForOneWorkspaceRemovesNoExtension(t *testing.T) {
	files, err := Generate(planForAliceOnly(t))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(files["site.yml"]), "files a package left when it left the plan") {
		t.Fatalf("a one-workspace sync removed an extension:\n%s", files["site.yml"])
	}
}

func TestGenerateForOneWorkspaceStillTellsPackagesEveryWorkspace(t *testing.T) {
	files, err := Generate(planForAliceOnly(t))
	if err != nil {
		t.Fatal(err)
	}
	vars := string(files["host_vars/devmachine.yml"])
	if !strings.Contains(vars, "name: alice") || !strings.Contains(vars, "name: bob") {
		t.Fatalf("a package that reads devmachine_workspaces would see only part of the machine:\n%s", vars)
	}
}

func TestSummaryForAnEmptyWorkspaceNamesTheWorkspace(t *testing.T) {
	plan := planWith(t, "main", []string{"base"}, map[string][]string{"alice": nil, "bob": {"dev"}})
	plan.OnlyWorkspace = "alice"

	if got := Summary(plan); len(got) != 1 || got[0] != "workspace alice: nothing to apply" {
		t.Fatalf("got %q", got)
	}
}
