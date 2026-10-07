package packages

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mydevmachine/devmachine/internal/gittest"
)

func TestCloneFetchesTheDefaultBranchATagAndACommit(t *testing.T) {
	r := gittest.New(t)
	r.Write("a.txt", "one\n", 0o644)
	first := r.Commit("one")
	r.Tag("v1")
	r.Write("b.txt", "two\n", 0o644)
	second := r.Commit("two")

	cases := []struct {
		ref, commit string
		hasB        bool
	}{
		{"", second, true}, {"main", second, true}, {"v1", first, false}, {first, first, false},
	}
	for _, tc := range cases {
		into := filepath.Join(t.TempDir(), "clone")
		commit, err := Clone(context.Background(), r.URL(), tc.ref, into)
		if err != nil || commit != tc.commit {
			t.Fatalf("ref %q: got %q %v, want %q", tc.ref, commit, err, tc.commit)
		}
		_, bErr := os.Stat(filepath.Join(into, "b.txt"))
		if (bErr == nil) != tc.hasB {
			t.Errorf("ref %q: b.txt there is %v", tc.ref, bErr == nil)
		}
		if _, err := os.Stat(filepath.Join(into, ".git")); !os.IsNotExist(err) {
			t.Errorf("ref %q: .git was left in the copy", tc.ref)
		}
	}
}

func TestCloneSaysWhatFailed(t *testing.T) {
	r := gittest.New(t)
	r.Write("a.txt", "one\n", 0o644)
	r.Commit("one")
	for _, tc := range []struct{ address, ref string }{
		{"file://" + filepath.Join(t.TempDir(), "missing.git"), ""},
		{r.URL(), "v9"},
	} {
		_, err := Clone(context.Background(), tc.address, tc.ref, filepath.Join(t.TempDir(), "clone"))
		if err == nil || !strings.Contains(err.Error(), "fetching "+tc.address) {
			t.Errorf("%s@%s: got %v", tc.address, tc.ref, err)
		}
	}
}

func TestCloneIgnoresAGitDirFromTheEnvironment(t *testing.T) {
	r := gittest.New(t)
	r.Write("a.txt", "one\n", 0o644)
	want := r.Commit("one")
	other := filepath.Join(t.TempDir(), "other")
	if out, err := exec.Command("git", "init", "-q", other).CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	before := listTree(t, other)
	t.Setenv("GIT_DIR", filepath.Join(other, ".git"))
	t.Setenv("GIT_WORK_TREE", other)
	t.Setenv("GIT_INDEX_FILE", filepath.Join(other, ".git", "index"))
	into := filepath.Join(t.TempDir(), "clone")
	commit, err := Clone(context.Background(), r.URL(), "", into)
	if err != nil || commit != want {
		t.Fatalf("got %q %v, want %q", commit, err, want)
	}
	if _, err := os.Stat(filepath.Join(into, "a.txt")); err != nil {
		t.Fatal(err)
	}
	if after := listTree(t, other); !slices.Equal(before, after) {
		t.Fatalf("the repository GIT_DIR named was touched:\nbefore %v\nafter  %v", before, after)
	}
}

func listTree(t *testing.T, root string) []string {
	t.Helper()
	var paths []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		paths = append(paths, fmt.Sprintf("%s %d %d", path, info.Size(), info.ModTime().UnixNano()))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return paths
}

func TestCloneRefusesAFolderThatIsThere(t *testing.T) {
	r := gittest.New(t)
	r.Write("a.txt", "one\n", 0o644)
	r.Commit("one")
	into := t.TempDir()
	if err := os.WriteFile(filepath.Join(into, "mine.txt"), []byte("keep\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Clone(context.Background(), r.URL(), "", into); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("got %v", err)
	}
	if _, err := os.Stat(filepath.Join(into, "mine.txt")); err != nil {
		t.Fatal(err)
	}
}

func TestCloneLeavesNothingBehindWhenItFails(t *testing.T) {
	r := gittest.New(t)
	r.Write("a.txt", "one\n", 0o644)
	r.Commit("one")
	into := filepath.Join(t.TempDir(), "clone")
	if _, err := Clone(context.Background(), r.URL(), "v9", into); err == nil {
		t.Fatal("want an error")
	}
	if _, err := os.Stat(into); !os.IsNotExist(err) {
		t.Fatalf("%s was left behind: %v", into, err)
	}
}

func TestCloneSaysWhenGitIsMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	_, err := Clone(context.Background(), "https://example.com/a.git", "", filepath.Join(t.TempDir(), "clone"))
	if err == nil || !strings.Contains(err.Error(), "git is not installed") {
		t.Fatalf("got %v", err)
	}
}
