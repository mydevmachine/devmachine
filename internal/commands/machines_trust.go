package commands

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/mydevmachine/devmachine/internal/config"
	"github.com/mydevmachine/devmachine/internal/hostkeys"
	"github.com/mydevmachine/devmachine/internal/repo"
	"github.com/spf13/cobra"
	"golang.org/x/crypto/ssh/knownhosts"
)

type trustResult struct {
	Machine              string `json:"machine"`
	Address              string `json:"address"`
	Status               string `json:"status"`
	KeyType              string `json:"key_type"`
	CurrentKeyType       string `json:"current_key_type,omitempty"`
	CurrentFingerprint   string `json:"current_fingerprint,omitempty"`
	PresentedFingerprint string `json:"presented_fingerprint"`
	Check                bool   `json:"check"`
	Changed              bool   `json:"changed"`
	// Fix is the command that resolves a changed or missing key, pinned with
	// --expect to the key that was just presented. It is a suggestion for a
	// person or an app to run after checking the key.
	Fix string `json:"fix,omitempty"`
	// Verify prints the same key's fingerprint on the machine itself, to run
	// from a console that does not go through this SSH connection.
	Verify string `json:"verify,omitempty"`
}

type trustOptions struct {
	check   bool
	replace bool
	yes     bool
	expect  string
}

func newMachinesTrustCmd(opts *options) *cobra.Command {
	var flags trustOptions
	cmd := &cobra.Command{
		Use:   "trust [name]",
		Short: "Inspect or deliberately update a machine's SSH host key",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, _, err := config.Dir(opts.configDir)
			if err != nil {
				return err
			}
			name, err := trustMachineName(args, opts.machine)
			if err != nil {
				return err
			}
			return runMachinesTrust(cmd.Context(), dir, name, cmd.InOrStdin(), cmd.OutOrStdout(), opts.format, flags)
		},
	}
	cmd.Flags().BoolVar(&flags.check, "check", false, "compare the presented key without writing")
	cmd.Flags().BoolVar(&flags.replace, "replace", false, "allow a deliberately changed host key to be replaced")
	cmd.Flags().BoolVar(&flags.yes, "yes", false, "update the local trust store without asking")
	cmd.Flags().StringVar(&flags.expect, "expect", "", "write only the host key with this SHA256 fingerprint, of any of its key types")
	return cmd
}

func trustMachineName(args []string, flag string) (string, error) {
	if len(args) == 1 && flag != "" && args[0] != flag {
		return "", fmt.Errorf("the positional name %q and --machine %q select different machines", args[0], flag)
	}
	if len(args) == 1 {
		return args[0], nil
	}
	return flag, nil
}

func runMachinesTrust(ctx context.Context, dir, name string, in io.Reader, out io.Writer, format string, flags trustOptions) error {
	cfg, err := config.Load(dir)
	if err != nil {
		return err
	}
	machine, err := cfg.Machine(name)
	if err != nil {
		return err
	}
	if err := requiresAddress(machine, "machines trust"); err != nil {
		return err
	}
	presented, address, err := scanHostKey(ctx, machine)
	if err != nil {
		return err
	}
	presented, err = hostKeyForExpected(ctx, machine, presented, address, flags.expect)
	if err != nil {
		return err
	}
	store, err := hostkeys.Open(machine.KnownHostsFile)
	if err != nil {
		return err
	}

	result := trustResult{
		Machine:              machine.Name,
		Address:              address,
		KeyType:              presented.Type(),
		PresentedFingerprint: hostkeys.Fingerprint(presented),
		Check:                flags.check,
	}
	current, keyErr := store.Key(machine.Name, machine.Port)
	switch {
	case keyErr == nil && bytes.Equal(current.Marshal(), presented.Marshal()):
		result.Status = "matching"
		result.CurrentFingerprint = hostkeys.Fingerprint(current)
		return reportTrust(out, format, result)

	case keyErr == nil:
		result.Status = "changed"
		result.CurrentKeyType = current.Type()
		result.CurrentFingerprint = hostkeys.Fingerprint(current)
		result.suggest("devmachine machines trust " + machine.Name + " --replace")
		if flags.check {
			return reportTrust(out, format, result)
		}
		if !flags.replace {
			return fmt.Errorf("host key changed for machine %q at %s: trusted %s, presented %s; this can mean an attack, a wrong address, or a deliberate rebuild; verify it before using --replace",
				machine.Name, address, result.CurrentFingerprint, result.PresentedFingerprint)
		}
		if err := expectFingerprint(flags.expect, result); err != nil {
			return err
		}
		if !flags.yes {
			ok, err := confirmHostKey(in, out, fmt.Sprintf("Replace the trusted host key for %s from %s to %s?", machine.Name, result.CurrentFingerprint, result.PresentedFingerprint))
			if err != nil {
				return err
			}
			if !ok {
				return errDeclined
			}
		}
		if err := store.Put(machine.Name, machine.Port, presented); err != nil {
			return err
		}
		result.Changed = true
		result.clearSuggestion()
		repo.AutoCommit(ctx, dir, "chore(config): replace host key for "+machine.Name)
		return reportTrust(out, format, result)

	default:
		var unknown *knownhosts.KeyError
		if !errors.As(keyErr, &unknown) || len(unknown.Want) != 0 {
			return keyErr
		}
		result.Status = "missing"
		result.suggest("devmachine machines trust " + machine.Name)
		if flags.check {
			return reportTrust(out, format, result)
		}
		if err := expectFingerprint(flags.expect, result); err != nil {
			return err
		}
		if !flags.yes {
			ok, err := confirmHostKey(in, out, fmt.Sprintf("Trust %s host key %s for %s? Compare it through another trusted channel first.", presented.Type(), result.PresentedFingerprint, machine.Name))
			if err != nil {
				return err
			}
			if !ok {
				return errDeclined
			}
		}
		if err := store.Put(machine.Name, machine.Port, presented); err != nil {
			return err
		}
		result.Changed = true
		result.clearSuggestion()
		repo.AutoCommit(ctx, dir, "chore(config): trust host key for "+machine.Name)
		return reportTrust(out, format, result)
	}
}

// suggest fills in the fix for a key that is not trusted yet, pinned to the
// key just presented, and the console command that verifies it.
func (r *trustResult) suggest(command string) {
	r.Fix = command + " --expect " + r.PresentedFingerprint
	r.Verify = hostKeyVerifyCommand(r.KeyType)
}

func (r *trustResult) clearSuggestion() {
	r.Fix = ""
	r.Verify = ""
}

// expectFingerprint refuses to write a key other than the one the operator
// verified: a second scan can meet a different server than the first.
func expectFingerprint(expect string, result trustResult) error {
	if expect == "" || expect == result.PresentedFingerprint {
		return nil
	}
	return fmt.Errorf("machine %q at %s presented %s, not the expected %s; nothing was written",
		result.Machine, result.Address, result.PresentedFingerprint, expect)
}

// hostKeyVerifyCommand returns the command that prints the fingerprint of the
// server's own key of keyType, or "" for a type with no standard file.
func hostKeyVerifyCommand(keyType string) string {
	var file string
	switch {
	case strings.Contains(keyType, "-cert-"):
		// A certificate's fingerprint is not the fingerprint of any file
		// under /etc/ssh.
		return ""
	case keyType == "ssh-ed25519":
		file = "ed25519"
	case strings.HasPrefix(keyType, "ecdsa-sha2-"):
		file = "ecdsa"
	case keyType == "ssh-rsa" || strings.HasPrefix(keyType, "rsa-sha2-"):
		file = "rsa"
	default:
		return ""
	}
	return "ssh-keygen -lf /etc/ssh/ssh_host_" + file + "_key.pub"
}

func reportTrust(out io.Writer, format string, result trustResult) error {
	if format == formatJSON {
		return writeJSON(out, result)
	}
	fmt.Fprintf(out, "%s at %s presented %s %s\n", result.Machine, result.Address, result.KeyType, result.PresentedFingerprint)
	switch result.Status {
	case "matching":
		fmt.Fprintln(out, "the presented host key already matches the trusted key; nothing changed")
	case "missing":
		if result.Check {
			fmt.Fprintln(out, "no host key is trusted; --check wrote nothing")
		} else {
			fmt.Fprintln(out, "trusted the host key")
		}
	case "changed":
		fmt.Fprintf(out, "previous trusted key: %s %s\n", result.CurrentKeyType, result.CurrentFingerprint)
		if result.Check {
			fmt.Fprintln(out, "the host key changed; --check wrote nothing")
		} else {
			fmt.Fprintln(out, "replaced the trusted host key")
		}
	}
	if result.Fix != "" {
		if result.Verify != "" {
			fmt.Fprintf(out, "verify it from the machine's own console: %s\n", result.Verify)
		}
		fmt.Fprintf(out, "once it matches: %s\n", result.Fix)
	}
	return nil
}
