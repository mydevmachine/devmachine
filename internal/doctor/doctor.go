// Package doctor answers one question: can this CLI do its job right now, and
// if not, which step is broken.
package doctor

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/mydevmachine/devmachine/internal/aliases"
	"github.com/mydevmachine/devmachine/internal/config"
	"github.com/mydevmachine/devmachine/internal/credentials"
	"github.com/mydevmachine/devmachine/internal/dns"
	"github.com/mydevmachine/devmachine/internal/facts"
	"github.com/mydevmachine/devmachine/internal/hostkeys"
	"github.com/mydevmachine/devmachine/internal/packages"
	"github.com/mydevmachine/devmachine/internal/provision"
	"github.com/mydevmachine/devmachine/internal/remote"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// The status of one check.
const (
	StatusPass = "pass"
	StatusFail = "fail"
	// StatusSkip means the check did not run: an earlier one failed, or there
	// is nothing for it to judge. It is not a pass and not a failure:
	// reporting it as either would lie.
	StatusSkip = "skip"
	// StatusWarn means something is worth fixing but nothing is broken: the
	// machine still works the way every check after it proves.
	StatusWarn = "warn"
)

// The checks, in the order they run.
const (
	CheckConfiguration   = "configuration"
	CheckHostKey         = "host key"
	CheckConnection      = "connection"
	CheckOperatingSystem = "operating system"
	CheckAnsible         = "ansible"
	// CheckSSHAliases is skipped entirely for a self machine: there is no
	// address for the computer you are standing on, so no Host entry is
	// ever written for it.
	CheckSSHAliases = "ssh aliases"
	// CheckBundle is only reported for a self machine: whether the directory
	// the bundle is written to exists and can be written.
	CheckBundle = "bundle"
	// CheckDNS prefixes one check per installed DNS provider, named
	// "dns: <provider>".
	CheckDNS = "dns"
)

// credentialPrefix names the check for one credential, so `credential: gh` and
// `credential: alice/claude` read as what they are.
const credentialPrefix = "credential: "

// CredentialCheck is what the check for one credential is called.
func CredentialCheck(d credentials.Declared) string { return credentialPrefix + credentials.Key(d) }

// The commands the remote checks run. They are constants so a test can answer
// them without guessing at the wording.
const (
	// One reader for the system, because a second one drifts from the
	// first. remote owns it: that is where it is acted on, and where the
	// list of supported systems lives.
	unameCommand     = remote.UnameCommand
	osReleaseCommand = remote.OSReleaseCommand
	ansibleCommand   = "command -v ansible-playbook"
)

// Check is one question and its answer.
type Check struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

// Dialer opens a connection to a machine. It is a parameter so the tests do
// not need one.
type Dialer func(context.Context, config.Machine, string) (remote.Client, string, error)

// Scanner reads a machine's presented host key without authenticating.
type Scanner func(context.Context, config.Machine) (ssh.PublicKey, string, error)

var scanHostKey = remote.ScanHostKey

// Run checks one machine and returns the results in order. It never returns an
// error: a failed check is the result, not an exception.
//
// machine names which one to check. Empty means the only configured machine,
// and with several it is an error rather than a guess.
func Run(ctx context.Context, dir, machine string, dial Dialer, wanted []credentials.Declared) []Check {
	return RunWithScanner(ctx, dir, machine, dial, scanHostKey, wanted)
}

// RunWithScanner is Run with the unauthenticated host-key probe supplied by
// the caller. Commands use the same seam as setup and machines trust; tests
// can prove ordering without opening a network connection.
func RunWithScanner(ctx context.Context, dir, machine string, dial Dialer, scan Scanner, wanted []credentials.Declared) []Check {
	reported := order(wanted)

	cfg, err := loadAndValidate(dir)
	if err != nil {
		return append(
			[]Check{{Name: CheckConfiguration, Status: StatusFail, Detail: err.Error()}},
			skipRest(reported, CheckConfiguration, "the configuration is not usable")...,
		)
	}
	m, err := cfg.Machine(machine)
	if err != nil {
		return append(
			[]Check{{Name: CheckConfiguration, Status: StatusFail, Detail: err.Error()}},
			skipRest(reported, CheckConfiguration, "there is no machine to check")...,
		)
	}

	if m.Self {
		return append([]Check{{
			Name:   CheckConfiguration,
			Status: StatusPass,
			Detail: fmt.Sprintf("machine %q, your computer as a machine, %d workspace(s)", m.Name, len(cfg.WorkspacesOn(m.Name))),
		}}, selfChecks(ctx, dir, m, dial, wanted)...)
	}

	checks := []Check{{
		Name:   CheckConfiguration,
		Status: StatusPass,
		Detail: fmt.Sprintf("machine %q, %d address(es), admin %s, port %d, %d workspace(s)",
			m.Name, len(m.Hosts), m.User, m.Port, len(cfg.WorkspacesOn(m.Name))),
	}}
	trust := hostKeyCheck(ctx, m, scan)
	checks = append(checks, trust)
	if trust.Status != StatusPass {
		return append(checks, skipRest(reported, CheckHostKey, "the SSH host identity is not trusted")...)
	}

	client, address, err := dial(ctx, m, "")
	if err != nil {
		checks = append(checks, Check{Name: CheckConnection, Status: StatusFail, Detail: err.Error()})
		return append(checks, skipRest(reported, CheckConnection, "the machine is unreachable")...)
	}
	defer client.Close()

	checks = append(checks, Check{
		Name:   CheckConnection,
		Status: StatusPass,
		Detail: fmt.Sprintf("connected through %s", address),
	})
	checks = append(checks, remoteChecks(ctx, client, dir, cfg, m)...)
	facts.Record(ctx, dir, m.Name, client, time.Now())
	checks = append(checks, sshAliasesCheck(cfg))
	checks = append(checks, credentialChecks(ctx, client, wanted)...)
	return append(checks, dnsChecks(ctx, dir, m.Name, baseOf(m), client)...)
}

// aliasCLI is the devmachine `aliases --write` would name as the ProxyCommand
// right now. It is a seam, so a test does not depend on its PATH.
var aliasCLI = aliases.CLIPath

// sshResolve runs `ssh -G <alias>` and parses its output into lowercase
// key/value pairs. `ssh -G` only resolves the configuration; it never opens a
// connection, so this is safe to run whether or not the alias is any good.
//
// It is a seam: a test supplies a fake so the check never shells out to a
// real ssh binary or depends on any actual SSH configuration file.
var sshResolve sshResolver = func(alias string) (map[string]string, error) {
	out, err := exec.Command("ssh", "-G", alias).Output()
	if err != nil {
		return nil, err
	}
	return parseSSHDashG(string(out)), nil
}

type sshResolver func(alias string) (map[string]string, error)

// proxyCommandOf is the ProxyCommand `ssh -G` reports, empty when there is
// none: it prints "none" or leaves the line out, depending on the version.
func proxyCommandOf(resolved map[string]string) string {
	if got := strings.TrimSpace(resolved["proxycommand"]); got != "none" {
		return got
	}
	return ""
}

func parseSSHDashG(out string) map[string]string {
	fields := make(map[string]string)
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		key, value, found := strings.Cut(line, " ")
		if !found {
			continue
		}
		fields[strings.ToLower(key)] = value
	}
	return fields
}

// sshAliasesCheck reports whether every workspace alias resolves to what
// `devmachine aliases` would write for the configuration today — whatever
// file it actually lives in.
//
// It asks `ssh -G <alias>` rather than reading a fixed file, so the generated
// block can live in ~/.ssh/config or in a file pulled in with `Include`.
//
// With `ssh_aliases` off, devmachine does not keep the aliases, so it has no
// standard to judge them by: the person's own ~/.ssh/config is not wrong for
// differing from a block they never asked for.
//
// It is a warning, never a failure: a stale or missing alias means `ssh
// <workspace>-devmachine` does not work from another terminal, not that the
// machine itself is broken.
func sshAliasesCheck(cfg config.Config) Check {
	if !cfg.SSHAliases {
		return Check{Name: CheckSSHAliases, Status: StatusSkip, Detail: "managed outside devmachine (ssh_aliases: false)"}
	}
	found, err := aliases.List(cfg, aliases.Options{CLI: aliasCLI()})
	if err != nil {
		return Check{Name: CheckSSHAliases, Status: StatusWarn, Detail: err.Error()}
	}
	if len(found) == 0 {
		return Check{Name: CheckSSHAliases, Status: StatusPass, Detail: "no workspace configured"}
	}

	var problems []string
	for _, a := range found {
		if msg := aliasMismatch(a); msg != "" {
			problems = append(problems, msg)
		}
	}
	if len(problems) == 0 {
		return Check{Name: CheckSSHAliases, Status: StatusPass}
	}
	return Check{Name: CheckSSHAliases, Status: StatusWarn,
		Detail: strings.Join(problems, "; ") + "; run `devmachine aliases --write`, or fix the file it is included from"}
}

// aliasMismatch reports what is wrong with one alias's resolved SSH
// configuration, or "" when it resolves exactly as `devmachine aliases`
// would write it.
func aliasMismatch(a aliases.Alias) string {
	resolved, err := sshResolve(a.Name)
	if err != nil {
		return fmt.Sprintf("%s does not resolve (%v)", a.Name, err)
	}

	var mismatches []string
	if got := resolved["hostname"]; got != a.Host {
		mismatches = append(mismatches, fmt.Sprintf("hostname %s, expected %s", got, a.Host))
	}
	if got := resolved["user"]; got != a.User {
		mismatches = append(mismatches, fmt.Sprintf("user %s, expected %s", got, a.User))
	}
	if got := resolved["port"]; got != strconv.Itoa(a.Port) {
		mismatches = append(mismatches, fmt.Sprintf("port %s, expected %d", got, a.Port))
	}
	if got := resolved["hostkeyalias"]; got != a.HostKeyAlias {
		mismatches = append(mismatches, fmt.Sprintf("hostkeyalias %s, expected %s", got, a.HostKeyAlias))
	}
	if got := proxyCommandOf(resolved); got != a.ProxyCommand {
		switch {
		case a.ProxyCommand == "":
			mismatches = append(mismatches, fmt.Sprintf("proxycommand %s, expected none", got))
		case got == "":
			mismatches = append(mismatches, "a fixed address, expected one resolved when ssh connects")
		default:
			mismatches = append(mismatches, fmt.Sprintf("proxycommand %s, expected %s", got, a.ProxyCommand))
		}
	}
	if len(mismatches) == 0 {
		return ""
	}
	return fmt.Sprintf("%s: %s", a.Name, strings.Join(mismatches, ", "))
}

// selfChecks is doctor's whole surface for a self machine: no SSH checks —
// there is no address to check one against — only whether your computer as
// a machine is what a self machine is allowed to be.
func selfChecks(ctx context.Context, dir string, m config.Machine, dial Dialer, wanted []credentials.Declared) []Check {
	client, _, err := dial(ctx, m, "")
	if err != nil {
		checks := []Check{{Name: CheckOperatingSystem, Status: StatusFail, Detail: err.Error()}}
		return append(checks, skipRest(selfOrder(wanted), CheckOperatingSystem, "your computer could not be reached")...)
	}
	defer client.Close()

	osCheck := selfOSCheck(ctx, client)
	checks := []Check{osCheck}
	if osCheck.Status != StatusPass {
		return append(checks, skipRest(selfOrder(wanted), CheckOperatingSystem, "a self machine is converged on macOS only")...)
	}

	checks = append(checks, selfAnsibleCheck(ctx, client))
	if cfg, err := config.Load(dir); err == nil {
		checks = append(checks, prerequisiteChecks(ctx, client, dir, cfg, m)...)
	}
	facts.Record(ctx, dir, m.Name, client, time.Now())

	base := baseOf(m)
	if base == "" {
		checks = append(checks, Check{Name: CheckBundle, Status: StatusFail,
			Detail: "could not find the home directory to write the bundle into"})
	} else {
		checks = append(checks, selfBundleCheck(ctx, client, base))
	}

	checks = append(checks, credentialChecks(ctx, client, wanted)...)
	return append(checks, dnsChecks(ctx, dir, m.Name, base, client)...)
}

// baseOf is where the bundle lives on m, empty when it could not be
// resolved. Doctor reports that rather than failing outright: every other
// check still has something to say.
func baseOf(m config.Machine) string {
	base, err := provision.Base(m)
	if err != nil {
		return ""
	}
	return base
}

func selfOSCheck(ctx context.Context, client remote.Client) Check {
	out, err := client.Run(ctx, "uname -s")
	if err != nil {
		return Check{Name: CheckOperatingSystem, Status: StatusFail, Detail: err.Error()}
	}
	system := strings.TrimSpace(out)
	if system != remote.KernelDarwin {
		return Check{Name: CheckOperatingSystem, Status: StatusFail,
			Detail: fmt.Sprintf("a self machine is converged on macOS only; this is %s", system)}
	}
	version, err := client.Run(ctx, remote.MacVersionCommand)
	if err != nil {
		return Check{Name: CheckOperatingSystem, Status: StatusFail, Detail: err.Error()}
	}
	mac := remote.System{Kernel: system, Version: strings.TrimSpace(version)}
	return Check{Name: CheckOperatingSystem, Status: StatusPass, Detail: mac.String()}
}

func selfAnsibleCheck(ctx context.Context, client remote.Client) Check {
	path, err := client.Run(ctx, ansibleCommand)
	if err != nil {
		return Check{Name: CheckAnsible, Status: StatusFail,
			Detail: "ansible-playbook is not on your computer: run `devmachine setup --machine <name>`"}
	}
	return Check{Name: CheckAnsible, Status: StatusPass, Detail: strings.TrimSpace(path)}
}

func selfBundleCheck(ctx context.Context, client remote.Client, base string) Check {
	probe := fmt.Sprintf(`mkdir -p "%s" && test -w "%s"`, base, base)
	if _, err := client.Run(ctx, probe); err != nil {
		return Check{Name: CheckBundle, Status: StatusFail, Detail: err.Error()}
	}
	return Check{Name: CheckBundle, Status: StatusPass, Detail: base}
}

func loadAndValidate(dir string) (config.Config, error) {
	cfg, err := config.Load(dir)
	if err != nil {
		return cfg, err
	}
	return cfg, cfg.Validate()
}

// order is every check this run reports, in the order they run. skipRest walks
// it, so a check added here is skipped by every cascade without anybody
// remembering to — including one credential's, which only this run knows the
// name of.
func order(wanted []credentials.Declared) []string {
	out := []string{CheckConfiguration, CheckHostKey, CheckConnection, CheckOperatingSystem, CheckAnsible, CheckSSHAliases}
	for _, d := range wanted {
		out = append(out, CredentialCheck(d))
	}
	return out
}

// selfOrder is order for a self machine: no SSH checks, and CheckBundle
// instead of nothing.
func selfOrder(wanted []credentials.Declared) []string {
	out := []string{CheckOperatingSystem, CheckAnsible, CheckBundle}
	for _, d := range wanted {
		out = append(out, CredentialCheck(d))
	}
	return out
}

func hostKeyCheck(ctx context.Context, m config.Machine, scan Scanner) Check {
	fail := func(err error) Check {
		return Check{Name: CheckHostKey, Status: StatusFail, Detail: err.Error()}
	}
	trustCommand := fmt.Sprintf("devmachine machines trust %s", m.Name)
	store, err := hostkeys.Open(m.KnownHostsFile)
	if err != nil {
		return fail(err)
	}
	pinned, err := store.Key(m.Name, m.Port)
	if err != nil {
		var keyErr *knownhosts.KeyError
		if errors.As(err, &keyErr) && len(keyErr.Want) == 0 {
			return fail(fmt.Errorf("%w for machine %q: run `%s`", remote.ErrHostKeyUnknown, m.Name, trustCommand))
		}
		return fail(err)
	}
	presented, address, err := scan(ctx, m)
	if err != nil {
		return fail(err)
	}
	if err := store.Check(m.Name, m.Port, presented); err != nil {
		var keyErr *knownhosts.KeyError
		if errors.As(err, &keyErr) {
			return fail(fmt.Errorf("%w for machine %q at %s: expected %s, received %s; verify the address, then run `%s --replace` only for a deliberate rebuild",
				remote.ErrHostKeyChanged, m.Name, address, hostkeys.Fingerprint(pinned), hostkeys.Fingerprint(presented), trustCommand))
		}
		return fail(err)
	}
	return Check{Name: CheckHostKey, Status: StatusPass,
		Detail: fmt.Sprintf("%s at %s", hostkeys.Fingerprint(presented), address)}
}

// skipRest returns a skip for every check that comes after `done`.
//
// It takes the last check already reported rather than a fixed list: reporting
// a connection as failed and then skipping it as well says two things about
// one check, and the one a reader believes is whichever they see first.
func skipRest(order []string, done, reason string) []Check {
	var out []Check
	past := false
	for _, n := range order {
		if past {
			out = append(out, Check{Name: n, Status: StatusSkip, Detail: reason})
		}
		if n == done {
			past = true
		}
	}
	return out
}

func remoteChecks(ctx context.Context, client remote.Client, dir string, cfg config.Config, m config.Machine) []Check {
	var out []Check

	system, err := remote.DetectSystem(ctx, client)
	if err != nil {
		out = append(out, Check{Name: CheckOperatingSystem, Status: StatusFail, Detail: err.Error()})
	} else {
		out = append(out, Check{Name: CheckOperatingSystem, Status: StatusPass, Detail: system.String()})
	}
	if system.MacOS() {
		out = append(out, macAnsibleCheck(ctx, client, dir, m.Name))
		out = append(out, prerequisiteChecks(ctx, client, dir, cfg, m)...)
		return append(out, sshAccessChecks(ctx, client, cfg.WorkspacesOn(m.Name))...)
	}

	path, err := client.Run(ctx, ansibleCommand)
	if err != nil {
		out = append(out, Check{
			Name:   CheckAnsible,
			Status: StatusFail,
			Detail: "ansible-playbook is not on the machine; `devmachine setup` installs it",
		})
	} else {
		out = append(out, Check{Name: CheckAnsible, Status: StatusPass, Detail: strings.TrimSpace(path)})
	}
	return out
}

// credentialChecks reports one check per declared credential.
//
// A missing credential is a warning, never a failure: the machine still works,
// and only the tool that needs the login does not. A failure is kept for what
// makes the machine itself unusable.
//
// A machine whose packages declare nothing has nothing to report, and reports
// nothing: an empty list is not a failure.
func credentialChecks(ctx context.Context, client remote.Client, wanted []credentials.Declared) []Check {
	if len(wanted) == 0 {
		return nil
	}

	present, err := credentials.Present(ctx, client, wanted)
	if err != nil {
		var out []Check
		for _, d := range wanted {
			out = append(out, Check{Name: CredentialCheck(d), Status: StatusWarn, Detail: err.Error()})
		}
		return out
	}

	var out []Check
	for _, d := range wanted {
		check := Check{Name: CredentialCheck(d)}
		switch {
		case credentials.Place(d) == "":
			check.Status = StatusSkip
			check.Detail = fmt.Sprintf(
				"package %q did not say where it is kept, so there is nowhere to look", d.Package)
		case present[credentials.Key(d)]:
			check.Status = StatusPass
			check.Detail = credentials.Place(d)
		default:
			check.Status = StatusWarn
			check.Detail = credentialFix(d)
		}
		out = append(out, check)
	}
	return out
}

// credentialFix is the command that makes a missing credential arrive. A
// warning that does not say what to do next is half a report.
func credentialFix(d credentials.Declared) string {
	if d.Kind == packages.KindManual {
		return "missing: run `" + credentials.LoginCommand(d) + "`"
	}
	return fmt.Sprintf("missing: run `devmachine secrets set %s`, then `devmachine credentials push`", d.Name)
}

// dnsCheckName is what an installed DNS provider's check is called.
func dnsCheckName(provider string) string { return CheckDNS + ": " + provider }

// dnsChecks reports one check per installed DNS provider: whether it can
// still be asked, and the zones it holds when it can.
//
// A provider that cannot be asked is a warning: it is a credential problem,
// and only `expose` and `dns` need it.
//
// A machine with no DNS provider installed reports nothing here — not
// installing one is a choice, not a fault.
func dnsChecks(ctx context.Context, dir, machine, base string, client remote.Client) []Check {
	providers, err := dns.Installed(dir, machine, base, client)
	if err != nil {
		return []Check{{Name: CheckDNS, Status: StatusWarn, Detail: err.Error()}}
	}

	var out []Check
	for _, p := range providers {
		zones, err := p.Zones(ctx)
		if err != nil {
			out = append(out, Check{Name: dnsCheckName(p.Name()), Status: StatusWarn, Detail: dnsFix(err)})
			continue
		}
		out = append(out, Check{Name: dnsCheckName(p.Name()), Status: StatusPass, Detail: strings.Join(zones, ", ")})
	}
	return out
}

// dnsFix is what a DNS warning tells somebody to do about it. A stale
// token found while creating a subdomain is a token found too late; this is
// meant to be found here first.
func dnsFix(err error) string {
	return fmt.Sprintf("%v (run `devmachine secrets set` for its credential, then `devmachine credentials push`)", err)
}

// OK reports whether no check failed. A warning is OK: it is worth fixing,
// but the machine still works.
func OK(checks []Check) bool {
	for _, c := range checks {
		if c.Status == StatusFail {
			return false
		}
	}
	return true
}
