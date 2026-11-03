package packages

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestWriteSkeletonProducesSomethingThatValidates(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sharing")
	if err := WriteSkeleton(dir, "sharing", ScopeWorkspace, ""); err != nil {
		t.Fatal(err)
	}

	problems, err := Validate(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 0 {
		t.Fatalf("the skeleton does not validate: %#v", problems)
	}
}

// A dash is fine in a package name and never fine in an Ansible variable, so
// the skeleton has to translate rather than write something that fails on the
// machine.
func TestWriteSkeletonTurnsADashIntoAVariableAnsibleAccepts(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "claude-code")
	if err := WriteSkeleton(dir, "claude-code", ScopeWorkspace, ""); err != nil {
		t.Fatal(err)
	}

	defaults := readFile(t, filepath.Join(dir, "defaults", "main.yml"))
	if !strings.Contains(defaults, "claude_code_packages") {
		t.Fatalf("got:\n%s", defaults)
	}
	if strings.Contains(defaults, "claude-code_packages") {
		t.Fatalf("a dash reached a variable name:\n%s", defaults)
	}
}

func TestWriteSkeletonRefusesToOverwrite(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sharing")
	if err := WriteSkeleton(dir, "sharing", ScopeWorkspace, ""); err != nil {
		t.Fatal(err)
	}
	if err := WriteSkeleton(dir, "sharing", ScopeWorkspace, ""); err == nil {
		t.Fatal("it overwrote a package that was already there")
	}
}

func TestWriteSkeletonRefusesAScopeThatIsNeitherMachineNorWorkspace(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sharing")
	err := WriteSkeleton(dir, "sharing", "local", "")
	if err == nil {
		t.Fatal("a third scope was accepted")
	}
	if !strings.Contains(err.Error(), ScopeMachine) || !strings.Contains(err.Error(), ScopeWorkspace) {
		t.Fatalf("the error does not say what the scopes are: %v", err)
	}
}

func TestWriteSkeletonRefusesANameThatIsNotAPackageName(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Sharing")
	if err := WriteSkeleton(dir, "Sharing", ScopeMachine, ""); err == nil {
		t.Fatal("a name with a capital was accepted")
	}
}

func TestSkeletonForADNSProviderValidates(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "example-registrar")
	if err := WriteSkeleton(dir, "example-registrar", ScopeMachine, "dns"); err != nil {
		t.Fatal(err)
	}
	problems, err := Validate(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 0 {
		t.Fatalf("the skeleton does not validate: %#v", problems)
	}
}

func TestSkeletonForADNSProviderAnswersTheContract(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "example-registrar")
	if err := WriteSkeleton(dir, "example-registrar", ScopeMachine, "dns"); err != nil {
		t.Fatal(err)
	}

	// A skeleton somebody has to fix before it runs teaches the wrong thing
	// on the first try.
	out, err := exec.Command(filepath.Join(dir, "bin", "provider"), "list", "example.com").Output()
	if err != nil {
		t.Fatalf("the skeleton does not run: %v", err)
	}
	var answer struct {
		Records []map[string]any `json:"records"`
	}
	if err := json.Unmarshal(out, &answer); err != nil {
		t.Fatalf("the skeleton does not answer JSON: %v\n%s", err, out)
	}
}

func TestSchemaPublishesTheFormatsItReads(t *testing.T) {
	if !slices.Equal(Schema().Formats, ReadableFormats) {
		t.Fatalf("got %#v", Schema().Formats)
	}
}

func TestSchemaMarksTheRequiredFields(t *testing.T) {
	required := map[string]bool{}
	for _, f := range Schema().Fields {
		required[f.Name] = f.Required
	}
	for _, want := range []string{"format", "name", "scope", "summary"} {
		if !required[want] {
			t.Errorf("%q should be required", want)
		}
	}
	if required["needs"] {
		t.Error("needs is not required")
	}
}

func TestSchemaCoversEveryManifestField(t *testing.T) {
	fields := map[string]bool{}
	for _, f := range Schema().Fields {
		fields[f.Name] = true
	}
	rt := reflect.TypeOf(Manifest{})
	for i := range rt.NumField() {
		tag, _, _ := strings.Cut(rt.Field(i).Tag.Get("yaml"), ",")
		if tag == "" || tag == "-" {
			continue
		}
		if !fields[tag] {
			t.Errorf("the schema does not document %q", tag)
		}
	}
}

func TestEverySchemaFieldExplainsItself(t *testing.T) {
	for _, f := range Schema().Fields {
		if strings.TrimSpace(f.Summary) == "" {
			t.Errorf("%q has no summary, and the schema is what a writer reads", f.Name)
		}
	}
}

func TestSkeletonDNSProviderAnswersZonesAndHelp(t *testing.T) {
	// The manifest it writes declares six commands. An entrypoint that
	// refuses two of them makes `devmachine packages help` fail on a package
	// the CLI itself generated, which is the worst possible first impression.
	dir := t.TempDir()
	if err := WriteSkeleton(dir, "example-registrar", ScopeMachine, kindDNS); err != nil {
		t.Fatal(err)
	}
	entry := filepath.Join(dir, "bin", "provider")

	for _, tc := range []struct{ command, key string }{
		{"zones", "zones"},
		{"help", "commands"},
	} {
		out, err := exec.Command("python3", entry, tc.command).Output()
		if err != nil {
			t.Fatalf("%s: %v", tc.command, err)
		}
		var answer map[string]any
		if err := json.Unmarshal(out, &answer); err != nil {
			t.Fatalf("%s: %v (%s)", tc.command, err, out)
		}
		if _, ok := answer[tc.key]; !ok {
			t.Fatalf("%s answered %s, with no %q", tc.command, out, tc.key)
		}
	}
}
