package commands

import (
	"context"
	"encoding/json"
	"io"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mydevmachine/devmachine/internal/config"
	"github.com/mydevmachine/devmachine/internal/hostkeys"
	"github.com/mydevmachine/devmachine/internal/keys"
	"github.com/mydevmachine/devmachine/internal/local"
)

func stubCreateLocal(t *testing.T) *[]string {
	t.Helper()
	created, _ := stubCreateLocalSized(t)
	return created
}

func stubCreateLocalSized(t *testing.T) (*[]string, *[]local.Size) {
	t.Helper()
	created, sizes, _ := stubCreateLocalAll(t)
	return created, sizes
}

func stubCreateLocalAll(t *testing.T) (*[]string, *[]local.Size, *[]local.Distro) {
	t.Helper()
	created := &[]string{}
	sizes := &[]local.Size{}
	distros := &[]local.Distro{}
	t.Cleanup(swap(&createLocal, func(_ context.Context, name string, distro local.Distro, size local.Size,
		_ io.Writer) (config.Machine, error) {
		*created = append(*created, name)
		*sizes = append(*sizes, size)
		*distros = append(*distros, distro)
		return config.Machine{
			Name: name, Hosts: []config.Host{{Address: local.Address}}, User: local.AdminUser, Port: 60022,
		}, nil
	}))
	return created, sizes, distros
}

func TestCreateLocalKeepsTheDefaultSizeWithoutFlags(t *testing.T) {
	_, sizes := stubCreateLocalSized(t)

	out, err := executeWithInput(t, "", "--config", filepath.Join(t.TempDir(), "devmachine"),
		"machines", "create-local", "sandbox")
	if err != nil {
		t.Fatalf("create-local returned %v (%s)", err, out)
	}
	if len(*sizes) != 1 || (*sizes)[0] != local.DefaultSize {
		t.Fatalf("sizes = %#v, want the default", *sizes)
	}
	if !strings.Contains(out, "2 CPUs, 4 GiB memory, 20 GiB disk") {
		t.Fatalf("the size is not reported: %q", out)
	}
}

func TestCreateLocalPassesTheChosenSizeAndReportsIt(t *testing.T) {
	_, sizes := stubCreateLocalSized(t)

	out, err := executeWithInput(t, "", "--config", filepath.Join(t.TempDir(), "devmachine"), "--format", "json",
		"machines", "create-local", "sandbox", "--cpus", "3", "--memory", "6", "--disk", "30")
	if err != nil {
		t.Fatalf("create-local returned %v (%s)", err, out)
	}
	want := local.Size{CPUs: 3, MemoryGiB: 6, DiskGiB: 30}
	if len(*sizes) != 1 || (*sizes)[0] != want {
		t.Fatalf("sizes = %#v, want %#v", *sizes, want)
	}
	start := strings.Index(out, "{")
	var got machineJSON
	if start < 0 || json.Unmarshal([]byte(out[start:]), &got) != nil {
		t.Fatalf("no JSON at the end: %q", out)
	}
	if got.Size == nil || *got.Size != (sizeJSON{CPUs: 3, MemoryGiB: 6, DiskGiB: 30}) {
		t.Fatalf("reported size %#v", got.Size)
	}
}

func TestCreateLocalWithAddAddsTheMachineWithItsPassword(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "devmachine")
	stubCreateLocal(t)
	steps := stubBootstrap(t, bootstrapStubs{keyWorks: false})

	out, err := executeWithInput(t, "", "--config", dir, "--format", "json",
		"machines", "create-local", "sandbox", "--add", "--no-aliases")
	if err != nil {
		t.Fatalf("create-local --add returned %v (%s)", err, out)
	}
	if steps.password != local.Password || !steps.installedKey || !steps.proved {
		t.Fatalf("it did not install the key with the local password: %q", steps.events)
	}
	if slices.Contains(steps.events, "ask trust") {
		t.Fatalf("it asked to trust the host key: %q", steps.events)
	}

	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatalf("no configuration: %v", err)
	}
	m, err := cfg.Machine("sandbox")
	if err != nil {
		t.Fatal(err)
	}
	if m.Port != 60022 || m.Hosts[0].Address != local.Address || m.Key != filepath.Join(keys.Dir(dir), "sandbox") {
		t.Fatalf("machine = %#v", m)
	}

	start := strings.Index(out, "{")
	var got machineJSON
	if start < 0 || json.Unmarshal([]byte(out[start:]), &got) != nil {
		t.Fatalf("no JSON at the end: %q", out)
	}
	if got.Name != "sandbox" || got.Key != m.Key || got.Port != 60022 {
		t.Fatalf("reported %#v", got)
	}
}

func TestCreateLocalWithAddRefusesATakenNameBeforeCreatingAnything(t *testing.T) {
	dir := writeConfigDir(t, "machines:\n  - name: sandbox\n    hosts: [203.0.113.10]\n")
	created := stubCreateLocal(t)
	stubBootstrap(t, bootstrapStubs{keyWorks: true})

	_, err := executeWithInput(t, "", "--config", dir, "machines", "create-local", "sandbox", "--add")
	if err == nil || !strings.Contains(err.Error(), "already configured") {
		t.Fatalf("got %v", err)
	}
	if len(*created) != 0 {
		t.Fatal("it created a VM it was never going to add")
	}
}

func TestCreateLocalWithoutAddWritesNoConfiguration(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "devmachine")
	stubCreateLocal(t)
	steps := stubBootstrap(t, bootstrapStubs{keyWorks: true})

	out, err := executeWithInput(t, "", "--config", dir, "machines", "create-local", "sandbox")
	if err != nil {
		t.Fatal(err)
	}
	if len(steps.events) != 0 {
		t.Fatalf("it reached the machine: %q", steps.events)
	}
	if _, err := config.Load(dir); err == nil {
		t.Fatal("it wrote a configuration")
	}
	if !strings.Contains(out, "--add") {
		t.Fatalf("it does not say how to add it in one step: %q", out)
	}
}

func TestCreateLocalRefusesAddFlagsWithoutAdd(t *testing.T) {
	created := stubCreateLocal(t)

	_, err := execute(t, "--config", t.TempDir(), "machines", "create-local", "sandbox", "--no-aliases")
	if err == nil || !strings.Contains(err.Error(), "--add") || len(*created) != 0 {
		t.Fatalf("got %v, created %v", err, *created)
	}
}

func TestCreateLocalWithAddScansTheHostKeyOnce(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "devmachine")
	stubCreateLocal(t)
	steps := stubBootstrap(t, bootstrapStubs{keyWorks: true})

	if out, err := executeWithInput(t, "", "--config", dir, "machines", "create-local", "sandbox", "--add",
		"--no-aliases"); err != nil {
		t.Fatalf("create-local --add returned %v (%s)", err, out)
	}
	scans := 0
	for _, e := range steps.events {
		if e == "scan host key" {
			scans++
		}
	}
	if scans != 1 {
		t.Fatalf("it scanned the host key %d times: %q", scans, steps.events)
	}
}

func TestCreateLocalWithAddFailingPrintsACommandThatWorks(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "devmachine")
	stubCreateLocal(t)
	steps := stubBootstrap(t, bootstrapStubs{keyWorks: false, proofFails: true})

	_, err := executeWithInput(t, "", "--config", dir, "machines", "create-local", "sandbox", "--add",
		"--no-aliases")
	if err == nil {
		t.Fatal("an unproved key was accepted")
	}
	want := "printf '%s' devmachine | devmachine --config " + dir + " machines add --name sandbox " +
		"--address 127.0.0.1 --port 60022 --fingerprint " + hostkeys.Fingerprint(steps.hostKey) +
		" --password-stdin --no-aliases"
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("got %v\nwant it to contain %s", err, want)
	}
}

func TestCreateLocalMakesUbuntuUnlessToldOtherwise(t *testing.T) {
	_, _, distros := stubCreateLocalAll(t)

	if out, err := executeWithInput(t, "", "--config", filepath.Join(t.TempDir(), "devmachine"),
		"machines", "create-local", "sandbox"); err != nil {
		t.Fatalf("create-local returned %v (%s)", err, out)
	}
	if !slices.Equal(*distros, []local.Distro{local.Ubuntu}) {
		t.Fatalf("distros = %q", *distros)
	}
}

func TestCreateLocalPassesTheChosenDistro(t *testing.T) {
	_, _, distros := stubCreateLocalAll(t)

	if out, err := executeWithInput(t, "", "--config", filepath.Join(t.TempDir(), "devmachine"),
		"machines", "create-local", "sandbox", "--distro", "arch"); err != nil {
		t.Fatalf("create-local returned %v (%s)", err, out)
	}
	if !slices.Equal(*distros, []local.Distro{local.Arch}) {
		t.Fatalf("distros = %q", *distros)
	}
}

func TestCreateLocalRefusesADistroItCannotMakeBeforeCreatingAnything(t *testing.T) {
	created := stubCreateLocal(t)

	_, err := executeWithInput(t, "", "--config", filepath.Join(t.TempDir(), "devmachine"),
		"machines", "create-local", "sandbox", "--distro", "fedora")
	if err == nil || !strings.Contains(err.Error(), "fedora") {
		t.Fatalf("got %v", err)
	}
	if len(*created) != 0 {
		t.Fatal("it created a VM anyway")
	}
}

func TestCreateLocalWithAddPassesTheChosenDistro(t *testing.T) {
	_, _, distros := stubCreateLocalAll(t)
	stubBootstrap(t, bootstrapStubs{keyWorks: false})

	if out, err := executeWithInput(t, "", "--config", filepath.Join(t.TempDir(), "devmachine"),
		"machines", "create-local", "sandbox", "--distro", "arch", "--add", "--no-aliases"); err != nil {
		t.Fatalf("create-local --add returned %v (%s)", err, out)
	}
	if !slices.Equal(*distros, []local.Distro{local.Arch}) {
		t.Fatalf("distros = %q", *distros)
	}
}
