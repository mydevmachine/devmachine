// Package gittest makes throwaway git repositories for tests, so nothing
// that fetches a package ever reaches the network.
package gittest

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Repo is a bare repository in a temporary folder, and the working copy its
// commits are made in.
type Repo struct {
	t    testing.TB
	work string
	bare string
}

// New makes an empty repository and keeps git away from the developer's own
// configuration for the rest of the test: no signing, no hooks, no aliases.
func New(t testing.TB) *Repo {
	t.Helper()
	root := t.TempDir()
	global := filepath.Join(root, "gitconfig")
	config := "[user]\n\tname = alice\n\temail = alice@example.com\n[commit]\n\tgpgsign = false\n[tag]\n\tgpgsign = false\n"
	if err := os.WriteFile(global, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", global)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	r := &Repo{t: t, work: filepath.Join(root, "work"), bare: filepath.Join(root, "remote.git")}
	r.git(root, "init", "-q", "--bare", "-b", "main", r.bare)
	r.git(root, "init", "-q", "-b", "main", r.work)
	r.git(r.work, "remote", "add", "origin", r.bare)
	return r
}

// Write puts a file in the working copy, with its folders, at mode.
func (r *Repo) Write(path, body string, mode os.FileMode) {
	r.t.Helper()
	full := filepath.Join(r.work, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(body), mode); err != nil {
		r.t.Fatal(err)
	}
	if err := os.Chmod(full, mode); err != nil {
		r.t.Fatal(err)
	}
}

// Link puts a symbolic link in the working copy.
func (r *Repo) Link(path, target string) {
	r.t.Helper()
	full := filepath.Join(r.work, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		r.t.Fatal(err)
	}
	if err := os.Symlink(target, full); err != nil {
		r.t.Fatal(err)
	}
}

// Remove deletes a file from the working copy.
func (r *Repo) Remove(path string) {
	r.t.Helper()
	if err := os.Remove(filepath.Join(r.work, filepath.FromSlash(path))); err != nil {
		r.t.Fatal(err)
	}
}

// Commit commits everything in the working copy, pushes main, and returns
// the new commit.
func (r *Repo) Commit(message string) string {
	r.t.Helper()
	r.git(r.work, "add", "-A")
	r.git(r.work, "commit", "-q", "-m", message)
	r.git(r.work, "push", "-q", "origin", "HEAD:main")
	return r.Head()
}

// Tag tags the last commit and pushes the tag.
func (r *Repo) Tag(name string) {
	r.t.Helper()
	r.git(r.work, "tag", name)
	r.git(r.work, "push", "-q", "origin", name)
}

// Head is the last commit.
func (r *Repo) Head() string {
	r.t.Helper()
	return strings.TrimSpace(r.git(r.work, "rev-parse", "HEAD"))
}

// URL is the bare repository's file:// address.
func (r *Repo) URL() string { return "file://" + filepath.ToSlash(r.bare) }

func (r *Repo) git(dir string, args ...string) string {
	r.t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}
