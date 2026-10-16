package commands

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mydevmachine/devmachine/internal/scheduled"
)

func executeIn(t *testing.T, stdin string, args ...string) (string, error) {
	t.Helper()
	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

type detachedCall struct{ taskID, runID, trigger string }

func stubDetached(t *testing.T) *[]detachedCall {
	t.Helper()
	calls := &[]detachedCall{}
	orig := startDetached
	startDetached = func(taskID, runID, trigger string) error {
		*calls = append(*calls, detachedCall{taskID, runID, trigger})
		return nil
	}
	t.Cleanup(func() { startDetached = orig })
	return calls
}

const hourlyCommandTask = `{"name":"hello","folder":"~","schedule":{"every":"hourly","minute":0,"timezone":"UTC"},` +
	`"action":{"kind":"command","command":"echo hi"}}`

type localReply struct {
	Protocol int             `json:"protocol"`
	Task     scheduled.Task  `json:"task"`
	Tasks    []localTask     `json:"tasks"`
	Runs     []scheduled.Run `json:"runs"`
	RunID    string          `json:"run_id"`
	Removed  string          `json:"removed"`
	Started  []string        `json:"started"`
	Error    *scheduledError `json:"error"`
}

func decodeLocal(t *testing.T, out string) localReply {
	t.Helper()
	var r localReply
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatalf("%v: %q", err, out)
	}
	if r.Protocol != scheduled.Protocol {
		t.Fatalf("protocol = %d in %q", r.Protocol, out)
	}
	return r
}

func putLocal(t *testing.T, body string) scheduled.Task {
	t.Helper()
	out, err := executeIn(t, body, "scheduled", "local", "put")
	if err != nil {
		t.Fatal(err, out)
	}
	return decodeLocal(t, out).Task
}

func TestLocalPutThenList(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	task := putLocal(t, hourlyCommandTask)
	if !scheduled.ValidTaskID(task.ID) || task.Version != 1 || task.CreatedAt.IsZero() {
		t.Fatalf("task = %+v", task)
	}

	out, err := execute(t, "scheduled", "local", "list")
	if err != nil {
		t.Fatal(err, out)
	}
	r := decodeLocal(t, out)
	if len(r.Tasks) != 1 || r.Tasks[0].ID != task.ID || r.Tasks[0].NextRunAt == nil || r.Tasks[0].LastRun != nil {
		t.Fatalf("tasks = %+v", r.Tasks)
	}
}

func TestLocalPutKeepsCreatedAtOnEdit(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	task := putLocal(t, hourlyCommandTask)
	task.Name = "renamed"
	body, _ := json.Marshal(task)
	edited := putLocal(t, string(body))
	if edited.ID != task.ID || edited.Name != "renamed" || !edited.CreatedAt.Equal(task.CreatedAt) {
		t.Fatalf("edited = %+v", edited)
	}
}

func TestLocalPutInvalid(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	out, err := executeIn(t, `{"folder":"~"}`, "scheduled", "local", "put")
	if err == nil {
		t.Fatal("want an error")
	}
	if r := decodeLocal(t, out); r.Error == nil || r.Error.Kind != "invalid" {
		t.Fatalf("reply = %q", out)
	}
}

func TestLocalPutDangerousNeedsFlag(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	body := `{"name":"x","folder":"~","schedule":{"every":"daily","at":"09:00","timezone":"UTC"},` +
		`"action":{"kind":"prompt","harness":"claude","prompt":"hi","permission_mode":"bypassPermissions"}}`
	if _, err := executeIn(t, body, "scheduled", "local", "put"); err == nil {
		t.Fatal("a dangerous mode went in without --allow-dangerous")
	}
	if out, err := executeIn(t, body, "scheduled", "local", "put", "--allow-dangerous"); err != nil {
		t.Fatal(err, out)
	}
}

func TestLocalPauseResumeRm(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	task := putLocal(t, hourlyCommandTask)

	out, _ := execute(t, "scheduled", "local", "pause", task.ID)
	if r := decodeLocal(t, out); !r.Task.Paused {
		t.Fatalf("pause: %q", out)
	}
	out, _ = execute(t, "scheduled", "local", "resume", task.ID)
	if r := decodeLocal(t, out); r.Task.Paused {
		t.Fatalf("resume: %q", out)
	}
	out, _ = execute(t, "scheduled", "local", "rm", task.ID)
	if r := decodeLocal(t, out); r.Removed != task.ID {
		t.Fatalf("rm: %q", out)
	}
	out, err := execute(t, "scheduled", "local", "rm", task.ID)
	if r := decodeLocal(t, out); err == nil || r.Error == nil || r.Error.Kind != "not-found" {
		t.Fatalf("second rm: %q", out)
	}
}

func TestLocalResumeOfSpentOnceRefused(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	body := `{"name":"x","folder":"~","done":true,"schedule":{"every":"once","date":"2020-01-01","at":"09:00","timezone":"UTC"},` +
		`"action":{"kind":"command","command":"true"}}`
	task := putLocal(t, body)
	out, err := execute(t, "scheduled", "local", "resume", task.ID)
	if err == nil || !strings.Contains(out, "already ran") {
		t.Fatalf("out = %q", out)
	}
}

func TestLocalRunStartsDetachedManual(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	calls := stubDetached(t)
	task := putLocal(t, hourlyCommandTask)
	out, err := execute(t, "scheduled", "local", "run", task.ID)
	if err != nil {
		t.Fatal(err, out)
	}
	r := decodeLocal(t, out)
	if len(*calls) != 1 || (*calls)[0].trigger != scheduled.TriggerManual || (*calls)[0].runID != r.RunID {
		t.Fatalf("calls = %+v reply = %q", *calls, out)
	}
}

func TestLocalTickStartsDueTasks(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	calls := stubDetached(t)
	body := `{"name":"x","folder":"~","schedule":{"every":"hourly","minute":0,"timezone":"UTC"},` +
		`"action":{"kind":"command","command":"true"}}`
	putLocal(t, body)
	out, err := execute(t, "scheduled", "local", "tick")
	if err != nil {
		t.Fatal(err, out)
	}
	for _, c := range *calls {
		if c.trigger != scheduled.TriggerSchedule {
			t.Fatalf("calls = %+v", *calls)
		}
	}
	decodeLocal(t, out)
}

func TestLocalExecAndLog(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("SHELL", "/bin/sh")
	task := putLocal(t, hourlyCommandTask)
	out, err := execute(t, "scheduled", "local", "exec", task.ID, "--trigger", "manual")
	if err != nil {
		t.Fatal(err, out)
	}
	var reply struct {
		Run scheduled.Run `json:"run"`
	}
	if err := json.Unmarshal([]byte(out), &reply); err != nil || reply.Run.Status != scheduled.StatusSucceeded {
		t.Fatalf("out = %q", out)
	}
	out, _ = execute(t, "scheduled", "local", "runs", task.ID)
	if r := decodeLocal(t, out); len(r.Runs) != 1 {
		t.Fatalf("runs: %q", out)
	}
	out, err = execute(t, "scheduled", "local", "log", task.ID, reply.Run.ID)
	if err != nil || out != "hi\n" {
		t.Fatalf("log = %q err = %v", out, err)
	}
}

func TestLocalIsHidden(t *testing.T) {
	out, _ := execute(t, "scheduled", "--help")
	if strings.Contains(out, "local") {
		t.Fatalf("help shows local: %q", out)
	}
}
