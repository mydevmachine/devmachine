package commands

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/mydevmachine/devmachine/internal/doctor"
	"github.com/mydevmachine/devmachine/internal/release"
	"github.com/mydevmachine/devmachine/internal/selfupdate"
)

// latestCLIRelease is the seam a test replaces so asking for the newest CLI
// never reaches the network.
var latestCLIRelease = selfupdate.Latest

// releaseCacheDir is where the answers to "what is latest" are remembered.
var releaseCacheDir = func() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "devmachine"), nil
}

// releaseClock is the seam a test replaces to move time forward.
var releaseClock = time.Now

const (
	cacheKeyCLI      = "cli"
	cacheKeyPackages = "packages"

	// packagesCheckTTL is how long `packages outdated` and the update hint
	// believe a looked-up packages release. A packages release comes out
	// every few days, so a day-old answer costs nobody anything.
	packagesCheckTTL = 24 * time.Hour
)

func releaseCache() release.Cache {
	return releaseCacheFor(release.CacheTTL)
}

func releaseCacheFor(ttl time.Duration) release.Cache {
	dir, err := releaseCacheDir()
	if err != nil {
		dir = ""
	}
	return release.Cache{Dir: dir, TTL: ttl, Now: releaseClock}
}

// The fetchers read the seams when they are called, not when they are
// passed, so a test that swaps a seam is always the one answering.
func fetchLatestCLI(ctx context.Context) (string, error) { return latestCLIRelease(ctx) }

func fetchLatestPackages(ctx context.Context) (string, error) { return latestPackagesRelease(ctx) }

// freshnessChecks are doctor's two questions about your computer rather than
// a machine: is the CLI the newest release, and is the packages pin.
func freshnessChecks(ctx context.Context, pinned string) []doctor.Check {
	cache := releaseCache()
	latestCLI, cliErr := cache.Lookup(ctx, cacheKeyCLI, fetchLatestCLI)

	var (
		latestPackages string
		packagesErr    error
	)
	if pinned != "" {
		latestPackages, packagesErr = cache.Lookup(ctx, cacheKeyPackages, fetchLatestPackages)
	}
	return []doctor.Check{
		doctor.CLICheck(version, latestCLI, cliErr),
		doctor.PackagesPinCheck(pinned, latestPackages, packagesErr),
	}
}
