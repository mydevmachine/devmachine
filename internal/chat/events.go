package chat

import (
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

// The kinds of failure that end a stream.
const (
	KindUnreachable        = "unreachable"
	KindLoginRefused       = "login-refused"
	KindHostKeyRejected    = "host-key-rejected"
	KindHarnessMissing     = "harness-missing"
	KindHarnessLoggedOut   = "harness-logged-out"
	KindHarnessFailed      = "harness-failed"
	KindCancelled          = "cancelled"
	KindSessionNotFound    = "session-not-found"
	KindTranscriptNotFound = "transcript-not-found"
	KindHarnessUnsupported = "harness-unsupported"
)

// The types of a stream line.
const (
	EventStarted = "started"
	EventText    = "text"
	EventTool    = "tool"
	EventDenied  = "denied"
	EventDone    = "done"
	EventError   = "error"
)

// Failure is why a stream ended without an answer.
type Failure struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
}

func (f *Failure) Error() string { return f.Kind + ": " + f.Message }

func fail(kind, format string, args ...any) *Failure {
	return &Failure{Kind: kind, Message: fmt.Sprintf(format, args...)}
}

// Event is one line of `chat send`'s output.
type Event struct {
	Type    string  `json:"type"`
	Chat    *Record `json:"chat,omitempty"`
	Delta   string  `json:"delta,omitempty"`
	Name    string  `json:"name,omitempty"`
	Summary string  `json:"summary,omitempty"`
	Kind    string  `json:"kind,omitempty"`
	Message string  `json:"message,omitempty"`
}

// Emitter writes events as NDJSON, one object per line.
type Emitter struct {
	mu  sync.Mutex
	out io.Writer
}

// NewEmitter writes to out.
func NewEmitter(out io.Writer) *Emitter { return &Emitter{out: out} }

// Emit writes one event.
func (e *Emitter) Emit(ev Event) error {
	line, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("encoding an event: %w", err)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, err := e.out.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("writing an event: %w", err)
	}
	return nil
}
