package network

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mydevmachine/devmachine/internal/config"
	"github.com/mydevmachine/devmachine/internal/packages"
)

func writeFile(t *testing.T, path, body string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
}

// networkPackageAt writes a network package whose resolve script is body.
func networkPackageAt(t *testing.T, dir, name, prefix, body string) {
	t.Helper()
	writeFile(t, filepath.Join(dir, name, "package.yml"), "format: 1\nname: "+name+
		"\nscope: machine\nsummary: A private network.\nvariables:\n  server:\n    summary: Where it signs in.\n    default: ''\n"+
		"network:\n  prefix: "+prefix+"\n  resolve: bin/resolve\n", 0o644)
	writeFile(t, filepath.Join(dir, name, "bin", "resolve"), "#!/bin/sh\n"+body, 0o755)
}

func localPackage(t *testing.T, configDir, name, prefix, body string) {
	t.Helper()
	networkPackageAt(t, packages.LocalDir(configDir), name, prefix, body)
}

func releasePackage(t *testing.T, configDir, version, name, prefix, body string) {
	t.Helper()
	networkPackageAt(t, filepath.Join(packages.CacheDir(configDir, version), "packages"), name, prefix, body)
	writeFile(t, filepath.Join(packages.CacheDir(configDir, version), ".checksum"), "abc123\n", 0o644)
}

func TestSplitTellsANetworkEntryFromALiteral(t *testing.T) {
	cases := []struct {
		entry, prefix, name string
		ok                  bool
	}{
		{"tailscale:main", "tailscale", "main", true},
		{"acme-net:box-1", "acme-net", "box-1", true},
		{"203.0.113.10", "", "", false},
		{"vps.example.com", "", "", false},
		{"2001:db8::1", "", "", false},
		{"fd7a:115c:a1e0::1", "", "", false},
		{"[2001:db8::1]", "", "", false},
		{"tailscale:", "", "", false},
		{"Acme:main", "", "", false},
	}
	for _, c := range cases {
		prefix, name, ok := Split(c.entry)
		if prefix != c.prefix || name != c.name || ok != c.ok {
			t.Errorf("Split(%q) = %q, %q, %v", c.entry, prefix, name, ok)
		}
	}
}

func TestLoadFindsNetworkPackagesInTheStore(t *testing.T) {
	configDir := t.TempDir()
	localPackage(t, configDir, "acme-net", "acme", "echo 100.64.0.7\n")
	releasePackage(t, configDir, "v17", "tailscale", "tailscale", "echo 100.64.0.5\n")
	writeFile(t, filepath.Join(packages.LocalDir(configDir), "plain", "package.yml"),
		"format: 1\nname: plain\nscope: machine\nsummary: Not a network.\n", 0o644)

	found, err := Load(config.Machine{ConfigDir: configDir, PackagesRelease: "v17"})
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, p := range found {
		names = append(names, p.Package+"="+p.Network.Prefix)
	}
	if strings.Join(names, ",") != "acme-net=acme,tailscale=tailscale" {
		t.Fatalf("got %v", names)
	}
}

func TestLoadWithoutAConfigurationDirectoryFindsNothing(t *testing.T) {
	found, err := Load(config.Machine{Name: "main"})
	if err != nil || len(found) != 0 {
		t.Fatalf("%v %#v", err, found)
	}
}

func TestPickPrefersThePackageTheMachineInstalls(t *testing.T) {
	providers := []Provider{
		{Package: "acme-a", Network: packages.Network{Prefix: "acme"}},
		{Package: "acme-b", Network: packages.Network{Prefix: "acme"}},
	}

	got, ok := Pick(providers, "acme", []string{"base", "acme-b"})
	if !ok || got.Package != "acme-b" {
		t.Fatalf("got %q %v", got.Package, ok)
	}
}

func TestPickFallsBackToAnyPackageInTheStore(t *testing.T) {
	providers := []Provider{
		{Package: "acme-a", Network: packages.Network{Prefix: "acme"}},
		{Package: "tailscale", Network: packages.Network{Prefix: "tailscale"}},
	}

	got, ok := Pick(providers, "tailscale", []string{"base"})
	if !ok || got.Package != "tailscale" {
		t.Fatalf("got %q %v", got.Package, ok)
	}
	if _, ok := Pick(providers, "zerotier", []string{"base"}); ok {
		t.Fatal("picked a package for a prefix nobody declares")
	}
}

func TestResolveRunsThePackagesScriptWithTheName(t *testing.T) {
	configDir := t.TempDir()
	localPackage(t, configDir, "acme-net", "acme", `[ "$1" = main ] || exit 1
echo 100.64.0.7
echo
echo 2001:db8::7
`)
	p := onlyProvider(t, configDir)

	got, err := p.Resolve(context.Background(), "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, ",") != "100.64.0.7,2001:db8::7" {
		t.Fatalf("got %v", got)
	}
}

func TestResolveHandsTheScriptTheMachinesSettings(t *testing.T) {
	configDir := t.TempDir()
	localPackage(t, configDir, "acme-net", "acme", `case "$DEVMACHINE_SETTINGS" in
*'"server":"https://net.example.com"'*) echo 100.64.0.7 ;;
*) echo "$DEVMACHINE_SETTINGS" >&2; exit 1 ;;
esac
`)
	p := onlyProvider(t, configDir)

	settings := p.Settings(map[string]any{"acme-net.server": "https://net.example.com"})
	if _, err := p.Resolve(context.Background(), "main", settings); err != nil {
		t.Fatal(err)
	}
}

func TestSettingsFallBackToTheDeclaredDefaults(t *testing.T) {
	configDir := t.TempDir()
	localPackage(t, configDir, "acme-net", "acme", "exit 0\n")
	p := onlyProvider(t, configDir)

	if got := p.Settings(nil); len(got) != 1 || got["server"] != "" {
		t.Fatalf("got %#v", got)
	}
}

func TestResolveExitThreeMeansNotAvailableHere(t *testing.T) {
	configDir := t.TempDir()
	localPackage(t, configDir, "acme-net", "acme", "echo 'acme is not running' >&2\nexit 3\n")
	p := onlyProvider(t, configDir)

	_, err := p.Resolve(context.Background(), "main", nil)
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("got %v", err)
	}
	if !strings.Contains(err.Error(), "acme is not running") {
		t.Fatalf("the script's own reason was lost: %v", err)
	}
}

func TestResolveAnyOtherFailureIsAnError(t *testing.T) {
	configDir := t.TempDir()
	localPackage(t, configDir, "acme-net", "acme", "echo 'broken state file' >&2\nexit 1\n")
	p := onlyProvider(t, configDir)

	_, err := p.Resolve(context.Background(), "main", nil)
	if err == nil || errors.Is(err, ErrUnavailable) {
		t.Fatalf("got %v", err)
	}
	if !strings.Contains(err.Error(), "broken state file") {
		t.Fatalf("the script's own reason was lost: %v", err)
	}
}

func TestResolveRefusesSomethingThatIsNotAnAddress(t *testing.T) {
	configDir := t.TempDir()
	localPackage(t, configDir, "acme-net", "acme", "echo 'main.example.com'\n")
	p := onlyProvider(t, configDir)

	if _, err := p.Resolve(context.Background(), "main", nil); err == nil ||
		!strings.Contains(err.Error(), "not an IP address") {
		t.Fatalf("got %v", err)
	}
}

func TestResolveWithNoAddressIsAnError(t *testing.T) {
	configDir := t.TempDir()
	localPackage(t, configDir, "acme-net", "acme", "exit 0\n")
	p := onlyProvider(t, configDir)

	if _, err := p.Resolve(context.Background(), "main", nil); err == nil ||
		!strings.Contains(err.Error(), "no address") {
		t.Fatalf("got %v", err)
	}
}

func TestResolveGivesUpOnAScriptThatHangs(t *testing.T) {
	configDir := t.TempDir()
	localPackage(t, configDir, "acme-net", "acme", "exec sleep 30\n")
	p := onlyProvider(t, configDir)
	was := ResolveTimeout
	ResolveTimeout = 200 * time.Millisecond
	t.Cleanup(func() { ResolveTimeout = was })

	start := time.Now()
	_, err := p.Resolve(context.Background(), "main", nil)
	if err == nil || !strings.Contains(err.Error(), "did not answer within") {
		t.Fatalf("got %v", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("waited %s for a hung script", elapsed)
	}
}

func onlyProvider(t *testing.T, configDir string) Provider {
	t.Helper()
	found, err := Load(config.Machine{ConfigDir: configDir})
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 {
		t.Fatalf("got %#v", found)
	}
	return found[0]
}

func TestResolveOnAMacAlsoFindsTheTailscaleApp(t *testing.T) {
	was := goos
	goos = "darwin"
	t.Cleanup(func() { goos = was })
	configDir := t.TempDir()
	localPackage(t, configDir, "acme-net", "acme", `case ":$PATH:" in
*:`+TailscaleAppDir+`:) echo 100.64.0.7 ;;
*) echo "$PATH" >&2; exit 1 ;;
esac
`)
	p := onlyProvider(t, configDir)

	if _, err := p.Resolve(context.Background(), "main", nil); err != nil {
		t.Fatalf("the app's directory is not last on the path: %v", err)
	}
}

func TestResolveOnLinuxKeepsThePathAsItIs(t *testing.T) {
	was := goos
	goos = "linux"
	t.Cleanup(func() { goos = was })
	configDir := t.TempDir()
	localPackage(t, configDir, "acme-net", "acme", `[ "$PATH" = "$EXPECTED_PATH" ] || { echo "$PATH" >&2; exit 1; }
echo 100.64.0.7
`)
	t.Setenv("EXPECTED_PATH", os.Getenv("PATH"))
	p := onlyProvider(t, configDir)

	if _, err := p.Resolve(context.Background(), "main", nil); err != nil {
		t.Fatal(err)
	}
}
