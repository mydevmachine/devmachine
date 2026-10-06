package commands

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/mydevmachine/devmachine/internal/config"
	"github.com/mydevmachine/devmachine/internal/dns"
	"github.com/mydevmachine/devmachine/internal/history"
	"github.com/mydevmachine/devmachine/internal/provision"
	"github.com/mydevmachine/devmachine/internal/remote"
	"github.com/spf13/cobra"
)

// Seams, so a test can see what would be launched without launching it.
var (
	realExecCommand = execCommand
	runInteractive  = realExecCommand
	realLookPath    = exec.LookPath
	lookPath        = realLookPath
	dial            = remote.Dial
	// dialMux is what `run` uses instead of dial: it multiplexes over one
	// system ssh connection, kept open for ControlPersist, so a program that
	// polls `run` every few seconds does not pay for a new handshake each
	// time.
	dialMux = remote.DialMux
)

// execCommand runs a session-holding command such as ssh, mosh, or a tunnel.
//
// It kills the child on SIGINT/SIGTERM rather than relying only on the
// terminal's own signal delivery: `tunnel` is meant to close with the
// process that opened it, including when something stops devmachine itself
// rather than the person pressing Ctrl-C at the terminal, and an ssh left
// running after that holds the port forever.
func execCommand(name string, args ...string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return execCommandContext(ctx, name, args...)
}

// execCommandContext is execCommand with the signal handling lifted out, so a
// test can prove the child dies with the context instead of having to send
// itself a signal.
func execCommandContext(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		// Closed on purpose, by the same signal that would have ended an
		// interactive session anyway: not a failure worth reporting as one.
		return nil
	}
	return err
}

// loadConfig resolves the directory, reads the configuration and judges it.
// Every command that touches the machine starts here.
func loadConfig(opts *options) (config.Config, error) {
	dir, _, err := config.Dir(opts.configDir)
	if err != nil {
		return config.Config{}, err
	}
	cfg, err := config.Load(dir)
	if err != nil {
		return cfg, err
	}
	return cfg, cfg.Validate()
}

// loadConfigIfAny is loadConfig for a command that only reads: with no
// config.yml yet nothing is configured, which is an answer and not a failure.
// The bool says whether a configuration was found.
func loadConfigIfAny(opts *options) (config.Config, bool, error) {
	cfg, err := loadConfig(opts)
	if errors.Is(err, os.ErrNotExist) {
		return config.Config{}, false, nil
	}
	return cfg, err == nil, err
}

// record writes one line in the command log.
//
// It is called once the machine has been reached, so the log holds what was
// asked of a machine rather than every attempt to find one.
func record(opts *options, tgt target, command string, ok bool) {
	dir, _, err := config.Dir(opts.configDir)
	if err != nil {
		return
	}
	history.Append(dir, history.Entry{At: time.Now(), Target: tgt.label(), Command: command, OK: ok})
}

// requiresAddress refuses on a self machine, for a command that has to reach
// the machine over SSH: there is no address for the computer you are
// standing on.
func requiresAddress(m config.Machine, command string) error {
	if !m.Self {
		return nil
	}
	return fmt.Errorf("%s is your computer (self: true): `%s` needs a machine it reaches over SSH", m.Name, command)
}

// firstAddress is the address an interactive session should use. It is the
// same order Dial tries, without opening a connection first.
func firstAddress(m config.Machine, command string) (string, error) {
	if err := requiresAddress(m, command); err != nil {
		return "", err
	}
	addresses, err := remote.Resolve(m)
	if err != nil {
		return "", err
	}
	return addresses[0], nil
}

func newSSHCmd(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "ssh [workspace]",
		Short: "Open an interactive session in a workspace",
		Long: "Names a workspace to land in that environment, wherever it runs. " +
			"With no name it opens a session on the machine itself, as its admin.\n\n" +
			"The session runs through the system ssh, so the terminal, the agent " +
			"and tmux behave exactly as they do when you run ssh yourself.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return interactive(cmd.Context(), opts, "ssh", args)
		},
	}
}

func newMoshCmd(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "mosh [workspace]",
		Short: "Open an interactive session over mosh",
		Long: "mosh survives a link that drops or roams, where ssh gives a " +
			"broken pipe. It needs mosh on both sides.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return interactive(cmd.Context(), opts, "mosh", args)
		},
	}
}

func interactive(ctx context.Context, opts *options, binary string, args []string) error {
	var name string
	if len(args) == 1 {
		name = args[0]
	}
	tgt, err := workspaceTarget(opts, name)
	if err != nil {
		return err
	}
	address, err := firstAddress(tgt.machine, binary)
	if err != nil {
		return err
	}
	if _, err := lookPath(binary); err != nil {
		return fmt.Errorf("%s is not installed on this machine", binary)
	}

	m, user := tgt.machine, tgt.login()
	if err := verifySystemHost(ctx, m); err != nil {
		return err
	}

	var argv []string
	switch binary {
	case "mosh":
		// mosh takes the remote ssh command as one string, which is where the
		// port and the key have to go.
		argv = []string{"--ssh=" + strictSSHCommand(m), user + "@" + address}
	default:
		argv = strictSSHArgs(m)
		argv = append(argv, user+"@"+address)
	}
	return explainRemoteLogin(opts, tgt, runInteractive(binary, argv...))
}

// explainRemoteLogin points at Remote Login when ssh gives up (255) on a
// machine last seen as a Mac: with "Only these users", macOS refuses an
// account outside that list whatever the key, and ssh says only "Connection
// closed". On any other machine 255 has too many causes to name one.
func explainRemoteLogin(opts *options, tgt target, err error) error {
	var exit interface{ ExitCode() int }
	if tgt.workspace == "" || !errors.As(err, &exit) || exit.ExitCode() != 255 {
		return err
	}
	dir, _, dirErr := config.Dir(opts.configDir)
	if dirErr != nil || observedSystem(dir, tgt.machine.Name) != remote.KernelDarwin {
		return err
	}
	return fmt.Errorf("%w: the Mac's Remote Login may not allow %s; `devmachine doctor --machine %s` checks it",
		err, tgt.user, tgt.machine.Name)
}

func newRunCmd(opts *options) *cobra.Command {
	var workspace, pkg string

	c := &cobra.Command{
		Use:   "run <command>",
		Short: "Run one command, or reach a package's entrypoint",
		Long: "Runs as the machine's admin by default, or inside a workspace " +
			"with --workspace.\n\n" +
			"--package reaches an installed package's entrypoint instead of a " +
			"shell command: `devmachine run --package cloudflare -- zones`. " +
			"This is the generic door onto a machine — it removes the need to " +
			"know an address, an account, a port or a key, and it does not " +
			"limit what a shell can do. `commands:` in the package's manifest " +
			"is what limits, and only for a package that asked to be limited. " +
			"Add --workspace and the entrypoint runs as that workspace's own " +
			"account instead of the machine's admin — for a command that has " +
			"to read that account's own files or use its own logins.",
		Args: func(cmd *cobra.Command, args []string) error {
			if pkg != "" {
				return nil
			}
			return cobra.ExactArgs(1)(cmd, args)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if pkg != "" {
				return runPackage(cmd, opts, pkg, workspace, args)
			}

			tgt, err := workspaceTarget(opts, workspace)
			if err != nil {
				return err
			}
			client, _, err := dialMux(cmd.Context(), tgt.machine, tgt.user)
			if err != nil {
				return err
			}
			defer client.Close()

			out, err := client.Run(cmd.Context(), withMachinePath(opts, tgt.machine, args[0]))
			record(opts, tgt, args[0], err == nil)
			// The output of a command that failed is usually the explanation,
			// so it is printed before the error is reported.
			if out != "" {
				cmd.Print(out)
			}
			return explainHostKey(cmd.Context(), tgt.machine, err)
		},
	}
	c.Flags().StringVar(&workspace, "workspace", "", "run inside this workspace instead of as the machine's admin")
	c.Flags().StringVar(&pkg, "package", "",
		"reach this package's entrypoint instead of a shell command; the command follows a `--`")
	return c
}

// runPackage reaches an installed package's entrypoint directly: the generic
// door for whatever `dns`, `packages` and the rest have not grown a verb for
// yet.
//
// With no workspace it runs as the machine's admin. With one, it runs as
// that workspace's own account on the machine the workspace lives on —
// what a package needs to read that account's own files or use its own
// logins, such as a per-workspace GitHub login.
func runPackage(cmd *cobra.Command, opts *options, name, workspace string, args []string) error {
	dashAt := cmd.ArgsLenAtDash()
	if dashAt < 0 {
		return fmt.Errorf("say what to run after `--`, for example: devmachine run --package %s -- <command>", name)
	}
	pkgArgs := args[dashAt:]

	tgt, err := workspaceTarget(opts, workspace)
	if err != nil {
		return err
	}
	dir, _, err := config.Dir(opts.configDir)
	if err != nil {
		return err
	}
	client, _, err := dialMux(cmd.Context(), tgt.machine, tgt.user)
	if err != nil {
		return err
	}
	defer client.Close()
	if tgt.user == "" {
		// No workspace: the entrypoint runs over the admin's connection, and
		// reads what only root can, a credential under /etc/devmachine.
		client = elevate(client)
	}

	base, err := provision.Base(tgt.machine)
	if err != nil {
		return err
	}
	ext, err := dns.Any(dir, tgt.machine.Name, tgt.workspace, base, name, client)
	if err != nil {
		return err
	}

	var out bytes.Buffer
	callErr := ext.Call(cmd.Context(), pkgArgs, &out)
	record(opts, tgt, fmt.Sprintf("run --package %s -- %s", name, strings.Join(pkgArgs, " ")), callErr == nil)
	if out.Len() > 0 {
		cmd.Print(out.String())
	}
	return explainHostKey(cmd.Context(), tgt.machine, callErr)
}

// explainHostKey turns ssh's bare refusal into what changed and how to fix it.
// The check runs only after a refusal, so the connection `run` reuses between
// calls stays free of an extra handshake.
func explainHostKey(ctx context.Context, m config.Machine, err error) error {
	if !errors.Is(err, remote.ErrHostKeyRejected) {
		return err
	}
	if verr := verifySystemHost(ctx, m); verr != nil {
		return verr
	}
	return err
}

// elevate makes an admin connection run as root. A seam, because the test
// clients run commands on the developer's computer as a stand-in for a root
// admin, where sudo is neither there nor wanted.
var elevate = remote.Elevated

// dialAdmin connects as the machine's admin login for steps that need root,
// and runs every command through sudo when that login is not root.
func dialAdmin(ctx context.Context, m config.Machine) (remote.Client, string, error) {
	client, address, err := dial(ctx, m, "")
	if err != nil {
		return nil, "", err
	}
	return elevate(client), address, nil
}
