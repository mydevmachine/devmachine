package facts

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

type fakeClient struct {
	out  map[string]string
	err  error
	runs []string
}

func (f *fakeClient) Run(_ context.Context, command string) (string, error) {
	f.runs = append(f.runs, command)
	if f.err != nil {
		return "", f.err
	}
	return f.out[command], nil
}

func (f *fakeClient) RunInput(ctx context.Context, command string, _ io.Reader) (string, error) {
	return f.Run(ctx, command)
}

func (f *fakeClient) Stream(context.Context, string, io.Writer, io.Writer) error { return nil }
func (f *fakeClient) Upload(context.Context, string, io.Reader) error            { return nil }
func (f *fakeClient) Close() error                                               { return nil }

func answering(out string) *fakeClient {
	return &fakeClient{out: map[string]string{ObserveCommand: out}}
}

var when = time.Date(2026, 10, 5, 15, 22, 0, 0, time.UTC)

const archOutput = `kernel=Linux
machine=x86_64
ansible_playbook=/usr/bin/ansible-playbook
os-release.NAME="Arch Linux"
os-release.ID=arch
os-release.BUILD_ID=rolling
`

func TestObserveReadsALinuxInOneCommand(t *testing.T) {
	c := answering(archOutput)
	got, err := Observe(context.Background(), c, when)
	if err != nil {
		t.Fatal(err)
	}
	want := Facts{
		ObservedAt: when, System: "Linux", OSFamily: "Archlinux", Distribution: "Archlinux",
		PkgMgr: "pacman", ServiceMgr: "systemd", Architecture: "x86_64",
		AnsiblePlaybook: "/usr/bin/ansible-playbook", PathPrefix: []string{},
	}
	if !got.Same(want) || !got.ObservedAt.Equal(when) {
		t.Fatalf("got %#v\nwant %#v", got, want)
	}
	if len(c.runs) != 1 {
		t.Fatalf("it ran %d commands, want one round trip: %q", len(c.runs), c.runs)
	}
}

func TestObserveNamesDebianAndUbuntuTheWayAnsibleDoes(t *testing.T) {
	for id, distribution := range map[string]string{"debian": "Debian", "ubuntu": "Ubuntu"} {
		got, err := Observe(context.Background(), answering("kernel=Linux\nmachine=aarch64\nansible_playbook=\n"+
			"os-release.ID="+id+"\nos-release.VERSION_ID=\"12\"\n"), when)
		if err != nil {
			t.Fatal(err)
		}
		if got.OSFamily != "Debian" || got.Distribution != distribution || got.DistributionVersion != "12" ||
			got.PkgMgr != "apt" || got.ServiceMgr != "systemd" || got.AnsiblePlaybook != "" {
			t.Fatalf("%s: %#v", id, got)
		}
	}
}

func TestObserveReadsAMacAndWhereItsManagerLives(t *testing.T) {
	for _, tc := range []struct {
		ansible, pkgMgr string
		prefix          []string
	}{
		{"/opt/local/bin/ansible-playbook", "macports", []string{"/opt/local/bin", "/opt/local/sbin"}},
		{"/opt/homebrew/bin/ansible-playbook", "homebrew", []string{"/opt/homebrew/bin", "/opt/homebrew/sbin"}},
		{"/usr/local/bin/ansible-playbook", "homebrew", []string{"/usr/local/bin", "/usr/local/sbin"}},
		{"", "", []string{}},
	} {
		got, err := Observe(context.Background(), answering("kernel=Darwin\nmachine=x86_64\n"+
			"version=15.7.9\nansible_playbook="+tc.ansible+"\n"), when)
		if err != nil {
			t.Fatal(err)
		}
		if got.System != "Darwin" || got.OSFamily != "Darwin" || got.Distribution != "MacOSX" ||
			got.DistributionVersion != "15.7.9" || got.ServiceMgr != "launchd" ||
			got.PkgMgr != tc.pkgMgr || !slices.Equal(got.PathPrefix, tc.prefix) {
			t.Fatalf("%q: %#v", tc.ansible, got)
		}
	}
}

func TestObserveSaysWhatItCouldNotRead(t *testing.T) {
	_, err := Observe(context.Background(), &fakeClient{err: errors.New("boom")}, when)
	if err == nil {
		t.Fatal("no error")
	}
}

func TestPlatformIsWhatPackagesCallTheSystem(t *testing.T) {
	for system, want := range map[string]string{"Linux": "linux", "Darwin": "macos", "": "", "FreeBSD": ""} {
		if got := (Facts{System: system}).Platform(); got != want {
			t.Fatalf("%q: got %q, want %q", system, got, want)
		}
	}
}

func TestLoadSaysNothingWasObservedWhenThereIsNoFile(t *testing.T) {
	_, found, err := Load(t.TempDir(), "vps")
	if err != nil || found {
		t.Fatalf("found=%v err=%v", found, err)
	}
}

func TestSaveWritesTheFileLoadReads(t *testing.T) {
	dir := t.TempDir()
	f := Facts{ObservedAt: when, System: "Linux", Distribution: "Debian", PathPrefix: []string{}}
	wrote, err := Save(dir, "vps", f)
	if err != nil || !wrote {
		t.Fatalf("wrote=%v err=%v", wrote, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "state", "machines", "vps.json")); err != nil {
		t.Fatal(err)
	}
	got, found, err := Load(dir, "vps")
	if err != nil || !found || !got.Same(f) || !got.ObservedAt.Equal(when) {
		t.Fatalf("got %#v found=%v err=%v", got, found, err)
	}
}

func TestSaveLeavesTheFileAloneWhenOnlyTheTimeChanged(t *testing.T) {
	dir := t.TempDir()
	f := Facts{ObservedAt: when, System: "Linux", Distribution: "Debian"}
	if _, err := Save(dir, "vps", f); err != nil {
		t.Fatal(err)
	}
	path := Path(dir, "vps")
	before, _ := os.ReadFile(path)

	f.ObservedAt = when.Add(time.Hour)
	wrote, err := Save(dir, "vps", f)
	if err != nil || wrote {
		t.Fatalf("wrote=%v err=%v", wrote, err)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatalf("the file changed:\n%s\n%s", before, after)
	}

	f.AnsiblePlaybook = "/usr/bin/ansible-playbook"
	if wrote, err := Save(dir, "vps", f); err != nil || !wrote {
		t.Fatalf("a changed fact was not written: wrote=%v err=%v", wrote, err)
	}
	got, _, _ := Load(dir, "vps")
	if !got.ObservedAt.Equal(f.ObservedAt) {
		t.Fatalf("observed_at = %v, want %v", got.ObservedAt, f.ObservedAt)
	}
}

func TestSaveLeavesNoTemporaryFileBehind(t *testing.T) {
	dir := t.TempDir()
	for i := range 3 {
		if _, err := Save(dir, "vps", Facts{System: "Linux", Architecture: string(rune('a' + i))}); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(filepath.Join(dir, "state", "machines"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "vps.json" {
		t.Fatalf("entries: %v", entries)
	}
}

func TestRecordWritesWhatItObserved(t *testing.T) {
	dir := t.TempDir()
	got, ok := Record(context.Background(), dir, "vps", answering(archOutput), when)
	if !ok || got.Distribution != "Archlinux" {
		t.Fatalf("ok=%v %#v", ok, got)
	}
	if _, found, _ := Load(dir, "vps"); !found {
		t.Fatal("nothing was written")
	}
}

func TestRecordWritesNothingWhenItReadNothing(t *testing.T) {
	dir := t.TempDir()
	if _, ok := Record(context.Background(), dir, "vps", answering(""), when); ok {
		t.Fatal("an empty answer was taken as an observation")
	}
	if _, err := os.Stat(filepath.Join(dir, "state")); !os.IsNotExist(err) {
		t.Fatalf("state was created: %v", err)
	}
}

// On a Mac a plain SSH command does not see the package manager's bin
// directory, so what the bootstrap reported must survive a later read that
// could not find it.
func TestRecordKeepsTheBootstrapsAnsibleOnAMac(t *testing.T) {
	dir := t.TempDir()
	reported := Facts{System: "Darwin", PkgMgr: "macports",
		AnsiblePlaybook: "/opt/local/bin/ansible-playbook-3.14", PathPrefix: []string{"/opt/local/bin", "/opt/local/sbin"}}
	if _, err := Save(dir, "studio", reported); err != nil {
		t.Fatal(err)
	}
	got, ok := Record(context.Background(), dir, "studio",
		answering("kernel=Darwin\nmachine=x86_64\nversion=15.7.9\nansible_playbook=\n"), when)
	if !ok || got.AnsiblePlaybook != reported.AnsiblePlaybook || got.PkgMgr != "macports" ||
		!slices.Equal(got.PathPrefix, reported.PathPrefix) {
		t.Fatalf("%#v", got)
	}
}

func TestRecordForgetsAnAnsibleThatWentAwayOnLinux(t *testing.T) {
	dir := t.TempDir()
	if _, err := Save(dir, "vps", Facts{System: "Linux", AnsiblePlaybook: "/usr/bin/ansible-playbook"}); err != nil {
		t.Fatal(err)
	}
	got, _ := Record(context.Background(), dir, "vps",
		answering("kernel=Linux\nmachine=x86_64\nansible_playbook=\nos-release.ID=debian\n"), when)
	if got.AnsiblePlaybook != "" {
		t.Fatalf("%#v", got)
	}
}

func TestRemoveDeletesTheFactsAndToleratesNone(t *testing.T) {
	dir := t.TempDir()
	if err := Remove(dir, "main"); err != nil {
		t.Fatalf("nothing to remove gave %v", err)
	}
	if _, err := Save(dir, "main", Facts{System: "Linux"}); err != nil {
		t.Fatal(err)
	}
	if err := Remove(dir, "main"); err != nil {
		t.Fatal(err)
	}
	if _, found, err := Load(dir, "main"); err != nil || found {
		t.Fatalf("found %v, err %v", found, err)
	}
}
