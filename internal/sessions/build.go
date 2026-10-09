package sessions

import (
	"path"
	"slices"
	"strings"
	"time"
)

// Session is one tmux session as `devmachine sessions` prints it.
type Session struct {
	Name           string    `json:"name"`
	Windows        int       `json:"windows"`
	Path           string    `json:"path"`
	Branch         string    `json:"branch"`
	CreatedAt      time.Time `json:"created_at"`
	AgeSeconds     int64     `json:"age_seconds"`
	LastActivityAt time.Time `json:"last_activity_at"`
	Harnesses      []string  `json:"harnesses"`
	Busy           bool      `json:"busy"`
	Attention      bool      `json:"attention"`
}

const busyWindowSeconds = 5

var harnessPrograms = map[string][]string{
	"claude":      {"claude"},
	"codex":       {"codex"},
	"pi":          {"pi"},
	"antigravity": {"agy"},
	"opencode":    {"opencode"},
	"kimi":        {"kimi", "kimi-code"},
	"cline":       {"cline", ".cline"},
	"herdr":       {"herdr"},
}

var shells = map[string]bool{"bash": true, "zsh": true, "sh": true, "fish": true, "dash": true}

// Build applies the busy, attention, branch and age rules to a snapshot.
func Build(s Snapshot) []Session {
	harnesses := harnessesBySession(s)
	out := make([]Session, 0, len(s.Sessions))
	for _, raw := range s.Sessions {
		found := harnesses[raw.Name]
		if found == nil {
			found = []string{}
		}
		out = append(out, Session{
			Name:           raw.Name,
			Windows:        raw.Windows,
			Path:           raw.Path,
			Branch:         s.Branches[raw.Path],
			CreatedAt:      time.Unix(raw.Created, 0).UTC(),
			AgeSeconds:     max(s.Now-raw.Created, 0),
			LastActivityAt: time.Unix(raw.Activity, 0).UTC(),
			Harnesses:      found,
			Busy:           busy(s.Now, raw, found),
			Attention:      strings.Contains(raw.Alerts, "!"),
		})
	}
	return out
}

func busy(now int64, raw rawSession, harnesses []string) bool {
	if len(harnesses) > 0 {
		return now-raw.Activity <= busyWindowSeconds
	}
	return !shells[raw.Command]
}

func harnessesBySession(s Snapshot) map[string][]string {
	harnessOf := map[string]string{}
	for id, programs := range harnessPrograms {
		for _, p := range programs {
			harnessOf[p] = id
		}
	}
	children := map[int][]int{}
	comm := map[int]string{}
	for _, p := range s.Procs {
		children[p.PPID] = append(children[p.PPID], p.PID)
		comm[p.PID] = p.Comm
	}

	found := map[string]map[string]bool{}
	note := func(session, name string) {
		id, ok := harnessOf[program(name)]
		if !ok {
			return
		}
		if found[session] == nil {
			found[session] = map[string]bool{}
		}
		found[session][id] = true
	}
	for _, pane := range s.Panes {
		note(pane.Session, pane.Command)
		seen := map[int]bool{}
		queue := []int{pane.PID}
		for len(queue) > 0 {
			pid := queue[0]
			queue = queue[1:]
			if seen[pid] {
				continue
			}
			seen[pid] = true
			if name, ok := comm[pid]; ok {
				note(pane.Session, name)
			}
			queue = append(queue, children[pid]...)
		}
	}

	out := map[string][]string{}
	for session, ids := range found {
		list := make([]string, 0, len(ids))
		for id := range ids {
			list = append(list, id)
		}
		slices.Sort(list)
		out[session] = list
	}
	return out
}

// program matches the app's ProcessTree.Entry.program: ps can print a full
// path, and a login shell as -zsh.
func program(comm string) string {
	head, _, _ := strings.Cut(comm, " ")
	return strings.TrimPrefix(path.Base(head), "-")
}
