package selfupdate

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/creack/pty"
	"github.com/mydevmachine/devmachine/internal/logfile"
)

func TestHomebrewPrefix(t *testing.T) {
	cases := []struct {
		path, brew, want string
	}{
		{"/opt/homebrew/Cellar/devmachine/0.7.18/bin/devmachine", "", "/opt/homebrew"},
		{"/opt/homebrew/bin/devmachine", "", "/opt/homebrew"},
		{"/usr/local/Cellar/devmachine/0.7.18/bin/devmachine", "", "/usr/local"},
		{"/usr/local/bin/devmachine", "", ""},
		{"/home/alice/.local/bin/devmachine", "", ""},
		{"/home/linuxbrew/.linuxbrew/Cellar/devmachine/0.7.18/bin/devmachine", "", "/home/linuxbrew/.linuxbrew"},
		{"/srv/brew/Cellar/devmachine/0.7.18/bin/devmachine", "/srv/brew", "/srv/brew"},
		{"/srv/brew-other/devmachine", "/srv/brew", ""},
	}
	for _, c := range cases {
		if got := HomebrewPrefix(c.path, c.brew); got != c.want {
			t.Errorf("HomebrewPrefix(%q, %q) = %q; want %q", c.path, c.brew, got, c.want)
		}
	}
}

func TestHomebrewPrefixFollowsASymlink(t *testing.T) {
	root := t.TempDir()
	prefix := filepath.Join(root, "brew")
	real := filepath.Join(prefix, "Cellar", "devmachine", "0.7.18", "bin", "devmachine")
	writeFile(t, real, "binary", 0o755)
	link := filepath.Join(root, "bin", "devmachine")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}

	if got := HomebrewPrefix(link, prefix); got == "" {
		t.Fatal("a symlink into the Cellar is a Homebrew install")
	}
	if got := HomebrewPrefix(link, ""); got != "" {
		t.Fatalf("without the prefix nothing says Homebrew: %q", got)
	}
}

func TestBinaryUnderPrefix(t *testing.T) {
	if got := BinaryUnder("/opt/homebrew"); got != "/opt/homebrew/bin/devmachine" {
		t.Fatalf("got %q", got)
	}
}

func fakeBrew(t *testing.T, script string) (Brew, string) {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "calls.log")
	body := "#!/bin/sh\necho \"$*\" >> \"$BREW_LOG\"\n" + script
	writeFile(t, filepath.Join(dir, "brew"), body, 0o755)
	out := &bytes.Buffer{}
	return Brew{
		Path:   filepath.Join(dir, "brew"),
		Env:    []string{"PATH=" + dir + ":/usr/bin:/bin", "BREW_LOG=" + log},
		Stdout: out,
		Stderr: out,
	}, log
}

func calls(t *testing.T, log string) []string {
	t.Helper()
	body, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(body)), "\n")
}

func TestBrewUpgradeTrustsTheTapWhenBrewCanTrust(t *testing.T) {
	brew, log := fakeBrew(t, "exit 0\n")
	if err := brew.Upgrade(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := []string{"update", "trust --help", "trust --formula " + Formula, "upgrade " + Formula}
	if got := calls(t, log); !slices.Equal(got, want) {
		t.Fatalf("got %q; want %q", got, want)
	}
}

func TestBrewUpgradeSkipsTrustOnAnOlderBrew(t *testing.T) {
	brew, log := fakeBrew(t, "[ \"$1\" = trust ] && exit 1\nexit 0\n")
	if err := brew.Upgrade(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := []string{"update", "trust --help", "upgrade " + Formula}
	if got := calls(t, log); !slices.Equal(got, want) {
		t.Fatalf("got %q; want %q", got, want)
	}
}

func TestBrewUpgradeReportsAFailedUpgrade(t *testing.T) {
	brew, _ := fakeBrew(t, "[ \"$1\" = upgrade ] && exit 3\nexit 0\n")
	err := brew.Upgrade(context.Background())
	if err == nil || !strings.Contains(err.Error(), "brew upgrade") {
		t.Fatalf("got %v", err)
	}
}

func TestBrewUpgradeGoesOnWhenBrewUpdateFails(t *testing.T) {
	brew, log := fakeBrew(t, "[ \"$1\" = update ] && exit 1\nexit 0\n")
	if err := brew.Upgrade(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := calls(t, log); got[len(got)-1] != "upgrade "+Formula {
		t.Fatalf("did not upgrade after a failed update: %q", got)
	}
}

type published struct {
	version string
	archive []byte
	sums    string
}

func tarball(t *testing.T, name, body string) []byte {
	t.Helper()
	buf := &bytes.Buffer{}
	gz := gzip.NewWriter(buf)
	tw := tar.NewWriter(gz)
	for _, f := range []struct{ name, body string }{{"LICENSE", "MIT"}, {name, body}} {
		if err := tw.WriteHeader(&tar.Header{Name: f.name, Mode: 0o755, Size: int64(len(f.body))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(f.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func publish(t *testing.T, r published) (*httptest.Server, *[]string) {
	t.Helper()
	var asked []string
	archive := ArchiveName(r.version, "linux", "amd64")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		asked = append(asked, req.URL.Path)
		switch req.URL.Path {
		case "/" + r.version + "/checksums.txt":
			_, _ = w.Write([]byte(r.sums))
		case "/" + r.version + "/" + archive:
			_, _ = w.Write(r.archive)
		default:
			http.NotFound(w, req)
		}
	}))
	t.Cleanup(server.Close)
	return server, &asked
}

func goodRelease(t *testing.T, version, body string) published {
	t.Helper()
	archive := tarball(t, "devmachine", body)
	sum := sha256.Sum256(archive)
	sums := fmt.Sprintf("%s  %s\n%s  %s\n",
		strings.Repeat("0", 64), ArchiveName(version, "darwin", "arm64"),
		hex.EncodeToString(sum[:]), ArchiveName(version, "linux", "amd64"))
	return published{version: version, archive: archive, sums: sums}
}

func installedBinary(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "bin", "devmachine")
	writeFile(t, path, "old binary", 0o755)
	return path
}

func TestArchiveNameMatchesTheRelease(t *testing.T) {
	if got := ArchiveName("v0.7.18", "darwin", "arm64"); got != "devmachine_0.7.18_darwin_arm64.tar.gz" {
		t.Fatalf("got %q", got)
	}
}

func TestReplaceDownloadsVerifiesAndSwapsTheBinary(t *testing.T) {
	server, _ := publish(t, goodRelease(t, "v0.8.0", "new binary"))
	path := installedBinary(t)

	d := Downloader{BaseURL: server.URL, OS: "linux", Arch: "amd64"}
	if err := d.Replace(context.Background(), "v0.8.0", path); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil || string(body) != "new binary" {
		t.Fatalf("got %q, %v", body, err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("the new binary is not executable: %v", info.Mode())
	}
	assertOnlyFile(t, filepath.Dir(path), "devmachine")
}

func TestReplaceThroughASymlinkReplacesTheTarget(t *testing.T) {
	server, _ := publish(t, goodRelease(t, "v0.8.0", "new binary"))
	real := installedBinary(t)
	link := filepath.Join(t.TempDir(), "devmachine")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}

	d := Downloader{BaseURL: server.URL, OS: "linux", Arch: "amd64"}
	if err := d.Replace(context.Background(), "v0.8.0", link); err != nil {
		t.Fatal(err)
	}
	if body, _ := os.ReadFile(real); string(body) != "new binary" {
		t.Fatalf("the target was not replaced: %q", body)
	}
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("the symlink itself was replaced")
	}
}

func TestReplaceRefusesAChecksumMismatch(t *testing.T) {
	r := goodRelease(t, "v0.8.0", "new binary")
	r.archive = tarball(t, "devmachine", "tampered binary")
	server, _ := publish(t, r)
	path := installedBinary(t)

	d := Downloader{BaseURL: server.URL, OS: "linux", Arch: "amd64"}
	err := d.Replace(context.Background(), "v0.8.0", path)
	if err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("got %v", err)
	}
	if body, _ := os.ReadFile(path); string(body) != "old binary" {
		t.Fatalf("the binary changed after a mismatch: %q", body)
	}
	assertOnlyFile(t, filepath.Dir(path), "devmachine")
}

func TestReplaceRefusesAnArchiveMissingFromTheChecksums(t *testing.T) {
	r := goodRelease(t, "v0.8.0", "new binary")
	r.sums = strings.Repeat("0", 64) + "  something_else.tar.gz\n"
	server, _ := publish(t, r)
	path := installedBinary(t)

	d := Downloader{BaseURL: server.URL, OS: "linux", Arch: "amd64"}
	err := d.Replace(context.Background(), "v0.8.0", path)
	if err == nil || !strings.Contains(err.Error(), "checksums.txt") {
		t.Fatalf("got %v", err)
	}
	if body, _ := os.ReadFile(path); string(body) != "old binary" {
		t.Fatalf("the binary changed: %q", body)
	}
}

func TestReplaceRefusesAnArchiveWithNoBinary(t *testing.T) {
	archive := tarball(t, "README.md", "no binary here")
	sum := sha256.Sum256(archive)
	r := published{version: "v0.8.0", archive: archive,
		sums: hex.EncodeToString(sum[:]) + "  " + ArchiveName("v0.8.0", "linux", "amd64") + "\n"}
	server, _ := publish(t, r)
	path := installedBinary(t)

	d := Downloader{BaseURL: server.URL, OS: "linux", Arch: "amd64"}
	if err := d.Replace(context.Background(), "v0.8.0", path); err == nil {
		t.Fatal("an archive with no devmachine binary was accepted")
	}
	if body, _ := os.ReadFile(path); string(body) != "old binary" {
		t.Fatalf("the binary changed: %q", body)
	}
}

func TestReplaceReportsAMissingRelease(t *testing.T) {
	server, _ := publish(t, goodRelease(t, "v0.8.0", "new binary"))
	path := installedBinary(t)

	d := Downloader{BaseURL: server.URL, OS: "linux", Arch: "amd64"}
	if err := d.Replace(context.Background(), "v9.9.9", path); err == nil {
		t.Fatal("a release that does not exist was installed")
	}
}

func TestContinueArgs(t *testing.T) {
	cases := []struct {
		in   []string
		want []string
	}{
		{[]string{"update"}, []string{"update", "--continue-after-self-update=0.7.18"}},
		{[]string{"--config", "/c", "update", "--yes", "--machine", "main"},
			[]string{"--config", "/c", "update", "--yes", "--machine", "main", "--continue-after-self-update=0.7.18"}},
		{[]string{"update", "--continue-after-self-update=0.7.1"}, []string{"update", "--continue-after-self-update=0.7.18"}},
		{[]string{"update", "--continue-after-self-update", "0.7.1", "--yes"}, []string{"update", "--yes", "--continue-after-self-update=0.7.18"}},
	}
	for _, c := range cases {
		if got := ContinueArgs(c.in, "0.7.18"); !slices.Equal(got, c.want) {
			t.Errorf("ContinueArgs(%q) = %q; want %q", c.in, got, c.want)
		}
	}
}

func writeFile(t *testing.T, path, body string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
}

func assertOnlyFile(t *testing.T, dir, name string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != name {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("%s holds %q; want only %s", dir, names, name)
	}
}

func TestBrewUpgradeShowsBrewATerminalThroughTheLog(t *testing.T) {
	master, terminal, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer master.Close()
	defer terminal.Close()
	go func() { _, _ = io.Copy(io.Discard, master) }()

	brew, callLog := fakeBrew(t, "[ -t 1 ] && [ -t 2 ] && echo \"tty $*\" >> \"$BREW_LOG\"\nexit 0\n")
	log := logfile.Open(t.TempDir())
	defer log.Close()
	brew.Stdout, brew.Stderr = log.Tee(terminal, "out"), log.Tee(terminal, "err")
	if err := brew.Upgrade(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"tty update", "tty trust --formula " + Formula, "tty upgrade " + Formula} {
		if !slices.Contains(calls(t, callLog), want) {
			t.Errorf("brew did not see a terminal for %q: %q", want, calls(t, callLog))
		}
	}
}
