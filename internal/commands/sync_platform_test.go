package commands

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mydevmachine/devmachine/internal/config"
	"github.com/mydevmachine/devmachine/internal/facts"
	"github.com/mydevmachine/devmachine/internal/packages"
	"github.com/mydevmachine/devmachine/internal/remote"
)

// onlyOn makes a local package declare the systems it runs on.
func onlyOn(t *testing.T, configDir, name, platforms string) {
	t.Helper()
	path := filepath.Join(packages.LocalDir(configDir), name, packages.FileName)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString("platforms: " + platforms + "\n"); err != nil {
		t.Fatal(err)
	}
}

func refuseToDial(t *testing.T) {
	t.Helper()
	dial = func(context.Context, config.Machine, string) (remote.Client, string, error) {
		return nil, "", errors.New("sync reached the machine before refusing")
	}
	t.Cleanup(func() { dial = remote.Dial })
}

func TestSyncRefusesAPackageForAnotherSystemBeforeReachingTheMachine(t *testing.T) {
	stub := stubSync(t)
	refuseToDial(t)
	dir := configWithPackages(t)
	onlyOn(t, dir, "base", "[linux]")
	saveFacts(t, dir, "main", observedMac)

	_, err := execute(t, "--config", dir, "sync", "--yes")
	want := `package "base" runs on linux; machine "main" is macos`
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("got %v, want %q", err, want)
	}
	if stub.called {
		t.Fatal("Ansible ran")
	}
}

func TestSyncRefusesAWorkspacePackageForAnotherSystem(t *testing.T) {
	stubSync(t)
	refuseToDial(t)
	dir := configWithPackages(t)
	onlyOn(t, dir, "claude-code", "[macos]")
	saveFacts(t, dir, "main", facts.Facts{System: "Linux", Distribution: "Debian"})

	_, err := execute(t, "--config", dir, "sync", "--yes")
	want := `package "claude-code" runs on macos; machine "main" is linux`
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("got %v, want %q", err, want)
	}
}

func TestSyncTakesAPackageThatRunsOnTheMachinesSystem(t *testing.T) {
	stub := stubSync(t)
	dir := configWithPackages(t)
	onlyOn(t, dir, "base", "[linux, macos]")
	saveFacts(t, dir, "main", observedMac)

	if _, err := execute(t, "--config", dir, "sync", "--yes"); err != nil {
		t.Fatal(err)
	}
	if !stub.called {
		t.Fatal("Ansible did not run")
	}
}

func TestSyncDoesNotRefuseAMachineNobodyHasRead(t *testing.T) {
	stub := stubSync(t)
	dir := configWithPackages(t)
	onlyOn(t, dir, "base", "[macos]")

	if _, err := execute(t, "--config", dir, "sync", "--yes"); err != nil {
		t.Fatal(err)
	}
	if !stub.called {
		t.Fatal("Ansible did not run")
	}
}

func TestSyncRefusesOnWhatItJustReadBeforeRunningAnsible(t *testing.T) {
	stub := stubSync(t)
	dialing(t, factsRemote{observed: "kernel=Darwin\nmachine=arm64\nversion=15.7.9\nansible_playbook=\n"})
	dir := configWithPackages(t)
	onlyOn(t, dir, "base", "[linux]")

	_, err := execute(t, "--config", dir, "sync", "--yes")
	want := `package "base" runs on linux; machine "main" is macos`
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("got %v, want %q", err, want)
	}
	if stub.called {
		t.Fatal("Ansible ran")
	}
}
