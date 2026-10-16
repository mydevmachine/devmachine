// Package chattest holds a fake claude and a fake codex for tests: shell
// scripts that print the harnesses' stream formats and never reach a model.
package chattest

import (
	"os"
	"path/filepath"
	"testing"
)

// fakeClaude is a stand-in for the claude binary: it records its arguments
// next to itself and prints a fixture stream, choosing it by FAKE_MODE in a
// file beside it, so the test drives it without its environment.
const claudeScript = `#!/bin/sh
here=$(cd "$(dirname "$0")" && pwd)
for a in "$@"; do printf '%s\n' "$a"; done > "$here/args.$$"
mv "$here/args.$$" "$here/args"
pwd > "$here/cwd"
mode=$(cat "$here/mode" 2>/dev/null)
sid=22222222-aaaa-4bbb-8ccc-000000000001
prev=""
for a in "$@"; do
  [ "$prev" = "--resume" ] && sid="$a"
  [ "$a" = "--fork-session" ] && sid=33333333-aaaa-4bbb-8ccc-000000000001
  prev="$a"
done
case "$mode" in
  missing-login) echo "Invalid API key · Please run /login" >&2; exit 1 ;;
  crash) echo "segmentation fault" >&2; exit 139 ;;
  slow) echo '{"type":"system","subtype":"init","session_id":"'$sid'","cwd":"/tmp"}'; sleep 30; exit 0 ;;
esac
echo '{"type":"system","subtype":"init","session_id":"'$sid'","cwd":"'"$(pwd)"'"}'
echo '{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"echo: "}},"parent_tool_use_id":null}'
last=""
for a in "$@"; do last="$a"; done
msg=$(printf '%s' "$last" | sed 's/\\/\\\\/g; s/"/\\"/g' | tr '\n' ' ')
printf '%s\n' '{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"'"$msg"'"}},"parent_tool_use_id":null}'
echo '{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Read","input":{"file_path":"notes.md"}}]},"parent_tool_use_id":null}'
if [ "$mode" = "fail" ]; then
  echo '{"type":"result","is_error":true,"result":"API Error: 529 overloaded","session_id":"'$sid'"}'
  exit 1
fi
echo '{"type":"result","is_error":false,"result":"done","session_id":"'$sid'"}'
`

const codexScript = `#!/bin/sh
here=$(cd "$(dirname "$0")" && pwd)
for a in "$@"; do printf '%s\n' "$a"; done > "$here/args.$$"
mv "$here/args.$$" "$here/args"
if read -r line; then echo "stdin was open" >&2; exit 9; fi
tid=0199aaaa-bbbb-7ccc-8ddd-000000000009
prev=""
for a in "$@"; do [ "$prev" = "resume" ] && tid="$a"; prev="$a"; done
echo '{"type":"thread.started","thread_id":"'$tid'"}'
echo '{"type":"item.completed","item":{"type":"agent_message","text":"codex says hi"}}'
echo '{"type":"turn.completed","usage":{}}'
`

// Bin is a folder holding the fake harnesses.
type Bin struct{ Dir string }

// Install writes the fake harnesses into a new folder.
func Install(t *testing.T) Bin {
	t.Helper()
	dir := t.TempDir()
	for name, body := range map[string]string{"claude": claudeScript, "codex": codexScript} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return Bin{Dir: dir}
}

// Mode makes the next runs answer another way: missing-login, crash, slow
// or fail.
func (f Bin) Mode(t *testing.T, mode string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(f.Dir, "mode"), []byte(mode), 0o600); err != nil {
		t.Fatal(err)
	}
}

// Read is a file the fakes wrote: args (one argument per line) or cwd.
func (f Bin) Read(t *testing.T, name string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(f.Dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

// Isolate keeps a run away from the real home and login shell.
func Isolate(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SHELL", "/bin/sh")
	return home
}
