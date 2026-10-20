package remote

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/creack/pty"
	"github.com/mydevmachine/devmachine/internal/logfile"
)

func TestLocalClientGivesALoggedCommandTheTerminal(t *testing.T) {
	master, terminal, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer master.Close()
	defer terminal.Close()
	go func() { _, _ = io.Copy(io.Discard, master) }()

	dir := t.TempDir()
	log := logfile.Open(dir)
	out := log.Tee(terminal, "out")
	err = (&localClient{}).Stream(context.Background(),
		`[ -t 1 ] && echo out=tty || echo out=pipe; [ -t 2 ] && echo err=tty >&2 || echo err=pipe >&2`, out, out)
	log.Close()
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(logfile.Path(dir))
	if err != nil {
		t.Fatal(err)
	}
	if got := string(body); !strings.Contains(got, "out=tty") || !strings.Contains(got, "err=tty") {
		t.Fatalf("the command did not see a terminal:\n%s", got)
	}
}
