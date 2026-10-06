package remote

import (
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
)

func lookUpHome(t *testing.T, name string, path string) string {
	t.Helper()
	cmd := exec.Command("sh", "-c", "set -eu\n"+HomeLookup+"\nprintf '%s' \"$home\"")
	cmd.Env = append(os.Environ(), "user="+name)
	if path != "" {
		cmd.Env = append(cmd.Env, "PATH="+path)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	return string(out)
}

// TestHomeLookupAsksGetentWhereThereIsOne: on Linux the lookup is exactly the
// getent it always was.
func TestHomeLookupAsksGetentWhereThereIsOne(t *testing.T) {
	bin := t.TempDir()
	fake := "#!/bin/sh\n[ \"$1 $2\" = \"passwd alice\" ] && echo 'alice:x:1000:1000::/home/alice:/bin/bash'\n"
	if err := os.WriteFile(filepath.Join(bin, "getent"), []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := lookUpHome(t, "alice", bin+":/usr/bin:/bin"); got != "/home/alice" {
		t.Fatalf("got %q", got)
	}
	if got := lookUpHome(t, "nobody-here", bin+":/usr/bin:/bin"); got != "" {
		t.Fatalf("a missing account got %q", got)
	}
}

// TestHomeLookupWorksOnThisComputer runs the lookup for the account running
// the test: getent on Linux, dscl on a Mac, which has no getent.
func TestHomeLookupWorksOnThisComputer(t *testing.T) {
	me, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	if got := lookUpHome(t, me.Username, ""); got != me.HomeDir {
		t.Fatalf("got %q, want %q", got, me.HomeDir)
	}
	if got := lookUpHome(t, "devmachine-no-such-account", ""); got != "" {
		t.Fatalf("a missing account got %q", got)
	}
}

// TestHomeLookupSurvivesAnsibleTemplating: sharing.go's script goes through
// Ansible's templating inside single quotes.
func TestHomeLookupSurvivesAnsibleTemplating(t *testing.T) {
	for _, unsafe := range []string{"'", "{{", "{%", "{#", "%"} {
		if strings.Contains(HomeLookup, unsafe) {
			t.Fatalf("HomeLookup holds %q", unsafe)
		}
	}
}
