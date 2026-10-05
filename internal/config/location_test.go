package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeLocation(t *testing.T) {
	tests := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{in: "", want: ""},
		{in: "   ", want: ""},
		{in: "home", want: "home"},
		{in: "  Hostinger ", want: "hostinger"},
		{in: "Living Room", want: "living room"},
		{in: "rack-2.dc_1", want: "rack-2.dc_1"},
		{in: "9th floor", want: "9th floor"},
		{in: strings.Repeat("a", 40), want: strings.Repeat("a", 40)},
		{in: strings.Repeat("a", 41), wantErr: true},
		{in: "-home", wantErr: true},
		{in: ".home", wantErr: true},
		{in: "home/office", wantErr: true},
		{in: "café", wantErr: true},
		{in: "home\toffice", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := NormalizeLocation(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("NormalizeLocation(%q) = %q, want an error", tt.in, got)
				}
				if !strings.Contains(err.Error(), "40") || !strings.Contains(err.Error(), "letters") {
					t.Fatalf("the error does not say what is allowed: %v", err)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("NormalizeLocation(%q) = %q, %v; want %q", tt.in, got, err, tt.want)
			}
		})
	}
}

func TestEffectiveLocation(t *testing.T) {
	tests := []struct {
		name    string
		machine Machine
		want    string
	}{
		{name: "a server with none is external", machine: Machine{Name: "main"}, want: LocationExternal},
		{name: "your computer with none is local", machine: Machine{Name: "mac", Self: true}, want: LocationLocal},
		{name: "a set one wins", machine: Machine{Name: "main", Location: "hostinger"}, want: "hostinger"},
		{name: "a set one wins on self", machine: Machine{Name: "mac", Self: true, Location: "office"}, want: "office"},
		{name: "a hand-written one is normalized", machine: Machine{Name: "main", Location: " Home "}, want: "home"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.machine.EffectiveLocation(); got != tt.want {
				t.Fatalf("EffectiveLocation() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestValidateRefusesALocationThatIsNotAllowed(t *testing.T) {
	c := Config{Machines: []Machine{{Name: "main", Hosts: []Host{{Address: "203.0.113.10"}}, Port: 22, Location: "home/office"}}}

	err := c.Validate()
	if err == nil || !strings.Contains(err.Error(), "main") || !strings.Contains(err.Error(), "location") {
		t.Fatalf("got %v", err)
	}
}

func TestValidateAcceptsAHandWrittenLocationInCapitals(t *testing.T) {
	c := Config{Machines: []Machine{{Name: "main", Hosts: []Host{{Address: "203.0.113.10"}}, Port: 22, Location: "Home"}}}

	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestLoadReadsTheLocation(t *testing.T) {
	dir := configDirWith(t, "machines:\n  - name: main\n    hosts: [203.0.113.10]\n    location: hostinger\n")

	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	m, _ := cfg.Machine("main")
	if m.Location != "hostinger" {
		t.Fatalf("location = %q", m.Location)
	}
}

func TestAddMachineWritesTheLocation(t *testing.T) {
	tests := []struct {
		name    string
		machine Machine
	}{
		{name: "server", machine: Machine{Name: "sandbox", Hosts: []Host{{Address: "203.0.113.20"}}, Port: 22, Location: "home"}},
		{name: "self", machine: Machine{Name: "mac", Self: true, Location: "home"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := configDirWith(t, "machines:\n  - name: main\n    hosts: [203.0.113.10]\n")

			if err := AddMachine(dir, tt.machine); err != nil {
				t.Fatal(err)
			}
			cfg, err := Load(dir)
			if err != nil {
				t.Fatal(err)
			}
			m, _ := cfg.Machine(tt.machine.Name)
			if m.Location != "home" {
				t.Fatalf("location = %q", m.Location)
			}
		})
	}
}

func TestAddMachineWritesNoLocationKeyWhenThereIsNone(t *testing.T) {
	dir := configDirWith(t, "machines:\n  - name: main\n    hosts: [203.0.113.10]\n")

	if err := AddMachine(dir, Machine{Name: "sandbox", Hosts: []Host{{Address: "203.0.113.20"}}, Port: 22}); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "location") {
		t.Fatalf("an empty location was written:\n%s", body)
	}
}

func TestSetMachineLocationWritesItAndKeepsComments(t *testing.T) {
	dir := configDirWith(t, `
machines:
  - name: main  # the server
    hosts: [203.0.113.10]
  - name: sandbox
    hosts: [203.0.113.20]
`)

	if err := SetMachineLocation(dir, "main", "hostinger"); err != nil {
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
	if m, _ := cfg.Machine("main"); m.Location != "hostinger" {
		t.Fatalf("location = %q", m.Location)
	}
	if m, _ := cfg.Machine("sandbox"); m.Location != "" {
		t.Fatalf("it wrote onto another machine: %q", m.Location)
	}
}

func TestSetMachineLocationClearsIt(t *testing.T) {
	dir := configDirWith(t, "machines:\n  - name: main\n    hosts: [203.0.113.10]\n    location: home\n")

	if err := SetMachineLocation(dir, "main", ""); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "location") {
		t.Fatalf("a cleared location was left behind:\n%s", body)
	}
}

func TestSetMachineLocationRefusesOneTheFileDoesNotHave(t *testing.T) {
	dir := configDirWith(t, "machines:\n  - name: main\n    hosts: [203.0.113.10]\n")

	if err := SetMachineLocation(dir, "sandbox", "home"); err == nil {
		t.Fatal("it edited a machine that is not there")
	}
}
