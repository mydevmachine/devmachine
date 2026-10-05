package local

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
)

func TestParseDistroAcceptsUbuntuAndArch(t *testing.T) {
	for _, c := range []struct {
		in   string
		want Distro
	}{
		{"", Ubuntu},
		{"ubuntu", Ubuntu},
		{"arch", Arch},
	} {
		got, err := ParseDistro(c.in)
		if err != nil {
			t.Fatalf("%q: %v", c.in, err)
		}
		if got != c.want {
			t.Fatalf("%q: got %q, want %q", c.in, got, c.want)
		}
	}
}

func TestParseDistroNamesWhatItTakes(t *testing.T) {
	_, err := ParseDistro("fedora")
	if err == nil {
		t.Fatal("it accepted fedora")
	}
	for _, want := range []string{`"fedora"`, "ubuntu", "arch"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the error does not carry %q: %v", want, err)
		}
	}
}

// TestArchTemplateIsX86UnderQemu: the aarch64 Arch image Lima offers is a 2022
// third-party build that does not boot under vz, so Arch is x86_64 everywhere.
func TestArchTemplateIsX86UnderQemu(t *testing.T) {
	body := string(Template(Arch))
	for _, want := range []string{"vmType: qemu", "\narch: x86_64", "Arch-Linux-x86_64-cloudimg"} {
		if !strings.Contains(body, want) {
			t.Fatalf("the Arch template does not carry %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, `arch: "aarch64"`) || strings.Contains(body, "arch: aarch64") {
		t.Fatal("the Arch template offers the aarch64 image")
	}
}

// TestArchTemplateArrivesLikeABoughtServer holds the Arch VM to the same
// promise as the Ubuntu one: root by password, no key, nothing shared.
func TestArchTemplateArrivesLikeABoughtServer(t *testing.T) {
	body := string(Template(Arch))
	for _, want := range []string{
		"sshd_config.d/01-", "PermitRootLogin yes", "PasswordAuthentication yes", "chpasswd",
		"rm -f /root/.ssh/authorized_keys", "systemctl restart sshd", "mounts: []", "public on purpose",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("the Arch template does not carry %q", want)
		}
	}
	if strings.Contains(body, "authorizedKeys") || strings.Contains(body, "ssh-ed25519") {
		t.Fatal("the Arch template installs a key")
	}
}

func TestRenderSizesTheArchTemplate(t *testing.T) {
	got, err := Render(Arch, DefaultSize)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, Template(Arch)) {
		t.Fatal("the default size changed the Arch template: the flags' defaults and machine-arch.yaml disagree")
	}
	got, err = Render(Arch, Size{CPUs: 4, MemoryGiB: 6, DiskGiB: 30})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "\ncpus: 4\n") || !strings.Contains(string(got), "Arch-Linux") {
		t.Fatalf("got:\n%s", got)
	}
}

func TestCreateBootsTheChosenDistro(t *testing.T) {
	r := stub(t, nil)

	if _, err := Create(context.Background(), "alpha", Arch, DefaultSize, io.Discard); err != nil {
		t.Fatal(err)
	}
	if len(r.bodies) != 1 || r.bodies[0] != string(Template(Arch)) {
		t.Fatalf("limactl did not get the Arch template: %q", r.bodies)
	}
}
