package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mydevmachine/devmachine/internal/config"
	"github.com/mydevmachine/devmachine/internal/remote"
	"github.com/mydevmachine/devmachine/internal/scheduled"
)

func scheduledTestMachine(t *testing.T) config.Machine {
	t.Helper()
	host, port := os.Getenv("DEVMACHINE_TEST_HOST"), os.Getenv("DEVMACHINE_TEST_PORT")
	key, knownHosts := os.Getenv("DEVMACHINE_TEST_KEY"), os.Getenv("DEVMACHINE_TEST_KNOWN_HOSTS")
	if host == "" || port == "" || key == "" || knownHosts == "" {
		t.Skip("no test VPS: run `eval \"$(scripts/fake-vps.sh env)\"` first")
	}
	n, err := strconv.Atoi(port)
	if err != nil {
		t.Fatalf("DEVMACHINE_TEST_PORT is not a number: %v", err)
	}
	user := os.Getenv("DEVMACHINE_TEST_USER")
	if user == "" {
		user = "root"
	}
	return config.Machine{Name: "sandbox", Hosts: []config.Host{{Address: host}}, User: user, Port: n,
		Key: key, KnownHostsFile: knownHosts}
}

// buildForMachine cross-compiles this tree for the test machine, which is
// what the scheduled package will put there.
func buildForMachine(t *testing.T, ctx context.Context, client remote.Client) []byte {
	t.Helper()
	arch, err := client.Run(ctx, "uname -m")
	if err != nil {
		t.Fatal(err)
	}
	goarch := map[string]string{"aarch64": "arm64", "arm64": "arm64", "x86_64": "amd64"}[strings.TrimSpace(arch)]
	if goarch == "" {
		t.Skipf("no build for %q", arch)
	}
	out := filepath.Join(t.TempDir(), "devmachine")
	build := exec.Command("go", "build", "-o", out, "../../cmd/devmachine")
	build.Env = append(os.Environ(), "GOOS=linux", "GOARCH="+goarch, "CGO_ENABLED=0")
	if msg, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building for the machine: %v\n%s", err, msg)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestScheduledWorkspaceSideOnTheTestMachine(t *testing.T) {
	m := scheduledTestMachine(t)
	ctx := context.Background()
	client, _, err := remote.Dial(ctx, m, "")
	if err != nil {
		t.Fatalf("Dial returned %v", err)
	}
	defer client.Close()

	home := "/tmp/dm-scheduled-test"
	t.Cleanup(func() { _, _ = client.Run(context.Background(), "rm -rf "+home) })
	fakeClaude := "printf '%s\\n' '#!/bin/sh' 'printf \"%s\\\\n\" \"$@\"' > " + home + "/bin/claude && chmod +x " + home + "/bin/claude"
	if _, err := client.Run(ctx, "rm -rf "+home+" && mkdir -p "+home+"/.devmachine/scheduled "+home+"/proj "+home+"/bin && "+fakeClaude); err != nil {
		t.Fatal(err)
	}
	if _, err := client.RunInput(ctx, "cat > "+home+"/.devmachine/scheduled/devmachine && chmod +x "+home+"/.devmachine/scheduled/devmachine",
		bytes.NewReader(buildForMachine(t, ctx, client))); err != nil {
		t.Fatal(err)
	}

	call := func(args []string, stdin []byte, v any) {
		t.Helper()
		script := "export HOME=" + home + " SHELL=/bin/sh PATH=" + home + "/bin:$PATH\n" + scheduledScript(args, stdin)
		out, err := client.RunInput(ctx, remote.ShellOnInput, strings.NewReader(script))
		if err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
		if v != nil {
			if err := json.Unmarshal([]byte(out), v); err != nil {
				t.Fatalf("%v: %v\n%s", args, err, out)
			}
		}
	}

	risky := `it's "quoted" $(touch ` + home + `/pwned) ` + "`touch " + home + "/pwned`\nline two"
	task := scheduled.Task{Name: risky, Folder: "~/proj", Schedule: scheduled.Schedule{Every: "daily", At: "09:00", Timezone: "America/Sao_Paulo"},
		Action: scheduled.Action{Kind: "command", Command: "pwd; echo done"}}
	body, _ := json.Marshal(task)
	var put struct {
		Protocol int            `json:"protocol"`
		Task     scheduled.Task `json:"task"`
	}
	call([]string{"put"}, body, &put)
	if put.Protocol != scheduled.Protocol || !scheduled.ValidTaskID(put.Task.ID) || put.Task.Name != risky {
		t.Fatalf("put = %+v", put)
	}

	var run struct {
		RunID string `json:"run_id"`
	}
	call([]string{"run", put.Task.ID}, nil, &run)

	var runs struct {
		Runs []scheduled.Run `json:"runs"`
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		call([]string{"runs", put.Task.ID}, nil, &runs)
		if len(runs.Runs) == 1 && runs.Runs[0].Status != scheduled.StatusRunning {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the run did not end: %+v", runs.Runs)
		}
		time.Sleep(300 * time.Millisecond)
	}
	if r := runs.Runs[0]; r.ID != run.RunID || r.Status != scheduled.StatusSucceeded || r.Trigger != scheduled.TriggerManual {
		t.Fatalf("run = %+v", r)
	}

	out, err := client.RunInput(ctx, remote.ShellOnInput, strings.NewReader("export HOME="+home+"\n"+
		scheduledScript([]string{"log", put.Task.ID, run.RunID}, nil)))
	if err != nil || out != home+"/proj\ndone\n" {
		t.Fatalf("log = %q err = %v", out, err)
	}
	if _, err := client.Run(ctx, "test ! -e "+home+"/pwned"); err != nil {
		t.Fatal("a quoted value was run by a shell")
	}

	promptTask := scheduled.Task{Name: "p", Folder: "~", Schedule: task.Schedule,
		Action: scheduled.Action{Kind: "prompt", Harness: "claude", Prompt: risky, PermissionMode: "acceptEdits"}}
	body, _ = json.Marshal(promptTask)
	call([]string{"put"}, body, &put)
	var execReply struct {
		Run scheduled.Run `json:"run"`
	}
	call([]string{"exec", put.Task.ID, "--trigger", "manual"}, nil, &execReply)
	if execReply.Run.Status != scheduled.StatusSucceeded {
		t.Fatalf("prompt run = %+v", execReply.Run)
	}
	out, err = client.RunInput(ctx, remote.ShellOnInput, strings.NewReader("export HOME="+home+"\n"+
		scheduledScript([]string{"log", put.Task.ID, execReply.Run.ID}, nil)))
	if err != nil || !strings.HasSuffix(out, "--permission-mode\nacceptEdits\n--\n"+risky+"\n") {
		t.Fatalf("prompt log = %q err = %v", out, err)
	}
	if _, err := client.Run(ctx, "test ! -e "+home+"/pwned"); err != nil {
		t.Fatal("the prompt was run by a shell")
	}

	var tick struct {
		Protocol int      `json:"protocol"`
		Started  []string `json:"started"`
	}
	call([]string{"tick"}, nil, &tick)
	if tick.Protocol != scheduled.Protocol {
		t.Fatalf("tick = %+v", tick)
	}
}
