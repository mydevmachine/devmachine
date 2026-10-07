package repo

import (
	"strings"
	"testing"
)

func TestGitIgnoreCoversEverySecretPath(t *testing.T) {
	got := GitIgnore()

	for _, want := range []string{"keys/", "secrets.json", "cache/", "history.log", "*.env", "state/", "packages/.install-*"} {
		if !strings.Contains(got, want) {
			t.Fatalf(".gitignore does not cover %q:\n%s", want, got)
		}
	}
}

func TestUnsafeFindsAKeyThatIsAlreadyTracked(t *testing.T) {
	// A directory that became a repository by hand, before this command
	// existed, can already be tracking the key. Adding a .gitignore now does
	// nothing for a file git is already following.
	got := Unsafe([]string{"config.yml", "keys/id_ed25519", "packages.lock"})

	if len(got) != 1 || got[0] != "keys/id_ed25519" {
		t.Fatalf("got %#v", got)
	}
}

func TestUnsafeAllowsThePublicHalfOfAKey(t *testing.T) {
	// A public key is public. Refusing it would make the guard the thing
	// people switch off.
	if got := Unsafe([]string{"keys/id_ed25519.pub"}); len(got) != 0 {
		t.Fatalf("got %#v", got)
	}
}

func TestUnsafeSaysNothingAboutACleanTree(t *testing.T) {
	if got := Unsafe([]string{"config.yml", "packages.lock", ".gitignore"}); len(got) != 0 {
		t.Fatalf("got %#v", got)
	}
}

func TestUnsafeCatchesAnEnvFileAnywhere(t *testing.T) {
	// A package stages /etc/devmachine/<name>/env, and somebody keeps a copy.
	got := Unsafe([]string{"staging/cloudflare.env"})
	if len(got) != 1 {
		t.Fatalf("got %#v", got)
	}
}
