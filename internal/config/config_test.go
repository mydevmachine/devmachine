package config

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestDirPrefersTheFlagOverEverything(t *testing.T) {
	t.Setenv("DEVMACHINE_CONFIG", "/from/env")
	t.Setenv("XDG_CONFIG_HOME", "/xdg")

	dir, source, err := Dir("/from/flag")
	if err != nil {
		t.Fatalf("Dir returned %v", err)
	}
	if dir != "/from/flag" || source != SourceFlag {
		t.Fatalf("got (%q, %q)", dir, source)
	}
}

func TestDirFallsBackToTheEnvironment(t *testing.T) {
	t.Setenv("DEVMACHINE_CONFIG", "/from/env")
	t.Setenv("XDG_CONFIG_HOME", "/xdg")

	dir, source, _ := Dir("")
	if dir != "/from/env" || source != SourceEnv {
		t.Fatalf("got (%q, %q)", dir, source)
	}
}

func TestDirFallsBackToXDG(t *testing.T) {
	t.Setenv("DEVMACHINE_CONFIG", "")
	t.Setenv("XDG_CONFIG_HOME", "/xdg")

	dir, source, _ := Dir("")
	if dir != filepath.Join("/xdg", "devmachine") || source != SourceXDG {
		t.Fatalf("got (%q, %q)", dir, source)
	}
}

func TestDirFallsBackToTheHomeDirectory(t *testing.T) {
	t.Setenv("DEVMACHINE_CONFIG", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "/home/alice")

	dir, source, _ := Dir("")
	if dir != filepath.Join("/home/alice", ".config", "devmachine") || source != SourceDefault {
		t.Fatalf("got (%q, %q)", dir, source)
	}
}

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

const twoMachines = `
machines:
  - name: main
    hosts:
      - tailscale:vps
      - 203.0.113.10
  - name: sandbox
    hosts:
      - 127.0.0.1
    port: 52862
    key: /keys/sandbox
workspaces:
  - name: alice
    machine: main
  - name: bob
    machine: sandbox
    user: bob-dev
domain: example.com
`

func TestLoadReadsMachinesInOrder(t *testing.T) {
	c, err := Load(writeConfig(t, twoMachines))
	if err != nil {
		t.Fatalf("Load returned %v", err)
	}
	if len(c.Machines) != 2 {
		t.Fatalf("got %d machines", len(c.Machines))
	}
	m := c.Machines[0]
	if m.Name != "main" || len(m.Hosts) != 2 || m.Hosts[0].Address != "tailscale:vps" {
		t.Fatalf("first machine = %#v", m)
	}
}

func TestLoadDefaultsTheAdminUserAndPortPerMachine(t *testing.T) {
	c, _ := Load(writeConfig(t, twoMachines))

	if c.Machines[0].User != "root" || c.Machines[0].Port != 22 {
		t.Fatalf("main did not get the defaults: %#v", c.Machines[0])
	}
	if c.Machines[1].Port != 52862 {
		t.Fatalf("sandbox lost its explicit port: %d", c.Machines[1].Port)
	}
}

func TestLoadAssociatesMachinesWithTheConfigurationTrustStore(t *testing.T) {
	dir := writeConfig(t, "machines:\n  - name: main\n    hosts: [203.0.113.10]\n")
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, KnownHostsFileName)
	if cfg.Machines[0].KnownHostsFile != want {
		t.Fatalf("got %q, want %q", cfg.Machines[0].KnownHostsFile, want)
	}
}

func TestLoadResolvesTheAgentKeyFileOnlyWhenAgentKeyIsSet(t *testing.T) {
	dir := writeConfig(t, "machines:\n  - name: main\n    hosts: [203.0.113.10]\n    agent_key: "+testAgentKey+"\n"+
		"  - name: sandbox\n    hosts: [127.0.0.1]\n")
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "keys", "main.agent.pub")
	if cfg.Machines[0].AgentKeyFile != want {
		t.Fatalf("got %q, want %q", cfg.Machines[0].AgentKeyFile, want)
	}
	if cfg.Machines[1].AgentKeyFile != "" {
		t.Fatalf("a machine with no agent_key got a file: %q", cfg.Machines[1].AgentKeyFile)
	}
}

func TestMachineAgentKeyFilePathIsRuntimeOnly(t *testing.T) {
	body, err := yaml.Marshal(Machine{Name: "main", AgentKeyFile: "/private/config/keys/main.agent.pub"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "agent.pub") {
		t.Fatalf("runtime agent key file path leaked into YAML:\n%s", body)
	}
}

func TestMachineTrustStorePathIsRuntimeOnly(t *testing.T) {
	body, err := yaml.Marshal(Machine{Name: "main", KnownHostsFile: "/private/config/known_hosts"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "known_hosts") || strings.Contains(string(body), "/private/config") {
		t.Fatalf("runtime trust-store path leaked into YAML:\n%s", body)
	}
}

func TestAWorkspaceUsesItsOwnNameAsTheLinuxUser(t *testing.T) {
	c, _ := Load(writeConfig(t, twoMachines))

	w, err := c.Workspace("alice")
	if err != nil {
		t.Fatalf("Workspace returned %v", err)
	}
	if w.LinuxUser() != "alice" {
		t.Fatalf("LinuxUser = %q, want the workspace name", w.LinuxUser())
	}
}

func TestAWorkspaceCanOverrideTheLinuxUser(t *testing.T) {
	c, _ := Load(writeConfig(t, twoMachines))

	w, _ := c.Workspace("bob")
	if w.LinuxUser() != "bob-dev" {
		t.Fatalf("LinuxUser = %q, want the override", w.LinuxUser())
	}
}

func TestMachineForAWorkspaceResolvesThroughTheMapping(t *testing.T) {
	c, _ := Load(writeConfig(t, twoMachines))

	m, w, err := c.MachineFor("bob")
	if err != nil {
		t.Fatalf("MachineFor returned %v", err)
	}
	if m.Name != "sandbox" {
		t.Fatalf("bob resolved to machine %q, want sandbox", m.Name)
	}
	if w.LinuxUser() != "bob-dev" {
		t.Fatalf("workspace = %#v", w)
	}
}

func TestAnUnknownWorkspaceListsTheOnesThatExist(t *testing.T) {
	c, _ := Load(writeConfig(t, twoMachines))

	_, err := c.Workspace("nope")
	if err == nil {
		t.Fatal("expected an error for an unknown workspace")
	}
	for _, want := range []string{"alice", "bob"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the error does not list %q: %v", want, err)
		}
	}
}

func TestMachineByNameFindsIt(t *testing.T) {
	c, _ := Load(writeConfig(t, twoMachines))

	m, err := c.Machine("sandbox")
	if err != nil {
		t.Fatalf("Machine returned %v", err)
	}
	if m.Port != 52862 {
		t.Fatalf("got %#v", m)
	}
}

func TestMachineWithNoNameRefusesToGuessBetweenSeveral(t *testing.T) {
	c, _ := Load(writeConfig(t, twoMachines))

	_, err := c.Machine("")
	if err == nil {
		t.Fatal("expected an error rather than a guess when there are several machines")
	}
	for _, want := range []string{"main", "sandbox", "--machine"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the error does not say how to choose (%q): %v", want, err)
		}
	}
}

func TestMachineWithNoNameTakesTheOnlyOne(t *testing.T) {
	c, _ := Load(writeConfig(t, "machines:\n  - name: only\n    hosts: [203.0.113.10]\n"))

	m, err := c.Machine("")
	if err != nil {
		t.Fatalf("Machine returned %v", err)
	}
	if m.Name != "only" {
		t.Fatalf("got %q", m.Name)
	}
}

func TestValidateRejectsAConfigWithNoMachine(t *testing.T) {
	err := Config{Domain: "example.com"}.Validate()
	if err == nil || !strings.Contains(err.Error(), "machines") {
		t.Fatalf("got %v", err)
	}
}

func TestValidateRejectsAMachineWithNoName(t *testing.T) {
	c := Config{Machines: []Machine{{Hosts: []Host{{Address: "203.0.113.10"}}, User: "root", Port: 22}}}
	if err := c.Validate(); err == nil {
		t.Fatal("expected an error for a machine with no name")
	}
}

func TestValidateRejectsUnsafeMachineIdentityNames(t *testing.T) {
	for _, name := range []string{" main", "main server", "main,backup", "[main]", "main*", "!main", "main\nbackup"} {
		t.Run(name, func(t *testing.T) {
			c := Config{Machines: []Machine{{
				Name: name, Hosts: []Host{{Address: "203.0.113.10"}}, User: "root", Port: 22,
			}}}
			if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "safe SSH identity") {
				t.Fatalf("Validate(%q) = %v, want safe-identity error", name, err)
			}
		})
	}
}

func TestValidateAcceptsSafeMachineIdentityNames(t *testing.T) {
	for _, name := range []string{"main", "main-2", "main_v2", "main.example"} {
		t.Run(name, func(t *testing.T) {
			c := Config{Machines: []Machine{{
				Name: name, Hosts: []Host{{Address: "203.0.113.10"}}, User: "root", Port: 22,
			}}}
			if err := c.Validate(); err != nil {
				t.Fatalf("Validate(%q) = %v", name, err)
			}
		})
	}
}

func TestValidateRejectsTwoMachinesWithTheSameName(t *testing.T) {
	c := Config{Machines: []Machine{
		{Name: "main", Hosts: []Host{{Address: "203.0.113.10"}}, User: "root", Port: 22},
		{Name: "main", Hosts: []Host{{Address: "203.0.113.11"}}, User: "root", Port: 22},
	}}
	err := c.Validate()
	if err == nil || !strings.Contains(err.Error(), "main") {
		t.Fatalf("got %v", err)
	}
}

func TestValidateRejectsAMachineWithNoHost(t *testing.T) {
	c := Config{Machines: []Machine{{Name: "main", User: "root", Port: 22}}}
	if err := c.Validate(); err == nil {
		t.Fatal("expected an error for a machine with no host")
	}
}

func TestValidateRejectsAPortOutOfRange(t *testing.T) {
	for _, port := range []int{0, -1, 70000} {
		c := Config{Machines: []Machine{{
			Name: "main", Hosts: []Host{{Address: "203.0.113.10"}}, User: "root", Port: port,
		}}}
		if err := c.Validate(); err == nil {
			t.Fatalf("expected an error for port %d", port)
		}
	}
}

// testAgentKey is a real authorized_keys line, so ParseAuthorizedKey has
// something valid to parse.
const testAgentKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIJCxZSGQhtHNQAuBgxqtDOX9gMd/zqJH6uxite447zWK test-agent-key"

func TestValidateRefusesBothKeyAndAgentKey(t *testing.T) {
	c := Config{Machines: []Machine{{
		Name: "main", Hosts: []Host{{Address: "203.0.113.10"}}, User: "root", Port: 22,
		Key: "/keys/main", AgentKey: testAgentKey,
	}}}
	err := c.Validate()
	if err == nil || !strings.Contains(err.Error(), "main") ||
		!strings.Contains(err.Error(), "key") || !strings.Contains(err.Error(), "agent_key") {
		t.Fatalf("got %v", err)
	}
}

func TestValidateRefusesAnUnusableAgentKey(t *testing.T) {
	c := Config{Machines: []Machine{{
		Name: "main", Hosts: []Host{{Address: "203.0.113.10"}}, User: "root", Port: 22,
		AgentKey: "not a key",
	}}}
	err := c.Validate()
	if err == nil || !strings.Contains(err.Error(), "main") || !strings.Contains(err.Error(), "agent_key") {
		t.Fatalf("got %v", err)
	}
}

func TestValidateAcceptsAnAgentKeyAlone(t *testing.T) {
	c := Config{Machines: []Machine{{
		Name: "main", Hosts: []Host{{Address: "203.0.113.10"}}, User: "root", Port: 22,
		AgentKey: testAgentKey,
	}}}
	if err := c.Validate(); err != nil {
		t.Fatalf("a machine with only agent_key was refused: %v", err)
	}
}

func TestValidateRefusesAnAgentKeyOnASelfMachine(t *testing.T) {
	cfg := Config{Machines: []Machine{{Name: "mac", Self: true, AgentKey: testAgentKey}}}
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "agent_key") {
		t.Fatalf("got %v", err)
	}
}

func TestValidateRejectsAWorkspaceOnAMachineThatDoesNotExist(t *testing.T) {
	c := Config{
		Machines:   []Machine{{Name: "main", Hosts: []Host{{Address: "203.0.113.10"}}, User: "root", Port: 22}},
		Workspaces: []Workspace{{Name: "alice", Machine: "ghost"}},
	}
	err := c.Validate()
	if err == nil || !strings.Contains(err.Error(), "ghost") {
		t.Fatalf("the error does not name the missing machine: %v", err)
	}
}

func TestAWorkspaceMayOmitItsMachineWhenThereIsOnlyOne(t *testing.T) {
	dir := writeConfig(t, "machines:\n  - name: only\n    hosts: [203.0.113.10]\nworkspaces:\n  - name: alice\n")

	c, err := Load(dir)
	if err != nil {
		t.Fatalf("Load returned %v", err)
	}
	if err := c.Validate(); err != nil {
		t.Fatalf("Validate returned %v", err)
	}
	m, _, err := c.MachineFor("alice")
	if err != nil {
		t.Fatalf("MachineFor returned %v", err)
	}
	if m.Name != "only" {
		t.Fatalf("got %q", m.Name)
	}
}

func TestAWorkspaceMustNameItsMachineWhenThereAreSeveral(t *testing.T) {
	c := Config{
		Machines: []Machine{
			{Name: "main", Hosts: []Host{{Address: "203.0.113.10"}}, User: "root", Port: 22},
			{Name: "sandbox", Hosts: []Host{{Address: "127.0.0.1"}}, User: "root", Port: 22},
		},
		Workspaces: []Workspace{{Name: "alice"}},
	}
	err := c.Validate()
	if err == nil || !strings.Contains(err.Error(), "alice") {
		t.Fatalf("the error does not name the workspace: %v", err)
	}
}

func TestValidateRejectsTwoWorkspacesWithTheSameName(t *testing.T) {
	c := Config{
		Machines: []Machine{{Name: "main", Hosts: []Host{{Address: "203.0.113.10"}}, User: "root", Port: 22}},
		Workspaces: []Workspace{
			{Name: "alice", Machine: "main"},
			{Name: "alice", Machine: "main"},
		},
	}
	if err := c.Validate(); err == nil {
		t.Fatal("expected an error for a duplicated workspace name")
	}
}

func TestWorkspacesOnReturnsOnlyThatMachines(t *testing.T) {
	c, _ := Load(writeConfig(t, twoMachines))

	got := c.WorkspacesOn("sandbox")
	if len(got) != 1 || got[0].Name != "bob" {
		t.Fatalf("got %#v", got)
	}
}

func TestLoadSaysWhichFileIsMissing(t *testing.T) {
	_, err := Load(t.TempDir())
	if err == nil || !strings.Contains(err.Error(), FileName) {
		t.Fatalf("got %v", err)
	}
}

func TestLoadRejectsBrokenYAML(t *testing.T) {
	if _, err := Load(writeConfig(t, "machines: [unclosed\n")); err == nil {
		t.Fatal("expected an error for broken YAML")
	}
}

func TestLoadReadsThePackagePinAndTheLists(t *testing.T) {
	dir := writeConfig(t, `
machines:
  - name: main
    hosts: [203.0.113.10]
    packages: [base, docker]
workspaces:
  - name: alice
    packages: [claude-code]
packages: v1
`)
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Packages != "v1" {
		t.Fatalf("pin is %q", cfg.Packages)
	}
	if len(cfg.Machines[0].Packages) != 2 || cfg.Machines[0].Packages[1] != "docker" {
		t.Fatalf("machine packages %#v", cfg.Machines[0].Packages)
	}
	if len(cfg.Workspaces[0].Packages) != 1 {
		t.Fatalf("workspace packages %#v", cfg.Workspaces[0].Packages)
	}
}

func TestValidateRefusesTheSamePackageTwiceOnOneTarget(t *testing.T) {
	c := Config{
		Machines: []Machine{{Name: "main", Hosts: []Host{{Address: "203.0.113.10"}}, Port: 22,
			Packages: []string{"docker", "docker"}}},
	}
	err := c.Validate()
	if err == nil {
		t.Fatal("a duplicate was accepted")
	}
	if !strings.Contains(err.Error(), "docker") || !strings.Contains(err.Error(), "main") {
		t.Fatalf("the error does not say what and where: %v", err)
	}
}

func TestValidateRefusesTheSamePackageTwiceOnAWorkspace(t *testing.T) {
	c := Config{
		Machines:   []Machine{{Name: "main", Hosts: []Host{{Address: "203.0.113.10"}}, Port: 22}},
		Workspaces: []Workspace{{Name: "alice", Packages: []string{"claude-code", "claude-code"}}},
	}
	err := c.Validate()
	if err == nil {
		t.Fatal("a duplicate was accepted")
	}
	if !strings.Contains(err.Error(), "claude-code") || !strings.Contains(err.Error(), "alice") {
		t.Fatalf("the error does not say what and where: %v", err)
	}
}

func TestValidateRefusesAPinThatIsABranch(t *testing.T) {
	c := Config{
		Machines: []Machine{{Name: "main", Hosts: []Host{{Address: "203.0.113.10"}}, Port: 22}},
		Packages: "main",
	}
	err := c.Validate()
	if err == nil {
		t.Fatal("a branch name was accepted as a pin")
	}
	if !strings.Contains(err.Error(), "tag") {
		t.Fatalf("the error does not say what a pin is: %v", err)
	}
}

func TestConfigSaveKeepsComments(t *testing.T) {
	dir := writeConfig(t, `
# The main server.
machines:
  - name: main
    hosts: [203.0.113.10]
`)
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Machines[0].Packages = []string{"base"}
	if err := Save(dir, cfg); err != nil {
		t.Fatal(err)
	}

	body, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "# The main server.") {
		t.Fatalf("the comment was lost:\n%s", body)
	}
	if !strings.Contains(string(body), "base") {
		t.Fatalf("the package was not written:\n%s", body)
	}
}

func TestConfigSaveLeavesTheRestOfTheFileAlone(t *testing.T) {
	dir := writeConfig(t, `
machines:
  - name: main
    hosts: [203.0.113.10, 100.64.0.7]
workspaces:
  - name: alice
domain: example.com
`)
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Workspaces[0].Packages = []string{"claude-code"}
	if err := Save(dir, cfg); err != nil {
		t.Fatal(err)
	}

	saved, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Domain != "example.com" {
		t.Fatalf("domain is %q", saved.Domain)
	}
	if len(saved.Machines[0].Hosts) != 2 || saved.Machines[0].Hosts[1].Address != "100.64.0.7" {
		t.Fatalf("hosts %#v", saved.Machines[0].Hosts)
	}
	// Load fills in the admin user and the port, so writing the struct back
	// would put values in the file that nobody wrote.
	body, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "port") || strings.Contains(string(body), "user") {
		t.Fatalf("a default leaked into the file:\n%s", body)
	}
}

func TestConfigSaveRoundTripsTheListsAndThePin(t *testing.T) {
	dir := writeConfig(t, `
machines:
  - name: main
    hosts: [203.0.113.10]
    packages: [base, docker]
workspaces:
  - name: alice
    packages: [claude-code]
packages: v1
`)
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Machines[0].Packages = []string{"base"}
	cfg.Workspaces[0].Packages = nil
	cfg.Packages = "v2"
	if err := Save(dir, cfg); err != nil {
		t.Fatal(err)
	}

	saved, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Machines[0].Packages) != 1 || saved.Machines[0].Packages[0] != "base" {
		t.Fatalf("machine packages %#v", saved.Machines[0].Packages)
	}
	if len(saved.Workspaces[0].Packages) != 0 {
		t.Fatalf("workspace packages %#v", saved.Workspaces[0].Packages)
	}
	if saved.Packages != "v2" {
		t.Fatalf("pin is %q", saved.Packages)
	}
}

func TestConfigSaveWritesAFileThatIsNotThereYet(t *testing.T) {
	dir := t.TempDir()
	c := Config{
		Machines: []Machine{{Name: "main", Hosts: []Host{{Address: "203.0.113.10"}}, Packages: []string{"base"}}},
		Packages: "v1",
	}
	if err := Save(dir, c); err != nil {
		t.Fatal(err)
	}

	saved, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Machines) != 1 || saved.Machines[0].Packages[0] != "base" {
		t.Fatalf("got %#v", saved.Machines)
	}
}

func TestConfigSaveRefusesATargetTheFileDoesNotHave(t *testing.T) {
	dir := writeConfig(t, "machines:\n  - name: main\n    hosts: [203.0.113.10]\n")
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Machines = append(cfg.Machines, Machine{Name: "spare", Hosts: []Host{{Address: "203.0.113.11"}}})

	err = Save(dir, cfg)
	if err == nil {
		t.Fatal("a machine that is not in the file was silently dropped")
	}
	if !strings.Contains(err.Error(), "spare") {
		t.Fatalf("the error does not say which one: %v", err)
	}
}

func configDirWith(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestAddMachineKeepsEverythingElseAsItWasWritten(t *testing.T) {
	dir := configDirWith(t, `# the machine I bought first
machines:
  - name: main
    hosts: [203.0.113.10]
domain: example.com
`)

	err := AddMachine(dir, Machine{
		Name:  "sandbox",
		Hosts: []Host{{Address: "198.51.100.7"}},
		User:  "root",
		Port:  2222,
		Key:   "/keys/sandbox",
	})
	if err != nil {
		t.Fatal(err)
	}

	body, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatal(err)
	}
	// A file that loses its comments the first time the CLI touches it is a
	// file people stop letting the CLI touch.
	if !strings.Contains(string(body), "# the machine I bought first") {
		t.Fatalf("the comment is gone:\n%s", body)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Machines) != 2 {
		t.Fatalf("machines = %#v", cfg.Machines)
	}
	added := cfg.Machines[1]
	if added.Name != "sandbox" || added.Port != 2222 || added.Key != "/keys/sandbox" {
		t.Fatalf("got %#v", added)
	}
	if cfg.Domain != "example.com" {
		t.Fatalf("domain = %q", cfg.Domain)
	}
}

func TestAddMachineWritesTheAgentKey(t *testing.T) {
	dir := configDirWith(t, "machines:\n  - name: main\n    hosts: [203.0.113.10]\n")

	err := AddMachine(dir, Machine{
		Name: "sandbox", Hosts: []Host{{Address: "198.51.100.7"}}, User: "root", Port: 22,
		AgentKey: testAgentKey,
	})
	if err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	added := cfg.Machines[1]
	if added.AgentKey != testAgentKey {
		t.Fatalf("agent_key = %q, want %q", added.AgentKey, testAgentKey)
	}
	if added.Key != "" {
		t.Fatalf("key = %q, want empty: an agent key has no file", added.Key)
	}
}

func TestAddMachineRefusesANameThatIsTaken(t *testing.T) {
	dir := configDirWith(t, "machines:\n  - name: main\n    hosts: [203.0.113.10]\n")

	err := AddMachine(dir, Machine{Name: "main", Hosts: []Host{{Address: "198.51.100.7"}}})
	if err == nil {
		t.Fatal("two machines were allowed the same name")
	}
	if !strings.Contains(err.Error(), "main") {
		t.Fatalf("the error does not name it: %v", err)
	}
}

func TestAddMachineNeedsAConfigurationToAddTo(t *testing.T) {
	err := AddMachine(t.TempDir(), Machine{Name: "main", Hosts: []Host{{Address: "203.0.113.10"}}})
	if err == nil {
		t.Fatal("it wrote a configuration out of nothing")
	}
	if !strings.Contains(err.Error(), "setup") {
		t.Fatalf("the error does not say where to start: %v", err)
	}
}

func TestRemoveMachineTakesItOutAndLeavesTheRest(t *testing.T) {
	dir := configDirWith(t, `machines:
  - name: main
    hosts: [203.0.113.10]
  - name: sandbox
    hosts: [198.51.100.7]
domain: example.com
`)

	if err := RemoveMachine(dir, "sandbox"); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Machines) != 1 || cfg.Machines[0].Name != "main" {
		t.Fatalf("machines = %#v", cfg.Machines)
	}
	if cfg.Domain != "example.com" {
		t.Fatalf("domain = %q", cfg.Domain)
	}
}

func TestRemoveMachineSaysWhichOnesThereAre(t *testing.T) {
	dir := configDirWith(t, "machines:\n  - name: main\n    hosts: [203.0.113.10]\n")

	err := RemoveMachine(dir, "absent")
	if err == nil {
		t.Fatal("it removed a machine that is not configured")
	}
	if !strings.Contains(err.Error(), "main") {
		t.Fatalf("the error does not say what is there: %v", err)
	}
}

func TestRemoveMachineRefusesToOrphanAWorkspace(t *testing.T) {
	// A workspace pointing at a machine that is gone is a configuration that
	// no longer loads, and the person finds out on the next command.
	dir := configDirWith(t, `machines:
  - name: main
    hosts: [203.0.113.10]
  - name: sandbox
    hosts: [198.51.100.7]
workspaces:
  - name: alice
    machine: sandbox
`)

	err := RemoveMachine(dir, "sandbox")
	if err == nil {
		t.Fatal("it left a workspace pointing at nothing")
	}
	if !strings.Contains(err.Error(), "alice") {
		t.Fatalf("the error does not name the workspace: %v", err)
	}

	cfg, _ := Load(dir)
	if len(cfg.Machines) != 2 {
		t.Fatal("it removed the machine anyway")
	}
}

func TestSettingsForStripsThePackagePrefix(t *testing.T) {
	got := SettingsFor(map[string]any{
		"caddy.email":   "someone@example.com",
		"base.timezone": "UTC",
	}, "caddy")

	if len(got) != 1 || got["email"] != "someone@example.com" {
		t.Fatalf("got %#v", got)
	}
}

func TestSettingsForKeepsADottedKeyInsideAPackage(t *testing.T) {
	// A package may want `claude-plugins.marketplace.url`. Only the first
	// segment is the package name.
	got := SettingsFor(map[string]any{"a.b.c": 1}, "a")
	if got["b.c"] != 1 {
		t.Fatalf("got %#v", got)
	}
}

func TestSettingsForIgnoresAnotherPackage(t *testing.T) {
	got := SettingsFor(map[string]any{"base.timezone": "UTC"}, "caddy")
	if len(got) != 0 {
		t.Fatalf("got %#v", got)
	}
}

func TestLoadReadsTheSettingsOfBothKindsOfTarget(t *testing.T) {
	dir := writeConfig(t, `
machines:
  - name: main
    hosts: [203.0.113.10]
    packages: [base]
    settings:
      base.timezone: America/Sao_Paulo
workspaces:
  - name: alice
    packages: [claude-plugins]
    settings:
      claude-plugins.plugins: [their-plugin]
`)
	c, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if c.Machines[0].Settings["base.timezone"] != "America/Sao_Paulo" {
		t.Fatalf("got %#v", c.Machines[0].Settings)
	}
	if got, ok := c.Workspaces[0].Settings["claude-plugins.plugins"].([]any); !ok || len(got) != 1 {
		t.Fatalf("got %#v", c.Workspaces[0].Settings)
	}
}

func TestValidateRefusesASettingWithNoPackagePrefix(t *testing.T) {
	c := Config{
		Machines: []Machine{{
			Name: "main", Hosts: []Host{{Address: "203.0.113.10"}}, Port: 22,
			Settings: map[string]any{"timezone": "UTC"},
		}},
	}
	err := c.Validate()
	if err == nil {
		t.Fatal("a setting belonging to nothing was accepted")
	}
	if !strings.Contains(err.Error(), "<package>.<name>") {
		t.Fatalf("the error does not give the shape: %v", err)
	}
}

func TestValidateRefusesASettingForAPackageTheTargetDoesNotHave(t *testing.T) {
	c := Config{
		Machines: []Machine{{
			Name: "main", Hosts: []Host{{Address: "203.0.113.10"}}, Port: 22,
			Packages: []string{"base"},
			Settings: map[string]any{"caddy.email": "x@example.com"},
		}},
	}
	err := c.Validate()
	if err == nil {
		t.Fatal("a setting for a package nobody installed was accepted")
	}
	// A typo in a package name would otherwise be silent: the value simply
	// never reaches anything, and the recipe keeps its default.
	if !strings.Contains(err.Error(), "caddy") {
		t.Fatalf("the error does not name it: %v", err)
	}
}

func TestValidateRefusesAWorkspaceSettingForAPackageItDoesNotHave(t *testing.T) {
	c := Config{
		Machines: []Machine{{Name: "main", Hosts: []Host{{Address: "203.0.113.10"}}, Port: 22}},
		Workspaces: []Workspace{{
			Name: "alice", Packages: []string{"zsh"},
			Settings: map[string]any{"claude-plugins.marketplace": "example.com/plugins"},
		}},
	}
	err := c.Validate()
	if err == nil {
		t.Fatal("a setting for a package the workspace does not have was accepted")
	}
	if !strings.Contains(err.Error(), "claude-plugins") || !strings.Contains(err.Error(), "alice") {
		t.Fatalf("the error does not say what and where: %v", err)
	}
}

func TestValidateAcceptsASettingForAPackageTheTargetHas(t *testing.T) {
	c := Config{
		Machines: []Machine{{
			Name: "main", Hosts: []Host{{Address: "203.0.113.10"}}, Port: 22,
			Packages: []string{"base", "caddy"},
			Settings: map[string]any{"caddy.email": "someone@example.com"},
		}},
	}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestLoadReadsTheDefaultPackagesForANewWorkspace(t *testing.T) {
	dir := writeConfig(t, `
machines:
  - name: main
    hosts: [203.0.113.10]
defaults:
  workspace: [workspace, dev, zsh]
`)

	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(cfg.Defaults.Workspace, []string{"workspace", "dev", "zsh"}) {
		t.Fatalf("got %#v", cfg.Defaults.Workspace)
	}
}

func TestAddWorkspaceKeepsEverythingElseAsItWasWritten(t *testing.T) {
	dir := configDirWith(t, `# the machine I bought first
machines:
  - name: main
    hosts: [203.0.113.10]
domain: example.com
`)

	err := AddWorkspace(dir, Workspace{
		Name:     "alice",
		Machine:  "main",
		Packages: []string{"workspace", "dev"},
	})
	if err != nil {
		t.Fatal(err)
	}

	body, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "# the machine I bought first") {
		t.Fatalf("the comment is gone:\n%s", body)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	w, err := cfg.Workspace("alice")
	if err != nil {
		t.Fatal(err)
	}
	if w.Machine != "main" || !slices.Equal(w.Packages, []string{"workspace", "dev"}) {
		t.Fatalf("got %#v", w)
	}
}

func TestAddWorkspaceMakesTheSectionWhenThereIsNone(t *testing.T) {
	dir := configDirWith(t, "machines:\n  - name: main\n    hosts: [203.0.113.10]\n")

	if err := AddWorkspace(dir, Workspace{Name: "alice"}); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Workspaces) != 1 || cfg.Workspaces[0].Name != "alice" {
		t.Fatalf("got %#v", cfg.Workspaces)
	}
}

func TestAddWorkspaceRefusesANameThatIsTaken(t *testing.T) {
	dir := configDirWith(t, `
machines:
  - name: main
    hosts: [203.0.113.10]
workspaces:
  - name: alice
`)

	err := AddWorkspace(dir, Workspace{Name: "alice"})
	if err == nil {
		t.Fatal("it added a second alice")
	}
	if !strings.Contains(err.Error(), "alice") {
		t.Fatalf("the error does not name it: %v", err)
	}
}

func TestRemoveWorkspaceTakesItOutAndLeavesTheRest(t *testing.T) {
	dir := configDirWith(t, `
machines:
  - name: main
    hosts: [203.0.113.10]
workspaces:
  - name: alice
  - name: bob
domain: example.com
`)

	if err := RemoveWorkspace(dir, "alice"); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Workspaces) != 1 || cfg.Workspaces[0].Name != "bob" {
		t.Fatalf("got %#v", cfg.Workspaces)
	}
	if cfg.Domain != "example.com" {
		t.Fatalf("the rest of the file changed: %#v", cfg)
	}
}

func TestRemoveWorkspaceSaysWhichOnesThereAre(t *testing.T) {
	dir := configDirWith(t, `
machines:
  - name: main
    hosts: [203.0.113.10]
workspaces:
  - name: bob
`)

	err := RemoveWorkspace(dir, "alice")
	if err == nil {
		t.Fatal("it removed a workspace that does not exist")
	}
	if !strings.Contains(err.Error(), "bob") {
		t.Fatalf("the error does not say what is there: %v", err)
	}
}

func TestUpdateWorkspaceWritesTheFieldsTheCLIEdits(t *testing.T) {
	dir := configDirWith(t, `
machines:
  - name: main
    hosts: [203.0.113.10]
  - name: sandbox
    hosts: [198.51.100.7]
workspaces:
  - name: alice  # the one I work in
    machine: main
`)

	err := UpdateWorkspace(dir, Workspace{
		Name:     "alice",
		Machine:  "sandbox",
		User:     "alice2",
		Packages: []string{"workspace", "zsh"},
		Settings: map[string]any{"zsh.theme": "plain"},
	})
	if err != nil {
		t.Fatal(err)
	}

	body, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "the one I work in") {
		t.Fatalf("the comment is gone:\n%s", body)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	w, _ := cfg.Workspace("alice")
	if w.Machine != "sandbox" || w.User != "alice2" {
		t.Fatalf("got %#v", w)
	}
	if w.Settings["zsh.theme"] != "plain" {
		t.Fatalf("the setting did not survive: %#v", w.Settings)
	}
}

func TestUpdateWorkspaceRemovesWhatWasEmptied(t *testing.T) {
	dir := configDirWith(t, `
machines:
  - name: main
    hosts: [203.0.113.10]
workspaces:
  - name: alice
    user: other
    settings:
      zsh.theme: plain
    packages: [zsh]
`)

	err := UpdateWorkspace(dir, Workspace{Name: "alice", Packages: []string{"zsh"}})
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	w, _ := cfg.Workspace("alice")
	if w.User != "" || len(w.Settings) != 0 {
		t.Fatalf("got %#v", w)
	}
}

func TestUpdateWorkspaceRefusesOneTheFileDoesNotHave(t *testing.T) {
	dir := configDirWith(t, "machines:\n  - name: main\n    hosts: [203.0.113.10]\n")

	if err := UpdateWorkspace(dir, Workspace{Name: "alice"}); err == nil {
		t.Fatal("it edited a workspace that is not there")
	}
}

func TestLoadReadsTheCredentialPreferencesAtBothLevels(t *testing.T) {
	dir := writeConfig(t, `
machines:
  - name: main
    hosts: [203.0.113.10]
credentials:
  gh: machine
workspaces:
  - name: alice
  - name: bob
    credentials:
      gh: own
`)

	c, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if c.Credentials["gh"] != CredentialMachine {
		t.Fatalf("the setup's default is %#v", c.Credentials)
	}
	if c.Workspaces[1].Credentials["gh"] != CredentialOwn {
		t.Fatalf("bob's own preference is %#v", c.Workspaces[1].Credentials)
	}
}

func TestCredentialsForPutsTheWorkspaceFirst(t *testing.T) {
	c := Config{
		Credentials: map[string]string{"gh": CredentialMachine, "claude": CredentialMachine},
		Workspaces: []Workspace{
			{Name: "bob", Credentials: map[string]string{"gh": CredentialOwn}},
		},
	}

	got := c.CredentialsFor(c.Workspaces[0])
	// Somebody may want most workspaces on one account and one on a client's.
	if got["gh"] != CredentialOwn {
		t.Fatalf("bob asked for his own gh, got %q", got["gh"])
	}
	if got["claude"] != CredentialMachine {
		t.Fatalf("bob said nothing about claude, so the setup's default stands: %q", got["claude"])
	}
}

func TestCredentialsForDoesNotWriteBackOntoTheConfiguration(t *testing.T) {
	c := Config{
		Credentials: map[string]string{"gh": CredentialMachine},
		Workspaces:  []Workspace{{Name: "bob", Credentials: map[string]string{"gh": CredentialOwn}}},
	}

	c.CredentialsFor(c.Workspaces[0])
	if c.Credentials["gh"] != CredentialMachine {
		t.Fatalf("one workspace's choice changed the setup's default to %q", c.Credentials["gh"])
	}
}

func TestValidateRefusesACredentialPreferenceNobodyUnderstands(t *testing.T) {
	c := Config{
		Machines:    []Machine{{Name: "main", Hosts: []Host{{Address: "203.0.113.10"}}, Port: 22}},
		Credentials: map[string]string{"gh": "shared"},
	}

	err := c.Validate()
	if err == nil || !strings.Contains(err.Error(), "machine") || !strings.Contains(err.Error(), "own") {
		t.Fatalf("the message has to say what the two answers are, got %v", err)
	}
}

func TestValidateRefusesAWorkspaceCredentialPreferenceNobodyUnderstands(t *testing.T) {
	c := Config{
		Machines:   []Machine{{Name: "main", Hosts: []Host{{Address: "203.0.113.10"}}, Port: 22}},
		Workspaces: []Workspace{{Name: "bob", Credentials: map[string]string{"gh": "yes"}}},
	}

	err := c.Validate()
	if err == nil || !strings.Contains(err.Error(), "bob") {
		t.Fatalf("the message has to name the workspace, got %v", err)
	}
}

func TestUpdateWorkspaceDefaultsPreservesComments(t *testing.T) {
	dir := writeConfig(t, `# operator note
machines:
  - name: main
    hosts: [203.0.113.10]
defaults:
  workspace: [workspace, dev] # future accounts
`)

	if err := UpdateWorkspaceDefaults(dir, []string{"workspace", "dev", "global-skills"}); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "operator note") || !strings.Contains(string(body), "future accounts") {
		t.Fatalf("comments were lost:\n%s", body)
	}
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(cfg.Defaults.Workspace, []string{"workspace", "dev", "global-skills"}) {
		t.Fatalf("got %#v", cfg.Defaults.Workspace)
	}
}

func TestRoutesAreReadFromAWorkspace(t *testing.T) {
	dir := configDirWith(t, `
machines:
  - name: main
    hosts: [203.0.113.10]
workspaces:
  - name: alice
    routes:
      - host: app.example.com
        port: 8080
`)
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	w, r, ok := cfg.RouteOwner("app.example.com")
	if !ok || w.Name != "alice" || r.Port != 8080 {
		t.Fatalf("route not read: %v %+v %v", ok, r, w.Name)
	}
}

func TestValidateRefusesTheSameHostTwice(t *testing.T) {
	dir := configDirWith(t, `
machines:
  - name: main
    hosts: [203.0.113.10]
workspaces:
  - name: alice
    routes: [{host: app.example.com, port: 8080}]
  - name: bob
    routes: [{host: app.example.com, port: 9090}]
`)
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	err = cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "app.example.com") || !strings.Contains(err.Error(), "twice") {
		t.Fatalf("a host on two workspaces must be refused, got %v", err)
	}
}

func TestValidateRefusesABadRoute(t *testing.T) {
	for _, body := range []string{
		"routes: [{host: 'a/b', port: 8080}]",
		"routes: [{host: 'app.example.com', port: 0}]",
		"routes: [{host: '', port: 8080}]",
	} {
		dir := configDirWith(t, "machines:\n  - name: main\n    hosts: [203.0.113.10]\nworkspaces:\n  - name: alice\n    "+body+"\n")
		cfg, err := Load(dir)
		if err != nil {
			t.Fatal(err)
		}
		if err := cfg.Validate(); err == nil {
			t.Fatalf("%s must be refused", body)
		}
	}
}

func TestAddRouteKeepsCommentsAndRefusesDuplicates(t *testing.T) {
	dir := configDirWith(t, `
machines:
  - name: main
    hosts: [203.0.113.10]
workspaces:
  - name: alice  # the one I work in
    packages: [workspace]
`)
	if err := AddRoute(dir, "alice", Route{Host: "app.example.com", Port: 8080}); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(filepath.Join(dir, FileName))
	if !strings.Contains(string(body), "the one I work in") {
		t.Fatalf("the comment is gone:\n%s", body)
	}
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, r, ok := cfg.RouteOwner("app.example.com"); !ok || r.Port != 8080 {
		t.Fatalf("route not written:\n%s", body)
	}
	err = AddRoute(dir, "bob", Route{Host: "app.example.com", Port: 1})
	if err == nil || !strings.Contains(err.Error(), "alice") {
		t.Fatalf("adding a host another workspace owns must name the owner, got %v", err)
	}
	if err := AddRoute(dir, "nobody", Route{Host: "x.example.com", Port: 1}); err == nil {
		t.Fatal("an unknown workspace must be refused")
	}
}

func TestRemoveRouteSaysWhoseItWas(t *testing.T) {
	dir := configDirWith(t, `
machines:
  - name: main
    hosts: [203.0.113.10]
workspaces:
  - name: alice
    routes:
      - host: app.example.com
        port: 8080
      - host: api.example.com
        port: 8081
`)
	owner, err := RemoveRoute(dir, "app.example.com")
	if err != nil || owner != "alice" {
		t.Fatalf("got %q, %v", owner, err)
	}
	cfg, _ := Load(dir)
	if _, _, ok := cfg.RouteOwner("app.example.com"); ok {
		t.Fatal("the route is still there")
	}
	if _, r, ok := cfg.RouteOwner("api.example.com"); !ok || r.Port != 8081 {
		t.Fatal("the other route was lost")
	}
	if _, err := RemoveRoute(dir, "gone.example.com"); err == nil {
		t.Fatal("removing a host nobody has must be refused")
	}
	owner, err = RemoveRoute(dir, "api.example.com")
	if err != nil || owner != "alice" {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(filepath.Join(dir, FileName))
	if strings.Contains(string(body), "routes") {
		t.Fatalf("an emptied routes list must be removed, not left as []:\n%s", body)
	}
}

func TestUpdateWorkspaceKeepsRoutes(t *testing.T) {
	dir := configDirWith(t, `
machines:
  - name: main
    hosts: [203.0.113.10]
workspaces:
  - name: alice
    routes: [{host: app.example.com, port: 8080}]
`)
	cfg, _ := Load(dir)
	w, _ := cfg.Workspace("alice")
	w.Packages = []string{"zsh"}
	if err := UpdateWorkspace(dir, w); err != nil {
		t.Fatal(err)
	}
	cfg, _ = Load(dir)
	if _, _, ok := cfg.RouteOwner("app.example.com"); !ok {
		t.Fatal("editing a workspace dropped its routes")
	}
}

func TestValidateRefusesAHostsOnASelfMachine(t *testing.T) {
	cfg := Config{Machines: []Machine{{Name: "mac", Self: true, Hosts: []Host{{Address: "203.0.113.10"}}}}}
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "hosts") {
		t.Fatalf("got %v", err)
	}
}

func TestValidateRefusesAUserOnASelfMachine(t *testing.T) {
	cfg := Config{Machines: []Machine{{Name: "mac", Self: true, User: "root"}}}
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "user") {
		t.Fatalf("got %v", err)
	}
}

func TestValidateRefusesAPortOnASelfMachine(t *testing.T) {
	cfg := Config{Machines: []Machine{{Name: "mac", Self: true, Port: 22}}}
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "port") {
		t.Fatalf("got %v", err)
	}
}

func TestValidateRefusesAKeyOnASelfMachine(t *testing.T) {
	cfg := Config{Machines: []Machine{{Name: "mac", Self: true, Key: "/keys/mac"}}}
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "key") {
		t.Fatalf("got %v", err)
	}
}

func TestValidateRefusesTwoSelfMachines(t *testing.T) {
	cfg := Config{Machines: []Machine{
		{Name: "mac1", Self: true},
		{Name: "mac2", Self: true},
	}}
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "mac1") || !strings.Contains(err.Error(), "mac2") {
		t.Fatalf("got %v", err)
	}
}

func TestValidateRefusesAWorkspaceOnASelfMachineNamedExplicitly(t *testing.T) {
	cfg := Config{
		Machines: []Machine{
			{Name: "mac", Self: true},
			{Name: "server", Hosts: []Host{{Address: "203.0.113.10"}}, User: "root", Port: 22},
		},
		Workspaces: []Workspace{{Name: "alice", Machine: "mac"}},
	}
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "alice") || !strings.Contains(err.Error(), "your computer") {
		t.Fatalf("got %v", err)
	}
}

func TestValidateRefusesAWorkspaceOnASelfMachineImplicitly(t *testing.T) {
	cfg := Config{
		Machines:   []Machine{{Name: "mac", Self: true}},
		Workspaces: []Workspace{{Name: "alice"}},
	}
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "alice") {
		t.Fatalf("got %v", err)
	}
}

func TestValidateAcceptsAValidSelfMachine(t *testing.T) {
	cfg := Config{Machines: []Machine{{Name: "mac", Self: true}}}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestLoadDoesNotApplyAddressDefaultsToASelfMachine(t *testing.T) {
	dir := configDirWith(t, "machines:\n  - name: mac\n    self: true\n")
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Load applied a default that Validate then refuses: %v", err)
	}
	m := cfg.Machines[0]
	if m.User != "" || m.Port != 0 {
		t.Fatalf("Load applied address defaults to a self machine: %#v", m)
	}
}

func TestAddMachineWritesSelfTrueAndNoAddressFields(t *testing.T) {
	dir := configDirWith(t, `# the machine I bought first
machines:
  - name: server
    hosts: [203.0.113.10]
`)
	if err := AddMachine(dir, Machine{Name: "mac", Self: true}); err != nil {
		t.Fatal(err)
	}

	body, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "# the machine I bought first") {
		t.Fatalf("the comment is gone:\n%s", body)
	}
	if strings.Contains(string(body), "hosts:\n    - null") {
		t.Fatalf("wrote a null hosts entry:\n%s", body)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	added, err := cfg.Machine("mac")
	if err != nil {
		t.Fatal(err)
	}
	if !added.Self || len(added.Hosts) != 0 || added.User != "" || added.Port != 0 || added.Key != "" {
		t.Fatalf("got %#v", added)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestMachineNeverPicksThisComputerImplicitly(t *testing.T) {
	cfg := Config{Machines: []Machine{{Name: "main", Hosts: []Host{{Address: "203.0.113.10"}}}, {Name: "mac", Self: true}}}
	m, err := cfg.Machine("")
	if err != nil || m.Name != "main" {
		t.Fatalf("got %q, %v; the one server is the implicit choice", m.Name, err)
	}
	if m, err := cfg.Machine("mac"); err != nil || !m.Self {
		t.Fatalf("naming it still reaches your computer: %v", err)
	}
}

func TestMachineWithOnlyThisComputerPicksIt(t *testing.T) {
	cfg := Config{Machines: []Machine{{Name: "mac", Self: true}}}
	if m, err := cfg.Machine(""); err != nil || m.Name != "mac" {
		t.Fatalf("got %q, %v", m.Name, err)
	}
}

func TestMachineWithTwoServersStillAsks(t *testing.T) {
	cfg := Config{Machines: []Machine{
		{Name: "main", Hosts: []Host{{Address: "203.0.113.10"}}},
		{Name: "sandbox", Hosts: []Host{{Address: "203.0.113.11"}}},
		{Name: "mac", Self: true},
	}}
	if _, err := cfg.Machine(""); err == nil || !strings.Contains(err.Error(), "--machine") {
		t.Fatalf("got %v", err)
	}
}

func TestSSHAliasesRoundTripsThroughLoad(t *testing.T) {
	dir := configDirWith(t, "machines:\n  - name: main\n    hosts: [203.0.113.10]\nssh_aliases: true\n")

	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.SSHAliases {
		t.Fatal("ssh_aliases: true was not read")
	}
}

func TestSSHAliasesDefaultsToFalseWhenAbsent(t *testing.T) {
	dir := configDirWith(t, "machines:\n  - name: main\n    hosts: [203.0.113.10]\n")

	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SSHAliases {
		t.Fatal("a configuration without the key should behave as if it were false")
	}
}

func TestSetSSHAliasesWritesTheKeyAndKeepsComments(t *testing.T) {
	dir := configDirWith(t, "# my machine\nmachines:\n  - name: main\n    hosts: [203.0.113.10]\n")

	if err := SetSSHAliases(dir, true); err != nil {
		t.Fatal(err)
	}

	body, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "# my machine") {
		t.Fatalf("the comment is gone:\n%s", body)
	}
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.SSHAliases {
		t.Fatal("ssh_aliases was not set")
	}
}

func TestSetSSHAliasesFalseRemovesTheKey(t *testing.T) {
	dir := configDirWith(t, "machines:\n  - name: main\n    hosts: [203.0.113.10]\nssh_aliases: true\n")

	if err := SetSSHAliases(dir, false); err != nil {
		t.Fatal(err)
	}

	body, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "ssh_aliases") {
		t.Fatalf("ssh_aliases: false should be written as nothing:\n%s", body)
	}
}

func TestSetMachineHostsPrependsAndKeepsComments(t *testing.T) {
	dir := configDirWith(t, "# my machine\nmachines:\n  - name: main\n    hosts: [203.0.113.10]\n")

	if err := SetMachineHosts(dir, "main", []string{"tailscale:main", "203.0.113.10"}); err != nil {
		t.Fatal(err)
	}

	body, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "# my machine") {
		t.Fatalf("the comment is gone:\n%s", body)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	m, err := cfg.Machine("main")
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Hosts) != 2 || m.Hosts[0].Address != "tailscale:main" || m.Hosts[1].Address != "203.0.113.10" {
		t.Fatalf("hosts = %#v", m.Hosts)
	}
}

func TestSetMachineHostsRefusesToEmptyTheList(t *testing.T) {
	dir := configDirWith(t, "machines:\n  - name: main\n    hosts: [203.0.113.10]\n")

	err := SetMachineHosts(dir, "main", nil)
	if err == nil {
		t.Fatal("it wrote a machine with no address")
	}
}

func TestSetMachineHostsRefusesAnUnknownMachine(t *testing.T) {
	dir := configDirWith(t, "machines:\n  - name: main\n    hosts: [203.0.113.10]\n")

	err := SetMachineHosts(dir, "sandbox", []string{"198.51.100.7"})
	if err == nil {
		t.Fatal("it silently did nothing")
	}
	if !strings.Contains(err.Error(), "sandbox") {
		t.Fatalf("the error does not name it: %v", err)
	}
}

func TestSetMachineHostsKeepsABlockListAndItsComments(t *testing.T) {
	before := "# my machines\n" +
		"machines:\n" +
		"  - name: main # the VPS\n" +
		"    hosts:\n" +
		"      - 203.0.113.10 # public\n" +
		"    packages: [tailscale-pkg]\n"
	after := "# my machines\n" +
		"machines:\n" +
		"  - name: main # the VPS\n" +
		"    hosts:\n" +
		"      - tailscale:main\n" +
		"      - 203.0.113.10 # public\n" +
		"    packages: [tailscale-pkg]\n"
	dir := configDirWith(t, before)

	if err := SetMachineHosts(dir, "main", []string{"tailscale:main", "203.0.113.10"}); err != nil {
		t.Fatal(err)
	}

	body, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != after {
		t.Fatalf("got:\n%s\nwant:\n%s", body, after)
	}
}

func TestSetMachineHostsWritesAFlowListAsABlockList(t *testing.T) {
	before := "machines:\n" +
		"  - name: main\n" +
		"    hosts: [203.0.113.10]\n"
	after := "machines:\n" +
		"  - name: main\n" +
		"    hosts:\n" +
		"      - tailscale:main\n" +
		"      - 203.0.113.10\n"
	dir := configDirWith(t, before)

	if err := SetMachineHosts(dir, "main", []string{"tailscale:main", "203.0.113.10"}); err != nil {
		t.Fatal(err)
	}

	body, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != after {
		t.Fatalf("got:\n%s\nwant:\n%s", body, after)
	}
}

func TestLoadTellsEachMachineWhereItsPackagesAre(t *testing.T) {
	dir := writeConfig(t, "packages: v17\nmachines:\n  - name: main\n    hosts: [203.0.113.10]\n")
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	m := cfg.Machines[0]
	if m.ConfigDir != dir || m.PackagesRelease != "v17" {
		t.Fatalf("got config dir %q and release %q", m.ConfigDir, m.PackagesRelease)
	}
}

func TestMachinePackagesLocationIsRuntimeOnly(t *testing.T) {
	body, err := yaml.Marshal(Machine{Name: "main", ConfigDir: "/private/config", PackagesRelease: "v17"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "/private/config") || strings.Contains(string(body), "v17") {
		t.Fatalf("runtime package location leaked into YAML:\n%s", body)
	}
}

const viaConfig = `
machines:
  - name: edge
    hosts: [203.0.113.10]
  - name: lab
    hosts: [100.64.0.7]
  - name: mac
    self: true
workspaces:
  - name: alice
    machine: lab
    routes: [{host: app.example.com, port: 8080, via: edge}]
  - name: bob
    machine: edge
    routes: [{host: bob.example.com, port: 9090}]
`

func TestARouteViaAnotherMachineIsServedThere(t *testing.T) {
	cfg, err := Load(configDirWith(t, viaConfig))
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	w, r, _ := cfg.RouteOwner("app.example.com")
	if r.Via != "edge" || cfg.ServingMachine(w, r) != "edge" {
		t.Fatalf("via not read: %+v, served by %q", r, cfg.ServingMachine(w, r))
	}
	w, r, _ = cfg.RouteOwner("bob.example.com")
	if cfg.ServingMachine(w, r) != "edge" {
		t.Fatalf("a route without via is served by its workspace's machine, got %q", cfg.ServingMachine(w, r))
	}

	var onEdge []string
	for _, s := range cfg.RoutesServedBy("edge") {
		onEdge = append(onEdge, s.Workspace.Name+" "+s.Route.Host)
	}
	if strings.Join(onEdge, ",") != "alice app.example.com,bob bob.example.com" {
		t.Fatalf("edge serves %v", onEdge)
	}
	if got := cfg.RoutesServedBy("lab"); len(got) != 0 {
		t.Fatalf("lab serves nothing, got %+v", got)
	}
}

func TestValidateRefusesAViaThatCannotServe(t *testing.T) {
	for via, want := range map[string]string{
		"nowhere": "no machine",
		"lab":     "its own machine",
		"mac":     "this computer",
	} {
		body := strings.Replace(viaConfig, "via: edge", "via: "+via, 1)
		cfg, err := Load(configDirWith(t, body))
		if err != nil {
			t.Fatal(err)
		}
		err = cfg.Validate()
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("via %s: want an error saying %q, got %v", via, want, err)
		}
	}
}

func TestAddRouteWritesViaOnlyWhenSet(t *testing.T) {
	dir := configDirWith(t, `
machines:
  - name: edge
    hosts: [203.0.113.10]
  - name: lab
    hosts: [100.64.0.7]
workspaces:
  - name: alice
    machine: lab
`)
	if err := AddRoute(dir, "alice", Route{Host: "app.example.com", Port: 8080, Via: "edge"}); err != nil {
		t.Fatal(err)
	}
	if err := AddRoute(dir, "alice", Route{Host: "api.example.com", Port: 8081}); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(filepath.Join(dir, FileName))
	for _, want := range []string{
		"{host: app.example.com, port: 8080, via: edge}",
		"{host: api.example.com, port: 8081}",
	} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("want %q in:\n%s", want, body)
		}
	}
}

func TestRemoveMachineRefusesOneThatServesARoute(t *testing.T) {
	dir := configDirWith(t, strings.Replace(viaConfig,
		"  - name: bob\n    machine: edge\n    routes: [{host: bob.example.com, port: 9090}]\n", "", 1))
	err := RemoveMachine(dir, "edge")
	if err == nil || !strings.Contains(err.Error(), "app.example.com") {
		t.Fatalf("a machine a route is sent to must not go, got %v", err)
	}
}

func TestSetMachineSettingsWritesThemAndKeepsComments(t *testing.T) {
	dir := configDirWith(t, `
machines:
  - name: main  # the server
    hosts: [203.0.113.10]
    packages: [hostinger, caddy]
    settings:
      caddy.email: alice@example.com  # for the certificates
  - name: sandbox
    hosts: [203.0.113.20]
`)

	err := SetMachineSettings(dir, "main", map[string]any{
		"caddy.email":     "alice@example.com",
		"hostinger.zones": []any{"example.com"},
	})
	if err != nil {
		t.Fatal(err)
	}

	body, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "the server") {
		t.Fatalf("the comment is gone:\n%s", body)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	m, _ := cfg.Machine("main")
	zones, ok := m.Settings["hostinger.zones"].([]any)
	if !ok || len(zones) != 1 || zones[0] != "example.com" {
		t.Fatalf("got %#v", m.Settings)
	}
	if m.Settings["caddy.email"] != "alice@example.com" {
		t.Fatalf("the other setting did not survive: %#v", m.Settings)
	}
	other, _ := cfg.Machine("sandbox")
	if len(other.Settings) != 0 {
		t.Fatalf("it wrote onto another machine: %#v", other.Settings)
	}
}

func TestSetMachineSettingsRemovesAnEmptiedMap(t *testing.T) {
	dir := configDirWith(t, `
machines:
  - name: main
    hosts: [203.0.113.10]
    packages: [caddy]
    settings:
      caddy.email: alice@example.com
`)

	if err := SetMachineSettings(dir, "main", nil); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "settings") {
		t.Fatalf("an empty settings key was left behind:\n%s", body)
	}
}

func TestSetMachineSettingsRefusesOneTheFileDoesNotHave(t *testing.T) {
	dir := configDirWith(t, "machines:\n  - name: main\n    hosts: [203.0.113.10]\n")

	if err := SetMachineSettings(dir, "sandbox", map[string]any{"caddy.email": "x"}); err == nil {
		t.Fatal("it edited a machine that is not there")
	}
}

func TestValidateAcceptsAccountsThatKeepPasswordLogin(t *testing.T) {
	c := Config{Machines: []Machine{{
		Name: "main", Hosts: []Host{{Address: "203.0.113.10"}}, User: "root", Port: 22,
		PasswordLoginKeep: []string{"alice", "bob"},
	}}}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestValidateRefusesAPasswordLoginAccountSshdWouldReadAsAPattern(t *testing.T) {
	for _, name := range []string{"", "a,b", "*", "!root", "a b"} {
		c := Config{Machines: []Machine{{
			Name: "main", Hosts: []Host{{Address: "203.0.113.10"}}, User: "root", Port: 22,
			PasswordLoginKeep: []string{name},
		}}}
		err := c.Validate()
		if err == nil || !strings.Contains(err.Error(), "password_login_keep") {
			t.Fatalf("%q: got %v", name, err)
		}
	}
}

func TestValidateRefusesPasswordLoginKeepOnYourOwnComputer(t *testing.T) {
	c := Config{Machines: []Machine{{Name: "mac", Self: true, PasswordLoginKeep: []string{"alice"}}}}
	err := c.Validate()
	if err == nil || !strings.Contains(err.Error(), "password_login_keep") {
		t.Fatalf("got %v", err)
	}
}
