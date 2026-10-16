package chat

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/mydevmachine/devmachine/internal/remote"
)

// WarmScript is Script for a harness that stays running: it reads its
// messages from the rest of stdin, after the script, as JSON lines.
//
// The ready line and the exec share the last line. A shell may read its
// script ahead in blocks, and a message written after the ready line must
// reach the harness, not a shell buffer that exec throws away.
func WarmScript(r Request) string {
	var b strings.Builder
	b.WriteString(pathPrelude(r.PathPrefix))
	b.WriteString("dm_folder=" + folderWord(r.Folder) + "\n")
	b.WriteString(`mkdir -p "$dm_folder" && cd "$dm_folder" || exit 3` + "\n")
	name := quote(r.Harness.Name)
	b.WriteString("command -v " + name + " >/dev/null 2>&1 || { echo " + missingSentinel + " >&2; exit 127; }\n")
	args := r.Harness.Args(Options{Resume: r.Resume, MCP: r.MCP, Stream: true})
	if r.Ask {
		args = append(append([]string{}, r.Harness.AskArgs...), "--input-format", "stream-json")
	}
	b.WriteString(startedEcho + "; exec " + name)
	for _, arg := range args {
		b.WriteString(" " + quote(arg))
	}
	b.WriteString("\n")
	return b.String()
}

// Warm is a harness that stays running between messages, so a message
// does not pay for its start-up. Only claude can: Harness.Streams.
type Warm struct {
	harness Harness
	stdin   *os.File
	lines   chan []byte
	exit    chan error
	cancel  context.CancelFunc
	stderr  *tailBuffer
	noise   lines
	proc    *remoteProcess

	mu      sync.Mutex
	dead    bool
	exitErr error
}

// StartWarm starts the harness and returns once it is about to run, before
// any message: the start-up happens while the person types. on hears the
// starting stage.
func StartWarm(ctx context.Context, client remote.Client, r Request, on func(Event)) (*Warm, *Failure) {
	in, stdin, err := os.Pipe()
	if err != nil {
		return nil, fail(KindHarnessFailed, "opening the harness's input: %v", err)
	}
	if _, err := io.WriteString(stdin, WarmScript(r)); err != nil {
		_ = in.Close()
		_ = stdin.Close()
		return nil, fail(KindHarnessFailed, "writing the harness's script: %v", err)
	}
	ctx, cancel := context.WithCancel(ctx)
	out, stdout := io.Pipe()
	w := &Warm{
		harness: r.Harness, stdin: stdin, lines: make(chan []byte, 64), exit: make(chan error, 1),
		cancel: cancel, stderr: &tailBuffer{max: 16 * 1024}, proc: &remoteProcess{client: client},
	}
	go func() {
		err := remote.StreamInput(remote.OwnProcessGroup(ctx), client, remote.ShellOnInput, in, stdout, w.stderr)
		_ = in.Close()
		_ = stdout.Close()
		w.exit <- err
	}()
	go func() {
		reader := bufio.NewReader(out)
		for {
			line, err := reader.ReadBytes('\n')
			if line = bytes.TrimSpace(line); len(line) > 0 {
				w.lines <- line
			}
			if err != nil {
				close(w.lines)
				return
			}
		}
	}()
	for line := range w.lines {
		if pid, ok := startedPID(line); ok {
			w.proc.pid.Store(int64(pid))
			on(startingStatus(r.Harness))
			return w, nil
		}
		w.noise.keep(line)
	}
	err = w.ended()
	return nil, classify(context.Background(), r.Harness.Name, err, false, nil, w.noise.noise(), w.stderr.String())
}

// Turn sends one message and reports its events until the harness ends
// its answer. Closing stop ends the turn and the process: a harness in the
// middle of an answer cannot be told to drop it.
func (w *Warm) Turn(message string, stop <-chan struct{}, on func(Event)) Outcome {
	p := w.harness.parser()
	var out Outcome
	var text strings.Builder
	finish := func(f *Failure) Outcome {
		out.SessionID, _, _ = p.result()
		out.Text, out.Failure = text.String(), f
		return out
	}
	line, _ := json.Marshal(userLine{Type: "user", Message: userMessage{Role: "user", Content: message}})
	if _, err := w.stdin.Write(append(line, '\n')); err != nil {
		w.Close()
		return finish(classify(context.Background(), w.harness.Name, w.ended(), false, nil, p.noise(), w.stderr.String()))
	}
	for {
		select {
		case <-stop:
			w.Close()
			return finish(fail(KindCancelled, "stopped before the answer finished"))
		case raw, ok := <-w.lines:
			if !ok {
				err := w.ended()
				_, _, failure := p.result()
				f := classify(context.Background(), w.harness.Name, err, false, failure, p.noise(), w.stderr.String())
				if f == nil {
					f = fail(KindHarnessFailed, "%s stopped without finishing its answer", w.harness.Name)
				}
				return finish(f)
			}
			for _, ev := range p.feed(raw) {
				switch ev.Type {
				case EventText:
					text.WriteString(ev.Delta)
				case EventTool:
					out.Tools = append(out.Tools, Tool{Name: ev.Name, Summary: ev.Summary})
				case EventDenied:
					out.Denied = append(out.Denied, Tool{Name: ev.Name, Summary: ev.Summary})
				}
				on(ev)
			}
			if _, ended, failure := p.result(); ended {
				return finish(failure)
			}
		}
	}
}

type userLine struct {
	Type    string      `json:"type"`
	Message userMessage `json:"message"`
}

type userMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ended waits for the process to be gone and returns how it ended.
func (w *Warm) ended() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.dead {
		w.exitErr = <-w.exit
		w.dead = true
	}
	return w.exitErr
}

// Dead says whether the process is gone, so a new one must start.
func (w *Warm) Dead() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.dead
}

func (w *Warm) kill() {
	w.proc.stop()
	w.cancel()
	_ = w.stdin.Close()
}

// Close ends the process.
func (w *Warm) Close() {
	w.kill()
	for line := range w.lines {
		w.noise.keep(line)
	}
	_ = w.ended()
}
