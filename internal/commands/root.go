// Package commands is the CLI's command tree.
package commands

import (
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
	if err := NewRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
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
