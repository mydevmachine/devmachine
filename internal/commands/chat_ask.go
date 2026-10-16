package commands

import (
	"context"
	"errors"
	"fmt"

	"github.com/mydevmachine/devmachine/internal/chat"
	"github.com/mydevmachine/devmachine/internal/config"
	"github.com/mydevmachine/devmachine/internal/remote"
	"github.com/spf13/cobra"
)

func newChatAskCmd(opts *options) *cobra.Command {
	var (
		flags           chatTargetFlags
		chatID, session string
		serve           bool
	)
	c := &cobra.Command{
		Use:   "ask (--workspace W | --local) --session S [--chat ID] [--serve] [-- QUESTION]",
		Short: "Ask about a coding session without writing to it",
		Long: "Every question reads the session's transcript as it is now and sends its latest turns, the " +
			"lines that match the question and this ask's earlier questions to the session's harness, run " +
			"with no tools and nothing kept. The session is never written to. The output is the same NDJSON " +
			"as `chat send`.\n\n" +
			"--serve keeps the harness running and reads the questions on stdin, with the protocol of " +
			"`chat session`.",
		RunE: func(cmd *cobra.Command, args []string) error {
			var question string
			var err error
			if serve {
				if len(args) > 0 {
					return errors.New("with --serve the questions come on stdin, not after `--`")
				}
			} else if question, err = messageAfterDash(cmd, args, `devmachine chat ask --workspace alice --session checkout -- "why did it change ChargeService?"`); err != nil {
				return err
			}
			store, dir, err := openChats(opts)
			if err != nil {
				return err
			}
			cfg, _, err := loadConfigIfAny(opts)
			if err != nil {
				return err
			}
			rec, err := resolveAsk(store, cfg, flags, chatID, session)
			if err != nil {
				return err
			}
			ctx, stop := stopSignals(cmd.Context())
			defer stop()
			emit := chat.NewEmitter(cmd.OutOrStdout())
			if serve {
				return serveAsk(ctx, cmd, store, cfg, dir, emit, rec, flags)
			}

			s, ok := beginTurn(cmd, store, emit, &rec, question)
			if !ok {
				return errors.New("could not store the question")
			}
			messages, err := store.Messages(rec.ID)
			if err != nil {
				return err
			}
			connecting(rec.Workspace, s.relay)
			client, prefix, failure := chatClient(ctx, cfg, dir, rec.Workspace)
			if failure != nil {
				return s.finish(chat.Outcome{Failure: failure})
			}
			defer client.Close()
			run := func(req chat.Request) chat.Outcome { return chat.Run(ctx, client, req, s.relay) }
			return askSession(ctx, s, client, askTarget(rec, flags, prefix), cfg.ChatFolder(), question, earlierThan(messages), run)
		},
	}
	addChatTargetFlags(c, &flags)
	c.Flags().StringVar(&session, "session", "", "the session to ask about")
	c.Flags().StringVar(&flags.folder, "folder", "",
		"the session's folder; needed for a session on this computer that is not in tmux")
	c.Flags().StringVar(&chatID, "chat", "", "continue this ask")
	c.Flags().BoolVar(&serve, "serve", false, "keep the harness running and read the questions on stdin")
	return c
}

// earlierThan is the side conversation before the question just stored.
func earlierThan(messages []chat.Message) string {
	if n := len(messages); n > 0 && messages[n-1].Role == chat.RoleUser {
		messages = messages[:n-1]
	}
	return chat.Earlier(messages)
}

// resolveAsk is the stored ask for --chat, or a new one not stored yet.
func resolveAsk(store *chat.Store, cfg config.Config, flags chatTargetFlags, chatID, session string) (chat.Record, error) {
	if chatID == "" {
		return newAskRecord(cfg, flags, session)
	}
	rec, err := store.Get(chatID)
	if err != nil {
		return rec, err
	}
	if err := sameTarget(rec, chatTargetFlags{workspace: flags.workspace, local: flags.local}); err != nil {
		return rec, err
	}
	if session != "" && session != rec.Session {
		return rec, fmt.Errorf("chat %s asks about session %q: drop --session, or start a new ask", rec.ID, rec.Session)
	}
	return rec, nil
}

func newAskRecord(cfg config.Config, flags chatTargetFlags, session string) (chat.Record, error) {
	if session == "" {
		return chat.Record{}, errors.New("say which session to ask about: --session <name>")
	}
	if !flags.local && flags.workspace == "" {
		return chat.Record{}, errors.New("say where the session runs: --workspace <name> or --local")
	}
	rec := chat.Record{
		Harness: flags.harness, Workspace: chat.LocalWorkspace,
		Kind: chat.KindAsk, Session: session, SessionFolder: flags.folder, Mode: chat.ModeTranscript,
	}
	if flags.workspace != "" {
		rec.Workspace = flags.workspace
		if _, _, err := cfg.MachineFor(flags.workspace); err != nil {
			return rec, err
		}
	}
	return rec, nil
}

func askTarget(rec chat.Record, flags chatTargetFlags, prefix []string) chat.Target {
	target := chat.Target{Session: rec.Session, Folder: flags.folder, Harness: flags.harness, PathPrefix: prefix}
	if target.Folder == "" && rec.Workspace == chat.LocalWorkspace {
		target.Folder = rec.SessionFolder
	}
	if target.Harness == "" {
		target.Harness = rec.Harness
	}
	return target
}

// serveAsk keeps one lean harness running for a session's asks: each
// question still reads the session again and carries the side conversation.
func serveAsk(ctx context.Context, cmd *cobra.Command, store *chat.Store, cfg config.Config, dir string, emit *chat.Emitter, rec chat.Record, flags chatTargetFlags) error {
	show := func(ev chat.Event) { _ = emit.Emit(ev) }
	connecting(rec.Workspace, show)
	client, prefix, failure := chatClient(ctx, cfg, dir, rec.Workspace)
	if failure != nil {
		return failSession(emit, failure)
	}
	defer client.Close()
	target := askTarget(rec, flags, prefix)
	if target.Harness == "" {
		show(readingTranscript)
		loc, failure := chat.Locate(ctx, client, target)
		if failure != nil {
			return failSession(emit, failure)
		}
		target.Harness = loc.Harness
	}
	harness, err := chat.Lookup(target.Harness)
	if err != nil {
		return failSession(emit, &chat.Failure{Kind: chat.KindHarnessUnsupported, Message: fmt.Sprintf("Ask is not available for %s yet", target.Harness)})
	}
	t := &turner{ctx: ctx, client: client, req: chat.Request{Harness: harness, Folder: cfg.ChatFolder(), Ask: true, PathPrefix: prefix}}
	if failure := t.start(show); failure != nil {
		return failSession(emit, failure)
	}
	defer t.close()
	if err := emit.Ready(storedOrNil(rec)); err != nil {
		return err
	}
	answer := func(question string, stop <-chan struct{}) {
		s, ok := beginTurn(cmd, store, emit, &rec, question)
		if !ok {
			return
		}
		messages, _ := store.Messages(rec.ID)
		run := func(req chat.Request) chat.Outcome {
			if req.Harness.Name != harness.Name {
				return chat.Run(ctx, client, req, s.relay)
			}
			return t.turn(req, stop, s.relay)
		}
		if err := askSession(ctx, s, client, target, cfg.ChatFolder(), question, earlierThan(messages), run); err != nil {
			var coded *exitError
			if !errors.As(err, &coded) {
				fmt.Fprintf(cmd.ErrOrStderr(), "error: %v\n", err)
			}
		}
		if latest, err := store.Get(rec.ID); err == nil {
			rec = latest
		}
	}
	return closeSession(emit, serveLoop(ctx, readCommands(cmd.InOrStdin(), cmd.ErrOrStderr()), answer))
}

var readingTranscript = chat.Status(chat.StageReadingTranscript, "Reading the session…")

// askSession answers from the session's transcript as it is now: it finds
// the session again, reads its transcript, and sends the latest turns, the
// lines that match the question and the side conversation so far to a harness
// with no tools and nothing kept. A fork was tried first; it re-sent the
// whole session on every question and stopped seeing what the session did
// after it was made.
func askSession(ctx context.Context, s *chatSession, client remote.Client, target chat.Target, runFolder, question, earlier string, run func(chat.Request) chat.Outcome) error {
	target.Question = question
	s.relay(readingTranscript)
	loc, failure := chat.Locate(ctx, client, target)
	if failure != nil {
		return s.finishWith(chat.Outcome{Failure: failure}, func(r *chat.Record) {
			if loc.Harness != "" {
				r.Harness = loc.Harness
			}
		})
	}
	built := chat.BuildContext(loc.Body, loc.Harness, loc.Folder, earlier, question)
	harness, _ := chat.Lookup(loc.Harness)
	out := run(chat.Request{Harness: harness, Folder: runFolder, Message: built.Prompt, Ask: true, PathPrefix: target.PathPrefix})
	out.SessionID = ""
	return s.finishWith(out, func(r *chat.Record) {
		r.Harness, r.Folder, r.Mode, r.Excerpts = loc.Harness, runFolder, chat.ModeTranscript, built.Count
		r.SourceSessionID, r.SessionFolder, r.HarnessSessionID = loc.SessionID, loc.Folder, ""
	})
}
