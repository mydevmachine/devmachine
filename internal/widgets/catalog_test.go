package widgets

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func notInstalled(string) Installed { return Installed{} }

func TestResolveListsEachWidgetWithItsOrigin(t *testing.T) {
	release, local := t.TempDir(), t.TempDir()
	writeWidget(t, filepath.Join(release, "claude-code", "widgets"), "usage", usageWidget)
	writeWidget(t, filepath.Join(local, "mine", "widgets"), "usage", usageWidget)

	catalog := Resolve("v40", []PackageWidgets{
		{Package: "claude-code", Scope: "workspace", Origin: OriginRelease, Version: "v40", Root: filepath.Join(release, "claude-code", "widgets")},
		{Package: "mine", Scope: "workspace", Origin: OriginLocal, Root: filepath.Join(local, "mine", "widgets")},
	}, notInstalled)

	if len(catalog.Widgets) != 2 || len(catalog.Problems) != 0 {
		t.Fatalf("got %+v", catalog)
	}
	first, second := catalog.Widgets[0], catalog.Widgets[1]
	if first.Name != "claude-code/usage" || first.Origin != OriginRelease || first.Version != "v40" {
		t.Fatalf("got %+v", first)
	}
	if second.Name != "mine/usage" || second.Origin != OriginLocal || second.Version != "" {
		t.Fatalf("got %+v", second)
	}
	if !first.Available || first.UnavailableReason != "" {
		t.Fatalf("an app/* widget is available without its package: %+v", first)
	}
}

func TestResolveLeavesABrokenWidgetOutAndListsItsProblem(t *testing.T) {
	root := t.TempDir()
	writeWidget(t, root, "usage", usageWidget)
	writeWidget(t, root, "bad", strings.Replace(strings.Replace(usageWidget, "name: usage", "name: bad", 1),
		"view: {kind: app.harness-usage}", "view: {kind: gauge}", 1))

	catalog := Resolve("v40", []PackageWidgets{{Package: "mine", Origin: OriginLocal, Root: root}}, notInstalled)
	if len(catalog.Widgets) != 1 || len(catalog.Problems) != 1 {
		t.Fatalf("got %+v", catalog)
	}
	if !strings.Contains(catalog.Problems[0].Message, `view.kind "gauge" draws number, json, not app/harness-usage`) {
		t.Fatalf("got %+v", catalog.Problems)
	}
}

func TestAvailabilityNeedsThePackageForAnythingButAppProviders(t *testing.T) {
	prs := Widget{Source: Source{Name: "github/prs"}}
	machinePkg := PackageWidgets{Package: "github-prs", Scope: "machine"}
	workspacePkg := PackageWidgets{Package: "github-prs", Scope: "workspace"}

	cases := []struct {
		name   string
		w      Widget
		pkg    PackageWidgets
		state  Installed
		ok     bool
		reason string
	}{
		{"app provider", Widget{Source: Source{Name: "app/clock"}}, machinePkg, Installed{}, true, ""},
		{"not added, machine package", prs, machinePkg, Installed{}, false, "add the package: devmachine packages add github-prs --machine <name>"},
		{"not added, workspace package", prs, workspacePkg, Installed{}, false, "add the package: devmachine packages add github-prs --workspace <name>"},
		{"added, not synced", prs, machinePkg, Installed{Added: true}, false, "sync to install the package: devmachine sync"},
		{"added and synced", prs, machinePkg, Installed{Added: true, Synced: true}, true, ""},
	}
	for _, tc := range cases {
		ok, reason := Availability(tc.w, tc.pkg, tc.state)
		if ok != tc.ok || reason != tc.reason {
			t.Errorf("%s: got %v %q", tc.name, ok, reason)
		}
	}
}

func TestCatalogJSONHasTheAgreedShape(t *testing.T) {
	root := t.TempDir()
	writeWidget(t, root, "usage", usageWidget)
	catalog := Resolve("v40", []PackageWidgets{{Package: "claude-code", Origin: OriginRelease, Version: "v40", Root: root}}, notInstalled)

	body, err := json.Marshal(catalog)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"engine", "packages_release", "widgets", "providers", "problems"} {
		if _, ok := got[key]; !ok {
			t.Errorf("missing %q", key)
		}
	}
	entry := got["widgets"].([]any)[0].(map[string]any)
	for _, key := range []string{"name", "package", "widget", "origin", "version", "path", "summary", "requires_engine",
		"fits", "context", "inputs", "source", "view", "sizes", "default_size", "places", "single", "surfaces", "available", "unavailable_reason", "package_path", "trust"} {
		if _, ok := entry[key]; !ok {
			t.Errorf("widget entry is missing %q", key)
		}
	}
	if len(entry) != 22 {
		t.Errorf("widget entry has %d keys, want 22: %v", len(entry), entry)
	}
	source := entry["source"].(map[string]any)
	if source["every"] != "60s" || source["with"].(map[string]any)["harness"] != "{{inputs.harness}}" {
		t.Errorf("got %v", source)
	}
	if got["problems"] == nil {
		t.Error("problems must be [] when there are none, never null")
	}
}

func TestAURLWidgetIsAvailableWithoutItsPackage(t *testing.T) {
	pkg := PackageWidgets{Package: "mine", Scope: "machine"}
	urlWidget := Widget{Source: Source{Kind: SourceURL}}
	if ok, reason := Availability(urlWidget, pkg, Installed{}); !ok || reason != "" {
		t.Fatalf("got %v %q", ok, reason)
	}
	commandWidget := Widget{Source: Source{Kind: SourceCommand}}
	if ok, reason := Availability(commandWidget, pkg, Installed{}); ok || !strings.Contains(reason, "devmachine packages add mine --machine") {
		t.Fatalf("got %v %q", ok, reason)
	}
}

func TestResolveListsPackageProvidersAndTrust(t *testing.T) {
	release := t.TempDir()
	writeWidget(t, filepath.Join(release, "devmachine-app", "widgets"), "machine-stats", machineStatsWidget)
	stats := map[string]PackageProvider{"stats": {Returns: map[string]string{"disk": "object"}, MinEvery: "10s"}}
	source := &PackageSource{URL: "https://example.com/alice/tools.git", Ref: "v1", Commit: "0123abc"}

	catalog := Resolve("v40", []PackageWidgets{
		{Package: "devmachine-app", Scope: "machine", Origin: OriginRelease, Version: "v40",
			Dir: filepath.Join(release, "devmachine-app"), Root: filepath.Join(release, "devmachine-app", "widgets"), Providers: stats},
		{Package: "alice-tools", Scope: "workspace", Origin: OriginLocal, Trust: TrustThirdParty, Source: source,
			Providers: map[string]PackageProvider{"disk": {Returns: map[string]string{"used": "number"}, MinEvery: "30s"}}},
		{Package: "mine", Scope: "machine", Origin: OriginLocal},
	}, notInstalled)

	if len(catalog.Widgets) != 1 || len(catalog.Problems) != 0 {
		t.Fatalf("got %+v", catalog)
	}
	w := catalog.Widgets[0]
	if w.Trust != TrustOfficial || w.PackageSource != nil || w.Provider == nil || w.Provider.MinEvery != "10s" {
		t.Fatalf("got %+v", w)
	}
	disk := catalog.Providers["alice-tools/disk"]
	if disk.Trust != TrustThirdParty || disk.Scope != "workspace" || disk.Command != "disk" || disk.MinEvery != "30s" {
		t.Fatalf("got %+v", catalog.Providers)
	}
	if catalog.Providers["devmachine-app/stats"].Trust != TrustOfficial || len(catalog.Providers) != 2 {
		t.Fatalf("got %+v", catalog.Providers)
	}
	known := catalog.KnownProviders()
	if _, ok := known["mine"]; !ok || len(known["mine"]) != 0 || len(known["alice-tools"]) != 1 {
		t.Fatalf("got %+v", known)
	}
}
