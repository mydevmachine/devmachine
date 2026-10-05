package provision

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func write(t *testing.T, path, body string) {
	t.Helper()
	writeMode(t, path, body, 0o644)
}

func writeMode(t *testing.T, path, body string, mode fs.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func entriesInTarball(t *testing.T, r io.Reader) map[string]*tar.Header {
	t.Helper()

	unzipped, err := gzip.NewReader(r)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]*tar.Header{}
	archive := tar.NewReader(unzipped)
	for {
		header, err := archive.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		out[header.Name] = header
	}
	return out
}

func namesInTarball(t *testing.T, r io.Reader) []string {
	t.Helper()

	var names []string
	for name := range entriesInTarball(t, r) {
		names = append(names, name)
	}
	return names
}

func modeInTarball(t *testing.T, r io.Reader, name string) fs.FileMode {
	t.Helper()

	header, ok := entriesInTarball(t, r)[name]
	if !ok {
		t.Fatalf("the archive has no %q", name)
	}
	return header.FileInfo().Mode()
}

func TestTarCarriesGeneratedFilesAndWholeDirectories(t *testing.T) {
	source := t.TempDir()
	write(t, filepath.Join(source, "tasks", "main.yml"), "---\n- name: x\n  package: {name: git}\n")

	reader, err := Tar(
		map[string][]byte{"ansible.cfg": []byte("[defaults]\n")},
		map[string]string{"roles/git": source},
	)
	if err != nil {
		t.Fatal(err)
	}

	got := namesInTarball(t, reader)
	for _, want := range []string{"ansible.cfg", "roles/git/tasks/main.yml"} {
		if !slices.Contains(got, want) {
			t.Fatalf("the archive leaves out %q: %v", want, got)
		}
	}
}

func TestTarFollowsADirectoryThatIsASymlink(t *testing.T) {
	real := t.TempDir()
	write(t, filepath.Join(real, "tasks", "main.yml"), "---\n")
	link := filepath.Join(t.TempDir(), "base")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}

	reader, err := Tar(nil, map[string]string{"roles/base": link})
	if err != nil {
		t.Fatal(err)
	}
	if got := namesInTarball(t, reader); !slices.Contains(got, "roles/base/tasks/main.yml") {
		t.Fatalf("the linked directory's files are missing: %v", got)
	}
}

func TestTarKeepsAnExecutableBitOnAScript(t *testing.T) {
	source := t.TempDir()
	writeMode(t, filepath.Join(source, "files", "resume"), "#!/bin/sh\n", 0o755)

	reader, err := Tar(nil, map[string]string{"roles/base": source})
	if err != nil {
		t.Fatal(err)
	}
	if mode := modeInTarball(t, reader, "roles/base/files/resume"); mode&0o111 == 0 {
		t.Fatalf("the executable bit was lost: %o", mode)
	}
}

func TestTarCarriesTheContentOfAGeneratedFile(t *testing.T) {
	reader, err := Tar(map[string][]byte{"playbook.yml": []byte("- hosts: all\n")}, nil)
	if err != nil {
		t.Fatal(err)
	}

	unzipped, err := gzip.NewReader(reader)
	if err != nil {
		t.Fatal(err)
	}
	archive := tar.NewReader(unzipped)
	header, err := archive.Next()
	if err != nil {
		t.Fatal(err)
	}
	if header.Name != "playbook.yml" {
		t.Fatalf("got %q", header.Name)
	}
	body, err := io.ReadAll(archive)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "- hosts: all\n" {
		t.Fatalf("got %q", body)
	}
}

func TestTarReportsADirectoryThatIsNotThere(t *testing.T) {
	if _, err := Tar(nil, map[string]string{"roles/base": filepath.Join(t.TempDir(), "gone")}); err == nil {
		t.Fatal("a missing directory was accepted")
	}
}
