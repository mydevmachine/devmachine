package commands

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/mydevmachine/devmachine/internal/config"
	"github.com/mydevmachine/devmachine/internal/facts"
	"github.com/mydevmachine/devmachine/internal/remote"
)

// factsRemote is a machine that says what it runs, and records every command
// it was asked to run.
type factsRemote struct {
	observed string
	runs     *[]string
}

func (f factsRemote) Run(_ context.Context, command string) (string, error) {
	if f.runs != nil {
		*f.runs = append(*f.runs, command)
	}
	if command == facts.ObserveCommand {
		return f.observed, nil
	}
	return "", nil
}

func (f factsRemote) Stream(ctx context.Context, command string, stdout, _ io.Writer) error {
	out, err := f.Run(ctx, command)
	if err != nil {
		return err
	}
	_, err = io.WriteString(stdout, out)
	return err
}

func (f factsRemote) RunInput(ctx context.Context, command string, _ io.Reader) (string, error) {
	return f.Run(ctx, command)
}

func (factsRemote) Upload(context.Context, string, io.Reader) error { return nil }
func (factsRemote) Close() error                                    { return nil }

const archObserved = "kernel=Linux\nmachine=x86_64\nansible_playbook=/usr/bin/ansible-playbook\nos-release.ID=arch\n"

func saveFacts(t *testing.T, dir, machine string, f facts.Facts) {
	t.Helper()
	if _, err := facts.Save(dir, machine, f); err != nil {
		t.Fatal(err)
	}
}

var observedMac = facts.Facts{
	ObservedAt: time.Date(2026, 10, 5, 15, 22, 0, 0, time.UTC),
	System:     "Darwin", OSFamily: "Darwin", Distribution: "MacOSX", DistributionVersion: "15.7.9",
	PkgMgr: "macports", ServiceMgr: "launchd", Architecture: "x86_64",
	AnsiblePlaybook: "/opt/local/bin/ansible-playbook-3.14",
	PathPrefix:      []string{"/opt/local/bin", "/opt/local/sbin"},
}

func TestMachinesShowPrintsTheMachineAndWhatWasObservedInJSON(t *testing.T) {
	dir := configWith(t, twoMachineConfig)
	saveFacts(t, dir, "sandbox", observedMac)
	dial = func(context.Context, config.Machine, string) (remote.Client, string, error) {
		return nil, "", errors.New("machines show must not connect")
	}
	t.Cleanup(func() { dial = remote.Dial })

	out, err := execute(t, "--config", dir, "--format", "json", "machines", "show", "sandbox")
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Name     string          `json:"name"`
		Hosts    []string        `json:"hosts"`
		Port     int             `json:"port"`
		Observed json.RawMessage `json:"observed"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if got.Name != "sandbox" || got.Port != 2222 || len(got.Hosts) != 1 {
		t.Fatalf("%s", out)
	}
	var observed facts.Facts
	if err := json.Unmarshal(got.Observed, &observed); err != nil {
		t.Fatal(err)
	}
	if !observed.Same(observedMac) {
		t.Fatalf("observed = %#v", observed)
	}
	for _, key := range []string{`"observed_at"`, `"pkg_mgr"`, `"path_prefix"`, `"admin_user"`, `"location"`} {
		if !strings.Contains(out, key) {
			t.Fatalf("no %s in %s", key, out)
		}
	}
}

func TestMachinesShowLeavesOutObservedForAMachineNeverRead(t *testing.T) {
	dir := configWith(t, twoMachineConfig)

	out, err := execute(t, "--config", dir, "--format", "json", "machines", "show", "main")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "observed") || !strings.Contains(out, `"name": "main"`) {
		t.Fatalf("%s", out)
	}
}

func TestMachinesShowInTextSaysWhatTheMachineRuns(t *testing.T) {
	dir := configWith(t, twoMachineConfig)
	saveFacts(t, dir, "sandbox", observedMac)

	out, err := execute(t, "--config", dir, "machines", "show", "sandbox")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"sandbox", "198.51.100.7", "MacOSX 15.7.9", "macports", "/opt/local/bin:/opt/local/sbin",
		"/opt/local/bin/ansible-playbook-3.14"} {
		if !strings.Contains(out, want) {
			t.Fatalf("no %q in:\n%s", want, out)
		}
	}
}

func TestMachinesShowInTextSaysHowToObserveAMachine(t *testing.T) {
	dir := configWith(t, twoMachineConfig)

	out, err := execute(t, "--config", dir, "machines", "show", "main")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "devmachine doctor --machine main") {
		t.Fatalf("%s", out)
	}
}

func TestMachinesShowRefusesAMachineThatIsNotConfigured(t *testing.T) {
	dir := configWith(t, twoMachineConfig)

	if _, err := execute(t, "--config", dir, "machines", "show", "nope"); err == nil {
		t.Fatal("no error for an unknown machine")
	}
}

// failingObserve is a machine whose read of what it runs fails.
type failingObserve struct{ factsRemote }

func (failingObserve) Run(_ context.Context, command string) (string, error) {
	if command == facts.ObserveCommand {
		return "", errors.New("connection reset")
	}
	return "", nil
}

func TestSyncFallsBackToTheSavedFactsWhenTheReadFails(t *testing.T) {
	dir := t.TempDir()
	saveFacts(t, dir, "studio", observedMac)

	observed, fresh := observedForSync(context.Background(), dir, "studio", failingObserve{})

	if fresh {
		t.Fatal("a failed read was reported as fresh")
	}
	m := config.Machine{Name: "studio"}
	if got := ansiblePlaybookFor(m, observed); got != observedMac.AnsiblePlaybook {
		t.Fatalf("ansible-playbook = %q, want the saved %q", got, observedMac.AnsiblePlaybook)
	}
}

func TestSyncUsesAFreshReadWhenItWorks(t *testing.T) {
	observed, fresh := observedForSync(context.Background(), t.TempDir(), "arch", factsRemote{observed: archObserved})

	if !fresh || observed.System != "Linux" {
		t.Fatalf("observed = %+v, fresh = %v", observed, fresh)
	}
}

func TestSyncKeepsWhatItReadAboutTheMachine(t *testing.T) {
	stubSync(t)
	dialing(t, factsRemote{observed: archObserved})
	dir := configWithPackages(t)

	if _, err := execute(t, "--config", dir, "sync", "--yes"); err != nil {
		t.Fatal(err)
	}
	got, found, err := facts.Load(dir, "main")
	if err != nil || !found || got.PkgMgr != "pacman" {
		t.Fatalf("found=%v err=%v %#v", found, err, got)
	}
}
