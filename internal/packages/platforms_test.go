package packages

import (
	"path/filepath"
	"slices"
	"testing"
)

func platformPackage(t *testing.T, scope, platforms string) string {
	t.Helper()
	dir := writePackage(t, "mac-tools", "format: 1\nname: mac-tools\nscope: "+scope+
		"\nsummary: Tools for a Mac.\nplatforms: "+platforms+"\n")
	write(t, filepath.Join(dir, "tasks", "main.yml"), "---\n[]\n")
	return dir
}

func TestManifestReadsPlatforms(t *testing.T) {
	m, err := ParseManifest(platformPackage(t, ScopeMachine, "[macos]"))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(m.Platforms, []string{PlatformMacOS}) {
		t.Fatalf("platforms = %#v", m.Platforms)
	}
}

func TestValidateAcceptsTheKnownPlatforms(t *testing.T) {
	problems, err := Validate(platformPackage(t, ScopeMachine, "[linux, macos]"))
	if err != nil || len(problems) != 0 {
		t.Fatalf("%v %#v", err, problems)
	}
}

func TestValidateRejectsAnUnknownPlatform(t *testing.T) {
	problems, err := Validate(platformPackage(t, ScopeMachine, "[darwin]"))
	if err != nil {
		t.Fatal(err)
	}
	problemAbout(t, problems, `platform "darwin"`)
}

func TestValidateRejectsAPlatformListedTwice(t *testing.T) {
	problems, err := Validate(platformPackage(t, ScopeMachine, "[linux, linux]"))
	if err != nil {
		t.Fatal(err)
	}
	problemAbout(t, problems, "twice")
}

func TestValidateAcceptsAWorkspacePackageForAnySystem(t *testing.T) {
	for _, platforms := range []string{"[macos]", "[linux]", "[linux, macos]"} {
		problems, err := Validate(platformPackage(t, ScopeWorkspace, platforms))
		if err != nil || len(problems) != 0 {
			t.Fatalf("%s: %v %#v", platforms, err, problems)
		}
	}
}

func TestSchemaNamesPlatforms(t *testing.T) {
	for _, f := range Schema().Fields {
		if f.Name == "platforms" {
			return
		}
	}
	t.Fatal("packages schema does not name platforms")
}
