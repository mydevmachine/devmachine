package commands

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mydevmachine/devmachine/internal/config"
	"github.com/mydevmachine/devmachine/internal/history"
	"github.com/mydevmachine/devmachine/internal/remote"
)

// inputRemote records each command it streams and the stdin that came with it.
type inputRemote struct {
	fakeRemote
	commands, inputs *[]string
}

func (r inputRemote) Stream(_ context.Context, command string, _, _ io.Writer) error {
	*r.commands = append(*r.commands, command)
	*r.inputs = append(*r.inputs, "")
	return nil
}

func (r inputRemote) StreamInput(_ context.Context, command string, stdin io.Reader, _, _ io.Writer) error {
	body, err := io.ReadAll(stdin)
	if err != nil {
		return err
	}
	*r.commands = append(*r.commands, command)
	*r.inputs = append(*r.inputs, string(body))
	return nil
}

var hostileWords = []string{"x; rm -rf ~", "$(id)", "it's", "", `ends in \`, `; touch pwned ; #`, "two\nlines", `\'`, "`id`"}

func TestRunArgvSendsTheProgramOnShellInput(t *testing.T) {
	var commands, inputs []string
	dialing(t, inputRemote{commands: &commands, inputs: &inputs})
	dir := configWith(t, twoMachineConfig)

	args := append([]string{"--config", dir, "--machine", "main", "run", "--argv", "--", "echo"}, hostileWords...)
	if _, err := execute(t, args...); err != nil {
		t.Fatal(err)
	}
	if len(commands) != 1 || commands[0] != "/bin/sh -s" {
		t.Fatalf("ran %q, want exactly /bin/sh -s", commands)
	}
	if want := argvScript(append([]string{"echo"}, hostileWords...)); inputs[0] != want {
		t.Fatalf("sent %q, want %q", inputs[0], want)
	}
}

func TestRunArgvPutsThePathPrefixInTheInput(t *testing.T) {
	var commands, inputs []string
	dialing(t, inputRemote{commands: &commands, inputs: &inputs})
	dir := configWith(t, twoMachineConfig)
	saveFacts(t, dir, "main", observedMac)

	if _, err := execute(t, "--config", dir, "--machine", "main", "run", "--argv", "--", "port", "installed"); err != nil {
		t.Fatal(err)
	}
	if commands[0] != "/bin/sh -s" {
		t.Fatalf("ran %q", commands[0])
	}
	if !strings.HasPrefix(inputs[0], "PATH='/opt/local/bin:/opt/local/sbin'") || !strings.HasSuffix(inputs[0], "exec 'port' 'installed'\n") {
		t.Fatalf("sent %q", inputs[0])
	}
}

func TestArgvScriptReachesTheProgramWordForWordUnderSh(t *testing.T) {
	words := append([]string{"printf", "[%s]\n"}, hostileWords...)
	sh := exec.CommandContext(context.Background(), "/bin/sh", "-s")
	sh.Stdin = strings.NewReader(argvScript(words))
	out, err := sh.Output()
	if err != nil {
		t.Fatal(err)
	}
	var want strings.Builder
	for _, w := range hostileWords {
		want.WriteString("[" + w + "]\n")
	}
	if string(out) != want.String() {
		t.Fatalf("got %q, want %q", out, want.String())
	}
}

func TestRunArgvOnASelfMachineRunsTheWordsWithoutInput(t *testing.T) {
	var commands, inputs []string
	dialing(t, inputRemote{commands: &commands, inputs: &inputs})
	dir := configWith(t, "machines:\n  - name: here\n    self: true\n")

	if _, err := execute(t, "--config", dir, "--machine", "here", "run", "--argv", "--", "echo", "it's"); err != nil {
		t.Fatal(err)
	}
	if want := `'echo' 'it'\''s'`; len(commands) != 1 || commands[0] != want || inputs[0] != "" {
		t.Fatalf("ran %q with input %q, want %q", commands, inputs, want)
	}
}

func TestRunNoLogLeavesTheLogAlone(t *testing.T) {
	dialing(t, fakeRemote{out: "ok\n"})
	dir := configWith(t, twoMachineConfig)

	if _, err := execute(t, "--config", dir, "--machine", "main", "run", "--no-log", "uptime"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, history.FileName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a --no-log run reached the command log: %v", err)
	}
	if _, err := execute(t, "--config", dir, "--machine", "main", "run", "uptime"); err != nil {
		t.Fatal(err)
	}
	if lines := historyLines(t, dir); len(lines) != 1 {
		t.Fatalf("got %q", lines)
	}
}

func TestRunPackageScriptRunsTheMachinesCopy(t *testing.T) {
	var commands, inputs []string
	dialing(t, inputRemote{commands: &commands, inputs: &inputs})
	dir := configDirWithProviderAccepting(t, "cloudflare", []string{"zones"})

	_, err := execute(t, "--config", dir, "run", "--package", "cloudflare", "--script", "bin/provider", "--", "status", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if len(commands) != 1 || commands[0] != "/bin/sh -s" {
		t.Fatalf("ran %q, want exactly /bin/sh -s", commands)
	}
	if !strings.HasSuffix(inputs[0], "exec /opt/devmachine/roles.local/cloudflare/bin/provider status --json\n") {
		t.Fatalf("sent %q", inputs[0])
	}
	if !strings.Contains(inputs[0], "set -a") {
		t.Fatalf("the package's credential was not sourced: %q", inputs[0])
	}
}

func TestRunPackageScriptSendsNoValueInTheCommand(t *testing.T) {
	var commands, inputs []string
	dialing(t, inputRemote{commands: &commands, inputs: &inputs})
	dir := configDirWithProviderAccepting(t, "cloudflare", []string{"zones"})

	args := append([]string{"--config", dir, "run", "--package", "cloudflare", "--script", "bin/provider", "--"}, hostileWords...)
	if _, err := execute(t, args...); err != nil {
		t.Fatal(err)
	}
	if len(commands) != 1 || commands[0] != "/bin/sh -s" {
		t.Fatalf("ran %q, want exactly /bin/sh -s", commands)
	}
	quoted := make([]string, len(hostileWords))
	for i, w := range hostileWords {
		quoted[i] = quoteForShell(w)
	}
	if want := strings.Join(quoted, " ") + "\n"; !strings.HasSuffix(inputs[0], want) {
		t.Fatalf("sent %q, want it to end in %q", inputs[0], want)
	}
}

func TestRunPackageScriptRefusals(t *testing.T) {
	dialing(t, fakeRemote{})
	dir := configDirWithProviderAccepting(t, "cloudflare", []string{"zones"})
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"--script", "../../etc/passwd", "--"}, "names a file inside the package"},
		{[]string{"--script", "/etc/passwd", "--"}, "names a file inside the package"},
		{[]string{"--script", "bin/missing", "--"}, "cloudflare has no file bin/missing"},
		{[]string{"--script", "bin/provider", "status"}, "the script's arguments follow `--`"},
	}
	for _, tc := range cases {
		args := append([]string{"--config", dir, "run", "--package", "cloudflare"}, tc.args...)
		_, err := execute(t, args...)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%v: got %v", tc.args, err)
		}
	}
}

func TestRunFlagsThatDoNotGoTogether(t *testing.T) {
	var runs []string
	dialing(t, factsRemote{runs: &runs})
	dir := configWith(t, twoMachineConfig)
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"run", "--argv", "echo", "hi"}, "with --argv, the program and its arguments follow `--`"},
		{[]string{"run", "--script", "bin/x", "--", "a"}, "--script names a file inside a package: add --package <name>"},
		{[]string{"run", "--argv", "--package", "cloudflare", "--", "zones"}, "--argv and --package do not go together"},
	}
	for _, tc := range cases {
		_, err := execute(t, append([]string{"--config", dir, "--machine", "main"}, tc.args...)...)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%v: got %v", tc.args, err)
		}
	}
	if len(runs) != 0 {
		t.Fatalf("a refused run still ran %q", runs)
	}
}

func TestRunPackageMistakesExitOneBeforeConnecting(t *testing.T) {
	dir := configDirWithProviderAccepting(t, "cloudflare", []string{"zones"})
	dialed := false
	dialMux = func(context.Context, config.Machine, string) (remote.Client, string, error) {
		dialed = true
		return fakeRemote{}, "203.0.113.10", nil
	}
	t.Cleanup(func() { dialMux = remote.DialMux })
	for _, args := range [][]string{
		{"zones"},
		{"--script", "../../etc/passwd", "--"},
		{"--script", "/etc/passwd", "--"},
		{"--script", "bin/provider", "status"},
		{"--script", "bin/provider", "status", "--", "more"},
	} {
		_, err := execute(t, append([]string{"--config", dir, "run", "--package", "cloudflare"}, args...)...)
		if got := exitCode(err); got != 1 {
			t.Errorf("%v: exit %d from %v", args, got, err)
		}
	}
	if dialed {
		t.Fatal("a wrong call connected before it was refused")
	}
}

func TestRunPackageScriptPassesAnEmptyWord(t *testing.T) {
	var commands, inputs []string
	dialing(t, inputRemote{commands: &commands, inputs: &inputs})
	dir := configDirWithProviderAccepting(t, "cloudflare", []string{"zones"})

	_, err := execute(t, "--config", dir, "run", "--package", "cloudflare", "--script", "bin/provider", "--", "", "a b")
	if err != nil {
		t.Fatal(err)
	}
	if len(inputs) != 1 || !strings.HasSuffix(inputs[0], "/bin/provider '' 'a b'\n") {
		t.Fatalf("sent %q", inputs)
	}
}
