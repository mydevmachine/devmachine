package scheduled

import (
	"strings"
	"testing"
	"time"
)

func validTask() Task {
	return Task{Version: 1, ID: "t_abc123", Name: "Daily briefing", Folder: "~/dev/app", Timeout: "30m",
		Schedule: Schedule{Every: "daily", At: "09:00", Timezone: "America/Sao_Paulo"},
		Action:   Action{Kind: "prompt", Harness: "claude", Prompt: "hi", PermissionMode: "acceptEdits"}}
}

func TestValidate(t *testing.T) {
	minute := 61
	cases := []struct {
		name   string
		mutate func(*Task)
		want   string
	}{
		{"ok", func(*Task) {}, ""},
		{"no name", func(t *Task) { t.Name = " " }, "name"},
		{"bad every", func(t *Task) { t.Schedule.Every = "yearly" }, "every"},
		{"bad at", func(t *Task) { t.Schedule.At = "25:00" }, "at"},
		{"hourly no minute", func(t *Task) { t.Schedule = Schedule{Every: "hourly", Timezone: "UTC"} }, "minute"},
		{"hourly bad minute", func(t *Task) { t.Schedule = Schedule{Every: "hourly", Minute: &minute, Timezone: "UTC"} }, "minute"},
		{"weekly no weekday", func(t *Task) { t.Schedule.Every = "weekly" }, "weekday"},
		{"monthly day 0", func(t *Task) { t.Schedule.Every = "monthly" }, "day"},
		{"once no date", func(t *Task) { t.Schedule.Every = "once" }, "date"},
		{"bad tz", func(t *Task) { t.Schedule.Timezone = "Mars/Base" }, "timezone"},
		{"no tz", func(t *Task) { t.Schedule.Timezone = "" }, "timezone"},
		{"no folder", func(t *Task) { t.Folder = "" }, "folder"},
		{"timeout too long", func(t *Task) { t.Timeout = "7h" }, "timeout"},
		{"timeout junk", func(t *Task) { t.Timeout = "soon" }, "timeout"},
		{"unknown harness", func(t *Task) { t.Action.Harness = "gemini" }, "harness"},
		{"empty prompt", func(t *Task) { t.Action.Prompt = "  " }, "prompt"},
		{"bad mode", func(t *Task) { t.Action.PermissionMode = "yolo" }, "permission mode"},
		{"dangerous without allow", func(t *Task) { t.Action.PermissionMode = "bypassPermissions" }, "--allow-dangerous"},
		{"command empty", func(t *Task) { t.Action = Action{Kind: "command"} }, "command"},
		{"bad kind", func(t *Task) { t.Action.Kind = "script" }, "kind"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			task := validTask()
			c.mutate(&task)
			err := task.Validate(false)
			if c.want == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want it to mention %q", err, c.want)
			}
		})
	}
}

func TestValidKinds(t *testing.T) {
	m := 0
	for _, s := range []Schedule{
		{Every: "hourly", Minute: &m, Timezone: "UTC"},
		{Every: "weekdays", At: "23:59", Timezone: "UTC"},
		{Every: "weekly", Weekday: "sun", At: "00:00", Timezone: "UTC"},
		{Every: "monthly", Day: 31, At: "09:00", Timezone: "UTC"},
		{Every: "once", Date: "2026-10-16", At: "09:00", Timezone: "UTC"},
	} {
		task := validTask()
		task.Schedule = s
		if err := task.Validate(false); err != nil {
			t.Errorf("%+v: %v", s, err)
		}
	}
}

func TestDangerousAllowed(t *testing.T) {
	task := validTask()
	task.Action.PermissionMode = "bypassPermissions"
	if err := task.Validate(true); err != nil {
		t.Fatal(err)
	}
}

func TestTimeoutDefault(t *testing.T) {
	task := validTask()
	task.Timeout = ""
	if err := task.Validate(false); err != nil {
		t.Fatal(err)
	}
	if task.TimeoutDuration() != 30*time.Minute {
		t.Fatalf("timeout = %v", task.TimeoutDuration())
	}
}

func TestNewIDShape(t *testing.T) {
	id := NewID()
	if !ValidTaskID(id) || NewID() == id {
		t.Fatalf("id = %q", id)
	}
}
