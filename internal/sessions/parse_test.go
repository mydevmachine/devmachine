package sessions

import (
	"os/exec"
	"reflect"
	"strings"
	"testing"
)

func lines(ls ...string) string {
	return strings.Join(ls, "\n") + "\n"
}

func TestParse(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   Snapshot
	}{
		{
			name: "two sessions",
			output: lines(
				"@@NOW@@", "1000000",
				"@@SESSIONS@@",
				"web\t2\t990000\t999990\t1!,2\tclaude\t/home/alice/acme-web",
				"api\t1\t980000\t999000\t\tzsh\t/home/alice/acme-api",
				"@@PANES@@",
				"web\tzsh\t10",
				"web\tclaude\t12",
				"api\tzsh\t20",
				"@@PS@@",
				"   10     1 zsh",
				"   11    10 claude",
				"   12     1 claude",
				"   20     1 -zsh",
				"@@GIT@@",
				"/home/alice/acme-web\tmain",
				"/home/alice/acme-api\tfeature/login",
			),
			want: Snapshot{
				Now: 1000000,
				Sessions: []rawSession{
					{Name: "web", Windows: 2, Created: 990000, Activity: 999990, Alerts: "1!,2", Command: "claude", Path: "/home/alice/acme-web"},
					{Name: "api", Windows: 1, Created: 980000, Activity: 999000, Alerts: "", Command: "zsh", Path: "/home/alice/acme-api"},
				},
				Panes: []rawPane{
					{Session: "web", Command: "zsh", PID: 10},
					{Session: "web", Command: "claude", PID: 12},
					{Session: "api", Command: "zsh", PID: 20},
				},
				Procs: []proc{
					{PID: 10, PPID: 1, Comm: "zsh"},
					{PID: 11, PPID: 10, Comm: "claude"},
					{PID: 12, PPID: 1, Comm: "claude"},
					{PID: 20, PPID: 1, Comm: "-zsh"},
				},
				Branches: map[string]string{
					"/home/alice/acme-web": "main",
					"/home/alice/acme-api": "feature/login",
				},
			},
		},
		{
			name:   "no tmux",
			output: lines("@@NO_TMUX@@"),
			want:   Snapshot{NoTmux: true},
		},
		{
			name: "no server running",
			output: lines(
				"@@NOW@@", "1000000",
				"@@SESSIONS@@",
				"@@PANES@@",
				"@@PS@@",
				"    1     0 init",
				"@@GIT@@",
			),
			want: Snapshot{
				Now:      1000000,
				Procs:    []proc{{PID: 1, PPID: 0, Comm: "init"}},
				Branches: map[string]string{},
			},
		},
		{
			name: "path with spaces and a pipe",
			output: lines(
				"@@NOW@@", "1000000",
				"@@SESSIONS@@",
				"odd\t1\t1\t2\t\tvim\t/home/alice/my repo|x",
				"@@PANES@@",
				"odd\tvim\t30",
				"@@PS@@",
				"@@GIT@@",
				"/home/alice/my repo|x\tfix/odd",
			),
			want: Snapshot{
				Now: 1000000,
				Sessions: []rawSession{
					{Name: "odd", Windows: 1, Created: 1, Activity: 2, Command: "vim", Path: "/home/alice/my repo|x"},
				},
				Panes:    []rawPane{{Session: "odd", Command: "vim", PID: 30}},
				Branches: map[string]string{"/home/alice/my repo|x": "fix/odd"},
			},
		},
		{
			name: "detached head, non-git path and a path with a tab",
			output: lines(
				"@@NOW@@", "1000000",
				"@@SESSIONS@@",
				"@@PANES@@",
				"@@PS@@",
				"@@GIT@@",
				"/home/alice/acme-web\tab12cd3",
				"/tmp\t",
				"/home/alice/a\tb\tmain",
			),
			want: Snapshot{
				Now: 1000000,
				Branches: map[string]string{
					"/home/alice/acme-web": "ab12cd3",
					"/tmp":                 "",
				},
			},
		},
		{
			name: "short session line is skipped",
			output: lines(
				"@@NOW@@", "1000000",
				"@@SESSIONS@@",
				"broken\t1\t2",
				"ok\t1\t1\t2\t\tzsh\t/home/bob",
				"@@PANES@@",
				"@@PS@@",
				"@@GIT@@",
			),
			want: Snapshot{
				Now: 1000000,
				Sessions: []rawSession{
					{Name: "ok", Windows: 1, Created: 1, Activity: 2, Command: "zsh", Path: "/home/bob"},
				},
				Branches: map[string]string{},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse(tt.output)
			if err != nil {
				t.Fatalf("Parse returned %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Parse =\n%#v\nwant\n%#v", got, tt.want)
			}
		})
	}
}

func TestParseWithoutMachineClockFails(t *testing.T) {
	_, err := Parse(lines("@@SESSIONS@@", "@@PANES@@", "@@PS@@", "@@GIT@@"))
	if err == nil || !strings.Contains(err.Error(), "missing machine clock") {
		t.Fatalf("Parse error = %v, want missing machine clock", err)
	}
}

func TestScriptListsEverySection(t *testing.T) {
	script := Script()
	for _, want := range []string{"@@NO_TMUX@@", "@@NOW@@", "@@SESSIONS@@", "@@PANES@@", "@@PS@@", "@@GIT@@", "session_alerts"} {
		if !strings.Contains(script, want) {
			t.Errorf("Script() does not contain %q", want)
		}
	}
}

func TestScriptRunsUnderShWhateverTheLoginShell(t *testing.T) {
	script := Script()
	body, ok := strings.CutPrefix(script, "sh -c '")
	if !ok || !strings.HasSuffix(body, "'") {
		t.Fatalf("Script() = %q, want it wrapped in sh -c '...'", script)
	}
	if strings.Contains(strings.TrimSuffix(body, "'"), "'") {
		t.Error("Script() body holds a single quote, which ends the sh -c argument early")
	}
}

func TestScriptIsValidSh(t *testing.T) {
	body := strings.TrimSuffix(strings.TrimPrefix(Script(), "sh -c '"), "'")
	if out, err := exec.Command("sh", "-n", "-c", body).CombinedOutput(); err != nil {
		t.Fatalf("sh -n rejected the script: %v\n%s", err, out)
	}
}

func TestScriptSeparatesTmuxFieldsWithTabs(t *testing.T) {
	if !strings.Contains(Script(), "#{session_name}\t#{session_windows}") {
		t.Error("Script() does not separate tmux fields with a tab")
	}
}
