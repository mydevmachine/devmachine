package packages

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

var (
	gitRef     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`)
	scpAddress = regexp.MustCompile(`^git@[A-Za-z0-9][A-Za-z0-9.-]*:[A-Za-z0-9][A-Za-z0-9._/~-]*$`)
)

// ValidName says whether name can be a package's name, which is also its
// folder: it never leads anywhere else.
func ValidName(name string) bool { return packageName.MatchString(name) }

// ParseGitAddress splits <address>[@<ref>]. Only an https:// address or a
// git@host:path one is accepted: anything else is a local path or a transport
// that runs a program, and neither belongs in a package somebody published.
func ParseGitAddress(arg string) (address, ref string, err error) {
	var prefix string
	switch {
	case strings.HasPrefix(arg, "https://"):
		prefix = "https://"
	case strings.HasPrefix(arg, "git@"):
		prefix = "git@"
	default:
		return "", "", fmt.Errorf("%q: a package is installed from an https:// or git@ address, for example https://example.com/alice/tools.git", arg)
	}
	rest := strings.TrimPrefix(arg, prefix)
	if prefix == "https://" {
		if host, _, _ := strings.Cut(rest, "/"); strings.Contains(host, "@") {
			return "", "", fmt.Errorf("%q: put no user name or token in the address: git asks for them, or uses your SSH key with a git@ address", arg)
		}
	}
	if i := strings.LastIndex(rest, "@"); i >= 0 {
		rest, ref = rest[:i], rest[i+1:]
		if !gitRef.MatchString(ref) || strings.Contains(ref, "..") {
			return "", "", fmt.Errorf("ref %q: write a tag, a branch or a commit, in letters, digits, dots, dashes, underscores and slashes", ref)
		}
	}
	address = prefix + rest
	if prefix == "git@" {
		if !scpAddress.MatchString(address) || strings.Contains(address, "..") {
			return "", "", fmt.Errorf("%q: write it like git@example.com:alice/tools.git", address)
		}
		return address, ref, nil
	}
	u, err := url.Parse(address)
	if err != nil || u.Host == "" || strings.Trim(u.Path, "/") == "" || u.RawQuery != "" || u.Fragment != "" {
		return "", "", fmt.Errorf("%q: write it like https://example.com/alice/tools.git", address)
	}
	return address, ref, nil
}
