package commands

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/mydevmachine/devmachine/internal/config"
	"github.com/mydevmachine/devmachine/internal/facts"
	"github.com/mydevmachine/devmachine/internal/packages"
	"github.com/mydevmachine/devmachine/internal/provision"
	"github.com/mydevmachine/devmachine/internal/remote"
	"github.com/mydevmachine/devmachine/internal/repo"
	"github.com/spf13/cobra"
)

// ansibleProvisioner is the real thing a sync applies with, and provisionerFor
// is the seam a test replaces so a sync runs without a machine.
func ansibleProvisioner(client remote.Client) provision.Provisioner {
	return &provision.Ansible{Client: client}
}

var provisionerFor = ansibleProvisioner

func newSyncCmd(opts *options) *cobra.Command {
	var (
		check bool
		yes   bool
		tags  []string
	)

	c := &cobra.Command{
		Use:   "sync",
		Short: "Put a machine into the state the configuration describes",
		Long: "It reads the packages each machine and workspace asks for, " +
			"sends them, and runs Ansible on the machine, streaming the " +
			"output as it arrives.\n\n" +
			"The plan is printed before anything happens. With --check the " +
			"machine reports what would change and changes nothing, and " +
			"nothing is written to the lock.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSync(cmd, opts, check, yes, tags)
		},
	}
	c.Flags().BoolVar(&check, "check", false, "a dry run: the machine reports what would change and changes nothing")
	c.Flags().BoolVar(&yes, "yes", false, "apply without asking")
	c.Flags().StringSliceVar(&tags, "tags", nil, "only the packages named, by name")
	return c
}

func runSync(cmd *cobra.Command, opts *options, check, yes bool, tags []string) error {
	prep, err := prepareSync(cmd.Context(), opts)
	if err != nil {
		return err
	}

	// In JSON the document on stdout is the contract, so the plan, the prompt
	// and the machine's own output are diagnostics and go to stderr.
	notes := cmd.OutOrStdout()
	if opts.format == formatJSON {
		notes = cmd.ErrOrStderr()
	}

	for _, line := range prep.summary {
		if _, err := fmt.Fprintln(notes, line); err != nil {
			return err
		}
	}

	if !yes && !check {
		ok, err := confirm(cmd.InOrStdin(), notes, "Apply this to "+prep.machine.Name+"?")
		if err != nil {
			return err
		}
		if !ok {
			return errDeclined
		}
	}

	result, err := prep.apply(cmd.Context(), opts, check, tags, notes)
	if err != nil {
		return err
	}
	return reportSync(cmd, opts, prep.cfg, prep.machine.Name, check, prep.summary, result)
}

// preparedSync is everything a sync works out before it reaches the machine:
// what to send, and the plan a person is shown.
type preparedSync struct {
	dir     string
	cfg     config.Config
	machine config.Machine
	store   *packages.Store
	lock    packages.Lock
	plan    packages.MachinePlan
	summary []string
}

func prepareSync(ctx context.Context, opts *options) (preparedSync, error) {
	dir, _, err := config.Dir(opts.configDir)
	if err != nil {
		return preparedSync{}, err
	}
	cfg, err := loadConfig(opts)
	if err != nil {
		return preparedSync{}, err
	}
	machine, err := cfg.Machine(opts.machine)
	if err != nil {
		return preparedSync{}, err
	}
	if machine.Self {
		if _, err := lookPath("ansible-playbook"); err != nil {
			return preparedSync{}, fmt.Errorf(
				"ansible-playbook is not on your computer: run `devmachine setup --machine %s`", machine.Name)
		}
	}

	store, err := openStore(ctx, dir, cfg.Packages)
	if err != nil {
		return preparedSync{}, err
	}
	lock, err := packages.LoadLock(dir)
	if err != nil {
		return preparedSync{}, err
	}
	plan, err := packages.ResolveMachine(store, cfg, machine, version)
	if err != nil {
		if cfg.Packages == "" {
			return preparedSync{}, fmt.Errorf("%w; no packages release is pinned, so only local packages exist: "+
				"run `devmachine packages pin` to pin the latest release", err)
		}
		return preparedSync{}, err
	}
	// Generate stays pure: it never reads the lock itself, so the previous
	// extensions come in on the plan.
	plan.PreviousExtensions = lock.Extensions[machine.Name]
	plan, failures := withUpstreams(ctx, cfg, plan, "")
	if err := validateLocalPackages(plan); err != nil {
		return preparedSync{}, err
	}
	if err := refuseShadowedOfficial(plan, store); err != nil {
		return preparedSync{}, err
	}
	if err := refuseForeignPackages(plan, machine.Name, knownPlatform(dir, machine)); err != nil {
		return preparedSync{}, err
	}
	if err := refuseMistypedPlan(plan); err != nil {
		return preparedSync{}, err
	}
	summary := provision.Summary(plan)
	for _, r := range provision.UnresolvedRoutes(plan) {
		summary = append(summary, fmt.Sprintf("warning: https://%s is left as it is on %s, since %s's address is not known: %v",
			r.Host, machine.Name, r.From, oneLine(failures[r.From])))
	}
	return preparedSync{dir: dir, cfg: cfg, machine: machine, store: store, lock: lock,
		plan: plan, summary: summary}, nil
}

func oneLine(err error) string {
	if err == nil {
		return "it could not be resolved"
	}
	return strings.Join(strings.Fields(err.Error()), " ")
}

// apply runs the plan on the machine, streaming Ansible's output to out, and
// records a real run in the lock.
func (p preparedSync) apply(ctx context.Context, opts *options, check bool, tags []string, out io.Writer) (provision.Result, error) {
	client, _, err := dial(ctx, p.machine, "")
	if err != nil {
		return provision.Result{}, err
	}
	defer client.Close()
	observed, ok := observedForSync(ctx, p.dir, p.machine.Name, client)
	if ok {
		if err := refuseForeignPackages(p.plan, p.machine.Name, observed.Platform()); err != nil {
			return provision.Result{}, err
		}
	}

	result, runErr := provisionerFor(client).Apply(ctx, p.plan, provision.Options{
		Check: check, Tags: tags, Out: out, AnsiblePlaybook: ansiblePlaybookFor(p.machine, observed),
	})
	record(opts, target{machine: p.machine}, syncCommandLine(check, tags), runErr == nil)
	if runErr != nil {
		return result, runErr
	}

	// A dry run changed nothing, so recording it as applied would make the
	// lock claim something nobody did.
	if !check {
		if err := packages.SaveLock(p.dir, p.lock.WithPlan(p.plan, p.store, time.Now(), tags)); err != nil {
			return result, err
		}
		repo.AutoCommit(ctx, p.dir, "chore(config): lock packages for "+p.machine.Name)
		if err := refreshAliases(p.cfg, out); err != nil {
			return result, err
		}
	}
	return result, nil
}

// refuseShadowedOfficial stops a sync when a package installed from a git
// address has a name the pinned release gained since it was installed: your
// copy of a name always wins, so it would take the official one's place on
// the machine without a word. Neither copy is picked for you.
func refuseShadowedOfficial(plan packages.MachinePlan, store *packages.Store) error {
	if store.Version() == "" {
		return nil
	}
	seen := map[string]bool{}
	for _, resolved := range append([]packages.Resolved{plan.OnMachine}, plan.Workspaces...) {
		for _, found := range resolved.Ordered {
			if found.Source != packages.SourceLocal || seen[found.Manifest.Path] {
				continue
			}
			seen[found.Manifest.Path] = true
			from := "a git address"
			origin, installed, err := packages.ReadOrigin(found.Manifest.Path)
			switch {
			case err == nil && !installed:
				continue
			case err == nil:
				from = origin.URL
			}
			name := found.Manifest.Name
			_, err = store.GetRelease(name)
			var notInRelease *packages.NotInReleaseError
			switch {
			case errors.As(err, &notInRelease):
				continue
			case err != nil:
				return fmt.Errorf("checking whether %s is an official package: %w", name, err)
			}
			return fmt.Errorf("%s is installed from %s, and packages release %s has an official %s: your copy would replace it "+
				"on %s. Take yours off with devmachine packages rm %s --machine <name> (or --workspace <name>) and delete it with "+
				"devmachine packages remove %s, or pin an earlier release with devmachine packages pin <release>",
				name, from, store.Version(), name, plan.Machine.Name, name, name)
		}
	}
	return nil
}

// validateLocalPackages checks the operator's own recipes before anything is
// sent.
//
// Only the local ones: a published package was checked when it was released,
// and a local one has never been checked by anybody.
func validateLocalPackages(plan packages.MachinePlan) error {
	var (
		lines []string
		seen  = map[string]bool{}
	)

	for _, resolved := range append([]packages.Resolved{plan.OnMachine}, plan.Workspaces...) {
		for _, found := range resolved.Ordered {
			if found.Source != packages.SourceLocal || seen[found.Manifest.Path] {
				continue
			}
			seen[found.Manifest.Path] = true

			problems, err := packages.Validate(found.Manifest.Path)
			if err != nil {
				return err
			}
			for _, p := range problems {
				lines = append(lines, fmt.Sprintf("%s: %s", found.Manifest.Path, p.Error()))
			}
		}
	}

	if len(lines) > 0 {
		return fmt.Errorf("%d problem(s) in your own packages:\n%s", len(lines), strings.Join(lines, "\n"))
	}
	return nil
}

// knownPlatform is the machine's system as package.yml names it, from what was
// last read, or "" when nothing was: the first sync reads it, so an unknown
// system is not a reason to refuse.
func knownPlatform(dir string, m config.Machine) string {
	if m.Self {
		return packages.PlatformMacOS
	}
	observed, found, err := facts.Load(dir, m.Name)
	if err != nil || !found {
		return ""
	}
	return observed.Platform()
}

// refuseForeignPackages stops a sync, before the machine is changed, on a
// package whose platforms leave out the machine's system.
func refuseForeignPackages(plan packages.MachinePlan, machine, platform string) error {
	if platform == "" {
		return nil
	}
	var (
		lines []string
		seen  = map[string]bool{}
	)
	for _, resolved := range append([]packages.Resolved{plan.OnMachine}, plan.Workspaces...) {
		for _, found := range resolved.Ordered {
			m := found.Manifest
			if len(m.Platforms) == 0 || slices.Contains(m.Platforms, platform) || seen[m.Name] {
				continue
			}
			seen[m.Name] = true
			lines = append(lines, fmt.Sprintf("package %q runs on %s; machine %q is %s",
				m.Name, strings.Join(m.Platforms, " and "), machine, platform))
		}
	}
	if len(lines) > 0 {
		return errors.New(strings.Join(lines, "\n"))
	}
	return nil
}

// syncCommandLine is what the command log records, so a line in it can be read
// back as the command somebody ran.
func syncCommandLine(check bool, tags []string) string {
	command := "sync"
	if check {
		command += " --check"
	}
	if len(tags) > 0 {
		command += " --tags " + strings.Join(tags, ",")
	}
	return command
}

func reportSync(cmd *cobra.Command, opts *options, cfg config.Config, machine string, check bool, plan []string, result provision.Result) error {
	if opts.format == formatJSON {
		return writeJSON(cmd.OutOrStdout(), struct {
			Machine string           `json:"machine"`
			Check   bool             `json:"check"`
			Plan    []string         `json:"plan"`
			Result  provision.Result `json:"result"`
			Locked  bool             `json:"locked"`
		}{machine, check, plan, result, !check})
	}

	out := cmd.OutOrStdout()
	if _, err := fmt.Fprintf(out, "\n%s: ok=%d changed=%d failed=%d\n",
		machine, result.Ok, result.Changed, result.Failed); err != nil {
		return err
	}
	if check {
		return writeLine(out, "a dry run: nothing changed, and the lock was not written")
	}

	workspaces := cfg.WorkspacesOn(machine)
	if len(workspaces) == 0 {
		return nil
	}
	if _, err := fmt.Fprintln(out, "\nReach a workspace:"); err != nil {
		return err
	}
	for _, w := range workspaces {
		for _, line := range reachLines(w.Name, cfg.SSHAliases) {
			if err := writeLine(out, "  "+line); err != nil {
				return err
			}
		}
	}
	return nil
}

func writeLine(out io.Writer, line string) error {
	_, err := fmt.Fprintln(out, line)
	return err
}

// observedForSync is a fresh read of the machine, or, when that read fails,
// what was saved before: a Mac's ansible-playbook is not on an SSH session's
// PATH, so losing the saved path would break the sync. fresh is false then.
func observedForSync(ctx context.Context, dir, machine string, client remote.Client) (observed facts.Facts, fresh bool) {
	if observed, ok := facts.Record(ctx, dir, machine, client, time.Now()); ok {
		return observed, true
	}
	saved, found, err := facts.Load(dir, machine)
	if err != nil || !found {
		return facts.Facts{}, false
	}
	return saved, false
}

// ansiblePlaybookFor is the absolute ansible-playbook a Mac reached over SSH
// is run with: its PATH has neither Homebrew nor MacPorts. Anywhere else it is
// empty, and ansible-playbook comes from PATH as it always has.
func ansiblePlaybookFor(m config.Machine, observed facts.Facts) string {
	if m.Self || observed.System != "Darwin" {
		return ""
	}
	return observed.AnsiblePlaybook
}
