package packages

import (
	"os"
	"path/filepath"
	"testing"
)

func writeBootstrapPackage(t *testing.T, dir, name, bootstrap string) {
	t.Helper()
	pkg := filepath.Join(dir, name)
	if err := os.MkdirAll(pkg, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "format: 1\nname: " + name + "\nscope: machine\n"
	if bootstrap != "" {
		body += "bootstrap: " + bootstrap + "\n"
	}
	if err := os.WriteFile(filepath.Join(pkg, FileName), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestBootstrappingFindsThePackagesThatDeclareABootstrap(t *testing.T) {
	config := t.TempDir()
	local := LocalDir(config)
	writeBootstrapPackage(t, local, MacBrew, "bin/bootstrap")
	writeBootstrapPackage(t, local, MacPorts, "bin/bootstrap")
	writeBootstrapPackage(t, local, "essentials", "")
	store := OpenCached(config, "")

	got, err := store.Bootstrapping([]string{"essentials", MacBrew, "not-there"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Manifest.Name != MacBrew {
		t.Fatalf("got %#v", got)
	}
	if want := filepath.Join(local, MacBrew, "bin", "bootstrap"); got[0].Manifest.BootstrapPath() != want {
		t.Fatalf("path %q, want %q", got[0].Manifest.BootstrapPath(), want)
	}

	both, err := store.Bootstrapping([]string{MacPorts, MacBrew})
	if err != nil || len(both) != 2 {
		t.Fatalf("got %#v, %v", both, err)
	}
}

func TestAPackageWithoutABootstrapHasNoPath(t *testing.T) {
	if got := (Manifest{Path: "/x"}).BootstrapPath(); got != "" {
		t.Fatalf("got %q", got)
	}
}
