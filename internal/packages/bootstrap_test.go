package packages

import (
	"path/filepath"
	"testing"
)

func bootstrapPackage(t *testing.T, scope, bootstrap string) string {
	t.Helper()
	dir := writePackage(t, "mac-brew", "format: 1\nname: mac-brew\nscope: "+scope+
		"\nsummary: Homebrew and Ansible on a Mac.\nplatforms: [macos]\nbootstrap: "+bootstrap+"\n")
	write(t, filepath.Join(dir, "tasks", "main.yml"), "---\n[]\n")
	return dir
}

func TestManifestReadsBootstrap(t *testing.T) {
	m, err := ParseManifest(bootstrapPackage(t, ScopeMachine, "bin/bootstrap"))
	if err != nil {
		t.Fatal(err)
	}
	if m.Bootstrap != "bin/bootstrap" {
		t.Fatalf("bootstrap = %q", m.Bootstrap)
	}
}

func TestValidateAcceptsAnExecutableShellBootstrapOnAMachinePackage(t *testing.T) {
	dir := bootstrapPackage(t, ScopeMachine, "bin/bootstrap")
	writeMode(t, filepath.Join(dir, "bin", "bootstrap"), "#!/bin/sh\necho '{}'\n", 0o755)

	problems, err := Validate(dir)
	if err != nil || len(problems) != 0 {
		t.Fatalf("%v %#v", err, problems)
	}
}

func TestValidateRejectsAMissingBootstrap(t *testing.T) {
	problems, err := Validate(bootstrapPackage(t, ScopeMachine, "bin/bootstrap"))
	if err != nil {
		t.Fatal(err)
	}
	p := problemAbout(t, problems, `bootstrap "bin/bootstrap" is not in the package`)
	if p.Line != 6 {
		t.Fatalf("line = %d, want 6", p.Line)
	}
}

func TestValidateRejectsABootstrapThatIsNotExecutable(t *testing.T) {
	dir := bootstrapPackage(t, ScopeMachine, "bin/bootstrap")
	write(t, filepath.Join(dir, "bin", "bootstrap"), "#!/bin/sh\n")

	problems, err := Validate(dir)
	if err != nil {
		t.Fatal(err)
	}
	problemAbout(t, problems, `bootstrap "bin/bootstrap" is not executable: chmod +x it`)
}

func TestValidateRejectsABootstrapOutsideThePackage(t *testing.T) {
	problems, err := Validate(bootstrapPackage(t, ScopeMachine, "../bootstrap"))
	if err != nil {
		t.Fatal(err)
	}
	problemAbout(t, problems, `bootstrap "../bootstrap" must stay inside the package`)
}

func TestValidateRejectsABootstrapOnAWorkspacePackage(t *testing.T) {
	dir := bootstrapPackage(t, ScopeWorkspace, "bin/bootstrap")
	writeMode(t, filepath.Join(dir, "bin", "bootstrap"), "#!/bin/sh\n", 0o755)

	problems, err := Validate(dir)
	if err != nil {
		t.Fatal(err)
	}
	problemAbout(t, problems, "`bootstrap` belongs to a machine package")
}
