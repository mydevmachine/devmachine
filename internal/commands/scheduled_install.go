package commands

import (
	"context"
	"fmt"

	"github.com/mydevmachine/devmachine/internal/config"
	"github.com/spf13/cobra"
)

var (
	editPackageFn = editPackage
	runSyncFn     = runSync
)

// scheduledPackageAvailable says whether the packages this configuration
// syncs from hold the scheduled package. Adding a package sync cannot find
// would leave the workspace with one every later sync refuses.
var scheduledPackageAvailable = func(ctx context.Context, opts *options, cfg config.Config) (bool, error) {
	dir, _, err := config.Dir(opts.configDir)
	if err != nil {
		return false, err
	}
	store, err := openStore(ctx, dir, cfg.Packages)
	if err != nil {
		return false, err
	}
	return store.Has(scheduledPackage), nil
}

// installScheduled sets up the scheduled package on w the moment the first
// task needs it: it adds the package to the workspace and syncs its machine,
// so creating a task never leaves a step for later. It also finishes a setup
// whose sync failed, which left the package on the workspace and no binary.
func installScheduled(cmd *cobra.Command, opts *options, cfg config.Config, w config.Workspace, yes bool) (config.Config, config.Workspace, error) {
	machine := machineNameOf(cfg, w)
	available, err := scheduledPackageAvailable(cmd.Context(), opts, cfg)
	if err != nil {
		return cfg, w, err
	}
	if !available {
		return cfg, w, &scheduledError{Kind: "not-available", Message: fmt.Sprintf(
			"the scheduled package is not in packages release %q that this configuration uses: "+
				"run devmachine update once a release with it is out, then try again", cfg.Packages)}
	}
	finishing := isScheduledInstalled(w)
	if !yes {
		question := fmt.Sprintf("Scheduled is not set up on %s. Set it up now? It adds the scheduled package and syncs %s.",
			w.Name, machine)
		if finishing {
			question = fmt.Sprintf("Scheduled's setup on %s did not finish. Finish it now? It syncs %s.", w.Name, machine)
		}
		ok, err := confirm(cmd.InOrStdin(), cmd.ErrOrStderr(), question)
		if err != nil {
			return cfg, w, err
		}
		if !ok {
			return cfg, w, scheduledMissing(cfg, w)
		}
	}

	// The package edit and the sync print their own progress; the answer on
	// stdout is still only the created task.
	out := cmd.OutOrStdout()
	cmd.SetOut(cmd.ErrOrStderr())
	defer cmd.SetOut(out)
	edit := *opts
	edit.format, edit.machine = formatTable, ""
	if err := editPackageFn(cmd, &edit, scheduledPackage, w.Name, true, false, true); err != nil {
		return cfg, w, fmt.Errorf("adding the scheduled package to %s: %w", w.Name, err)
	}
	sync := edit
	sync.machine = machine
	if err := runSyncFn(cmd, &sync, false, true, []string{scheduledPackage}, ""); err != nil {
		return cfg, w, &scheduledError{Kind: "setup-failed", Message: fmt.Sprintf(
			"setting up scheduled on %s failed: %v. The scheduled package stays on %s: once the cause is fixed, "+
				"run devmachine sync --machine %s --tags scheduled, or add the task again",
			machine, err, w.Name, machine)}
	}
	cfg, err = loadConfig(opts)
	if err != nil {
		return cfg, w, err
	}
	w, err = cfg.Workspace(w.Name)
	return cfg, w, err
}
