package commands

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/mydevmachine/devmachine/internal/config"
	"github.com/mydevmachine/devmachine/internal/credentials"
	"github.com/mydevmachine/devmachine/internal/packages"
	"github.com/mydevmachine/devmachine/internal/repo"
	"github.com/spf13/cobra"
)

// openStore is the seam a test replaces so a listing never reaches the
// network.
var openStore = packages.Open

// sourceMissing is what `packages list` prints for a name a target asks for
// and nothing provides.
//
// Leaving it out of the listing would make `sync` the first thing that ever
// mentions it, which is late: the listing is where somebody looks to find out
// what a typo did.
const sourceMissing = "missing"

// packageTarget is the machine or workspace a package belongs to.
type packageTarget struct {
	kind string
	name string
}

func (t packageTarget) label() string { return t.kind + " " + t.name }

// resolvePackageTarget reads the two flags that say where a package goes.
//
// Exactly one of them. With neither, the only configured machine is the
// answer, and with several machines the lookup refuses to guess rather than
// picking one.
func resolvePackageTarget(cfg config.Config, machine, workspace string) (packageTarget, error) {
	if machine != "" && workspace != "" {
		return packageTarget{}, errors.New(
			"--machine and --workspace name two different places: pass one of them, not both")
	}
	if workspace != "" {
		w, err := cfg.Workspace(workspace)
		if err != nil {
			return packageTarget{}, err
		}
		return packageTarget{kind: packages.ScopeWorkspace, name: w.Name}, nil
	}
	m, err := cfg.Machine(machine)
	if err != nil {
		return packageTarget{}, err
	}
	return packageTarget{kind: packages.ScopeMachine, name: m.Name}, nil
}

// listFor returns the target's package list, and a function that writes a new
// one back into the configuration.
func listFor(cfg *config.Config, target packageTarget) ([]string, func([]string)) {
	if target.kind == packages.ScopeWorkspace {
		for i := range cfg.Workspaces {
			if cfg.Workspaces[i].Name == target.name {
				return cfg.Workspaces[i].Packages, func(list []string) { cfg.Workspaces[i].Packages = list }
			}
		}
	}
	for i := range cfg.Machines {
		if cfg.Machines[i].Name == target.name {
			return cfg.Machines[i].Packages, func(list []string) { cfg.Machines[i].Packages = list }
		}
	}
	return nil, func([]string) {}
}

func newPackagesAddCmd(opts *options) *cobra.Command {
	return newPackagesEditCmd(opts, true)
}

func newPackagesRmCmd(opts *options) *cobra.Command {
	return newPackagesEditCmd(opts, false)
}

// newPackagesEditCmd builds `add` and `rm`, which differ in one word and one
// slice operation.
func newPackagesEditCmd(opts *options, add bool) *cobra.Command {
	var (
		workspace string
		check     bool
		yes       bool
	)

	verb, use, short := "add", "add <package>", "Put a package on a machine or a workspace"
	if !add {
		verb, use, short = "rm", "rm <package>", "Take a package off a machine or a workspace"
	}

	c := &cobra.Command{
		Use:   use,
		Short: short,
		Long: "It edits the configuration and touches no machine. `devmachine " +
			"sync` is what applies the change.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return editPackage(cmd, opts, args[0], workspace, add, check, yes)
		},
	}
	c.Flags().StringVar(&workspace, "workspace", "", "the workspace to "+verb+" it on, instead of a machine")
	c.Flags().BoolVar(&check, "check", false, "say what would change, and change nothing")
	c.Flags().BoolVar(&yes, "yes", false, "do not ask")
	return c
}

// refuseRemovingAccount keeps the one package every workspace needs. Taking it
// out of the list would change nothing, since the plan adds it back, so saying
// no is the honest answer.
func refuseRemovingAccount(name string) error {
	if name != packages.AccountPackage {
		return nil
	}
	return fmt.Errorf(
		"every workspace has the %s package, because it creates the account: it cannot be removed. "+
			"To remove the workspace itself, use `devmachine workspaces rm`", name)
}

func editPackage(cmd *cobra.Command, opts *options, name, workspace string, add, check, yes bool) error {
	dir, _, err := config.Dir(opts.configDir)
	if err != nil {
		return err
	}
	cfg, err := loadConfig(opts)
	if err != nil {
		return err
	}
	target, err := resolvePackageTarget(cfg, opts.machine, workspace)
	if err != nil {
		return err
	}
	if !add && workspace != "" {
		if err := refuseRemovingAccount(name); err != nil {
			return err
		}
	}

	current, write := listFor(&cfg, target)
	held := slices.Contains(current, name)
	if add == held {
		word := "is already on"
		if !add {
			word = "is not on"
		}
		return reportEdit(cmd, opts, name, target, false, fmt.Sprintf("%s %s %s", name, word, target.label()))
	}

	if add {
		if err := refuseForeignAdd(dir, cfg, target, name); err != nil {
			return err
		}
	}

	wanted := slices.Clone(current)
	if add {
		wanted = append(wanted, name)
	} else {
		wanted = slices.DeleteFunc(wanted, func(n string) bool { return n == name })
	}

	if check {
		return reportEdit(cmd, opts, name, target, false,
			fmt.Sprintf("would %s %s %s %s", verbFor(add), name, preposition(add), target.label()))
	}
	if !yes {
		ok, err := confirm(cmd.InOrStdin(), cmd.OutOrStdout(),
			fmt.Sprintf("%s %s %s %s?", strings.ToUpper(verbFor(add)[:1])+verbFor(add)[1:],
				name, preposition(add), target.label()))
		if err != nil {
			return err
		}
		if !ok {
			return errDeclined
		}
	}

	write(wanted)
	if err := config.Save(dir, cfg); err != nil {
		return err
	}
	repo.AutoCommit(cmd.Context(), dir, "chore(config): "+packageCommitVerb(add)+" package "+name)
	return reportEdit(cmd, opts, name, target, true,
		fmt.Sprintf("%sed %s %s %s", verbFor(add), name, preposition(add), target.label()))
}

func verbFor(add bool) string {
	if add {
		return "add"
	}
	return "remov"
}

// packageCommitVerb is what AutoCommit's message says happened, matching how
// `machines add`/`rm` and `workspaces new`/`rm` already word theirs.
func packageCommitVerb(add bool) string {
	if add {
		return "add"
	}
	return "remove"
}

func preposition(add bool) string {
	if add {
		return "to"
	}
	return "from"
}

func reportEdit(cmd *cobra.Command, opts *options, name string, target packageTarget, changed bool, line string) error {
	if opts.format == formatJSON {
		return writeJSON(cmd.OutOrStdout(), struct {
			Package string `json:"package"`
			Target  string `json:"target"`
			Changed bool   `json:"changed"`
			Message string `json:"message"`
		}{name, target.label(), changed, line})
	}
	cmd.Println(line)
	return nil
}

// packageRow is one package in the listing: it appears once, whatever how many
// targets asked for it.
type packageRow struct {
	Name     string `json:"name"`
	Scope    string `json:"scope,omitempty"`
	Source   string `json:"source"`
	Summary  string `json:"summary,omitempty"`
	Category string `json:"category,omitempty"`
	// Kind is the contract a callable package answers, such as "dns", which
	// is how a client finds the DNS providers without knowing their names.
	Kind string `json:"kind,omitempty"`
	// Platforms are the operating systems the package runs on, and an empty
	// list means any of them.
	Platforms []string `json:"platforms"`
	// Needs are the packages it brings in before itself, which is what a
	// bundle such as essentials is made of.
	Needs       []string            `json:"needs"`
	Credentials []packageCredential `json:"credentials"`
	Variables   []packageVariable   `json:"variables"`
	Installed   []string            `json:"installed_on"`
}

// packageVariable is a setting a package reads, as its manifest declares it.
// Default is the manifest's own fallback, never a value the operator set.
type packageVariable struct {
	Name    string `json:"name"`
	Summary string `json:"summary,omitempty"`
	Default any    `json:"default"`
	// Type is the one the manifest declares, or else read off the default,
	// so it is left out when there is neither.
	Type string `json:"type,omitempty"`
}

func variablesOf(m packages.Manifest) []packageVariable {
	out := make([]packageVariable, 0, len(m.Variables))
	for _, name := range slices.Sorted(maps.Keys(m.Variables)) {
		v := m.Variables[name]
		out = append(out, packageVariable{Name: name, Summary: v.Summary, Default: v.Default, Type: variableType(v)})
	}
	return out
}

func variableType(v packages.Variable) string {
	if v.Type != "" {
		return v.Type
	}
	return packages.TypeOf(v.Default)
}

// packageCredential is a credential a package declares: where its value goes,
// so a client can store it before the package is added. It never carries the
// value.
type packageCredential struct {
	Name  string `json:"name"`
	Kind  string `json:"kind"`
	Scope string `json:"scope,omitempty"`
	Env   string `json:"env,omitempty"`
	Path  string `json:"path,omitempty"`
	// Shareable is whether a run obeys `workspaces edit --share <name>=machine`
	// rather than refusing it, so a client offers that choice only where it works.
	Shareable bool `json:"shareable"`
}

func credentialsOf(m packages.Manifest) []packageCredential {
	out := make([]packageCredential, 0, len(m.Credentials))
	for _, c := range m.Credentials {
		out = append(out, packageCredential{
			Name: c.Name, Kind: c.Kind, Scope: c.Scope, Env: c.Env, Path: c.Path, Shareable: credentials.CanShare(c),
		})
	}
	return out
}

func newPackagesListCmd(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "Every package available, and which targets have it",
		Long: "It reads the recipes and the configuration together, so a " +
			"package appears once with the machines and workspaces that ask " +
			"for it.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			release, rows, err := listPackages(cmd.Context(), opts)
			if err != nil {
				return err
			}

			if opts.format == formatJSON {
				return writeJSON(cmd.OutOrStdout(), struct {
					Release  string       `json:"release"`
					Packages []packageRow `json:"packages"`
				}{release, rows})
			}

			cmd.Printf("%-16s %-10s %-8s %-28s %s\n", "NAME", "SCOPE", "SOURCE", "INSTALLED ON", "SUMMARY")
			for _, row := range rows {
				on := "none"
				if len(row.Installed) > 0 {
					on = strings.Join(row.Installed, ", ")
				}
				cmd.Printf("%-16s %-10s %-8s %-28s %s\n", row.Name, row.Scope, row.Source, on, row.Summary)
			}
			return nil
		},
	}
}

// listPackages returns the release the packages were read from, and the
// packages. With no configuration yet, that is the latest release: it is the
// one `setup` would pin, so it is what a new machine can choose from.
func listPackages(ctx context.Context, opts *options) (string, []packageRow, error) {
	dir, _, err := config.Dir(opts.configDir)
	if err != nil {
		return "", nil, err
	}
	cfg, found, err := loadConfigIfAny(opts)
	if err != nil {
		return "", nil, err
	}
	release := cfg.Packages
	if !found {
		if release, err = latestPackagesRelease(ctx); err != nil {
			return "", nil, fmt.Errorf("there is no configuration yet, so the packages come from the "+
				"latest packages release, and it could not be found: %w", err)
		}
	}
	store, err := openStore(ctx, dir, release)
	if err != nil {
		return "", nil, err
	}
	available, err := store.All()
	if err != nil {
		return "", nil, err
	}

	installed := installedOn(cfg)
	rows := make([]packageRow, 0, len(available))
	for _, found := range available {
		rows = append(rows, packageRow{
			Name:        found.Manifest.Name,
			Scope:       found.Manifest.Scope,
			Source:      found.Source,
			Summary:     found.Manifest.Summary,
			Category:    found.Manifest.Category,
			Kind:        found.Manifest.Kind,
			Platforms:   onOrNone(found.Manifest.Platforms),
			Needs:       onOrNone(found.Manifest.Needs),
			Credentials: credentialsOf(found.Manifest),
			Variables:   variablesOf(found.Manifest),
			Installed:   onOrNone(installed[found.Manifest.Name]),
		})
		delete(installed, found.Manifest.Name)
	}
	for _, name := range slices.Sorted(maps.Keys(installed)) {
		rows = append(rows, packageRow{
			Name: name, Source: sourceMissing, Platforms: []string{}, Needs: []string{},
			Credentials: []packageCredential{}, Variables: []packageVariable{}, Installed: installed[name],
		})
	}

	slices.SortFunc(rows, func(a, b packageRow) int { return strings.Compare(a.Name, b.Name) })
	return release, rows, nil
}

// onOrNone keeps `installed_on` a list in the JSON, never null.
func onOrNone(targets []string) []string {
	if targets == nil {
		return []string{}
	}
	return targets
}

// installedOn maps a package name onto the targets that ask for it, in the
// order the configuration lists them.
func installedOn(cfg config.Config) map[string][]string {
	out := map[string][]string{}
	for _, m := range cfg.Machines {
		for _, name := range m.Packages {
			out[name] = append(out[name], packageTarget{packages.ScopeMachine, m.Name}.label())
		}
	}
	for _, w := range cfg.Workspaces {
		for _, name := range w.Packages {
			out[name] = append(out[name], packageTarget{packages.ScopeWorkspace, w.Name}.label())
		}
	}
	return out
}
