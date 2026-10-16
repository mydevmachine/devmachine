package commands

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/mydevmachine/devmachine/internal/chat"
	"github.com/mydevmachine/devmachine/internal/remote"
	"github.com/spf13/cobra"
)

// chatIdle is how long a session waits for a message before it closes.
var chatIdle = 10 * time.Minute

func newChatSessionCmd(opts *options) *cobra.Command {
	var flags chatFlags
	c := &cobra.Command{
		Use:   "session (--chat ID | [--workspace W | --local] [--harness claude|codex] [--folder DIR]) [--mcp]",
		Short: "Keep a chat's harness running and answer messages read from stdin",
		Long: "Starts the harness before any message, so a message pays only for its answer. Reads " +
			`{"type":"send","text":"…"} and {"type":"cancel"} on stdin, one per line, and prints ready, ` +
			"then the events of `chat send` for each message, then closed. It closes after 10 minutes " +
			"with no message, or when stdin ends.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			store, dir, err := openChats(opts)
			if err != nil {
				return err
			}
			cfg, _, err := loadConfigIfAny(opts)
			if err != nil {
				return err
			}
			rec, err := resolveChat(cmd, cfg, store, flags)
			if err != nil {
				return err
			}
			harness, err := chat.Lookup(rec.Harness)
			if err != nil {
				return err
			}
			ctx, stop := stopSignals(cmd.Context())
			defer stop()
			emit := chat.NewEmitter(cmd.OutOrStdout())
			show := func(ev chat.Event) { _ = emit.Emit(ev) }
			connecting(rec.Workspace, show)
			client, prefix, failure := chatClient(ctx, cfg, dir, rec.Workspace)
			if failure != nil {
				return failSession(emit, failure)
			}
			defer client.Close()
			t := &turner{ctx: ctx, client: client, resume: true, req: chat.Request{
				Harness: harness, Folder: rec.Folder, Resume: rec.HarnessSessionID, MCP: rec.MCP, PathPrefix: prefix,
			}}
			if failure := t.start(show); failure != nil {
				return failSession(emit, failure)
			}
			defer t.close()
			if err := emit.Ready(storedOrNil(rec)); err != nil {
				return err
			}
			answer := func(text string, stop <-chan struct{}) {
				s, ok := beginTurn(cmd, store, emit, &rec, text)
				if !ok {
					return
				}
				out := t.turn(chat.Request{
					Harness: harness, Folder: rec.Folder, Message: text, MCP: rec.MCP, PathPrefix: prefix,
				}, stop, s.relay)
				endTurn(cmd, store, s, &rec, out, nil)
			}
			return closeSession(emit, serveLoop(ctx, readCommands(cmd.InOrStdin(), cmd.ErrOrStderr()), answer))
		},
	}
	addChatTargetFlags(c, &flags.chatTargetFlags)
	c.Flags().StringVar(&flags.folder, "folder", "", "the folder the harness runs in on the target (default: chat.folder, ~/chats)")
	c.Flags().StringVar(&flags.chatID, "chat", "", "continue this conversation")
	addMCPFlag(c, &flags.mcp)
	return c
}

// connecting says the wait is for the connection, on a workspace only.
func connecting(workspace string, on func(chat.Event)) {
	if workspace != chat.LocalWorkspace {
		on(chat.Status(chat.StageConnecting, "Connecting to "+workspace+"…"))
	}
}

func storedOrNil(rec chat.Record) *chat.Record {
	if rec.ID == "" {
		return nil
	}
	return &rec
}

// beginTurn stores the message, creating the conversation on its first
// one, and prints started.
func beginTurn(cmd *cobra.Command, store *chat.Store, emit *chat.Emitter, rec *chat.Record, text string) (*chatSession, bool) {
	var err error
	if rec.ID == "" {
		rec.Title = chat.Title(text)
		*rec, err = store.Create(*rec)
	} else {
		*rec, err = store.Get(rec.ID)
	}
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "error: %v\n", err)
		return nil, false
	}
	s := &chatSession{store: store, emit: emit, rec: *rec}
	if err := s.start(text); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "error: %v\n", err)
		return nil, false
	}
	return s, true
}

// endTurn stores the answer and prints done or error. A failed answer ends
// the turn, not the session.
func endTurn(cmd *cobra.Command, store *chat.Store, s *chatSession, rec *chat.Record, out chat.Outcome, change func(*chat.Record)) {
	var coded *exitError
	if err := s.finishWith(out, change); err != nil && !errors.As(err, &coded) {
		fmt.Fprintf(cmd.ErrOrStderr(), "error: %v\n", err)
	}
	if latest, err := store.Get(rec.ID); err == nil {
		*rec = latest
	}
}

func failSession(emit *chat.Emitter, failure *chat.Failure) error {
	_ = emit.Emit(chat.Event{Type: chat.EventError, Kind: failure.Kind, Message: failure.Message})
	_ = emit.Emit(chat.Event{Type: chat.EventClosed, Reason: chat.ClosedError})
	return &exitError{code: 1, err: failure, quiet: true}
}

func closeSession(emit *chat.Emitter, reason string) error {
	return emit.Emit(chat.Event{Type: chat.EventClosed, Reason: reason})
}

type inbound struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// readCommands reads the session's stdin, one JSON command per line, and
// ends with an eof command.
func readCommands(r io.Reader, errOut io.Writer) <-chan inbound {
	ch := make(chan inbound)
	go func() {
		defer close(ch)
		scanner := bufio.NewScanner(r)
		scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
		for scanner.Scan() {
			var in inbound
			if err := json.Unmarshal(scanner.Bytes(), &in); err != nil || (in.Type != "send" && in.Type != "cancel") {
				fmt.Fprintf(errOut, "ignoring %q: a line is {\"type\":\"send\",\"text\":\"…\"} or {\"type\":\"cancel\"}\n", scanner.Text())
				continue
			}
			ch <- in
		}
		ch <- inbound{Type: "eof"}
	}()
	return ch
}

// serveLoop answers each send in turn, one at a time, until stdin ends, the
// context ends or no message came for chatIdle. A cancel stops the turn
// that runs; a send that comes during a turn waits for it.
func serveLoop(ctx context.Context, in <-chan inbound, answer func(text string, stop <-chan struct{})) string {
	timer := time.NewTimer(chatIdle)
	defer timer.Stop()
	var queue []string
	eof := false
	for {
		var text string
		switch {
		case len(queue) > 0:
			text, queue = queue[0], queue[1:]
		case eof:
			return chat.ClosedEOF
		default:
			select {
			case <-ctx.Done():
				return chat.ClosedEOF
			case <-timer.C:
				return chat.ClosedIdle
			case c := <-in:
				switch c.Type {
				case "eof":
					return chat.ClosedEOF
				case "cancel":
					continue
				}
				text = c.Text
			}
		}
		stop := make(chan struct{})
		var once sync.Once
		halt := func() { once.Do(func() { close(stop) }) }
		done := make(chan struct{})
		go func() {
			answer(text, stop)
			close(done)
		}()
	turn:
		for {
			select {
			case <-done:
				break turn
			case <-ctx.Done():
				halt()
				<-done
				return chat.ClosedEOF
			case c := <-in:
				switch c.Type {
				case "cancel":
					halt()
				case "send":
					queue = append(queue, c.Text)
				case "eof":
					eof, in = true, nil
				}
			}
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(chatIdle)
	}
}

// turner answers one message at a time: claude through a process kept
// running between messages, codex through one process per message.
type turner struct {
	ctx    context.Context
	client remote.Client
	req    chat.Request
	// resume carries the harness's session id into the next process; an
	// ask keeps none.
	resume bool
	warm   *chat.Warm
}

// start launches the harness; on hears its starting stage. A restart
// between turns passes a no-op, so no status shows while nobody waits.
func (t *turner) start(on func(chat.Event)) *chat.Failure {
	if !t.req.Harness.Streams {
		return nil
	}
	w, failure := chat.StartWarm(t.ctx, t.client, t.req, on)
	t.warm = w
	return failure
}

func (t *turner) turn(req chat.Request, stop <-chan struct{}, on func(chat.Event)) chat.Outcome {
	if !t.req.Harness.Streams {
		ctx, cancel := context.WithCancel(t.ctx)
		defer cancel()
		go func() {
			select {
			case <-stop:
				cancel()
			case <-ctx.Done():
			}
		}()
		if t.resume {
			req.Resume = t.req.Resume
		}
		out := chat.Run(ctx, t.client, req, on)
		t.keep(out)
		return out
	}
	if t.warm == nil || t.warm.Dead() {
		if failure := t.start(on); failure != nil {
			return chat.Outcome{Failure: failure}
		}
	}
	out := t.warm.Turn(req.Message, stop, on)
	t.keep(out)
	if t.warm.Dead() {
		t.warm = nil
		_ = t.start(func(chat.Event) {})
	}
	return out
}

func (t *turner) keep(out chat.Outcome) {
	if t.resume && out.SessionID != "" {
		t.req.Resume = out.SessionID
	}
}

func (t *turner) close() {
	if t.warm != nil {
		t.warm.Close()
	}
}
