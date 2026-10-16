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

// The limits of the context block an ask on a Codex session sends.
const (
	recentTurns     = 6
	excerptUnitMax  = 2000
	excerptTotalMax = 24 * 1024
	TranscriptLimit = 8 * 1024 * 1024
)

type unit struct {
	role, text string
}

// Excerpt is the context block built from a Codex transcript.
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

// BuildExcerpt reads a Codex rollout and builds the prompt that asks the
// question about it: the last turns, plus the lines that share a word with
// the question, within a fixed size.
func BuildExcerpt(rollout []byte, folder, question string) Excerpt {
	units := rolloutUnits(rollout)
	chosen := map[int]bool{}
	var turns []int
	for i := len(units) - 1; i >= 0 && len(turns) < recentTurns; i-- {
		if units[i].role == RoleUser || units[i].role == RoleAssistant {
			turns = append(turns, i)
		}
	}
	size := 0
	take := func(i int) bool {
		n := len(clip(units[i].text))
		if size+n > excerptTotalMax {
			return false
		}
		chosen[i] = true
		size += n
		return true
	}
	for _, i := range turns {
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
	fmt.Fprintf(&b, "Here is a coding session transcript excerpt. The session ran in %s. "+
		"It is context only: answer the question about it, and do not continue the session's work.\n\n<transcript>\n", folder)
	count := 0
	for i, u := range units {
		if chosen[i] {
			fmt.Fprintf(&b, "[%s] %s\n\n", u.role, clip(u.text))
			count++
		}
	}
	fmt.Fprintf(&b, "</transcript>\n\nQuestion: %s", question)
	return Excerpt{Prompt: b.String(), Count: count}
}

func clip(s string) string {
	if r := []rune(s); len(r) > excerptUnitMax {
		return string(r[:excerptUnitMax]) + "…"
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

func rolloutUnits(rollout []byte) []unit {
	var units []unit
	scanner := bufio.NewScanner(bytes.NewReader(rollout))
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		var line rolloutLine
		if json.Unmarshal(scanner.Bytes(), &line) != nil || line.Type != "response_item" {
			continue
		}
		p := line.Payload
		var role, text string
		switch {
		case p.Type == "message" && (p.Role == RoleUser || p.Role == RoleAssistant):
			var parts []string
			for _, c := range p.Content {
				parts = append(parts, c.Text)
			}
			role, text = p.Role, strings.TrimSpace(strings.Join(parts, "\n"))
			if role == RoleUser && strings.HasPrefix(text, "<") {
				continue
			}
		case strings.HasSuffix(p.Type, "_call_output"):
			role, text = "tool output", outputText(p.Output)
		case strings.HasSuffix(p.Type, "_call"):
			role, text = "tool", strings.TrimSpace(p.Name+" "+p.Arguments+p.Input)
		default:
			continue
		}
		if text != "" {
			units = append(units, unit{role: role, text: text})
		}
	}
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
		return strings.TrimSpace(v.Output)
	}
	return strings.TrimSpace(s)
}
