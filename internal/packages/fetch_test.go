package packages

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// tarballWith builds a gzipped tar holding exactly these files.
func tarballWith(t *testing.T, files map[string]string) []byte {
	t.Helper()

	var buffer bytes.Buffer
	gz := gzip.NewWriter(&buffer)
	writer := tar.NewWriter(gz)

	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	slices.Sort(names)

	for _, name := range names {
		body := files[name]
		header := &tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}
		if err := writer.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

// stubReleaseURL points the fetcher at a test server and returns the undo.
func stubReleaseURL(base string) func() {
	previous := releaseURL
	releaseURL = func(version, asset string) string {
		return fmt.Sprintf("%s/%s/%s", base, version, asset)
	}
	return func() { releaseURL = previous }
}

// serveRelease answers like a GitHub release: the tarball and a checksums.txt.
func serveRelease(t *testing.T, tarball []byte, sum string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, ".tar.gz"):
			_, _ = w.Write(tarball)
		case strings.HasSuffix(r.URL.Path, "checksums.txt"):
			fmt.Fprintf(w, "%s  packages-v1.tar.gz\n", sum)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func TestFetchExtractsAndVerifies(t *testing.T) {
	tarball := tarballWith(t, map[string]string{
		"packages/git/package.yml":    "format: 1\nname: git\nscope: machine\nsummary: Installs git.\n",
		"packages/git/tasks/main.yml": "---\n- name: Install git\n  package: {name: git, state: present}\n",
	})
	sum := fmt.Sprintf("%x", sha256.Sum256(tarball))

	srv := serveRelease(t, tarball, sum)
	defer srv.Close()
	defer stubReleaseURL(srv.URL)()

	cache := t.TempDir()
	dir, got, err := Fetch(context.Background(), cache, "v1")
	if err != nil {
		t.Fatal(err)
	}
	if got != sum {
		t.Fatalf("checksum %q, want %q", got, sum)
	}
	if _, err := os.Stat(filepath.Join(dir, "git", "package.yml")); err != nil {
		t.Fatalf("the recipe was not extracted: %v", err)
	}
}

func TestFetchRefusesATarballThatDoesNotMatchItsChecksum(t *testing.T) {
	tarball := tarballWith(t, map[string]string{"packages/git/package.yml": "format: 1\nname: git\n"})
	srv := serveRelease(t, tarball, strings.Repeat("0", 64))
	defer srv.Close()
	defer stubReleaseURL(srv.URL)()

	_, _, err := Fetch(context.Background(), t.TempDir(), "v1")
	if err == nil {
		t.Fatal("a tarball with the wrong checksum was accepted")
	}
	if !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("the error does not say why: %v", err)
	}
}

func TestFetchDoesNotDownloadTwice(t *testing.T) {
	tarball := tarballWith(t, map[string]string{"packages/git/package.yml": "format: 1\nname: git\n"})
	sum := fmt.Sprintf("%x", sha256.Sum256(tarball))

	var downloads int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".tar.gz") {
			downloads++
			_, _ = w.Write(tarball)
			return
		}
		fmt.Fprintf(w, "%s  packages-v1.tar.gz\n", sum)
	}))
	defer srv.Close()
	defer stubReleaseURL(srv.URL)()

	cache := t.TempDir()
	for range 2 {
		if _, _, err := Fetch(context.Background(), cache, "v1"); err != nil {
			t.Fatal(err)
		}
	}
	if downloads != 1 {
		t.Fatalf("downloaded %d times; a cached release needs no network", downloads)
	}
}

func TestFetchRefusesAPathThatEscapesTheCache(t *testing.T) {
	tarball := tarballWith(t, map[string]string{"packages/../../escape.txt": "x"})
	sum := fmt.Sprintf("%x", sha256.Sum256(tarball))
	srv := serveRelease(t, tarball, sum)
	defer srv.Close()
	defer stubReleaseURL(srv.URL)()

	_, _, err := Fetch(context.Background(), t.TempDir(), "v1")
	if err == nil {
		t.Fatal("a tarball writing outside the cache was accepted")
	}
	if !strings.Contains(err.Error(), "outside the cache") {
		t.Fatalf("the error does not say why: %v", err)
	}
}

// A symlink is the other way a tarball reaches out of the directory it was
// extracted into, and no recipe has ever needed one.
func TestFetchRefusesASymlink(t *testing.T) {
	var buffer bytes.Buffer
	gz := gzip.NewWriter(&buffer)
	writer := tar.NewWriter(gz)
	if err := writer.WriteHeader(&tar.Header{
		Name: "packages/escape", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd", Mode: 0o777,
	}); err != nil {
		t.Fatal(err)
	}
	writer.Close()
	gz.Close()

	tarball := buffer.Bytes()
	srv := serveRelease(t, tarball, fmt.Sprintf("%x", sha256.Sum256(tarball)))
	defer srv.Close()
	defer stubReleaseURL(srv.URL)()

	if _, _, err := Fetch(context.Background(), t.TempDir(), "v1"); err == nil {
		t.Fatal("a symlink in a release was accepted")
	}
}

func TestFetchSaysWhenTheReleaseHasNoChecksumForTheAsset(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "deadbeef  something-else.tar.gz")
	}))
	defer srv.Close()
	defer stubReleaseURL(srv.URL)()

	_, _, err := Fetch(context.Background(), t.TempDir(), "v1")
	if err == nil {
		t.Fatal("a release with no checksum for the asset was accepted")
	}
	if !strings.Contains(err.Error(), "packages-v1.tar.gz") {
		t.Fatalf("the error does not name the asset: %v", err)
	}
}

func TestFetchSaysWhatTheServerAnswered(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	defer stubReleaseURL(srv.URL)()

	_, _, err := Fetch(context.Background(), t.TempDir(), "v9")
	if err == nil {
		t.Fatal("a missing release was accepted")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Fatalf("the error does not carry the status: %v", err)
	}
}

func TestFetchKeepsAReleaseAnotherProcessFinishedWhileItDownloaded(t *testing.T) {
	tarball := tarballWith(t, map[string]string{"packages/git/package.yml": "format: 1\nname: git\n"})
	sum := fmt.Sprintf("%x", sha256.Sum256(tarball))
	cache := t.TempDir()
	dir := CacheDir(cache, "v1")
	inUse := filepath.Join(dir, "packages", "git", "in-use")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".tar.gz") {
			if err := writeFinishedRelease(dir, sum, inUse); err != nil {
				t.Error(err)
			}
			_, _ = w.Write(tarball)
			return
		}
		fmt.Fprintf(w, "%s  packages-v1.tar.gz\n", sum)
	}))
	defer srv.Close()
	defer stubReleaseURL(srv.URL)()

	got, gotSum, err := Fetch(context.Background(), cache, "v1")
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join(dir, "packages") || gotSum != sum {
		t.Fatalf("Fetch = %q, %q; want the finished release", got, gotSum)
	}
	if _, err := os.Stat(inUse); err != nil {
		t.Fatalf("the release another process finished was replaced under it: %v", err)
	}
	assertOnlyRelease(t, cache, "v1")
}

func TestFetchConcurrentlyNeverHidesTheRelease(t *testing.T) {
	tarball := tarballWith(t, map[string]string{
		"packages/git/package.yml":    "format: 1\nname: git\n",
		"packages/skills/package.yml": "format: 1\nname: skills\n",
	})
	srv := serveRelease(t, tarball, fmt.Sprintf("%x", sha256.Sum256(tarball)))
	defer srv.Close()
	defer stubReleaseURL(srv.URL)()

	cache := t.TempDir()
	const fetchers = 8
	errs := make(chan error, fetchers)
	for range fetchers {
		go func() {
			dir, _, err := Fetch(context.Background(), cache, "v1")
			if err != nil {
				errs <- err
				return
			}
			for range 50 {
				if _, err := os.Stat(filepath.Join(dir, "skills", "package.yml")); err != nil {
					errs <- fmt.Errorf("the release went missing after Fetch returned: %w", err)
					return
				}
			}
			errs <- nil
		}()
	}
	for range fetchers {
		if err := <-errs; err != nil {
			t.Error(err)
		}
	}
	assertOnlyRelease(t, cache, "v1")
}

func TestFetchReplacesACacheDirectoryWithNoChecksum(t *testing.T) {
	tarball := tarballWith(t, map[string]string{"packages/git/package.yml": "format: 1\nname: git\n"})
	srv := serveRelease(t, tarball, fmt.Sprintf("%x", sha256.Sum256(tarball)))
	defer srv.Close()
	defer stubReleaseURL(srv.URL)()

	cache := t.TempDir()
	write(t, filepath.Join(CacheDir(cache, "v1"), "packages", "stale", "package.yml"), "format: 1\nname: stale\n")

	dir, _, err := Fetch(context.Background(), cache, "v1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "git", "package.yml")); err != nil {
		t.Fatalf("the release was not extracted: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "stale")); !os.IsNotExist(err) {
		t.Fatalf("an unverified directory survived: %v", err)
	}
	assertOnlyRelease(t, cache, "v1")
}

func writeFinishedRelease(dir, sum string, files ...string) error {
	for _, path := range append(files, filepath.Join(dir, checksumFile)) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(sum+"\n"), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func assertOnlyRelease(t *testing.T, cache, version string) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Dir(CacheDir(cache, version)))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	if !slices.Equal(names, []string{version}) {
		t.Fatalf("the cache holds %v, want only %s", names, version)
	}
}

func TestCacheDirIsUnderTheConfigurationDirectory(t *testing.T) {
	got := CacheDir("/tmp/conf", "v1")
	if got != filepath.Join("/tmp/conf", "cache", "packages", "v1") {
		t.Fatalf("got %q", got)
	}
}
