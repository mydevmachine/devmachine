package packages

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOriginRoundTrips(t *testing.T) {
	dir := t.TempDir()
	want := Origin{URL: "https://example.com/alice/tools.git", Ref: "v1", Commit: "0123456789abcdef", InstalledAt: "2026-10-07T12:00:00Z"}
	if err := WriteOrigin(dir, want); err != nil {
		t.Fatal(err)
	}
	got, ok, err := ReadOrigin(dir)
	if err != nil || !ok || got != want {
		t.Fatalf("got %+v %v %v", got, ok, err)
	}
	if body := readFile(t, filepath.Join(dir, OriginFile)); !strings.HasPrefix(body, "url: https://example.com/alice/tools.git\n") {
		t.Fatalf("got %q", body)
	}
}

func TestReadOriginSaysWhenThereIsNone(t *testing.T) {
	if _, ok, err := ReadOrigin(t.TempDir()); ok || err != nil {
		t.Fatalf("got %v %v", ok, err)
	}
}

func TestReadOriginRefusesABrokenFile(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, OriginFile), "url: [\n")
	if _, _, err := ReadOrigin(dir); err == nil || !strings.Contains(err.Error(), OriginFile) {
		t.Fatalf("got %v", err)
	}
}

func TestWriteOriginNeverWritesThroughALink(t *testing.T) {
	dir := t.TempDir()
	task := filepath.Join(dir, "tasks", "main.yml")
	write(t, task, "---\n[]\n")
	if err := os.Symlink(filepath.Join("tasks", "main.yml"), filepath.Join(dir, OriginFile)); err != nil {
		t.Fatal(err)
	}
	if err := WriteOrigin(dir, Origin{URL: "https://example.com/alice/tools.git"}); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, task); got != "---\n[]\n" {
		t.Fatalf("the file the link pointed at was written: %q", got)
	}
	info, err := os.Lstat(filepath.Join(dir, OriginFile))
	if err != nil || !info.Mode().IsRegular() {
		t.Fatalf("got %v %v", info, err)
	}
}
