package doctor

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mydevmachine/devmachine/internal/config"
	"github.com/mydevmachine/devmachine/internal/facts"
	"github.com/mydevmachine/devmachine/internal/remote"
)

const macMachine = "machines:\n  - name: studio\n    hosts: [203.0.113.20]\n    user: alice\n"

// macDir is a configuration with one Mac reached over SSH, its host key
// trusted, and the packages named written locally with a bootstrap.
func macDir(t *testing.T, machinePackages string, local ...string) string {
	t.Helper()
	dir := configDir(t, macMachine+machinePackages)
	for _, name := range local {
		pkg := filepath.Join(dir, "packages", name)
		if err := os.MkdirAll(filepath.Join(pkg, "bin"), 0o755); err != nil {
			t.Fatal(err)
		}
		manifest := "format: 1\nname: " + name + "\nscope: machine\nplatforms: [macos]\nbootstrap: bin/bootstrap\n"
		if err := os.WriteFile(filepath.Join(pkg, "package.yml"), []byte(manifest), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(pkg, "bin", "bootstrap"), []byte("#!/bin/sh\n# "+name+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func macClient(extra map[string]string) fakeClient {
	out := map[string]string{
		unameCommand:             "Darwin\n",
		remote.MacVersionCommand: "15.7.9\n",
		macAnsibleCommand(""):    "/opt/homebrew/bin/ansible-playbook\n",
	}
	for k, v := range extra {
		out[k] = v
	}
	return fakeClient{out: out}
}

func runOnMac(t *testing.T, dir string, client fakeClient) []Check {
	t.Helper()
	return Run(context.Background(), dir, "", dialling(client), nil)
}

func TestDoctorOnAMacPassesTheSystemAndFindsAnsibleOffPath(t *testing.T) {
	checks := runOnMac(t, macDir(t, "    packages: [mac-brew]\n", "mac-brew"),
		macClient(map[string]string{"sh -s check": `{"missing":[]}`}))

	if got := find(t, checks, CheckOperatingSystem); got.Status != StatusPass || got.Detail != "macos 15.7.9" {
		t.Fatalf("got %#v", got)
	}
	if got := find(t, checks, CheckAnsible); got.Status != StatusPass || got.Detail != "/opt/homebrew/bin/ansible-playbook" {
		t.Fatalf("got %#v", got)
	}
	if got := find(t, checks, CheckPrerequisites); got.Status != StatusPass || !strings.Contains(got.Detail, "mac-brew") {
		t.Fatalf("got %#v", got)
	}
}

func TestDoctorOnAMacLooksFirstWhereTheBootstrapSaidAnsibleIs(t *testing.T) {
	dir := macDir(t, "")
	reported := facts.Reported(facts.Facts{ObservedAt: time.Now()}, "/opt/local/bin/ansible-playbook-3.14",
		[]string{"/opt/local/bin", "/opt/local/sbin"})
	if _, err := facts.Save(dir, "studio", reported); err != nil {
		t.Fatal(err)
	}
	client := macClient(map[string]string{
		macAnsibleCommand("/opt/local/bin/ansible-playbook-3.14"): "/opt/local/bin/ansible-playbook-3.14\n",
		remote.MacManagersCommand:                                 "",
	})
	checks := runOnMac(t, dir, client)
	if got := find(t, checks, CheckAnsible); got.Status != StatusPass || got.Detail != "/opt/local/bin/ansible-playbook-3.14" {
		t.Fatalf("got %#v", got)
	}

	command := macAnsibleCommand("/opt/local/bin/ansible-playbook-3.14")
	for _, place := range []string{`"$1"`, "/opt/homebrew/bin/ansible-playbook",
		"/usr/local/bin/ansible-playbook", "/opt/local/bin/ansible-playbook", "$HOME/.local/bin/ansible-playbook"} {
		if !strings.Contains(command, place) {
			t.Fatalf("it does not look at %s: %s", place, command)
		}
	}
	if !strings.HasSuffix(command, " '/opt/local/bin/ansible-playbook-3.14'") {
		t.Fatalf("the reported path is not the script's argument: %s", command)
	}
	if strings.Index(command, `"$1"`) > strings.Index(command, "/opt/homebrew") {
		t.Fatalf("the reported path is not looked at first: %s", command)
	}
}

func TestDoctorFindsAnsibleWhenTheAdminLoginShellIsZsh(t *testing.T) {
	zsh, err := exec.LookPath("zsh")
	if err != nil {
		t.Skip("no zsh here")
	}
	bin := t.TempDir()
	playbook := filepath.Join(bin, "ansible-playbook")
	if err := os.WriteFile(playbook, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	out, err := exec.Command(zsh, "-f", "-c", macAnsibleCommand(playbook)).CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != playbook {
		t.Fatalf("zsh ran it to %v: %s", err, out)
	}
}

func TestDoctorListsWhatAMacLacksAsWarnings(t *testing.T) {
	checks := runOnMac(t, macDir(t, "    packages: [mac-brew]\n", "mac-brew"), macClient(map[string]string{
		"sh -s check": `{"missing":[{"name":"Xcode Command Line Tools","minutes":10},{"name":"Homebrew","minutes":5}]}`,
	}))

	for _, name := range []string{"Xcode Command Line Tools", "Homebrew"} {
		got := find(t, checks, PrerequisitePrefix+name)
		if got.Status != StatusWarn || !strings.Contains(got.Detail, "devmachine setup") {
			t.Fatalf("%s: got %#v", name, got)
		}
	}
	for _, c := range checks {
		switch c.Status {
		case StatusPass, StatusWarn, StatusFail, StatusSkip:
		default:
			t.Fatalf("a status the app does not know: %#v", c)
		}
	}
}

func TestDoctorOnAMacUsesTheManagerItHas(t *testing.T) {
	dir := macDir(t, "", "mac-brew", "mac-ports")
	checks := runOnMac(t, dir, macClient(map[string]string{
		remote.MacManagersCommand: "ports\n",
		"sh -s check":             `{"missing":[]}`,
	}))
	if got := find(t, checks, CheckPrerequisites); got.Status != StatusPass || !strings.Contains(got.Detail, "mac-ports") {
		t.Fatalf("got %#v", got)
	}
}

func TestDoctorOnAMacWithNoManagerChosenSkipsThePrerequisites(t *testing.T) {
	checks := runOnMac(t, macDir(t, "", "mac-brew", "mac-ports"), macClient(map[string]string{
		remote.MacManagersCommand: "",
	}))
	got := find(t, checks, CheckPrerequisites)
	if got.Status != StatusSkip || !strings.Contains(got.Detail, "--package-manager") {
		t.Fatalf("got %#v", got)
	}
}

func TestDoctorOnASelfMacAsksMacBrewByDefault(t *testing.T) {
	dir := selfDir(t)
	if err := os.MkdirAll(filepath.Join(dir, "packages", "mac-brew", "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := "format: 1\nname: mac-brew\nscope: machine\nbootstrap: bin/bootstrap\n"
	if err := os.WriteFile(filepath.Join(dir, "packages", "mac-brew", "package.yml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "packages", "mac-brew", "bin", "bootstrap"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	client := selfClient("Darwin")
	client.out["sh -s check"] = `{"missing":[]}`
	checks := Run(context.Background(), dir, "", dialling(client), nil)
	if got := find(t, checks, CheckPrerequisites); got.Status != StatusPass || !strings.Contains(got.Detail, "mac-brew") {
		t.Fatalf("got %#v", got)
	}
}

func TestDoctorOnLinuxReportsNoPrerequisites(t *testing.T) {
	client := fakeClient{out: map[string]string{
		unameCommand: "Linux\n", osReleaseCommand: "ID=debian\n", ansibleCommand: "/usr/bin/ansible-playbook\n",
	}}
	dial := func(context.Context, config.Machine, string) (remote.Client, string, error) {
		return client, "203.0.113.10", nil
	}
	checks := Run(context.Background(), configDir(t, "machines:\n  - name: main\n    hosts: [203.0.113.10]\n"), "", dial, nil)
	for _, c := range checks {
		if strings.HasPrefix(c.Name, "prerequisite") {
			t.Fatalf("a Linux machine got %#v", c)
		}
	}
}

const macWorkspaces = "workspaces:\n  - name: bob\n    machine: studio\n  - name: carol\n    machine: studio\n    user: carol2\n"

func TestDoctorOnAMacWarnsWhenRemoteLoginLeavesAWorkspaceOut(t *testing.T) {
	dir := macDir(t, macWorkspaces)
	checks := runOnMac(t, dir, macClient(map[string]string{
		remoteLoginCommand([]string{"bob", "carol2"}): "bob outside\ncarol2 member\n",
	}))

	bob := find(t, checks, SSHAccessPrefix+"bob")
	if bob.Status != StatusWarn {
		t.Fatalf("got %#v", bob)
	}
	for _, want := range []string{"Remote Login", "devmachine sync", "All users"} {
		if !strings.Contains(bob.Detail, want) {
			t.Fatalf("the detail does not say %q: %q", want, bob.Detail)
		}
	}
	if carol := find(t, checks, SSHAccessPrefix+"carol"); carol.Status != StatusPass {
		t.Fatalf("got %#v", carol)
	}
}

func TestDoctorOnAMacPassesEveryWorkspaceWhenRemoteLoginAllowsAllUsers(t *testing.T) {
	checks := runOnMac(t, macDir(t, macWorkspaces), macClient(map[string]string{
		remoteLoginCommand([]string{"bob", "carol2"}): "all\n",
	}))
	for _, name := range []string{"bob", "carol"} {
		if got := find(t, checks, SSHAccessPrefix+name); got.Status != StatusPass {
			t.Fatalf("%s: got %#v", name, got)
		}
	}
}

func TestDoctorOnAMacSkipsAWorkspaceWhoseAccountIsNotThereYet(t *testing.T) {
	checks := runOnMac(t, macDir(t, macWorkspaces), macClient(map[string]string{
		remoteLoginCommand([]string{"bob", "carol2"}): "bob absent\ncarol2 member\n",
	}))
	if got := find(t, checks, SSHAccessPrefix+"bob"); got.Status != StatusSkip {
		t.Fatalf("got %#v", got)
	}
}

func TestDoctorOnAMacWarnsWhenItCannotReadRemoteLogin(t *testing.T) {
	client := macClient(nil)
	client.err = map[string]error{remoteLoginCommand([]string{"bob", "carol2"}): errors.New("exit status 1")}
	checks := runOnMac(t, macDir(t, macWorkspaces), client)
	if got := find(t, checks, SSHAccessPrefix+"bob"); got.Status != StatusWarn {
		t.Fatalf("got %#v", got)
	}
}

func TestRemoteLoginCommandAsksOnceForEveryAccount(t *testing.T) {
	command := remoteLoginCommand([]string{"bob", "carol2"})
	for _, want := range []string{"/Groups/com.apple.access_ssh", "'bob'", "'carol2'", "checkmember"} {
		if !strings.Contains(command, want) {
			t.Fatalf("it does not hold %q: %s", want, command)
		}
	}
}

func TestDoctorOnLinuxHasNoSSHAccessCheck(t *testing.T) {
	client := fakeClient{out: map[string]string{
		unameCommand: "Linux\n", osReleaseCommand: "ID=debian\n", ansibleCommand: "/usr/bin/ansible-playbook\n",
	}}
	dial := func(context.Context, config.Machine, string) (remote.Client, string, error) {
		return client, "203.0.113.10", nil
	}
	dir := configDir(t, "machines:\n  - name: main\n    hosts: [203.0.113.10]\nworkspaces:\n  - name: bob\n")
	for _, c := range Run(context.Background(), dir, "", dial, nil) {
		if strings.HasPrefix(c.Name, SSHAccessPrefix) {
			t.Fatalf("a Linux machine got %#v", c)
		}
	}
}
