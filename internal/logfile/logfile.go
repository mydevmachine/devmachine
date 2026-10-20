// Package logfile writes the CLI's own log: what a command printed, and how
// it ended, so somebody whose install went wrong has something to send.
//
// Like history.log, it never fails the command it records. Every error inside
// it is dropped: a log that can break a run is a log somebody turns off.
package logfile

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/mydevmachine/devmachine/internal/redact"
)

const (
	// Folder is the log's folder inside the configuration directory.
	Folder = "logs"
	// FileName is the current log. The macOS app opens this exact path.
	FileName = "devmachine.log"
	// MaxSize is how large the current log grows before it is rotated.
	MaxSize = 5 << 20
	// Kept is how many rotated logs survive, as FileName.1 to FileName.3.
	Kept = 3
)

// Path is where the current log lives for a configuration directory.
func Path(configDir string) string {
	return filepath.Join(configDir, Folder, FileName)
}

// Log is one invocation's handle on the file. A nil Log writes nothing.
type Log struct {
	mu     sync.Mutex
	path   string
	file   *os.File
	opened bool
	size   int64
	limit  int64
	pid    int
	now    func() time.Time
	tees   []*tee
}

// Open opens the log for appending. It returns a Log that writes nothing when
// the file cannot be opened.
func Open(configDir string) *Log {
	l := &Log{path: Path(configDir), limit: MaxSize, pid: os.Getpid(), now: time.Now}
	folder := filepath.Dir(l.path)
	if err := os.MkdirAll(folder, 0o700); err != nil {
		return l
	}
	// The folder ignores itself, so a configuration directory that became a
	// repository before logs existed never stages them, whatever its own
	// .gitignore says.
	ignore := filepath.Join(folder, ".gitignore")
	if _, err := os.Stat(ignore); os.IsNotExist(err) {
		_ = os.WriteFile(ignore, []byte("*\n"), 0o600)
	}
	l.openFile()
	return l
}

func (l *Log) openFile() {
	file, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	l.file = file
	l.opened = true
	if info, err := file.Stat(); err == nil {
		l.size = info.Size()
	}
}

// Line writes one stamped line of the given kind, after redacting text.
func (l *Log) Line(kind, text string) {
	if l == nil {
		return
	}
	var r redact.Redactor
	l.mu.Lock()
	defer l.mu.Unlock()
	for line := range strings.SplitSeq(text, "\n") {
		if kept, ok := r.Line(line); ok {
			l.write(kind, kept)
		}
	}
}

func (l *Log) write(kind, line string) {
	if l.file == nil {
		return
	}
	if l.size >= l.limit {
		l.rotate()
		if l.file == nil {
			return
		}
	}
	entry := fmt.Sprintf("%s [%d] %-6s %s\n",
		l.now().UTC().Format("2006-01-02T15:04:05.000Z"), l.pid, kind, line)
	n, _ := l.file.WriteString(entry)
	l.size += int64(n)
}

func (l *Log) rotate() {
	_ = l.file.Close()
	l.file = nil
	_ = os.Remove(fmt.Sprintf("%s.%d", l.path, Kept))
	for i := Kept - 1; i >= 1; i-- {
		_ = os.Rename(fmt.Sprintf("%s.%d", l.path, i), fmt.Sprintf("%s.%d", l.path, i+1))
	}
	_ = os.Rename(l.path, l.path+".1")
	l.openFile()
}

// Opened says whether the file was ever opened, so a caller does not point
// somebody at a log that holds nothing of this run.
func (l *Log) Opened() bool {
	if l == nil {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.opened
}

// Tee returns a writer that passes everything to w and logs each whole line
// as the given kind. What reaches w, and what w answers, is never changed.
func (l *Log) Tee(w io.Writer, kind string) io.Writer {
	if l == nil {
		return w
	}
	t := &tee{log: l, under: w, kind: kind}
	l.mu.Lock()
	l.tees = append(l.tees, t)
	l.mu.Unlock()
	return t
}

// Close writes what a tee holds of an unfinished line, and closes the file.
func (l *Log) Close() {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, t := range l.tees {
		t.flush()
	}
	if l.file != nil {
		_ = l.file.Close()
		l.file = nil
	}
}

type tee struct {
	log     *Log
	under   io.Writer
	kind    string
	pending []byte
	red     redact.Redactor
}

func (t *tee) Write(p []byte) (int, error) {
	n, err := t.under.Write(p)
	t.log.mu.Lock()
	defer t.log.mu.Unlock()
	t.pending = append(t.pending, p...)
	for {
		i := bytes.IndexByte(t.pending, '\n')
		if i < 0 {
			break
		}
		t.emit(string(t.pending[:i]))
		t.pending = t.pending[i+1:]
	}
	return n, err
}

// Underlying is the writer the tee passes to, for a caller that asks whether
// its output reaches a terminal.
func (t *tee) Underlying() io.Writer { return t.under }

func (t *tee) flush() {
	if len(t.pending) > 0 {
		t.emit(string(t.pending))
		t.pending = nil
	}
}

func (t *tee) emit(line string) {
	line = strings.TrimRight(line, "\r")
	if i := strings.LastIndexByte(line, '\r'); i >= 0 {
		line = line[i+1:]
	}
	if kept, ok := t.red.Line(line); ok {
		t.log.write(t.kind, kept)
	}
}
