package provision

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	osuser "os/user"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mydevmachine/devmachine/internal/remote"
)

// okRecap is a real recap line, spacing and all.
const okRecap = "\nPLAY RECAP *******************************************************\n" +
	"devmachine                 : ok=37   changed=14   unreachable=0    failed=0    skipped=6    rescued=0    ignored=0\n"

// fakeClient records what it was asked to send and run, and answers with
// canned output.
type fakeClient struct {
	uploaded map[string][]string
	commands []string
	output   string
	err      error
	runErr   error
	// runErrOn limits runErr to commands holding this text.
	runErrOn string
}

func (c *fakeClient) Run(_ context.Context, command string) (string, error) {
	c.commands = append(c.commands, command)
	if c.runErrOn != "" && !strings.Contains(command, c.runErrOn) {
		return "", nil
	}
	return "", c.runErr
}

func (c *fakeClient) Stream(_ context.Context, command string, stdout, _ io.Writer) error {
	c.commands = append(c.commands, command)
	if _, err := io.WriteString(stdout, c.output); err != nil {
		return err
	}
	return c.err
}

func (c *fakeClient) RunInput(ctx context.Context, command string, _ io.Reader) (string, error) {
	return c.Run(ctx, command)
}

func (c *fakeClient) Upload(_ context.Context, dir string, tarball io.Reader) error {
	if c.uploaded == nil {
		c.uploaded = map[string][]string{}
	}
	body, err := io.ReadAll(tarball)
	if err != nil {
		return err
	}
	c.uploaded[dir] = namesIn(body)
	c.commands = append(c.commands, "upload "+dir)
	return nil
}

func (c *fakeClient) Close() error { return nil }

// namesIn lists an archive's entries without a testing.T, because Upload has
// no way to fail a test.
func namesIn(body []byte) []string {
	names, err := archiveNames(bytes.NewReader(body))
	if err != nil {
		return []string{"unreadable archive: " + err.Error()}
	}
	return names
}

func sent(t *testing.T, c *fakeClient) []string {
	t.Helper()
	names, ok := c.uploaded[RemoteDir]
	if !ok {
		t.Fatalf("nothing was sent to %s: %v", RemoteDir, c.uploaded)
	}
	return names
}

func TestApplyUploadsBeforeItRuns(t *testing.T) {
	c := &fakeClient{output: okRecap}
	a := &Ansible{Client: c}

	_, err := a.Apply(context.Background(), planWith(t, "main", []string{"base"}, nil), Options{Out: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	if len(c.uploaded) == 0 {
		t.Fatal("it ran without sending anything")
	}
	if !strings.Contains(c.commands[len(c.commands)-1], "ansible-playbook") {
		t.Fatalf("last command was %q", c.commands[len(c.commands)-1])
	}
}

// A package that moved from roles.local to the release, or left the machine,
// would otherwise keep running from the copy an earlier sync unpacked: Ansible
// searches roles.local first, and unpacking never deletes.
func TestApplyStartsFromAnEmptyBundle(t *testing.T) {
	c := &fakeClient{output: okRecap}
	a := &Ansible{Client: c}

	if _, err := a.Apply(context.Background(), planWith(t, "main", []string{"base"}, nil), Options{Out: io.Discard}); err != nil {
		t.Fatal(err)
	}
	if len(c.commands) < 4 {
		t.Fatalf("commands: %q", c.commands)
	}
	if !strings.Contains(c.commands[0], "sudo -n true") {
		t.Fatalf("first it must prove it can become root, it ran %q", c.commands[0])
	}
	if c.commands[1] != remote.AsRoot("rm -rf "+RemoteDir) {
		t.Fatalf("then empty the bundle as root, it ran %q", c.commands[1])
	}
	if c.commands[2] != "upload "+RemoteDir {
		t.Fatalf("then send the new one, it ran %q", c.commands[2])
	}
}

func TestApplyRunsTheGeneratedPlaybookFromTheRemoteDirectory(t *testing.T) {
	c := &fakeClient{output: okRecap}
	a := &Ansible{Client: c}

	if _, err := a.Apply(context.Background(), planWith(t, "main", []string{"base"}, nil),
		Options{Out: io.Discard}); err != nil {
		t.Fatal(err)
	}
	want := "run=$(mktemp -d) && trap 'rm -rf \"$run\"' EXIT && " +
		"cp /opt/devmachine/site.yml \"$run/site.yml\" && cd \"$run\" && " +
		"ANSIBLE_CONFIG=/opt/devmachine/ansible.cfg " +
		"ansible-playbook -i /opt/devmachine/inventory.ini site.yml"
	// As root, so an admin with sudo runs every task the way root would —
	// including the ones that become a workspace's own unprivileged account,
	// which Ansible cannot do from one unprivileged account to another
	// without ACLs on its temporary files.
	if c.commands[len(c.commands)-1] != remote.AsRoot(want) {
		t.Fatalf("got %q", c.commands[len(c.commands)-1])
	}
}

// TestApplyCallsAnsibleByTheReportedPathAsRoot: a plain SSH command on a Mac
// has neither Homebrew nor MacPorts on PATH, and MacPorts may name it with a
// Python suffix.
func TestApplyCallsAnsibleByTheReportedPathAsRoot(t *testing.T) {
	c := &fakeClient{output: okRecap}
	a := &Ansible{Client: c}

	if _, err := a.Apply(context.Background(), planWith(t, "main", []string{"base"}, nil),
		Options{Out: io.Discard, AnsiblePlaybook: "/opt/local/bin/ansible-playbook-3.14"}); err != nil {
		t.Fatal(err)
	}
	want := "run=$(mktemp -d) && trap 'rm -rf \"$run\"' EXIT && " +
		"cp /opt/devmachine/site.yml \"$run/site.yml\" && cd \"$run\" && " +
		"ANSIBLE_CONFIG=/opt/devmachine/ansible.cfg " +
		"'/opt/local/bin/ansible-playbook-3.14' -i /opt/devmachine/inventory.ini site.yml"
	if c.commands[len(c.commands)-1] != remote.AsRoot(want) {
		t.Fatalf("got %q", c.commands[len(c.commands)-1])
	}
}

func TestApplySendsTheGeneratedFilesAndTheRecipes(t *testing.T) {
	c := &fakeClient{output: okRecap}
	a := &Ansible{Client: c}

	if _, err := a.Apply(context.Background(), planWith(t, "main", []string{"docker"}, nil),
		Options{Out: io.Discard}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"ansible.cfg", "inventory.ini", "host_vars/devmachine.yml", "site.yml",
		"roles/docker/package.yml", "roles/docker/tasks/main.yml", "roles/base/tasks/main.yml",
	} {
		if !slices.Contains(sent(t, c), want) {
			t.Fatalf("%q was not sent: %v", want, sent(t, c))
		}
	}
}

// A recipe in the cache that nothing asks for is the ordinary state. Sending
// the whole cache to every machine would make that state cost bandwidth and
// confusion.
func TestApplySendsOnlyTheRecipesThisMachineNeeds(t *testing.T) {
	c := &fakeClient{output: okRecap}
	a := &Ansible{Client: c}

	if _, err := a.Apply(context.Background(), planWith(t, "main", []string{"base"}, nil),
		Options{Out: io.Discard}); err != nil {
		t.Fatal(err)
	}
	for _, name := range sent(t, c) {
		if strings.HasPrefix(name, "roles/unused") {
			t.Fatalf("a recipe nothing asked for was sent: %v", sent(t, c))
		}
	}
}

// The operator's own copy has to land in the directory roles_path looks at
// first, or the overlay does nothing.
func TestApplySendsALocalPackageIntoTheOverlayDirectory(t *testing.T) {
	c := &fakeClient{output: okRecap}
	a := &Ansible{Client: c}

	if _, err := a.Apply(context.Background(), planWithLocalPackage(t, "main", "caddy"),
		Options{Out: io.Discard}); err != nil {
		t.Fatal(err)
	}
	names := sent(t, c)
	if !slices.Contains(names, "roles.local/caddy/package.yml") {
		t.Fatalf("the local copy did not land in the overlay: %v", names)
	}
	if slices.Contains(names, "roles/caddy/package.yml") {
		t.Fatalf("the published copy was sent as well: %v", names)
	}
	// What the local copy does not override still comes from the release.
	if !slices.Contains(names, "roles/firewall/package.yml") {
		t.Fatalf("a published dependency was dropped: %v", names)
	}
}

func TestApplySendsAWorkspaceRecipeToo(t *testing.T) {
	c := &fakeClient{output: okRecap}
	a := &Ansible{Client: c}

	if _, err := a.Apply(context.Background(),
		planWith(t, "main", nil, map[string][]string{"alice": {"claude-code"}}),
		Options{Out: io.Discard}); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(sent(t, c), "roles/claude-code/tasks/main.yml") {
		t.Fatalf("the workspace recipe was not sent: %v", sent(t, c))
	}
}

func TestApplyPassesCheckAndTagsThrough(t *testing.T) {
	c := &fakeClient{output: okRecap}
	a := &Ansible{Client: c}

	_, err := a.Apply(context.Background(), planWith(t, "main", []string{"base"}, nil),
		Options{Check: true, Tags: []string{"base", "docker"}, Out: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	command := c.commands[len(c.commands)-1]
	if !strings.Contains(command, "--check") {
		t.Fatalf("--check was dropped: %q", command)
	}
	if !strings.Contains(command, "--tags base,docker") {
		t.Fatalf("--tags was dropped: %q", command)
	}
}

func TestApplyReadsTheRecap(t *testing.T) {
	c := &fakeClient{output: "devmachine : ok=12 changed=3 unreachable=0 failed=0 skipped=1 rescued=0 ignored=0\n"}
	a := &Ansible{Client: c}

	got, err := a.Apply(context.Background(), planWith(t, "main", []string{"base"}, nil), Options{Out: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	if got.Changed != 3 || got.Failed != 0 || got.Ok != 12 {
		t.Fatalf("got %#v", got)
	}
}

func TestApplyReadsARecapWithItsRealSpacing(t *testing.T) {
	c := &fakeClient{output: okRecap}
	a := &Ansible{Client: c}

	got, err := a.Apply(context.Background(), planWith(t, "main", []string{"base"}, nil), Options{Out: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	if got.Ok != 37 || got.Changed != 14 || got.Failed != 0 {
		t.Fatalf("got %#v", got)
	}
}

func TestApplyFailsWhenTheRecapSaysSo(t *testing.T) {
	c := &fakeClient{
		output: "devmachine : ok=4 changed=0 unreachable=0 failed=2 skipped=0 rescued=0 ignored=0\n",
		err:    errors.New("Process exited with status 2"),
	}
	a := &Ansible{Client: c}

	got, err := a.Apply(context.Background(), planWith(t, "main", []string{"base"}, nil), Options{Out: io.Discard})
	if err == nil {
		t.Fatal("a failed run was reported as success")
	}
	if got.Failed != 2 {
		t.Fatalf("the recap was not read on failure: %#v", got)
	}
	// The count belongs in the message: an exit status alone says nothing.
	if !strings.Contains(err.Error(), "2") {
		t.Fatalf("the error does not say how much failed: %v", err)
	}
}

// The output is streamed, never collected and printed at the end: a run that
// looks stuck when it is not has already cost this project an afternoon.
func TestApplyStreamsToTheCallersWriter(t *testing.T) {
	c := &fakeClient{output: okRecap}
	a := &Ansible{Client: c}

	var out bytes.Buffer
	if _, err := a.Apply(context.Background(), planWith(t, "main", []string{"base"}, nil),
		Options{Out: &out}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "PLAY RECAP") {
		t.Fatalf("the caller saw nothing: %q", out.String())
	}
}

func TestApplyWithNoWriterStillRuns(t *testing.T) {
	c := &fakeClient{output: okRecap}
	a := &Ansible{Client: c}

	if _, err := a.Apply(context.Background(), planWith(t, "main", []string{"base"}, nil), Options{}); err != nil {
		t.Fatal(err)
	}
}

// An Ansible is a Provisioner. The interface exists so a caller can hold one.
var _ Provisioner = (*Ansible)(nil)

func TestApplyUsesTheSelfBaseForASelfMachine(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, ".local", "share", "devmachine", "bundle")

	c := &fakeClient{output: okRecap}
	a := &Ansible{Client: c}

	if _, err := a.Apply(context.Background(), planSelf(t, []string{"base"}), Options{Out: io.Discard}); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.uploaded[want]; !ok {
		t.Fatalf("nothing was sent to %s: %v", want, c.uploaded)
	}
	if !strings.Contains(c.commands[len(c.commands)-1], want) {
		t.Fatalf("the playbook command does not use %s: %q", want, c.commands[len(c.commands)-1])
	}
}

// TestApplyExportsTheRealUserForASelfMachine is the other half of the
// ansible_user fix: even with it pinned in the inventory, HOME still comes
// from the shell devmachine runs the playbook in, and that shell inherits
// devmachine's own environment. Exporting it explicitly is what keeps a
// wrong LOGNAME from reaching the run at all.
func TestApplyExportsTheRealUserForASelfMachine(t *testing.T) {
	was := currentUser
	currentUser = func() (*osuser.User, error) { return &osuser.User{Username: "alice"}, nil }
	t.Cleanup(func() { currentUser = was })
	t.Setenv("HOME", "/Users/alice")

	c := &fakeClient{output: okRecap}
	a := &Ansible{Client: c}

	if _, err := a.Apply(context.Background(), planSelf(t, []string{"base"}), Options{Out: io.Discard}); err != nil {
		t.Fatal(err)
	}
	command := c.commands[len(c.commands)-1]
	if !strings.HasPrefix(command, "export USER='alice' LOGNAME='alice' HOME='/Users/alice' && ") {
		t.Fatalf("got %q", command)
	}
}

func TestApplyDoesNotExportAnythingForANonSelfMachine(t *testing.T) {
	c := &fakeClient{output: okRecap}
	a := &Ansible{Client: c}

	if _, err := a.Apply(context.Background(), planWith(t, "main", []string{"base"}, nil),
		Options{Out: io.Discard}); err != nil {
		t.Fatal(err)
	}
	command := c.commands[len(c.commands)-1]
	if strings.Contains(command, "export USER") {
		t.Fatalf("a non-self machine exported a user: %q", command)
	}
}

func TestApplyStopsWhenTheBundleCannotBeEmptied(t *testing.T) {
	c := &fakeClient{output: okRecap, runErr: errors.New("permission denied"), runErrOn: "rm -rf"}
	a := &Ansible{Client: c}

	_, err := a.Apply(context.Background(), planWith(t, "main", []string{"base"}, nil), Options{Out: io.Discard})
	if err == nil || !strings.Contains(err.Error(), "emptying") {
		t.Fatalf("got %v", err)
	}
	if len(c.uploaded) != 0 {
		t.Fatal("it sent a bundle on top of one it could not empty")
	}
}

// TestApplyStopsWhenTheAdminCannotBecomeRoot: a sudo that wants a password is
// one sentence before anything is touched, not a mkdir error after.
func TestApplyStopsWhenTheAdminCannotBecomeRoot(t *testing.T) {
	c := &fakeClient{output: okRecap, runErr: errors.New("a password is required"), runErrOn: "sudo -n true"}
	a := &Ansible{Client: c}

	_, err := a.Apply(context.Background(), planWith(t, "main", []string{"base"}, nil), Options{Out: io.Discard})
	if !errors.Is(err, remote.ErrNoRoot) {
		t.Fatalf("got %v", err)
	}
	if len(c.commands) != 1 {
		t.Fatalf("it went on past the check: %q", c.commands)
	}
}

// TestApplyNeverAsksForRootOnASelfMachine: the bundle is the operator's own
// and the play runs without become; sudo on their computer is not ours to use.
func TestApplyNeverAsksForRootOnASelfMachine(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	c := &fakeClient{output: okRecap}
	a := &Ansible{Client: c}

	if _, err := a.Apply(context.Background(), planSelf(t, []string{"base"}), Options{Out: io.Discard}); err != nil {
		t.Fatal(err)
	}
	for _, command := range c.commands {
		if strings.Contains(command, "sudo") {
			t.Fatalf("a self machine asked for sudo: %q", command)
		}
	}
}
