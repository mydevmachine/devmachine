package commands

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/mydevmachine/devmachine/internal/config"
	"github.com/mydevmachine/devmachine/internal/facts"
	"github.com/spf13/cobra"
)

// machineShowJSON is one entry of `machines list` plus what was last read
// from the machine. Observed is left out for a machine never read.
type machineShowJSON struct {
	machineJSON
	Observed *facts.Facts `json:"observed,omitempty"`
}

func newMachinesShowCmd(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "show [name]",
		Short: "Show one machine, and what setup, sync or doctor last read from it",
		Long: "It prints the machine as the configuration describes it and, under " +
			"`observed`, the system, package manager, init system and paths " +
			"setup, sync or doctor last read from it. It does not connect: run " +
			"`devmachine doctor` to read the machine again.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := opts.machine
			if len(args) == 1 {
				name = args[0]
			}
			return showMachine(cmd.OutOrStdout(), opts, name)
		},
	}
}

func showMachine(out io.Writer, opts *options, name string) error {
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
	observed, found, err := facts.Load(dir, m.Name)
	if err != nil {
		return err
	}

	show := machineShowJSON{}
	for _, entry := range asJSON(cfg).Machines {
		if entry.Name == m.Name {
			show.machineJSON = entry
		}
	}
	if found {
		show.Observed = &observed
	}

	if opts.format == formatJSON {
		return writeJSON(out, show)
	}
	return printMachine(out, show)
}

// withMachinePath puts the machine's command path, as last observed, in front
// of PATH for one command. A machine never read, or one that needs no prefix,
// gets the command as it was written.
func withMachinePath(opts *options, m config.Machine, command string) string {
	dir, _, err := config.Dir(opts.configDir)
	if err != nil {
		return command
	}
	return inMachinePath(dir, m, command)
}

// inMachinePath is withMachinePath for a caller that already has the
// configuration directory.
func inMachinePath(dir string, m config.Machine, command string) string {
	observed, found, err := facts.Load(dir, m.Name)
	if err != nil || !found {
		return command
	}
	return withPathPrefix(observed.CommandPath(), command)
}

func withPathPrefix(prefix []string, command string) string {
	if len(prefix) == 0 {
		return command
	}
	return "PATH=" + quoteForShell(strings.Join(prefix, ":")) + `:"$PATH"; export PATH; ` + command
}

func printMachine(out io.Writer, m machineShowJSON) error {
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	row := func(label, value string) { fmt.Fprintf(w, "%s\t%s\n", label, value) }

	row("machine", m.Name)
	if !m.Self {
		row("addresses", orDash(strings.Join(m.Hosts, ", ")))
		row("port", orDash(portText(m.Port)))
		row("admin", m.AdminUser)
	}
	row("location", m.Location)
	row("workspaces", orNone(strings.Join(m.Workspaces, ", ")))
	row("packages", orNone(strings.Join(m.Packages, ", ")))

	f := m.Observed
	if f == nil {
		row("system", fmt.Sprintf("not read yet: run `devmachine doctor --machine %s`", m.Name))
	} else {
		row("system", f.String())
		row("package manager", orDash(f.PkgMgr))
		row("init system", orDash(f.ServiceMgr))
		row("ansible", orDash(f.AnsiblePlaybook))
		if len(f.PathPrefix) > 0 {
			row("path prefix", strings.Join(f.PathPrefix, ":"))
		}
		row("observed", f.ObservedAt.Local().Format(time.RFC3339))
	}
	if err := w.Flush(); err != nil {
		return fmt.Errorf("writing the machine: %w", err)
	}
	return nil
}
