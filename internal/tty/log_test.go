package tty

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/mydevmachine/devmachine/internal/logfile"
)

func TestALoggedChildKeepsItsColoursOnTheTerminalAndLogsThemPlain(t *testing.T) {
	master, terminal, err := pty.Open()
	if err != nil {
		t.Fatalf("opening a pseudo-terminal: %v", err)
	}
	defer master.Close()
	defer terminal.Close()
	screen := make(chan string)
	go func() {
		var seen bytes.Buffer
		_, _ = io.Copy(&seen, master)
		screen <- seen.String()
	}()

	dir := t.TempDir()
	log := logfile.Open(dir)
	cmd := exec.Command("/bin/sh", "-c",
		`[ -t 1 ] && printf '\033[32mok: tty\033[0m\n' || echo 'ok: pipe'; printf 'GH_TOKEN=fake-value\n'`)
	cmd.Stdout = log.Tee(terminal, "out")
	if err := Run(cmd); err != nil {
		t.Fatalf("Run: %v", err)
	}
	log.Close()
	terminal.Close()

	var shown string
	select {
	case shown = <-screen:
	case <-time.After(5 * time.Second):
		t.Fatal("the terminal never closed")
	}
	if !strings.Contains(shown, "\x1b[32mok: tty\x1b[0m") {
		t.Errorf("terminal showed %q, want the coloured line", shown)
	}
	body, err := os.ReadFile(logfile.Path(dir))
	if err != nil {
		t.Fatal(err)
	}
	got := string(body)
	if !strings.Contains(got, "out    ok: tty\n") || !strings.Contains(got, "out    GH_TOKEN=***\n") {
		t.Errorf("log is %q", got)
	}
	for _, junk := range []string{"\x1b", "\r", "fake-value"} {
		if strings.Contains(got, junk) {
			t.Errorf("log holds %q: %q", junk, got)
		}
	}
}
