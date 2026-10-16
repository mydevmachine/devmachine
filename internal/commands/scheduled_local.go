package commands

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/mydevmachine/devmachine/internal/scheduled"
	"github.com/spf13/cobra"
)

type scheduledError struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
}

func (e *scheduledError) Error() string { return e.Message }

type localTask struct {
	scheduled.Task
	NextRunAt *time.Time     `json:"next_run_at"`
	LastRun   *scheduled.Run `json:"last_run"`
}

var errInvalidTask = errors.New("invalid task")

// startDetached starts `scheduled local exec` in its own session and does
// not wait: the timer's tick and the Mac's "run now" both return at once,
// and the run outlives the connection that asked for it.
var startDetached = func(taskID, runID, trigger string) error {
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("finding the devmachine binary: %w", err)
	}
	cmd := exec.Command(self, "scheduled", "local", "exec", taskID, "--run-id", runID, "--trigger", trigger)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting the run: %w", err)
	}
	return cmd.Process.Release()
}

func newScheduledLocalCmd() *cobra.Command {
	c := &cobra.Command{
		Use:    "local",
		Short:  "The workspace side of scheduled tasks, called by the timer and by the Mac",
		Hidden: true,
	}
	var allowDangerous bool
	put := localOp("put", "Create or replace a task read as JSON on stdin", cobra.NoArgs,
		func(cmd *cobra.Command, s scheduled.Store, _ []string) (any, error) {
			return localPut(cmd.InOrStdin(), s, allowDangerous)
		})
	put.Flags().BoolVar(&allowDangerous, "allow-dangerous", false, "accept a permission mode with no approvals")

	var runID, trigger string
	execCmd := localOp("exec <id>", "Run a task now and wait for it", cobra.ExactArgs(1),
		func(cmd *cobra.Command, s scheduled.Store, args []string) (any, error) {
			run, err := scheduled.Exec(cmd.Context(), s, args[0], scheduled.ExecOptions{RunID: runID, Trigger: trigger})
			return map[string]any{"run": run}, err
		})
	execCmd.Flags().StringVar(&runID, "run-id", "", "the id to record the run under")
	execCmd.Flags().StringVar(&trigger, "trigger", scheduled.TriggerSchedule, "schedule or manual")

	c.AddCommand(
		localOp("list", "Every task with its next and last run", cobra.NoArgs,
			func(_ *cobra.Command, s scheduled.Store, _ []string) (any, error) { return localList(s) }),
		put,
		localOp("pause <id>", "Stop a task from running on its schedule", cobra.ExactArgs(1),
			func(_ *cobra.Command, s scheduled.Store, args []string) (any, error) {
				return localSetPaused(s, args[0], true)
			}),
		localOp("resume <id>", "Let a paused task run on its schedule again", cobra.ExactArgs(1),
			func(_ *cobra.Command, s scheduled.Store, args []string) (any, error) {
				return localSetPaused(s, args[0], false)
			}),
		localOp("rm <id>", "Delete a task and its runs", cobra.ExactArgs(1),
			func(_ *cobra.Command, s scheduled.Store, args []string) (any, error) {
				return map[string]any{"removed": args[0]}, s.Delete(args[0])
			}),
		localOp("run <id>", "Start a task now without waiting", cobra.ExactArgs(1),
			func(_ *cobra.Command, s scheduled.Store, args []string) (any, error) { return localRunNow(s, args[0]) }),
		localOp("runs <id>", "A task's runs, newest first", cobra.ExactArgs(1),
			func(_ *cobra.Command, s scheduled.Store, args []string) (any, error) {
				runs, err := s.Runs(args[0])
				return map[string]any{"runs": runs}, err
			}),
		localOp("tick", "Start what is due; the timer runs this every minute", cobra.NoArgs,
			func(_ *cobra.Command, s scheduled.Store, _ []string) (any, error) {
				return scheduled.Tick(s, time.Now(), func(taskID, runID string) error {
					return startDetached(taskID, runID, scheduled.TriggerSchedule)
				})
			}),
		execCmd,
		newScheduledLocalLogCmd(),
	)
	return c
}

// localOp wraps one workspace operation: it opens the store, and answers in
// JSON carrying the protocol, an error included, so the Mac always has a
// reply to read.
func localOp(use, short string, args cobra.PositionalArgs,
	do func(cmd *cobra.Command, s scheduled.Store, args []string) (any, error)) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		Args:  args,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openLocalStore()
			var reply any
			if err == nil {
				reply, err = do(cmd, s, args)
			}
			if err != nil {
				return writeLocalError(cmd.OutOrStdout(), err)
			}
			return writeLocal(cmd.OutOrStdout(), reply)
		},
	}
}

func newScheduledLocalLogCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "log <id> <run-id>",
		Short: "What a run printed",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openLocalStore()
			if err != nil {
				return writeLocalError(cmd.OutOrStdout(), err)
			}
			data, err := s.ReadLog(args[0], args[1])
			if err != nil {
				return writeLocalError(cmd.OutOrStdout(), err)
			}
			if _, err := cmd.OutOrStdout().Write(data); err != nil {
				return fmt.Errorf("writing the output: %w", err)
			}
			return nil
		},
	}
}

func openLocalStore() (scheduled.Store, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return scheduled.Store{}, fmt.Errorf("finding the home folder: %w", err)
	}
	return scheduled.Open(home)
}

func writeLocal(out io.Writer, reply any) error {
	body, err := json.Marshal(reply)
	if err != nil {
		return fmt.Errorf("rendering JSON: %w", err)
	}
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(body, &fields); err != nil {
		return fmt.Errorf("rendering JSON: %w", err)
	}
	fields["protocol"] = json.RawMessage(fmt.Sprint(scheduled.Protocol))
	return writeJSON(out, fields)
}

func writeLocalError(out io.Writer, err error) error {
	kind := "other"
	switch {
	case errors.Is(err, scheduled.ErrNotFound):
		kind = "not-found"
	case errors.Is(err, errInvalidTask):
		kind = "invalid"
	}
	if werr := writeJSON(out, map[string]any{
		"protocol": scheduled.Protocol,
		"error":    scheduledError{Kind: kind, Message: err.Error()},
	}); werr != nil {
		return werr
	}
	return &exitError{code: 1, err: err, quiet: true}
}

func localList(s scheduled.Store) (any, error) {
	tasks, err := s.List()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	out := make([]localTask, 0, len(tasks))
	for _, t := range tasks {
		lt := localTask{Task: t}
		if !t.Paused && !t.Done {
			if next := t.Schedule.Next(now); !next.IsZero() {
				lt.NextRunAt = &next
			}
		}
		if lt.LastRun, err = s.LastRun(t.ID); err != nil {
			return nil, err
		}
		out = append(out, lt)
	}
	return map[string]any{"tasks": out}, nil
}

func localPut(in io.Reader, s scheduled.Store, allowDangerous bool) (any, error) {
	var t scheduled.Task
	if err := json.NewDecoder(in).Decode(&t); err != nil {
		return nil, fmt.Errorf("%w: reading the task: %v", errInvalidTask, err)
	}
	now := time.Now().UTC()
	if t.ID == "" {
		t.ID, t.CreatedAt = scheduled.NewID(), now
	} else {
		old, err := s.Get(t.ID)
		if err != nil {
			return nil, err
		}
		t.CreatedAt = old.CreatedAt
	}
	if err := t.Validate(allowDangerous); err != nil {
		return nil, fmt.Errorf("%w: %v", errInvalidTask, err)
	}
	t.Version, t.UpdatedAt = 1, now
	if err := s.Put(t); err != nil {
		return nil, err
	}
	return map[string]any{"task": t}, nil
}

func localSetPaused(s scheduled.Store, id string, paused bool) (any, error) {
	t, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	if !paused && t.Done && t.Schedule.Next(time.Now()).IsZero() {
		return nil, fmt.Errorf("%w: this one-off task already ran; edit its date to run it again", errInvalidTask)
	}
	t.Paused, t.UpdatedAt = paused, time.Now().UTC()
	if err := s.Put(t); err != nil {
		return nil, err
	}
	return map[string]any{"task": t}, nil
}

func localRunNow(s scheduled.Store, id string) (any, error) {
	if _, err := s.Get(id); err != nil {
		return nil, err
	}
	runID := scheduled.NewRunID(time.Now())
	if err := startDetached(id, runID, scheduled.TriggerManual); err != nil {
		return nil, err
	}
	return map[string]any{"run_id": runID}, nil
}
