package commands

import (
	"context"

	"github.com/mydevmachine/devmachine/internal/config"
	"github.com/mydevmachine/devmachine/internal/repo"
	"github.com/spf13/cobra"
)

func newPackagesPinCmd(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "pin [release]",
		Short: "Pin the packages release every recipe is read from",
		Long: "Writes `packages: <release>` to config.yml. With no release, pins the " +
			"latest published one. Nothing is fetched or applied until the next sync.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, _, err := config.Dir(opts.configDir)
			if err != nil {
				return err
			}
			cfg, err := loadConfig(opts)
			if err != nil {
				return err
			}

			release := ""
			if len(args) == 1 {
				release = args[0]
			} else if release, err = releaseCache().Refresh(cmd.Context(), cacheKeyPackages, fetchLatestPackages); err != nil {
				return err
			}

			previous, err := pinPackages(cmd.Context(), dir, cfg, release)
			if err != nil {
				return err
			}

			if previous == "" || previous == release {
				cmd.Printf("pinned packages %s. `devmachine sync` applies it.\n", release)
			} else {
				cmd.Printf("pinned packages %s (was %s). `devmachine sync` applies it.\n", release, previous)
			}
			return nil
		},
	}
}

// pinPackages writes `packages: <release>` to config.yml and commits it when
// the configuration is versioned. It returns the release pinned before.
func pinPackages(ctx context.Context, dir string, cfg config.Config, release string) (string, error) {
	previous := cfg.Packages
	cfg.Packages = release
	if err := cfg.Validate(); err != nil {
		return previous, err
	}
	if err := config.Save(dir, cfg); err != nil {
		return previous, err
	}
	repo.AutoCommit(ctx, dir, "chore(config): pin packages "+release)
	return previous, nil
}
