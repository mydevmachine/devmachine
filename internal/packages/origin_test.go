package packages

import (
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
