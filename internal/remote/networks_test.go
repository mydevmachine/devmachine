package remote

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mydevmachine/devmachine/internal/config"
	"github.com/mydevmachine/devmachine/internal/packages"
)

// networkPackage writes a local network package into a configuration
// directory, with a resolve script whose body is given.
func networkPackage(t *testing.T, configDir, name, prefix, body string) {
	t.Helper()
	dir := filepath.Join(packages.LocalDir(configDir), name)
	if err := os.MkdirAll(filepath.Join(dir, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := "format: 1\nname: " + name + "\nscope: machine\nsummary: A private network.\n" +
		"network:\n  prefix: " + prefix + "\n  resolve: bin/resolve\n"
	if err := os.WriteFile(filepath.Join(dir, "package.yml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bin", "resolve"), []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
}

func refuseBuiltInTailscale(t *testing.T) {
	t.Helper()
	lookPath = func(string) (string, error) {
		t.Fatal("the built-in tailscale resolver ran although a package declares the prefix")
		return "", nil
	}
	t.Cleanup(func() { lookPath = realLookPath })
}

func TestResolveAsksThePackageThatDeclaresThePrefix(t *testing.T) {
	configDir := t.TempDir()
	networkPackage(t, configDir, "acme-net", "acme", "echo 100.64.0.7\necho 100.64.0.8\n")
	m := config.Machine{Name: "main", ConfigDir: configDir, Hosts: []config.Host{
		{Address: "acme:main"}, {Address: "203.0.113.10"},
	}}

	got := ResolveAll(context.Background(), m)
	want := []Address{
		{Address: "100.64.0.7", Source: "acme:main", Package: "acme-net"},
		{Address: "100.64.0.8", Source: "acme:main", Package: "acme-net"},
		{Address: "203.0.113.10", Source: "203.0.113.10"},
	}
	if len(got.Addresses) != len(want) {
		t.Fatalf("got %#v", got)
	}
	for i := range want {
		if got.Addresses[i] != want[i] {
			t.Fatalf("address %d: got %#v, want %#v", i, got.Addresses[i], want[i])
		}
	}
	if len(got.Dropped) != 0 {
		t.Fatalf("dropped %#v", got.Dropped)
	}
}

func TestResolvePrefersAPackageOverTheBuiltInTailscale(t *testing.T) {
	refuseBuiltInTailscale(t)
	configDir := t.TempDir()
	networkPackage(t, configDir, "tailscale", "tailscale", "echo 100.64.0.5\n")
	m := config.Machine{Name: "main", ConfigDir: configDir, Hosts: []config.Host{{Address: "tailscale:main"}}}

	got, err := Resolve(m)
	if err != nil || len(got) != 1 || got[0] != "100.64.0.5" {
		t.Fatalf("got %#v, %v", got, err)
	}
}

func TestResolveUsesThePackageTheMachineInstalls(t *testing.T) {
	configDir := t.TempDir()
	networkPackage(t, configDir, "acme-a", "acme", "echo 100.64.0.1\n")
	networkPackage(t, configDir, "acme-b", "acme", "echo 100.64.0.2\n")
	m := config.Machine{Name: "main", ConfigDir: configDir, Packages: []string{"acme-b"},
		Hosts: []config.Host{{Address: "acme:main"}}}

	got := ResolveAll(context.Background(), m)
	if len(got.Addresses) != 1 || got.Addresses[0].Package != "acme-b" {
		t.Fatalf("got %#v", got)
	}
}

func TestResolveFallsBackToTheBuiltInTailscaleWhenNoPackageDeclaresIt(t *testing.T) {
	lookPath = func(string) (string, error) { return "/usr/bin/tailscale", nil }
	tailscaleIP = func(string, string) (string, error) { return "100.64.0.5", nil }
	t.Cleanup(func() { lookPath = realLookPath; tailscaleIP = realTailscaleIP })
	configDir := t.TempDir()
	networkPackage(t, configDir, "acme-net", "acme", "echo 100.64.0.7\n")
	m := config.Machine{Name: "main", ConfigDir: configDir, Hosts: []config.Host{{Address: "tailscale:main"}}}

	got := ResolveAll(context.Background(), m)
	if len(got.Addresses) != 1 || got.Addresses[0] != (Address{Address: "100.64.0.5", Source: "tailscale:main", Package: "tailscale"}) {
		t.Fatalf("got %#v", got)
	}
}

func TestResolveSkipsANetworkThatIsNotAvailableHere(t *testing.T) {
	configDir := t.TempDir()
	networkPackage(t, configDir, "acme-net", "acme", "echo 'acme is not running' >&2\nexit 3\n")
	m := config.Machine{Name: "main", ConfigDir: configDir, Hosts: []config.Host{
		{Address: "acme:main"}, {Address: "203.0.113.10"},
	}}

	got := ResolveAll(context.Background(), m)
	if len(got.Addresses) != 1 || got.Addresses[0].Address != "203.0.113.10" {
		t.Fatalf("got %#v", got)
	}
	if len(got.Dropped) != 1 || got.Dropped[0].Source != "acme:main" ||
		!strings.Contains(got.Dropped[0].Reason, "acme is not running") {
		t.Fatalf("dropped %#v", got.Dropped)
	}
}

func TestResolveDropsAnEntryWhoseScriptFailed(t *testing.T) {
	configDir := t.TempDir()
	networkPackage(t, configDir, "acme-net", "acme", "echo 'broken state' >&2\nexit 1\n")
	m := config.Machine{Name: "main", ConfigDir: configDir, Hosts: []config.Host{
		{Address: "acme:main"}, {Address: "203.0.113.10"},
	}}

	got := ResolveAll(context.Background(), m)
	if len(got.Addresses) != 1 || len(got.Dropped) != 1 ||
		!strings.Contains(got.Dropped[0].Reason, "broken state") {
		t.Fatalf("got %#v", got)
	}
}

func TestResolveDropsAPrefixNoPackageDeclares(t *testing.T) {
	m := config.Machine{Name: "main", ConfigDir: t.TempDir(), Hosts: []config.Host{
		{Address: "zerotier:main"}, {Address: "203.0.113.10"},
	}}

	got := ResolveAll(context.Background(), m)
	if len(got.Addresses) != 1 || len(got.Dropped) != 1 ||
		!strings.Contains(got.Dropped[0].Reason, `no package declares the prefix "zerotier"`) {
		t.Fatalf("got %#v", got)
	}
}

func TestResolveErrorNamesEveryDroppedEntry(t *testing.T) {
	configDir := t.TempDir()
	networkPackage(t, configDir, "acme-net", "acme", "exit 3\n")
	m := config.Machine{Name: "main", ConfigDir: configDir, Hosts: []config.Host{{Address: "acme:main"}}}

	_, err := Resolve(m)
	if err == nil || !strings.Contains(err.Error(), "acme:main") {
		t.Fatalf("got %v", err)
	}
}

func TestLiteralKnowsAnIPv6AddressIsNotAPrefix(t *testing.T) {
	if !Literal("2001:db8::1") || !Literal("vps.example.com") {
		t.Fatal("an IPv6 address and a DNS name are literal")
	}
	if Literal("acme:main") {
		t.Fatal("a network entry is not literal")
	}
}

// listener accepts one connection and echoes what it reads back, prefixed.
func echoListener(t *testing.T) (string, int) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	go func() {
		conn, err := l.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		body, _ := io.ReadAll(conn)
		_, _ = conn.Write(append([]byte("echo:"), body...))
	}()
	host, port, _ := net.SplitHostPort(l.Addr().String())
	n, _ := strconv.Atoi(port)
	return host, n
}

// closedPort is a port on this computer nothing listens on.
func closedPort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := l.Addr().String()
	_ = l.Close()
	return address
}

func TestProxyPipesThroughTheFirstAddressThatAnswers(t *testing.T) {
	host, port := echoListener(t)
	targets := []string{closedPort(t), net.JoinHostPort(host, strconv.Itoa(port))}
	var out bytes.Buffer

	used, err := Proxy(context.Background(), targets, strings.NewReader("SSH-2.0-test\n"), &out)
	if err != nil {
		t.Fatal(err)
	}
	if used != targets[1] {
		t.Fatalf("connected through %q", used)
	}
	if out.String() != "echo:SSH-2.0-test\n" {
		t.Fatalf("got %q", out.String())
	}
}

func TestProxyNamesEveryAddressWhenNoneAnswers(t *testing.T) {
	first, second := closedPort(t), closedPort(t)

	_, err := Proxy(context.Background(), []string{first, second}, strings.NewReader(""), io.Discard)
	if err == nil || !strings.Contains(err.Error(), first) || !strings.Contains(err.Error(), second) {
		t.Fatalf("got %v", err)
	}
}

func TestProxyWithNothingToTryIsAnError(t *testing.T) {
	_, err := Proxy(context.Background(), nil, strings.NewReader(""), io.Discard)
	if err == nil {
		t.Fatal("expected an error")
	}
}

func TestProxyReturnsWhenTheMachineClosesTheConnection(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	go func() {
		conn, err := l.Accept()
		if err == nil {
			_, _ = conn.Write([]byte("bye\n"))
			_ = conn.Close()
		}
	}()
	stdin, keepOpen := io.Pipe()
	defer keepOpen.Close()
	var out bytes.Buffer

	if _, err := Proxy(context.Background(), []string{l.Addr().String()}, stdin, &out); err != nil &&
		!errors.Is(err, io.EOF) {
		t.Fatal(err)
	}
	if out.String() != "bye\n" {
		t.Fatalf("got %q", out.String())
	}
}

func TestUpstreamPrefersAPrivateNetworkAndNeverLoopback(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   []Address
		want string
	}{
		{"private network first", []Address{
			{Address: "203.0.113.7", Source: "203.0.113.7"},
			{Address: "100.64.0.7", Source: "tailscale:lab", Package: "tailscale"},
		}, "100.64.0.7"},
		{"loopback skipped", []Address{
			{Address: "127.0.0.1", Source: "127.0.0.1"},
			{Address: "localhost", Source: "localhost"},
			{Address: "::1", Source: "::1"},
			{Address: "192.0.2.4", Source: "192.0.2.4"},
		}, "192.0.2.4"},
	} {
		got, err := Upstream(Resolution{Machine: "lab", Addresses: tc.in})
		if err != nil || got != tc.want {
			t.Fatalf("%s: got %q, %v", tc.name, got, err)
		}
	}
}

func TestUpstreamSaysWhyThereIsNone(t *testing.T) {
	_, err := Upstream(Resolution{
		Machine:   "lab",
		Addresses: []Address{{Address: "127.0.0.1", Source: "127.0.0.1"}},
		Dropped:   []Dropped{{Source: "tailscale:lab", Reason: "tailscale is not running"}},
	})
	if err == nil || !strings.Contains(err.Error(), "lab") || !strings.Contains(err.Error(), "tailscale is not running") {
		t.Fatalf("got %v", err)
	}
}

func TestUpstreamTakesOnlyAnAddressCaddyAndAShellCanUse(t *testing.T) {
	for _, tc := range []struct {
		in   []Address
		want string
	}{
		{[]Address{{Address: "[2001:db8::1]", Source: "[2001:db8::1]"}}, "2001:db8::1"},
		{[]Address{{Address: "lab.example.com", Source: "lab.example.com"}, {Address: "192.0.2.4", Source: "192.0.2.4"}}, "192.0.2.4"},
		{[]Address{{Address: "lab.example.com", Source: "lab.example.com"}}, "lab.example.com"},
		{[]Address{{Address: "lab'; rm -rf /", Source: "x"}, {Address: "192.0.2.4", Source: "192.0.2.4"}}, "192.0.2.4"},
	} {
		got, err := Upstream(Resolution{Machine: "lab", Addresses: tc.in})
		if err != nil || got != tc.want {
			t.Fatalf("%+v: got %q, %v", tc.in, got, err)
		}
	}
}

func TestTheBuiltInTailscaleOnAMacFallsBackToTheApp(t *testing.T) {
	app := filepath.Join(t.TempDir(), "Tailscale")
	if err := os.WriteFile(app, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	var used string
	lookPath = func(string) (string, error) { return "", errors.New("not found") }
	tailscaleIP = func(binary, _ string) (string, error) { used = binary; return "100.64.0.5", nil }
	wasGOOS, wasApp := goos, tailscaleAppCLI
	goos, tailscaleAppCLI = "darwin", app
	t.Cleanup(func() {
		lookPath, tailscaleIP, goos, tailscaleAppCLI = realLookPath, realTailscaleIP, wasGOOS, wasApp
	})

	got, err := builtInTailscale("main")
	if err != nil || got != "100.64.0.5" || used != app {
		t.Fatalf("got %q %v, ran %q", got, err, used)
	}
}

func TestTheBuiltInTailscaleOnLinuxDoesNotLookForTheApp(t *testing.T) {
	app := filepath.Join(t.TempDir(), "Tailscale")
	if err := os.WriteFile(app, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	lookPath = func(string) (string, error) { return "", errors.New("not found") }
	wasGOOS, wasApp := goos, tailscaleAppCLI
	goos, tailscaleAppCLI = "linux", app
	t.Cleanup(func() { lookPath, goos, tailscaleAppCLI = realLookPath, wasGOOS, wasApp })

	if _, err := builtInTailscale("main"); err == nil || err.Error() != "tailscale is not installed" {
		t.Fatalf("got %v", err)
	}
}
