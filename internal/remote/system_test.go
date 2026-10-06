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
	want := `this CLI does not set up "fedora" yet: it supports debian, ubuntu and arch, and systems based on them`
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

func TestDetectSystemSetsUpADerivativeTheWayOfWhatItIsBasedOn(t *testing.T) {
	for _, c := range []struct{ name, release, id, base string }{
		{"manjaro", "NAME=\"Manjaro Linux\"\nID=manjaro\nID_LIKE=arch\n", "manjaro", "arch"},
		{"manjaro arm", "NAME=\"Manjaro ARM\"\nID=manjaro-arm\nID_LIKE=\"manjaro arch\"\n", "manjaro-arm", "arch"},
		{"endeavouros", "NAME=\"EndeavourOS\"\nID=\"endeavouros\"\nID_LIKE=\"arch\"\n", "endeavouros", "arch"},
		{"cachyos", "NAME=\"CachyOS Linux\"\nID=cachyos\nID_LIKE=arch\n", "cachyos", "arch"},
		{"linux mint", "NAME=\"Linux Mint\"\nID=linuxmint\nID_LIKE=\"ubuntu debian\"\n", "linuxmint", "ubuntu"},
		{"lmde", "NAME=\"LMDE\"\nID=linuxmint\nID_LIKE=debian\n", "linuxmint", "debian"},
		{"pop os", "NAME=\"Pop!_OS\"\nID=pop\nID_LIKE=\"ubuntu debian\"\n", "pop", "ubuntu"},
		{"old raspbian", "NAME=\"Raspbian GNU/Linux\"\nID=raspbian\nID_LIKE=debian\n", "raspbian", "debian"},
		{"quoted with spaces", "ID='zorin'\nID_LIKE=' ubuntu  debian '\n", "zorin", "ubuntu"},
	} {
		got, err := DetectSystem(context.Background(), systemClient("Linux\n", c.release))
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got.ID != c.id || got.Base() != c.base || !got.Derivative() {
			t.Fatalf("%s: got %#v, base %q", c.name, got, got.Base())
		}
	}
}

func TestASupportedDistributionIsNotADerivative(t *testing.T) {
	for _, release := range []string{
		"NAME=\"Debian GNU/Linux\"\nID=debian\n",
		"PRETTY_NAME=\"Raspberry Pi OS\"\nNAME=\"Debian GNU/Linux\"\nID=debian\n",
		"NAME=\"Ubuntu\"\nID=ubuntu\nID_LIKE=debian\n",
		"NAME=\"Arch Linux ARM\"\nID=archarm\nID_LIKE=arch\n",
	} {
		got, err := DetectSystem(context.Background(), systemClient("Linux\n", release))
		if err != nil {
			t.Fatalf("%q: %v", release, err)
		}
		if got.Derivative() || got.Base() != got.ID {
			t.Fatalf("%q: got %#v", release, got)
		}
		if got.Describe() != got.ID {
			t.Fatalf("%q: a person reads %q", release, got.Describe())
		}
	}
}

func TestDetectSystemStillRefusesWhatIsNotBasedOnASupportedDistribution(t *testing.T) {
	for _, release := range []string{
		"NAME=\"Fedora Linux\"\nID=fedora\n",
		"NAME=\"Rocky Linux\"\nID=\"rocky\"\nID_LIKE=\"rhel centos fedora\"\n",
		"NAME=\"openSUSE Tumbleweed\"\nID=\"opensuse-tumbleweed\"\nID_LIKE=\"opensuse suse\"\n",
		"NAME=\"Alpine Linux\"\nID=alpine\n",
		"ID=archlike\nID_LIKE=archlinux\n",
	} {
		_, err := DetectSystem(context.Background(), systemClient("Linux\n", release))
		var unsupported *UnsupportedSystemError
		if !errors.As(err, &unsupported) {
			t.Fatalf("%q: got %v, want a refusal", release, err)
		}
	}
}

func TestADerivativeSaysItIsNotTested(t *testing.T) {
	s := System{Kernel: "Linux", ID: "manjaro", Like: "arch"}
	want := "manjaro, which is based on arch: it is set up the arch way, but devmachine is not tested on it"
	if got := s.Describe(); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if s.String() != "manjaro" {
		t.Fatalf("String is %q", s.String())
	}
}

func TestLinuxBaseReadsIDLikeOnlyWhenIDIsNotSupported(t *testing.T) {
	for _, c := range []struct{ id, like, base string }{
		{"debian", "", "debian"},
		{"ubuntu", "debian", "ubuntu"},
		{"archarm", "arch", "archarm"},
		{"manjaro", "arch", "arch"},
		{"pop", "ubuntu debian", "ubuntu"},
		{"pop", `"ubuntu debian"`, "ubuntu"},
		{"fedora", "", ""},
		{"rocky", "rhel centos fedora", ""},
	} {
		if got := LinuxBase(c.id, c.like); got != c.base {
			t.Fatalf("%s like %q: got %q, want %q", c.id, c.like, got, c.base)
		}
	}
}
