package commands

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mydevmachine/devmachine/internal/config"
	"github.com/mydevmachine/devmachine/internal/hostkeys"
)

const machinesInThreePlaces = `
machines:
  - name: main
    hosts: [203.0.113.10]
    location: hostinger
  - name: sandbox
    hosts: [203.0.113.20]
  - name: mac
    self: true
`

func TestJSONReportsEveryMachinesEffectiveLocation(t *testing.T) {
	want := map[string]string{"main": "hostinger", "sandbox": "external", "mac": "local"}

	tests := []struct {
		name    string
		args    []string
		decoded func(string) ([]machineJSON, error)
	}{
		{
			name: "config show",
			args: []string{"config", "show"},
			decoded: func(out string) ([]machineJSON, error) {
				var got configJSON
				err := json.Unmarshal([]byte(out), &got)
				return got.Machines, err
			},
		},
		{
			name: "machines list",
			args: []string{"machines", "list"},
			decoded: func(out string) ([]machineJSON, error) {
				var got []machineJSON
				err := json.Unmarshal([]byte(out), &got)
				return got, err
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := writeConfigDir(t, machinesInThreePlaces)

			out, err := execute(t, append([]string{"--config", dir, "--format", "json"}, tt.args...)...)
			if err != nil {
				t.Fatalf("%s returned %v (%s)", tt.name, err, out)
			}
			got, err := tt.decoded(out)
			if err != nil || len(got) != len(want) {
				t.Fatalf("output was not the machines: %v (%q)", err, out)
			}
			for _, m := range got {
				if m.Location != want[m.Name] {
					t.Fatalf("%s: location = %q, want %q", m.Name, m.Location, want[m.Name])
				}
			}
		})
	}
}

func TestJSONAlwaysHasTheLocationKey(t *testing.T) {
	dir := writeConfigDir(t, oneMachine)

	out, err := execute(t, "--config", dir, "--format", "json", "machines", "list")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"location": "external"`) {
		t.Fatalf("no location key: %s", out)
	}
}

func TestMachinesListShowsALocationColumn(t *testing.T) {
	dir := writeConfigDir(t, machinesInThreePlaces)

	out, err := execute(t, "--config", dir, "machines", "list")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 4 || !strings.Contains(lines[0], "LOCATION") {
		t.Fatalf("no LOCATION header: %q", out)
	}
	for _, row := range []struct{ name, location string }{
		{"main", "hostinger"}, {"sandbox", "external"}, {"mac", "local"},
	} {
		found := false
		for _, line := range lines[1:] {
			fields := strings.Fields(line)
			if len(fields) > 0 && fields[0] == row.name {
				found = strings.Contains(line, " "+row.location+" ")
			}
		}
		if !found {
			t.Fatalf("%s is not shown in %s: %q", row.name, row.location, out)
		}
	}
}

func TestMachinesAddWithFlagsWritesTheLocation(t *testing.T) {
	tests := []struct {
		name     string
		flag     []string
		stored   string
		reported string
	}{
		{name: "given", flag: []string{"--location", "  Home Office "}, stored: "home office", reported: "home office"},
		{name: "left out", stored: "", reported: "external"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := writeConfigDir(t, oneMachine)
			steps := stubBootstrap(t, bootstrapStubs{keyWorks: true})

			args := append([]string{"--config", dir, "machines", "add",
				"--name", "sandbox", "--address", "203.0.113.20",
				"--fingerprint", hostkeys.Fingerprint(steps.hostKey), "--no-aliases"}, tt.flag...)
			if out, err := executeWithInput(t, "", args...); err != nil {
				t.Fatalf("machines add returned %v (%s)", err, out)
			}
			cfg, err := config.Load(dir)
			if err != nil {
				t.Fatal(err)
			}
			m, _ := cfg.Machine("sandbox")
			if m.Location != tt.stored || m.EffectiveLocation() != tt.reported {
				t.Fatalf("location = %q (%q), want %q (%q)", m.Location, m.EffectiveLocation(), tt.stored, tt.reported)
			}
		})
	}
}

func TestMachinesAddRefusesABadLocationBeforeReachingTheServer(t *testing.T) {
	dir := writeConfigDir(t, oneMachine)
	before := readConfigFile(t, dir)
	steps := stubBootstrap(t, bootstrapStubs{keyWorks: true})

	_, err := executeWithInput(t, "", "--config", dir, "machines", "add",
		"--name", "sandbox", "--address", "203.0.113.20",
		"--fingerprint", hostkeys.Fingerprint(steps.hostKey), "--location", "home/office")
	if err == nil || !strings.Contains(err.Error(), "location") {
		t.Fatalf("got %v", err)
	}
	if len(steps.events) != 0 {
		t.Fatalf("it reached the server first: %q", steps.events)
	}
	if readConfigFile(t, dir) != before {
		t.Fatal("it wrote the configuration")
	}
}

func TestMachinesAddAsksForTheLocation(t *testing.T) {
	tests := []struct {
		name   string
		answer string
		want   string
	}{
		{name: "the default is external", answer: "", want: ""},
		{name: "external said out loud", answer: "external", want: ""},
		{name: "a place", answer: "Bedroom", want: "bedroom"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := writeConfigDir(t, oneMachine)
			stubBootstrap(t, bootstrapStubs{keyWorks: true})

			out, err := executeWithInput(t, "sandbox\n198.51.100.7\nroot\n22\n"+tt.answer+"\n1\n",
				"--config", dir, "machines", "add", "--no-aliases")
			if err != nil {
				t.Fatalf("machines add returned %v (%s)", err, out)
			}
			if !strings.Contains(out, "location") || !strings.Contains(out, "[external]") {
				t.Fatalf("it did not ask for the location: %q", out)
			}
			cfg, err := config.Load(dir)
			if err != nil {
				t.Fatal(err)
			}
			if m, _ := cfg.Machine("sandbox"); m.Location != tt.want {
				t.Fatalf("location = %q, want %q", m.Location, tt.want)
			}
		})
	}
}

func TestMachinesAddDoesNotAskForALocationGivenAsAFlag(t *testing.T) {
	dir := writeConfigDir(t, oneMachine)
	stubBootstrap(t, bootstrapStubs{keyWorks: true})

	out, err := executeWithInput(t, "sandbox\n198.51.100.7\nroot\n22\n1\n",
		"--config", dir, "machines", "add", "--no-aliases", "--location", "office")
	if err != nil {
		t.Fatalf("machines add returned %v (%s)", err, out)
	}
	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if m, _ := cfg.Machine("sandbox"); m.Location != "office" {
		t.Fatalf("location = %q", m.Location)
	}
}

func TestMachinesAddSelfWritesTheLocation(t *testing.T) {
	dir := writeConfigDir(t, oneMachine)
	writeFakeBootstrap(t, dir, "mac-brew", "")

	if out, err := execute(t, "--config", dir, "machines", "add", "--self", "mac", "--location", "Office"); err != nil {
		t.Fatalf("machines add --self returned %v (%s)", err, out)
	}
	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if m, _ := cfg.Machine("mac"); m.Location != "office" {
		t.Fatalf("location = %q", m.Location)
	}
}

func TestCreateLocalWithAddWritesTheLocation(t *testing.T) {
	tests := []struct {
		name string
		flag []string
		want string
	}{
		{name: "local by default", want: "local"},
		{name: "given", flag: []string{"--location", "Bedroom"}, want: "bedroom"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "devmachine")
			stubCreateLocal(t)
			stubBootstrap(t, bootstrapStubs{keyWorks: true})

			args := append([]string{"--config", dir, "--format", "json",
				"machines", "create-local", "sandbox", "--add", "--no-aliases"}, tt.flag...)
			out, err := executeWithInput(t, "", args...)
			if err != nil {
				t.Fatalf("create-local --add returned %v (%s)", err, out)
			}
			cfg, err := config.Load(dir)
			if err != nil {
				t.Fatal(err)
			}
			if m, _ := cfg.Machine("sandbox"); m.Location != tt.want {
				t.Fatalf("location = %q, want %q", m.Location, tt.want)
			}
			start := strings.Index(out, "{")
			var got machineJSON
			if start < 0 || json.Unmarshal([]byte(out[start:]), &got) != nil || got.Location != tt.want {
				t.Fatalf("reported %#v from %q", got, out)
			}
		})
	}
}

func TestCreateLocalWithoutAddReportsLocal(t *testing.T) {
	stubCreateLocal(t)

	out, err := executeWithInput(t, "", "--config", filepath.Join(t.TempDir(), "devmachine"), "--format", "json",
		"machines", "create-local", "sandbox")
	if err != nil {
		t.Fatal(err)
	}
	start := strings.Index(out, "{")
	var got machineJSON
	if start < 0 || json.Unmarshal([]byte(out[start:]), &got) != nil || got.Location != config.LocationLocal {
		t.Fatalf("reported %#v from %q", got, out)
	}
}

func TestCreateLocalRefusesLocationWithoutAdd(t *testing.T) {
	created := stubCreateLocal(t)

	_, err := execute(t, "--config", t.TempDir(), "machines", "create-local", "sandbox", "--location", "home")
	if err == nil || !strings.Contains(err.Error(), "--add") || len(*created) != 0 {
		t.Fatalf("got %v, created %v", err, *created)
	}
}

func TestCreateLocalRefusesABadLocationBeforeCreatingAnything(t *testing.T) {
	created := stubCreateLocal(t)

	_, err := execute(t, "--config", filepath.Join(t.TempDir(), "devmachine"),
		"machines", "create-local", "sandbox", "--add", "--location", "-home")
	if err == nil || !strings.Contains(err.Error(), "location") || len(*created) != 0 {
		t.Fatalf("got %v, created %v", err, *created)
	}
}

func TestMachinesEditLocation(t *testing.T) {
	tests := []struct {
		name   string
		start  string
		args   []string
		stored string
	}{
		{name: "sets it", args: []string{"--location", "Hostinger"}, stored: "hostinger"},
		{name: "changes it", start: "    location: home\n", args: []string{"--location", "office"}, stored: "office"},
		{name: "clears it", start: "    location: home\n", args: []string{"--location", ""}, stored: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := configWithKey(t, tt.start)

			out, err := execute(t, append([]string{"--config", dir, "machines", "edit", "main", "--yes"}, tt.args...)...)
			if err != nil {
				t.Fatalf("machines edit returned %v (%s)", err, out)
			}
			cfg, err := config.Load(dir)
			if err != nil {
				t.Fatal(err)
			}
			if m, _ := cfg.Machine("main"); m.Location != tt.stored {
				t.Fatalf("location = %q, want %q", m.Location, tt.stored)
			}
		})
	}
}

func TestMachinesEditLocationAndASettingTogether(t *testing.T) {
	dir := configWithKey(t, "    packages: [hostinger]\n")

	if out, err := execute(t, "--config", dir, "machines", "edit", "main", "--yes",
		"--location", "hostinger", "--set", "hostinger.zones=[example.com]"); err != nil {
		t.Fatalf("machines edit returned %v (%s)", err, out)
	}
	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	m, _ := cfg.Machine("main")
	if m.Location != "hostinger" || m.Settings["hostinger.zones"] == nil {
		t.Fatalf("machine = %#v", m)
	}
}

func TestMachinesEditRefusesABadLocation(t *testing.T) {
	dir := configWithKey(t, "")
	before := readConfigFile(t, dir)

	_, err := execute(t, "--config", dir, "machines", "edit", "main", "--yes", "--location", "home/office")
	if err == nil || !strings.Contains(err.Error(), "location") {
		t.Fatalf("got %v", err)
	}
	if readConfigFile(t, dir) != before {
		t.Fatal("it wrote the configuration")
	}
}

func TestMachinesEditLocationCheckWritesNothing(t *testing.T) {
	dir := configWithKey(t, "")
	before := readConfigFile(t, dir)

	out, err := execute(t, "--config", dir, "machines", "edit", "main", "--check", "--location", "home")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "would") || !strings.Contains(out, "home") {
		t.Fatalf("a dry run should say what it would do: %q", out)
	}
	if readConfigFile(t, dir) != before {
		t.Fatal("a dry run wrote")
	}
}

func TestMachinesEditTheSameLocationIsNothingToChange(t *testing.T) {
	dir := configWithKey(t, "    location: home\n")

	_, err := execute(t, "--config", dir, "machines", "edit", "main", "--yes", "--location", "Home")
	if err == nil || !strings.Contains(err.Error(), "nothing to change") {
		t.Fatalf("got %v", err)
	}
}
