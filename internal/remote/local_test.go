package remote

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/mydevmachine/devmachine/internal/config"
)

func TestDialReturnsALocalClientForASelfMachine(t *testing.T) {
	client, address, err := Dial(context.Background(), config.Machine{Name: "mac", Self: true}, "")
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	if address != SelfAddress {
		t.Fatalf("got address %q", address)
	}
	if _, ok := client.(*localClient); !ok {
		t.Fatalf("got %T, want a local client", client)
	}
}

func TestLocalClientRunsACommand(t *testing.T) {
	client := &localClient{}
	out, err := client.Run(context.Background(), "echo hi")
	if err != nil {
		t.Fatal(err)
	}
	if out != "hi\n" {
		t.Fatalf("got %q", out)
	}
}

func TestLocalClientStreamsOutputAsItArrives(t *testing.T) {
	client := &localClient{}
	var out bytes.Buffer
	if err := client.Stream(context.Background(), "echo one", &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	if out.String() != "one\n" {
		t.Fatalf("got %q", out.String())
	}
}

func TestLocalClientReportsANonZeroExitAsAnError(t *testing.T) {
	client := &localClient{}
	if _, err := client.Run(context.Background(), "exit 3"); err == nil {
		t.Fatal("a non-zero exit was reported as success")
	}
	if err := client.Stream(context.Background(), "exit 3", io.Discard, io.Discard); err == nil {
		t.Fatal("a non-zero exit was reported as success")
	}
}

func TestLocalClientRunInputFeedsStdin(t *testing.T) {
	client := &localClient{}
	out, err := client.RunInput(context.Background(), "cat", bytes.NewReader([]byte("hello\n")))
	if err != nil {
		t.Fatal(err)
	}
	if out != "hello\n" {
		t.Fatalf("got %q", out)
	}
}

func TestLocalClientUploadExtractsATarballIntoADirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "bundle")
	tarball := tarballWith(t, map[string]string{
		"ansible.cfg":         "[defaults]\n",
		"roles/base/task.yml": "---\n",
	})

	client := &localClient{}
	if err := client.Upload(context.Background(), dir, bytes.NewReader(tarball)); err != nil {
		t.Fatal(err)
	}

	body, err := os.ReadFile(filepath.Join(dir, "ansible.cfg"))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "[defaults]\n" {
		t.Fatalf("got %q", body)
	}
	if _, err := os.Stat(filepath.Join(dir, "roles/base/task.yml")); err != nil {
		t.Fatal(err)
	}
}

func TestLocalClientUploadRefusesAnEntryEscapingTheDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "bundle")
	tarball := tarballWith(t, map[string]string{"../escaped.txt": "nope\n"})

	client := &localClient{}
	if err := client.Upload(context.Background(), dir, bytes.NewReader(tarball)); err == nil {
		t.Fatal("an entry outside the directory was extracted without complaint")
	}
}

// A command that fails says why on stderr; without it the only thing a person
// sees is "exit status 1".
func TestLocalClientPutsStderrInTheError(t *testing.T) {
	c := &localClient{}
	_, err := c.Run(context.Background(), "ls /no-such-dir-here")
	if err == nil || !strings.Contains(err.Error(), "No such file") {
		t.Fatalf("got %v", err)
	}
	_, err = c.RunInput(context.Background(), "cat >/dev/null; ls /no-such-dir-here", strings.NewReader("x"))
	if err == nil || !strings.Contains(err.Error(), "No such file") {
		t.Fatalf("got %v", err)
	}
}

// TestElevatedNeverUsesSudoOnYourOwnComputer: a self machine runs as the
// operator, and asking for sudo there is not the CLI's to do.
func TestElevatedNeverUsesSudoOnYourOwnComputer(t *testing.T) {
	local := &localClient{}
	if Elevated(local) != Client(local) {
		t.Fatal("a self machine's client was wrapped")
	}
}

func TestLocalClientLeavesAnOrdinaryCommandInDevmachinesProcessGroup(t *testing.T) {
	out, err := (&localClient{}).Run(context.Background(), "ps -o pgid= -p $$")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(out); got != strconv.Itoa(syscall.Getpgrp()) {
		t.Fatalf("an ordinary command ran in process group %s, want %d", got, syscall.Getpgrp())
	}
}

func TestLocalClientGivesARunItsOwnProcessGroup(t *testing.T) {
	out, err := (&localClient{}).Run(OwnProcessGroup(context.Background()), "ps -o pgid= -p $$; echo $$")
	if err != nil {
		t.Fatal(err)
	}
	fields := strings.Fields(out)
	if len(fields) != 2 || fields[0] != fields[1] {
		t.Fatalf("got %q, want the shell to lead its own process group", out)
	}
}

func TestLocalClientStopsEveryProcessOfARunWhenItsContextEnds(t *testing.T) {
	assertRunStopsItsChild(t, "sleep 60")
}

func TestLocalClientKillsAChildThatIgnoresSIGTERMAfterTheGrace(t *testing.T) {
	orig := groupGrace
	groupGrace = 100 * time.Millisecond
	t.Cleanup(func() { groupGrace = orig })
	assertRunStopsItsChild(t, "sh -c \"trap '' TERM; exec sleep 60\"")
}

func assertRunStopsItsChild(t *testing.T, child string) {
	t.Helper()
	pidFile := filepath.Join(t.TempDir(), "pid")
	ctx, cancel := context.WithCancel(OwnProcessGroup(context.Background()))
	done := make(chan error, 1)
	go func() {
		done <- (&localClient{}).Stream(ctx, child+" & echo $! > "+pidFile+"; wait", io.Discard, io.Discard)
	}()

	var pid int
	for deadline := time.Now().Add(5 * time.Second); pid == 0; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the command never started")
		}
		body, _ := os.ReadFile(pidFile)
		pid, _ = strconv.Atoi(strings.TrimSpace(string(body)))
	}
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("Stream kept going after its context ended")
	}

	for deadline := time.Now().Add(2 * time.Second); syscall.Kill(pid, 0) == nil; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
			t.Fatalf("process %d the command started outlived it", pid)
		}
	}
}
