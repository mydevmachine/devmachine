package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mydevmachine/devmachine/internal/config"
	"github.com/mydevmachine/devmachine/internal/remote"
)

// localShell runs what download sends on this computer, so a test's
// "machine" is a temporary folder named by absolute paths.
type localShell struct{}

func (localShell) Run(ctx context.Context, command string) (string, error) {
	var out bytes.Buffer
	err := localShell{}.Stream(ctx, command, &out, nil)
	return out.String(), err
}

func (localShell) RunInput(ctx context.Context, command string, _ io.Reader) (string, error) {
	return localShell{}.Run(ctx, command)
}

func (localShell) Stream(ctx context.Context, command string, stdout, stderr io.Writer) error {
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Stdout = stdout
	if stderr != nil {
		cmd.Stderr = stderr
	}
	return cmd.Run()
}

func (localShell) Upload(context.Context, string, io.Reader) error { return nil }
func (localShell) Close() error                                    { return nil }

// downloadingFrom answers every dial with a localShell and returns a folder
// standing for the machine, one standing for ~/Downloads, and the dials made.
func downloadingFrom(t *testing.T) (machine, downloads string, calls *[]dialCall) {
	t.Helper()
	machine, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	downloads, err = filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var made []dialCall
	stub := func(_ context.Context, m config.Machine, user string) (remote.Client, string, error) {
		made = append(made, dialCall{machine: m.Name, user: user})
		return localShell{}, "203.0.113.10", nil
	}
	origDial, origDialMux, origDir := dial, dialMux, defaultDownloadDir
	dial, dialMux = stub, stub
	defaultDownloadDir = func() (string, error) { return downloads, nil }
	t.Cleanup(func() { dial, dialMux, defaultDownloadDir = origDial, origDialMux, origDir })
	return machine, downloads, &made
}

func onMachine(t *testing.T, machine, rel, body string) string {
	t.Helper()
	p := filepath.Join(machine, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestDownloadSavesIntoDownloadsAsTheWorkspaceAccount(t *testing.T) {
	machine, downloads, calls := downloadingFrom(t)
	dir := configWith(t, twoMachineConfig)
	remoteFile := onMachine(t, machine, "proj/photo ção.png", "png")

	out, stderr, err := executeSplit(t, "--config", dir, "download", remoteFile, "--workspace", "bob")
	if err != nil {
		t.Fatalf("download returned %v (%s)", err, stderr)
	}
	want := filepath.Join(downloads, "photo ção.png")
	if out != want+"\n" {
		t.Fatalf("stdout %q, want only %q", out, want)
	}
	if body, _ := os.ReadFile(want); string(body) != "png" {
		t.Fatalf("body %q", body)
	}
	if len(*calls) != 1 || (*calls)[0] != (dialCall{machine: "sandbox", user: "bob-dev"}) {
		t.Fatalf("dialed %#v, want sandbox as bob-dev", *calls)
	}
}

func TestDownloadToANamedFolderNeverOverwrites(t *testing.T) {
	machine, _, _ := downloadingFrom(t)
	dir := configWith(t, oneMachineConfig)
	to := t.TempDir()
	remoteFile := onMachine(t, machine, "report.pdf", "new")
	if err := os.WriteFile(filepath.Join(to, "report.pdf"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := execute(t, "--config", dir, "download", remoteFile, "--to", to)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(to, "report-2.pdf")
	if strings.TrimSpace(out) != want {
		t.Fatalf("got %q, want %q", out, want)
	}
	if old, _ := os.ReadFile(filepath.Join(to, "report.pdf")); string(old) != "old" {
		t.Fatalf("the existing file was overwritten: %q", old)
	}
}

func TestDownloadPacksAFolderAsATarball(t *testing.T) {
	machine, downloads, _ := downloadingFrom(t)
	dir := configWith(t, oneMachineConfig)
	onMachine(t, machine, "site/index.html", "hi")

	out, err := execute(t, "--config", dir, "download", filepath.Join(machine, "site"))
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(downloads, "site.tar.gz")
	if strings.TrimSpace(out) != want {
		t.Fatalf("got %q, want %q", out, want)
	}
}

func TestDownloadPrintsJSONAndTriesEveryPath(t *testing.T) {
	machine, downloads, _ := downloadingFrom(t)
	dir := configWith(t, oneMachineConfig)
	good := onMachine(t, machine, "a.txt", "four")
	missing := filepath.Join(machine, "missing.txt")

	out, _, err := executeSplit(t, "--config", dir, "--format", "json", "download", missing, good)
	if err == nil {
		t.Fatal("expected a failure for the missing path")
	}
	var got []map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if len(got) != 2 {
		t.Fatalf("got %v", got)
	}
	if got[0]["remote"] != missing || !strings.Contains(got[0]["error"].(string), "does not exist") {
		t.Fatalf("first entry %v", got[0])
	}
	want := map[string]any{"remote": good, "local": filepath.Join(downloads, "a.txt"), "bytes": float64(4), "folder": false}
	for k, v := range want {
		if got[1][k] != v {
			t.Fatalf("second entry %s = %v, want %v (%v)", k, got[1][k], v, got[1])
		}
	}
	if _, ok := got[1]["error"]; ok {
		t.Fatalf("a downloaded file carries an error: %v", got[1])
	}
}

func TestDownloadNamesEachFailureOnStderr(t *testing.T) {
	machine, downloads, _ := downloadingFrom(t)
	dir := configWith(t, oneMachineConfig)
	good := onMachine(t, machine, "a.txt", "x")

	out, stderr, err := executeSplit(t, "--config", dir, "download", filepath.Join(machine, "nope.txt"), good)
	if err == nil || !strings.Contains(err.Error(), "1 of 2") {
		t.Fatalf("got %v", err)
	}
	if out != filepath.Join(downloads, "a.txt")+"\n" {
		t.Fatalf("stdout %q", out)
	}
	if !strings.Contains(stderr, "nope.txt") {
		t.Fatalf("stderr does not name the failure: %q", stderr)
	}
}

func TestDownloadRefusesAMissingDestinationBeforeConnecting(t *testing.T) {
	_, _, calls := downloadingFrom(t)
	dir := configWith(t, oneMachineConfig)

	_, err := execute(t, "--config", dir, "download", "a.txt", "--to", filepath.Join(t.TempDir(), "nope"))
	if err == nil || !strings.Contains(err.Error(), "--to") {
		t.Fatalf("got %v", err)
	}
	if len(*calls) != 0 {
		t.Fatalf("it dialed anyway: %#v", *calls)
	}
}

func TestDownloadNeverGuessesBetweenSeveralMachines(t *testing.T) {
	_, _, calls := downloadingFrom(t)
	dir := configWith(t, twoMachineConfig)

	_, err := execute(t, "--config", dir, "download", "a.txt")
	if err == nil || !strings.Contains(err.Error(), "--machine") {
		t.Fatalf("got %v", err)
	}
	if len(*calls) != 0 {
		t.Fatalf("it dialed a machine nobody named: %#v", *calls)
	}
}

func TestDownloadRefusesYourOwnComputer(t *testing.T) {
	_, _, calls := downloadingFrom(t)
	dir := configWith(t, "machines:\n  - name: laptop\n    self: true\n")

	_, err := execute(t, "--config", dir, "download", "a.txt")
	if err == nil || !strings.Contains(err.Error(), "self: true") {
		t.Fatalf("got %v", err)
	}
	if len(*calls) != 0 {
		t.Fatalf("it dialed anyway: %#v", *calls)
	}
}

func TestDownloadRecordsEachPathInTheCommandLog(t *testing.T) {
	machine, _, _ := downloadingFrom(t)
	dir := configWith(t, oneMachineConfig)

	if _, err := execute(t, "--config", dir, "download", onMachine(t, machine, "a.txt", "x"), "--workspace", "acme"); err != nil {
		t.Fatal(err)
	}
	line := historyLines(t, dir)[0]
	for _, want := range []string{"workspace acme", "download", "a.txt", "ok"} {
		if !strings.Contains(line, want) {
			t.Fatalf("the log line leaves out %q: %q", want, line)
		}
	}
}

type progressLine struct {
	Remote string `json:"remote"`
	Done   int64  `json:"done"`
	Total  int64  `json:"total"`
}

func progressLines(t *testing.T, stderr string) []progressLine {
	t.Helper()
	var lines []progressLine
	for _, raw := range strings.Split(strings.TrimSpace(stderr), "\n") {
		var l progressLine
		if err := json.Unmarshal([]byte(raw), &l); err != nil {
			t.Fatalf("stderr line %q is not a progress line: %v", raw, err)
		}
		lines = append(lines, l)
	}
	return lines
}

func TestDownloadProgressSizesEveryFileBeforeTheFirstByte(t *testing.T) {
	machine, _, _ := downloadingFrom(t)
	dir := configWith(t, oneMachineConfig)
	a := onMachine(t, machine, "a.txt", "four")
	b := onMachine(t, machine, "b.txt", "seven!!")

	out, stderr, err := executeSplit(t, "--config", dir, "--format", "json", "download", "--progress", a, b)
	if err != nil {
		t.Fatalf("download returned %v (%s)", err, stderr)
	}
	var results []map[string]any
	if err := json.Unmarshal([]byte(out), &results); err != nil || len(results) != 2 {
		t.Fatalf("stdout is not the usual result: %v\n%s", err, out)
	}
	lines := progressLines(t, stderr)
	start := []progressLine{{Remote: a, Total: 4}, {Remote: b, Total: 7}}
	if len(lines) < 4 {
		t.Fatalf("got %+v, want a start and an end line per file", lines)
	}
	for i, want := range start {
		if lines[i] != want {
			t.Fatalf("line %d = %+v, want %+v", i, lines[i], want)
		}
	}
	last := map[string]progressLine{}
	for _, l := range lines {
		last[l.Remote] = l
	}
	if last[a].Done != 4 || last[b].Done != 7 {
		t.Fatalf("final lines %+v, want each file whole", last)
	}
}

func TestDownloadProgressGivesAFolderNoTotal(t *testing.T) {
	machine, _, _ := downloadingFrom(t)
	dir := configWith(t, oneMachineConfig)
	onMachine(t, machine, "site/index.html", "hi")
	site := filepath.Join(machine, "site")

	_, stderr, err := executeSplit(t, "--config", dir, "download", "--progress", site)
	if err != nil {
		t.Fatalf("download returned %v (%s)", err, stderr)
	}
	lines := progressLines(t, stderr)
	end := lines[len(lines)-1]
	if lines[0] != (progressLine{Remote: site}) || end.Total != 0 || end.Done == 0 {
		t.Fatalf("got %+v, want a zero total and the archive's bytes at the end", lines)
	}
}

func TestDownloadProgressLeavesOutAPathThatFailed(t *testing.T) {
	machine, _, _ := downloadingFrom(t)
	dir := configWith(t, oneMachineConfig)
	good := onMachine(t, machine, "a.txt", "four")
	missing := filepath.Join(machine, "missing.txt")

	_, stderr, _ := executeSplit(t, "--config", dir, "--format", "json", "download", "--progress", missing, good)
	for _, l := range progressLines(t, stderr) {
		if l.Remote != good {
			t.Fatalf("progress for %q, which was never read", l.Remote)
		}
	}
}

func TestDownloadWithoutProgressKeepsStderrEmpty(t *testing.T) {
	machine, _, _ := downloadingFrom(t)
	dir := configWith(t, oneMachineConfig)
	a := onMachine(t, machine, "a.txt", "four")

	_, stderr, err := executeSplit(t, "--config", dir, "--format", "json", "download", a)
	if err != nil || stderr != "" {
		t.Fatalf("err %v, stderr %q", err, stderr)
	}
}
