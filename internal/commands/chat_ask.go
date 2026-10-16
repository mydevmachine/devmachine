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
		Long: "On a claude session it asks a fork of the session, so the answer has the session's whole " +
			"context and the session itself is not changed. On a codex session it sends an excerpt of the " +
			"session's transcript to a new read-only codex. The output is the same NDJSON as `chat send`.",
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
			s := chatSession{store: store, emit: chat.NewEmitter(cmd.OutOrStdout()), rec: rec}
			if err := s.start(question); err != nil {
				return err
			}
			client, prefix, failure := chatClient(ctx, cfg, dir, rec.Workspace)
			if failure != nil {
				return s.finish(chat.Outcome{Failure: failure})
			}
			defer client.Close()
			if rec.HarnessSessionID != "" {
				harness, err := chat.Lookup(rec.Harness)
				if err != nil {
					return s.finish(chat.Outcome{Failure: &chat.Failure{Kind: chat.KindHarnessUnsupported, Message: err.Error()}})
				}
				return s.finish(chat.Run(ctx, client, chat.Request{
					Harness: harness, Folder: rec.Folder, Message: question,
					Resume: rec.HarnessSessionID, PathPrefix: prefix,
				}, s.relay))
			}
			folder := flags.folder
			if folder == "" && rec.Workspace == chat.LocalWorkspace && rec.Folder != "" {
				folder = rec.Folder
			}
			return firstAsk(ctx, &s, client, chat.Target{
				Session: rec.Session, Folder: folder, Harness: flags.harness, PathPrefix: prefix,
			}, question)
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
		Folder: flags.folder, Kind: chat.KindAsk, Session: session,
	}
	if flags.workspace != "" {
		rec.Workspace = flags.workspace
		if _, _, err := cfg.MachineFor(flags.workspace); err != nil {
			return rec, err
		}
	}
	return rec, nil
}

func firstAsk(ctx context.Context, s *chatSession, client remote.Client, target chat.Target, question string) error {
	loc, failure := chat.Locate(ctx, client, target)
	if failure != nil {
		return s.finishWith(chat.Outcome{Failure: failure}, func(r *chat.Record) {
			if loc.Harness != "" {
				r.Harness = loc.Harness
			}
		})
	}
	req := chat.Request{Message: question, PathPrefix: target.PathPrefix, Resume: loc.SessionID, Fork: true, Folder: loc.RunFolder}
	mode, excerpts := chat.ModeFork, 0
	if loc.Harness == chat.Codex.Name {
		body, failure := chat.ReadTranscript(ctx, client, loc.Transcript, chat.TranscriptLimit)
		if failure != nil {
			return s.finish(chat.Outcome{Failure: failure})
		}
		excerpt := chat.BuildExcerpt(body, loc.Folder, question)
		req = chat.Request{Message: excerpt.Prompt, PathPrefix: target.PathPrefix, Folder: loc.Folder}
		mode, excerpts = chat.ModeTranscript, excerpt.Count
	}
	req.Harness, _ = chat.Lookup(loc.Harness)
	update := func(r *chat.Record) {
		r.Harness, r.Folder, r.Mode, r.SourceSessionID, r.Excerpts = loc.Harness, req.Folder, mode, loc.SessionID, excerpts
	}
	if err := s.store.Update(s.rec.ID, update); err != nil {
		return err
	}
	return s.finishWith(chat.Run(ctx, client, req, s.relay), update)
}
