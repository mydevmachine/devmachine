package packages

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// releaseBase is where the published recipes live. A release asset, not the
// tag's own archive: a tag can be moved and its archive silently changes
// underneath you, while an asset's checksum goes in the lock file.
const releaseBase = "https://github.com/mydevmachine/packages/releases/download"

// checksumFile records which tarball produced a cache directory, so a cached
// release can be used without the network and still be identified in the lock.
const checksumFile = ".checksum"

// releaseURL is a variable so a test can answer without the network.
var releaseURL = githubReleaseURL

var fetchClient = &http.Client{Timeout: 5 * time.Minute}

func githubReleaseURL(version, asset string) string {
	return fmt.Sprintf("%s/%s/%s", releaseBase, version, asset)
}

// CacheDir is where a fetched release is extracted to.
func CacheDir(configDir, version string) string {
	return filepath.Join(configDir, "cache", "packages", version)
}

// Fetch makes sure the pinned release is in the cache, and returns the
// directory the recipes were extracted to and the tarball's checksum.
//
// Nothing is embedded in the binary, so the first build of a machine needs
// GitHub to be reachable. After that, converging needs no network at all.
func Fetch(ctx context.Context, configDir, version string) (string, string, error) {
	dir := CacheDir(configDir, version)
	if sum, err := os.ReadFile(filepath.Join(dir, checksumFile)); err == nil {
		return filepath.Join(dir, "packages"), strings.TrimSpace(string(sum)), nil
	}

	asset := "packages-" + version + ".tar.gz"
	want, err := fetchChecksum(ctx, version, asset)
	if err != nil {
		return "", "", err
	}
	body, err := get(ctx, releaseURL(version, asset))
	if err != nil {
		return "", "", err
	}

	got := fmt.Sprintf("%x", sha256.Sum256(body))
	if got != want {
		return "", "", fmt.Errorf(
			"%s has checksum %s but the release says %s: the asset changed, which a pin exists to prevent",
			asset, got, want)
	}

	// Extract beside the target and rename, so an interrupted fetch never
	// leaves a half-extracted release that the next run would trust. Each
	// fetch stages in its own directory: another process may be fetching the
	// same release at the same moment.
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return "", "", fmt.Errorf("creating the packages cache: %w", err)
	}
	staging, err := os.MkdirTemp(filepath.Dir(dir), version+".partial-")
	if err != nil {
		return "", "", fmt.Errorf("creating the packages cache: %w", err)
	}
	defer func() { _ = os.RemoveAll(staging) }()
	if err := os.Chmod(staging, 0o755); err != nil {
		return "", "", err
	}
	if err := extract(body, staging); err != nil {
		return "", "", err
	}
	if err := os.WriteFile(filepath.Join(staging, checksumFile), []byte(got+"\n"), 0o644); err != nil {
		return "", "", err
	}
	if err := publish(staging, dir); err != nil {
		return "", "", fmt.Errorf("saving packages %s in the cache: %w", version, err)
	}
	return filepath.Join(dir, "packages"), got, nil
}

// publish moves a verified staging directory into place. A release directory
// holding a checksum is complete and another process may be reading it, so it
// is never removed: the first fetch to finish wins and the others use its copy.
func publish(staging, dir string) error {
	err := os.Rename(staging, dir)
	if err == nil || complete(dir) {
		return nil
	}
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	if err := os.Rename(staging, dir); err != nil && !complete(dir) {
		return err
	}
	return nil
}

func complete(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, checksumFile))
	return err == nil
}

func fetchChecksum(ctx context.Context, version, asset string) (string, error) {
	body, err := get(ctx, releaseURL(version, "checksums.txt"))
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(body), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == asset {
			return fields[0], nil
		}
	}
	return "", fmt.Errorf("release %s has no checksum for %s", version, asset)
}

func get(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := fetchClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching %s: the server answered %d", url, resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

func extract(tarball []byte, into string) error {
	gz, err := gzip.NewReader(bytes.NewReader(tarball))
	if err != nil {
		return fmt.Errorf("reading the release archive: %w", err)
	}
	defer func() { _ = gz.Close() }()

	root := filepath.Clean(into)
	reader := tar.NewReader(gz)
	for {
		header, err := reader.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("reading the release archive: %w", err)
		}

		target := filepath.Join(root, filepath.Clean(header.Name))
		// A tarball is downloaded content, and downloaded content does not get
		// to choose where it lands.
		if !strings.HasPrefix(target, root+string(os.PathSeparator)) {
			return fmt.Errorf("the release archive holds %q, which would write outside the cache", header.Name)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := writeFromTar(target, reader, os.FileMode(header.Mode)&0o777); err != nil {
				return err
			}
		default:
			// A symlink in a recipe would be a way to reach outside the
			// package. Nothing needs one.
			return fmt.Errorf("the release archive holds %q, which is not a plain file or directory", header.Name)
		}
	}
}

func writeFromTar(target string, reader io.Reader, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	// A release is at most a few hundred kilobytes of YAML, so the whole file
	// is copied without a limit reader; the checksum has already vouched for
	// the bytes by the time anything is written.
	if _, err := io.Copy(file, reader); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}
