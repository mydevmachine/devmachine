package commands

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/mydevmachine/devmachine/internal/config"
	"github.com/mydevmachine/devmachine/internal/facts"
	"github.com/mydevmachine/devmachine/internal/hostkeys"
	"github.com/mydevmachine/devmachine/internal/local"
	"github.com/mydevmachine/devmachine/internal/repo"
	"github.com/spf13/cobra"
)

func newMachinesCmd(opts *options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "machines",
		Short: "The servers this configuration knows about",
	}
	cmd.AddCommand(
		newMachinesListCmd(opts),
		newMachinesShowCmd(opts),
		newMachinesAddCmd(opts),
		newMachinesTrustCmd(opts),
		newMachinesScanCmd(opts),
		newMachinesEditCmd(opts),
		newMachinesRmCmd(opts),
		newMachinesCreateLocalCmd(opts),
		newMachinesStartCmd(),
		newMachinesStopCmd(),
		newMachinesDeleteLocalCmd(),
	)
	return cmd
}

func newMachinesListCmd(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List the configured machines and the workspaces on each",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, found, err := loadConfigIfAny(opts)
			if err != nil {
				return err
			}

			if opts.format == formatJSON {
				return writeJSON(cmd.OutOrStdout(), asJSON(cfg).Machines)
			}
			if !found {
				cmd.Println("No machines yet: `devmachine setup` adds the first one.")
				return nil
			}

			return printMachinesTable(cmd.OutOrStdout(), cfg)
		},
	}
}

// printMachinesTable writes one row per machine under a header, so a new
// column never shifts the meaning of the ones before it.
func printMachinesTable(out io.Writer, cfg config.Config) error {
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tADDRESSES\tPORT\tLOCATION\tWORKSPACES")
	for _, m := range cfg.Machines {
		addresses := make([]string, 0, len(m.Hosts))
		for _, h := range m.Hosts {
			addresses = append(addresses, h.Address)
		}
		names := make([]string, 0)
		for _, ws := range cfg.WorkspacesOn(m.Name) {
			names = append(names, ws.Name)
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", m.Name, orDash(strings.Join(addresses, ",")),
			orDash(portText(m.Port)), m.EffectiveLocation(), orNone(strings.Join(names, ", ")))
	}
	if err := w.Flush(); err != nil {
		return fmt.Errorf("writing the machines table: %w", err)
	}
	return nil
}

func portText(port int) string {
	if port == 0 {
		return ""
	}
	return strconv.Itoa(port)
}

func orNone(value string) string {
	if value == "" {
		return "none"
	}
	return value
}

func newMachinesAddCmd(opts *options) *cobra.Command {
	var (
		s        setupOptions
		selfName string
	)

	c := &cobra.Command{
		Use:   "add",
		Short: "Connect another server and add it to the configuration",
		Long: "Asks the same questions as `setup`, minus the domain, and runs the " +
			"same bootstrap: it installs a key, proves the key on a connection of " +
			"its own, turns password login off, and installs Ansible.\n\n" +
			"`setup` writes the first machine. This writes every one after it, and, " +
			"with --address and no config.yml yet, the first one too, without a terminal.\n\n" +
			"On a Mac it installs Ansible through a package manager package instead: mac-brew " +
			"(Homebrew) or mac-ports (MacPorts). It asks which one when the Mac has neither or both " +
			"(--package-manager), and asks before it installs anything (--install-prerequisites); " +
			"--yes never installs them.\n\n" +
			"--self <name> adds your computer as a machine instead: no address, no key, no " +
			"password. It runs the mac-brew package's bootstrap (mac-ports when the machine lists it) " +
			"on your computer, which installs nothing when Homebrew and ansible-playbook are already there.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, _, err := config.Dir(opts.configDir)
			if err != nil {
				return err
			}
			if selfName != "" {
				return runMachinesAddSelf(cmd.Context(), dir, cmd.InOrStdin(), cmd.OutOrStdout(), selfName, s)
			}
			return runMachinesAdd(cmd.Context(), dir, cmd.InOrStdin(), cmd.OutOrStdout(), s)
		},
	}
	c.Flags().BoolVar(&s.noEssentials, "no-essentials", false,
		"start the machine with no packages, instead of the essentials")
	c.Flags().BoolVar(&s.noHarden, "no-harden", false,
		"leave password login on (the key is still installed and proved)")
	c.Flags().BoolVar(&s.noAliases, "no-aliases", false,
		"do not ask about SSH host entries, and do not write them")
	c.Flags().BoolVar(&s.yes, "yes", false, "answer yes to writing SSH host entries, without asking")
	addPrerequisiteFlags(c, &s)
	c.Flags().StringVar(&selfName, "self", "",
		"add your computer as a machine, named <name>, instead of asking for an address")
	c.Flags().StringVar(&s.address, "address", "",
		"the machine's address; with it, nothing is asked and every other answer is a flag or its default")
	c.Flags().StringVar(&s.name, "name", "", "the machine's name (needed with --address)")
	c.Flags().StringVar(&s.user, "user", config.DefaultAdminUser,
		"the admin login: root, or an account with passwordless sudo")
	c.Flags().IntVar(&s.port, "port", config.DefaultPort, "the SSH port")
	c.Flags().StringVar(&s.key, "key", "new",
		"`new` for a key of the CLI's own for this machine (made or reused), a private key file, "+
			"or agent:<SHA256 fingerprint> for a key the SSH agent holds")
	c.Flags().StringVar(&s.fingerprint, "fingerprint", "",
		"the host key fingerprint to trust on first contact (SHA256:…), checked through another channel")
	c.Flags().BoolVar(&s.tailscale, "tailscale", false, "also add the tailscale package")
	c.Flags().StringVar(&s.domain, "domain", "",
		"the domain, written only when there is no config.yml yet and add writes a new one")
	c.Flags().BoolVar(&s.passwordStdin, "password-stdin", false,
		"with --address, read the admin password from stdin, used once to install the key")
	c.Flags().StringVar(&s.location, "location", "",
		"where the machine is, such as hostinger, home or office; left out, it is asked, "+
			"or external with --address (local with --self)")
	return c
}

// runMachinesAddSelf writes a self machine into config.yml and prepares it:
// no address is asked for, because there is none to give.
func runMachinesAddSelf(ctx context.Context, dir string, in io.Reader, out io.Writer, name string,
	opts setupOptions) error {
	if err := checkPackageManager(opts.packageManager); err != nil {
		return err
	}
	location, err := config.NormalizeLocation(opts.location)
	if err != nil {
		return err
	}
	current, err := config.Load(dir)
	if err != nil {
		return err
	}
	if name == "" {
		return errors.New("a machine needs a name: it is how every other command says which one to act on")
	}
	for _, m := range current.Machines {
		if m.Self {
			return fmt.Errorf(
				"machine %q is already `self: true`: only one machine can be your computer", m.Name)
		}
	}
	if _, err := current.Machine(name); err == nil {
		return fmt.Errorf("a machine named %q is already configured: pick another name", name)
	}

	m := config.Machine{Name: name, Self: true, Location: location}
	if err := config.AddMachine(dir, m); err != nil {
		return err
	}
	repo.AutoCommit(ctx, dir, "chore(config): add machine "+name)
	fmt.Fprintf(out, "\nadded %s (your computer) to %s\n\n", name, filepath.Join(dir, config.FileName))

	prep, err := newAnsiblePrep(dir, current.Packages, bufio.NewReader(in), in, opts, nil)
	if err != nil {
		return err
	}
	if err := prepareExistingSelf(ctx, out, m, prep); err != nil {
		return err
	}

	fmt.Fprintf(out, "\nNext: `devmachine doctor --machine %s`, then `devmachine sync --machine %s`.\n", name, name)
	return nil
}

type machineEditOptions struct {
	set   []string
	unset []string
	check bool
	yes   bool
	// location is applied only when locationSet, so `--location ""` can
	// clear it.
	location    string
	locationSet bool
}

func newMachinesEditCmd(opts *options) *cobra.Command {
	var e machineEditOptions

	c := &cobra.Command{
		Use:   "edit <name>",
		Short: "Change a machine's package settings or its location",
		Long: "It edits the configuration and touches no machine. `devmachine " +
			"sync` is what applies the change.\n\n" +
			"`--set <package>.<name>=<value>` writes into the machine's " +
			"`settings:`, which is how a machine package's variables are set. " +
			"The value is read as YAML, so `[a, b]` is a list. An empty value, " +
			"or `--unset <package>.<name>`, takes the setting out again.\n\n" +
			"A setting for a package the machine does not install is refused.\n\n" +
			"`--location <text>` says where the machine is, such as hostinger, home " +
			"or office. `--location \"\"` clears it: external again, or local for " +
			"your own computer.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			e.locationSet = cmd.Flags().Changed("location")
			return runMachineEdit(cmd, opts, args[0], e)
		},
	}
	c.Flags().StringArrayVar(&e.set, "set", nil, "a setting, as <package>.<name>=<value>")
	c.Flags().StringArrayVar(&e.unset, "unset", nil, "a setting to take out, as <package>.<name>")
	c.Flags().StringVar(&e.location, "location", "", "where the machine is; an empty value clears it")
	c.Flags().BoolVar(&e.check, "check", false, "say what would change, and change nothing")
	c.Flags().BoolVar(&e.yes, "yes", false, "do not ask")
	return c
}

func runMachineEdit(cmd *cobra.Command, opts *options, name string, e machineEditOptions) error {
	dir, _, err := config.Dir(opts.configDir)
	if err != nil {
		return err
	}
	cfg, err := loadConfig(opts)
	if err != nil {
		return err
	}
	m, err := cfg.Machine(name)
	if err != nil {
		return err
	}

	settings, changes, err := editSettings(m.Settings, e.set, e.unset)
	if err != nil {
		return err
	}
	settingsChanged := len(changes) > 0
	location, locationChange, err := editLocation(m, e)
	if err != nil {
		return err
	}
	if locationChange != "" {
		changes = append(changes, locationChange)
	}
	if len(changes) == 0 {
		return fmt.Errorf("nothing to change on %q: pass --set, --unset or --location", name)
	}

	// Validating the whole configuration is what refuses a setting for a
	// package the machine does not install.
	edited := cfg
	edited.Machines = slices.Clone(cfg.Machines)
	for i := range edited.Machines {
		if edited.Machines[i].Name == name {
			edited.Machines[i].Settings = settings
			edited.Machines[i].Location = location
		}
	}
	if err := edited.Validate(); err != nil {
		return err
	}

	if e.check {
		for _, line := range changes {
			cmd.Printf("would %s\n", line)
		}
		cmd.Println("Nothing was written.")
		return nil
	}
	if !e.yes {
		ok, err := confirm(cmd.InOrStdin(), cmd.OutOrStdout(),
			fmt.Sprintf("On %s: %s?", name, strings.Join(changes, "; ")))
		if err != nil {
			return err
		}
		if !ok {
			return errDeclined
		}
	}

	if settingsChanged {
		if err := config.SetMachineSettings(dir, name, settings); err != nil {
			return err
		}
	}
	if locationChange != "" {
		if err := config.SetMachineLocation(dir, name, location); err != nil {
			return err
		}
	}
	repo.AutoCommit(cmd.Context(), dir, "chore(config): update machine "+name)
	for _, line := range changes {
		cmd.Printf("%s: %s\n", name, line)
	}
	if settingsChanged {
		cmd.Println("The machine is untouched until the next `devmachine sync`.")
	}
	return nil
}

// editLocation is the location the machine gets from `--location`, and the
// change in words, empty when there is none.
func editLocation(m config.Machine, e machineEditOptions) (string, string, error) {
	if !e.locationSet {
		return m.Location, "", nil
	}
	location, err := config.NormalizeLocation(e.location)
	if err != nil {
		return "", "", err
	}
	current, _ := config.NormalizeLocation(m.Location)
	if location == current {
		return m.Location, "", nil
	}
	if location == "" {
		cleared := m
		cleared.Location = ""
		return "", "clear the location (" + cleared.EffectiveLocation() + " again)", nil
	}
	return location, "set the location to " + location, nil
}

func newMachinesRmCmd(opts *options) *cobra.Command {
	var yes bool

	c := &cobra.Command{
		Use:   "rm <name>",
		Short: "Forget a machine, leaving the server running",
		Long: "Takes the machine out of the configuration and does nothing at all " +
			"to the server: it keeps running, with everything on it, and it is " +
			"still reachable by the key.\n\n" +
			"It is not `machines delete-local`, which destroys a machine on this " +
			"computer and everything on it.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, _, err := config.Dir(opts.configDir)
			if err != nil {
				return err
			}
			if !yes {
				ok, err := confirm(cmd.InOrStdin(), cmd.OutOrStdout(),
					"Forget the machine "+args[0]+"? The server keeps running, untouched.")
				if err != nil {
					return err
				}
				if !ok {
					return errDeclined
				}
			}
			if err := config.RemoveMachine(dir, args[0]); err != nil {
				return err
			}
			if err := facts.Remove(dir, args[0]); err != nil {
				return err
			}
			repo.AutoCommit(cmd.Context(), dir, "chore(config): remove machine "+args[0])
			cmd.Printf("%s is out of the configuration.\n", args[0])
			cmd.Printf("The server itself is untouched and still running: nothing on it was " +
				"changed or deleted, and the key still gets in.\n")
			cmd.Printf("`machines delete-local` is the one that destroys a machine, and only " +
				"one on your computer.\n")

			updated, err := config.Load(dir)
			if err != nil {
				return err
			}
			return refreshAliases(updated, cmd.OutOrStdout())
		},
	}
	c.Flags().BoolVar(&yes, "yes", false, "forget it without asking")
	return c
}

// runMachinesAdd asks for a machine, records it, then takes it over.
//
// It reads and writes through the streams it is given, for the same reason
// setup does: every branch of the bootstrap is reachable without a terminal.
func runMachinesAdd(ctx context.Context, dir string, in io.Reader, out io.Writer, opts setupOptions) error {
	if err := checkPackageManager(opts.packageManager); err != nil {
		return err
	}
	location, err := config.NormalizeLocation(opts.location)
	if err != nil {
		return err
	}
	current, err := config.Load(dir)
	fresh := errors.Is(err, os.ErrNotExist)
	if fresh && !opts.unattended() {
		return fmt.Errorf("there is no %s yet: `devmachine setup` asks for the first machine, domain "+
			"included, and `machines add --address …` adds it without questions",
			filepath.Join(dir, config.FileName))
	}
	if fresh {
		current, err = config.Config{}, nil
	}
	if err != nil {
		return err
	}
	if opts.domain != "" && !fresh {
		return fmt.Errorf("--domain only goes into a new configuration, and %s already exists: "+
			"set `domain:` in it instead", filepath.Join(dir, config.FileName))
	}

	if opts.passwordStdin && !opts.unattended() {
		return errors.New("--password-stdin needs --address: without it the answers are asked on " +
			"stdin, and the password is asked there too")
	}
	var password passwordSource
	if opts.unattended() {
		given := ""
		if opts.passwordStdin {
			if given, err = readPasswordStdin(in); err != nil {
				return err
			}
		}
		password = givenPassword(given)
		// Nothing is read: an unattended run that reached a question would
		// otherwise block on a terminal nobody is watching.
		in = strings.NewReader("")
	}
	r := bufio.NewReader(in)
	if password == nil {
		password = askingPassword(r, in, out)
	}
	var m config.Machine
	if opts.unattended() {
		m = machineFromFlags(opts)
	} else if m, err = askForMachine(r, out, ""); err != nil {
		return err
	}
	if m.Name == "" {
		return errors.New("a machine needs a name: it is how every other command says which one to act on")
	}
	if _, err := current.Machine(m.Name); err == nil {
		return fmt.Errorf("a machine named %q is already configured: pick another name", m.Name)
	}
	if location == "" && !opts.unattended() {
		if location, err = askForLocation(r, out); err != nil {
			return err
		}
	}
	m.Location = location
	if fresh {
		if err := (config.Config{Machines: []config.Machine{m}, Domain: opts.domain}).Validate(); err != nil {
			return err
		}
	}
	trusted := filepath.Join(dir, config.KnownHostsFileName)
	staging, err := os.MkdirTemp("", "devmachine-add-")
	if err != nil {
		return fmt.Errorf("making a place to hold the host key until the machine is added: %w", err)
	}
	defer func() { _ = os.RemoveAll(staging) }()
	// Until the bootstrap works, the host key is trusted in a file of this
	// run's own. Written to known_hosts at once, a failed run left a key
	// keyed by the name, and the next try at a corrected address or a
	// rebuilt server was refused as a changed key for a machine that is not
	// configured, which `machines trust` cannot replace.
	m.KnownHostsFile = filepath.Join(staging, config.KnownHostsFileName)
	switch {
	case opts.hostKey != nil:
		err = trustPresented(out, m, opts.hostKey, m.Hosts[0].Address,
			func(string) (bool, error) { return true, nil })
	case opts.unattended():
		err = trustExpected(ctx, out, m, opts.fingerprint)
	default:
		err = trustFirstContact(ctx, r, out, m)
	}
	if err != nil {
		return err
	}

	var key chosenKey
	if opts.unattended() {
		key, err = keyFromFlag(out, dir, m.Name, opts.key)
	} else {
		key, err = askForKey(r, out, dir, m.Name)
	}
	if err != nil {
		return err
	}
	if err := recordKey(dir, &m, key); err != nil {
		return err
	}
	release := current.Packages
	if fresh {
		release = pinForNewConfig(ctx, out)
	}
	m.Packages = startingPackages(ctx, dir, release, opts.noEssentials, out)

	// The machine is written only after the bootstrap proved the key. Written
	// first, a failed run left an entry behind, and running again to fix it
	// was refused as a name already configured.
	prep, err := newAnsiblePrep(dir, release, r, in, opts, func(name string) error {
		m.Packages = append(m.Packages, name)
		return nil
	})
	if err != nil {
		return err
	}
	prep.replaceStarting = func(names []string) error {
		m.Packages = names
		return nil
	}
	if err := bootstrap(ctx, out, dir, m, key, opts.noHarden, password, prep); err != nil {
		return err
	}
	if err := keepHostKey(m, trusted); err != nil {
		return err
	}
	m.KnownHostsFile = trusted
	if fresh {
		err = writeNewConfig(dir, m, release, opts.domain, true)
		if errors.Is(err, os.ErrExist) {
			fresh = false
			fmt.Fprintf(out, "\n%s appeared while %s was being set up, so %s is added to it",
				config.FileName, m.Name, m.Name)
			if opts.domain != "" {
				fmt.Fprintf(out, "; --domain %s was not written, since that file already says which domain it has",
					opts.domain)
			}
			fmt.Fprintln(out, ".")
		}
	}
	if !fresh {
		err = config.AddMachine(dir, m)
	}
	if err != nil {
		return err
	}
	repo.AutoCommit(ctx, dir, "chore(config): add machine "+m.Name)
	fmt.Fprintf(out, "\nadded %s to %s\n\n", m.Name, filepath.Join(dir, config.FileName))
	if err := offerSSHAliases(r, out, dir, opts.noAliases, opts.yes || opts.unattended()); err != nil {
		return err
	}
	switch {
	case opts.unattended() && opts.tailscale:
		err = addTailscale(out, dir, m.Name)
	case !opts.unattended():
		err = offerTailscale(r, out, dir, m.Name)
	}
	if err != nil {
		return err
	}

	if updated, err := config.Load(dir); err == nil {
		if err := refreshAliases(updated, out); err != nil {
			return err
		}
	}

	fmt.Fprintf(out, "\nNext: `devmachine doctor --machine %s`, then `devmachine sync --machine %s`.\n",
		m.Name, m.Name)
	return nil
}

// keepHostKey copies the machine's host key from the run's own file into the
// configuration's known_hosts, replacing whatever an earlier run left there
// under the same name.
func keepHostKey(m config.Machine, trusted string) error {
	staged, err := hostkeys.Open(m.KnownHostsFile)
	if err != nil {
		return err
	}
	key, err := staged.Key(m.Name, m.Port)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(trusted), 0o700); err != nil {
		return fmt.Errorf("creating the configuration directory for host trust: %w", err)
	}
	store, err := hostkeys.Open(trusted)
	if err != nil {
		return err
	}
	return store.Put(m.Name, m.Port, key)
}

// pinForNewConfig is the packages release a new configuration is pinned to:
// the latest, as setup pins it, or none when it cannot be found.
func pinForNewConfig(ctx context.Context, out io.Writer) string {
	release, err := latestPackagesRelease(ctx)
	if err != nil {
		fmt.Fprintf(out, "\nno packages release is pinned (%v): run `devmachine packages pin` before the first sync.\n", err)
		return ""
	}
	return release
}

// writeNewConfig writes the configuration setup would have written for this
// machine, and AGENTS.md beside it. With exclusive it never overwrites a
// config.yml, and says so with an error that wraps os.ErrExist.
func writeNewConfig(dir string, m config.Machine, release, domain string, exclusive bool) error {
	entry := machineEntry(m)
	entry.Packages = m.Packages
	if err := writeConfig(dir, filepath.Join(dir, config.FileName), configFile{
		Packages: release,
		Machines: []machineFile{entry},
		Defaults: defaultsFile{Workspace: config.DefaultWorkspacePackages},
		Domain:   domain,
	}, exclusive); err != nil {
		return err
	}
	return writeAgentsFile(dir)
}

// askForLocation asks where the machine is. External, the default, is
// written as nothing, since nothing already means external.
func askForLocation(r *bufio.Reader, out io.Writer) (string, error) {
	answer, err := ask(r, out, "location (where it is: a provider, home, office…)", config.LocationExternal)
	if err != nil {
		return "", err
	}
	location, err := config.NormalizeLocation(answer)
	if err != nil || location == config.LocationExternal {
		return "", err
	}
	return location, nil
}

// machineFromFlags is askForMachine's answer for an unattended run.
func machineFromFlags(opts setupOptions) config.Machine {
	return config.Machine{
		Name:  opts.name,
		Hosts: []config.Host{{Address: opts.address}},
		User:  opts.user,
		Port:  opts.port,
	}
}

// createLocal is the seam a test replaces so no VM is booted.
var createLocal = local.Create

func newMachinesCreateLocalCmd(opts *options) *cobra.Command {
	var (
		add    bool
		s      setupOptions
		size   = local.DefaultSize
		distro string
	)
	c := &cobra.Command{
		Use:   "create-local <name>",
		Short: "Create a machine on your computer, as a bought server arrives",
		Long: "Create a machine on your computer, as a bought server arrives.\n\n" +
			"It needs Lima. The machine comes up with root reachable over SSH by " +
			"password and no key installed, which is where `devmachine setup` starts.\n\n" +
			"Without --add, nothing is written to the configuration: `setup` or " +
			"`machines add` does that. With --add, it is added at once, the way " +
			"`machines add --address` adds a server.\n\n" +
			"--cpus, --memory and --disk size the VM; each must fit this computer.\n\n" +
			"--distro picks the system: ubuntu (the default) or arch. Arch is x86_64 under " +
			"qemu, emulated on Apple Silicon: slow, but it boots.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			for _, flag := range []string{"key", "no-essentials", "no-aliases", "location"} {
				if !add && cmd.Flags().Changed(flag) {
					return fmt.Errorf("--%s only applies with --add", flag)
				}
			}
			d, err := local.ParseDistro(distro)
			if err != nil {
				return err
			}
			if !add {
				m, err := createLocal(cmd.Context(), args[0], d, size, cmd.ErrOrStderr())
				if err != nil {
					return err
				}
				m.Location = config.LocationLocal
				return reportLocalMachine(cmd, opts, m, size, false)
			}
			dir, _, err := config.Dir(opts.configDir)
			if err != nil {
				return err
			}
			m, err := createAndAddLocal(cmd.Context(), dir, opts.configDir, cmd.ErrOrStderr(), args[0], d, size, s)
			if err != nil {
				return err
			}
			return reportLocalMachine(cmd, opts, m, size, true)
		},
	}
	c.Flags().BoolVar(&add, "add", false,
		"also add it to the configuration: install a key with its password, prove it, and write it")
	c.Flags().StringVar(&s.key, "key", "new",
		"with --add: `new`, a private key file, or agent:<SHA256 fingerprint>, as `machines add --key`")
	c.Flags().BoolVar(&s.noEssentials, "no-essentials", false, "with --add: start the machine with no packages")
	c.Flags().BoolVar(&s.noAliases, "no-aliases", false, "with --add: do not write SSH host entries")
	c.Flags().StringVar(&s.location, "location", config.LocationLocal,
		"with --add: where the machine is, such as bedroom or office")
	c.Flags().StringVar(&distro, "distro", string(local.Ubuntu), "the system the VM runs: ubuntu or arch")
	c.Flags().IntVar(&size.CPUs, "cpus", local.DefaultSize.CPUs, "CPUs for the VM, at most this computer's cores")
	c.Flags().IntVar(&size.MemoryGiB, "memory", local.DefaultSize.MemoryGiB,
		"memory for the VM in GiB, less than this computer has")
	c.Flags().IntVar(&size.DiskGiB, "disk", local.DefaultSize.DiskGiB,
		fmt.Sprintf("disk for the VM in GiB, at least %d", local.MinDiskGiB))
	return c
}

// createAndAddLocal creates a local machine and adds it, as one step.
//
// Progress goes to out, which is stderr, so `--format json` leaves a document
// on stdout and nothing else.
func createAndAddLocal(ctx context.Context, dir, configFlag string, out io.Writer, name string,
	distro local.Distro, size local.Size, s setupOptions) (config.Machine, error) {
	location, err := config.NormalizeLocation(s.location)
	if err != nil {
		return config.Machine{}, err
	}
	s.location = location
	current, err := config.Load(dir)
	if err == nil {
		if _, err := current.Machine(name); err == nil {
			return config.Machine{}, fmt.Errorf("a machine named %q is already configured: pick another name", name)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return config.Machine{}, err
	}

	m, err := createLocal(ctx, name, distro, size, out)
	if err != nil {
		return config.Machine{}, err
	}
	// The host key is trusted as it answers: this command made the VM a
	// moment ago, and it answers only on this computer's loopback.
	presented, _, err := scanHostKey(ctx, m)
	if err != nil {
		return config.Machine{}, fmt.Errorf("the local machine %q is running, and was not added: %w", name, err)
	}
	s.name, s.address, s.user, s.port = m.Name, m.Hosts[0].Address, m.User, m.Port
	s.fingerprint, s.hostKey = hostkeys.Fingerprint(presented), presented
	s.passwordStdin = true
	if err := runMachinesAdd(ctx, dir, strings.NewReader(local.Password), out, s); err != nil {
		return config.Machine{}, fmt.Errorf("the local machine %q is running, and was not added: %w; "+
			"once the cause is fixed, add it with: %s", name, err, addLocalCommand(configFlag, s))
	}
	added, err := config.Load(dir)
	if err != nil {
		return config.Machine{}, err
	}
	return added.Machine(name)
}

// addLocalCommand is the `machines add` that adds a running local machine
// by hand, ready to copy: every answer create-local --add gave, and the
// public password on stdin.
func addLocalCommand(configFlag string, s setupOptions) string {
	command := "printf '%s' " + local.Password + " | devmachine"
	if configFlag != "" {
		command += " --config " + configFlag
	}
	command += fmt.Sprintf(" machines add --name %s --address %s --port %d --fingerprint %s --password-stdin",
		s.name, s.address, s.port, s.fingerprint)
	if s.key != "" && s.key != "new" {
		command += " --key " + s.key
	}
	if s.noEssentials {
		command += " --no-essentials"
	}
	if s.noAliases {
		command += " --no-aliases"
	}
	if s.location != "" {
		command += " --location " + locationArgument(s.location)
	}
	return command
}

// locationArgument is a location as one shell word. A normalized location
// holds no quote, so wrapping one with a space is enough.
func locationArgument(location string) string {
	if strings.Contains(location, " ") {
		return "'" + location + "'"
	}
	return location
}

func newMachinesStartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "start <name>",
		Short: "Start a machine on your computer",
		Long: "Start a machine on your computer.\n\n" +
			"Only a local machine, created with `create-local`: a bought server " +
			"is not the CLI's to switch on.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := local.Start(cmd.Context(), args[0]); err != nil {
				return err
			}
			cmd.Printf("%s is running\n", args[0])
			return nil
		},
	}
}

func newMachinesStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop <name>",
		Short: "Stop a machine on your computer",
		Long: "Stop a machine on your computer, keeping its disk.\n\n" +
			"Only a local machine, created with `create-local`: a bought server " +
			"is not the CLI's to switch off.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := local.Stop(cmd.Context(), args[0]); err != nil {
				return err
			}
			cmd.Printf("%s is stopped\n", args[0])
			return nil
		},
	}
}

func newMachinesDeleteLocalCmd() *cobra.Command {
	var yes bool

	c := &cobra.Command{
		Use:   "delete-local <name>",
		Short: "Destroy a machine on your computer",
		Long: "Destroy a machine on your computer, and everything on it.\n\n" +
			"Only a local machine, created with `create-local`. This is the one " +
			"that destroys: `machines rm` only forgets a server, and leaves it " +
			"running.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !yes {
				ok, err := confirm(cmd.InOrStdin(), cmd.OutOrStdout(),
					"Destroy the local machine "+args[0]+" and everything on it?")
				if err != nil {
					return err
				}
				if !ok {
					return errDeclined
				}
			}
			if err := local.Delete(cmd.Context(), args[0]); err != nil {
				return err
			}
			cmd.Printf("%s is gone\n", args[0])
			return nil
		},
	}
	c.Flags().BoolVar(&yes, "yes", false, "destroy it without asking")
	return c
}

// reportLocalMachine prints a new local machine the way `machines list` prints
// a configured one, so the same fields mean the same thing.
func reportLocalMachine(cmd *cobra.Command, opts *options, m config.Machine, size local.Size, added bool) error {
	addresses := make([]string, 0, len(m.Hosts))
	for _, h := range m.Hosts {
		addresses = append(addresses, h.Address)
	}

	if opts.format == formatJSON {
		return writeJSON(cmd.OutOrStdout(), machineJSON{
			Name: m.Name, Hosts: addresses, AdminUser: m.User,
			Port: m.Port, Key: m.Key, AgentKey: m.AgentKey, Workspaces: []string{}, Packages: onOrNone(m.Packages),
			Location: m.EffectiveLocation(),
			Size:     &sizeJSON{CPUs: size.CPUs, MemoryGiB: size.MemoryGiB, DiskGiB: size.DiskGiB},
		})
	}

	cmd.Printf("%-12s %-28s port %-6d admin: %s  location: %s\n",
		m.Name, strings.Join(addresses, ","), m.Port, m.User, m.EffectiveLocation())
	cmd.Printf("Size: %d CPUs, %d GiB memory, %d GiB disk.\n", size.CPUs, size.MemoryGiB, size.DiskGiB)
	if added {
		cmd.Printf("It is in the configuration, and logs in with %s.\n", chosenKey{Path: m.Key, Public: m.AgentKey}.describe())
		return nil
	}
	cmd.Printf("It has no key on it yet, and the root password is %q.\n", local.Password)
	cmd.Printf("Set it up with `devmachine setup` (or `devmachine machines add` if you already have a machine).\n")
	cmd.Printf("Next time, `create-local %s --add` does both in one step.\n", m.Name)
	return nil
}
