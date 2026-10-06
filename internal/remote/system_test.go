package remote

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func systemClient(kernel, release string) *recordingClient {
	return &recordingClient{output: map[string]string{UnameCommand: kernel, OSReleaseCommand: release}}
}

func TestDetectSystemReadsTheLinuxDistribution(t *testing.T) {
	for _, id := range []string{"debian", "ubuntu", "arch", "archarm"} {
		got, err := DetectSystem(context.Background(), systemClient("Linux\n", "NAME=x\nID="+id+"\n"))
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		if got.Kernel != "Linux" || got.ID != id {
			t.Fatalf("%s: got %#v", id, got)
		}
	}
}

func TestDetectSystemRefusesALinuxItDoesNotSetUp(t *testing.T) {
	c := systemClient("Linux\n", "ID=fedora\n")
	_, err := DetectSystem(context.Background(), c)
	want := `this CLI does not set up "fedora" yet: it supports debian, ubuntu and arch`
	if err == nil || err.Error() != want {
		t.Fatalf("got %v, want %q", err, want)
	}
	var unsupported *UnsupportedSystemError
	if !errors.As(err, &unsupported) {
		t.Fatalf("not an UnsupportedSystemError: %T", err)
	}
}

func TestDetectSystemRefusesASystemThatIsNeitherLinuxNorMacOS(t *testing.T) {
	c := systemClient("FreeBSD\n", "")
	_, err := DetectSystem(context.Background(), c)
	want := `"FreeBSD" is not a system this CLI sets up: it supports Linux (debian, ubuntu, arch) and macOS`
	if err == nil || err.Error() != want {
		t.Fatalf("got %v, want %q", err, want)
	}
	// Reading os-release on a system that has none says nothing useful.
	if strings.Contains(c.transcript(), OSReleaseCommand) {
		t.Fatalf("it read os-release on FreeBSD: %s", c.transcript())
	}
}

func TestDetectSystemReadsTheMacOSVersion(t *testing.T) {
	c := &recordingClient{output: map[string]string{UnameCommand: "Darwin\n", MacVersionCommand: "15.7.9\n"}}
	got, err := DetectSystem(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	if !got.MacOS() || got.Version != "15.7.9" || got.ID != "" {
		t.Fatalf("got %#v", got)
	}
	if got.String() != "macos 15.7.9" {
		t.Fatalf("a person reads %q", got.String())
	}
	if strings.Contains(c.transcript(), OSReleaseCommand) {
		t.Fatalf("it read os-release on a Mac: %s", c.transcript())
	}
}

func TestDetectSystemSaysWhenItCannotReadTheMacOSVersion(t *testing.T) {
	c := &recordingClient{output: map[string]string{UnameCommand: "Darwin\n"}, failOn: "sw_vers"}
	_, err := DetectSystem(context.Background(), c)
	if err == nil || !strings.Contains(err.Error(), "sw_vers") {
		t.Fatalf("got %v", err)
	}
}

func TestALinuxSystemIsNotMacOS(t *testing.T) {
	if (System{Kernel: "Linux", ID: "debian"}).MacOS() {
		t.Fatal("debian reads as macOS")
	}
	if got := (System{Kernel: "Linux", ID: "debian"}).String(); got != "debian" {
		t.Fatalf("debian reads as %q", got)
	}
}

func TestDetectSystemSaysWhatItCouldNotRead(t *testing.T) {
	for _, failOn := range []string{"uname", "os-release"} {
		c := systemClient("Linux\n", "ID=debian\n")
		c.failOn = failOn
		_, err := DetectSystem(context.Background(), c)
		if err == nil || !strings.Contains(err.Error(), failOn) {
			t.Fatalf("%s: got %v", failOn, err)
		}
		var unsupported *UnsupportedSystemError
		if errors.As(err, &unsupported) {
			t.Fatalf("%s: a failed read was reported as an unsupported system", failOn)
		}
	}
}

// TestEveryInstallableDistributionIsNamedInTheRefusal keeps the sentence a
// refused machine reads in step with the table that decides.
func TestEveryInstallableDistributionIsNamedInTheRefusal(t *testing.T) {
	for id := range ansibleInstall {
		named := false
		for _, name := range supportedLinuxNames {
			if strings.HasPrefix(id, name) {
				named = true
			}
		}
		if !named {
			t.Fatalf("%q installs Ansible but the refusal does not name it: %q", id, supportedLinuxNames)
		}
	}
}

func TestSupportedLinuxIsTheInstallTable(t *testing.T) {
	for id := range ansibleInstall {
		if !SupportedLinux(id) {
			t.Fatalf("%q is in the install table but not supported", id)
		}
	}
	if SupportedLinux("fedora") {
		t.Fatal("fedora is supported without a way to install Ansible")
	}
}
