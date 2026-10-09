package sessions

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Snapshot is what Script printed, split into its sections.
type Snapshot struct {
	Now      int64
	NoTmux   bool
	Sessions []rawSession
	Panes    []rawPane
	Procs    []proc
	Branches map[string]string
}

type rawSession struct {
	Name              string
	Windows           int
	Created, Activity int64
	Alerts, Command   string
	Path              string
}

type rawPane struct {
	Session, Command string
	PID              int
}

type proc struct {
	PID, PPID int
	Comm      string
}

var errNoClock = errors.New("missing machine clock")

// Parse reads Script's output; it fails only when the machine clock is missing.
func Parse(output string) (Snapshot, error) {
	snap := Snapshot{Branches: map[string]string{}}
	section := ""
	haveNow := false
	for _, line := range strings.Split(output, "\n") {
		switch line {
		case noTmuxSentinel:
			return Snapshot{NoTmux: true}, nil
		case nowSentinel, sessionsSentinel, panesSentinel, psSentinel, gitSentinel:
			section = line
			continue
		}
		if line == "" {
			continue
		}
		switch section {
		case nowSentinel:
			now, err := strconv.ParseInt(strings.TrimSpace(line), 10, 64)
			if err != nil {
				return Snapshot{}, fmt.Errorf("reading machine clock %q: %w", line, err)
			}
			snap.Now, haveNow = now, true
		case sessionsSentinel:
			if s, ok := parseSession(line); ok {
				snap.Sessions = append(snap.Sessions, s)
			}
		case panesSentinel:
			if p, ok := parsePane(line); ok {
				snap.Panes = append(snap.Panes, p)
			}
		case psSentinel:
			if p, ok := parseProc(line); ok {
				snap.Procs = append(snap.Procs, p)
			}
		case gitSentinel:
			if fields := strings.Split(line, "\t"); len(fields) == 2 {
				snap.Branches[fields[0]] = fields[1]
			}
		}
	}
	if !haveNow {
		return Snapshot{}, errNoClock
	}
	return snap, nil
}

func parseSession(line string) (rawSession, bool) {
	f := strings.Split(line, "\t")
	if len(f) != 7 {
		return rawSession{}, false
	}
	windows, errWindows := strconv.Atoi(f[1])
	created, errCreated := strconv.ParseInt(f[2], 10, 64)
	activity, errActivity := strconv.ParseInt(f[3], 10, 64)
	if errors.Join(errWindows, errCreated, errActivity) != nil {
		return rawSession{}, false
	}
	return rawSession{
		Name: f[0], Windows: windows, Created: created, Activity: activity,
		Alerts: f[4], Command: f[5], Path: f[6],
	}, true
}

func parsePane(line string) (rawPane, bool) {
	f := strings.Split(line, "\t")
	if len(f) != 3 {
		return rawPane{}, false
	}
	pid, err := strconv.Atoi(f[2])
	if err != nil {
		return rawPane{}, false
	}
	return rawPane{Session: f[0], Command: f[1], PID: pid}, true
}

func parseProc(line string) (proc, bool) {
	f := strings.Fields(line)
	if len(f) < 3 {
		return proc{}, false
	}
	pid, errPID := strconv.Atoi(f[0])
	ppid, errPPID := strconv.Atoi(f[1])
	if errPID != nil || errPPID != nil {
		return proc{}, false
	}
	return proc{PID: pid, PPID: ppid, Comm: strings.Join(f[2:], " ")}, true
}
