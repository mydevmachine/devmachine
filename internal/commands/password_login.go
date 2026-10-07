package commands

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/mydevmachine/devmachine/internal/config"
	"github.com/mydevmachine/devmachine/internal/remote"
	"github.com/mydevmachine/devmachine/internal/repo"
	"github.com/spf13/cobra"
)

// passwordLogin is what bootstrap does about SSH password login.
type passwordLogin struct {
	// leaveOn is --no-harden: nothing about passwords changes.
	leaveOn bool
	// keep are the accounts --keep-password-login named. Set, nothing is
	// asked.
	keep []string
	// ask asks which accounts keep a password, given the people on the
	// machine. Nil asks nothing: every account loses it.
	ask func(people []string, mac bool) (keep []string, leaveOn bool, err error)
}

func addKeepPasswordLoginFlag(c *cobra.Command, s *setupOptions) {
	c.Flags().StringSliceVar(&s.keepPasswordLogin, "keep-password-login", nil,
		"accounts that keep SSH password login when it goes off for everybody else, separated by commas")
}

// checkKeepPasswordLogin refuses flags that contradict each other before the
// machine is touched.
func checkKeepPasswordLogin(opts setupOptions) error {
	if len(opts.keepPasswordLogin) > 0 && opts.noHarden {
		return errors.New("--keep-password-login names accounts that keep their password when it goes off " +
			"for everybody else, and --no-harden leaves it on for all: pick one")
	}
	for _, name := range opts.keepPasswordLogin {
		if err := remote.ValidAccountName(name); err != nil {
			return fmt.Errorf("--keep-password-login: %w", err)
		}
	}
	return nil
}

func passwordLoginFor(opts setupOptions, r *bufio.Reader, out io.Writer) passwordLogin {
	return passwordLogin{
		leaveOn: opts.noHarden,
		keep:    opts.keepPasswordLogin,
		ask: func(people []string, mac bool) ([]string, bool, error) {
			return askPasswordLogin(r, out, people, mac)
		},
	}
}

// decide settles which accounts keep their password, asking when it may.
func (l passwordLogin) decide(ctx context.Context, client remote.Client, system remote.System,
	admin string) (keep []string, leaveOn bool, err error) {
	if len(l.keep) > 0 || l.ask == nil {
		return l.keep, false, nil
	}
	accounts, err := humanAccounts(ctx, client, system)
	if err != nil {
		return nil, false, err
	}
	var people []string
	for _, name := range accounts {
		if name != admin {
			people = append(people, name)
		}
	}
	return l.ask(people, system.MacOS())
}

// askPasswordLogin is the wizard's question. Turning password login off is
// the default, and on a Mac the question says out loud that it is every
// account on it — a Mac somebody uses has people who log in with a password.
func askPasswordLogin(r *bufio.Reader, out io.Writer, people []string, mac bool) ([]string, bool, error) {
	fmt.Fprintf(out, "\nSSH password login, now that the key is proved:\n")
	if mac {
		fmt.Fprintf(out, "On a Mac this is every account on it. Whoever logs in over SSH with a password "+
			"needs a key afterwards; the login window, sudo and Screen Sharing keep their passwords.\n")
	}
	if len(people) > 0 {
		fmt.Fprintf(out, "Other accounts on this machine: %s.\n", strings.Join(people, ", "))
	}
	fmt.Fprintf(out, "  1) off for every account\n  2) off, except for accounts you name\n  3) leave it as it is\n")
	answer, err := ask(r, out, "choice", "1")
	if err != nil {
		return nil, false, err
	}
	switch answer {
	case "1":
		return nil, false, nil
	case "3":
		return nil, true, nil
	case "2":
	default:
		return nil, false, fmt.Errorf("%q is not 1, 2 or 3", answer)
	}

	named, err := ask(r, out, "accounts that keep password login, separated by commas", strings.Join(people, ","))
	if err != nil {
		return nil, false, err
	}
	keep := splitAccounts(named)
	if len(keep) == 0 {
		return nil, false, errors.New("no account was named: run it again and pick 1 to turn password login off for everybody")
	}
	for _, name := range keep {
		if err := remote.ValidAccountName(name); err != nil {
			return nil, false, err
		}
	}
	return keep, false, nil
}

func splitAccounts(list string) []string {
	var names []string
	for _, name := range strings.Split(list, ",") {
		if name = strings.TrimSpace(name); name != "" && !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	return names
}

type passwordLoginOptions struct {
	off, on bool
	keep    []string
	check   bool
	yes     bool
}

type passwordLoginReport struct {
	Machine string `json:"machine"`
	remote.PasswordLoginState
	// Keep is what the configuration says, which ssh_hardening keeps.
	Keep []string `json:"keep"`
}

func newMachinesPasswordLoginCmd(opts *options) *cobra.Command {
	var flags passwordLoginOptions
	c := &cobra.Command{
		Use:   "password-login <name>",
		Short: "Show or change which accounts log in over SSH with a password",
		Long: "With no flag, asks the machine's sshd, account by account, who can log in with a password.\n\n" +
			"--off turns password login off for every account; --keep a,b turns it off for every account " +
			"except those; --on takes the CLI's drop-in away, so the system's own setting applies again " +
			"(password login on, on macOS, Debian and Ubuntu).\n\n" +
			"The change is made over the admin login's key, which is the proof that a way in stays open. " +
			"sshd checks the new file before it is used, and a file it refuses is put back as it was.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, _, err := config.Dir(opts.configDir)
			if err != nil {
				return err
			}
			return runMachinesPasswordLogin(cmd.Context(), dir, args[0], cmd.InOrStdin(), cmd.OutOrStdout(),
				opts.format, flags)
		},
	}
	c.Flags().BoolVar(&flags.off, "off", false, "turn password login off for every account")
	c.Flags().BoolVar(&flags.on, "on", false, "take the CLI's drop-in away, so the system's own setting applies")
	c.Flags().StringSliceVar(&flags.keep, "keep", nil,
		"turn password login off for every account except these, separated by commas")
	c.Flags().BoolVar(&flags.check, "check", false, "say what would change, and change nothing")
	c.Flags().BoolVar(&flags.yes, "yes", false, "change it without asking")
	return c
}

func (f passwordLoginOptions) changes() (bool, error) {
	chosen := 0
	for _, set := range []bool{f.off, f.on, len(f.keep) > 0} {
		if set {
			chosen++
		}
	}
	if chosen > 1 {
		return false, errors.New("--off, --on and --keep each say a different thing: pick one")
	}
	for _, name := range f.keep {
		if err := remote.ValidAccountName(name); err != nil {
			return false, fmt.Errorf("--keep: %w", err)
		}
	}
	return chosen == 1, nil
}

func (f passwordLoginOptions) describe(machine string) string {
	switch {
	case f.on:
		return fmt.Sprintf("leave SSH password login on %s to the system's own setting", machine)
	case len(f.keep) > 0:
		return fmt.Sprintf("turn SSH password login off on %s for every account except %s", machine, joinAnd(f.keep))
	default:
		return fmt.Sprintf("turn SSH password login off on %s for every account", machine)
	}
}

func runMachinesPasswordLogin(ctx context.Context, dir, name string, in io.Reader, out io.Writer, format string,
	flags passwordLoginOptions) error {
	change, err := flags.changes()
	if err != nil {
		return err
	}
	cfg, err := config.Load(dir)
	if err != nil {
		return err
	}
	m, err := cfg.Machine(name)
	if err != nil {
		return err
	}
	if err := requiresAddress(m, "machines password-login"); err != nil {
		return err
	}
	if flags.on && slices.Contains(m.Packages, "ssh_hardening") {
		return fmt.Errorf("machine %q has the ssh_hardening package, which turns password login off again on every sync: "+
			"remove it first (`devmachine packages rm ssh_hardening --machine %s`), or use --keep to leave some accounts their password",
			m.Name, m.Name)
	}

	client, _, err := dial(ctx, m, m.User)
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()
	system, err := detectSystem(ctx, client)
	if err != nil {
		return err
	}

	if change {
		if flags.check {
			fmt.Fprintf(out, "would %s; nothing was changed\n", flags.describe(m.Name))
			return nil
		}
		if !flags.yes {
			question := strings.ToUpper(flags.describe(m.Name)[:1]) + flags.describe(m.Name)[1:] + "?"
			if system.MacOS() && !flags.on {
				fmt.Fprintf(out, "On a Mac this is every account on it: whoever logs in over SSH with a password "+
					"and is not kept needs a key afterwards.\n")
			}
			ok, err := confirm(in, out, question)
			if err != nil {
				return err
			}
			if !ok {
				return errDeclined
			}
		}
		keep := flags.keep
		if flags.on {
			keep = nil
			err = passwordLoginOn(ctx, client, system)
		} else {
			err = harden(ctx, client, system, keep)
		}
		if err != nil {
			return err
		}
		if err := config.SetMachinePasswordLoginKeep(dir, m.Name, keep); err != nil {
			return err
		}
		m.PasswordLoginKeep = keep
		repo.AutoCommit(ctx, dir, "chore(config): password login on machine "+m.Name)
	}

	state, err := readPasswordLogin(ctx, client, system)
	if err != nil {
		return err
	}
	report := passwordLoginReport{Machine: m.Name, PasswordLoginState: state, Keep: m.PasswordLoginKeep}
	if report.Keep == nil {
		report.Keep = []string{}
	}
	if format == formatJSON {
		return writeJSON(out, report)
	}
	printPasswordLogin(out, report)
	return nil
}

var (
	passwordLoginOn   = remote.PasswordLoginOn
	readPasswordLogin = remote.ReadPasswordLogin
)

func printPasswordLogin(out io.Writer, r passwordLoginReport) {
	state := "off"
	if r.Default {
		state = "on"
	}
	fmt.Fprintf(out, "%s: SSH password login is %s by default", r.Machine, state)
	if !r.DropIn {
		fmt.Fprintf(out, " (the system's own setting; the CLI's drop-in is not there)")
	}
	fmt.Fprintln(out)
	for _, a := range r.Accounts {
		answer := "key only"
		if a.Allowed {
			answer = "password or key"
		}
		fmt.Fprintf(out, "  %s: %s\n", a.User, answer)
	}
}
