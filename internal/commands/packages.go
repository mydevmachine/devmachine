package commands

import (
	"fmt"
	"path/filepath"

	"github.com/mydevmachine/devmachine/internal/config"
	"github.com/mydevmachine/devmachine/internal/dns"
	"github.com/mydevmachine/devmachine/internal/packages"
	"github.com/mydevmachine/devmachine/internal/provision"
	"github.com/spf13/cobra"
)

func newPackagesCmd(opts *options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "packages",
		Short: "The recipes a machine and its workspaces are built from",
	}
	cmd.AddCommand(
		newPackagesListCmd(opts),
		newPackagesAddCmd(opts),
		newPackagesRmCmd(opts),
		newPackagesInstallCmd(opts),
		newPackagesUpdateCmd(opts),
		newPackagesRemoveCmd(opts),
		newPackagesNewCmd(opts),
		newPackagesValidateCmd(opts),
		newPackagesSchemaCmd(opts),
		newPackagesHelpCmd(opts),
		newPackagesPinCmd(opts),
		newPackagesOutdatedCmd(opts),
	)
	return cmd
}

func newPackagesHelpCmd(opts *options) *cobra.Command {
	var asJSON bool

	c := &cobra.Command{
		Use:   "help <name>",
		Short: "Ask an installed package what it accepts",
		Long: "The package is the source of truth about itself: nothing this " +
			"prints is written in the CLI or in a document somebody has to keep " +
			"in sync.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]

			tgt, err := machineTarget(opts)
			if err != nil {
				return err
			}
			dir, _, err := config.Dir(opts.configDir)
			if err != nil {
				return err
			}
			client, _, err := dialAdmin(cmd.Context(), tgt.machine)
			if err != nil {
				return err
			}
			defer client.Close()

			base, err := provision.Base(tgt.machine)
			if err != nil {
				return err
			}
			ext, err := dns.Any(dir, tgt.machine.Name, "", base, name, client)
			if err != nil {
				return err
			}

			commands, err := ext.Help(cmd.Context())
			if err != nil {
				return err
			}

			if asJSON || opts.format == formatJSON {
				return writeJSON(cmd.OutOrStdout(), struct {
					Name     string        `json:"name"`
					Commands []dns.Command `json:"commands"`
				}{name, commands})
			}

			for _, cmdRow := range commands {
				cmd.Printf("%-16s %s\n", cmdRow.Name, cmdRow.Summary)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "print the answer as JSON")
	return c
}

func newPackagesNewCmd(opts *options) *cobra.Command {
	var scope, kind, into string

	c := &cobra.Command{
		Use:   "new <name>",
		Short: "Write a new package that already validates",
		Long: "The skeleton installs something real rather than carrying a " +
			"comment telling you where to type. It is a starting point you " +
			"edit, and `packages validate` is what says when it is right.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			dir := filepath.Join(into, name)
			if err := packages.WriteSkeleton(dir, name, scope, kind); err != nil {
				return err
			}
			if opts.format == formatJSON {
				return writeJSON(cmd.OutOrStdout(), struct {
					Path  string `json:"path"`
					Name  string `json:"name"`
					Scope string `json:"scope"`
					Kind  string `json:"kind,omitempty"`
				}{dir, name, scope, kind})
			}
			cmd.Printf("wrote %s\n", dir)
			return nil
		},
	}
	c.Flags().StringVar(&scope, "scope", packages.ScopeMachine,
		fmt.Sprintf("where it is installed: %s or %s", packages.ScopeMachine, packages.ScopeWorkspace))
	c.Flags().StringVar(&kind, "kind", "", `the contract its entrypoint answers, for example "dns"`)
	c.Flags().StringVar(&into, "into", ".", "the directory to write the package into")
	return c
}

func newPackagesValidateCmd(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "validate <dir>",
		Short: "Check a package, and say what is wrong and where",
		Long: "Every problem is reported at once, not the first: correcting " +
			"one at a time is four round trips for one answer's worth of work.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := args[0]
			problems, err := packages.Validate(dir)
			if err != nil {
				return err
			}
			warnings, err := packages.Warnings(dir)
			if err != nil {
				return err
			}

			if opts.format == formatJSON {
				if err := writeJSON(cmd.OutOrStdout(), struct {
					OK       bool               `json:"ok"`
					Package  string             `json:"package"`
					Problems []packages.Problem `json:"problems"`
					Warnings []packages.Problem `json:"warnings,omitempty"`
				}{len(problems) == 0, dir, problems, warnings}); err != nil {
					return err
				}
			} else {
				for _, p := range problems {
					cmd.Printf("%s%c%s\n", dir, filepath.Separator, p.Error())
				}
				for _, w := range warnings {
					cmd.Printf("warning: %s%c%s\n", dir, filepath.Separator, w.Error())
				}
				switch {
				case len(problems) == 0 && len(warnings) > 0:
					cmd.Printf("%s is fine, with %d warning(s)\n", dir, len(warnings))
				case len(problems) == 0:
					cmd.Printf("%s is fine\n", dir)
				}
			}

			if len(problems) > 0 {
				return fmt.Errorf("%s has %d problem(s)", dir, len(problems))
			}
			return nil
		},
	}
}

func newPackagesSchemaCmd(opts *options) *cobra.Command {
	var asJSON bool

	c := &cobra.Command{
		Use:   "schema",
		Short: "Print the package.yml format this CLI reads",
		Long: "Whoever writes a recipe asks the binary what the format is, " +
			"rather than a page that drifts the moment the format changes.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			schema := packages.Schema()
			if asJSON || opts.format == formatJSON {
				return writeJSON(cmd.OutOrStdout(), schema)
			}

			cmd.Printf("formats read: %v\n\n", schema.Formats)
			cmd.Printf("%-16s %-9s %s\n", "field", "required", "what it is")
			for _, f := range schema.Fields {
				required := "no"
				if f.Required {
					required = "yes"
				}
				cmd.Printf("%-16s %-9s %s\n", f.Name, required, f.Summary)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "print the format as JSON")
	return c
}
