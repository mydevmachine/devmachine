package chat

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"unicode"
)

// The limits of the context an ask sends.
const (
	recentTurns   = 6
	unitMax       = 2000
	toolOutputMax = 400
	contextMax    = 28 * 1024
)

const roleTool, roleToolOutput = "tool", "tool output"

type unit struct {
	role, text string
}

// Excerpt is the prompt an ask sends: the context built from the session's
// transcript, the side conversation so far, and the question.
type Excerpt struct {
	Prompt string
	// Count is how many pieces of the transcript went into the prompt.
	Count int
}

var stopWords = map[string]bool{
	"what": true, "when": true, "where": true, "which": true, "while": true, "does": true,
	"this": true, "that": true, "with": true, "from": true, "have": true, "about": true,
	"there": true, "their": true, "would": true, "could": true, "should": true, "into": true,
	"your": true, "them": true, "they": true, "were": true, "been": true, "will": true,
	"just": true, "than": true, "then": true, "some": true, "session": true, "doing": true,
}

// BuildContext reads a session's transcript, a claude jsonl or a codex
// rollout, and builds the prompt that asks the question about it: the last
// turns, plus the lines that share a word with the question, within a fixed
// size. earlier, the side conversation so far, goes right before the
// question.
func BuildContext(transcript []byte, harness, folder, earlier, question string) Excerpt {
	units := codexUnits(transcript)
	if harness == Claude.Name {
		units = claudeUnits(transcript)
	}
	chosen := map[int]bool{}
	start, turns := len(units), 0
	for start > 0 && turns < recentTurns {
		start--
		if units[start].role == RoleUser || units[start].role == RoleAssistant {
			turns++
		}
	}
	size := 0
	take := func(i int) {
		n := len(units[i].text)
		if size+n > contextMax {
			return
		}
		chosen[i] = true
		size += n
	}
	for i := len(units) - 1; i >= start; i-- {
		take(i)
	}
	words := keywords(question)
	type match struct{ i, score int }
	var matches []match
	for i, u := range units {
		if chosen[i] {
			continue
		}
		if score := matchScore(u.text, words); score > 0 {
			matches = append(matches, match{i, score})
		}
	}
	slices.SortStableFunc(matches, func(a, b match) int {
		if a.score != b.score {
			return b.score - a.score
		}
		return b.i - a.i
	})
	for _, m := range matches {
		take(m.i)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Here is an excerpt of a coding session's transcript. The session ran in %s. "+
		"Answer the question from it; you have no tools, and you must not continue the session's work.\n\n<transcript>\n", folder)
	count := 0
	for i, u := range units {
		if chosen[i] {
			fmt.Fprintf(&b, "[%s] %s\n\n", u.role, u.text)
			count++
		}
	}
	fmt.Fprintf(&b, "</transcript>\n\n%sQuestion: %s", earlier, question)
	return Excerpt{Prompt: b.String(), Count: count}
}

func clip(s string, limit int) string {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > limit {
		return string(r[:limit]) + "…"
	}
	return s
}

func keywords(question string) []string {
	seen := map[string]bool{}
	var out []string
	for _, w := range strings.FieldsFunc(strings.ToLower(question), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' && r != '-' && r != '.'
	}) {
		w = strings.Trim(w, ".-")
		if len([]rune(w)) < 4 || stopWords[w] || seen[w] {
			continue
		}
		seen[w] = true
		out = append(out, w)
	}
	return out
}

func matchScore(text string, words []string) int {
	lower := strings.ToLower(text)
	score := 0
	for _, w := range words {
		if strings.Contains(lower, w) {
			score++
		}
	}
	return score
}

func eachLine(transcript []byte, read func([]byte)) {
	scanner := bufio.NewScanner(bytes.NewReader(transcript))
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		read(scanner.Bytes())
	}
}

func appendUnit(units []unit, role, text string) []unit {
	limit := unitMax
	if role == roleToolOutput {
		limit = toolOutputMax
	}
	if text = clip(text, limit); text == "" {
		return units
	}
	return append(units, unit{role: role, text: text})
}

type claudeEntry struct {
	Type        string `json:"type"`
	IsSidechain bool   `json:"isSidechain"`
	IsMeta      bool   `json:"isMeta"`
	Cwd         string `json:"cwd"`
	Message     struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

type claudeBlock struct {
	Type    string          `json:"type"`
	Text    string          `json:"text"`
	Name    string          `json:"name"`
	Input   map[string]any  `json:"input"`
	Content json.RawMessage `json:"content"`
}

func claudeUnits(transcript []byte) []unit {
	var units []unit
	cwd := ""
	eachLine(transcript, func(line []byte) {
		var e claudeEntry
		if json.Unmarshal(line, &e) != nil || e.IsSidechain || e.IsMeta || (e.Type != "user" && e.Type != "assistant") {
			return
		}
		if cwd == "" {
			cwd = e.Cwd
		}
		var text string
		if json.Unmarshal(e.Message.Content, &text) == nil {
			if e.Type == "user" && !strings.HasPrefix(strings.TrimSpace(text), "<") {
				units = appendUnit(units, RoleUser, text)
			}
			return
		}
		var blocks []claudeBlock
		if json.Unmarshal(e.Message.Content, &blocks) != nil {
			return
		}
		for _, block := range blocks {
			switch block.Type {
			case "text":
				if e.Type == "user" && strings.HasPrefix(strings.TrimSpace(block.Text), "<") {
					continue
				}
				units = appendUnit(units, e.Type, block.Text)
			case "tool_use":
				units = appendUnit(units, roleTool, strings.TrimSpace(block.Name+" "+toolSummary(block.Input, cwd)))
			case "tool_result":
				units = appendUnit(units, roleToolOutput, resultText(block.Content))
			}
		}
	})
	return units
}

func resultText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var parts []struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) != nil {
		return ""
	}
	var texts []string
	for _, p := range parts {
		texts = append(texts, p.Text)
	}
	return strings.Join(texts, "\n")
}

type rolloutLine struct {
	Type    string `json:"type"`
	Payload struct {
		Type    string `json:"type"`
		Role    string `json:"role"`
		Name    string `json:"name"`
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		Arguments string          `json:"arguments"`
		Input     string          `json:"input"`
		Output    json.RawMessage `json:"output"`
	} `json:"payload"`
}

func codexUnits(rollout []byte) []unit {
	var units []unit
	eachLine(rollout, func(raw []byte) {
		var line rolloutLine
		if json.Unmarshal(raw, &line) != nil || line.Type != "response_item" {
			return
		}
		p := line.Payload
		switch {
		case p.Type == "message" && (p.Role == RoleUser || p.Role == RoleAssistant):
			var parts []string
			for _, c := range p.Content {
				parts = append(parts, c.Text)
			}
			text := strings.TrimSpace(strings.Join(parts, "\n"))
			if p.Role == RoleUser && strings.HasPrefix(text, "<") {
				return
			}
			units = appendUnit(units, p.Role, text)
		case strings.HasSuffix(p.Type, "_call_output"):
			units = appendUnit(units, roleToolOutput, outputText(p.Output))
		case strings.HasSuffix(p.Type, "_call"):
			units = appendUnit(units, roleTool, p.Name+" "+p.Arguments+p.Input)
		}
	})
	return units
}

func outputText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		raw = json.RawMessage(s)
	}
	var v struct {
		Output string `json:"output"`
	}
	if json.Unmarshal(raw, &v) == nil && v.Output != "" {
		return v.Output
	}
	return s
}
