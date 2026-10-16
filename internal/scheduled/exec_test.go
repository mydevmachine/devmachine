package scheduled

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func setupHome(t *testing.T) (string, Store) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	s, err := Open(home)
	if err != nil {
		t.Fatal(err)
	}
	return home, s
}

func fakeBin(t *testing.T, name, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func testExec() ExecOptions {
	return ExecOptions{Shell: "/bin/sh", ShellFlags: []string{"-c"}, Trigger: TriggerSchedule}
}

func commandTask(cmd string) Task {
	t := validTask()
	t.ID = NewID()
	t.Folder = "~"
	t.Action = Action{Kind: "command", Command: cmd}
	return t
}

func promptTask(h, prompt string) Task {
	t := validTask()
	t.ID = NewID()
	t.Folder = "~"
	t.Action = Action{Kind: "prompt", Harness: h, Prompt: prompt}
	return t
}

func mustPut(t *testing.T, s Store, task Task) {
	t.Helper()
	if err := s.Put(task); err != nil {
		t.Fatal(err)
	}
}

func runIt(t *testing.T, s Store, task Task, o ExecOptions) (Run, string) {
	t.Helper()
	mustPut(t, s, task)
	run, err := Exec(context.Background(), s, task.ID, o)
	if err != nil {
		t.Fatal(err)
	}
	log, _ := s.ReadLog(task.ID, run.ID)
	return run, string(log)
}

func TestExecPassesPromptVerbatim(t *testing.T) {
	home, s := setupHome(t)
	t.Setenv("PATH", fakeBin(t, "claude", `printf '%s\n' "$@"`)+":/usr/bin:/bin")
	prompt := "say \"hi\" $(touch pwned) `touch pwned2`\nline two"

	run, log := runIt(t, s, promptTask("claude", prompt), testExec())

	if run.Status != StatusSucceeded {
		t.Fatalf("run = %+v log = %q", run, log)
	}
	if !strings.Contains(log, "-p\n--\n"+prompt) {
		t.Fatalf("log = %q", log)
	}
	for _, f := range []string{"pwned", "pwned2"} {
		if _, err := os.Stat(filepath.Join(home, f)); err == nil {
			t.Fatal("the prompt was read by a shell")
		}
	}
}

func TestExecRunsInTheFolder(t *testing.T) {
	home, s := setupHome(t)
	if err := os.Mkdir(filepath.Join(home, "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	task := commandTask("pwd")
	task.Folder = "~/app"
	run, log := runIt(t, s, task, testExec())
	if run.Status != StatusSucceeded || !strings.HasSuffix(strings.TrimSpace(log), "/app") {
		t.Fatalf("run = %+v log = %q", run, log)
	}
}

func TestExecCommandFailedRecordsExitCode(t *testing.T) {
	_, s := setupHome(t)
	run, _ := runIt(t, s, commandTask("echo boom; exit 3"), testExec())
	if run.Status != StatusFailed || run.ExitCode == nil || *run.ExitCode != 3 {
		t.Fatalf("run = %+v", run)
	}
	stored, _ := s.LastRun(run.TaskID)
	if stored == nil || stored.Status != StatusFailed || stored.EndedAt.IsZero() {
		t.Fatalf("stored = %+v", stored)
	}
}

func TestExecTimeoutKillsGroup(t *testing.T) {
	home, s := setupHome(t)
	pidFile := filepath.Join(home, "child.pid")
	task := commandTask("sleep 30 & echo $! > " + pidFile + "; sleep 30")
	task.Timeout = "1s"
	began := time.Now()

	run, _ := runIt(t, s, task, testExec())

	if run.Status != StatusTimeout {
		t.Fatalf("run = %+v", run)
	}
	if time.Since(began) > 10*time.Second {
		t.Fatalf("took %v", time.Since(began))
	}
	data, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(data)))
	time.Sleep(200 * time.Millisecond)
	if syscall.Kill(pid, 0) == nil {
		t.Fatalf("child %d still alive", pid)
	}
}

func TestExecFolderMissing(t *testing.T) {
	_, s := setupHome(t)
	task := commandTask("true")
	task.Folder = "~/gone"
	run, _ := runIt(t, s, task, testExec())
	if run.Status != StatusError || !strings.Contains(run.Message, "does not exist") {
		t.Fatalf("run = %+v", run)
	}
}

func TestExecHarnessMissing(t *testing.T) {
	_, s := setupHome(t)
	t.Setenv("PATH", t.TempDir()+":/usr/bin:/bin")
	run, _ := runIt(t, s, promptTask("claude", "hi"), testExec())
	if run.Status != StatusError || !strings.Contains(run.Message, "claude-code") {
		t.Fatalf("run = %+v", run)
	}
}

func TestExecLogCapped(t *testing.T) {
	_, s := setupHome(t)
	run, _ := runIt(t, s, commandTask("head -c 2097152 /dev/zero | tr '\\0' a; echo END"), testExec())
	if run.Status != StatusSucceeded {
		t.Fatalf("run = %+v", run)
	}
	info, err := os.Stat(s.LogPath(run.TaskID, run.ID))
	if err != nil || info.Size() != LogLimit {
		t.Fatalf("size = %v err = %v", info.Size(), err)
	}
	log, _ := s.ReadLog(run.TaskID, run.ID)
	if !strings.HasSuffix(string(log), "END\n") {
		t.Fatal("the cap kept the head, not the tail")
	}
}

func TestExecSkipsWhileRunning(t *testing.T) {
	_, s := setupHome(t)
	task := commandTask("true")
	mustPut(t, s, task)
	if err := os.WriteFile(s.LockPath(task.ID), []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
		t.Fatal(err)
	}
	run, err := Exec(context.Background(), s, task.ID, testExec())
	if err != nil || run.Status != StatusSkipped {
		t.Fatalf("run = %+v err = %v", run, err)
	}
	if _, err := os.Stat(s.LockPath(task.ID)); err != nil {
		t.Fatal("a skipped run removed the running one's lock")
	}
}

func TestExecTakesAStaleLock(t *testing.T) {
	_, s := setupHome(t)
	task := commandTask("true")
	mustPut(t, s, task)
	if err := os.WriteFile(s.LockPath(task.ID), []byte("999999"), 0o600); err != nil {
		t.Fatal(err)
	}
	run, err := Exec(context.Background(), s, task.ID, testExec())
	if err != nil || run.Status != StatusSucceeded {
		t.Fatalf("run = %+v err = %v", run, err)
	}
	if _, err := os.Stat(s.LockPath(task.ID)); !os.IsNotExist(err) {
		t.Fatal("the lock was not released")
	}
}

func TestExecOnceMarksDone(t *testing.T) {
	_, s := setupHome(t)
	task := commandTask("true")
	task.Schedule = Schedule{Every: "once", Date: "2026-10-16", At: "09:00", Timezone: "UTC"}
	runIt(t, s, task, testExec())
	got, _ := s.Get(task.ID)
	if !got.Done {
		t.Fatal("a one-off task that ran is not done")
	}
}

func TestExecManualOnceStaysOpen(t *testing.T) {
	_, s := setupHome(t)
	task := commandTask("true")
	task.Schedule = Schedule{Every: "once", Date: "2026-10-16", At: "09:00", Timezone: "UTC"}
	o := testExec()
	o.Trigger = TriggerManual
	runIt(t, s, task, o)
	if got, _ := s.Get(task.ID); got.Done {
		t.Fatal("run now used up a one-off task")
	}
}

func TestExecUsesGivenRunID(t *testing.T) {
	_, s := setupHome(t)
	o := testExec()
	o.RunID = NewRunID(time.Now())
	run, _ := runIt(t, s, commandTask("true"), o)
	if run.ID != o.RunID {
		t.Fatalf("run id = %q want %q", run.ID, o.RunID)
	}
}
