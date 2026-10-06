package remote

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// scriptedClient answers each command with the stdout, stderr and error a
// test gave it, and remembers what it was asked.
type scriptedClient struct {
	recordingClient
	stdout map[string]string
	stderr map[string]string
	fail   map[string]bool
	stdin  []string
}

func (c *scriptedClient) Run(_ context.Context, command string) (string, error) {
	c.commands = append(c.commands, command)
	if c.fail[command] {
		return c.stdout[command], fmt.Errorf("running %q: exit status 1: %s", command, c.stderr[command])
	}
	return c.stdout[command], nil
}

func (c *scriptedClient) RunInput(ctx context.Context, command string, stdin io.Reader) (string, error) {
	body, err := io.ReadAll(stdin)
	if err != nil {
		return "", err
	}
	c.stdin = append(c.stdin, string(body))
	return c.Run(ctx, command)
}

func (c *scriptedClient) Stream(_ context.Context, command string, stdout, stderr io.Writer) error {
	c.commands = append(c.commands, command)
	_, _ = io.WriteString(stdout, c.stdout[command])
	_, _ = io.WriteString(stderr, c.stderr[command])
	if c.fail[command] {
		return fmt.Errorf("running %q: exit status 1", command)
	}
	return nil
}

const script = "/opt/devmachine/bootstrap/mac-brew/bin/bootstrap"

func TestBootstrapCheckListsWhatIsMissing(t *testing.T) {
	b := Bootstrap{Path: script}
	c := &scriptedClient{stdout: map[string]string{
		b.Command("check"): `{"missing":[{"name":"Xcode Command Line Tools","minutes":10},{"name":"Homebrew","minutes":5}]}` + "\n",
	}}
	got, err := b.Check(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	want := []Prerequisite{{Name: "Xcode Command Line Tools", Minutes: 10}, {Name: "Homebrew", Minutes: 5}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v", got)
	}
	if c.commands[0] != "sh '"+script+"' check" {
		t.Fatalf("ran %q", c.commands[0])
	}
}

func TestBootstrapCheckWithNothingMissing(t *testing.T) {
	b := Bootstrap{Path: script}
	c := &scriptedClient{stdout: map[string]string{b.Command("check"): `{"missing":[]}`}}
	got, err := b.Check(context.Background(), c)
	if err != nil || len(got) != 0 {
		t.Fatalf("got %#v, %v", got, err)
	}
}

// TestBootstrapCheckFromABodyLeavesNothingOnTheMachine: doctor only reads,
// so the script reaches sh on stdin rather than as a file.
func TestBootstrapCheckFromABodyLeavesNothingOnTheMachine(t *testing.T) {
	b := Bootstrap{Body: []byte("#!/bin/sh\necho '{\"missing\":[]}'\n")}
	c := &scriptedClient{stdout: map[string]string{"sh -s check": `{"missing":[]}`}}
	if _, err := b.Check(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	if c.commands[0] != "sh -s check" || c.stdin[0] != string(b.Body) {
		t.Fatalf("ran %q with %q", c.commands, c.stdin)
	}
}

func TestBootstrapApplyReadsThePathsAndStreamsProgress(t *testing.T) {
	b := Bootstrap{Path: script}
	c := &scriptedClient{
		stdout: map[string]string{b.Command("apply"): `{"ansible_playbook":"/opt/homebrew/bin/ansible-playbook","path_prefix":["/opt/homebrew/bin","/opt/homebrew/sbin"]}` + "\n"},
		stderr: map[string]string{b.Command("apply"): "Homebrew: present at /opt/homebrew.\n"},
	}
	var progress strings.Builder
	got, err := b.Apply(context.Background(), c, &progress)
	if err != nil {
		t.Fatal(err)
	}
	want := Prepared{AnsiblePlaybook: "/opt/homebrew/bin/ansible-playbook",
		PathPrefix: []string{"/opt/homebrew/bin", "/opt/homebrew/sbin"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v", got)
	}
	if !strings.Contains(progress.String(), "Homebrew: present") {
		t.Fatalf("the progress did not reach the person: %q", progress.String())
	}
}

func TestBootstrapApplyRefusesARelativePlaybook(t *testing.T) {
	b := Bootstrap{Path: script}
	c := &scriptedClient{stdout: map[string]string{b.Command("apply"): `{"ansible_playbook":"ansible-playbook"}`}}
	if _, err := b.Apply(context.Background(), c, io.Discard); err == nil || !strings.Contains(err.Error(), "absolute") {
		t.Fatalf("got %v", err)
	}
}

func TestBootstrapFailureNamesTheStepAndWhatToDo(t *testing.T) {
	b := Bootstrap{Path: script}
	failure := `{"error":{"step":"homebrew","message":"the Homebrew installer failed; its output is above."}}`
	for _, action := range []string{"check", "apply"} {
		c := &scriptedClient{
			stdout: map[string]string{b.Command(action): failure + "\n"},
			fail:   map[string]bool{b.Command(action): true},
		}
		var err error
		if action == "check" {
			_, err = b.Check(context.Background(), c)
		} else {
			_, err = b.Apply(context.Background(), c, io.Discard)
		}
		var stopped *BootstrapError
		if !errors.As(err, &stopped) || stopped.Step != "homebrew" {
			t.Fatalf("%s: got %v", action, err)
		}
		want := "the bootstrap stopped at homebrew: the Homebrew installer failed; its output is above."
		if err.Error() != want {
			t.Fatalf("%s: got %q, want %q", action, err.Error(), want)
		}
	}
}

func TestBootstrapThatPrintsNoJSONSaysWhatRan(t *testing.T) {
	b := Bootstrap{Path: script}
	c := &scriptedClient{
		stdout: map[string]string{b.Command("check"): "sh: bootstrap: not found\n"},
		fail:   map[string]bool{b.Command("check"): true},
	}
	_, err := b.Check(context.Background(), c)
	if err == nil || !strings.Contains(err.Error(), "check") {
		t.Fatalf("got %v", err)
	}
	c = &scriptedClient{stdout: map[string]string{b.Command("check"): "not json"}}
	if _, err := b.Check(context.Background(), c); err == nil {
		t.Fatal("a check with no JSON was read as nothing missing")
	}
}

func TestMacManagersFindsHomebrewAndMacPortsByPath(t *testing.T) {
	for _, tc := range []struct {
		out         string
		brew, ports bool
	}{
		{"", false, false},
		{"brew\n", true, false},
		{"ports\n", false, true},
		{"brew\nports\n", true, true},
	} {
		c := &scriptedClient{stdout: map[string]string{MacManagersCommand: tc.out}}
		got, err := MacManagers(context.Background(), c)
		if err != nil {
			t.Fatal(err)
		}
		if got.Brew != tc.brew || got.Ports != tc.ports {
			t.Fatalf("%q: got %#v", tc.out, got)
		}
	}
	for _, path := range []string{"/opt/homebrew/bin/brew", "/usr/local/bin/brew", "/opt/local/bin/port"} {
		if !strings.Contains(MacManagersCommand, path) {
			t.Fatalf("it does not look at %s: %s", path, MacManagersCommand)
		}
	}
}

// TestBootstrapRunsARealScript runs a stand-in bootstrap with sh on this
// computer, so the quoting and the JSON reading meet a real shell.
func TestBootstrapRunsARealScript(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "it's here", "bootstrap")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "#!/bin/sh\ncase \"$1\" in\ncheck) echo '{\"missing\":[]}' ;;\n" +
		"apply) echo progress >&2; echo '{\"ansible_playbook\":\"/usr/bin/true\",\"path_prefix\":[]}' ;;\nesac\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	c := &localClient{}
	if missing, err := (Bootstrap{Path: path}).Check(context.Background(), c); err != nil || len(missing) != 0 {
		t.Fatalf("check: %#v, %v", missing, err)
	}
	var progress strings.Builder
	got, err := (Bootstrap{Path: path}).Apply(context.Background(), c, &progress)
	if err != nil || got.AnsiblePlaybook != "/usr/bin/true" || progress.String() != "progress\n" {
		t.Fatalf("apply: %#v, %v, %q", got, err, progress.String())
	}
}
