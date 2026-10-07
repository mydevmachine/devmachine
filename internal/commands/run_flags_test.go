package commands

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mydevmachine/devmachine/internal/history"
)

func TestRunArgvQuotesEveryWord(t *testing.T) {
	var runs []string
	dialing(t, factsRemote{runs: &runs})
	dir := configWith(t, twoMachineConfig)

	_, err := execute(t, "--config", dir, "--machine", "main", "run", "--argv", "--", "echo", "x; rm -rf ~", "$(id)", "it's", "")
	if err != nil {
		t.Fatal(err)
	}
	want := `'echo' 'x; rm -rf ~' '$(id)' 'it'\''s' ''`
	if len(runs) != 1 || runs[0] != want {
		t.Fatalf("ran %q, want %q", runs, want)
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
	var runs []string
	dialing(t, factsRemote{runs: &runs})
	dir := configDirWithProviderAccepting(t, "cloudflare", []string{"zones"})

	_, err := execute(t, "--config", dir, "run", "--package", "cloudflare", "--script", "bin/provider", "--", "status", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || !strings.Contains(runs[0], "/opt/devmachine/roles.local/cloudflare/bin/provider status --json") {
		t.Fatalf("ran %q", runs)
	}
	if !strings.Contains(runs[0], "set -a") {
		t.Fatalf("the package's credential was not sourced: %q", runs[0])
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
