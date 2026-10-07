package remote

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"regexp"
	"strings"

	"github.com/mydevmachine/devmachine/internal/config"
	"github.com/mydevmachine/devmachine/internal/network"
)

// builtInPrefix is the one prefix the CLI still resolves by itself, for a
// packages release older than the `network:` block. It is used only when no
// package declares the prefix, and it goes away once no supported release
// needs it.
const builtInPrefix = "tailscale"

// Address is one address to try, and where it came from.
type Address struct {
	// Address is what is dialled: an IP address, or a DNS name written as
	// a literal host entry.
	Address string `json:"address"`
	// Source is the host entry as written in config.yml.
	Source string `json:"source"`
	// Package is the network package that resolved Source, empty for a
	// literal entry.
	Package string `json:"package"`
}

// Dropped is a host entry that gave no address, and why.
type Dropped struct {
	Source string `json:"source"`
	Reason string `json:"reason"`
}

// Resolution is every address of a machine in the order it is tried, and
// every entry that was skipped.
type Resolution struct {
	Machine   string    `json:"machine"`
	Addresses []Address `json:"addresses"`
	Dropped   []Dropped `json:"dropped"`
}

// Literal reports whether a host entry is an address ssh can dial as written,
// rather than a `<prefix>:<name>` entry a network package has to resolve.
func Literal(address string) bool {
	_, _, isNetwork := network.Split(address)
	return !isNetwork
}

// ResolveAll turns the configured hosts into addresses to try, in order.
//
// An entry that cannot be resolved is dropped rather than fatal: the entries
// after it are the fallbacks it exists for. That holds for a script that
// failed as much as for a network that is simply off, because either way
// the public address is still worth trying.
func ResolveAll(ctx context.Context, m config.Machine) Resolution {
	r := Resolution{Machine: m.Name, Addresses: []Address{}, Dropped: []Dropped{}}

	var (
		providers []network.Provider
		loadErr   error
		loaded    bool
	)
	for _, h := range m.Hosts {
		prefix, name, isNetwork := network.Split(h.Address)
		if !isNetwork {
			r.add(Address{Address: h.Address, Source: h.Address})
			continue
		}
		if !loaded {
			providers, loadErr = network.Load(m)
			loaded = true
		}

		provider, found := network.Pick(providers, prefix, m.Packages)
		switch {
		case found:
			ips, err := provider.Resolve(ctx, name, provider.Settings(m.Settings))
			if err != nil {
				r.drop(h.Address, err.Error())
				continue
			}
			for _, ip := range ips {
				r.add(Address{Address: ip, Source: h.Address, Package: provider.Package})
			}
		case prefix == builtInPrefix:
			ip, err := builtInTailscale(name)
			if err != nil {
				r.drop(h.Address, err.Error())
				continue
			}
			r.add(Address{Address: ip, Source: h.Address, Package: builtInPrefix})
		case loadErr != nil:
			r.drop(h.Address, fmt.Sprintf("reading the packages to find who resolves %q: %v", prefix, loadErr))
		default:
			r.drop(h.Address, fmt.Sprintf("no package declares the prefix %q: add the network package that does to this machine", prefix))
		}
	}
	return r
}

func (r *Resolution) add(a Address) {
	for _, existing := range r.Addresses {
		if existing.Address == a.Address {
			return
		}
	}
	r.Addresses = append(r.Addresses, a)
}

func (r *Resolution) drop(source, reason string) {
	r.Dropped = append(r.Dropped, Dropped{Source: source, Reason: reason})
}

// Resolve is ResolveAll as the list of addresses Dial tries. Only an empty
// result is an error, and it says what was dropped.
func Resolve(m config.Machine) ([]string, error) {
	r := ResolveAll(context.Background(), m)
	if len(r.Addresses) == 0 {
		if len(r.Dropped) > 0 {
			reasons := make([]string, 0, len(r.Dropped))
			for _, d := range r.Dropped {
				reasons = append(reasons, fmt.Sprintf("%s (%s)", d.Source, d.Reason))
			}
			return nil, fmt.Errorf("no address left to try: %s", strings.Join(reasons, "; "))
		}
		return nil, fmt.Errorf("machine %q has no address", m.Name)
	}
	out := make([]string, 0, len(r.Addresses))
	for _, a := range r.Addresses {
		out = append(out, a.Address)
	}
	return out, nil
}

// builtInTailscale is the resolver the CLI shipped before network packages,
// kept exactly as it was for a release whose tailscale package has no
// `network:` block.
func builtInTailscale(name string) (string, error) {
	binary, err := tailscaleBinary()
	if err != nil {
		return "", err
	}
	return tailscaleIP(binary, name)
}

// tailscaleBinary is the tailscale on PATH or, on a Mac that has only
// Tailscale's app, the CLI inside the app.
func tailscaleBinary() (string, error) {
	if binary, err := lookPath("tailscale"); err == nil {
		return binary, nil
	}
	if goos == "darwin" {
		if info, err := os.Stat(tailscaleAppCLI); err == nil && !info.IsDir() {
			return tailscaleAppCLI, nil
		}
	}
	return "", errors.New("tailscale is not installed")
}

// Proxy connects to the first target that accepts a TCP connection and pipes
// stdin to it and its answers to stdout, the way `nc` would, until the
// machine closes the connection. It returns the target it used.
//
// Only the connection is retried: once one target answered, the SSH
// handshake has started through it and cannot move to another.
func Proxy(ctx context.Context, targets []string, stdin io.Reader, stdout io.Writer) (string, error) {
	if len(targets) == 0 {
		return "", errors.New("no address to connect to")
	}
	var (
		conn     net.Conn
		used     string
		failures []string
	)
	for _, target := range targets {
		d := net.Dialer{Timeout: dialTimeout}
		c, err := d.DialContext(ctx, "tcp", target)
		if err == nil {
			conn, used = c, target
			break
		}
		failures = append(failures, fmt.Sprintf("%s (%v)", target, err))
	}
	if conn == nil {
		return "", fmt.Errorf("no address answered: %s", strings.Join(failures, "; "))
	}
	defer func() { _ = conn.Close() }()

	go func() {
		_, _ = io.Copy(conn, stdin)
		if tcp, ok := conn.(*net.TCPConn); ok {
			_ = tcp.CloseWrite()
		}
	}()
	if _, err := io.Copy(stdout, conn); err != nil {
		return used, fmt.Errorf("reading from %s: %w", used, err)
	}
	return used, nil
}

// Upstream is the address another machine proxies to when it serves a site
// for this one: an IP a network package resolved first, since that is a
// private network both machines can be on, then a literal IP, then a DNS
// name, which only works if the serving machine resolves it the same way. A
// loopback address is never it — it names the machine doing the proxying.
func Upstream(r Resolution) (string, error) {
	var literalIP, name string
	for _, a := range r.Addresses {
		address := strings.TrimSuffix(strings.TrimPrefix(a.Address, "["), "]")
		ip := net.ParseIP(address)
		switch {
		case isLoopback(address):
		case ip != nil && a.Package != "":
			return address, nil
		case ip != nil && literalIP == "":
			literalIP = address
		case ip == nil && name == "" && hostname.MatchString(address):
			name = address
		}
	}
	if literalIP != "" {
		return literalIP, nil
	}
	if name != "" {
		return name, nil
	}
	reasons := []string{"no address but a loopback one"}
	for _, d := range r.Dropped {
		reasons = append(reasons, fmt.Sprintf("%s (%s)", d.Source, d.Reason))
	}
	return "", fmt.Errorf("no address of %s another machine can reach: %s", r.Machine, strings.Join(reasons, "; "))
}

// hostname is a DNS name that is safe to write into a Caddy file and a shell
// line as it is; anything else in `hosts` is not offered as an upstream.
var hostname = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?(\.[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?)*$`)

func isLoopback(address string) bool {
	if address == "localhost" {
		return true
	}
	ip := net.ParseIP(address)
	return ip != nil && ip.IsLoopback()
}
