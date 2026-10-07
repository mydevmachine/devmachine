package dns

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mydevmachine/devmachine/internal/packages"
	"github.com/mydevmachine/devmachine/internal/provision"
	"github.com/mydevmachine/devmachine/internal/remote"
)

// pkgSpec is what a fixture package.yml needs to say, kept small on purpose:
// these tests are about Installed, One and Choose, not about the manifest
// format itself.
type pkgSpec struct {
	name       string
	kind       string
	entrypoint string
	commands   []string
	credential string
}

func providerPackage(name, kind string) pkgSpec {
	return pkgSpec{name: name, kind: kind, entrypoint: "bin/provider", commands: []string{"*"}, credential: name}
}

func plainPackage(name string) pkgSpec {
	return pkgSpec{name: name}
}

func providerPackageWithoutCredential(name string) pkgSpec {
	return pkgSpec{name: name, kind: KindDNS, entrypoint: "bin/provider", commands: []string{"*"}}
}

// writeManifest puts one package.yml under the store, so store.Get(name)
// finds it whether or not the lock says it is installed.
func writeManifest(t *testing.T, dir string, spec pkgSpec) {
	t.Helper()

	pkgDir := filepath.Join(packages.LocalDir(dir), spec.name)
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatal(err)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "format: 1\nname: %s\n", spec.name)
	if spec.kind != "" {
		fmt.Fprintf(&b, "kind: %s\n", spec.kind)
	}
	if spec.entrypoint != "" {
		fmt.Fprintf(&b, "entrypoint: %s\n", spec.entrypoint)
	}
	if len(spec.commands) > 0 {
		b.WriteString("commands:\n")
		for _, c := range spec.commands {
			fmt.Fprintf(&b, "  - %q\n", c)
		}
	}
	if spec.credential != "" {
		b.WriteString("credentials:\n")
		fmt.Fprintf(&b, "  - name: %s\n    kind: secret\n    env: %s_TOKEN\n",
			spec.credential, strings.ToUpper(spec.credential))
	}

	if err := os.WriteFile(packages.ManifestPath(pkgDir), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

// configDirWith writes each package's manifest and locks it onto machine
// "main": the ordinary state of a package that was added and synced.
func configDirWith(t *testing.T, specs ...pkgSpec) string {
	t.Helper()

	dir := t.TempDir()
	entries := make([]packages.LockEntry, 0, len(specs))
	for _, spec := range specs {
		writeManifest(t, dir, spec)
		entries = append(entries, packages.LockEntry{Name: spec.name, Source: packages.SourceLocal})
	}

	lock := packages.Lock{
		AppliedAt: time.Now().UTC().Format(time.RFC3339),
		Machines:  map[string][]packages.LockEntry{"main": entries},
	}
	if err := packages.SaveLock(dir, lock); err != nil {
		t.Fatal(err)
	}
	return dir
}

// configDirWithForWorkspace is configDirWith, but the lock says the package
// is installed for a workspace rather than for the machine.
func configDirWithForWorkspace(t *testing.T, workspace string, specs ...pkgSpec) string {
	t.Helper()

	dir := t.TempDir()
	entries := make([]packages.LockEntry, 0, len(specs))
	for _, spec := range specs {
		writeManifest(t, dir, spec)
		entries = append(entries, packages.LockEntry{Name: spec.name, Source: packages.SourceLocal})
	}

	lock := packages.Lock{
		AppliedAt:  time.Now().UTC().Format(time.RFC3339),
		Workspaces: map[string][]packages.LockEntry{workspace: entries},
	}
	if err := packages.SaveLock(dir, lock); err != nil {
		t.Fatal(err)
	}
	return dir
}

// configDirWithCachedButNotInstalled writes a package's manifest into the
// store without locking it onto any machine: the cache holds every recipe in
// the release, and only the lock says what was actually applied.
func configDirWithCachedButNotInstalled(t *testing.T, spec pkgSpec) string {
	t.Helper()
	dir := t.TempDir()
	writeManifest(t, dir, spec)
	return dir
}

// holdingClient answers `zones` for each provider named in the map, told
// apart by the provider's name in its entrypoint path, and otherwise says it
// holds nothing.
type holdingClient struct {
	zones map[string][]string
}

func (h *holdingClient) Run(_ context.Context, command string) (string, error) {
	for name, zones := range h.zones {
		if strings.Contains(command, "/"+name+"/") {
			body, err := json.Marshal(struct {
				Zones []string `json:"zones"`
			}{Zones: zones})
			return string(body), err
		}
	}
	return `{"zones": []}`, nil
}

func (h *holdingClient) RunInput(ctx context.Context, command string, _ io.Reader) (string, error) {
	return h.Run(ctx, command)
}

func (h *holdingClient) Stream(ctx context.Context, command string, stdout, _ io.Writer) error {
	out, err := h.Run(ctx, command)
	_, _ = io.WriteString(stdout, out)
	return err
}

func (h *holdingClient) Upload(context.Context, string, io.Reader) error { return nil }

func (h *holdingClient) Close() error { return nil }

// configDirWithProvidersHolding installs one DNS provider package per key,
// wired to a client that answers each provider's own zones.
func configDirWithProvidersHolding(t *testing.T, holdings map[string][]string) (string, remote.Client) {
	t.Helper()

	specs := make([]pkgSpec, 0, len(holdings))
	for name := range holdings {
		specs = append(specs, providerPackage(name, KindDNS))
	}
	return configDirWith(t, specs...), &holdingClient{zones: holdings}
}

func TestInstalledFindsOnlyDNSPackages(t *testing.T) {
	dir := configDirWith(t,
		providerPackage("cloudflare", "dns"),
		providerPackage("something-else", "other"),
		plainPackage("docker"),
	)

	got, err := Installed(dir, "main", provision.RemoteDir, &recordingClient{out: `{"zones": []}`})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name() != "cloudflare" {
		t.Fatalf("got %#v", got)
	}
}

func TestInstalledIgnoresAPackageThatIsNotOnThisMachine(t *testing.T) {
	// The cache holds every recipe in the release. Only the lock says what
	// was applied, and asking a provider that was never installed fails in a
	// way nobody can act on.
	dir := configDirWithCachedButNotInstalled(t, providerPackage("cloudflare", "dns"))

	got, err := Installed(dir, "main", provision.RemoteDir, &recordingClient{out: `{"zones": []}`})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("got %#v", got)
	}
}

func TestOneSaysHowToInstallAProviderThatIsNotThere(t *testing.T) {
	_, err := One(configDirWith(t), "main", provision.RemoteDir, "cloudflare", &recordingClient{})
	if err == nil {
		t.Fatal("a provider that is not installed was used")
	}
	if !strings.Contains(err.Error(), "devmachine packages add cloudflare") {
		t.Fatalf("the error does not say how to install it: %v", err)
	}
	// Adding it writes a line in the configuration. Until `sync` runs, there
	// is nothing on the machine to call.
	if !strings.Contains(err.Error(), "sync") {
		t.Fatalf("the error stops one step short: %v", err)
	}
}

func TestOneRefusesAPackageOfAnotherKind(t *testing.T) {
	dir := configDirWith(t, plainPackage("docker"))

	_, err := One(dir, "main", provision.RemoteDir, "docker", &recordingClient{})
	if err == nil {
		t.Fatal("a plain machine package was used as a DNS provider")
	}
	if !strings.Contains(err.Error(), "docker") {
		t.Fatalf("the error does not name it: %v", err)
	}
}

func TestOneRefusesAProviderWithNoCredential(t *testing.T) {
	// Without a credential the shell would fail on a missing file, with a
	// message that says nothing anybody can act on.
	dir := configDirWith(t, providerPackageWithoutCredential("cloudflare"))

	_, err := One(dir, "main", provision.RemoteDir, "cloudflare", &recordingClient{})
	if err == nil {
		t.Fatal("a provider with no credential was used")
	}
	if !strings.Contains(err.Error(), "credential") {
		t.Fatalf("the error does not say why: %v", err)
	}
}

func TestChooseUsesTheFlagWithoutAskingAnybody(t *testing.T) {
	client := &recordingClient{}
	dir := configDirWith(t, providerPackage("cloudflare", "dns"))

	got, err := Choose(context.Background(), dir, "main", provision.RemoteDir, "www.example.com", "cloudflare", client, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	// The flag is an instruction, not a hint. It is not checked against the
	// zone list, because overriding is the reason it exists.
	if got.Name != "cloudflare" {
		t.Fatalf("got %#v", got)
	}
	if len(client.commands) != 0 {
		t.Fatalf("it asked anyway: %#v", client.commands)
	}
	if !strings.Contains(got.Why, "--dns-provider") {
		t.Fatalf("it does not say why: %#v", got)
	}
}

func TestChooseAsksAndFindsTheHolder(t *testing.T) {
	dir, client := configDirWithProvidersHolding(t, map[string][]string{
		"hostinger":  {"example.com"},
		"cloudflare": {"client.example.net"},
	})

	got, err := Choose(context.Background(), dir, "main", provision.RemoteDir, "app.client.example.net", "", client, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "cloudflare" || got.Zone != "client.example.net" {
		t.Fatalf("got %#v", got)
	}
}

func TestChooseFallsThroughToManual(t *testing.T) {
	dir, client := configDirWithProvidersHolding(t, map[string][]string{
		"hostinger": {"example.com"},
	})

	got, err := Choose(context.Background(), dir, "main", provision.RemoteDir, "www.example.org", "", client, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != ProviderManual {
		t.Fatalf("got %#v", got)
	}
	// Nobody holding the zone is an ordinary state, not a failure: it is
	// somebody whose registrar has no package yet.
	if got.Zone != "www.example.org" {
		t.Fatalf("manual acts on the whole name: %#v", got)
	}
}

func TestChooseSaysWhyOnTheWriterWhenItFellThrough(t *testing.T) {
	dir := configDirWith(t, providerPackage("hostinger", "dns"))
	client := &recordingClient{
		out: `{"error": {"kind": "unauthenticated", "message": "the token was rejected"}}`,
		err: errors.New("Process exited with status 1"),
	}

	var out strings.Builder
	got, err := Choose(context.Background(), dir, "main", provision.RemoteDir, "www.example.com", "", client, &out)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != ProviderManual {
		t.Fatalf("got %#v", got)
	}
	// Falling through to manual because a token expired, silently, is how
	// somebody spends an afternoon.
	if !strings.Contains(out.String(), "hostinger") {
		t.Fatalf("it did not say which provider could not be asked: %q", out.String())
	}
}

func TestChooseWithNoMachineReachableIsManualAndNotAnError(t *testing.T) {
	got, err := Choose(context.Background(), configDirWith(t), "main", provision.RemoteDir, "www.example.com", "", nil, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	// A DNS command with the machine down cannot write. It can still print
	// the record somebody has to create by hand, which is better than an
	// error about SSH.
	if got.Name != ProviderManual {
		t.Fatalf("got %#v", got)
	}
}

func TestAnyBuildsAPackageOfAnyKind(t *testing.T) {
	dir := configDirWith(t, providerPackage("cloudflare", "dns"))

	got, err := Any(dir, "main", "", provision.RemoteDir, "cloudflare", &recordingClient{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Name() != "cloudflare" {
		t.Fatalf("got %#v", got)
	}
}

func TestAnyRefusesAPackageWithNoEntrypoint(t *testing.T) {
	dir := configDirWith(t, plainPackage("docker"))

	_, err := Any(dir, "main", "", provision.RemoteDir, "docker", &recordingClient{})
	if err == nil {
		t.Fatal("a package with no entrypoint was called")
	}
	if !strings.Contains(err.Error(), "entrypoint") {
		t.Fatalf("the error does not say why: %v", err)
	}
}

func TestAnyFindsAPackageInstalledForAWorkspace(t *testing.T) {
	dir := configDirWithForWorkspace(t, "alice", providerPackage("cloudflare", "dns"))

	got, err := Any(dir, "main", "alice", provision.RemoteDir, "cloudflare", &recordingClient{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Name() != "cloudflare" {
		t.Fatalf("got %#v", got)
	}
}

func TestAnySaysHowToInstallAPackageThatIsNotThere(t *testing.T) {
	_, err := Any(configDirWith(t), "main", "", provision.RemoteDir, "cloudflare", &recordingClient{})
	if err == nil {
		t.Fatal("a package that is not installed was used")
	}
	if !strings.Contains(err.Error(), "devmachine packages add cloudflare") {
		t.Fatalf("the error does not say how to install it: %v", err)
	}
}

func TestAnyCallsAPackageThatNeedsNoCredential(t *testing.T) {
	dir := configDirWith(t, pkgSpec{name: "tool", entrypoint: "bin/tool", commands: []string{"context"}})
	client := &recordingClient{out: "ok"}

	got, err := Any(dir, "main", "", provision.RemoteDir, "tool", client)
	if err != nil {
		t.Fatalf("a package with no credential must still be reachable: %v", err)
	}
	var out strings.Builder
	if err := got.Call(context.Background(), []string{"context", "--session", "s1"}, &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	if len(client.inputs) != 1 || strings.Contains(client.inputs[0], "set -a") ||
		!strings.Contains(client.inputs[0], "bin/tool context --session s1") {
		t.Fatalf("sent %q", client.inputs)
	}
}

func TestAnyCallsALocalPackageWhereAnsibleUnpackedIt(t *testing.T) {
	dir := configDirWith(t, pkgSpec{name: "tool", entrypoint: "bin/tool", commands: []string{"*"}})
	client := &recordingClient{out: "ok"}

	got, err := Any(dir, "main", "", provision.RemoteDir, "tool", client)
	if err != nil {
		t.Fatal(err)
	}
	if err := got.Call(context.Background(), []string{"context"}, io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	if len(client.inputs) != 1 || !strings.HasPrefix(client.inputs[0], "exec /opt/devmachine/roles.local/tool/bin/tool ") {
		t.Fatalf("sent %q, want the entrypoint under roles.local", client.inputs)
	}
}

func TestManualFromAnUnreachableMachineSaysSo(t *testing.T) {
	var out strings.Builder
	got, err := Choose(context.Background(), configDirWith(t), "main", provision.RemoteDir, "www.example.com", "", nil, &out)
	if err != nil {
		t.Fatal(err)
	}
	if err := got.Provider.Upsert(context.Background(), got.Zone, Record{Name: "@", Type: "A", Value: "203.0.113.10"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "main could not be reached") || strings.Contains(out.String(), "No installed provider holds") {
		t.Fatalf("the reason is the machine, not the providers:\n%s", out.String())
	}
}

func TestManualWithNoProviderInstalledSaysSo(t *testing.T) {
	var out strings.Builder
	got, err := Choose(context.Background(), configDirWith(t), "main", provision.RemoteDir, "www.example.com", "", &recordingClient{}, &out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.Why, "no DNS provider is installed") {
		t.Fatalf("got %#v", got)
	}
	if err := got.Provider.Delete(context.Background(), got.Zone, Record{Name: "@", Type: "A", Value: "203.0.113.10"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "No DNS provider is installed on main") || strings.Contains(out.String(), "No installed provider holds") {
		t.Fatalf("the reason is that none is installed:\n%s", out.String())
	}
}

func TestManualWhenNoInstalledProviderHoldsTheZoneSaysSo(t *testing.T) {
	dir, client := configDirWithProvidersHolding(t, map[string][]string{"hostinger": {"example.com"}})
	var out strings.Builder
	got, err := Choose(context.Background(), dir, "main", provision.RemoteDir, "www.example.org", "", client, &out)
	if err != nil {
		t.Fatal(err)
	}
	if err := got.Provider.Upsert(context.Background(), got.Zone, Record{Name: "@", Type: "A", Value: "203.0.113.10"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "No installed provider holds www.example.org") {
		t.Fatalf("%s", out.String())
	}
}
