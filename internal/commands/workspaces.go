package commands

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/mydevmachine/devmachine/internal/config"
	"github.com/mydevmachine/devmachine/internal/expose"
	"github.com/mydevmachine/devmachine/internal/facts"
	"github.com/mydevmachine/devmachine/internal/keys"
	"github.com/mydevmachine/devmachine/internal/remote"
	"github.com/mydevmachine/devmachine/internal/repo"
	"github.com/spf13/cobra"
	"golang.org/x/crypto/ssh"
	"gopkg.in/yaml.v3"
)

func newWorkspacesCmd(opts *options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "workspaces",
		Short: "The environments this configuration knows about",
	}
	cmd.AddCommand(
		newWorkspacesListCmd(opts),
		newWorkspacesNewCmd(opts),
		newWorkspacesEditCmd(opts),
		newWorkspacesDefaultsCmd(opts),
		newWorkspacesRmCmd(opts),
		newWorkspacesDestroyCmd(opts),
	)
	return cmd
}

func newWorkspacesDefaultsCmd(opts *options) *cobra.Command {
	var add, remove []string
	var check, yes bool
	c := &cobra.Command{
		Use:   "defaults",
		Short: "Print or change the packages inherited by future workspaces",
		Long: "With no flag, it prints defaults.workspace. With --add or --rm, it edits only " +
			"defaults.workspace in the local configuration. Existing workspaces and every machine are unchanged.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, _, err := config.Dir(opts.configDir)
			if err != nil {
				return err
			}
			cfg, err := loadConfig(opts)
			if err != nil {
				return err
			}
			for _, name := range remove {
				if slices.Contains(add, name) {
					return fmt.Errorf("--add %s and --rm %s say two different things: pass one of them", name, name)
				}
			}
			if len(add) == 0 && len(remove) == 0 {
				return printWorkspaceDefaults(cmd, opts, cfg.Defaults.Workspace)
			}
			wanted := slices.Clone(cfg.Defaults.Workspace)
			var changes []string
			for _, name := range add {
				if !slices.Contains(wanted, name) {
					wanted = append(wanted, name)
					changes = append(changes, "add the package "+name)
				}
			}
			for _, name := range remove {
				if slices.Contains(wanted, name) {
					wanted = slices.DeleteFunc(wanted, func(held string) bool { return held == name })
					changes = append(changes, "take off the package "+name)
				}
			}
			if len(changes) == 0 {
				return errors.New("nothing to change in future workspace defaults: " +
					"every package to add is there already, and every one to take off is not")
			}
			if check {
				for _, line := range changes {
					cmd.Printf("would %s for future workspaces\n", line)
				}
				cmd.Println("Nothing was written; existing workspaces are unchanged.")
				return nil
			}
			if !yes {
				ok, err := confirm(cmd.InOrStdin(), cmd.OutOrStdout(), "For future workspaces: "+strings.Join(changes, "; ")+"?")
				if err != nil {
					return err
				}
				if !ok {
					return errDeclined
				}
			}
			if err := config.UpdateWorkspaceDefaults(dir, wanted); err != nil {
				return err
			}
			repo.AutoCommit(cmd.Context(), dir, "chore(config): update future workspace defaults")
			if opts.format == formatJSON {
				return writeJSON(cmd.OutOrStdout(), workspaceDefaultsJSON{onOrNone(wanted), true})
			}
			for _, line := range changes {
				cmd.Printf("future workspaces: %s\n", line)
			}
			cmd.Println("The local defaults changed; existing workspaces are unchanged, and no machine was touched.")
			return nil
		},
	}
	c.Flags().StringSliceVar(&add, "add", nil, "a package future workspaces should inherit")
	c.Flags().StringSliceVar(&remove, "rm", nil, "a package future workspaces should stop inheriting")
	c.Flags().BoolVar(&check, "check", false, "say what would change, and change nothing")
	c.Flags().BoolVar(&yes, "yes", false, "do not ask")
	return c
}

type workspaceDefaultsJSON struct {
	Packages []string `json:"packages"`
	Changed  bool     `json:"changed"`
}

func printWorkspaceDefaults(cmd *cobra.Command, opts *options, list []string) error {
	if opts.format == formatJSON {
		return writeJSON(cmd.OutOrStdout(), workspaceDefaultsJSON{onOrNone(list), false})
	}
	if len(list) == 0 {
		cmd.Println("Future workspaces get no package by default. `devmachine workspaces defaults --add <package>` adds one.")
		return nil
	}
	cmd.Printf("Future workspaces get: %s\n", strings.Join(list, ", "))
	return nil
}

// workspaceRow is the output contract, kept apart from the configuration
// struct so a change to how config.yml is written does not silently change
// what every consumer reads.
type workspaceRow struct {
	Name     string         `json:"name"`
	Machine  string         `json:"machine"`
	User     string         `json:"user"`
	Packages []string       `json:"packages"`
	Settings map[string]any `json:"settings,omitempty"`
	// Credentials is the workspace's own answer about each login, "machine"
	// or "own", as `edit --share` writes it. A login it leaves out follows
	// the configuration's `credentials:`, then the package.
	Credentials map[string]string `json:"credentials"`
}

func newWorkspacesListCmd(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "Each workspace, the machine it lives on and what it gets",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig(opts)
			if err != nil {
				return err
			}

			rows := make([]workspaceRow, 0, len(cfg.Workspaces))
			for _, w := range cfg.Workspaces {
				rows = append(rows, workspaceRow{
					Name: w.Name, Machine: machineNameOf(cfg, w), User: w.LinuxUser(),
					Packages: onOrNone(w.Packages), Settings: w.Settings,
					Credentials: credentialsOrNone(w.Credentials),
				})
			}

			if opts.format == formatJSON {
				return writeJSON(cmd.OutOrStdout(), struct {
					Workspaces []workspaceRow `json:"workspaces"`
				}{rows})
			}

			if len(rows) == 0 {
				cmd.Println("No workspace is configured. `devmachine workspaces new <name>` makes one.")
				return nil
			}
			cmd.Printf("%-16s %-12s %-12s %s\n", "NAME", "MACHINE", "USER", "PACKAGES")
			for _, row := range rows {
				list := "none"
				if len(row.Packages) > 0 {
					list = strings.Join(row.Packages, ", ")
				}
				cmd.Printf("%-16s %-12s %-12s %s\n", row.Name, row.Machine, row.User, list)
			}
			return nil
		},
	}
}

// machineNameOf names the machine a workspace runs on, filling in the only one
// there is when the entry leaves it out.
func machineNameOf(cfg config.Config, w config.Workspace) string {
	if w.Machine == "" && len(cfg.Machines) == 1 {
		return cfg.Machines[0].Name
	}
	return w.Machine
}

func newWorkspacesNewCmd(opts *options) *cobra.Command {
	var (
		like     string
		user     string
		packages []string
		check    bool
		yes      bool
	)

	c := &cobra.Command{
		Use:   "new <name>",
		Short: "Declare a workspace, for the next sync to create",
		Long: "It edits the configuration and touches no machine. `devmachine " +
			"sync` is what creates the account on the machine.\n\n" +
			"The package list comes from `defaults.workspace` in the " +
			"configuration, unless --like or --packages says otherwise.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkspaceNew(cmd, opts, args[0], workspaceNewOptions{
				like: like, user: user,
				packages: packages, packagesGiven: cmd.Flags().Changed("packages"),
				check: check, yes: yes,
			})
		},
	}
	c.Flags().StringVar(&like, "like", "", "copy another workspace's packages")
	c.Flags().StringVar(&user, "user", "", "the account on the machine, when it cannot be the workspace's name")
	c.Flags().StringSliceVar(&packages, "packages", nil, "the packages it gets, instead of the default")
	c.Flags().BoolVar(&check, "check", false, "say what would change, and change nothing")
	c.Flags().BoolVar(&yes, "yes", false, "do not ask")
	return c
}

type workspaceNewOptions struct {
	like          string
	user          string
	packages      []string
	packagesGiven bool
	check         bool
	yes           bool
}

func runWorkspaceNew(cmd *cobra.Command, opts *options, name string, o workspaceNewOptions) error {
	dir, _, err := config.Dir(opts.configDir)
	if err != nil {
		return err
	}
	cfg, err := loadConfig(opts)
	if err != nil {
		return err
	}
	if _, err := cfg.Workspace(name); err == nil {
		return fmt.Errorf("a workspace named %q is already configured: pick another name", name)
	}

	machine, err := cfg.Machine(opts.machine)
	if err != nil {
		return err
	}
	if err := adminKey(machine); err != nil {
		return err
	}

	list, err := packagesForNewWorkspace(cfg, o)
	if err != nil {
		return err
	}

	w := config.Workspace{Name: name, Machine: machine.Name, User: o.user, Packages: list}

	if o.check {
		cmd.Printf("would add the workspace %s to %s, with %s\n", name, machine.Name, describePackages(list))
		cmd.Println("Nothing was written.")
		return nil
	}
	if !o.yes {
		ok, err := confirm(cmd.InOrStdin(), cmd.OutOrStdout(),
			fmt.Sprintf("Add the workspace %s to %s, with %s?", name, machine.Name, describePackages(list)))
		if err != nil {
			return err
		}
		if !ok {
			return errDeclined
		}
	}

	if err := config.AddWorkspace(dir, w); err != nil {
		return err
	}
	repo.AutoCommit(cmd.Context(), dir, "chore(config): add workspace "+name)
	cmd.Printf("%s is in the configuration, on %s, with %s.\n", name, machine.Name, describePackages(list))
	cmd.Println("The machine is untouched: `devmachine sync` is what creates the account.")

	updated, err := config.Load(dir)
	if err != nil {
		return err
	}
	if err := refreshAliases(updated, cmd.OutOrStdout()); err != nil {
		return err
	}
	cmd.Println("\nOnce `devmachine sync` has run, reach it:")
	for _, line := range reachLines(name, updated.SSHAliases) {
		cmd.Printf("  %s\n", line)
	}
	return nil
}

// packagesForNewWorkspace decides what a new workspace gets.
//
// --packages beats --like, which beats the configured default. --like copies
// the packages and nothing else: a copied account name would collide, and a
// copied machine would put one workspace wherever another happens to be.
func packagesForNewWorkspace(cfg config.Config, o workspaceNewOptions) ([]string, error) {
	if o.packagesGiven {
		return o.packages, nil
	}
	if o.like != "" {
		source, err := cfg.Workspace(o.like)
		if err != nil {
			return nil, err
		}
		return slices.Clone(source.Packages), nil
	}
	return slices.Clone(cfg.Defaults.Workspace), nil
}

func describePackages(list []string) string {
	if len(list) == 0 {
		return "no packages"
	}
	return "the packages " + strings.Join(list, ", ")
}

// adminKey reports whether there is an administrative key to copy into a new
// account.
//
// A workspace is reachable because that key is authorised on it. With nothing
// to copy the account would be created with no way in, which is worse than
// refusing to declare it.
func adminKey(m config.Machine) error {
	if m.Key != "" {
		if _, err := keys.PublicFor(m.Key); err != nil {
			return fmt.Errorf(
				"machine %q names the key %s and it cannot be read: a workspace is reachable "+
					"because that key is copied into it. Run `devmachine setup` to give the machine a key: %w",
				m.Name, m.Key, err)
		}
		return nil
	}

	held, err := agentKeys()
	if err != nil {
		return fmt.Errorf("reading the SSH agent: %w", err)
	}
	if len(held) == 0 {
		return fmt.Errorf(
			"machine %q has no key, and the SSH agent holds none either: a workspace is "+
				"reachable because a key is copied into it, and there is nothing to copy. "+
				"Run `devmachine setup`", m.Name)
	}

	if m.AgentKey == "" {
		return nil
	}
	// The machine logs in with one recorded key, not whatever the agent
	// happens to hold: a workspace is only reachable through the key that
	// actually gets in.
	want, _, _, _, err := ssh.ParseAuthorizedKey([]byte(m.AgentKey))
	if err != nil {
		return fmt.Errorf("machine %q's `agent_key` is not a usable public key: %w", m.Name, err)
	}
	for _, k := range held {
		if parsed, _, _, _, err := ssh.ParseAuthorizedKey([]byte(k.PublicKey)); err == nil &&
			bytes.Equal(parsed.Marshal(), want.Marshal()) {
			return nil
		}
	}
	return fmt.Errorf(
		"machine %q logs in with the agent key %s, and the SSH agent does not currently hold it: "+
			"unlock the password manager it lives in, or check SSH_AUTH_SOCK points at the right agent",
		m.Name, ssh.FingerprintSHA256(want))
}

func newWorkspacesRmCmd(opts *options) *cobra.Command {
	var yes bool

	c := &cobra.Command{
		Use:   "rm <name>",
		Short: "Forget a workspace, leaving its account on the machine",
		Long: "Takes the workspace out of the configuration. The account on the machine, " +
			"its home and its files stay on the machine.\n\n" +
			"Deleting somebody's home is not something a configuration edit " +
			"should do, and `sync` could not put it back.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, _, err := config.Dir(opts.configDir)
			if err != nil {
				return err
			}
			cfg, err := loadConfig(opts)
			if err != nil {
				return err
			}
			w, err := cfg.Workspace(args[0])
			if err != nil {
				return err
			}
			for _, r := range w.Routes {
				if r.Via != "" {
					return fmt.Errorf("%s publishes %s through %s, which would keep serving it with nothing left to "+
						"take it off: `devmachine expose rm %s` first", w.Name, r.Host, r.Via, r.Host)
				}
			}

			if !yes {
				ok, err := confirm(cmd.InOrStdin(), cmd.OutOrStdout(),
					"Forget the workspace "+w.Name+"? Its account and home stay on the machine.")
				if err != nil {
					return err
				}
				if !ok {
					return errDeclined
				}
			}
			if err := config.RemoveWorkspace(dir, w.Name); err != nil {
				return err
			}
			repo.AutoCommit(cmd.Context(), dir, "chore(config): remove workspace "+w.Name)

			cmd.Printf("%s is out of the configuration.\n", w.Name)
			cmd.Printf("The account %s, its home and its files are still on the machine %s.\n",
				w.LinuxUser(), machineNameOf(cfg, w))
			cmd.Println("Remove them there by hand if you really want them gone.")

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

// linuxUserName is what an account name looks like: something that goes
// into a shell command unquoted-safe as a bare word.
var linuxUserName = regexp.MustCompile(`^[a-z_][a-z0-9_-]*$`)

func newWorkspacesDestroyCmd(opts *options) *cobra.Command {
	var confirmName string
	var check, keepDNS bool

	c := &cobra.Command{
		Use:   "destroy <name>",
		Short: "Delete a workspace's account, home and configuration",
		Long: "This is the only command that deletes a home: its account on the machine, " +
			"everything under that account's home directory, its Caddy routes " +
			"and its entry in the configuration. It asks for the workspace's " +
			"name typed again, because there is no undo.\n\n" +
			"The DNS record of each of its sites goes too, the way `expose rm` " +
			"removes one: only while it still points at the machine that " +
			"serves the site, and printed to remove by hand when no provider " +
			"can do it. --keep-dns leaves every record alone.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, _, err := config.Dir(opts.configDir)
			if err != nil {
				return err
			}
			cfg, err := loadConfig(opts)
			if err != nil {
				return err
			}
			w, err := cfg.Workspace(args[0])
			if err != nil {
				return err
			}
			machine, err := cfg.Machine(machineNameOf(cfg, w))
			if err != nil {
				return err
			}

			user := w.LinuxUser()
			admin := machine.User
			if admin == "" {
				admin = config.DefaultAdminUser
			}
			if user == config.DefaultAdminUser || user == admin {
				return fmt.Errorf("refusing to destroy %q: it is the machine's administrative account", w.Name)
			}
			if !linuxUserName.MatchString(user) {
				return fmt.Errorf("%q is not an account name this command will pass to a shell", user)
			}

			cmd.Printf("This deletes, on %s:\n", machine.Name)
			cmd.Printf("  the account %s and everything under %s\n", user, homeOf(user, observedSystem(dir, machine.Name)))
			for _, r := range w.Routes {
				if keepDNS {
					cmd.Printf("  https://%s, which will stop answering (its DNS record stays: --keep-dns)\n", r.Host)
					continue
				}
				cmd.Printf("  https://%s, which will stop answering, and its DNS record while it points at %s\n",
					r.Host, cfg.ServingMachine(w, r))
			}
			cmd.Printf("and removes %s from the configuration. None of it can be put back.\n", w.Name)

			if check {
				cmd.Printf("would destroy %s\n", w.Name)
				if keepDNS {
					keptSites(cmd.OutOrStdout(), cfg, w)
				} else {
					unpointSites(cmd.Context(), cmd.OutOrStdout(), dir, cfg, w, nil, true)
				}
				return nil
			}

			if confirmName != "" {
				if confirmName != w.Name {
					return fmt.Errorf("--confirm %q does not match the workspace %q", confirmName, w.Name)
				}
			} else {
				cmd.Print("Type the workspace name to confirm: ")
				line, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
				if err != nil && line == "" {
					return errDeclined
				}
				if strings.TrimSpace(line) != w.Name {
					return errDeclined
				}
			}

			client, _, err := dialAdmin(cmd.Context(), machine)
			if err != nil {
				return fmt.Errorf("destroying %s needs the machine: %w; `devmachine workspaces rm %s` "+
					"forgets it without touching the machine", w.Name, err, w.Name)
			}
			defer client.Close()

			var local []string
			for _, r := range w.Routes {
				if r.Via == "" {
					local = append(local, r.Host)
				}
			}
			sitesDir, sitesErr := caddySitesDir(cmd.Context(), dir, cfg, machine)
			if sitesErr != nil && len(local) > 0 {
				return fmt.Errorf("%s publishes %s, and the file that serves it cannot be found (%v): "+
					"destroying the account would leave Caddy serving it. `devmachine expose rm` each host "+
					"and `devmachine sync` first", w.Name, strings.Join(local, ", "), sitesErr)
			}
			elsewhere, err := servingClients(cmd.Context(), dir, cfg, w)
			for _, c := range elsewhere {
				defer c.client.Close()
			}
			if err != nil {
				return err
			}
			for _, c := range elsewhere {
				if _, err := c.client.Run(cmd.Context(), c.script); err != nil {
					return fmt.Errorf("taking %s's sites off %s: %w; nothing was destroyed, try again once %s answers",
						w.Name, c.machine, err, c.machine)
				}
			}

			script := "set -e\n" +
				"if id -u " + quoteForShell(user) + " >/dev/null 2>&1; then\n" +
				deleteAccountScript(user, observedSystem(dir, machine.Name)) +
				"  echo removed\n" +
				"else\n" +
				"  echo absent\n" +
				"fi"
			if sitesErr == nil {
				routesFile := path.Join(sitesDir, expose.WorkspaceFileName(w.Name))
				script += "\nrm -f " + quoteForShell(routesFile) + " && " + reloadCaddyScript()
			}

			out, err := client.Run(cmd.Context(), script)
			if err != nil {
				record(opts, target{machine: machine, workspace: w.Name}, "workspaces destroy "+w.Name, false)
				return fmt.Errorf("destroying %s on %s: %w", w.Name, machine.Name, err)
			}

			if err := config.RemoveWorkspace(dir, w.Name); err != nil {
				return err
			}
			record(opts, target{machine: machine, workspace: w.Name}, "workspaces destroy "+w.Name, true)
			repo.AutoCommit(cmd.Context(), dir, "chore(config): destroy workspace "+w.Name)

			if strings.Contains(out, "absent") {
				cmd.Printf("%s had no account on %s; it is out of the configuration.\n", w.Name, machine.Name)
			} else {
				cmd.Printf("%s is destroyed: the account, its home and its configuration are gone.\n", w.Name)
			}
			clients := map[string]remote.Client{machine.Name: client}
			for _, c := range elsewhere {
				clients[c.machine] = c.client
			}
			if keepDNS {
				keptSites(cmd.OutOrStdout(), cfg, w)
			} else {
				unpointSites(cmd.Context(), cmd.OutOrStdout(), dir, cfg, w, clients, false)
			}

			updated, err := config.Load(dir)
			if err != nil {
				return err
			}
			return refreshAliases(updated, cmd.OutOrStdout())
		},
	}
	c.Flags().StringVar(&confirmName, "confirm", "", "the workspace name, to skip the interactive prompt")
	c.Flags().BoolVar(&check, "check", false, "say what would be destroyed, and destroy nothing")
	c.Flags().BoolVar(&keepDNS, "keep-dns", false, "leave the DNS record of each of its sites alone")
	return c
}

// keptSites says, for each site of w, how to remove the DNS record
// --keep-dns left, on the machine that serves it.
func keptSites(out io.Writer, cfg config.Config, w config.Workspace) {
	for _, r := range w.Routes {
		m, err := cfg.Machine(cfg.ServingMachine(w, r))
		if err != nil {
			continue
		}
		if note := dnsLeftNote(m, r.Host, "--keep-dns"); note != "" {
			fmt.Fprintln(out, note)
		}
	}
}

func newWorkspacesEditCmd(opts *options) *cobra.Command {
	var e workspaceEditOptions

	c := &cobra.Command{
		Use:   "edit <name>",
		Short: "Change a workspace's machine, account, packages or settings",
		Long: "It edits the configuration and touches no machine. `devmachine " +
			"sync` is what applies the change.\n\n" +
			"`--set <package>.<name>=<value>` writes into the workspace's " +
			"`settings:`, which is how a package's variables are set. An empty " +
			"value, or `--unset <package>.<name>`, takes the setting out again.\n\n" +
			"`--share <credential>=own` keeps this workspace's own login instead " +
			"of the one shared across the machine, which is how one workspace " +
			"signs in to a different account. `=machine` puts it back, and an " +
			"empty value falls back to whatever the configuration says.\n\n" +
			"Changing the machine looks like moving a workspace and is not: the " +
			"next sync creates the account on the new machine, and the old one " +
			"keeps everything it had.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			e.machine = opts.machine
			return runWorkspaceEdit(cmd, opts, args[0], e)
		},
	}
	c.Flags().StringVar(&e.user, "user", "", "the account on the machine this workspace owns")
	c.Flags().StringSliceVar(&e.add, "add", nil, "a package to add")
	c.Flags().StringSliceVar(&e.remove, "rm", nil, "a package to take off")
	c.Flags().StringArrayVar(&e.set, "set", nil, "a setting, as <package>.<name>=<value>")
	c.Flags().StringArrayVar(&e.unset, "unset", nil, "a setting to take out, as <package>.<name>")
	c.Flags().StringArrayVar(&e.share, "share", nil,
		"whether a login is shared, as <credential>=machine|own")
	c.Flags().BoolVar(&e.check, "check", false, "say what would change, and change nothing")
	c.Flags().BoolVar(&e.yes, "yes", false, "do not ask")
	return c
}

type workspaceEditOptions struct {
	// machine comes from the global --machine, so one flag means the same
	// thing everywhere: which machine this acts on.
	machine string
	user    string
	add     []string
	remove  []string
	set     []string
	unset   []string
	share   []string
	check   bool
	yes     bool
}

func runWorkspaceEdit(cmd *cobra.Command, opts *options, name string, e workspaceEditOptions) error {
	dir, _, err := config.Dir(opts.configDir)
	if err != nil {
		return err
	}
	cfg, err := loadConfig(opts)
	if err != nil {
		return err
	}
	w, err := cfg.Workspace(name)
	if err != nil {
		return err
	}
	was := machineNameOf(cfg, w)

	for _, p := range e.remove {
		if err := refuseRemovingAccount(p); err != nil {
			return err
		}
	}
	changes, err := applyWorkspaceEdit(cfg, &w, e)
	if err != nil {
		return err
	}
	if len(changes) == 0 {
		return fmt.Errorf(
			"nothing to change on %q: pass --machine, --user, --add, --rm, --set, --unset or --share", name)
	}

	// Validating the whole configuration is what catches a setting for a
	// package the workspace does not install, which would otherwise reach
	// nothing and leave the recipe on its default.
	edited := cfg
	edited.Workspaces = slices.Clone(cfg.Workspaces)
	for i := range edited.Workspaces {
		if edited.Workspaces[i].Name == name {
			edited.Workspaces[i] = w
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

	if err := config.UpdateWorkspace(dir, w); err != nil {
		return err
	}
	repo.AutoCommit(cmd.Context(), dir, "chore(config): update workspace "+name)
	for _, line := range changes {
		cmd.Printf("%s: %s\n", name, line)
	}
	if e.machine != "" && e.machine != was {
		cmd.Printf("This moves nothing. The next `devmachine sync` creates the account on %s, "+
			"and %s keeps everything it had.\n", e.machine, was)
	}
	cmd.Println("The machine is untouched until the next `devmachine sync`.")

	if e.machine != "" && e.machine != was {
		updated, err := config.Load(dir)
		if err != nil {
			return err
		}
		return refreshAliases(updated, cmd.OutOrStdout())
	}
	return nil
}

// applyWorkspaceEdit puts the flags onto the workspace and returns what each
// one changed, in the words the confirmation and the result both use.
func applyWorkspaceEdit(cfg config.Config, w *config.Workspace, e workspaceEditOptions) ([]string, error) {
	var changes []string

	if e.machine != "" {
		if _, err := cfg.Machine(e.machine); err != nil {
			return nil, err
		}
		if e.machine != machineNameOf(cfg, *w) {
			changes = append(changes, "move to the machine "+e.machine)
			w.Machine = e.machine
		}
	}
	if e.user != "" && e.user != w.User {
		changes = append(changes, "use the account "+e.user)
		w.User = e.user
	}

	for _, name := range e.remove {
		if slices.Contains(e.add, name) {
			return nil, fmt.Errorf("--add %s and --rm %s say two different things: pass one of them", name, name)
		}
	}
	packages := slices.Clone(w.Packages)
	for _, name := range e.add {
		if slices.Contains(packages, name) {
			continue
		}
		packages = append(packages, name)
		changes = append(changes, "add the package "+name)
	}
	for _, name := range e.remove {
		if !slices.Contains(packages, name) {
			continue
		}
		packages = slices.DeleteFunc(packages, func(n string) bool { return n == name })
		changes = append(changes, "take off the package "+name)
	}
	w.Packages = packages

	shared := maps.Clone(w.Credentials)
	if shared == nil {
		shared = map[string]string{}
	}
	for _, raw := range e.share {
		name, choice, ok := strings.Cut(raw, "=")
		if !ok || name == "" {
			return nil, fmt.Errorf("--share %q is not a choice: write it as <credential>=machine|own", raw)
		}
		switch choice {
		case "":
			if _, held := shared[name]; held {
				delete(shared, name)
				changes = append(changes, "stop deciding about the login "+name)
			}
		case config.CredentialMachine, config.CredentialOwn:
			if shared[name] != choice {
				shared[name] = choice
				changes = append(changes, fmt.Sprintf("take the login %s as %s", name, choice))
			}
		default:
			return nil, fmt.Errorf("--share %s=%s: a login is %q or %q",
				name, choice, config.CredentialMachine, config.CredentialOwn)
		}
	}
	if len(shared) == 0 {
		shared = nil
	}
	w.Credentials = shared

	settings, edits, err := editSettings(w.Settings, e.set, e.unset)
	if err != nil {
		return nil, err
	}
	w.Settings = settings

	return append(changes, edits...), nil
}

// editSettings applies --set and --unset to a copy of settings and returns
// it with what each one changed. A machine and a workspace hold settings the
// same way, so both edit commands read the flags here.
func editSettings(current map[string]any, set, unset []string) (map[string]any, []string, error) {
	var changes []string
	settings := maps.Clone(current)
	if settings == nil {
		settings = map[string]any{}
	}
	remove := func(key string) {
		if _, held := settings[key]; held {
			delete(settings, key)
			changes = append(changes, "take out the setting "+key)
		}
	}
	for _, raw := range set {
		key, text, ok := strings.Cut(raw, "=")
		if !ok || key == "" {
			return nil, nil, fmt.Errorf("--set %q is not a setting: write it as <package>.<name>=<value>", raw)
		}
		if slices.Contains(unset, key) {
			return nil, nil, fmt.Errorf("--set %s and --unset %s say two different things: pass one of them", key, key)
		}
		if text == "" {
			remove(key)
			continue
		}
		settings[key] = settingValue(text)
		changes = append(changes, "set "+key+" to "+text)
	}
	for _, key := range unset {
		if key == "" || strings.Contains(key, "=") {
			return nil, nil, fmt.Errorf("--unset %q is not a setting name: write it as <package>.<name>", key)
		}
		remove(key)
	}
	if len(settings) == 0 {
		settings = nil
	}
	return settings, changes, nil
}

// settingValue reads a value the way the file that holds it would.
//
// The setting lands in YAML and is handed to a recipe, so a package declaring
// a list or a flag has to be settable from the command line. Anything that is
// not valid YAML is taken as the plain string it looks like.
func settingValue(text string) any {
	var value any
	if err := yaml.Unmarshal([]byte(text), &value); err != nil || value == nil {
		return text
	}
	return value
}

// servingClient is a machine that serves a workspace's sites through `via`,
// and the script that takes them off it.
type servingClient struct {
	machine string
	client  remote.Client
	script  string
}

// servingClients reaches every machine that serves the workspace's sites with
// `via`, before anything is destroyed: once the workspace is out of the
// configuration, nothing would remove its file there again.
func servingClients(ctx context.Context, dir string, cfg config.Config, w config.Workspace) ([]servingClient, error) {
	var out []servingClient
	seen := map[string]bool{}
	for _, r := range w.Routes {
		if r.Via == "" || seen[r.Via] {
			continue
		}
		seen[r.Via] = true
		m, err := cfg.Machine(r.Via)
		if err != nil {
			return out, err
		}
		sitesDir, err := caddySitesDir(ctx, dir, cfg, m)
		if err != nil {
			return out, fmt.Errorf("%s publishes %s through %s, whose Caddy cannot be found (%v): "+
				"`devmachine expose rm %s` first", w.Name, r.Host, m.Name, err, r.Host)
		}
		client, _, err := dialAdmin(ctx, m)
		if err != nil {
			return out, fmt.Errorf("%s publishes %s through %s, which could not be reached (%v): destroying the "+
				"account would leave Caddy there serving it. `devmachine expose rm %s` once %s answers, or try again",
				w.Name, r.Host, m.Name, err, r.Host, m.Name)
		}
		routesFile := path.Join(sitesDir, expose.WorkspaceFileName(w.Name))
		out = append(out, servingClient{machine: m.Name, client: client,
			script: "rm -f " + quoteForShell(routesFile) + " && " + reloadCaddyScript()})
	}
	return out, nil
}

// reloadCaddyScript reloads Caddy through systemd where there is one, and
// through Caddy's own admin endpoint where there is not, as on a Mac. A Caddy
// that answers neither does not stop what came before it.
func reloadCaddyScript() string {
	return "(systemctl reload caddy 2>/dev/null || caddy reload --config " + quoteForShell(expose.Caddyfile) +
		" --adapter caddyfile 2>/dev/null || true)"
}

// removeOldSiteCommand is what a person runs to remove a site file by hand,
// reloading Caddy the way reloadCaddyScript does.
func removeOldSiteCommand(file string) string {
	return "devmachine run 'rm " + file + " && (systemctl reload caddy || caddy reload --config " +
		expose.Caddyfile + " --adapter caddyfile)'"
}

// deleteAccountScript deletes an account, its home and its processes on a
// machine running system, as facts name it.
func deleteAccountScript(user, system string) string {
	quotedUser := quoteForShell(user)
	if system == "Darwin" {
		return "  pkill -KILL -u " + quotedUser + " 2>/dev/null || true\n" +
			"  if dscl . -read /Groups/" + remote.MacRemoteLoginGroup + " >/dev/null 2>&1; then\n" +
			"    dseditgroup -o edit -d " + quotedUser + " -t user " + remote.MacRemoteLoginGroup + " 2>/dev/null || true\n" +
			"  fi\n" +
			"  sysadminctl -deleteUser " + quotedUser + "\n"
	}
	// claude-remote-control enables linger for the account, keeping a
	// user manager (and its processes) alive; userdel refuses a user
	// with running processes, so linger goes off and everything of
	// theirs is killed first.
	return "  loginctl disable-linger " + quotedUser + " 2>/dev/null || true\n" +
		"  loginctl terminate-user " + quotedUser + " 2>/dev/null || true\n" +
		"  pkill -KILL -u " + quotedUser + " 2>/dev/null || true\n" +
		"  userdel --force --remove " + quotedUser + "\n"
}

// homeOf is where an account's home is on a machine running system, as
// facts name it: /Users on a Mac, /home anywhere else.
func homeOf(user, system string) string {
	if system == "Darwin" {
		return "/Users/" + user
	}
	return "/home/" + user
}

// observedSystem is the system last read from a machine, empty when it was
// never read.
func observedSystem(dir, machine string) string {
	f, found, err := facts.Load(dir, machine)
	if err != nil || !found {
		return ""
	}
	return f.System
}
