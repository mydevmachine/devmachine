package logfile

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func readLog(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the log: %v", err)
	}
	return string(body)
}

func TestPathIsTheContractTheAppOpens(t *testing.T) {
	if got, want := Path("/tmp/alice"), filepath.Join("/tmp/alice", "logs", "devmachine.log"); got != want {
		t.Fatalf("Path = %q, want %q", got, want)
	}
}

func TestOpenCreatesAPrivateFolderAndFile(t *testing.T) {
	dir := t.TempDir()
	l := Open(dir)
	l.Line("start", "hello")
	l.Close()

	folder, err := os.Stat(filepath.Join(dir, "logs"))
	if err != nil {
		t.Fatal(err)
	}
	if perm := folder.Mode().Perm(); perm != 0o700 {
		t.Fatalf("folder mode %o, want 700", perm)
	}
	file, err := os.Stat(Path(dir))
	if err != nil {
		t.Fatal(err)
	}
	if perm := file.Mode().Perm(); perm != 0o600 {
		t.Fatalf("file mode %o, want 600", perm)
	}
}

func TestOpenKeepsTheFolderOutOfAnyRepository(t *testing.T) {
	dir := t.TempDir()
	Open(dir).Close()

	body, err := os.ReadFile(filepath.Join(dir, "logs", ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "*\n" {
		t.Fatalf(".gitignore = %q", body)
	}
}

func TestLineIsStampedAndRedacted(t *testing.T) {
	dir := t.TempDir()
	l := Open(dir)
	l.now = func() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 6_000_000, time.UTC) }
	l.Line("start", "login --token=fake-token on 203.0.113.10")
	l.Close()

	got := readLog(t, Path(dir))
	want := fmt.Sprintf("2026-01-02T03:04:05.006Z [%d] start  login --token=*** on 203.0.113.10\n", os.Getpid())
	if got != want {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
}

func TestTeePassesEverythingThroughAndLogsWholeLines(t *testing.T) {
	dir := t.TempDir()
	l := Open(dir)
	var screen bytes.Buffer
	w := l.Tee(&screen, "out")

	fmt.Fprint(w, "first line\nsecond ")
	fmt.Fprint(w, "half\nGH_TOKEN=fake-value\nno newline")
	l.Close()

	if want := "first line\nsecond half\nGH_TOKEN=fake-value\nno newline"; screen.String() != want {
		t.Fatalf("screen got %q", screen.String())
	}
	got := readLog(t, Path(dir))
	for _, want := range []string{"out    first line\n", "out    second half\n", "out    GH_TOKEN=***\n", "out    no newline\n"} {
		if !strings.Contains(got, want) {
			t.Fatalf("log lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "fake-value") {
		t.Fatalf("log holds the secret:\n%s", got)
	}
}

func TestTeeDropsAPrivateKeyAcrossWrites(t *testing.T) {
	dir := t.TempDir()
	l := Open(dir)
	w := l.Tee(&bytes.Buffer{}, "err")
	fmt.Fprint(w, "-----BEGIN OPENSSH PRIVATE KEY-----\nFAKEBODY")
	fmt.Fprint(w, "LINE\n-----END OPENSSH PRIVATE KEY-----\n")
	l.Close()

	if got := readLog(t, Path(dir)); strings.Contains(got, "FAKEBODY") {
		t.Fatalf("log holds the key:\n%s", got)
	}
}

func TestRotationKeepsThreeOldFiles(t *testing.T) {
	dir := t.TempDir()
	for i := range 6 {
		l := Open(dir)
		l.limit = 10
		l.Line("start", fmt.Sprintf("run %d with a line longer than the limit", i))
		l.Close()
	}

	for _, name := range []string{"devmachine.log", "devmachine.log.1", "devmachine.log.2", "devmachine.log.3"} {
		if _, err := os.Stat(filepath.Join(dir, "logs", name)); err != nil {
			t.Fatalf("%s is missing: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "logs", "devmachine.log.4")); !os.IsNotExist(err) {
		t.Fatalf("a fourth old file survived: %v", err)
	}
	if got := readLog(t, Path(dir)); !strings.Contains(got, "run 5") {
		t.Fatalf("the newest run is not in the current file: %q", got)
	}
	if got := readLog(t, Path(dir)+".1"); !strings.Contains(got, "run 4") {
		t.Fatalf("the run before is not in .1: %q", got)
	}
}

func TestRotationHappensDuringALongRun(t *testing.T) {
	dir := t.TempDir()
	l := Open(dir)
	l.limit = 10
	l.Line("out", "one line that fills the file")
	l.Line("out", "the next line starts a new one")
	l.Close()

	if got := readLog(t, Path(dir)); !strings.Contains(got, "the next line") || strings.Contains(got, "fills the file") {
		t.Fatalf("current file: %q", got)
	}
}

func TestAnUnwritableFolderCostsNothing(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	l := Open(dir)
	var screen bytes.Buffer
	fmt.Fprint(l.Tee(&screen, "out"), "still shown\n")
	l.Line("end", "ok")
	l.Close()

	if screen.String() != "still shown\n" {
		t.Fatalf("screen got %q", screen.String())
	}
}

func TestANilLogIsSafe(t *testing.T) {
	var l *Log
	var screen bytes.Buffer
	fmt.Fprint(l.Tee(&screen, "out"), "x\n")
	l.Line("end", "ok")
	l.Close()
	if screen.String() != "x\n" {
		t.Fatalf("screen got %q", screen.String())
	}
}
