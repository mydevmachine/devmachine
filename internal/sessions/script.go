package sessions

import "strings"

const (
	noTmuxSentinel   = "@@NO_TMUX@@"
	nowSentinel      = "@@NOW@@"
	sessionsSentinel = "@@SESSIONS@@"
	panesSentinel    = "@@PANES@@"
	psSentinel       = "@@PS@@"
	gitSentinel      = "@@GIT@@"
)

var (
	sessionFormat = strings.Join([]string{
		"#{session_name}", "#{session_windows}", "#{session_created}", "#{session_activity}",
		"#{session_alerts}", "#{pane_current_command}", "#{pane_current_path}",
	}, "\t")
	paneFormat = strings.Join([]string{"#{session_name}", "#{pane_current_command}", "#{pane_pid}"}, "\t")
)

// Script runs under sh because the account's login shell can be fish, which
// reads none of it; the body holds no single quote so it stays one argument.
func Script() string {
	body := strings.Join([]string{
		"command -v tmux >/dev/null 2>&1 || { echo " + noTmuxSentinel + "; exit 0; }",
		"echo " + nowSentinel + "; date +%s",
		"echo " + sessionsSentinel,
		`tmux list-sessions -F "` + sessionFormat + `" 2>/dev/null`,
		"echo " + panesSentinel,
		`tmux list-panes -a -F "` + paneFormat + `" 2>/dev/null`,
		"echo " + psSentinel,
		"ps -A -o pid=,ppid=,comm= 2>/dev/null",
		"echo " + gitSentinel,
		`tmux list-panes -a -F "#{pane_current_path}" 2>/dev/null | sort -u | while IFS= read -r p; do`,
		`  b=$(git -C "$p" rev-parse --abbrev-ref HEAD 2>/dev/null) || b=`,
		`  [ "$b" = HEAD ] && b=$(git -C "$p" rev-parse --short HEAD 2>/dev/null)`,
		`  printf "%s\t%s\n" "$p" "$b"`,
		"done",
		"exit 0",
	}, "\n")
	return "sh -c '" + body + "'"
}
