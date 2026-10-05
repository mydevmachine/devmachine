package commands

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mydevmachine/devmachine/internal/config"
	"github.com/mydevmachine/devmachine/internal/remote"
)

const (
	missingEverything = `{"missing":[{"name":"Xcode Command Line Tools","minutes":10},{"name":"Homebrew","minutes":5}]}`
	appliedBrew       = `{"ansible_playbook":"/opt/homebrew/bin/ansible-playbook","path_prefix":["/opt/homebrew/bin","/opt/homebrew/sbin"]}`
)

// fakeBootstrap is a package manager package in the config's own packages
// folder, whose bootstrap is a real sh script.
type fakeBootstrap struct{ dir string }

func (f fakeBootstrap) installed() bool {
	_, err := os.Stat(filepath.Join(f.dir, "bin", "applied"))
	return err == nil
}

// writeFakeBootstrap writes a package whose check prints missing, or nothing
// missing when it is empty, and whose apply leaves a mark when something was
// missing and prints the Homebrew paths, as the real one does.
func writeFakeBootstrap(t *testing.T, configDir, name, missing string) fakeBootstrap {
	t.Helper()
	if missing == "" {
		missing = `{"missing":[]}`
	}
	dir := filepath.Join(configDir, "packages", name)
	if err := os.MkdirAll(filepath.Join(dir, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"package.yml":    fmt.Sprintf("format: 1\nname: %s\nscope: machine\nplatforms: [macos]\nbootstrap: bin/bootstrap\n", name),
		"bin/check.json": missing + "\n",
		"bin/apply.json": appliedBrew + "\n",
		"bin/bootstrap": "#!/bin/sh\nhere=$(dirname \"$0\")\ncase \"$1\" in\n" +
			"check) cat \"$here/check.json\" ;;\n" +
			"apply) grep -q '\"missing\":\\[\\]' \"$here/check.json\" || { touch \"$here/applied\"; echo installing >&2; }\n" +
			"\tcat \"$here/apply.json\" ;;\nesac\n",
	}
	for file, body := range files {
		if err := os.WriteFile(filepath.Join(dir, file), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return fakeBootstrap{dir: dir}
}

func withTerminal(t *testing.T, yes bool) {
	t.Helper()
	t.Cleanup(swap(&hasTerminal, func(io.Reader) bool { return yes }))
}

func recordedObservations(t *testing.T) map[string]observed {
	t.Helper()
	got := map[string]observed{}
	t.Cleanup(swap(&recordObserved, func(_, machine string, o observed) error {
		got[machine] = o
		return nil
	}))
	return got
}

func TestSetupOnAPreparedSelfMachineInstallsNothing(t *testing.T) {
	dir := selfConfig(t)
	fake := writeFakeBootstrap(t, dir, "mac-brew", "")
	seen := recordedObservations(t)

	out, err := execute(t, "--config", dir, "setup")
	if err != nil {
		t.Fatalf("setup returned %v (%s)", err, out)
	}
	if fake.installed() || strings.Contains(out, "Install them now?") || strings.Contains(out, "installing") {
		t.Fatalf("it installed or asked when nothing was missing: %s", out)
	}
	if !strings.Contains(out, "mac is already prepared: ansible-playbook is /opt/homebrew/bin/ansible-playbook.") {
		t.Fatalf("got %s", out)
	}
	if seen["mac"].AnsiblePlaybook != "/opt/homebrew/bin/ansible-playbook" {
		t.Fatalf("recorded %#v", seen)
	}
	skills := strings.Index(out, "Agent skills")
	next := strings.Index(out, "Next: `devmachine doctor`")
	if skills < 0 || next < skills {
		t.Fatalf("the skills prompt and Next are out of order: %s", out)
	}
}

func TestSetupOnASelfMachineAsksBeforeInstalling(t *testing.T) {
	for _, answer := range []string{"n", ""} {
		dir := selfConfig(t)
		fake := writeFakeBootstrap(t, dir, "mac-brew", missingEverything)
		withTerminal(t, true)

		cmd := NewRootCmd()
		var out strings.Builder
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		cmd.SetIn(strings.NewReader(answer + "\n"))
		cmd.SetArgs([]string{"--config", dir, "setup"})
		err := cmd.Execute()

		if !errors.Is(err, errPrerequisitesDeclined) || err.Error() != "nothing was installed; setup stops here" {
			t.Fatalf("%q: got %v", answer, err)
		}
		if fake.installed() {
			t.Fatalf("%q: it installed after a no", answer)
		}
		for _, want := range []string{"Xcode Command Line Tools (about 10 minutes)", "Homebrew (about 5 minutes)", "Install them now?"} {
			if !strings.Contains(out.String(), want) {
				t.Fatalf("%q: the list does not show %q: %s", answer, want, out.String())
			}
		}
	}
}

func TestSetupOnASelfMachineInstallsAfterAYes(t *testing.T) {
	dir := selfConfig(t)
	fake := writeFakeBootstrap(t, dir, "mac-brew", missingEverything)
	withTerminal(t, true)

	cmd := NewRootCmd()
	var out strings.Builder
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(strings.NewReader("y\n"))
	cmd.SetArgs([]string{"--config", dir, "setup"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("setup returned %v (%s)", err, out.String())
	}
	if !fake.installed() || !strings.Contains(out.String(), "installing\n") {
		t.Fatalf("it did not install, or hid the progress: %s", out.String())
	}
	if !strings.Contains(out.String(), "mac is prepared: ansible-playbook is /opt/homebrew/bin/ansible-playbook.") {
		t.Fatalf("got %s", out.String())
	}
}

func TestInstallPrerequisitesIsConsentWithoutATerminal(t *testing.T) {
	dir := selfConfig(t)
	fake := writeFakeBootstrap(t, dir, "mac-brew", missingEverything)

	out, err := execute(t, "--config", dir, "setup", "--install-prerequisites")
	if err != nil {
		t.Fatalf("setup returned %v (%s)", err, out)
	}
	if !fake.installed() {
		t.Fatalf("--install-prerequisites did not install: %s", out)
	}
}

// TestYesNeverInstallsPrerequisites: the macOS app passes --yes with nobody
// watching, and it must not mean "install the Command Line Tools".
func TestYesNeverInstallsPrerequisites(t *testing.T) {
	dir := writeConfigDir(t, "machines:\n  - name: server\n    hosts: [203.0.113.10]\n")
	fake := writeFakeBootstrap(t, dir, "mac-brew", missingEverything)

	out, err := execute(t, "--config", dir, "machines", "add", "--self", "mac", "--yes")
	if err == nil {
		t.Fatalf("--yes went ahead: %s", out)
	}
	want := "nothing was installed: run again with --install-prerequisites to install Xcode Command Line Tools and Homebrew"
	if err.Error() != want {
		t.Fatalf("got %q, want %q", err, want)
	}
	if fake.installed() {
		t.Fatal("--yes installed prerequisites")
	}
}

func TestASelfMachineThatListsMacPortsUsesItsBootstrap(t *testing.T) {
	dir := configWith(t, "machines:\n  - name: mac\n    self: true\n    packages: [mac-ports]\n")
	brew := writeFakeBootstrap(t, dir, "mac-brew", missingEverything)
	ports := writeFakeBootstrap(t, dir, "mac-ports", missingEverything)

	if out, err := execute(t, "--config", dir, "setup", "--install-prerequisites"); err != nil {
		t.Fatalf("setup returned %v (%s)", err, out)
	}
	if brew.installed() || !ports.installed() {
		t.Fatalf("brew ran %v, ports ran %v", brew.installed(), ports.installed())
	}
}

func TestASelfMachineWithNoBootstrapPackageSaysToPin(t *testing.T) {
	_, err := execute(t, "--config", selfConfig(t), "setup")
	if err == nil || !strings.Contains(err.Error(), "devmachine packages pin") || !strings.Contains(err.Error(), "mac-brew") {
		t.Fatalf("got %v", err)
	}
}

func TestPackageManagerTakesOnlyBrewOrPorts(t *testing.T) {
	_, err := execute(t, "--config", selfConfig(t), "setup", "--package-manager", "nix")
	if err == nil || !strings.Contains(err.Error(), "brew") || !strings.Contains(err.Error(), "ports") {
		t.Fatalf("got %v", err)
	}
}

// macClient is a Mac reached over SSH, answering the commands setup runs on
// one.
type macClient struct {
	managers string
	check    string
	apply    string
	failRun  string
	commands []string
	uploads  map[string][]string
}

func (c *macClient) Run(_ context.Context, command string) (string, error) {
	c.commands = append(c.commands, command)
	switch {
	case command == remote.MacManagersCommand:
		return c.managers, nil
	case strings.HasSuffix(command, " check"):
		if c.failRun == "check" {
			return c.check, errors.New("exit status 1")
		}
		return c.check, nil
	}
	return "", nil
}

func (c *macClient) RunInput(ctx context.Context, command string, _ io.Reader) (string, error) {
	return c.Run(ctx, command)
}

func (c *macClient) Stream(_ context.Context, command string, stdout, stderr io.Writer) error {
	c.commands = append(c.commands, command)
	_, _ = io.WriteString(stderr, "progress from the Mac\n")
	_, _ = io.WriteString(stdout, c.apply)
	if c.failRun == "apply" {
		return errors.New("exit status 1")
	}
	return nil
}

func (c *macClient) Upload(_ context.Context, dir string, tarball io.Reader) error {
	zipped, err := gzip.NewReader(tarball)
	if err != nil {
		return err
	}
	archive := tar.NewReader(zipped)
	for {
		header, err := archive.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		if c.uploads == nil {
			c.uploads = map[string][]string{}
		}
		c.uploads[dir] = append(c.uploads[dir], header.Name)
	}
	return nil
}

func (c *macClient) Close() error { return nil }

// onAMac makes setup's connection reach mac, a Mac whose admin has
// passwordless sudo.
func onAMac(t *testing.T, mac *macClient) {
	t.Helper()
	t.Cleanup(swap(&dial, func(context.Context, config.Machine, string) (remote.Client, string, error) {
		return mac, "203.0.113.20", nil
	}))
	t.Cleanup(swap(&detectSystem, func(context.Context, remote.Client) (remote.System, error) {
		return remote.System{Kernel: remote.KernelDarwin, Version: "15.7.9"}, nil
	}))
	t.Cleanup(swap(&checkRoot, func(context.Context, remote.Client, string) error { return nil }))
	t.Cleanup(swap(&tailscaleSSH, func(remote.Client) bool { return false }))
	t.Cleanup(swap(&installAnsible, func(context.Context, remote.Client, io.Writer) error {
		t.Fatal("the Linux Ansible install ran on a Mac")
		return nil
	}))
}

func macConfig(t *testing.T, packages string) string {
	t.Helper()
	return configWith(t, "machines:\n  - name: studio\n    hosts: [203.0.113.20]\n    user: alice\n"+packages)
}

func TestSetupOnAMacWithHomebrewAddsMacBrewAndRunsItsBootstrap(t *testing.T) {
	dir := macConfig(t, "")
	writeFakeBootstrap(t, dir, "mac-brew", "")
	writeFakeBootstrap(t, dir, "mac-ports", "")
	mac := &macClient{managers: "brew\n", check: `{"missing":[]}`, apply: appliedBrew}
	onAMac(t, mac)
	seen := recordedObservations(t)

	out, err := execute(t, "--config", dir, "setup")
	if err != nil {
		t.Fatalf("setup returned %v (%s)", err, out)
	}
	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(cfg.Machines[0].Packages, []string{"mac-brew"}) {
		t.Fatalf("packages = %v", cfg.Machines[0].Packages)
	}
	if !slices.Contains(mac.uploads[remote.MacBootstrapDir], "mac-brew/bin/bootstrap") {
		t.Fatalf("uploaded %v", mac.uploads)
	}
	joined := strings.Join(mac.commands, "\n")
	check := "sh '/opt/devmachine/bootstrap/mac-brew/bin/bootstrap' check"
	if !slices.Contains(mac.commands, check) || !slices.Contains(mac.commands, strings.TrimSuffix(check, "check")+"apply") {
		t.Fatalf("the bootstrap did not run as the admin login: %s", joined)
	}
	if !strings.Contains(joined, "rm -rf ") || !strings.Contains(joined, "/opt/devmachine/bootstrap/mac-brew'\\''") {
		t.Fatalf("an older copy is not emptied first: %s", joined)
	}
	if got := seen["studio"]; got.AnsiblePlaybook != "/opt/homebrew/bin/ansible-playbook" ||
		!slices.Equal(got.PathPrefix, []string{"/opt/homebrew/bin", "/opt/homebrew/sbin"}) {
		t.Fatalf("recorded %#v", got)
	}
	if !strings.Contains(out, "studio is already prepared: ansible-playbook is /opt/homebrew/bin/ansible-playbook.") {
		t.Fatalf("got %s", out)
	}
}

func TestSetupOnAMacWithMacPortsPicksMacPorts(t *testing.T) {
	dir := macConfig(t, "")
	writeFakeBootstrap(t, dir, "mac-brew", "")
	writeFakeBootstrap(t, dir, "mac-ports", "")
	onAMac(t, &macClient{managers: "ports\n", check: `{"missing":[]}`, apply: appliedBrew})

	if out, err := execute(t, "--config", dir, "setup"); err != nil {
		t.Fatalf("setup returned %v (%s)", err, out)
	}
	cfg, _ := config.Load(dir)
	if !slices.Equal(cfg.Machines[0].Packages, []string{"mac-ports"}) {
		t.Fatalf("packages = %v", cfg.Machines[0].Packages)
	}
}

func TestSetupOnAMacWithNeitherManagerAndNoTerminalStops(t *testing.T) {
	for managers, found := range map[string]string{"": "neither Homebrew nor MacPorts", "brew\nports\n": "both Homebrew and MacPorts"} {
		dir := macConfig(t, "")
		writeFakeBootstrap(t, dir, "mac-brew", "")
		mac := &macClient{managers: managers}
		onAMac(t, mac)

		_, err := execute(t, "--config", dir, "setup")
		want := "the Mac has " + found + ": run again with --package-manager brew or --package-manager ports"
		if err == nil || err.Error() != want {
			t.Fatalf("got %v, want %q", err, want)
		}
		if mac.uploads != nil {
			t.Fatal("it copied a package before the choice was made")
		}
	}
}

func TestPackageManagerFlagChoosesForAMacWithNeither(t *testing.T) {
	dir := macConfig(t, "")
	writeFakeBootstrap(t, dir, "mac-ports", "")
	onAMac(t, &macClient{check: `{"missing":[]}`, apply: appliedBrew})

	if out, err := execute(t, "--config", dir, "setup", "--package-manager", "ports"); err != nil {
		t.Fatalf("setup returned %v (%s)", err, out)
	}
	cfg, _ := config.Load(dir)
	if !slices.Equal(cfg.Machines[0].Packages, []string{"mac-ports"}) {
		t.Fatalf("packages = %v", cfg.Machines[0].Packages)
	}
}

func TestSetupOnAMacWithBothManagersAsks(t *testing.T) {
	dir := macConfig(t, "")
	writeFakeBootstrap(t, dir, "mac-ports", "")
	onAMac(t, &macClient{managers: "brew\nports\n", check: `{"missing":[]}`, apply: appliedBrew})
	withTerminal(t, true)

	cmd := NewRootCmd()
	var out strings.Builder
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(strings.NewReader("2\n"))
	cmd.SetArgs([]string{"--config", dir, "setup"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("setup returned %v (%s)", err, out.String())
	}
	if !strings.Contains(out.String(), "Which package manager should it use?") {
		t.Fatalf("it did not ask: %s", out.String())
	}
	cfg, _ := config.Load(dir)
	if !slices.Equal(cfg.Machines[0].Packages, []string{"mac-ports"}) {
		t.Fatalf("packages = %v", cfg.Machines[0].Packages)
	}
}

func TestAMacThatListsBothManagerPackagesIsRefused(t *testing.T) {
	dir := macConfig(t, "    packages: [mac-brew, mac-ports]\n")
	writeFakeBootstrap(t, dir, "mac-brew", "")
	writeFakeBootstrap(t, dir, "mac-ports", "")
	onAMac(t, &macClient{managers: "brew\n"})

	_, err := execute(t, "--config", dir, "setup")
	want := `machine "studio" lists mac-brew and mac-ports: keep one`
	if err == nil || err.Error() != want {
		t.Fatalf("got %v, want %q", err, want)
	}
}

func TestAMacThatListsItsManagerPackageKeepsIt(t *testing.T) {
	dir := macConfig(t, "    packages: [mac-ports]\n")
	writeFakeBootstrap(t, dir, "mac-ports", "")
	mac := &macClient{managers: "brew\n", check: `{"missing":[]}`, apply: appliedBrew}
	onAMac(t, mac)

	if out, err := execute(t, "--config", dir, "setup"); err != nil {
		t.Fatalf("setup returned %v (%s)", err, out)
	}
	if slices.Contains(mac.commands, remote.MacManagersCommand) {
		t.Fatal("it looked for a manager the machine already chose")
	}
	if !slices.Contains(mac.uploads[remote.MacBootstrapDir], "mac-ports/bin/bootstrap") {
		t.Fatalf("uploaded %v", mac.uploads)
	}
}

func TestSetupOnAMacWithYesStillAsksForConsent(t *testing.T) {
	dir := macConfig(t, "    packages: [mac-brew]\n")
	writeFakeBootstrap(t, dir, "mac-brew", "")
	mac := &macClient{check: missingEverything, apply: appliedBrew}
	onAMac(t, mac)

	_, err := execute(t, "--config", dir, "setup", "--yes")
	if err == nil || !strings.Contains(err.Error(), "--install-prerequisites") {
		t.Fatalf("got %v", err)
	}
	for _, command := range mac.commands {
		if strings.HasSuffix(command, " apply") {
			t.Fatal("--yes ran the bootstrap's apply")
		}
	}
}

func TestSetupOnAMacStreamsTheInstallAfterConsent(t *testing.T) {
	dir := macConfig(t, "    packages: [mac-brew]\n")
	writeFakeBootstrap(t, dir, "mac-brew", "")
	onAMac(t, &macClient{check: missingEverything, apply: appliedBrew})

	out, err := execute(t, "--config", dir, "setup", "--install-prerequisites")
	if err != nil {
		t.Fatalf("setup returned %v (%s)", err, out)
	}
	if !strings.Contains(out, "progress from the Mac") || !strings.Contains(out, "studio is prepared") {
		t.Fatalf("got %s", out)
	}
}

func TestABootstrapFailureSaysTheStepAndWhatToDo(t *testing.T) {
	dir := macConfig(t, "    packages: [mac-brew]\n")
	writeFakeBootstrap(t, dir, "mac-brew", "")
	onAMac(t, &macClient{
		check:   missingEverything,
		apply:   `{"error":{"step":"homebrew","message":"the Homebrew installer failed; its output is above."}}`,
		failRun: "apply",
	})

	_, err := execute(t, "--config", dir, "setup", "--install-prerequisites")
	want := "the bootstrap stopped at homebrew: the Homebrew installer failed; its output is above."
	if err == nil || err.Error() != want {
		t.Fatalf("got %v, want %q", err, want)
	}
}

// TestMachinesAddOnAMacWritesTheManagerPackageWithTheMachine: the machine is
// written only after the bootstrap, so the package goes in with it.
func TestMachinesAddOnAMacWritesTheManagerPackageWithTheMachine(t *testing.T) {
	stubBootstrap(t, bootstrapStubs{keyWorks: true, kernel: remote.KernelDarwin})
	dir := writeConfigDir(t, "machines:\n  - name: server\n    hosts: [203.0.113.10]\n")
	writeFakeBootstrap(t, dir, "mac-brew", "")
	mac := &macClient{managers: "brew\n", check: `{"missing":[]}`, apply: appliedBrew}
	t.Cleanup(swap(&dialWith, func(context.Context, config.Machine, string, remote.Auth) (remote.Client, string, error) {
		return mac, "203.0.113.20", nil
	}))

	out := &strings.Builder{}
	err := runMachinesAdd(context.Background(), dir, strings.NewReader(""), out, setupOptions{
		address: "203.0.113.20", name: "studio", user: "alice", port: 22, key: "new",
		noEssentials: true, noAliases: true, hostKey: commandHostKey(t),
	})
	if err != nil {
		t.Fatalf("machines add returned %v (%s)", err, out)
	}
	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	m, err := cfg.Machine("studio")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(m.Packages, []string{"mac-brew"}) {
		t.Fatalf("packages = %v", m.Packages)
	}
}
