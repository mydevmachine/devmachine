package commands

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/mydevmachine/devmachine/internal/packages"
	"github.com/mydevmachine/devmachine/internal/release"
	"github.com/spf13/cobra"
)

// packagesOutdated is what `packages outdated --format json` prints.
type packagesOutdated struct {
	Pinned   string `json:"pinned"`
	Latest   string `json:"latest"`
	Newer    bool   `json:"newer"`
	NotesURL string `json:"notes_url"`
}

func newPackagesOutdatedCmd(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "outdated",
		Short: "Say whether a newer packages release than the pinned one is out",
		Long: "Compares the release config.yml pins with the newest published one. " +
			"The newest release is asked of GitHub at most once a day; offline, the last " +
			"answer is used. Nothing changes: `devmachine update` moves the pin.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			pinned, err := pinnedRelease(opts)
			if err != nil {
				return err
			}
			result, err := checkPackagesOutdated(cmd.Context(), pinned)
			if err != nil {
				return err
			}
			if opts.format == formatJSON {
				return writeJSON(cmd.OutOrStdout(), result)
			}
			switch {
			case result.Newer:
				cmd.Println(packagesHintLine(result.Latest, result.Pinned))
				cmd.Println("what changed: " + result.NotesURL)
			case result.Pinned == "":
				cmd.Printf("nothing is pinned; the latest packages release is %s. `devmachine packages pin` pins it.\n", result.Latest)
			default:
				cmd.Printf("packages %s is pinned; the latest release is %s.\n", result.Pinned, result.Latest)
			}
			return nil
		},
	}
}

// pinnedRelease is the packages release config.yml pins. No configuration
// at all pins nothing, the same as a configuration without `packages:`.
func pinnedRelease(opts *options) (string, error) {
	cfg, err := loadConfig(opts)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return cfg.Packages, nil
}

func checkPackagesOutdated(ctx context.Context, pinned string) (packagesOutdated, error) {
	latest, err := releaseCacheFor(packagesCheckTTL).LookupOrStale(ctx, cacheKeyPackages, fetchLatestPackages)
	if err != nil {
		return packagesOutdated{}, fmt.Errorf("cannot tell the latest packages release, and none was read before: %w", err)
	}
	return packagesOutdated{
		Pinned:   pinned,
		Latest:   latest,
		Newer:    pinned != "" && release.Newer(latest, pinned),
		NotesURL: packages.NotesURL(latest),
	}, nil
}

// packagesHintLine is the one line that says a newer packages release is out.
func packagesHintLine(latest, pinned string) string {
	return fmt.Sprintf("packages %s is out (you pin %s): run devmachine update", latest, pinned)
}
