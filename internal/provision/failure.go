package provision

import (
	"encoding/json"
	"regexp"
	"strings"
)

// FailedTask is the task that stopped a run, as the default callback prints
// it.
type FailedTask struct {
	Task    string
	Message string
}

var failedLine = regexp.MustCompile(`^(?:fatal|failed): \[[^\]]+\].*?=> (.*)$`)

const maxFailureMessage = 400

// readFailure finds the first task that failed and was not ignored. Its
// message is what a person needs: the recap only counts failures, and the
// task that failed can be hundreds of lines above it.
func readFailure(output string) (FailedTask, bool) {
	lines := strings.Split(output, "\n")
	task := ""
	for i, line := range lines {
		line = strings.TrimRight(line, "\r")
		if m := taskHeader.FindStringSubmatch(line); m != nil {
			task = m[1]
			continue
		}
		m := failedLine.FindStringSubmatch(line)
		if m == nil || ignored(lines[i+1:]) {
			continue
		}
		return FailedTask{Task: task, Message: failureMessage(m[1])}, true
	}
	return FailedTask{}, false
}

func ignored(rest []string) bool {
	for _, line := range rest {
		if line = strings.TrimSpace(line); line != "" {
			return line == "...ignoring"
		}
	}
	return false
}

func failureMessage(result string) string {
	var fields struct {
		Msg    any    `json:"msg"`
		Stderr string `json:"stderr"`
	}
	message := result
	if json.Unmarshal([]byte(result), &fields) == nil {
		switch msg := fields.Msg.(type) {
		case string:
			message = msg
		case nil:
			if fields.Stderr != "" {
				message = fields.Stderr
			}
		default:
			if data, err := json.Marshal(msg); err == nil {
				message = string(data)
			}
		}
	}
	message = strings.Join(strings.Fields(message), " ")
	if runes := []rune(message); len(runes) > maxFailureMessage {
		message = string(runes[:maxFailureMessage]) + "…"
	}
	return message
}
