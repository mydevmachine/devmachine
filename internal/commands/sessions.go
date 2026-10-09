package commands

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"sync"
	"text/tabwriter"
	"time"

	"github.com/mydevmachine/devmachine/internal/config"
	"github.com/mydevmachine/devmachine/internal/remote"
	"github.com/mydevmachine/devmachine/internal/sessions"
	"github.com/spf13/cobra"
)

const sessionsTimeout = 5 * time.Second

type sessionsWorkspace struct {
	Name     string             `json:"name"`
	Machine  string             `json:"machine"`
	Sessions []sessions.Session `json:"sessions"`
	Error    *sessionsError     `json:"error,omitempty"`
}

type sessionsError struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
}

func classifySessionsError(err error) sessionsError {
	kind := "other"
	switch {
	case errors.Is(err, sessions.ErrTmuxMissing):
		kind = "tmux-missing"
	case errors.Is(err, remote.ErrHostKeyRejected):
		kind = "host-key-rejected"
	case errors.Is(err, context.DeadlineExceeded), strings.Contains(err.Error(), "no address answered"):
		kind = "unreachable"
	}
	return sessionsError{Kind: kind, Message: err.Error()}
}

func newSessionsCmd(opts *options) *cobra.Command {
	var (
		names  []string
		asJSON bool
	)
	c := &cobra.Command{
		Use:   "sessions",
		Short: "The tmux sessions of every workspace, with branch, age, busy and attention",
		Long: "It asks each workspace's machine for its tmux sessions, all at once, and prints " +
			"what came back. A workspace that cannot answer carries an error; the others still answer.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig(opts)
			if err != nil {
				return err
			}
			chosen, err := chooseSessionWorkspaces(cfg, names)
			if err != nil {
				return err
			}
			results := collectSessions(cmd.Context(), cfg, chosen)
			if asJSON || opts.format == formatJSON {
				return writeJSON(cmd.OutOrStdout(), struct {
					Workspaces []sessionsWorkspace `json:"workspaces"`
				}{results})
			}
			return writeSessionsTable(cmd.OutOrStdout(), results)
		},
	}
	c.Flags().StringArrayVar(&names, "workspace", nil, "only this workspace (repeatable)")
	c.Flags().BoolVar(&asJSON, "json", false, "print the answer as JSON")
	return c
}

func chooseSessionWorkspaces(cfg config.Config, names []string) ([]config.Workspace, error) {
	if len(names) == 0 {
		return cfg.Workspaces, nil
	}
	for _, name := range names {
		if _, err := cfg.Workspace(name); err != nil {
			return nil, err
		}
	}
	var chosen []config.Workspace
	for _, w := range cfg.Workspaces {
		for _, name := range names {
			if w.Name == name {
				chosen = append(chosen, w)
				break
			}
		}
	}
	return chosen, nil
}

func collectSessions(ctx context.Context, cfg config.Config, chosen []config.Workspace) []sessionsWorkspace {
	results := make([]sessionsWorkspace, len(chosen))
	var wg sync.WaitGroup
	for i, w := range chosen {
		wg.Go(func() {
			results[i] = collectWorkspaceSessions(ctx, cfg, w)
		})
	}
	wg.Wait()
	return results
}

func collectWorkspaceSessions(ctx context.Context, cfg config.Config, w config.Workspace) sessionsWorkspace {
	result := sessionsWorkspace{Name: w.Name, Machine: machineNameOf(cfg, w), Sessions: []sessions.Session{}}
	fail := func(err error) sessionsWorkspace {
		e := classifySessionsError(err)
		result.Error = &e
		return result
	}
	m, _, err := cfg.MachineFor(w.Name)
	if err != nil {
		return fail(err)
	}
	ctx, cancel := context.WithTimeout(ctx, sessionsTimeout)
	defer cancel()
	client, _, err := dialMux(ctx, m, w.LinuxUser())
	if err != nil {
		return fail(err)
	}
	defer client.Close()
	found, err := sessions.Collect(ctx, client)
	if err != nil {
		return fail(err)
	}
	result.Sessions = found
	return result
}

func writeSessionsTable(out io.Writer, results []sessionsWorkspace) error {
	tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "WORKSPACE\tSESSION\tBRANCH\tAGE\tSTATE")
	for _, w := range results {
		if w.Error != nil {
			fmt.Fprintf(tw, "%s\t-\t-\t-\terror: %s\n", w.Name, w.Error.Message)
			continue
		}
		for _, s := range w.Sessions {
			branch := s.Branch
			if branch == "" {
				branch = path.Base(s.Path)
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", w.Name, s.Name, branch, sessionAge(s.AgeSeconds), sessionState(s))
		}
	}
	if err := tw.Flush(); err != nil {
		return fmt.Errorf("writing the output: %w", err)
	}
	return nil
}

func sessionAge(seconds int64) string {
	switch {
	case seconds < 60:
		return "<1m"
	case seconds < 3600:
		return fmt.Sprintf("%dm", seconds/60)
	case seconds < 86400:
		return fmt.Sprintf("%dh", seconds/3600)
	default:
		return fmt.Sprintf("%dd", seconds/86400)
	}
}

func sessionState(s sessions.Session) string {
	switch {
	case s.Busy:
		return "busy"
	case s.Attention:
		return "attention"
	default:
		return ""
	}
}
