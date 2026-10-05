package commands

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mydevmachine/devmachine/internal/config"
)

// selfConfig is a configuration with one self machine and nothing else, the
// shape every command in this file starts from.
func selfConfig(t *testing.T) string {
	t.Helper()
	return configWith(t, "machines:\n  - name: mac\n    self: true\n")
}

func withMissingBinary(t *testing.T, name string) {
	t.Helper()
	orig := lookPath
	lookPath = func(n string) (string, error) {
		if n == name {
			return "", errors.New("not found")
		}
		return "/usr/bin/" + n, nil
	}
	t.Cleanup(func() { lookPath = orig })
}

func TestSyncOnASelfMachineRefusesWhenAnsiblePlaybookIsAbsent(t *testing.T) {
	withMissingBinary(t, "ansible-playbook")

	_, err := execute(t, "--config", selfConfig(t), "sync", "--yes")
	if err == nil {
		t.Fatal("sync ran without ansible-playbook on PATH")
	}
	if !strings.Contains(err.Error(), "setup") {
		t.Fatalf("the error does not name setup: %v", err)
	}
}

func TestSSHRefusesOnASelfMachine(t *testing.T) {
	captureInteractive(t)

	_, err := execute(t, "--config", selfConfig(t), "ssh")
	if err == nil {
		t.Fatal("ssh opened a session on a self machine")
	}
	if !strings.Contains(err.Error(), "self: true") || !strings.Contains(err.Error(), "ssh") {
		t.Fatalf("got %v", err)
	}
}

func TestMoshRefusesOnASelfMachine(t *testing.T) {
	captureInteractive(t)

	_, err := execute(t, "--config", selfConfig(t), "mosh")
	if err == nil {
		t.Fatal("mosh opened a session on a self machine")
	}
	if !strings.Contains(err.Error(), "self: true") {
		t.Fatalf("got %v", err)
	}
}

// tunnel and expose add reach `firstAddress` the same way ssh and mosh do,
// through the same shared guard; a workspace can never live on a self
// machine (config.Validate refuses it), so there is no config that reaches
// tunnel with a self target for an end-to-end test. The guard itself is
// exercised directly here instead.
func TestFirstAddressRefusesOnASelfMachine(t *testing.T) {
	_, err := firstAddress(config.Machine{Name: "mac", Self: true}, "tunnel")
	if err == nil {
		t.Fatal("firstAddress answered for a self machine")
	}
	if !strings.Contains(err.Error(), "mac") || !strings.Contains(err.Error(), "self: true") ||
		!strings.Contains(err.Error(), "tunnel") {
		t.Fatalf("got %v", err)
	}
}

func TestMachinesTrustRefusesOnASelfMachine(t *testing.T) {
	_, err := execute(t, "--config", selfConfig(t), "machines", "trust", "mac")
	if err == nil {
		t.Fatal("trust scanned a host key on a self machine")
	}
	if !strings.Contains(err.Error(), "self: true") {
		t.Fatalf("got %v", err)
	}
}

func TestMachinesAddSelfWritesTheMachineAndPreparesIt(t *testing.T) {
	dir := writeConfigDir(t, "# the machine I bought first\nmachines:\n  - name: server\n    hosts: [203.0.113.10]\n")
	fake := writeFakeBootstrap(t, dir, "mac-brew", "")

	out, err := execute(t, "--config", dir, "machines", "add", "--self", "mac")
	if err != nil {
		t.Fatalf("machines add --self returned %v (%s)", err, out)
	}
	if fake.installed() {
		t.Fatal("it installed something when nothing was missing")
	}
	if !strings.Contains(out, "mac is already prepared: ansible-playbook is /opt/homebrew/bin/ansible-playbook.") {
		t.Fatalf("it does not say the machine is prepared: %s", out)
	}

	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Machines) != 2 || !cfg.Machines[1].Self || cfg.Machines[1].Name != "mac" {
		t.Fatalf("machines = %#v", cfg.Machines)
	}
	if len(cfg.Machines[1].Hosts) != 0 || cfg.Machines[1].User != "" || cfg.Machines[1].Port != 0 {
		t.Fatalf("a self machine got address fields: %#v", cfg.Machines[1])
	}

	body, err := os.ReadFile(filepath.Join(dir, config.FileName))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "# the machine I bought first") {
		t.Fatalf("the comment is gone:\n%s", body)
	}
}

func TestMachinesAddSelfRefusesWhenOneAlreadyExists(t *testing.T) {
	dir := writeConfigDir(t, "machines:\n  - name: mac\n    self: true\n")

	_, err := execute(t, "--config", dir, "machines", "add", "--self", "mac2")
	if err == nil {
		t.Fatal("a second self machine was allowed")
	}
	if !strings.Contains(err.Error(), "self: true") {
		t.Fatalf("got %v", err)
	}
}

func TestMachinesAddSelfRefusesANameThatIsTaken(t *testing.T) {
	dir := writeConfigDir(t, "machines:\n  - name: mac\n    hosts: [203.0.113.10]\n")

	_, err := execute(t, "--config", dir, "machines", "add", "--self", "mac")
	if err == nil {
		t.Fatal("a taken name was accepted")
	}
	if !strings.Contains(err.Error(), "mac") {
		t.Fatalf("got %v", err)
	}
}
