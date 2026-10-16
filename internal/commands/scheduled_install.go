package commands

import (
	"fmt"

	"github.com/mydevmachine/devmachine/internal/config"
	"github.com/spf13/cobra"
)

var (
	editPackageFn = editPackage
	runSyncFn     = runSync
)

// installScheduled sets up the scheduled package on w the moment the first
// task needs it: it adds the package to the workspace and syncs its machine,
// so creating a task never leaves a step for later.
func installScheduled(cmd *cobra.Command, opts *options, cfg config.Config, w config.Workspace, yes bool) (config.Config, config.Workspace, error) {
	machine := machineNameOf(cfg, w)
	if !yes {
		ok, err := confirm(cmd.InOrStdin(), cmd.ErrOrStderr(), fmt.Sprintf(
			"Scheduled is not set up on %s. Set it up now? It adds the scheduled package and syncs %s.", w.Name, machine))
		if err != nil {
			return cfg, w, err
		}
		if !ok {
			return cfg, w, scheduledNotInstalled(cfg, w)
		}
	}

	// The package edit and the sync print their own progress; the answer on
	// stdout is still only the created task.
	out := cmd.OutOrStdout()
	cmd.SetOut(cmd.ErrOrStderr())
	defer cmd.SetOut(out)
	o := *opts
	o.format, o.machine = formatTable, machine
	if err := editPackageFn(cmd, &o, scheduledPackage, w.Name, true, false, true); err != nil {
		return cfg, w, fmt.Errorf("adding the scheduled package to %s: %w", w.Name, err)
	}
	if err := runSyncFn(cmd, &o, false, true, []string{scheduledPackage}); err != nil {
		return cfg, w, fmt.Errorf("setting up scheduled on %s: %w", machine, err)
	}
	cfg, err := loadConfig(opts)
	if err != nil {
		return cfg, w, err
	}
	w, err = cfg.Workspace(w.Name)
	return cfg, w, err
}
