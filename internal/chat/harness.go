package chat

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// Options are how one run of a harness is asked for.
type Options struct {
	Message string
	// Resume is the harness's own session id to continue.
	Resume string
	// MCP starts the MCP servers, hooks and plugins of the account's own
	// settings. Off, a run skips them: they are most of a harness's start-up.
	MCP bool
	// Stream keeps claude reading messages on stdin, one JSON line each,
	// instead of answering one message and exiting.
	Stream bool
}

// Harness is a coding CLI a chat talks through.
type Harness struct {
	Name string
	Args func(Options) []string
	// Streams says whether the harness can stay running between messages.
	Streams bool
	// AskArgs answer one prompt read from stdin, as fast as the harness
	// starts: no tools, no MCP servers, nothing kept on disk.
	AskArgs []string
	parser  func() parser
}

type parser interface {
	feed(line []byte) []Event
	result() (sessionID string, ended bool, failure *Failure)
	noise() string
}

// Claude is Claude Code, always in plan mode so it cannot change a file.
var Claude = Harness{
	Name: "claude",
	Args: func(o Options) []string {
		args := []string{"-p", "--output-format", "stream-json", "--verbose", "--include-partial-messages", "--permission-mode", "plan"}
		if !o.MCP {
			args = append(args, "--strict-mcp-config", "--setting-sources", "project")
		}
		if o.Stream {
			args = append(args, "--input-format", "stream-json")
		}
		if o.Resume != "" {
			args = append(args, "--resume", o.Resume)
		}
		if o.Stream {
			return args
		}
		return append(args, "--", o.Message)
	},
	Streams: true,
	AskArgs: []string{"-p", "--output-format", "stream-json", "--verbose", "--include-partial-messages",
		"--strict-mcp-config", "--setting-sources", "project", "--disable-slash-commands", "--tools", "",
		"--no-session-persistence", "--model", "haiku"},
	parser: func() parser { return &claudeParser{} },
}

// Codex is the Codex CLI, always in a read-only sandbox. It has no fork.
var Codex = Harness{
	Name: "codex",
	Args: func(o Options) []string {
		args := []string{"exec", "--json", "--skip-git-repo-check", "--sandbox", "read-only"}
		if !o.MCP {
			args = append(args, "--ignore-user-config")
		}
		if o.Resume != "" {
			args = append(args, "resume", o.Resume)
		}
		return append(args, "--", o.Message)
	},
	AskArgs: []string{"exec", "--json", "--ephemeral", "--ignore-user-config", "--skip-git-repo-check", "--sandbox", "read-only", "-"},
	parser:  func() parser { return &codexParser{} },
}

// Lookup is the harness with this name.
func Lookup(name string) (Harness, error) {
	switch name {
	case Claude.Name:
		return Claude, nil
	case Codex.Name:
		return Codex, nil
	}
	return Harness{}, fmt.Errorf("harness %q: a chat runs on claude or codex", name)
}

var loggedOutText = regexp.MustCompile(`(?i)invalid api key|please run /login|not logged in|log in again|oauth token (has )?expired|\b401\b|unauthorized`)

func loggedOut(text string) bool { return loggedOutText.MatchString(text) }

type lines struct{ other []string }

func (l *lines) keep(line []byte) {
	if s := strings.TrimSpace(string(line)); s != "" && len(l.other) < 40 {
		l.other = append(l.other, s)
	}
}

func (l *lines) noise() string { return strings.Join(l.other, "\n") }

type claudeParser struct {
	lines
	sessionID, cwd string
	wroteText      bool
	ended          bool
	failure        *Failure
}

type claudeLine struct {
	Type            string  `json:"type"`
	Subtype         string  `json:"subtype"`
	SessionID       string  `json:"session_id"`
	Cwd             string  `json:"cwd"`
	ParentToolUseID *string `json:"parent_tool_use_id"`
	IsError         bool    `json:"is_error"`
	Result          string  `json:"result"`
	Denials         []struct {
		ToolName  string         `json:"tool_name"`
		ToolInput map[string]any `json:"tool_input"`
	} `json:"permission_denials"`
	Event   json.RawMessage `json:"event"`
	Message struct {
		Content []struct {
			Type  string         `json:"type"`
			Name  string         `json:"name"`
			Input map[string]any `json:"input"`
		} `json:"content"`
	} `json:"message"`
}

type claudeStreamEvent struct {
	Type         string `json:"type"`
	ContentBlock struct {
		Type string `json:"type"`
	} `json:"content_block"`
	Delta struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"delta"`
}

func (p *claudeParser) feed(raw []byte) []Event {
	var line claudeLine
	if err := json.Unmarshal(raw, &line); err != nil || line.Type == "" {
		p.keep(raw)
		return nil
	}
	if line.SessionID != "" && p.sessionID == "" {
		p.sessionID = line.SessionID
	}
	if line.ParentToolUseID != nil && *line.ParentToolUseID != "" {
		return nil
	}
	switch line.Type {
	case "system":
		if line.Subtype == "init" {
			p.sessionID, p.cwd = line.SessionID, line.Cwd
		}
	case "stream_event":
		return p.streamEvent(line.Event)
	case "assistant":
		var out []Event
		for _, block := range line.Message.Content {
			if block.Type == "tool_use" {
				out = append(out, Event{Type: EventTool, Name: block.Name, Summary: toolSummary(block.Input, p.cwd)})
			}
		}
		return out
	case "result":
		p.ended = true
		if line.IsError {
			kind := KindHarnessFailed
			if loggedOut(line.Result) {
				kind = KindHarnessLoggedOut
			}
			p.failure = &Failure{Kind: kind, Message: strings.TrimSpace(line.Result)}
		}
		var out []Event
		for _, d := range line.Denials {
			out = append(out, Event{Type: EventDenied, Name: d.ToolName, Summary: toolSummary(d.ToolInput, p.cwd)})
		}
		return out
	}
	return nil
}

func (p *claudeParser) streamEvent(raw json.RawMessage) []Event {
	var ev claudeStreamEvent
	if err := json.Unmarshal(raw, &ev); err != nil {
		return nil
	}
	switch {
	case ev.Type == "content_block_start" && ev.ContentBlock.Type == "text" && p.wroteText:
		return []Event{{Type: EventText, Delta: "\n\n"}}
	case ev.Type == "content_block_delta" && ev.Delta.Type == "text_delta" && ev.Delta.Text != "":
		p.wroteText = true
		return []Event{{Type: EventText, Delta: ev.Delta.Text}}
	}
	return nil
}

func (p *claudeParser) result() (string, bool, *Failure) { return p.sessionID, p.ended, p.failure }

const summaryMax = 120

func toolSummary(input map[string]any, cwd string) string {
	for _, key := range []string{"file_path", "notebook_path", "path", "command", "pattern", "url", "query", "description", "prompt"} {
		value, ok := input[key].(string)
		if !ok || value == "" {
			continue
		}
		if cwd != "" {
			value = strings.TrimPrefix(value, strings.TrimSuffix(cwd, "/")+"/")
		}
		return shorten(value)
	}
	return ""
}

func shorten(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > summaryMax {
		return string(r[:summaryMax-1]) + "…"
	}
	return s
}

type codexParser struct {
	lines
	threadID  string
	wroteText bool
	ended     bool
	failure   *Failure
}

type codexLine struct {
	Type     string `json:"type"`
	ThreadID string `json:"thread_id"`
	Message  string `json:"message"`
	Error    struct {
		Message string `json:"message"`
	} `json:"error"`
	Item struct {
		Type    string `json:"type"`
		Text    string `json:"text"`
		Command string `json:"command"`
	} `json:"item"`
}

var codexShellWrapper = regexp.MustCompile(`^\S*/?(ba|z|fi)?sh -l?c '(.*)'$`)

func (p *codexParser) feed(raw []byte) []Event {
	var line codexLine
	if err := json.Unmarshal(raw, &line); err != nil || line.Type == "" {
		p.keep(raw)
		return nil
	}
	switch line.Type {
	case "thread.started":
		p.threadID = line.ThreadID
	case "item.started":
		if line.Item.Type == "command_execution" {
			command := line.Item.Command
			if m := codexShellWrapper.FindStringSubmatch(command); m != nil {
				command = m[2]
			}
			return []Event{{Type: EventTool, Name: "shell", Summary: shorten(command)}}
		}
	case "item.completed":
		if line.Item.Type == "agent_message" && line.Item.Text != "" {
			var out []Event
			if p.wroteText {
				out = append(out, Event{Type: EventText, Delta: "\n\n"})
			}
			p.wroteText = true
			return append(out, Event{Type: EventText, Delta: line.Item.Text})
		}
	case "turn.completed":
		p.ended = true
	case "turn.failed":
		p.ended = true
		p.setFailure(line.Error.Message)
	case "error":
		p.setFailure(line.Message)
	}
	return nil
}

func (p *codexParser) setFailure(message string) {
	kind := KindHarnessFailed
	if loggedOut(message) {
		kind = KindHarnessLoggedOut
	}
	if p.failure == nil || kind == KindHarnessLoggedOut {
		p.failure = &Failure{Kind: kind, Message: strings.TrimSpace(message)}
	}
}

func (p *codexParser) result() (string, bool, *Failure) { return p.threadID, p.ended, p.failure }
