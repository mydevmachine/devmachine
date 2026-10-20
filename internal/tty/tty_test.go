package tty

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/creack/pty"
)

type passThrough struct {
	under io.Writer
	mu    sync.Mutex
	seen  bytes.Buffer
}

func (p *passThrough) Write(b []byte) (int, error) {
	p.mu.Lock()
	p.seen.Write(b)
	p.mu.Unlock()
	return p.under.Write(b)
}

func (p *passThrough) Underlying() io.Writer { return p.under }

func (p *passThrough) String() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.seen.String()
}

func openTerminal(t *testing.T, cols uint16) (*os.File, *os.File) {
	t.Helper()
	master, slave, err := pty.Open()
	if err != nil {
		t.Fatalf("opening a pseudo-terminal: %v", err)
	}
	t.Cleanup(func() { master.Close(); slave.Close() })
	if err := pty.Setsize(master, &pty.Winsize{Rows: 24, Cols: cols}); err != nil {
		t.Fatalf("sizing the pseudo-terminal: %v", err)
	}
	go func() { _, _ = io.Copy(io.Discard, master) }()
	return master, slave
}

const probe = `[ -t 1 ] && echo out=tty || echo out=pipe; [ -t 2 ] && echo err=tty >&2 || echo err=pipe >&2`

func TestOfFindsATerminalThroughAPassThroughWriter(t *testing.T) {
	_, terminal := openTerminal(t, 80)
	if _, ok := Of(terminal); !ok {
		t.Fatal("a terminal is not found as one")
	}
	if f, ok := Of(&passThrough{under: &passThrough{under: terminal}}); !ok || f != terminal {
		t.Fatalf("Of through two writers = %v, %v; want the terminal", f, ok)
	}
}

func TestOfRefusesWhatIsNotATerminal(t *testing.T) {
	file, err := os.Create(filepath.Join(t.TempDir(), "out"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	for name, w := range map[string]io.Writer{
		"file":                  file,
		"buffer":                &bytes.Buffer{},
		"pass-through to file":  &passThrough{under: file},
		"pass-through to nil":   &passThrough{},
		"pass-through to bytes": &passThrough{under: &bytes.Buffer{}},
	} {
		if _, ok := Of(w); ok {
			t.Errorf("%s: found a terminal", name)
		}
	}
}

func TestRunGivesAChildATerminalWhenTheWriterReachesOne(t *testing.T) {
	_, terminal := openTerminal(t, 80)
	out := &passThrough{under: terminal}
	errOut := &passThrough{under: terminal}

	cmd := exec.Command("/bin/sh", "-c", probe)
	cmd.Stdout, cmd.Stderr = out, errOut
	if err := Run(cmd); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := out.String(); !strings.Contains(got, "out=tty") {
		t.Errorf("stdout saw %q, want out=tty", got)
	}
	if got := errOut.String(); !strings.Contains(got, "err=tty") {
		t.Errorf("stderr saw %q, want err=tty", got)
	}
}

func TestRunGivesTheChildTheTerminalsWidth(t *testing.T) {
	_, terminal := openTerminal(t, 132)
	out := &passThrough{under: terminal}

	cmd := exec.Command("/bin/sh", "-c", "stty size <&2")
	cmd.Stdout, cmd.Stderr = out, out
	if err := Run(cmd); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := strings.TrimSpace(out.String()); got != "24 132" {
		t.Fatalf("stty size = %q, want 24 132", got)
	}
}

func TestRunKeepsEveryByteOfALongOutput(t *testing.T) {
	_, terminal := openTerminal(t, 80)
	out := &passThrough{under: terminal}

	cmd := exec.Command("/bin/sh", "-c", `i=0; while [ $i -lt 2000 ]; do echo "line $i"; i=$((i+1)); done`)
	cmd.Stdout = out
	if err := Run(cmd); err != nil {
		t.Fatalf("Run: %v", err)
	}
	got := out.String()
	if !strings.HasSuffix(got, "line 1999\n") || strings.Count(got, "\n") != 2000 {
		t.Fatalf("got %d lines ending %q", strings.Count(got, "\n"), got[max(0, len(got)-20):])
	}
}

func TestRunLeavesAChildOnAPipeWhenTheWriterReachesNoTerminal(t *testing.T) {
	file, err := os.Create(filepath.Join(t.TempDir(), "out"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	out := &passThrough{under: file}

	cmd := exec.Command("/bin/sh", "-c", probe)
	cmd.Stdout, cmd.Stderr = out, out
	if err := Run(cmd); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := out.String(); !strings.Contains(got, "out=pipe") || !strings.Contains(got, "err=pipe") {
		t.Fatalf("child saw %q, want pipes", got)
	}
	if strings.Contains(out.String(), "\r") {
		t.Fatalf("output on a pipe gained a carriage return: %q", out.String())
	}
}

func TestRunReturnsTheChildsFailure(t *testing.T) {
	_, terminal := openTerminal(t, 80)
	cmd := exec.Command("/bin/sh", "-c", "echo bye; exit 3")
	cmd.Stdout = &passThrough{under: terminal}
	err := Run(cmd)
	var exit *exec.ExitError
	if !asExit(err, &exit) || exit.ExitCode() != 3 {
		t.Fatalf("Run = %v, want exit status 3", err)
	}
}

func TestRunReportsAProgramThatCannotStart(t *testing.T) {
	_, terminal := openTerminal(t, 80)
	cmd := exec.Command(filepath.Join(t.TempDir(), "missing"))
	cmd.Stdout = &passThrough{under: terminal}
	if err := Run(cmd); err == nil {
		t.Fatal("Run of a missing program succeeded")
	}
}

func asExit(err error, target **exec.ExitError) bool {
	return errors.As(err, target)
}
