package remote

import (
	"context"
	"fmt"
	"strings"
)

// UnameCommand names the kernel, which is the first fork: Linux has an
// os-release to read, and other systems do not.
const UnameCommand = "uname -s"

// MacVersionCommand prints the macOS version, such as 15.7.9.
const MacVersionCommand = "sw_vers -productVersion"

// KernelDarwin is what `uname -s` says on a Mac.
const KernelDarwin = "Darwin"

// MacRemoteLoginGroup is the group macOS limits SSH to when Remote Login
// allows only some users. It does not exist while Remote Login allows all.
const MacRemoteLoginGroup = "com.apple.access_ssh"

// System is what a machine runs, as far as setting it up is concerned.
type System struct {
	// Kernel is what `uname -s` says: "Linux", "Darwin".
	Kernel string
	// ID is os-release's ID on Linux, such as "debian" or "arch".
	ID string
	// Like is the supported distribution a derivative is set up as, read from
	// os-release's ID_LIKE. It is empty when ID itself is supported.
	Like string
	// Version is the macOS version on a Mac, such as "15.7.9".
	Version string
}

// MacOS reports whether the machine is a Mac.
func (s System) MacOS() bool { return s.Kernel == KernelDarwin }

// String is the name a person reads: the distribution on Linux, "macos" and
// its version on a Mac, the kernel anywhere else.
func (s System) String() string {
	switch {
	case s.ID != "":
		return s.ID
	case s.MacOS() && s.Version != "":
		return "macos " + s.Version
	case s.MacOS():
		return "macos"
	}
	return s.Kernel
}

// Derivative reports whether the machine is set up as the distribution it
// says it is based on, rather than as itself.
func (s System) Derivative() bool { return s.Like != "" }

// Base is the supported distribution the machine is set up as.
func (s System) Base() string {
	if s.Derivative() {
		return s.Like
	}
	return s.ID
}

// Describe is String, plus a warning when the machine is a derivative.
func (s System) Describe() string {
	if !s.Derivative() {
		return s.String()
	}
	return fmt.Sprintf("%s, which is based on %s: it is set up the %s way, but devmachine is not tested on it",
		s.ID, s.Like, s.Like)
}

// supportedLinuxNames is how the refusals name the distributions
// ansibleInstall knows. archarm is Arch, so it is not named on its own.
var supportedLinuxNames = []string{"debian", "ubuntu", "arch"}

// SupportedLinux reports whether this CLI sets up a Linux with that
// os-release ID. The install table decides: a distribution is supported
// exactly when there is a way to put Ansible on it.
func SupportedLinux(id string) bool {
	_, ok := ansibleInstall[id]
	return ok
}

// LinuxBase is the supported distribution a Linux with that os-release ID
// and ID_LIKE is set up as, or "" when there is none.
//
// ID_LIKE is read only when ID is not supported. A derivative such as Manjaro
// or Linux Mint keeps its base's package manager and package names, so it is
// set up that way; nobody tested it, and it says so. ID_LIKE lists the
// closest base first, so Linux Mint's "ubuntu debian" is set up as Ubuntu.
func LinuxBase(id, idLike string) string {
	if SupportedLinux(id) {
		return id
	}
	for _, like := range strings.Fields(strings.Trim(idLike, `"'`)) {
		if SupportedLinux(like) {
			return like
		}
	}
	return ""
}

// UnsupportedSystemError is a machine this CLI does not set up. It is found
// before anything on the machine is changed.
type UnsupportedSystemError struct {
	System System
}

func (e *UnsupportedSystemError) Error() string {
	names := supportedLinuxNames
	if e.System.Kernel == "Linux" {
		return fmt.Sprintf("this CLI does not set up %q yet: it supports %s and %s, and systems based on them",
			e.System.ID, strings.Join(names[:len(names)-1], ", "), names[len(names)-1])
	}
	return fmt.Sprintf("%q is not a system this CLI sets up: it supports Linux (%s) and macOS",
		e.System.Kernel, strings.Join(names, ", "))
}

// DetectSystem asks the machine what it runs, and refuses one this CLI does
// not set up. It only reads.
func DetectSystem(ctx context.Context, c Client) (System, error) {
	kernel, err := c.Run(ctx, UnameCommand)
	if err != nil {
		return System{}, fmt.Errorf("running uname -s to find out which system this is: %w", err)
	}
	s := System{Kernel: strings.TrimSpace(kernel)}

	switch s.Kernel {
	case "Linux":
		release, err := c.Run(ctx, OSReleaseCommand)
		if err != nil {
			return System{}, fmt.Errorf("reading /etc/os-release to find out which distribution this is: %w", err)
		}
		s.ID = OSReleaseID(release)
		base := LinuxBase(s.ID, osReleaseValue(release, "ID_LIKE"))
		if base == "" {
			return s, &UnsupportedSystemError{System: s}
		}
		if base != s.ID {
			s.Like = base
		}
		return s, nil
	case KernelDarwin:
		version, err := c.Run(ctx, MacVersionCommand)
		if err != nil {
			return System{}, fmt.Errorf("running sw_vers to find out which macOS this is: %w", err)
		}
		s.Version = strings.TrimSpace(version)
		return s, nil
	default:
		return s, &UnsupportedSystemError{System: s}
	}
}
