package scheduled

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/mydevmachine/devmachine/internal/harness"
)

// LogLimit is how much of a run's output is kept: the last mebibyte.
const LogLimit = 1 << 20

// RunsKept is how many runs of a task are kept.
const RunsKept = 50

const notFoundExit = 127

// ExecOptions shape one run. Shell and ShellFlags default to the account's
// login shell, which is what puts mise, brew and ~/.local/bin on PATH.
type ExecOptions struct {
	RunID      string
	Trigger    string
	Now        func() time.Time
	Shell      string
	ShellFlags []string
}

func (o ExecOptions) withDefaults() ExecOptions {
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Trigger == "" {
		o.Trigger = TriggerSchedule
	}
	if o.Shell == "" {
		o.Shell = os.Getenv("SHELL")
	}
	if o.Shell == "" {
		o.Shell = "/bin/sh"
	}
	if o.ShellFlags == nil {
		o.ShellFlags = []string{"-lc"}
	}
	if o.RunID == "" {
		o.RunID = NewRunID(o.Now())
	}
	return o
}

// Exec runs one task now and records the run. It returns an error only when
// the run could not be recorded; a run that went wrong is a run with that
// status.
func Exec(ctx context.Context, s Store, taskID string, o ExecOptions) (Run, error) {
	o = o.withDefaults()
	task, err := s.Get(taskID)
	if err != nil {
		return Run{}, err
	}
	run := Run{ID: o.RunID, TaskID: taskID, Trigger: o.Trigger, StartedAt: o.Now()}

	release, held, err := takeLock(s.LockPath(taskID))
	if err != nil {
		return Run{}, err
	}
	if held {
		run.Status, run.Message, run.EndedAt = StatusSkipped, "the previous run is still going", run.StartedAt
		return run, s.PutRun(run)
	}
	defer release()

	run.Status = StatusRunning
	if err := s.PutRun(run); err != nil {
		return Run{}, err
	}
	run = execute(ctx, s, task, run, o)
	run.EndedAt = o.Now()
	run.DurationMS = run.EndedAt.Sub(run.StartedAt).Milliseconds()
	if err := capLog(s.LogPath(taskID, run.ID)); err != nil {
		return run, err
	}
	if err := s.PutRun(run); err != nil {
		return run, err
	}
	if task.Schedule.Every == "once" && o.Trigger == TriggerSchedule {
		task.Done = true
		if err := s.Put(task); err != nil {
			return run, err
		}
	}
	return run, s.Prune(taskID, RunsKept)
}

func execute(ctx context.Context, s Store, task Task, run Run, o ExecOptions) Run {
	fail := func(msg string) Run {
		run.Status, run.Message = StatusError, msg
		return run
	}
	folder, err := expandHome(task.Folder)
	if err != nil {
		return fail(err.Error())
	}
	if info, err := os.Stat(folder); err != nil || !info.IsDir() {
		return fail(fmt.Sprintf("folder %s does not exist", task.Folder))
	}
	args, h, err := commandArgs(task)
	if err != nil {
		return fail(err.Error())
	}
	logFile, err := os.OpenFile(s.LogPath(task.ID, run.ID), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fail(fmt.Sprintf("opening the log: %v", err))
	}
	defer logFile.Close()

	ctx, cancel := context.WithTimeout(ctx, task.TimeoutDuration())
	defer cancel()
	cmd := exec.CommandContext(ctx, o.Shell, append(slices.Clone(o.ShellFlags), args...)...)
	cmd.Dir = folder
	cmd.Stdout, cmd.Stderr = logFile, logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 5 * time.Second

	err = cmd.Run()
	var exitErr *exec.ExitError
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		run.Status, run.Message = StatusTimeout, fmt.Sprintf("stopped after %s", task.TimeoutDuration())
	case err == nil:
		run.Status, run.ExitCode = StatusSucceeded, intPtr(0)
	case errors.As(err, &exitErr):
		code := exitErr.ExitCode()
		run.Status, run.ExitCode = StatusFailed, intPtr(code)
		if h != nil && code == notFoundExit {
			run.Status = StatusError
			run.Message = fmt.Sprintf("%s is not installed for this account; add the %s package to this workspace", h.Binary, h.Package)
		}
	default:
		return fail(fmt.Sprintf("starting the run: %v", err))
	}
	return run
}

// commandArgs is what follows the shell's flags. A prompt goes through
// `exec "$@"` so the login shell sets PATH but never reads the prompt.
func commandArgs(task Task) ([]string, *harness.Harness, error) {
	if task.Action.Kind == "command" {
		return []string{task.Action.Command}, nil, nil
	}
	h, ok := harness.Get(task.Action.Harness)
	if !ok {
		return nil, nil, fmt.Errorf("unknown harness %q", task.Action.Harness)
	}
	argv, err := h.Argv(task.Action.Prompt, task.Action.Model, task.Action.PermissionMode)
	if err != nil {
		return nil, nil, err
	}
	return append([]string{`exec "$@"`, "scheduled"}, argv...), &h, nil
}

func expandHome(path string) (string, error) {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("finding the home folder: %w", err)
	}
	return filepath.Join(home, strings.TrimPrefix(path, "~")), nil
}

func intPtr(i int) *int { return &i }

// takeLock holds path for this process. held means another live process
// holds it; a lock left by a process that died is taken over.
func takeLock(path string) (release func(), held bool, err error) {
	for range 2 {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_, werr := f.WriteString(strconv.Itoa(os.Getpid()))
			if cerr := f.Close(); werr == nil {
				werr = cerr
			}
			if werr != nil {
				return nil, false, fmt.Errorf("writing the lock: %w", werr)
			}
			return func() { os.Remove(path) }, false, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return nil, false, fmt.Errorf("taking the lock: %w", err)
		}
		if lockAlive(path) {
			return nil, true, nil
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, false, fmt.Errorf("removing a stale lock: %w", err)
		}
	}
	return nil, true, nil
}

func lockAlive(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return false
	}
	err = syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// capLog keeps the last LogLimit bytes of a log, where the error usually is.
func capLog(path string) error {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("opening the log: %w", err)
	}
	info, err := f.Stat()
	if err != nil || info.Size() <= LogLimit {
		f.Close()
		return nil
	}
	tail := make([]byte, LogLimit)
	_, err = f.ReadAt(tail, info.Size()-LogLimit)
	f.Close()
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("reading the log: %w", err)
	}
	if err := os.WriteFile(path, tail, 0o600); err != nil {
		return fmt.Errorf("trimming the log: %w", err)
	}
	return nil
}
