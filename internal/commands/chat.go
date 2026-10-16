package commands

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/mydevmachine/devmachine/internal/chat"
	"github.com/mydevmachine/devmachine/internal/config"
	"github.com/mydevmachine/devmachine/internal/facts"
	"github.com/mydevmachine/devmachine/internal/remote"
	"github.com/spf13/cobra"
)

func newChatCmd(opts *options) *cobra.Command {
	c := &cobra.Command{
		Use:   "chat",
		Short: "Chat with the claude or codex you are logged in to, on a workspace or this computer",
		Long: "A chat runs the coding CLI already logged in on a workspace (or on this computer), " +
			"read only, one process per message. No API key: it uses that CLI's own login.",
	}
	c.AddCommand(newChatSendCmd(opts), newChatAskCmd(opts), newChatListCmd(opts), newChatShowCmd(opts),
		newChatRenameCmd(opts), newChatDeleteCmd(opts))
	return c
}

type chatTargetFlags struct {
	workspace, harness, folder string
	local                      bool
}

func messageAfterDash(cmd *cobra.Command, args []string, example string) (string, error) {
	at := cmd.ArgsLenAtDash()
	if at < 0 || at >= len(args) || strings.TrimSpace(strings.Join(args[at:], " ")) == "" {
		return "", fmt.Errorf("the message follows `--`: %s", example)
	}
	if at > 0 {
		return "", fmt.Errorf("unexpected %q before `--`: %s", strings.Join(args[:at], " "), example)
	}
	return strings.Join(args[at:], " "), nil
}

func openChats(opts *options) (*chat.Store, string, error) {
	dir, _, err := config.Dir(opts.configDir)
	if err != nil {
		return nil, "", err
	}
	store, err := chat.Open(dir)
	return store, dir, err
}

// chatFlags are what send and session are told about the conversation.
type chatFlags struct {
	chatTargetFlags
	chatID string
	mcp    bool
}

// resolveChat is the conversation a send or a session talks through: the
// stored record for --chat, or a new one not stored yet, which takes its
// title from the first message. --mcp given on an existing chat changes it.
func resolveChat(cmd *cobra.Command, cfg config.Config, store *chat.Store, f chatFlags) (chat.Record, error) {
	if f.chatID == "" {
		rec, err := newChatRecord(cfg, f.chatTargetFlags)
		rec.MCP = f.mcp
		return rec, err
	}
	rec, err := store.Get(f.chatID)
	if err != nil {
		return rec, err
	}
	if rec.Kind == chat.KindAsk {
		return rec, fmt.Errorf("%s is an ask; use `devmachine chat ask --chat %s`", rec.ID, rec.ID)
	}
	if err := sameTarget(rec, f.chatTargetFlags); err != nil {
		return rec, err
	}
	if cmd.Flags().Changed("mcp") && f.mcp != rec.MCP {
		if err := store.Update(rec.ID, func(r *chat.Record) { r.MCP = f.mcp }); err != nil {
			return rec, err
		}
		rec.MCP = f.mcp
	}
	return rec, nil
}

func addMCPFlag(c *cobra.Command, mcp *bool) {
	c.Flags().BoolVar(mcp, "mcp", false,
		"run the harness with the account's MCP servers, hooks and plugins (slower to start); kept on the chat")
}

func newChatSendCmd(opts *options) *cobra.Command {
	var flags chatFlags
	c := &cobra.Command{
		Use:   "send [--chat ID] [--workspace W | --local] [--harness claude|codex] [--folder DIR] [--mcp] -- MESSAGE",
		Short: "Send a message and stream the answer as NDJSON",
		Long: "Without --chat it starts a conversation; with --chat it continues one, on the harness, " +
			"workspace and folder the conversation started with. The output is one JSON object per line: " +
			"started, then text and tool, then done or error.",
		RunE: func(cmd *cobra.Command, args []string) error {
			message, err := messageAfterDash(cmd, args, `devmachine chat send -- "why is the build slow?"`)
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
			if rec.ID == "" {
				rec.Title = chat.Title(message)
				if rec, err = store.Create(rec); err != nil {
					return err
				}
			}
			s := chatSession{store: store, emit: chat.NewEmitter(cmd.OutOrStdout()), rec: rec}
			if err := s.start(message); err != nil {
				return err
			}
			client, prefix, failure := chatClient(ctx, cfg, dir, rec.Workspace)
			if failure != nil {
				return s.finish(chat.Outcome{Failure: failure})
			}
			defer client.Close()
			return s.finish(chat.Run(ctx, client, chat.Request{
				Harness: harness, Folder: rec.Folder, Message: message,
				Resume: rec.HarnessSessionID, MCP: rec.MCP, PathPrefix: prefix,
			}, s.relay))
		},
	}
	addChatTargetFlags(c, &flags.chatTargetFlags)
	c.Flags().StringVar(&flags.folder, "folder", "", "the folder the harness runs in on the target (default: chat.folder, ~/chats)")
	c.Flags().StringVar(&flags.chatID, "chat", "", "continue this conversation")
	addMCPFlag(c, &flags.mcp)
	c.Flags().Bool("json", false, "accepted for symmetry: send always prints NDJSON")
	return c
}

func addChatTargetFlags(c *cobra.Command, flags *chatTargetFlags) {
	c.Flags().StringVar(&flags.workspace, "workspace", "", "run on this workspace (default: chat.default_workspace, then the first workspace)")
	c.Flags().BoolVar(&flags.local, "local", false, "run on this computer")
	c.Flags().StringVar(&flags.harness, "harness", "", "claude or codex (default: chat.harness, claude)")
	c.MarkFlagsMutuallyExclusive("workspace", "local")
}

func newChatRecord(cfg config.Config, flags chatTargetFlags) (chat.Record, error) {
	rec := chat.Record{
		Harness: cfg.ChatHarness(), Workspace: cfg.ChatWorkspace(),
		Folder: cfg.ChatFolder(), Kind: chat.KindChat,
	}
	if flags.harness != "" {
		rec.Harness = flags.harness
	}
	switch {
	case flags.local:
		rec.Workspace = chat.LocalWorkspace
	case flags.workspace != "":
		rec.Workspace = flags.workspace
	}
	if flags.folder != "" {
		rec.Folder = flags.folder
	}
	if _, err := chat.Lookup(rec.Harness); err != nil {
		return rec, err
	}
	if rec.Workspace != chat.LocalWorkspace {
		if _, _, err := cfg.MachineFor(rec.Workspace); err != nil {
			return rec, err
		}
	}
	return rec, nil
}

func sameTarget(rec chat.Record, flags chatTargetFlags) error {
	var differ []string
	if flags.harness != "" && flags.harness != rec.Harness {
		differ = append(differ, fmt.Sprintf("--harness %s (it runs on %s)", flags.harness, rec.Harness))
	}
	if flags.local && rec.Workspace != chat.LocalWorkspace {
		differ = append(differ, fmt.Sprintf("--local (it runs on workspace %s)", rec.Workspace))
	}
	if flags.workspace != "" && flags.workspace != rec.Workspace {
		differ = append(differ, fmt.Sprintf("--workspace %s (it runs on %s)", flags.workspace, rec.Workspace))
	}
	if flags.folder != "" && flags.folder != rec.Folder {
		differ = append(differ, fmt.Sprintf("--folder %s (it runs in %s)", flags.folder, rec.Folder))
	}
	if len(differ) > 0 {
		return fmt.Errorf("chat %s keeps the harness, workspace and folder it started with: drop %s, or start a new chat",
			rec.ID, strings.Join(differ, ", "))
	}
	return nil
}

// chatClient reaches where a chat runs, with the PATH prefix that machine
// needs. A failure is already a stream failure.
func chatClient(ctx context.Context, cfg config.Config, dir, workspace string) (remote.Client, []string, *chat.Failure) {
	if workspace == chat.LocalWorkspace {
		client, _, err := remote.Dial(ctx, config.Machine{Name: chat.LocalWorkspace, Self: true}, "")
		if err != nil {
			return nil, nil, &chat.Failure{Kind: chat.KindHarnessFailed, Message: err.Error()}
		}
		return client, nil, nil
	}
	m, w, err := cfg.MachineFor(workspace)
	if err != nil {
		return nil, nil, &chat.Failure{Kind: chat.KindUnreachable, Message: err.Error()}
	}
	client, _, err := dialMux(ctx, m, w.LinuxUser())
	if err != nil {
		if f := chat.ClassifyConnection(err); f != nil {
			return nil, nil, f
		}
		return nil, nil, &chat.Failure{Kind: chat.KindUnreachable, Message: err.Error()}
	}
	var prefix []string
	if observed, found, err := facts.Load(dir, m.Name); err == nil && found {
		prefix = observed.CommandPath()
	}
	return client, prefix, nil
}

// chatSession is one message's stream: it prints the events and keeps the
// local copy of the conversation.
type chatSession struct {
	store *chat.Store
	emit  *chat.Emitter
	rec   chat.Record
}

func (s *chatSession) start(message string) error {
	if err := s.store.AppendMessage(s.rec.ID, chat.Message{Role: chat.RoleUser, Text: message, At: time.Now().UTC()}); err != nil {
		return err
	}
	return s.emit.Emit(chat.Event{Type: chat.EventStarted, Chat: &s.rec})
}

func (s *chatSession) relay(ev chat.Event) { _ = s.emit.Emit(ev) }

func (s *chatSession) finish(out chat.Outcome) error {
	return s.finishWith(out, nil)
}

func (s *chatSession) finishWith(out chat.Outcome, change func(*chat.Record)) error {
	err := s.store.Update(s.rec.ID, func(r *chat.Record) {
		if out.SessionID != "" && (out.Failure == nil || r.HarnessSessionID == "") {
			r.HarnessSessionID = out.SessionID
		}
		if change != nil {
			change(r)
		}
	})
	if err != nil {
		return err
	}
	if out.Text != "" || len(out.Tools) > 0 || len(out.Denied) > 0 || out.Failure != nil {
		if err := s.store.AppendMessage(s.rec.ID, chat.Message{
			Role: chat.RoleAssistant, Text: out.Text, Tools: out.Tools, Denied: out.Denied,
			At: time.Now().UTC(), Error: out.Failure,
		}); err != nil {
			return err
		}
	}
	rec, err := s.store.Get(s.rec.ID)
	if err != nil {
		return err
	}
	if out.Failure != nil {
		if err := s.emit.Emit(chat.Event{Type: chat.EventError, Kind: out.Failure.Kind, Message: out.Failure.Message}); err != nil {
			return err
		}
		return &exitError{code: 1, err: out.Failure, quiet: true}
	}
	return s.emit.Emit(chat.Event{Type: chat.EventDone, Chat: &rec})
}

func newChatListCmd(opts *options) *cobra.Command {
	var all, asJSON bool
	c := &cobra.Command{
		Use:   "list",
		Short: "The conversations, most recent first",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			store, _, err := openChats(opts)
			if err != nil {
				return err
			}
			records, err := store.List(all)
			if err != nil {
				return err
			}
			if asJSON || opts.format == formatJSON {
				return writeJSON(cmd.OutOrStdout(), struct {
					Chats []chat.Record `json:"chats"`
				}{records})
			}
			return writeChatTable(cmd.OutOrStdout(), records)
		},
	}
	c.Flags().BoolVar(&all, "all", false, "also list the asks about a session")
	c.Flags().BoolVar(&asJSON, "json", false, "print the answer as JSON")
	return c
}

func writeChatTable(out io.Writer, records []chat.Record) error {
	tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tTITLE\tHARNESS\tWORKSPACE\tUPDATED")
	for _, r := range records {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", r.ID, r.Title, r.Harness, r.Workspace, r.UpdatedAt.Local().Format("2006-01-02 15:04"))
	}
	if err := tw.Flush(); err != nil {
		return fmt.Errorf("writing the output: %w", err)
	}
	return nil
}

func newChatShowCmd(opts *options) *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "show ID",
		Short: "A conversation and its messages, from the local copy",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, _, err := openChats(opts)
			if err != nil {
				return err
			}
			rec, err := store.Get(args[0])
			if err != nil {
				return err
			}
			messages, err := store.Messages(rec.ID)
			if err != nil {
				return err
			}
			if asJSON || opts.format == formatJSON {
				return writeJSON(cmd.OutOrStdout(), struct {
					Chat     chat.Record    `json:"chat"`
					Messages []chat.Message `json:"messages"`
				}{rec, messages})
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "%s  (%s on %s, in %s)\n", rec.Title, rec.Harness, rec.Workspace, rec.Folder)
			for _, m := range messages {
				who := "you"
				if m.Role == chat.RoleAssistant {
					who = rec.Harness
				}
				fmt.Fprintf(out, "\n%s:\n", who)
				for _, t := range m.Tools {
					fmt.Fprintf(out, "  · %s %s\n", t.Name, t.Summary)
				}
				if m.Text != "" {
					fmt.Fprintln(out, m.Text)
				}
				for _, t := range m.Denied {
					fmt.Fprintf(out, "  blocked: %s %s\n", t.Name, t.Summary)
				}
				if m.Error != nil {
					fmt.Fprintf(out, "error (%s): %s\n", m.Error.Kind, m.Error.Message)
				}
			}
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "print the answer as JSON")
	return c
}

func newChatRenameCmd(opts *options) *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "rename ID TITLE",
		Short: "Give a conversation another title",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			title := strings.TrimSpace(strings.Join(args[1:], " "))
			if title == "" {
				return errors.New("the title is empty")
			}
			return changeChat(cmd, opts, args[0], asJSON, func(r *chat.Record) { r.Title = title })
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "print the record as JSON")
	return c
}

func changeChat(cmd *cobra.Command, opts *options, id string, asJSON bool, change func(*chat.Record)) error {
	store, _, err := openChats(opts)
	if err != nil {
		return err
	}
	if err := store.Update(id, change); err != nil {
		return err
	}
	if !asJSON && opts.format != formatJSON {
		return nil
	}
	rec, err := store.Get(id)
	if err != nil {
		return err
	}
	return writeJSON(cmd.OutOrStdout(), rec)
}

func newChatDeleteCmd(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "delete ID",
		Short: "Forget a conversation's local copy; the harness's own transcript stays",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, _, err := openChats(opts)
			if err != nil {
				return err
			}
			return store.Delete(args[0])
		},
	}
}
