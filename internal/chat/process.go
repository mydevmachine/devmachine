package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/mydevmachine/devmachine/internal/remote"
)

// The stages a status event names, in the order they can happen.
const (
	StageConnecting        = "connecting"
	StageReadingTranscript = "reading-transcript"
	StageStarting          = "starting"
	StageWaiting           = "waiting"
)

// Status is a status event: what the wait before an answer is spent on.
func Status(stage, message string) Event {
	return Event{Type: EventStatus, Stage: stage, Message: message}
}

// HarnessTitle is how a harness is named to a person: "Claude", "Codex".
func HarnessTitle(name string) string {
	if name == "" {
		return name
	}
	return strings.ToUpper(name[:1]) + name[1:]
}

func startingStatus(h Harness) Event {
	return Status(StageStarting, "Starting "+HarnessTitle(h.Name)+"…")
}

func waitingStatus(name string) Event {
	return Status(StageWaiting, HarnessTitle(name)+" is thinking…")
}

// startedEcho is the script's line that prints the shell's pid right before
// exec turns that shell into the harness, so the pid is the harness's own.
const startedEcho = `echo '{"type":"devmachine-started","pid":'$$'}'`

var startedPrefix = []byte(`{"type":"devmachine-started"`)

func startedPID(line []byte) (int, bool) {
	if !bytes.HasPrefix(line, startedPrefix) {
		return 0, false
	}
	var v struct {
		PID int `json:"pid"`
	}
	if json.Unmarshal(line, &v) != nil {
		return 0, true
	}
	return v.PID, true
}

// remoteProcess stops a harness on another machine. Killing the local ssh
// is not enough there: ssh's shared connection holds the session's input
// and output for the master, which keeps the command running, and its
// output open, until the command ends on its own.
type remoteProcess struct {
	client remote.Client
	pid    atomic.Int64
}

func (p *remoteProcess) stop() {
	pid := p.pid.Load()
	if pid <= 0 || remote.IsLocal(p.client) {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	n := strconv.FormatInt(pid, 10)
	_, _ = p.client.Run(ctx, "pkill -TERM -P "+n+" 2>/dev/null; kill -TERM "+n+" 2>/dev/null; true")
}
