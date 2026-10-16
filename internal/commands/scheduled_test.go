package commands

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/mydevmachine/devmachine/internal/config"
	"github.com/mydevmachine/devmachine/internal/remote"
	"github.com/mydevmachine/devmachine/internal/scheduled"
	"github.com/spf13/cobra"
)

const scheduledConfig = "machines:\n  - name: main\n    hosts: [203.0.113.10]\n" +
	"workspaces:\n  - name: alice\n    machine: main\n    packages: [scheduled]\n" +
	"  - name: bob\n    machine: main\n"

type exitCodeErr int

func (e exitCodeErr) Error() string { return fmt.Sprintf("exit status %d", int(e)) }
func (e exitCodeErr) ExitCode() int { return int(e) }

type scheduledFake struct {
	mu      sync.Mutex
	scripts []string
	reply   func(script string) (string, error)
}

func (f *scheduledFake) Run(ctx context.Context, cmd string) (string, error) {
	return f.RunInput(ctx, cmd, nil)
}

func (f *scheduledFake) RunInput(_ context.Context, command string, stdin io.Reader) (string, error) {
	script := command
	if stdin != nil {
		data, _ := io.ReadAll(stdin)
		script = string(data)
	}
	f.mu.Lock()
	f.scripts = append(f.scripts, script)
	f.mu.Unlock()
	return f.reply(script)
}

func (f *scheduledFake) Stream(ctx context.Context, cmd string, stdout, _ io.Writer) error {
	out, err := f.RunInput(ctx, cmd, nil)
	_, _ = io.WriteString(stdout, out)
	return err
}

func (f *scheduledFake) Upload(context.Context, string, io.Reader) error { return nil }
func (f *scheduledFake) Close() error                                    { return nil }

func (f *scheduledFake) last() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.scripts) == 0 {
		return ""
	}
	return f.scripts[len(f.scripts)-1]
}

func replyWith(body string) func(string) (string, error) {
	return func(string) (string, error) { return body, nil }
}

const aliceTasks = `{"protocol":1,"tasks":[{"version":1,"id":"t_000001","name":"Daily briefing","paused":false,"done":false,` +
	`"folder":"~/dev/app","schedule":{"every":"weekdays","at":"09:00","timezone":"UTC"},` +
	`"action":{"kind":"prompt","harness":"claude","prompt":"hi"},"next_run_at":"2026-10-16T09:00:00Z","last_run":null}]}`

type scheduledListReply struct {
	Workspaces []scheduledWorkspace `json:"workspaces"`
}

func listScheduled(t *testing.T, dir string) scheduledListReply {
	t.Helper()
	out, err := execute(t, "--config", dir, "--format", "json", "scheduled", "list")
	if err != nil {
		t.Fatal(err, out)
	}
	var r scheduledListReply
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatal(err, out)
	}
	return r
}

func TestScheduledListAcrossWorkspaces(t *testing.T) {
	dir := configWith(t, scheduledConfig)
	alice := &scheduledFake{reply: replyWith(aliceTasks)}
	dialByUser(t, map[string]remote.Client{"alice": alice}, nil)

	r := listScheduled(t, dir)

	if len(r.Workspaces) != 2 {
		t.Fatalf("workspaces = %+v", r.Workspaces)
	}
	a, b := r.Workspaces[0], r.Workspaces[1]
	if a.Name != "alice" || !a.Installed || len(a.Tasks) != 1 || a.Tasks[0].ID != "t_000001" || a.Error != nil {
		t.Fatalf("alice = %+v", a)
	}
	if b.Name != "bob" || b.Installed || len(b.Tasks) != 0 || b.Error != nil {
		t.Fatalf("bob = %+v", b)
	}
	if !strings.Contains(alice.last(), "scheduled local 'list'") || !strings.Contains(alice.last(), ".devmachine/scheduled/devmachine") {
		t.Fatalf("script = %q", alice.last())
	}
}

func TestScheduledListErrorKinds(t *testing.T) {
	cases := []struct {
		name string
		fail error
		fake *scheduledFake
		kind string
	}{
		{"unreachable", context.DeadlineExceeded, nil, "unreachable"},
		{"login refused", remote.ErrAuthRefused, nil, "login-refused"},
		{"binary missing", nil, &scheduledFake{reply: func(string) (string, error) { return "", exitCodeErr(127) }}, "setup-incomplete"},
		{"binary missing, bash's exec", nil, &scheduledFake{reply: func(string) (string, error) { return "", exitCodeErr(126) }}, "setup-incomplete"},
		{"older binary", nil, &scheduledFake{reply: replyWith(`{"tasks":[]}`)}, "outdated"},
		{"not json", nil, &scheduledFake{reply: replyWith("unknown command \"scheduled\"")}, "outdated"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := configWith(t, scheduledConfig)
			clients := map[string]remote.Client{}
			if c.fake != nil {
				clients["alice"] = c.fake
			}
			failures := map[string]error{}
			if c.fail != nil {
				failures["alice"] = c.fail
			}
			dialByUser(t, clients, failures)
			r := listScheduled(t, dir)
			if r.Workspaces[0].Error == nil || r.Workspaces[0].Error.Kind != c.kind {
				t.Fatalf("alice = %+v", r.Workspaces[0])
			}
		})
	}
}

func decodePutBody(t *testing.T, script string) scheduled.Task {
	t.Helper()
	_, body, ok := strings.Cut(script, "<<'"+taskHeredoc+"'\n")
	if !ok {
		t.Fatalf("no task on stdin: %q", script)
	}
	body, _, _ = strings.Cut(body, "\n"+taskHeredoc)
	var task scheduled.Task
	if err := json.Unmarshal([]byte(body), &task); err != nil {
		t.Fatal(err, body)
	}
	return task
}

func TestScheduledAddPrompt(t *testing.T) {
	dir := configWith(t, scheduledConfig)
	alice := &scheduledFake{reply: func(script string) (string, error) {
		task := decodePutBody(t, script)
		task.ID = "t_000002"
		body, _ := json.Marshal(map[string]any{"protocol": 1, "task": task})
		return string(body), nil
	}}
	dialByUser(t, map[string]remote.Client{"alice": alice}, nil)

	out, err := execute(t, "--config", dir, "scheduled", "add", "--workspace", "alice",
		"--name", "Daily briefing", "--folder", "~/dev/app", "--harness", "claude",
		"--prompt", "it's \"quoted\"\nand $(not run)", "--model", "sonnet", "--permission-mode", "acceptEdits",
		"--every", "weekdays", "--at", "09:00", "--timezone", "America/Sao_Paulo")
	if err != nil {
		t.Fatal(err, out)
	}
	if !strings.Contains(out, "t_000002") || !strings.Contains(out, "Weekdays at 09:00") {
		t.Fatalf("out = %q", out)
	}
	script := alice.last()
	if !strings.Contains(script, "scheduled local 'put'") {
		t.Fatalf("script = %q", script)
	}
	task := decodePutBody(t, script)
	if task.Name != "Daily briefing" || task.Action.Prompt != "it's \"quoted\"\nand $(not run)" ||
		task.Action.Model != "sonnet" || task.Schedule.Timezone != "America/Sao_Paulo" || task.ID != "" {
		t.Fatalf("task = %+v", task)
	}
}

func TestScheduledAddRefusesBeforeDialing(t *testing.T) {
	dir := configWith(t, scheduledConfig)
	dialByUser(t, map[string]remote.Client{}, nil)
	cases := [][]string{
		{"--harness", "claude", "--prompt", "x", "--permission-mode", "bypassPermissions", "--every", "daily", "--at", "09:00"},
		{"--command", "true", "--harness", "claude", "--prompt", "x", "--every", "daily", "--at", "09:00"},
		{"--every", "daily", "--at", "09:00"},
		{"--command", "true", "--every", "daily", "--at", "9am"},
	}
	for _, extra := range cases {
		args := append([]string{"--config", dir, "scheduled", "add", "--workspace", "alice", "--name", "x", "--folder", "~"}, extra...)
		if out, err := execute(t, args...); err == nil {
			t.Fatalf("%v went through: %q", extra, out)
		}
	}
}

func TestScheduledEditChangesOnlyGivenFlags(t *testing.T) {
	dir := configWith(t, scheduledConfig)
	alice := &scheduledFake{reply: func(script string) (string, error) {
		if strings.Contains(script, "scheduled local 'list'") {
			return aliceTasks, nil
		}
		task := decodePutBody(t, script)
		body, _ := json.Marshal(map[string]any{"protocol": 1, "task": task})
		return string(body), nil
	}}
	dialByUser(t, map[string]remote.Client{"alice": alice}, nil)

	out, err := execute(t, "--config", dir, "scheduled", "edit", "t_000001", "--workspace", "alice", "--at", "10:30")
	if err != nil {
		t.Fatal(err, out)
	}
	task := decodePutBody(t, alice.last())
	if task.ID != "t_000001" || task.Schedule.At != "10:30" || task.Schedule.Every != "weekdays" ||
		task.Action.Prompt != "hi" || task.Name != "Daily briefing" {
		t.Fatalf("task = %+v", task)
	}
}

func TestScheduledEditHarnessDropsTheOldAgentsModeAndModel(t *testing.T) {
	dir := configWith(t, scheduledConfig)
	withMode := strings.Replace(aliceTasks, `"prompt":"hi"`, `"prompt":"hi","model":"sonnet","permission_mode":"acceptEdits"`, 1)
	alice := &scheduledFake{reply: func(script string) (string, error) {
		if strings.Contains(script, "scheduled local 'list'") {
			return withMode, nil
		}
		task := decodePutBody(t, script)
		body, _ := json.Marshal(map[string]any{"protocol": 1, "task": task})
		return string(body), nil
	}}
	dialByUser(t, map[string]remote.Client{"alice": alice}, nil)

	out, err := execute(t, "--config", dir, "scheduled", "edit", "t_000001", "--workspace", "alice", "--harness", "codex")
	if err != nil {
		t.Fatal(err, out)
	}
	task := decodePutBody(t, alice.last())
	if task.Action.Harness != "codex" || task.Action.PermissionMode != "" || task.Action.Model != "" || task.Action.Prompt != "hi" {
		t.Fatalf("action = %+v", task.Action)
	}
}

func TestScheduledRmAsks(t *testing.T) {
	dir := configWith(t, scheduledConfig)
	alice := &scheduledFake{reply: replyWith(aliceTasks)}
	dialByUser(t, map[string]remote.Client{"alice": alice}, nil)

	out, err := executeIn(t, "n\n", "--config", dir, "scheduled", "rm", "t_000001", "--workspace", "alice")
	if !errors.Is(err, errDeclined) {
		t.Fatalf("err = %v out = %q", err, out)
	}
	for _, s := range alice.scripts {
		if strings.Contains(s, "'rm'") {
			t.Fatal("removed after no")
		}
	}
}

func TestScheduledRunPrintsRunID(t *testing.T) {
	dir := configWith(t, scheduledConfig)
	alice := &scheduledFake{reply: replyWith(`{"protocol":1,"run_id":"20261015T120000Z-ab12"}`)}
	dialByUser(t, map[string]remote.Client{"alice": alice}, nil)
	out, err := execute(t, "--config", dir, "--format", "json", "scheduled", "run", "t_000001", "--workspace", "alice")
	if err != nil || !strings.Contains(out, "20261015T120000Z-ab12") {
		t.Fatalf("out = %q err = %v", out, err)
	}
	if !strings.Contains(alice.last(), "scheduled local 'run' 't_000001'") {
		t.Fatalf("script = %q", alice.last())
	}
}

func TestScheduledWorkspaceErrorReachesTheCaller(t *testing.T) {
	dir := configWith(t, scheduledConfig)
	alice := &scheduledFake{reply: func(string) (string, error) {
		return `{"protocol":1,"error":{"kind":"not-found","message":"t_000009: no such task"}}`, exitCodeErr(1)
	}}
	dialByUser(t, map[string]remote.Client{"alice": alice}, nil)
	out, err := execute(t, "--config", dir, "scheduled", "pause", "t_000009", "--workspace", "alice")
	if err == nil || !strings.Contains(err.Error(), "no such task") {
		t.Fatalf("err = %v out = %q", err, out)
	}
}

func TestScheduledHarnesses(t *testing.T) {
	out, err := execute(t, "--format", "json", "scheduled", "harnesses")
	if err != nil {
		t.Fatal(err)
	}
	var r struct {
		Harnesses []struct {
			Name            string   `json:"name"`
			PermissionModes []string `json:"permission_modes"`
			Dangerous       []string `json:"dangerous"`
			Models          []string `json:"models"`
		} `json:"harnesses"`
	}
	if err := json.Unmarshal([]byte(out), &r); err != nil || len(r.Harnesses) != 2 || r.Harnesses[0].Name != "claude" {
		t.Fatalf("out = %q", out)
	}
}

func TestScheduledAddInstallsOnFirstUse(t *testing.T) {
	dir := configWith(t, scheduledConfig)
	var order []string
	origEdit, origSync := editPackageFn, runSyncFn
	editPackageFn = func(_ *cobra.Command, o *options, name, workspace string, add, _, yes bool) error {
		if o.machine != "" {
			t.Errorf("packages add got --machine %q with --workspace, which it refuses", o.machine)
		}
		order = append(order, fmt.Sprintf("edit %s %s %v %v", name, workspace, add, yes))
		return nil
	}
	runSyncFn = func(_ *cobra.Command, o *options, _, yes bool, tags []string) error {
		order = append(order, fmt.Sprintf("sync %s %v %v", o.machine, yes, tags))
		return nil
	}
	origAvail := scheduledPackageAvailable
	scheduledPackageAvailable = func(context.Context, *options, config.Config) (bool, error) { return true, nil }
	t.Cleanup(func() { editPackageFn, runSyncFn, scheduledPackageAvailable = origEdit, origSync, origAvail })
	bob := &scheduledFake{reply: func(script string) (string, error) {
		order = append(order, "put")
		task := decodePutBody(t, script)
		task.ID = "t_000003"
		body, _ := json.Marshal(map[string]any{"protocol": 1, "task": task})
		return string(body), nil
	}}
	dialByUser(t, map[string]remote.Client{"bob": bob}, nil)

	out, err := execute(t, "--config", dir, "scheduled", "add", "--workspace", "bob", "--name", "x",
		"--folder", "~", "--command", "date", "--every", "daily", "--at", "09:00", "--yes")
	if err != nil {
		t.Fatal(err, out)
	}
	want := []string{"edit scheduled bob true true", "sync main true [scheduled]", "put"}
	if strings.Join(order, "|") != strings.Join(want, "|") {
		t.Fatalf("order = %q", order)
	}
}

func TestScheduledAddRefusesWhenThePinnedReleaseHasNoPackage(t *testing.T) {
	dir := configWith(t, scheduledConfig)
	edited := false
	origEdit, origAvail := editPackageFn, scheduledPackageAvailable
	editPackageFn = func(*cobra.Command, *options, string, string, bool, bool, bool) error {
		edited = true
		return nil
	}
	scheduledPackageAvailable = func(context.Context, *options, config.Config) (bool, error) { return false, nil }
	t.Cleanup(func() { editPackageFn, scheduledPackageAvailable = origEdit, origAvail })
	dialByUser(t, map[string]remote.Client{}, nil)

	out, err := execute(t, "--config", dir, "scheduled", "add", "--workspace", "bob", "--name", "x",
		"--folder", "~", "--command", "date", "--every", "daily", "--at", "09:00", "--yes")
	var se *scheduledError
	if !errors.As(err, &se) || se.Kind != "not-available" || edited {
		t.Fatalf("err = %v edited = %v out = %q", err, edited, out)
	}
}

func TestScheduledAddWithoutYesOnUninstalledRefuses(t *testing.T) {
	dir := configWith(t, scheduledConfig)
	called := false
	origEdit := editPackageFn
	editPackageFn = func(*cobra.Command, *options, string, string, bool, bool, bool) error {
		called = true
		return nil
	}
	origAvail := scheduledPackageAvailable
	scheduledPackageAvailable = func(context.Context, *options, config.Config) (bool, error) { return true, nil }
	t.Cleanup(func() { editPackageFn, scheduledPackageAvailable = origEdit, origAvail })
	dialByUser(t, map[string]remote.Client{}, nil)

	out, err := executeIn(t, "", "--config", dir, "scheduled", "add", "--workspace", "bob", "--name", "x",
		"--folder", "~", "--command", "date", "--every", "daily", "--at", "09:00")
	var se *scheduledError
	if !errors.As(err, &se) || se.Kind != "not-installed" || called {
		t.Fatalf("err = %v out = %q", err, out)
	}
	if !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("no way forward in %q", err)
	}
}

func TestScheduledMissingBinaryNamesTheSyncThatFinishesTheSetup(t *testing.T) {
	dir := configWith(t, scheduledConfig)
	alice := &scheduledFake{reply: func(string) (string, error) { return "", exitCodeErr(126) }}
	dialByUser(t, map[string]remote.Client{"alice": alice}, nil)

	r := listScheduled(t, dir)

	got := r.Workspaces[0]
	if got.Installed || got.Error == nil || got.Error.Kind != "setup-incomplete" {
		t.Fatalf("alice = %+v", got)
	}
	if !strings.Contains(got.Error.Message, "devmachine sync --machine main --tags scheduled") {
		t.Fatalf("no fix in %q", got.Error.Message)
	}
}

func TestScheduledMissingBinaryOnAWorkspaceWithoutThePackage(t *testing.T) {
	dir := configWith(t, scheduledConfig)
	bob := &scheduledFake{reply: func(string) (string, error) { return "", exitCodeErr(127) }}
	dialByUser(t, map[string]remote.Client{"bob": bob}, nil)

	_, err := execute(t, "--config", dir, "scheduled", "pause", "t_000001", "--workspace", "bob")
	var se *scheduledError
	if !errors.As(err, &se) || se.Kind != "not-installed" {
		t.Fatalf("err = %v", err)
	}
}

func TestScheduledScriptChecksForTheBinaryFirst(t *testing.T) {
	script := scheduledScript([]string{"list"}, nil)
	check, run, ok := strings.Cut(script, "\n")
	if !ok || !strings.Contains(check, "-x "+scheduledBinary) || !strings.Contains(check, "exit 127") {
		t.Fatalf("script = %q", script)
	}
	if !strings.HasPrefix(run, "exec "+scheduledBinary) {
		t.Fatalf("script = %q", script)
	}
}

// stubScheduledSetup replaces the package edit and the sync, recording each
// step into order.
func stubScheduledSetup(t *testing.T, order *[]string, syncErr error) {
	t.Helper()
	origEdit, origSync, origAvail := editPackageFn, runSyncFn, scheduledPackageAvailable
	editPackageFn = func(_ *cobra.Command, _ *options, name, workspace string, add, _, yes bool) error {
		*order = append(*order, fmt.Sprintf("edit %s %s %v %v", name, workspace, add, yes))
		return nil
	}
	runSyncFn = func(_ *cobra.Command, o *options, _, yes bool, tags []string) error {
		*order = append(*order, fmt.Sprintf("sync %s %v %v", o.machine, yes, tags))
		return syncErr
	}
	scheduledPackageAvailable = func(context.Context, *options, config.Config) (bool, error) { return true, nil }
	t.Cleanup(func() { editPackageFn, runSyncFn, scheduledPackageAvailable = origEdit, origSync, origAvail })
}

func TestScheduledAddFinishesAnIncompleteSetup(t *testing.T) {
	dir := configWith(t, scheduledConfig)
	var order []string
	stubScheduledSetup(t, &order, nil)
	synced := func() bool { return slices.Contains(order, "sync main true [scheduled]") }
	alice := &scheduledFake{reply: func(script string) (string, error) {
		order = append(order, "put")
		if !synced() {
			return "", exitCodeErr(126)
		}
		task := decodePutBody(t, script)
		task.ID = "t_000004"
		body, _ := json.Marshal(map[string]any{"protocol": 1, "task": task})
		return string(body), nil
	}}
	dialByUser(t, map[string]remote.Client{"alice": alice}, nil)

	out, err := execute(t, "--config", dir, "scheduled", "add", "--workspace", "alice", "--name", "x",
		"--folder", "~", "--command", "date", "--every", "daily", "--at", "09:00", "--yes")
	if err != nil {
		t.Fatal(err, out)
	}
	want := []string{"put", "edit scheduled alice true true", "sync main true [scheduled]", "put"}
	if strings.Join(order, "|") != strings.Join(want, "|") {
		t.Fatalf("order = %q", order)
	}
	if !strings.Contains(out, "t_000004") {
		t.Fatalf("out = %q", out)
	}
}

func TestScheduledAddAsksBeforeFinishingAnIncompleteSetup(t *testing.T) {
	dir := configWith(t, scheduledConfig)
	var order []string
	stubScheduledSetup(t, &order, nil)
	alice := &scheduledFake{reply: func(string) (string, error) { return "", exitCodeErr(127) }}
	dialByUser(t, map[string]remote.Client{"alice": alice}, nil)

	out, err := executeIn(t, "", "--config", dir, "scheduled", "add", "--workspace", "alice", "--name", "x",
		"--folder", "~", "--command", "date", "--every", "daily", "--at", "09:00")
	var se *scheduledError
	if !errors.As(err, &se) || se.Kind != "setup-incomplete" || len(order) != 0 {
		t.Fatalf("err = %v order = %q out = %q", err, order, out)
	}
}

func TestScheduledAddSaysWhatFailedInTheSetup(t *testing.T) {
	dir := configWith(t, scheduledConfig)
	var order []string
	stubScheduledSetup(t, &order, errors.New(`1 task(s) failed on main, first "scheduled : Unpack the release": `+
		`Failed to find handler: Process exited with status 2`))
	dialByUser(t, map[string]remote.Client{}, nil)

	_, err := execute(t, "--config", dir, "scheduled", "add", "--workspace", "bob", "--name", "x",
		"--folder", "~", "--command", "date", "--every", "daily", "--at", "09:00", "--yes")
	if err == nil {
		t.Fatal("a failed setup was reported as success")
	}
	for _, want := range []string{"main", `"scheduled : Unpack the release"`, "Failed to find handler",
		"devmachine sync --machine main --tags scheduled"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the error leaves out %q: %v", want, err)
		}
	}
	var se *scheduledError
	if !errors.As(err, &se) || se.Kind != "setup-failed" {
		t.Fatalf("err = %#v", err)
	}
}
