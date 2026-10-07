package packages

import (
	"context"
	"os"
	"path/filepath"
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

func TestCloneSaysWhenGitIsMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	_, err := Clone(context.Background(), "https://example.com/a.git", "", filepath.Join(t.TempDir(), "clone"))
	if err == nil || !strings.Contains(err.Error(), "git is not installed") {
		t.Fatalf("got %v", err)
	}
}
