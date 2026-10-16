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
	)
	c := &cobra.Command{
		Use:   "ask (--workspace W | --local) --session S [--chat ID] -- QUESTION",
		Short: "Ask about a coding session without writing to it",
		Long: "Every question reads the session's transcript as it is now and sends its latest turns, the " +
			"lines that match the question and this ask's earlier questions to the session's harness, run " +
			"with no tools and nothing kept. The session is never written to. The output is the same NDJSON " +
			"as `chat send`.",
		RunE: func(cmd *cobra.Command, args []string) error {
			question, err := messageAfterDash(cmd, args, `devmachine chat ask --workspace alice --session checkout -- "why did it change ChargeService?"`)
			if err != nil {
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
			var rec chat.Record
			if chatID != "" {
				if rec, err = store.Get(chatID); err != nil {
					return err
				}
				if err := sameTarget(rec, chatTargetFlags{workspace: flags.workspace, local: flags.local}); err != nil {
					return err
				}
				if session != "" && session != rec.Session {
					return fmt.Errorf("chat %s asks about session %q: drop --session, or start a new ask", rec.ID, rec.Session)
				}
			} else {
				if rec, err = newAskRecord(cfg, flags, session, question); err != nil {
					return err
				}
			}
			ctx, stop := stopSignals(cmd.Context())
			defer stop()
			if chatID == "" {
				if rec, err = store.Create(rec); err != nil {
					return err
				}
			}
			messages, err := store.Messages(rec.ID)
			if err != nil {
				return err
			}
			s := chatSession{store: store, emit: chat.NewEmitter(cmd.OutOrStdout()), rec: rec}
			if err := s.start(question); err != nil {
				return err
			}
			client, prefix, failure := chatClient(ctx, cfg, dir, rec.Workspace)
			if failure != nil {
				return s.finish(chat.Outcome{Failure: failure})
			}
			defer client.Close()
			target := chat.Target{Session: rec.Session, Folder: flags.folder, Harness: flags.harness, PathPrefix: prefix}
			if target.Folder == "" && rec.Workspace == chat.LocalWorkspace {
				target.Folder = rec.SessionFolder
			}
			if target.Harness == "" {
				target.Harness = rec.Harness
			}
			return askSession(ctx, &s, client, target, cfg.ChatFolder(), question, chat.Earlier(messages))
		},
	}
	addChatTargetFlags(c, &flags)
	c.Flags().StringVar(&session, "session", "", "the session to ask about")
	c.Flags().StringVar(&flags.folder, "folder", "",
		"the session's folder; needed for a session on this computer that is not in tmux")
	c.Flags().StringVar(&chatID, "chat", "", "continue this ask")
	return c
}

func newAskRecord(cfg config.Config, flags chatTargetFlags, session, question string) (chat.Record, error) {
	if session == "" {
		return chat.Record{}, errors.New("say which session to ask about: --session <name>")
	}
	if !flags.local && flags.workspace == "" {
		return chat.Record{}, errors.New("say where the session runs: --workspace <name> or --local")
	}
	rec := chat.Record{
		Title: chat.Title(question), Harness: flags.harness, Workspace: chat.LocalWorkspace,
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

// askSession answers from the session's transcript as it is now: it finds
// the session again, reads its transcript, and sends the latest turns, the
// lines that match the question and the side conversation so far to a harness
// with no tools and nothing kept. A fork was tried first; it re-sent the
// whole session on every question and stopped seeing what the session did
// after it was made.
func askSession(ctx context.Context, s *chatSession, client remote.Client, target chat.Target, runFolder, question, earlier string) error {
	loc, failure := chat.Locate(ctx, client, target)
	if failure != nil {
		return s.finishWith(chat.Outcome{Failure: failure}, func(r *chat.Record) {
			if loc.Harness != "" {
				r.Harness = loc.Harness
			}
		})
	}
	body, failure := chat.ReadTranscript(ctx, client, loc.Transcript, chat.TranscriptLimit)
	if failure != nil {
		return s.finish(chat.Outcome{Failure: failure})
	}
	built := chat.BuildContext(body, loc.Harness, loc.Folder, earlier, question)
	harness, _ := chat.Lookup(loc.Harness)
	out := chat.Run(ctx, client, chat.Request{
		Harness: harness, Folder: runFolder, Message: built.Prompt, Ask: true, PathPrefix: target.PathPrefix,
	}, s.relay)
	out.SessionID = ""
	return s.finishWith(out, func(r *chat.Record) {
		r.Harness, r.Folder, r.Mode, r.Excerpts = loc.Harness, runFolder, chat.ModeTranscript, built.Count
		r.SourceSessionID, r.SessionFolder, r.HarnessSessionID = loc.SessionID, loc.Folder, ""
	})
}
