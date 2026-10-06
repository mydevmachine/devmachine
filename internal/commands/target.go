package commands

import (
	"github.com/mydevmachine/devmachine/internal/config"
)

// target is where a command acts: a machine, and the user to log in as.
//
// A machine-level command (stats, doctor) logs in as the machine's admin. A
// workspace command logs in as that workspace's account. Resolving both
// to the same shape keeps every command's body the same.
type target struct {
	machine   config.Machine
	user      string
	workspace string
}

// machineTarget resolves --machine, for a command that acts on the server.
func machineTarget(opts *options) (target, error) {
	cfg, err := loadConfig(opts)
	if err != nil {
		return target{}, err
	}
	m, err := cfg.Machine(opts.machine)
	if err != nil {
		return target{}, err
	}
	return target{machine: m}, nil
}

// workspaceTarget resolves a workspace name to the machine it lives on.
//
// With no name it falls back to the machine itself, so `devmachine ssh` still
// means "the server" and `devmachine ssh alice` means "alice's environment".
func workspaceTarget(opts *options, name string) (target, error) {
	if name == "" {
		return machineTarget(opts)
	}
	cfg, err := loadConfig(opts)
	if err != nil {
		return target{}, err
	}
	m, w, err := cfg.MachineFor(name)
	if err != nil {
		return target{}, err
	}
	return target{machine: m, user: w.LinuxUser(), workspace: w.Name}, nil
}

// label is how the command log names this target.
func (t target) label() string {
	if t.workspace != "" {
		return "workspace " + t.workspace
	}
	return "machine " + t.machine.Name
}

// login is the account to authenticate as.
func (t target) login() string {
	if t.user != "" {
		return t.user
	}
	return t.machine.User
}
