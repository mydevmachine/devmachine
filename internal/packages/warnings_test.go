package packages

import (
	"path/filepath"
	"strings"
	"testing"
)

const notesManifest = `format: 1
name: notes
scope: workspace
summary: Notes for each workspace.
`

const readAccountTask = `- name: Read the account's home and group
  ansible.builtin.user:
    name: "{{ devmachine_workspace.user }}"
  check_mode: true
  changed_when: false
  register: devmachine_account
`

const useAccountTask = `- name: Write the notes folder
  ansible.builtin.file:
    path: "{{ devmachine_account.home }}/notes"
    state: directory
    group: "{{ devmachine_account.group }}"
`

// TestWarnsWhenAPackageReadsAnAccountItNeverRegisters: a registered variable
// outlives the role, so without its own register the package reads the
// account another package left, which can be another workspace's.
func TestWarnsWhenAPackageReadsAnAccountItNeverRegisters(t *testing.T) {
	dir := writePackage(t, "notes", notesManifest)
	write(t, filepath.Join(dir, "defaults", "main.yml"), "---\nnotes_home: \"{{ devmachine_account.home }}\"\n")
	write(t, filepath.Join(dir, "tasks", "main.yml"), "---\n"+useAccountTask)

	warnings, err := Warnings(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 1 {
		t.Fatalf("want one warning, got %#v", warnings)
	}
	got := warnings[0]
	if got.File != filepath.Join("defaults", "main.yml") || got.Line != 2 {
		t.Fatalf("the warning does not point at the first read: %#v", got)
	}
	for _, want := range []string{"devmachine_account", "another workspace", "Read the account's home and group", "register: devmachine_account"} {
		if !strings.Contains(got.What, want) {
			t.Fatalf("the warning leaves out %q: %s", want, got.What)
		}
	}

	problems, err := Validate(dir)
	if err != nil || len(problems) != 0 {
		t.Fatalf("a warning must not fail validation: %v %#v", err, problems)
	}
}

func TestNoWarningWhenThePackageRegistersTheAccountItself(t *testing.T) {
	dir := writePackage(t, "notes", notesManifest)
	write(t, filepath.Join(dir, "tasks", "main.yml"), "---\n"+readAccountTask+"\n"+useAccountTask)

	warnings, err := Warnings(dir)
	if err != nil || len(warnings) != 0 {
		t.Fatalf("%v %#v", err, warnings)
	}
}

func TestNoWarningForAPackageThatOnlyMentionsTheAccountInAComment(t *testing.T) {
	dir := writePackage(t, "prefs", "format: 1\nname: prefs\nscope: workspace\nsummary: Prefs.\n")
	write(t, filepath.Join(dir, "tasks", "main.yml"), "---\n# devmachine_account is not read here\n[]\n")

	warnings, err := Warnings(dir)
	if err != nil || len(warnings) != 0 {
		t.Fatalf("%v %#v", err, warnings)
	}
}
