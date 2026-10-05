package commands

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/mydevmachine/devmachine/internal/aliases"
	"github.com/mydevmachine/devmachine/internal/config"
	"github.com/mydevmachine/devmachine/internal/hostkeys"
	"github.com/mydevmachine/devmachine/internal/keys"
	"github.com/mydevmachine/devmachine/internal/packages"
	"github.com/mydevmachine/devmachine/internal/remote"
	agentskills "github.com/mydevmachine/devmachine/internal/skills"
	"github.com/spf13/cobra"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
	"golang.org/x/term"
	"gopkg.in/yaml.v3"
)

// The bootstrap's steps, as seams, so every branch of the flow can be driven
// without a machine to reach.
var (
	dialWith       = remote.DialWith
	installKey     = remote.InstallKey
	proveAuth      = remote.ProveAuth
	harden         = remote.Harden
	installAnsible = remote.InstallAnsible
	checkRoot      = remote.CheckRoot
	detectSystem   = remote.DetectSystem
	tailscaleSSH   = remote.IsTailscaleSSH
	agentKeys      = keys.FromAgent
	scanHostKey    = remote.ScanHostKey
	confirmHostKey = confirm
	setupSkills    = offerSetupSkills
	streamLocal    = streamLocalCommand
)

// brewInstallCommand is the official one-line Homebrew install. `setup`
// prints it and runs nothing: that command asks for sudo, and asking for
// sudo on the operator's own computer is not this CLI's to do.
const brewInstallCommand = `/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"`

// streamLocalCommand runs a command on your computer with its output
// streamed to out as it arrives, the same way installing Ansible on a remote
// machine streams over SSH.
func streamLocalCommand(ctx context.Context, out io.Writer, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout, cmd.Stderr = out, out
	return cmd.Run()
}

// setupOptions are the flags the flow reads.
type setupOptions struct {
	force        bool
	noHarden     bool
	noEssentials bool
	noAliases    bool
	yes          bool
	machine      string
	// The answers `machines add` takes as flags. An address makes the whole
	// run unattended: nothing is asked, and what has no flag takes its
	// default.
	name        string
	address     string
	user        string
	port        int
	key         string
	fingerprint string
	tailscale   bool
	// location is where the machine is. Empty means the default: external
	// for a server, local for your own computer.
	location string
	// passwordStdin reads the admin password from stdin, for a server that
	// takes nothing else yet. It is used once, to install the key.
	passwordStdin bool
	// domain is written only into a new configuration, the one `machines
	// add` writes when there is no config.yml yet.
	domain string
	// hostKey is a host key already read from the machine and trusted as it
	// is, for a VM `create-local --add` made a moment ago. It has no flag.
	hostKey ssh.PublicKey
}

// unattended is a `machines add` that asks nothing, because its answers came
// as flags.
func (o setupOptions) unattended() bool { return o.address != "" }

// tailscalePackage is the recipe `setup` and `machines add` offer to add when
// the person wants the machine reachable over Tailscale too.
const tailscalePackage = "tailscale"

// essentials is the package a new machine starts with: base tools, git, a
// firewall, SSH hardening, Caddy and what the macOS app reads. Nearly everyone
// wants them, so the choice is to leave them out, not to remember to add them.
const essentials = "essentials"

// releaseHasPackage answers whether a package release carries a package. A
// machine only gets essentials from a release that has it: an older pin would
// make the first sync fail on a name it has never heard of.
var releaseHasPackage = func(ctx context.Context, dir, release, name string) bool {
	store, err := packages.Open(ctx, dir, release)
	if err != nil {
		return false
	}
	return store.Has(name)
}

// startingPackages is what a new machine is written with.
func startingPackages(ctx context.Context, dir, release string, noEssentials bool, out io.Writer) []string {
	if noEssentials || release == "" {
		return nil
	}
	if !releaseHasPackage(ctx, dir, release, essentials) {
		fmt.Fprintf(out, "packages %s has no %s package, so the machine starts with none.\n", release, essentials)
		return nil
	}
	return []string{essentials}
}

func newSetupCmd(opts *options) *cobra.Command {
	var s setupOptions

	c := &cobra.Command{
		Use:   "setup",
		Short: "Connect a new server, or prepare the configured one",
		Long: "With no configuration, asks for your server's address and how to log in, " +
			"then gets it ready: it sets up a key, checks the key works, turns off password " +
			"logins and installs Ansible. With a configuration, it only makes sure Ansible " +
			"is installed; --force starts over.\n\n" +
			"It never asks which situation you are in. A key that already works " +
			"is found by trying it, and the root password is asked for only when " +
			"that fails — many servers arrive with a key already pasted in, and " +
			"their owner has no password to give.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, _, err := config.Dir(opts.configDir)
			if err != nil {
				return err
			}
			s.machine = opts.machine
			return runSetup(cmd.Context(), dir, cmd.InOrStdin(), cmd.OutOrStdout(), s)
		},
	}
	c.Flags().BoolVar(&s.force, "force", false, "overwrite a configuration that already exists")
	c.Flags().BoolVar(&s.noHarden, "no-harden", false,
		"during a new takeover, leave password login on (the key is still installed and proved)")
	c.Flags().BoolVar(&s.noEssentials, "no-essentials", false,
		"start the machine with no packages, instead of the essentials")
	c.Flags().BoolVar(&s.noAliases, "no-aliases", false,
		"do not ask about SSH host entries, and do not write them")
	c.Flags().BoolVar(&s.yes, "yes", false, "answer yes to writing SSH host entries, without asking")
	c.AddCommand(newSetupGitCmd(opts))
	return c
}

// runSetup asks the questions, writes config.yml, and takes the machine over.
//
// It reads and writes through the streams it is given, so the whole flow is
// testable without a terminal.
func runSetup(ctx context.Context, dir string, in io.Reader, out io.Writer, opts setupOptions) error {
	path := filepath.Join(dir, config.FileName)
	if _, err := os.Stat(path); err == nil && !opts.force {
		if err := prepareExisting(ctx, dir, out, opts.machine); err != nil {
			return err
		}
		if err := finishSetup(ctx, dir, in, out); err != nil {
			return err
		}
		fmt.Fprintf(out, "\nNext: `devmachine doctor`, then `devmachine sync`.\n")
		return nil
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("checking %s: %w", path, err)
	}

	r := bufio.NewReader(in)

	m, err := askForMachine(r, out, "main")
	if err != nil {
		return err
	}
	domain, err := ask(r, out, "domain (leave empty for none)", "")
	if err != nil {
		return err
	}

	cfg := config.Config{Machines: []config.Machine{m}, Domain: domain}
	if err := cfg.Validate(); err != nil {
		return err
	}
	m.KnownHostsFile = filepath.Join(dir, config.KnownHostsFileName)
	if err := trustFirstContact(ctx, r, out, m); err != nil {
		return err
	}

	key, err := askForKey(r, out, dir, m.Name)
	if err != nil {
		return err
	}
	if err := recordKey(dir, &m, key); err != nil {
		return err
	}

	release := pinForNewConfig(ctx, out)
	m.Packages = startingPackages(ctx, dir, release, opts.noEssentials, out)
	if err := writeNewConfig(dir, m, release, domain, false); err != nil {
		return err
	}
	fmt.Fprintf(out, "\nwrote %s\n\n", path)

	if err := bootstrap(ctx, out, m, key, opts.noHarden, askingPassword(r, in, out)); err != nil {
		return err
	}
	if err := offerSSHAliases(r, out, dir, opts.noAliases, opts.yes); err != nil {
		return err
	}
	if err := offerTailscale(r, out, dir, m.Name); err != nil {
		return err
	}
	if err := finishSetup(ctx, dir, in, out); err != nil {
		return err
	}

	sshAliases, err := currentSSHAliases(dir)
	if err != nil {
		return err
	}
	fmt.Fprint(out, nextAfterSetup(m.Packages, sshAliases))
	return nil
}

// nextAfterSetup is printed once the machine is reachable.
func nextAfterSetup(machinePackages []string, sshAliases bool) string {
	note := "The machine starts with no packages: `devmachine packages add essentials` gives it base tools, git, a firewall, Caddy and what the macOS app reads.\n"
	if slices.Contains(machinePackages, essentials) {
		note = "The machine starts with the essentials: base tools, git, a firewall, Caddy and what the macOS app reads.\n" +
			"To start bare instead, remove `essentials` from config.yml, or run setup with --no-essentials.\n"
	}
	reach := "Once a workspace exists: `devmachine ssh <workspace>` (or `mosh`) reaches it from here.\n"
	if sshAliases {
		reach += "With SSH aliases on: `ssh <workspace>-devmachine` reaches it from any terminal or editor " +
			"(VS Code Remote-SSH, Zed, the macOS app).\n"
	}
	return "\n" + note + "AGENTS.md tells coding agents how to work in this folder.\n" + `
Next:
  devmachine workspaces new acme   your first workspace (any name)
  devmachine sync                  build it all on the server
` + "\n" + reach
}

// offerSSHAliases asks once whether the CLI should keep ~/.ssh/config's
// managed block up to date on its own, and does the first write when the
// answer is yes.
//
// It only runs during a new setup or `machines add`: those are the two
// moments a machine goes from unreachable to reachable, and "ssh
// acme-devmachine does not work" is the exact complaint this closes.
func offerSSHAliases(r *bufio.Reader, out io.Writer, dir string, noAliases, yes bool) error {
	if noAliases {
		return nil
	}
	ok := yes
	if !yes {
		var err error
		ok, err = confirmDefaultYes(r, out,
			"Write SSH host entries to ~/.ssh/config so `ssh <workspace>-devmachine` and `mosh` work from any terminal?")
		if err != nil {
			return err
		}
	}
	if !ok {
		return nil
	}
	if err := config.SetSSHAliases(dir, true); err != nil {
		return err
	}
	cfg, err := config.Load(dir)
	if err != nil {
		return err
	}
	path, err := aliases.PathFor(cfg)
	if err != nil {
		return err
	}
	block, err := aliases.Render(cfg, aliases.Options{CLI: aliasCLI()})
	if err != nil {
		return err
	}
	if err := aliases.Write(path, block); err != nil {
		return err
	}
	fmt.Fprintf(out, "wrote the SSH aliases into %s; they will stay up to date automatically from now on.\n", path)
	return nil
}

// offerTailscale asks whether the machine should also be reachable over
// Tailscale, and adds the package when the answer is yes.
//
// It only adds the package: the private address itself is recorded by
// `devmachine login tailscale`, once the machine has actually signed in and
// can say what it is called on the tailnet.
func offerTailscale(r *bufio.Reader, out io.Writer, dir, machine string) error {
	ok, err := confirm(r, out,
		"Reach this machine over Tailscale too (a private address that keeps working when the public one does not)?")
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	return addTailscale(out, dir, machine)
}

// addTailscale puts the tailscale package on a machine, once.
func addTailscale(out io.Writer, dir, machine string) error {
	cfg, err := config.Load(dir)
	if err != nil {
		return err
	}
	for i := range cfg.Machines {
		if cfg.Machines[i].Name != machine {
			continue
		}
		if slices.Contains(cfg.Machines[i].Packages, tailscalePackage) {
			return nil
		}
		cfg.Machines[i].Packages = append(cfg.Machines[i].Packages, tailscalePackage)
		if err := config.Save(dir, cfg); err != nil {
			return err
		}
		fmt.Fprintf(out, "added the %s package to %s.\n", tailscalePackage, machine)
		fmt.Fprintln(out, "Next: `devmachine sync`, then `devmachine login tailscale` to sign in and "+
			"add its private address; the public address stays as a fallback.")
		return nil
	}
	return nil
}

// currentSSHAliases reads the operator's recorded answer back, so the closing
// message can say whether an alias also reaches a workspace.
func currentSSHAliases(dir string) (bool, error) {
	cfg, err := config.Load(dir)
	if err != nil {
		return false, err
	}
	return cfg.SSHAliases, nil
}

// prepareExisting resumes setup without taking ownership a second time. The
// configuration already says which key and host identity to trust, so this
// path only connects with those choices and installs the last prerequisite.
// In particular, it must not rewrite configuration, install a key, or change
// SSH policy on a machine the CLI already owns.
func prepareExisting(ctx context.Context, dir string, out io.Writer, name string) error {
	cfg, err := config.Load(dir)
	if err != nil {
		return err
	}
	if err := cfg.Validate(); err != nil {
		return err
	}
	m, err := cfg.Machine(name)
	if err != nil {
		return err
	}

	fmt.Fprintf(out, "%s already describes %s; preparing it without rewriting configuration.\n", config.FileName, m.Name)

	if m.Self {
		return prepareExistingSelf(ctx, out, m)
	}

	client, address, err := dial(ctx, m, m.User)
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()

	if err := refuseUnknownSystem(ctx, client, out, address); err != nil {
		return err
	}
	if tailscaleSSH(client) {
		if err := installKeyOverTailscaleSSH(ctx, client, out, m, address); err != nil {
			return err
		}
	}

	fmt.Fprintf(out, "connected as %s@%s; installing Ansible if needed...\n", m.User, address)
	if err := checkRoot(ctx, client, m.User); err != nil {
		return err
	}
	if err := installAnsible(ctx, client, out); err != nil {
		return err
	}
	return nil
}

// refuseUnknownSystem stops on a machine this CLI does not set up, while
// nothing on it has been changed yet.
func refuseUnknownSystem(ctx context.Context, client remote.Client, out io.Writer, address string) error {
	system, err := detectSystem(ctx, client)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "%s runs %s.\n", address, system)
	return nil
}

// installKeyOverTailscaleSSH makes sure the machine's key is in
// authorized_keys when the connection came in through Tailscale SSH, which
// accepts any key and so never proved this one. A first run over Tailscale SSH
// by an older CLI took that login as proof and installed nothing; running
// setup again is how such a machine is repaired.
func installKeyOverTailscaleSSH(ctx context.Context, client remote.Client, out io.Writer,
	m config.Machine, address string) error {
	public, err := machinePublicKey(m)
	if err != nil {
		return err
	}
	if public == "" {
		return nil
	}
	fmt.Fprintf(out, "%s answered through Tailscale SSH, which does not check keys; "+
		"making sure the machine's key is installed for when it is reached without it.\n", address)
	return installKey(ctx, client, public)
}

// machinePublicKey is the public half of the key a machine logs in with, or
// empty when the agent offers whatever it holds and there is no one key.
func machinePublicKey(m config.Machine) (string, error) {
	switch {
	case m.Key != "":
		return keys.PublicFor(m.Key)
	case m.AgentKey != "":
		return m.AgentKey, nil
	}
	return "", nil
}

// prepareExistingSelf is `setup`'s whole job for a self machine: no host key
// to trust, no key to install, no password, no hardening. It only makes sure
// Homebrew and Ansible are on your computer.
func prepareExistingSelf(ctx context.Context, out io.Writer, m config.Machine) error {
	if _, err := lookPath("brew"); err != nil {
		return fmt.Errorf(
			"homebrew is not installed on your computer: run this, then `devmachine setup --machine %s` again:\n%s",
			m.Name, brewInstallCommand)
	}
	if _, err := lookPath("ansible-playbook"); err == nil {
		fmt.Fprintf(out, "%s is already prepared: Homebrew and ansible-playbook are both on PATH.\n", m.Name)
		return nil
	}

	fmt.Fprintf(out, "installing Ansible with Homebrew...\n")
	if err := streamLocal(ctx, out, "brew", "install", "ansible"); err != nil {
		return fmt.Errorf("installing Ansible with Homebrew: %w", err)
	}
	fmt.Fprintf(out, "Ansible is installed.\n")
	return nil
}

func finishSetup(ctx context.Context, dir string, in io.Reader, out io.Writer) error {
	cfg, err := config.Load(dir)
	if err != nil {
		return err
	}
	if cfg.Packages == "" {
		fmt.Fprintln(out, "\nAgent skills: no package release is pinned. Run `devmachine packages pin`, then `devmachine skills add`.")
		return nil
	}
	return setupSkills(ctx, dir, in, out)
}

func offerSetupSkills(ctx context.Context, dir string, in io.Reader, out io.Writer) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	agents := detectAgents(home)
	if len(agents) == 0 {
		fmt.Fprintln(out, "\nAgent skills were not installed because no supported harness was detected. Run `devmachine skills add --agent <name>` later.")
		return nil
	}
	ok, err := confirm(in, out, "Install the official Devmachine skills for "+joinAgents(agents)+"?")
	if err != nil {
		return err
	}
	if !ok {
		fmt.Fprintln(out, "Agent skills were not installed. Run `devmachine skills add` later.")
		return nil
	}
	source, err := skillSource(ctx, dir, "")
	if err != nil {
		return err
	}
	result, err := (agentskills.Installer{Home: home}).Install(source, agents)
	if err != nil {
		return err
	}
	origin := result.Origin
	if origin == "" {
		origin = result.Source
	}
	fmt.Fprintf(out, "installed %s from %s (%d filesystem changes)\n", strings.Join(result.Skills, ", "), origin, result.Changed)
	return nil
}

// trustFirstContact records the host identity before setup offers any
// authentication method or changes the remote machine.
func trustFirstContact(ctx context.Context, reader io.Reader, out io.Writer, machine config.Machine) error {
	return trustFirstContactWith(ctx, out, machine, func(fingerprint string) (bool, error) {
		question := fmt.Sprintf("Trust this %s fingerprint for %s? This is trust on first use; compare it through the provider console or another trusted channel first.", fingerprint, machine.Name)
		return confirmHostKey(reader, out, question)
	})
}

// trustExpected trusts a first-contact host key only when it is the one the
// person said to expect. With nobody to ask, trusting whatever answered would
// be trust on first use with no one looking.
func trustExpected(ctx context.Context, out io.Writer, machine config.Machine, expected string) error {
	return trustFirstContactWith(ctx, out, machine, func(fingerprint string) (bool, error) {
		if expected == fingerprint {
			return true, nil
		}
		if expected == "" {
			return false, fmt.Errorf("%s presented %s, and no --fingerprint was given to check it against: "+
				"compare it through the provider console or a connection you already trust, then pass "+
				"--fingerprint %s", machine.Name, fingerprint, fingerprint)
		}
		return false, fmt.Errorf("%s presented %s, not the %s that --fingerprint expects: nothing was "+
			"trusted or changed. Check which one is right before trying again", machine.Name, fingerprint, expected)
	})
}

func trustFirstContactWith(ctx context.Context, out io.Writer, machine config.Machine,
	approve func(fingerprint string) (bool, error)) error {
	presented, address, err := scanHostKey(ctx, machine)
	if err != nil {
		return err
	}
	return trustPresented(out, machine, presented, address, approve)
}

// trustPresented records a host key already read from the machine, once
// approve says yes, or checks it against the one already trusted.
func trustPresented(out io.Writer, machine config.Machine, presented ssh.PublicKey, address string,
	approve func(fingerprint string) (bool, error)) error {
	fingerprint := hostkeys.Fingerprint(presented)
	fmt.Fprintf(out, "\n%s at %s presented %s host key %s.\n", machine.Name, address, presented.Type(), fingerprint)

	store, err := hostkeys.Open(machine.KnownHostsFile)
	if err != nil {
		return err
	}
	current, err := store.Key(machine.Name, machine.Port)
	if err == nil {
		if bytes.Equal(current.Marshal(), presented.Marshal()) {
			fmt.Fprintf(out, "the presented host key matches the trusted key in %s\n", machine.KnownHostsFile)
			return nil
		}
		return fmt.Errorf("%w for machine %q at %s: expected %s, received %s; use `devmachine machines trust %s --replace` only after verifying a deliberate rebuild",
			remote.ErrHostKeyChanged, machine.Name, address, hostkeys.Fingerprint(current), fingerprint, machine.Name)
	}
	var keyErr *knownhosts.KeyError
	if !errors.As(err, &keyErr) || len(keyErr.Want) != 0 {
		return err
	}

	ok, err := approve(fingerprint)
	if err != nil {
		return err
	}
	if !ok {
		return errDeclined
	}
	if err := os.MkdirAll(filepath.Dir(machine.KnownHostsFile), 0o700); err != nil {
		return fmt.Errorf("creating the configuration directory for host trust: %w", err)
	}
	if err := store.Put(machine.Name, machine.Port, presented); err != nil {
		return err
	}
	fmt.Fprintf(out, "trusted %s for %s\n", fingerprint, machine.Name)
	return nil
}

// askForMachine asks where the machine is and who to log in as. The domain is
// not asked for here: it belongs to the configuration, not to a machine, so
// only setup has a reason to ask.
func askForMachine(r *bufio.Reader, out io.Writer, defaultName string) (config.Machine, error) {
	var m config.Machine

	name, err := ask(r, out, "machine name", defaultName)
	if err != nil {
		return m, err
	}
	address, err := ask(r, out, "address (an IP, a hostname, or tailscale:<name>)", "")
	if err != nil {
		return m, err
	}
	if address == "" {
		return m, errors.New("an address is required: the CLI has nothing to reach without one")
	}
	admin, err := ask(r, out, "administrative login", config.DefaultAdminUser)
	if err != nil {
		return m, err
	}
	portText, err := ask(r, out, "SSH port", strconv.Itoa(config.DefaultPort))
	if err != nil {
		return m, err
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		return m, fmt.Errorf("%q is not a port number", portText)
	}

	return config.Machine{
		Name:  name,
		Hosts: []config.Host{{Address: address}},
		User:  admin,
		Port:  port,
	}, nil
}

// chosenKey is how the CLI will log in from now on.
type chosenKey struct {
	// Path is the private key on disk. It is empty when an SSH agent holds
	// the key and nothing on disk does.
	Path string
	// Public is the authorized_keys line, which is what gets installed.
	Public string
}

func (k chosenKey) auth() remote.Auth {
	if k.Path == "" {
		return remote.Auth{Agent: true, AgentPublicKey: k.Public}
	}
	return remote.Auth{KeyPath: k.Path}
}

func (k chosenKey) describe() string {
	if k.Path == "" {
		return "the key from the SSH agent"
	}
	return "the key " + k.Path
}

// recordKey puts the chosen key on the machine the way its origin needs: a
// path when it is a file, or the public half plus the file that lets the
// system ssh binary find it in the agent when it is not.
func recordKey(dir string, m *config.Machine, key chosenKey) error {
	if key.Path != "" {
		m.Key = key.Path
		return nil
	}
	if key.Public == "" {
		// No key was chosen (no agent, no file, nothing generated): keep the
		// pre-existing behaviour of asking the agent for everything it holds.
		return nil
	}
	if _, err := keys.WriteAgent(dir, m.Name, key.Public); err != nil {
		return err
	}
	m.AgentKey = key.Public
	return nil
}

// agentKeyPrefix is how --key names a key the SSH agent holds, by its SHA256
// fingerprint: `agent:SHA256:…`.
const agentKeyPrefix = "agent:"

// keyFromFlag is askForKey's answer for an unattended run: the CLI's own key
// for the machine — made now, or reused — the key file --key names, or the
// key the SSH agent holds with the fingerprint --key names.
func keyFromFlag(out io.Writer, dir, machine, path string) (chosenKey, error) {
	if fingerprint, ok := strings.CutPrefix(path, agentKeyPrefix); ok {
		return keyFromAgent(fingerprint)
	}
	if path != "" && path != "new" {
		expanded, err := expandHome(path)
		if err != nil {
			return chosenKey{}, err
		}
		public, err := keys.PublicFor(expanded)
		if err != nil {
			return chosenKey{}, err
		}
		return chosenKey{Path: expanded, Public: public}, nil
	}
	mine := filepath.Join(keys.Dir(dir), machine)
	if _, err := os.Stat(mine); err == nil {
		public, err := keys.PublicFor(mine)
		if err != nil {
			return chosenKey{}, err
		}
		return chosenKey{Path: mine, Public: public}, nil
	}
	generated, public, err := keys.Generate(keys.Dir(dir), machine)
	if err != nil {
		return chosenKey{}, err
	}
	fmt.Fprintf(out, "made %s\n", generated)
	return chosenKey{Path: generated, Public: public}, nil
}

// keyFromAgent is the key the SSH agent holds with this fingerprint. Only its
// public half is recorded, so from then on that one key is offered and never
// everything the agent holds.
func keyFromAgent(fingerprint string) (chosenKey, error) {
	held, err := agentKeys()
	if err != nil {
		return chosenKey{}, err
	}
	if len(held) == 0 {
		return chosenKey{}, fmt.Errorf("--key %s%s names a key in the SSH agent, and no agent answered "+
			"with any key: check SSH_AUTH_SOCK, and that the password manager holding it is unlocked",
			agentKeyPrefix, fingerprint)
	}
	offered := make([]string, 0, len(held))
	for _, k := range held {
		if k.Fingerprint == fingerprint {
			return chosenKey{Public: k.PublicKey}, nil
		}
		offered = append(offered, k.Fingerprint)
	}
	return chosenKey{}, fmt.Errorf("the SSH agent holds no key %s; it holds %s",
		fingerprint, strings.Join(offered, ", "))
}

// askForKey offers the three ways in: a key of the CLI's own, a key file, or
// one the agent already holds.
func askForKey(r *bufio.Reader, out io.Writer, dir, machine string) (chosenKey, error) {
	held, err := agentKeys()
	if err != nil {
		// An agent that answers and will not talk is worth saying out loud,
		// and is not a reason to stop: the other two ways in still work.
		fmt.Fprintf(out, "\nthe SSH agent could not be read (%v), so its keys are not offered\n", err)
		held = nil
	}

	mine := filepath.Join(keys.Dir(dir), machine)
	_, err = os.Stat(mine)
	reuse := err == nil

	fmt.Fprintf(out, "\nHow should the CLI log in to this machine?\n")
	if reuse {
		fmt.Fprintf(out, "  1) use the key it already made at %s (recommended)\n", mine)
	} else {
		fmt.Fprintf(out, "  1) make a key of its own, used for nothing else (recommended)\n")
	}
	fmt.Fprintf(out, "  2) use a key file already on your computer\n")
	for i, k := range held {
		fmt.Fprintf(out, "  %d) %s  %s  (from the SSH agent)\n", i+3, k.Fingerprint, k.Comment)
	}

	answer, err := ask(r, out, "choice", "1")
	if err != nil {
		return chosenKey{}, err
	}
	choice, err := strconv.Atoi(answer)
	if err != nil || choice < 1 || choice > 2+len(held) {
		return chosenKey{}, fmt.Errorf("%q is not one of the choices", answer)
	}

	switch choice {
	case 1:
		if reuse {
			public, err := keys.PublicFor(mine)
			if err != nil {
				return chosenKey{}, err
			}
			return chosenKey{Path: mine, Public: public}, nil
		}
		path, public, err := keys.Generate(keys.Dir(dir), machine)
		if err != nil {
			return chosenKey{}, err
		}
		fmt.Fprintf(out, "made %s\n", path)
		return chosenKey{Path: path, Public: public}, nil

	case 2:
		path, err := ask(r, out, "path to the private key", "")
		if err != nil {
			return chosenKey{}, err
		}
		path, err = expandHome(path)
		if err != nil {
			return chosenKey{}, err
		}
		if path == "" {
			return chosenKey{}, errors.New("no path was given")
		}
		public, err := keys.PublicFor(path)
		if err != nil {
			return chosenKey{}, err
		}
		return chosenKey{Path: path, Public: public}, nil
	}

	// An agent key has no path. Leaving `key` out of the configuration is
	// what makes the CLI ask the agent every time from then on.
	return chosenKey{Public: held[choice-3].PublicKey}, nil
}

// bootstrap gets in, makes sure the key works, and shuts the password door.
//
// The order is the whole point and is not negotiable: prove the key on a
// connection of its own before turning password login off. The other way round
// is locking the door with the key still inside.
func bootstrap(ctx context.Context, out io.Writer, m config.Machine, key chosenKey, noHarden bool,
	password passwordSource) error {
	client, address, err := dialWith(ctx, m, m.User, key.auth())
	unproved := false
	if err == nil {
		if err := refuseUnknownSystem(ctx, client, out, address); err != nil {
			_ = client.Close()
			return err
		}
	}
	switch {
	case err == nil && tailscaleSSH(client):
		unproved = true
		// Tailscale SSH let the connection in by tailnet identity, not by the
		// key, so "the key already logs in" would be a claim nothing proved.
		// The key goes in anyway: without it the machine is unreachable the
		// day Tailscale SSH is off, or from outside the tailnet.
		fmt.Fprintf(out, "%s answered through Tailscale SSH, which lets tailnet members in without "+
			"checking a key, so the key cannot be proved from here; installing it anyway.\n", address)
		if err := installKey(ctx, client, key.Public); err != nil {
			_ = client.Close()
			return err
		}
	case err == nil:
		fmt.Fprintf(out, "%s already logs in as %s@%s, so no password is needed.\n",
			key.describe(), m.User, address)
	case !errors.Is(err, remote.ErrAuthRefused):
		// Nothing answered. A machine nobody can reach is not one whose
		// password is worth asking for.
		return err
	default:
		client, err = installWithPassword(ctx, out, m, key, password)
		if err != nil {
			return err
		}
	}
	defer func() { _ = client.Close() }()

	// Before anything is changed: an admin with no way to root is one clear
	// sentence here, rather than an apt lock error halfway through.
	if err := checkRoot(ctx, client, m.User); err != nil {
		return err
	}

	switch {
	case noHarden:
		fmt.Fprintf(out, "--no-harden: password login is left as it was.\n")
	case unproved:
		// Locking down comes after the proof, never with it — and nothing
		// proved the key past Tailscale SSH. Turning passwords off now could
		// leave no way in the day Tailscale is down.
		fmt.Fprintf(out, "password login is left as it was: the key could not be proved past Tailscale SSH. "+
			"Prove it from outside the tailnet before the ssh_hardening package turns passwords off.\n")
	default:
		fmt.Fprintf(out, "turning password login off...\n")
		if err := harden(ctx, client); err != nil {
			return err
		}
		fmt.Fprintf(out, "password login is off; the key is the only way in.\n")
	}

	// The last thing done by hand. From here on everything is a play.
	fmt.Fprintf(out, "installing Ansible...\n")
	return installAnsible(ctx, client, out)
}

// installWithPassword is the branch for a machine as it was bought: a root
// password and nothing else. It returns the connection that proved the key.
func installWithPassword(ctx context.Context, out io.Writer, m config.Machine, key chosenKey,
	source passwordSource) (remote.Client, error) {
	address := m.Hosts[0].Address
	fmt.Fprintf(out, "%s does not log in yet, so the password is needed once to install it.\n", key.describe())

	password, err := source(fmt.Sprintf("password for %s@%s", m.User, address))
	if err != nil {
		return nil, err
	}
	if password == "" {
		return nil, errors.New("no password was given, and there is no other way in yet")
	}

	client, _, err := dialWith(ctx, m, m.User, remote.Auth{Password: password})
	if err != nil {
		return nil, err
	}
	if err := refuseUnknownSystem(ctx, client, out, address); err != nil {
		_ = client.Close()
		return nil, err
	}
	err = installKey(ctx, client, key.Public)
	// The password's whole life ends here: one connection, one use, nothing
	// written down.
	_ = client.Close()
	if err != nil {
		return nil, err
	}
	fmt.Fprintf(out, "the key is installed; proving it on a connection of its own...\n")

	proved, err := proveAuth(ctx, m, m.User, key.auth())
	if err != nil {
		// The detail is in the error the caller prints. What goes here is the
		// one thing somebody needs to know before they panic: the machine is
		// still reachable the way they reached it a minute ago.
		fmt.Fprintf(out,
			"\nThe key was installed and does not log in, so nothing was hardened and "+
				"password login is still on: you can still get in with the password.\n")
		return nil, fmt.Errorf("the key was installed but not proved, so password login was left on: %w", err)
	}
	fmt.Fprintf(out, "the key works.\n")
	return proved, nil
}

// passwordSource answers the one question the bootstrap may ask: the admin's
// password, needed only when the key does not log in yet.
type passwordSource func(question string) (string, error)

// askingPassword asks for the password on the streams the flow reads from.
func askingPassword(r *bufio.Reader, source io.Reader, out io.Writer) passwordSource {
	return func(question string) (string, error) { return askPassword(r, source, out, question) }
}

// givenPassword answers with a password that came on stdin, or, when none
// did, with what to do about it: an unattended run has nobody to ask.
func givenPassword(password string) passwordSource {
	return func(string) (string, error) {
		if password == "" {
			return "", errors.New("the key does not log in yet and no password was given: pass the " +
				"admin password on stdin with --password-stdin, or put the key's public half in the " +
				"admin's authorized_keys first")
		}
		return password, nil
	}
}

// readPasswordStdin reads the whole of stdin as the password. Only the line
// ending is taken off, so a password that begins or ends with a space keeps it.
func readPasswordStdin(in io.Reader) (string, error) {
	body, err := io.ReadAll(in)
	if err != nil {
		return "", fmt.Errorf("reading the password from stdin: %w", err)
	}
	password := strings.TrimSuffix(strings.TrimSuffix(string(body), "\n"), "\r")
	if password == "" {
		return "", errors.New("--password-stdin read nothing from stdin: pipe the admin password in")
	}
	return password, nil
}

// askPassword reads a password without echoing it when there is a terminal to
// turn the echo off on, and plainly when there is not.
//
// Deciding from the stream rather than from os.Stdin is what makes every
// branch of this flow testable, and it is also correct: a caller piping
// answers in has no terminal to hide anything from.
func askPassword(r *bufio.Reader, source io.Reader, out io.Writer, question string) (string, error) {
	fmt.Fprintf(out, "%s: ", question)

	if f, ok := source.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		body, err := term.ReadPassword(int(f.Fd()))
		fmt.Fprintln(out)
		if err != nil {
			return "", fmt.Errorf("reading the password: %w", err)
		}
		return string(body), nil
	}

	line, err := r.ReadString('\n')
	if err != nil && line == "" {
		if errors.Is(err, io.EOF) {
			return "", nil
		}
		return "", fmt.Errorf("reading the password: %w", err)
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// expandHome turns a leading ~ into the home directory, because that is how
// people write the path to their own key.
func expandHome(path string) (string, error) {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("finding the home directory: %w", err)
	}
	return filepath.Join(home, strings.TrimPrefix(path, "~")), nil
}

// writeConfig puts the file on disk, readable by its owner and nobody else.
// With exclusive, it only creates the file: one that appeared since the
// caller looked is not overwritten, and the error wraps os.ErrExist.
func writeConfig(dir, path string, file configFile, exclusive bool) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating %s: %w", dir, err)
	}
	body, err := yaml.Marshal(file)
	if err != nil {
		return fmt.Errorf("rendering the configuration: %w", err)
	}
	unlock, err := config.Lock(dir)
	if err != nil {
		return err
	}
	defer unlock()
	flags := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	if exclusive {
		flags = os.O_WRONLY | os.O_CREATE | os.O_EXCL
	}
	f, err := os.OpenFile(path, flags, 0o600)
	if err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	if _, err := f.Write(body); err != nil {
		_ = f.Close()
		return fmt.Errorf("writing %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

// machineEntry is a machine as the file holds it.
func machineEntry(m config.Machine) machineFile {
	addresses := make([]string, 0, len(m.Hosts))
	for _, h := range m.Hosts {
		addresses = append(addresses, h.Address)
	}
	return machineFile{
		Name: m.Name, Hosts: addresses, User: m.User, Port: m.Port, Key: m.Key, AgentKey: m.AgentKey,
		Location: m.Location,
	}
}

// configFile is the shape written to disk. It is separate from config.Config so
// the file keeps its intended field order and omits what was left empty.
type configFile struct {
	Packages   string          `yaml:"packages,omitempty"`
	Machines   []machineFile   `yaml:"machines"`
	Workspaces []workspaceFile `yaml:"workspaces,omitempty"`
	Defaults   defaultsFile    `yaml:"defaults,omitempty"`
	Domain     string          `yaml:"domain,omitempty"`
}

type defaultsFile struct {
	Workspace []string `yaml:"workspace,omitempty"`
}

type machineFile struct {
	Name     string   `yaml:"name"`
	Hosts    []string `yaml:"hosts"`
	User     string   `yaml:"user"`
	Port     int      `yaml:"port"`
	Key      string   `yaml:"key,omitempty"`
	AgentKey string   `yaml:"agent_key,omitempty"`
	Location string   `yaml:"location,omitempty"`
	Packages []string `yaml:"packages,omitempty"`
}

type workspaceFile struct {
	Name    string `yaml:"name"`
	Machine string `yaml:"machine,omitempty"`
	User    string `yaml:"user,omitempty"`
}

// ask prints a question and reads one line. An empty answer takes the default.
func ask(r *bufio.Reader, out io.Writer, question, fallback string) (string, error) {
	if fallback != "" {
		fmt.Fprintf(out, "%s [%s]: ", question, fallback)
	} else {
		fmt.Fprintf(out, "%s: ", question)
	}

	line, err := r.ReadString('\n')
	if err != nil && line == "" {
		if errors.Is(err, io.EOF) {
			return fallback, nil
		}
		return "", fmt.Errorf("reading the answer: %w", err)
	}

	answer := strings.TrimSpace(line)
	if answer == "" {
		return fallback, nil
	}
	return answer, nil
}
