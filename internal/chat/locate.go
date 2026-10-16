package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/mydevmachine/devmachine/internal/remote"
)

// Target is a coding session to ask about.
type Target struct {
	// Session is the session's name: a tmux session on a workspace, or the
	// name the app gives a session on this computer.
	Session string
	// Folder is the session's folder. Given, no tmux is asked; this is how
	// a session on this computer, which runs under zmx, is found.
	Folder string
	// Harness is the harness the caller already knows runs there.
	Harness    string
	PathPrefix []string
}

// Located is where a session's transcript is.
type Located struct {
	Folder     string
	Harness    string
	Transcript string
	// SessionID is the harness's own id for the session.
	SessionID string
}

const (
	noSessionSentinel = "@@NO_SESSION@@"
	noFolderSentinel  = "@@NO_FOLDER@@"
	realSentinel      = "@@REAL@@"
	psSentinel        = "@@PS@@"
	claudeSentinel    = "@@CLAUDE@@"
	codexSentinel     = "@@CODEX@@"
	newestSentinel    = "@@NEWEST@@"
)

// AskHarnesses are the harnesses Ask can answer about.
var AskHarnesses = []string{"claude", "codex"}

var knownHarnesses = []string{"claude", "codex", "pi", "opencode", "kimi", "cline", "antigravity"}

var programHarness = map[string]string{
	"opencode": "opencode", "kimi": "kimi", "kimi-code": "kimi",
	"cline": "cline", ".cline": "cline", "agy": "antigravity",
}

var versionName = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

// Locate finds the session's folder, its harness and its newest transcript.
// It only reads: nothing of the session is written.
func Locate(ctx context.Context, client remote.Client, t Target) (Located, *Failure) {
	out, err := client.RunInput(ctx, remote.ShellOnInput, strings.NewReader(locateScript(t)))
	if f := ClassifyConnection(err); f != nil {
		return Located{}, f
	}
	if err != nil {
		return Located{}, fail(KindSessionNotFound, "reading session %q: %v", t.Session, err)
	}
	sections := splitSections(out)
	if _, ok := sections[noSessionSentinel]; ok {
		return Located{}, fail(KindSessionNotFound, "there is no tmux session %q here", t.Session)
	}
	if _, ok := sections[noFolderSentinel]; ok {
		return Located{}, fail(KindSessionNotFound, "the folder %q of session %q does not exist", t.Folder, t.Session)
	}
	loc := Located{Folder: firstLine(sections[realSentinel]), Harness: t.Harness}
	if loc.Harness == "" {
		loc.Harness = paneHarness(firstLine(sections[""]), sections[psSentinel])
	}
	if loc.Harness != "" && !slices.Contains(AskHarnesses, loc.Harness) {
		return loc, fail(KindHarnessUnsupported, "Ask is not available for %s yet", loc.Harness)
	}

	out, err = client.RunInput(ctx, remote.ShellOnInput, strings.NewReader(transcriptScript(loc.Folder, loc.Harness, t.PathPrefix)))
	if f := ClassifyConnection(err); f != nil {
		return loc, f
	}
	if err != nil {
		return loc, fail(KindTranscriptNotFound, "looking for the transcript of session %q: %v", t.Session, err)
	}
	found := splitSections(out)
	claudeFile := firstLine(found[claudeSentinel])
	codexLines := strings.SplitN(found[codexSentinel], "\n", 2)
	codexFile := strings.TrimSpace(codexLines[0])
	if loc.Harness == "" {
		switch newest := firstLine(found[newestSentinel]); {
		case newest != "" && newest == claudeFile:
			loc.Harness = "claude"
		case newest != "" && newest == codexFile:
			loc.Harness = "codex"
		}
	}
	switch loc.Harness {
	case "claude":
		if claudeFile == "" {
			break
		}
		loc.Transcript = claudeFile
		loc.SessionID = strings.TrimSuffix(path.Base(claudeFile), ".jsonl")
	case "codex":
		if codexFile == "" {
			break
		}
		loc.Transcript = codexFile
		if len(codexLines) == 2 {
			loc.SessionID = codexSessionID(codexLines[1])
		}
	}
	if loc.Transcript == "" {
		harness := loc.Harness
		if harness == "" {
			harness = "claude or codex"
		}
		return loc, fail(KindTranscriptNotFound, "no %s transcript was written from %s", harness, loc.Folder)
	}
	return loc, nil
}

// ReadTranscript is the end of a transcript, at most limit bytes of it.
func ReadTranscript(ctx context.Context, client remote.Client, file string, limit int) ([]byte, *Failure) {
	out, err := client.RunInput(ctx, remote.ShellOnInput,
		strings.NewReader("tail -c "+strconv.Itoa(limit)+" "+quote(file)+"\n"))
	if f := ClassifyConnection(err); f != nil {
		return nil, f
	}
	if err != nil {
		return nil, fail(KindTranscriptNotFound, "reading %s: %v", file, err)
	}
	return []byte(out), nil
}

func pathPrelude(prefix []string) string {
	var b strings.Builder
	b.WriteString(`dm_login_path=$("${SHELL:-/bin/sh}" -l -c env </dev/null 2>/dev/null | sed -n 's/^PATH=//p' | tail -n 1)` + "\n")
	if len(prefix) > 0 {
		b.WriteString("PATH=" + quote(strings.Join(prefix, ":")) + `:"$PATH"` + "\n")
	}
	b.WriteString(`[ -n "$dm_login_path" ] && PATH="$PATH:$dm_login_path"` + "\n")
	b.WriteString(`PATH="$PATH:$HOME/.local/bin"; export PATH` + "\n")
	return b.String()
}

func locateScript(t Target) string {
	var b strings.Builder
	b.WriteString(pathPrelude(t.PathPrefix))
	if t.Folder != "" {
		b.WriteString("dm_dir=" + folderWord(t.Folder) + "\n")
	} else {
		b.WriteString("tmux has-session -t " + quote("="+t.Session) + " 2>/dev/null || { echo " + noSessionSentinel + "; exit 0; }\n")
		b.WriteString("dm_info=$(tmux -u display-message -p -t " + quote("="+t.Session+":") +
			` '#{pane_current_path}	#{pane_current_command}	#{pane_pid}' 2>/dev/null)` + "\n")
		b.WriteString(`dm_dir=$(printf '%s' "$dm_info" | cut -f 1)` + "\n")
		b.WriteString(`[ -n "$dm_dir" ] || { echo ` + noSessionSentinel + "; exit 0; }\n")
		b.WriteString(`printf '%s\n' "$dm_info"` + "\n")
	}
	b.WriteString(`cd "$dm_dir" 2>/dev/null || { echo ` + noFolderSentinel + "; exit 0; }\n")
	b.WriteString("echo " + realSentinel + "\npwd -P\n")
	if t.Folder == "" && t.Harness == "" {
		b.WriteString("echo " + psSentinel + "\nps -A -o pid=,ppid=,args= 2>/dev/null\n")
	}
	b.WriteString("exit 0\n")
	return b.String()
}

// ClaudeSlug is the folder Claude Code keeps a project's transcripts in:
// every character that is not an ASCII letter or digit becomes "-".
func ClaudeSlug(folder string) string {
	var b strings.Builder
	for _, r := range folder {
		if r < 128 && (r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			b.WriteRune(r)
			continue
		}
		b.WriteByte('-')
	}
	return b.String()
}

func transcriptScript(folder, harness string, prefix []string) string {
	var b strings.Builder
	b.WriteString(pathPrelude(prefix))
	if harness == "" || harness == "claude" {
		slug := quote(ClaudeSlug(folder))
		b.WriteString(`dm_claude=$(ls -t "$HOME"/.claude/projects/` + slug + `/*.jsonl "$HOME"/.claude/projects/` + slug + `-*/*.jsonl 2>/dev/null | head -n 1)` + "\n")
		b.WriteString("echo " + claudeSentinel + "\n" + `printf '%s\n' "$dm_claude"` + "\n")
	}
	if harness == "" || harness == "codex" {
		needle := quote(`"cwd":` + jsonString(folder))
		b.WriteString(`dm_codex=$(ls -t "$HOME"/.codex/sessions/*/*/*/rollout-*.jsonl 2>/dev/null | head -n 300 | while IFS= read -r f; do head -n 1 "$f" | grep -qF -- ` +
			needle + ` && { printf '%s\n' "$f"; break; }; done)` + "\n")
		b.WriteString("echo " + codexSentinel + "\n" + `printf '%s\n' "$dm_codex"` + "\n")
		b.WriteString(`[ -n "$dm_codex" ] && head -n 1 "$dm_codex" | head -c 1048576` + "\n")
	}
	if harness == "" {
		b.WriteString("echo " + newestSentinel + "\n" + `ls -t $dm_claude $dm_codex 2>/dev/null | head -n 1` + "\n")
	}
	b.WriteString("exit 0\n")
	return b.String()
}

func jsonString(s string) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	return strings.TrimSpace(buf.String())
}

func splitSections(out string) map[string]string {
	sections := map[string]string{}
	current := ""
	var lines []string
	flush := func() { sections[current] = strings.Join(lines, "\n") }
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "@@") && strings.HasSuffix(line, "@@") {
			flush()
			current, lines = line, nil
			continue
		}
		lines = append(lines, line)
	}
	flush()
	return sections
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return strings.TrimSpace(line)
}

func codexSessionID(firstLine string) string {
	var v struct {
		Payload struct {
			ID string `json:"id"`
		} `json:"payload"`
	}
	if json.Unmarshal([]byte(strings.TrimSpace(firstLine)), &v) != nil {
		return ""
	}
	return v.Payload.ID
}

// paneHarness reads the harness from the pane's command, then from the
// command lines of the processes under the pane.
func paneHarness(info, ps string) string {
	fields := strings.Split(info, "\t")
	if len(fields) < 3 {
		return ""
	}
	command := strings.ToLower(strings.TrimSpace(fields[1]))
	if h, ok := programHarness[command]; ok {
		return h
	}
	if slices.Contains(knownHarnesses, command) {
		return command
	}
	if versionName.MatchString(command) {
		return "claude"
	}
	root := strings.TrimSpace(fields[2])
	children := map[string][][2]string{}
	for _, line := range strings.Split(ps, "\n") {
		parts := strings.Fields(line)
		if len(parts) < 3 {
			continue
		}
		args := strings.Join(parts[2:], " ")
		if parts[0] == root {
			if h := harnessIn(args); h != "" {
				return h
			}
		}
		children[parts[1]] = append(children[parts[1]], [2]string{parts[0], args})
	}
	queue := []string{root}
	for len(queue) > 0 {
		pid := queue[0]
		queue = queue[1:]
		for _, child := range children[pid] {
			if h := harnessIn(child[1]); h != "" {
				return h
			}
			queue = append(queue, child[0])
		}
	}
	return ""
}

func harnessIn(args string) string {
	words := strings.Fields(args)
	if len(words) == 0 {
		return ""
	}
	first := path.Base(words[0])
	switch {
	case first == "claude" || strings.Contains(args, "/claude/versions/"):
		return "claude"
	case first == "codex" || strings.Contains(args, "@openai/codex"):
		return "codex"
	case first == "pi" || strings.Contains(args, "pi-coding-agent"):
		return "pi"
	}
	names := []string{first}
	if first == "node" || first == "bun" || first == "deno" {
		for _, w := range words[1:] {
			if !strings.HasPrefix(w, "-") {
				names = append(names, path.Base(w))
				break
			}
		}
	}
	for _, name := range names {
		if h, ok := programHarness[name]; ok {
			return h
		}
	}
	return ""
}
