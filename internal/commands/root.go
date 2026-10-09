// Package commands is the CLI's command tree.
package commands

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var version = "dev"

// SetVersion records the version the binary was built with.
func SetVersion(v string) { version = v }

// options are the flags every command shares.
type options struct {
	configDir string
	format    string
	// machine names which machine a machine-level command acts on. Empty
	// means the only configured one; with several it is an error, not a guess.
	machine string
}

// Execute runs the CLI and turns an error into an exit code.
//
// Nothing else in the package calls os.Exit: a command reports a problem by
// returning an error, and this is the single place that decides what that
// costs.
func Execute() {
	err := NewRootCmd().Execute()
	if err == nil {
		return
	}
	if line, ok := errorLine(err); ok {
		fmt.Fprintln(os.Stderr, line)
	}
	os.Exit(exitCode(err))
}

// exitError is a failure that asks for its own exit code instead of 1. A
// quiet one already spoke for itself: run passing on a command's own failure
// adds no line under what that command wrote.
type exitError struct {
	code  int
	err   error
	quiet bool
}

func (e *exitError) Error() string { return e.err.Error() }
func (e *exitError) Unwrap() error { return e.err }

// errorLine is what Execute prints for err, unless err is quiet.
func errorLine(err error) (string, bool) {
	var coded *exitError
	if errors.As(err, &coded) && coded.quiet {
		return "", false
	}
	return "error: " + err.Error(), true
}

// exitCode is what a failure costs: 1, unless the command chose another.
func exitCode(err error) int {
	var coded *exitError
	if errors.As(err, &coded) {
		return coded.code
	}
	return 1
}

// NewRootCmd builds the command tree. It takes no global state so a test can
// build a fresh tree per case.
func NewRootCmd() *cobra.Command {
	opts := &options{}

	root := &cobra.Command{
		Use:   "devmachine",
		Short: "Operate a personal development VPS",
		// The CLI prints its own errors in Execute, and a usage dump on a
		// runtime failure buries the line that matters.
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return validateFormat(opts.format)
		},
		PersistentPostRun: func(cmd *cobra.Command, args []string) {
			showUpdateHint(cmd, opts)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	// cobra's Print writes to stderr unless told otherwise, and a program
	// reading an answer reads stdout.
	root.SetOut(os.Stdout)

	root.PersistentFlags().StringVar(&opts.configDir, "config", "",
		"configuration directory (default: $DEVMACHINE_CONFIG, then $XDG_CONFIG_HOME/devmachine, then ~/.config/devmachine)")
	root.PersistentFlags().StringVar(&opts.format, "format", formatTable,
		"output format: table or json")
	root.PersistentFlags().StringVar(&opts.machine, "machine", "",
		"which machine to act on (default: the only one configured)")

	root.AddCommand(
		newVersionCmd(),
		newSetupCmd(opts),
		newConfigCmd(opts),
		newMachinesCmd(opts),
		newWorkspacesCmd(opts),
		newAliasesCmd(opts),
		newSecretsCmd(opts),
		newDNSCmd(opts),
		newExposeCmd(opts),
		newTunnelCmd(opts),
		newMachineCmd(opts),
		newHelpJSONCmd(opts),
		newDoctorCmd(opts),
		newStatsCmd(opts),
		newSSHCmd(opts),
		newMoshCmd(opts),
		newRunCmd(opts),
		newUploadCmd(opts),
		newDownloadCmd(opts),
		newPackagesCmd(opts),
		newSkillsCmd(opts),
		newWidgetsCmd(opts),
		newSyncCmd(opts),
		newUpdateCmd(opts),
		newCredentialsCmd(opts),
		newLoginCmd(opts),
		newResolveCmd(opts),
		newSessionsCmd(opts),
		newSSHProxyCmd(opts),
	)
	return root
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.Println(version)
			return nil
		},
	}
}
