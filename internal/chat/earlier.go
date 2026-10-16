package chat

import "strings"

const (
	earlierPairs = 6
	earlierMax   = 8 * 1024
)

// Earlier is the side conversation so far, for a question that starts a new
// fork or a new codex: the last answered questions, newest kept first when
// the size runs out. Empty when nothing was answered yet.
func Earlier(messages []Message) string {
	var pairs []string
	for i := 0; i+1 < len(messages); i++ {
		q, a := messages[i], messages[i+1]
		if q.Role != RoleUser || a.Role != RoleAssistant || strings.TrimSpace(a.Text) == "" {
			continue
		}
		pairs = append(pairs, "Q: "+strings.TrimSpace(q.Text)+"\nA: "+strings.TrimSpace(a.Text)+"\n\n")
		i++
	}
	const heading = "Earlier in this side conversation:\n\n"
	var kept []string
	size := len(heading)
	for i := len(pairs) - 1; i >= 0 && len(kept) < earlierPairs; i-- {
		if size+len(pairs[i]) > earlierMax {
			break
		}
		size += len(pairs[i])
		kept = append([]string{pairs[i]}, kept...)
	}
	if len(kept) == 0 {
		return ""
	}
	return heading + strings.Join(kept, "")
}
