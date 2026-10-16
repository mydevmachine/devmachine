package chat

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"strconv"
	"strings"
	"sync"

	"github.com/mydevmachine/devmachine/internal/remote"
)

// Request is one message to a harness.
type Request struct {
	Harness Harness
	// Folder is where the harness runs; a leading ~ is the account's home.
	Folder  string
	Message string
	// Resume is the harness's own session id to continue; empty starts one.
	Resume string
	// Fork asks claude for a new session that starts from Resume.
	Fork bool
	// PathPrefix goes in front of PATH, for a machine whose package manager
	// installs outside the default PATH.
	PathPrefix []string
	// Ask runs the harness's AskArgs with Message on stdin instead of in an
	// argument, which an ask's context would outgrow.
	Ask bool
}

// Outcome is how a run ended.
type Outcome struct {
	SessionID string
	Text      string
	Tools     []Tool
	Denied    []Tool
	Failure   *Failure
}

const missingSentinel = "@@DEVMACHINE_HARNESS_MISSING@@"

// Script is the program /bin/sh reads on stdin to run one message. It reaches
// sh only, never the account's login shell, which can be fish and read the
// quoting another way. The login shell is asked only for its PATH, so a
// harness installed by a version manager is still found.
func Script(r Request) string {
	var b strings.Builder
	b.WriteString(pathPrelude(r.PathPrefix))
	b.WriteString("dm_folder=" + folderWord(r.Folder) + "\n")
	b.WriteString(`mkdir -p "$dm_folder" && cd "$dm_folder" || exit 3` + "\n")
	name := quote(r.Harness.Name)
	b.WriteString("command -v " + name + " >/dev/null 2>&1 || { echo " + missingSentinel + " >&2; exit 127; }\n")
	b.WriteString("exec " + name)
	args := r.Harness.Args(r.Message, r.Resume, r.Fork)
	if r.Ask {
		args = r.Harness.AskArgs
	}
	for _, arg := range args {
		b.WriteString(" " + quote(arg))
	}
	if !r.Ask {
		b.WriteString(" </dev/null\n")
		return b.String()
	}
	end := hereDocEnd(r.Message)
	b.WriteString(" <<'" + end + "'\n" + strings.TrimSuffix(r.Message, "\n") + "\n" + end + "\n")
	return b.String()
}

// hereDocEnd is a here-document's last line that no line of body is.
func hereDocEnd(body string) string {
	for n := 1; ; n++ {
		end := "DM_PROMPT_" + strconv.Itoa(n)
		if !strings.Contains(body, end) {
			return end
		}
	}
}

func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

func folderWord(folder string) string {
	switch {
	case folder == "" || folder == "~":
		return `"$HOME"`
	case strings.HasPrefix(folder, "~/"):
		return `"$HOME"/` + quote(strings.TrimPrefix(folder, "~/"))
	}
	return quote(folder)
}

// Run sends one message and reports each event as it arrives. It never
// returns an error: every way a run can end is in the Outcome.
func Run(ctx context.Context, client remote.Client, r Request, on func(Event)) Outcome {
	p := r.Harness.parser()
	reader, writer := io.Pipe()
	stderr := &tailBuffer{max: 16 * 1024}
	done := make(chan error, 1)
	go func() {
		err := remote.StreamInput(remote.OwnProcessGroup(ctx), client, remote.ShellOnInput, strings.NewReader(Script(r)), writer, stderr)
		_ = writer.Close()
		done <- err
	}()

	var out Outcome
	var text strings.Builder
	lines := bufio.NewReader(reader)
	for {
		line, readErr := lines.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			for _, ev := range p.feed(bytes.TrimSpace(line)) {
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
		}
		if readErr != nil {
			break
		}
	}
	runErr := <-done
	sessionID, ended, failure := p.result()
	out.SessionID, out.Text = sessionID, text.String()
	out.Failure = classify(ctx, r.Harness.Name, runErr, ended, failure, p.noise(), stderr.String())
	return out
}

func classify(ctx context.Context, harness string, runErr error, ended bool, failure *Failure, noise, stderr string) *Failure {
	explanation := strings.TrimSpace(strings.Join([]string{strings.TrimSpace(noise), strings.TrimSpace(strings.ReplaceAll(stderr, missingSentinel, ""))}, "\n"))
	switch {
	case ctx.Err() != nil:
		return fail(KindCancelled, "stopped before the answer finished")
	case failure != nil:
		return failure
	case runErr != nil && ClassifyConnection(runErr) != nil:
		return ClassifyConnection(runErr)
	case strings.Contains(stderr, missingSentinel) || exitCode(runErr) == 127:
		return fail(KindHarnessMissing, "%s is not installed for this account, or not on its login shell's PATH", harness)
	case loggedOut(explanation):
		return fail(KindHarnessLoggedOut, "%s is not logged in: %s", harness, lastLines(explanation))
	case runErr != nil:
		if explanation == "" {
			explanation = runErr.Error()
		}
		return fail(KindHarnessFailed, "%s", lastLines(explanation))
	case !ended:
		if explanation == "" {
			explanation = "no output"
		}
		return fail(KindHarnessFailed, "%s stopped without finishing its answer: %s", harness, lastLines(explanation))
	}
	return nil
}

// ClassifyConnection names a failure to reach the machine, or returns nil
// when err is something else.
func ClassifyConnection(err error) *Failure {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, remote.ErrHostKeyRejected):
		return &Failure{Kind: KindHostKeyRejected, Message: err.Error()}
	case errors.Is(err, remote.ErrAuthRefused):
		return &Failure{Kind: KindLoginRefused, Message: err.Error()}
	case errors.Is(err, context.DeadlineExceeded),
		strings.Contains(err.Error(), "no address answered"),
		strings.Contains(err.Error(), "the connection dropped"):
		return &Failure{Kind: KindUnreachable, Message: err.Error()}
	}
	return nil
}

func exitCode(err error) int {
	var exit interface{ ExitCode() int }
	if errors.As(err, &exit) {
		return exit.ExitCode()
	}
	return 0
}

func lastLines(s string) string {
	all := strings.Split(strings.TrimSpace(s), "\n")
	if len(all) > 5 {
		all = all[len(all)-5:]
	}
	return strings.Join(all, "\n")
}

type tailBuffer struct {
	mu  sync.Mutex
	buf []byte
	max int
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > t.max {
		t.buf = t.buf[len(t.buf)-t.max:]
	}
	return len(p), nil
}

func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(t.buf)
}
