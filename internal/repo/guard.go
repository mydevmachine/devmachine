// Package repo turns a configuration directory into a git repository, and
// guards every commit it makes so a private key or a secret never reaches one.
package repo

import (
	"strings"
)

// IgnoreHeading explains why the file exists, so a later reader does not
// mistake it for something to prune.
const IgnoreHeading = "# Written by `devmachine setup git`. Everything here is a private key, a\n" +
	"# secret, a record of which host was touched, or a package fetch left\n" +
	"# half done. None of it belongs in a repository, not even a private one.\n"

// Ignored is what a configuration directory's .gitignore must hold.
func Ignored() []string {
	return []string{"keys/", "secrets.json", "cache/", "history.log", "*.env", "state/", "packages/.install-*"}
}

// GitIgnore renders the file.
func GitIgnore() string {
	var b strings.Builder
	b.WriteString(IgnoreHeading)
	for _, p := range Ignored() {
		b.WriteString(p)
		b.WriteString("\n")
	}
	return b.String()
}

// Unsafe returns the tracked paths that must never have been tracked.
//
// An empty slice means the tree is safe to commit. This is a plain path
// match, not a content scan: a scanner that looks for things that resemble
// tokens misses a key format it has not seen, and this list is closed because
// the CLI wrote every one of these paths itself.
func Unsafe(paths []string) []string {
	var out []string
	for _, p := range paths {
		if isUnsafe(p) {
			out = append(out, p)
		}
	}
	return out
}

func isUnsafe(p string) bool {
	switch {
	case strings.HasPrefix(p, "keys/"):
		return !strings.HasSuffix(p, ".pub")
	case p == "secrets.json":
		return true
	case strings.HasPrefix(p, "cache/"):
		return true
	case p == "history.log":
		return true
	case strings.HasSuffix(p, ".env"):
		return true
	case strings.HasPrefix(p, "packages/.install-"):
		return true
	default:
		return false
	}
}
