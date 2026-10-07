package packages

import (
	"strings"
	"testing"
)

func TestParseGitAddress(t *testing.T) {
	good := []struct{ arg, address, ref string }{
		{"https://example.com/alice/tools.git", "https://example.com/alice/tools.git", ""},
		{"https://example.com/alice/tools.git@v1", "https://example.com/alice/tools.git", "v1"},
		{"https://example.com/alice/tools@feature/x", "https://example.com/alice/tools", "feature/x"},
		{"git@example.com:alice/tools.git", "git@example.com:alice/tools.git", ""},
		{"git@example.com:alice/tools.git@0123abc", "git@example.com:alice/tools.git", "0123abc"},
	}
	for _, tc := range good {
		address, ref, err := ParseGitAddress(tc.arg)
		if err != nil || address != tc.address || ref != tc.ref {
			t.Errorf("%s: got %q %q %v", tc.arg, address, ref, err)
		}
	}
	bad := map[string]string{
		"http://example.com/a.git":                  "an https:// or git@ address",
		"file:///tmp/a":                             "an https:// or git@ address",
		"/tmp/a":                                    "an https:// or git@ address",
		"ext::sh -c id":                             "an https:// or git@ address",
		"https://token@example.com/a.git":           "no user name or token",
		"https://example.com/a.git@--upload-pack=x": "ref",
		"https://example.com/a.git@a..b":            "ref",
		"https://example.com/a.git@":                "ref",
		"git@-oProxyCommand=x:a/b":                  "git@example.com:alice/tools.git",
		"git@example.com:../a":                      "git@example.com:alice/tools.git",
		"https://example.com":                       "https://example.com/alice/tools.git",
		"https://example.com/a.git?x=1":             "https://example.com/alice/tools.git",
	}
	for arg, want := range bad {
		if _, _, err := ParseGitAddress(arg); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: want an error containing %q, got %v", arg, want, err)
		}
	}
}

func TestValidName(t *testing.T) {
	for name, want := range map[string]bool{"alice-tools": true, "a_b": true, "../x": false, "a/b": false, "": false, "Tools": false} {
		if ValidName(name) != want {
			t.Errorf("%q: got %v", name, !want)
		}
	}
}
