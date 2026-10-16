package commands

import (
	"fmt"
	"strings"

	"github.com/mydevmachine/devmachine/internal/config"
	"github.com/mydevmachine/devmachine/internal/repo"
	"github.com/spf13/cobra"
)

func newConfigCmd(opts *options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Inspect the configuration this run would use",
	}
	cmd.AddCommand(newConfigPathCmd(opts), newConfigShowCmd(opts), newConfigSetCmd(opts), newConfigUnsetCmd(opts))
	return cmd
}

func newConfigPathCmd(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "path",
		Short: "Print the configuration directory and the rule that chose it",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, source, err := config.Dir(opts.configDir)
			if err != nil {
				return err
			}
			if opts.format == formatJSON {
				return writeJSON(cmd.OutOrStdout(), struct {
					Path   string `json:"path"`
					Source string `json:"source"`
				}{dir, string(source)})
			}
			cmd.Printf("%s (from %s)\n", dir, source)
			return nil
		},
	}
}

func newConfigSetCmd(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Change one setting in config.yml: " + strings.Join(config.ChatKeys, ", "),
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return setConfigValue(cmd, opts, args[0], args[1])
		},
	}
}

func newConfigUnsetCmd(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "unset <key>",
		Short: "Take one setting out of config.yml, so its default applies again",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return setConfigValue(cmd, opts, args[0], "")
		},
	}
}

func setConfigValue(cmd *cobra.Command, opts *options, key, value string) error {
	dir, _, err := config.Dir(opts.configDir)
	if err != nil {
		return err
	}
	if value == "" && cmd.Name() == "set" {
		return fmt.Errorf("%s needs a value: `devmachine config unset %s` takes it out", key, key)
	}
	if err := config.SetChat(dir, key, value); err != nil {
		return err
	}
	repo.AutoCommit(cmd.Context(), dir, fmt.Sprintf("chore(config): set %s", key))
	return nil
}

func newConfigShowCmd(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Print the machines and workspaces this run would use",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, found, err := loadConfigIfAny(opts)
			if err != nil {
				return err
			}
			dir, _, err := config.Dir(opts.configDir)
			if err != nil {
				return err
			}

			if opts.format == formatJSON {
				return writeJSON(cmd.OutOrStdout(), asJSON(dir, cfg))
			}
			if !found {
				cmd.Printf("no configuration at %s yet: `devmachine setup` writes it.\n", dir)
				return nil
			}

			for _, m := range cfg.Machines {
				addresses := make([]string, 0, len(m.Hosts))
				for _, h := range m.Hosts {
					addresses = append(addresses, h.Address)
				}
				cmd.Printf("machine %s\n", m.Name)
				cmd.Printf("  hosts      %s\n", strings.Join(addresses, ", "))
				cmd.Printf("  admin      %s\n", m.User)
				cmd.Printf("  port       %d\n", m.Port)
				if m.Key != "" {
					cmd.Printf("  key        %s\n", m.Key)
				}
				if m.AgentKey != "" {
					cmd.Printf("  agent_key  %s\n", m.AgentKey)
				}
				for _, w := range cfg.WorkspacesOn(m.Name) {
					cmd.Printf("  workspace  %s (user %s)\n", w.Name, w.LinuxUser())
				}
			}
			if cfg.Domain != "" {
				cmd.Printf("domain  %s\n", cfg.Domain)
			}
			if cfg.DNSProvider != "" {
				cmd.Printf("dns     %s\n", cfg.DNSProvider)
			}
			return nil
		},
	}
}

// machineJSON and workspaceJSON are the output contract. They are separate
// from the configuration structs so a change to how config.yml is written does
// not silently change what every consumer reads.
type machineJSON struct {
	Name       string    `json:"name"`
	Hosts      []string  `json:"hosts"`
	AdminUser  string    `json:"admin_user"`
	Port       int       `json:"port"`
	Key        string    `json:"key,omitempty"`
	AgentKey   string    `json:"agent_key,omitempty"`
	Workspaces []string  `json:"workspaces"`
	Packages   []string  `json:"packages"`
	Self       bool      `json:"self,omitempty"`
	Location   string    `json:"location"`
	Platform   string    `json:"platform,omitempty"`
	Size       *sizeJSON `json:"size,omitempty"`
}

// sizeJSON is the size create-local gave a VM. Only create-local reports it:
// the configuration does not record it.
type sizeJSON struct {
	CPUs      int `json:"cpus"`
	MemoryGiB int `json:"memory_gib"`
	DiskGiB   int `json:"disk_gib"`
}

type configJSON struct {
	Machines    []machineJSON     `json:"machines"`
	Workspaces  []workspaceJSON   `json:"workspaces"`
	Defaults    defaultsJSON      `json:"defaults"`
	Credentials map[string]string `json:"credentials"`
	Domain      string            `json:"domain,omitempty"`
	DNSProvider string            `json:"dns_provider,omitempty"`
	Chat        config.Chat       `json:"chat"`
}

type defaultsJSON struct {
	Workspace []string `json:"workspace"`
}

// credentialsOrNone keeps a credentials answer an object in the JSON, never
// null.
func credentialsOrNone(answers map[string]string) map[string]string {
	if answers == nil {
		return map[string]string{}
	}
	return answers
}

type workspaceJSON struct {
	Name    string `json:"name"`
	Machine string `json:"machine"`
	User    string `json:"user"`
}

func asJSON(dir string, cfg config.Config) configJSON {
	out := configJSON{
		Machines: []machineJSON{}, Workspaces: []workspaceJSON{},
		Defaults:    defaultsJSON{Workspace: onOrNone(cfg.Defaults.Workspace)},
		Credentials: credentialsOrNone(cfg.Credentials),
		Domain:      cfg.Domain, DNSProvider: cfg.DNSProvider,
		Chat: config.Chat{
			DefaultWorkspace: cfg.ChatWorkspace(), Folder: cfg.ChatFolder(), Harness: cfg.ChatHarness(),
		},
	}

	for _, m := range cfg.Machines {
		addresses := make([]string, 0, len(m.Hosts))
		for _, h := range m.Hosts {
			addresses = append(addresses, h.Address)
		}
		names := []string{}
		for _, w := range cfg.WorkspacesOn(m.Name) {
			names = append(names, w.Name)
		}
		out.Machines = append(out.Machines, machineJSON{
			Name: m.Name, Hosts: addresses, AdminUser: m.User,
			Port: m.Port, Key: m.Key, AgentKey: m.AgentKey, Workspaces: names,
			Packages: onOrNone(m.Packages), Self: m.Self, Location: m.EffectiveLocation(),
			Platform: knownPlatform(dir, m),
		})
	}

	for _, w := range cfg.Workspaces {
		machine := w.Machine
		if machine == "" && len(cfg.Machines) == 1 {
			machine = cfg.Machines[0].Name
		}
		out.Workspaces = append(out.Workspaces, workspaceJSON{
			Name: w.Name, Machine: machine, User: w.LinuxUser(),
		})
	}
	return out
}
