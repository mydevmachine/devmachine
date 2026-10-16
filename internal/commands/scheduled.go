package commands

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"text/tabwriter"
	"time"

	"github.com/mydevmachine/devmachine/internal/config"
	"github.com/mydevmachine/devmachine/internal/harness"
	"github.com/mydevmachine/devmachine/internal/scheduled"
	"github.com/spf13/cobra"
)

const scheduledPackage = "scheduled"

type scheduledWorkspace struct {
	Name      string          `json:"name"`
	Machine   string          `json:"machine"`
	Installed bool            `json:"installed"`
	Tasks     []localTask     `json:"tasks"`
	Error     *scheduledError `json:"error,omitempty"`
}

func newScheduledCmd(opts *options) *cobra.Command {
	c := &cobra.Command{
		Use:   "scheduled",
		Short: "Tasks that run on their own in a workspace: a command or a prompt, on a schedule",
		Long: "A scheduled task runs a shell command, or a prompt for a coding agent, in a folder of a " +
			"workspace, on a schedule. The workspace keeps and runs its own tasks from one timer, so " +
			"they run with this computer off. The first task in a workspace sets up the scheduled package.",
	}
	c.AddCommand(
		newScheduledListCmd(opts),
		newScheduledAddCmd(opts),
		newScheduledEditCmd(opts),
		newScheduledSimpleCmd(opts, "pause", "Stop a task from running on its schedule"),
		newScheduledSimpleCmd(opts, "resume", "Let a paused task run on its schedule again"),
		newScheduledRmCmd(opts),
		newScheduledRunCmd(opts),
		newScheduledRunsCmd(opts),
		newScheduledLogCmd(opts),
		newScheduledHarnessesCmd(opts),
		newScheduledLocalCmd(),
	)
	return c
}

func scheduledWorkspaceOf(opts *options, name string) (config.Config, config.Workspace, error) {
	cfg, err := loadConfig(opts)
	if err != nil {
		return config.Config{}, config.Workspace{}, err
	}
	w, err := cfg.Workspace(name)
	return cfg, w, err
}

func isScheduledInstalled(w config.Workspace) bool {
	return slices.Contains(w.Packages, scheduledPackage)
}

func newScheduledListCmd(opts *options) *cobra.Command {
	var names []string
	c := &cobra.Command{
		Use:   "list",
		Short: "Every workspace's scheduled tasks, with their next and last run",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig(opts)
			if err != nil {
				return err
			}
			results := collectScheduled(cmd.Context(), cfg, names)
			if opts.format == formatJSON {
				return writeJSON(cmd.OutOrStdout(), struct {
					Workspaces []scheduledWorkspace `json:"workspaces"`
				}{results})
			}
			return writeScheduledTable(cmd.OutOrStdout(), results)
		},
	}
	c.Flags().StringArrayVar(&names, "workspace", nil, "only this workspace (repeatable)")
	return c
}

func collectScheduled(ctx context.Context, cfg config.Config, names []string) []scheduledWorkspace {
	var chosen []config.Workspace
	var results []scheduledWorkspace
	for _, w := range cfg.Workspaces {
		if len(names) == 0 || slices.Contains(names, w.Name) {
			chosen = append(chosen, w)
		}
	}
	for _, name := range names {
		if _, err := cfg.Workspace(name); err != nil {
			results = append(results, scheduledWorkspace{Name: name, Tasks: []localTask{},
				Error: &scheduledError{Kind: "not-configured", Message: err.Error()}})
		}
	}
	found := make([]scheduledWorkspace, len(chosen))
	var wg sync.WaitGroup
	for i, w := range chosen {
		wg.Go(func() {
			found[i] = scheduledWorkspace{Name: w.Name, Machine: machineNameOf(cfg, w),
				Installed: isScheduledInstalled(w), Tasks: []localTask{}}
			if !found[i].Installed {
				return
			}
			var reply struct {
				Tasks []localTask `json:"tasks"`
			}
			if err := callWorkspace(ctx, cfg, w, scheduledListTimeout, []string{"list"}, nil, &reply); err != nil {
				found[i].Error = asScheduledError(err)
				return
			}
			if reply.Tasks != nil {
				found[i].Tasks = reply.Tasks
			}
		})
	}
	wg.Wait()
	return append(found, results...)
}

func asScheduledError(err error) *scheduledError {
	var se *scheduledError
	if errors.As(err, &se) {
		return se
	}
	return &scheduledError{Kind: "other", Message: err.Error()}
}

func writeScheduledTable(out io.Writer, results []scheduledWorkspace) error {
	tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "WORKSPACE\tID\tNAME\tSCHEDULE\tACTION\tNEXT\tLAST")
	for _, w := range results {
		if w.Error != nil {
			fmt.Fprintf(tw, "%s\t-\t-\t-\t-\t-\terror: %s\n", w.Name, w.Error.Message)
			continue
		}
		for _, t := range w.Tasks {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", w.Name, t.ID, t.Name, t.Schedule.Describe(),
				actionLabel(t.Action), nextLabel(t), lastLabel(t.LastRun))
		}
	}
	if err := tw.Flush(); err != nil {
		return fmt.Errorf("writing the output: %w", err)
	}
	return nil
}

func actionLabel(a scheduled.Action) string {
	if a.Kind == "command" {
		return "command"
	}
	return a.Harness
}

func nextLabel(t localTask) string {
	switch {
	case t.Paused:
		return "paused"
	case t.Done:
		return "done"
	case t.NextRunAt == nil:
		return "-"
	default:
		return t.NextRunAt.Local().Format("2006-01-02 15:04")
	}
}

func lastLabel(r *scheduled.Run) string {
	if r == nil {
		return "-"
	}
	return r.Status + " " + r.StartedAt.Local().Format("2006-01-02 15:04")
}

type taskFlags struct {
	name, folder, command, harness, prompt, model, mode string
	every, at, weekday, date, timezone, timeout         string
	minute, day                                         int
	allowDangerous, yes                                 bool
}

func (f *taskFlags) register(c *cobra.Command) {
	fl := c.Flags()
	fl.StringVar(&f.name, "name", "", "what the task is called")
	fl.StringVar(&f.folder, "folder", "", "the folder it runs in, such as ~/dev/app")
	fl.StringVar(&f.command, "command", "", "a shell command to run")
	fl.StringVar(&f.harness, "harness", "", "the coding agent for a prompt: "+strings.Join(harness.Names(), ", "))
	fl.StringVar(&f.prompt, "prompt", "", "the prompt the agent runs")
	fl.StringVar(&f.model, "model", "", "the agent's model (default: the agent's own)")
	fl.StringVar(&f.mode, "permission-mode", "", "the agent's permission mode (see `devmachine scheduled harnesses`)")
	fl.StringVar(&f.every, "every", "", "once, hourly, daily, weekdays, weekly or monthly")
	fl.StringVar(&f.at, "at", "", "the time it runs, such as 09:00")
	fl.IntVar(&f.minute, "minute", 0, "the minute of the hour, for --every hourly")
	fl.StringVar(&f.weekday, "weekday", "", "mon to sun, for --every weekly")
	fl.IntVar(&f.day, "day", 0, "the day of the month, for --every monthly (a short month uses its last day)")
	fl.StringVar(&f.date, "date", "", "the date, such as 2026-10-16, for --every once")
	fl.StringVar(&f.timezone, "timezone", "", "the timezone of --at (default: this computer's)")
	fl.StringVar(&f.timeout, "timeout", "", "how long a run may take (default 30m, at most 6h)")
	fl.BoolVar(&f.allowDangerous, "allow-dangerous", false, "accept a permission mode that never asks for approval")
}

// apply writes the flags the person set onto t. For a new task every flag
// counts; for an edit, only the ones on the command line.
func (f *taskFlags) apply(c *cobra.Command, t *scheduled.Task) error {
	set := func(name string) bool { return c.Flags().Changed(name) }
	if set("command") && (set("prompt") || set("harness")) {
		return errors.New("a task runs either --command or --harness with --prompt, not both")
	}
	strs := []struct {
		flag string
		dst  *string
		val  string
	}{
		{"name", &t.Name, f.name}, {"folder", &t.Folder, f.folder}, {"timeout", &t.Timeout, f.timeout},
		{"at", &t.Schedule.At, f.at}, {"weekday", &t.Schedule.Weekday, f.weekday},
		{"date", &t.Schedule.Date, f.date}, {"timezone", &t.Schedule.Timezone, f.timezone},
		{"model", &t.Action.Model, f.model}, {"permission-mode", &t.Action.PermissionMode, f.mode},
	}
	for _, s := range strs {
		if set(s.flag) {
			*s.dst = s.val
		}
	}
	if set("every") {
		t.Schedule = scheduled.Schedule{Every: f.every, At: t.Schedule.At, Weekday: t.Schedule.Weekday,
			Day: t.Schedule.Day, Date: t.Schedule.Date, Minute: t.Schedule.Minute, Timezone: t.Schedule.Timezone}
	}
	if set("minute") {
		m := f.minute
		t.Schedule.Minute = &m
	}
	if set("day") {
		t.Schedule.Day = f.day
	}
	if set("command") {
		t.Action = scheduled.Action{Kind: "command", Command: f.command}
	}
	if set("prompt") || set("harness") {
		t.Action.Kind, t.Action.Command = "prompt", ""
		if set("prompt") {
			t.Action.Prompt = f.prompt
		}
		if set("harness") && f.harness != t.Action.Harness {
			t.Action.Harness = f.harness
			if !set("model") {
				t.Action.Model = ""
			}
			if !set("permission-mode") {
				t.Action.PermissionMode = ""
			}
		}
	}
	if t.Action.Kind == "" {
		return errors.New("say what the task runs: --command, or --harness with --prompt")
	}
	if t.Schedule.Timezone == "" {
		t.Schedule.Timezone = localTimezone()
	}
	if set("every") || set("at") || set("date") || set("day") || set("weekday") || set("minute") {
		t.Done = false
	}
	return t.Validate(f.allowDangerous)
}

// localTimezone is this computer's IANA zone name, which is what a person
// means by "at 09:00".
func localTimezone() string {
	if tz := os.Getenv("TZ"); tz != "" {
		if _, err := time.LoadLocation(tz); err == nil {
			return tz
		}
	}
	if name := time.Local.String(); name != "Local" && name != "" {
		return name
	}
	if target, err := os.Readlink("/etc/localtime"); err == nil {
		if _, zone, ok := strings.Cut(target, "zoneinfo/"); ok {
			return filepath.Clean(zone)
		}
	}
	return "UTC"
}

func putTask(cmd *cobra.Command, cfg config.Config, w config.Workspace, t scheduled.Task, allowDangerous bool) (scheduled.Task, error) {
	body, err := json.Marshal(t)
	if err != nil {
		return scheduled.Task{}, fmt.Errorf("encoding the task: %w", err)
	}
	args := []string{"put"}
	if allowDangerous {
		args = append(args, "--allow-dangerous")
	}
	var reply struct {
		Task scheduled.Task `json:"task"`
	}
	err = callWorkspace(cmd.Context(), cfg, w, scheduledCallTimeout, args, body, &reply)
	return reply.Task, err
}

func reportTask(cmd *cobra.Command, opts *options, verb string, w config.Workspace, t scheduled.Task) error {
	if opts.format == formatJSON {
		return writeJSON(cmd.OutOrStdout(), map[string]any{"workspace": w.Name, "task": t})
	}
	_, err := fmt.Fprintf(cmd.OutOrStdout(), "%s %s (%s) on %s: %s\n", verb, t.ID, t.Name, w.Name, t.Schedule.Describe())
	return err
}

func newScheduledAddCmd(opts *options) *cobra.Command {
	var (
		f         taskFlags
		workspace string
	)
	c := &cobra.Command{
		Use:   "add --workspace <w> --name <n> --folder <f> (--command <c> | --harness <h> --prompt <p>) --every <e> ...",
		Short: "Create a scheduled task in a workspace",
		Example: "  devmachine scheduled add --workspace alice --name \"Daily briefing\" --folder ~/dev/app \\\n" +
			"    --harness claude --prompt \"Summarize yesterday's commits\" --every weekdays --at 09:00",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, w, err := scheduledWorkspaceOf(opts, workspace)
			if err != nil {
				return err
			}
			t := scheduled.Task{Version: 1}
			if err := f.apply(cmd, &t); err != nil {
				return err
			}
			if !isScheduledInstalled(w) {
				if cfg, w, err = installScheduled(cmd, opts, cfg, w, f.yes); err != nil {
					return err
				}
			}
			created, err := putTask(cmd, cfg, w, t, f.allowDangerous)
			if err != nil {
				return err
			}
			return reportTask(cmd, opts, "created", w, created)
		},
	}
	f.register(c)
	c.Flags().StringVar(&workspace, "workspace", "", "the workspace the task runs in")
	c.Flags().BoolVar(&f.yes, "yes", false, "set up the scheduled package without asking, when the workspace has none")
	_ = c.MarkFlagRequired("workspace")
	return c
}

func findTask(cmd *cobra.Command, cfg config.Config, w config.Workspace, id string) (scheduled.Task, error) {
	var reply struct {
		Tasks []localTask `json:"tasks"`
	}
	if err := callWorkspace(cmd.Context(), cfg, w, scheduledCallTimeout, []string{"list"}, nil, &reply); err != nil {
		return scheduled.Task{}, err
	}
	for _, t := range reply.Tasks {
		if t.ID == id {
			return t.Task, nil
		}
	}
	return scheduled.Task{}, &scheduledError{Kind: "not-found", Message: fmt.Sprintf("%s has no task %s", w.Name, id)}
}

func newScheduledEditCmd(opts *options) *cobra.Command {
	var (
		f         taskFlags
		workspace string
	)
	c := &cobra.Command{
		Use:   "edit <id> --workspace <w> [flags of add]",
		Short: "Change a scheduled task; only the flags given change",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, w, err := scheduledWorkspaceOf(opts, workspace)
			if err != nil {
				return err
			}
			t, err := findTask(cmd, cfg, w, args[0])
			if err != nil {
				return err
			}
			if err := f.apply(cmd, &t); err != nil {
				return err
			}
			edited, err := putTask(cmd, cfg, w, t, f.allowDangerous)
			if err != nil {
				return err
			}
			return reportTask(cmd, opts, "changed", w, edited)
		},
	}
	f.register(c)
	c.Flags().StringVar(&workspace, "workspace", "", "the workspace the task is in")
	_ = c.MarkFlagRequired("workspace")
	return c
}

func newScheduledSimpleCmd(opts *options, op, short string) *cobra.Command {
	var workspace string
	c := &cobra.Command{
		Use:   op + " <id> --workspace <w>",
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, w, err := scheduledWorkspaceOf(opts, workspace)
			if err != nil {
				return err
			}
			var reply struct {
				Task scheduled.Task `json:"task"`
			}
			if err := callWorkspace(cmd.Context(), cfg, w, scheduledCallTimeout, []string{op, args[0]}, nil, &reply); err != nil {
				return err
			}
			return reportTask(cmd, opts, op+"d", w, reply.Task)
		},
	}
	c.Flags().StringVar(&workspace, "workspace", "", "the workspace the task is in")
	_ = c.MarkFlagRequired("workspace")
	return c
}

func newScheduledRmCmd(opts *options) *cobra.Command {
	var (
		workspace string
		yes       bool
	)
	c := &cobra.Command{
		Use:   "rm <id> --workspace <w>",
		Short: "Delete a scheduled task and the record of its runs",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, w, err := scheduledWorkspaceOf(opts, workspace)
			if err != nil {
				return err
			}
			if !yes {
				t, err := findTask(cmd, cfg, w, args[0])
				if err != nil {
					return err
				}
				ok, err := confirm(cmd.InOrStdin(), cmd.ErrOrStderr(), fmt.Sprintf("Delete %q from %s?", t.Name, w.Name))
				if err != nil {
					return err
				}
				if !ok {
					return errDeclined
				}
			}
			var reply struct {
				Removed string `json:"removed"`
			}
			if err := callWorkspace(cmd.Context(), cfg, w, scheduledCallTimeout, []string{"rm", args[0]}, nil, &reply); err != nil {
				return err
			}
			if opts.format == formatJSON {
				return writeJSON(cmd.OutOrStdout(), map[string]string{"workspace": w.Name, "removed": reply.Removed})
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "deleted %s from %s\n", reply.Removed, w.Name)
			return err
		},
	}
	c.Flags().StringVar(&workspace, "workspace", "", "the workspace the task is in")
	c.Flags().BoolVar(&yes, "yes", false, "do not ask")
	_ = c.MarkFlagRequired("workspace")
	return c
}

func newScheduledRunCmd(opts *options) *cobra.Command {
	var workspace string
	c := &cobra.Command{
		Use:   "run <id> --workspace <w>",
		Short: "Run a task now, to try it; its schedule does not change",
		Long: "It starts the run in the workspace and returns at once with the run's id. " +
			"`devmachine scheduled runs` shows when it ends and `devmachine scheduled log` what it printed.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, w, err := scheduledWorkspaceOf(opts, workspace)
			if err != nil {
				return err
			}
			var reply struct {
				RunID string `json:"run_id"`
			}
			if err := callWorkspace(cmd.Context(), cfg, w, scheduledCallTimeout, []string{"run", args[0]}, nil, &reply); err != nil {
				return err
			}
			if opts.format == formatJSON {
				return writeJSON(cmd.OutOrStdout(), map[string]string{"workspace": w.Name, "task": args[0], "run_id": reply.RunID})
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "started run %s of %s on %s\n", reply.RunID, args[0], w.Name)
			return err
		},
	}
	c.Flags().StringVar(&workspace, "workspace", "", "the workspace the task is in")
	_ = c.MarkFlagRequired("workspace")
	return c
}

func newScheduledRunsCmd(opts *options) *cobra.Command {
	var workspace string
	c := &cobra.Command{
		Use:   "runs <id> --workspace <w>",
		Short: "A task's runs, newest first",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, w, err := scheduledWorkspaceOf(opts, workspace)
			if err != nil {
				return err
			}
			var reply struct {
				Runs []scheduled.Run `json:"runs"`
			}
			if err := callWorkspace(cmd.Context(), cfg, w, scheduledCallTimeout, []string{"runs", args[0]}, nil, &reply); err != nil {
				return err
			}
			if reply.Runs == nil {
				reply.Runs = []scheduled.Run{}
			}
			if opts.format == formatJSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"workspace": w.Name, "task": args[0], "runs": reply.Runs})
			}
			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintln(tw, "RUN\tSTATUS\tSTARTED\tDURATION\tEXIT\tMESSAGE")
			for _, r := range reply.Runs {
				exit := "-"
				if r.ExitCode != nil {
					exit = fmt.Sprint(*r.ExitCode)
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", r.ID, r.Status, r.StartedAt.Local().Format("2006-01-02 15:04:05"),
					(time.Duration(r.DurationMS) * time.Millisecond).Round(time.Second), exit, r.Message)
			}
			if err := tw.Flush(); err != nil {
				return fmt.Errorf("writing the output: %w", err)
			}
			return nil
		},
	}
	c.Flags().StringVar(&workspace, "workspace", "", "the workspace the task is in")
	_ = c.MarkFlagRequired("workspace")
	return c
}

func newScheduledLogCmd(opts *options) *cobra.Command {
	var workspace string
	c := &cobra.Command{
		Use:   "log <id> <run-id> --workspace <w>",
		Short: "What a run printed (its last mebibyte)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, w, err := scheduledWorkspaceOf(opts, workspace)
			if err != nil {
				return err
			}
			out, err := runWorkspaceScript(cmd.Context(), cfg, w, scheduledCallTimeout,
				scheduledScript([]string{"log", args[0], args[1]}, nil))
			if err != nil {
				if strings.HasPrefix(strings.TrimSpace(out), "{") {
					return decodeScheduledReply(cfg, w, out, err, &struct{}{})
				}
				return classifyScheduledError(cfg, w, err)
			}
			_, err = io.WriteString(cmd.OutOrStdout(), out)
			return err
		},
	}
	c.Flags().StringVar(&workspace, "workspace", "", "the workspace the task is in")
	_ = c.MarkFlagRequired("workspace")
	return c
}

func newScheduledHarnessesCmd(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "harnesses",
		Short: "The coding agents a prompt can run in, with their models and permission modes",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			type row struct {
				Name            string   `json:"name"`
				Package         string   `json:"package"`
				PermissionModes []string `json:"permission_modes"`
				Dangerous       []string `json:"dangerous"`
				Models          []string `json:"models"`
			}
			rows := []row{}
			for _, n := range harness.Names() {
				h, _ := harness.Get(n)
				rows = append(rows, row{h.Name, h.Package, h.PermissionModes, h.Dangerous, h.Models})
			}
			if opts.format == formatJSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"harnesses": rows})
			}
			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintln(tw, "HARNESS\tMODELS\tPERMISSION MODES")
			for _, r := range rows {
				fmt.Fprintf(tw, "%s\t%s\t%s\n", r.Name, strings.Join(r.Models, ", "), strings.Join(r.PermissionModes, ", "))
			}
			if err := tw.Flush(); err != nil {
				return fmt.Errorf("writing the output: %w", err)
			}
			return nil
		},
	}
}
