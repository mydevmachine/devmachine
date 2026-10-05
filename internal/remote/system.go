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

// System is what a machine runs, as far as setting it up is concerned.
type System struct {
	// Kernel is what `uname -s` says: "Linux", "Darwin".
	Kernel string
	// ID is os-release's ID on Linux, such as "debian" or "arch".
	ID string
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

// UnsupportedSystemError is a machine this CLI does not set up. It is found
// before anything on the machine is changed.
type UnsupportedSystemError struct {
	System System
}

func (e *UnsupportedSystemError) Error() string {
	names := supportedLinuxNames
	if e.System.Kernel == "Linux" {
		return fmt.Sprintf("this CLI does not set up %q yet: it supports %s and %s",
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
		if !SupportedLinux(s.ID) {
			return s, &UnsupportedSystemError{System: s}
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
