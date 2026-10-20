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

// A dead host fails sooner, on ssh's ConnectTimeout, and a login the SSH agent
// has not approved answers login-waiting within seconds (remote.Login). The
// rest is for a slow machine.
const sessionsTimeout = 30 * time.Second

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
	case errors.Is(err, remote.ErrAuthRefused):
		kind = "login-refused"
	case errors.Is(err, remote.ErrLoginWaiting):
		kind = "login-waiting"
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
			chosen, unknown := chooseSessionWorkspaces(cfg, names)
			results := append(collectSessions(cmd.Context(), cfg, chosen), unknown...)
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

func chooseSessionWorkspaces(cfg config.Config, names []string) ([]config.Workspace, []sessionsWorkspace) {
	if len(names) == 0 {
		return cfg.Workspaces, nil
	}
	var unknown []sessionsWorkspace
	for _, name := range names {
		if _, err := cfg.Workspace(name); err != nil {
			unknown = append(unknown, sessionsWorkspace{
				Name:     name,
				Sessions: []sessions.Session{},
				Error:    &sessionsError{Kind: "not-configured", Message: err.Error()},
			})
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
	return chosen, unknown
}

// loginOnce is one login per machine and Linux user, shared by every
// workspace that logs in as that user: workspaces asked all at once would
// otherwise each log in, each prompt the SSH agent and each count on the
// machine.
type loginOnce struct {
	mu     sync.Mutex
	logins map[string]*onceLogin
}

type onceLogin struct {
	once sync.Once
	err  error
}

// loginer is a client that opens its connection before the commands that use it.
type loginer interface {
	Login(ctx context.Context) error
}

func (l *loginOnce) login(ctx context.Context, key string, client remote.Client) error {
	c, ok := client.(loginer)
	if !ok {
		return nil
	}
	l.mu.Lock()
	o, found := l.logins[key]
	if !found {
		o = &onceLogin{}
		l.logins[key] = o
	}
	l.mu.Unlock()
	o.once.Do(func() { o.err = c.Login(ctx) })
	return o.err
}

func collectSessions(ctx context.Context, cfg config.Config, chosen []config.Workspace) []sessionsWorkspace {
	results := make([]sessionsWorkspace, len(chosen))
	logins := &loginOnce{logins: map[string]*onceLogin{}}
	var wg sync.WaitGroup
	for i, w := range chosen {
		wg.Go(func() {
			results[i] = collectWorkspaceSessions(ctx, cfg, w, logins)
		})
	}
	wg.Wait()
	return results
}

func collectWorkspaceSessions(ctx context.Context, cfg config.Config, w config.Workspace, logins *loginOnce) sessionsWorkspace {
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
	if err := logins.login(ctx, m.Name+"\x00"+w.LinuxUser(), client); err != nil {
		return fail(err)
	}
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
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", w.Name, s.Name, sessionBranch(s), sessionAge(s.AgeSeconds), sessionState(s))
		}
	}
	if err := tw.Flush(); err != nil {
		return fmt.Errorf("writing the output: %w", err)
	}
	return nil
}

func sessionBranch(s sessions.Session) string {
	switch {
	case s.Branch != "":
		return s.Branch
	case s.Path != "" && s.Path != "/":
		return path.Base(s.Path)
	default:
		return "-"
	}
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
	case s.Attention:
		return "attention"
	case s.Busy:
		return "busy"
	default:
		return ""
	}
}
