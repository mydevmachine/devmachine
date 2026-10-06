package provision

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type shareRun struct {
	home, outside, master string
	script                string
}

func newShareRun(t *testing.T) shareRun {
	t.Helper()
	files, err := Generate(planSharing(t, "bob"))
	if err != nil {
		t.Fatal(err)
	}
	copies := sharedTasks(t, files["site.yml"], "shell")
	if len(copies) != 1 {
		t.Fatalf("got %d tasks, want 1:\n%s", len(copies), files["site.yml"])
	}

	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r := shareRun{
		home:    filepath.Join(root, "home"),
		outside: filepath.Join(root, "outside"),
		master:  filepath.Join(root, "master"),
	}
	for _, d := range []string{r.home, r.outside} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(r.master, []byte("token: shared\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	r.script = strings.NewReplacer(
		"{{ "+sharedLoopVar+".user | quote }}", "alice",
		"{{ "+sharedLoopVar+".path | quote }}", "'.config/gh/hosts.yml'",
		"'/etc/devmachine/gh/hosts.yml'", "'"+r.master+"'",
	).Replace(asString(copies[0]["shell"]))
	return r
}

func (r shareRun) run(t *testing.T) (string, error) {
	t.Helper()
	return r.runWith(t, map[string]string{"runuser": "#!/bin/sh\nshift 3\nexec \"$@\"\n"}, os.Getenv("PATH"))
}

func (r shareRun) runWith(t *testing.T, stubs map[string]string, path string) (string, error) {
	t.Helper()
	bin := t.TempDir()
	stubs["getent"] = "#!/bin/sh\nprintf '%s:x:1000:1000::" + r.home + ":/bin/sh\\n' \"$2\"\n"
	for name, body := range stubs {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("/bin/sh", "-c", r.script)
	cmd.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+path)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return stdout.String(), &shareError{stderr: stderr.String()}
	}
	return stdout.String(), nil
}

type shareError struct{ stderr string }

func (e *shareError) Error() string { return e.stderr }

func TestSharedCopyLandsInsideTheHomeReadableByTheAccountAlone(t *testing.T) {
	r := newShareRun(t)

	out, err := r.run(t)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "changed") {
		t.Fatalf("a first copy has to report a change, got %q", out)
	}
	path := filepath.Join(r.home, ".config", "gh", "hosts.yml")
	if got, _ := os.ReadFile(path); string(got) != "token: shared\n" {
		t.Fatalf("got %q", got)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", info.Mode().Perm())
	}
	dir, _ := os.Stat(filepath.Dir(path))
	if dir.Mode().Perm() != 0o700 {
		t.Fatalf("directory mode %v", dir.Mode().Perm())
	}
}

func TestSharedCopyRunsAsTheAccountThroughSudoWhereThereIsNoRunuser(t *testing.T) {
	r := newShareRun(t)
	log := filepath.Join(t.TempDir(), "sudo.log")
	sudo := "#!/bin/sh\nprintf '%s ' \"$@\" > " + log + "\nshift 4\nexec \"$@\"\n"

	if _, err := r.runWith(t, map[string]string{"sudo": sudo}, "/usr/bin:/bin"); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(r.home, ".config", "gh", "hosts.yml")); string(got) != "token: shared\n" {
		t.Fatalf("got %q", got)
	}
	if called, _ := os.ReadFile(log); !strings.HasPrefix(string(called), "-n -u alice -- /bin/sh -c") {
		t.Fatalf("sudo was called as %q", called)
	}
}

func TestSharedCopyReportsNoChangeWhenTheCopyIsAlreadyThere(t *testing.T) {
	r := newShareRun(t)
	if _, err := r.run(t); err != nil {
		t.Fatal(err)
	}

	out, err := r.run(t)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "changed") {
		t.Fatalf("a second copy of the same login changed something: %q", out)
	}
	leftovers, _ := filepath.Glob(filepath.Join(r.home, ".config", "gh", ".devmachine.*"))
	if len(leftovers) != 0 {
		t.Fatalf("temporary files were left behind: %v", leftovers)
	}
}

func TestSharedCopyRefusesADirectoryThatLinksOutOfTheHome(t *testing.T) {
	r := newShareRun(t)
	if err := os.Symlink(r.outside, filepath.Join(r.home, ".config")); err != nil {
		t.Fatal(err)
	}
	before, _ := os.Stat(r.outside)

	_, err := r.run(t)
	if err == nil || !strings.Contains(err.Error(), "reaches outside the home of") {
		t.Fatalf("got %v", err)
	}
	if _, err := os.Stat(filepath.Join(r.outside, "gh")); !os.IsNotExist(err) {
		t.Fatalf("it created a directory outside the home: %v", err)
	}
	after, _ := os.Stat(r.outside)
	if after.Mode() != before.Mode() {
		t.Fatalf("it changed the mode outside the home: %v -> %v", before.Mode(), after.Mode())
	}
}

func TestSharedCopyRefusesALoginFileThatIsASymlink(t *testing.T) {
	r := newShareRun(t)
	victim := filepath.Join(r.outside, "shadow")
	if err := os.WriteFile(victim, []byte("root:hash\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(r.home, ".config", "gh")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, filepath.Join(dir, "hosts.yml")); err != nil {
		t.Fatal(err)
	}

	_, err := r.run(t)
	if err == nil || !strings.Contains(err.Error(), "reaches outside the home of") {
		t.Fatalf("got %v", err)
	}
	if got, _ := os.ReadFile(victim); string(got) != "root:hash\n" {
		t.Fatalf("the linked file was written: %q", got)
	}
	if info, _ := os.Stat(victim); info.Mode().Perm() != 0o644 {
		t.Fatalf("the linked file's mode changed: %v", info.Mode().Perm())
	}
}
