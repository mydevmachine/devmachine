package provision

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/mydevmachine/devmachine/internal/remote"
)

// ErrSyncRunning is another sync holding the machine. Two runs share one
// bundle directory, and each empties it before sending its own.
var ErrSyncRunning = errors.New("another sync is running")

// bootIDCommand prints what changes on every boot: Linux's boot id, or a
// Mac's boot time. A lock recorded before the last boot has no holder left.
const bootIDCommand = "cat /proc/sys/kernel/random/boot_id 2>/dev/null || sysctl -n kern.boottime 2>/dev/null"

// lockScript takes the lock directory and holds it until its input closes.
// mkdir is the lock because it is atomic everywhere and a Mac has no flock.
// The holder's pid and boot id let a later run take over a lock whose holder
// was killed outright or lost to a reboot.
func lockScript(lock string) string {
	quoted := shellQuote(lock)
	return "lock=" + quoted + "\n" +
		"boot=$(" + bootIDCommand + ")\n" +
		"mkdir -p " + shellQuote(path.Dir(lock)) + " || exit 1\n" +
		`if ! mkdir "$lock" 2>/dev/null; then
  holder=$(cat "$lock/holder" 2>/dev/null)
  if [ -z "$holder" ]; then sleep 1; holder=$(cat "$lock/holder" 2>/dev/null); fi
  pid=${holder%% *}
  if [ -n "$holder" ] && [ "${holder#* }" = "$boot" ] && kill -0 "$pid" 2>/dev/null; then echo busy; exit 0; fi
  if [ "$(cat "$lock/holder" 2>/dev/null)" = "$holder" ]; then rm -rf "$lock"; fi
  mkdir "$lock" 2>/dev/null || { echo busy; exit 0; }
fi
trap 'rm -rf "$lock"' EXIT
trap 'exit 1' HUP INT TERM
echo "$$ $boot" > "$lock/holder"
echo locked
cat > /dev/null
`
}

// holdMachine takes the machine's sync lock and holds it through a session
// that waits on its input. Closing that input lets go, and so does a dropped
// connection: the session ends and its shell removes the lock on the way out.
func holdMachine(ctx context.Context, client remote.Client, machine, lock string, self bool) (func(), error) {
	input, hold := io.Pipe()
	answers, answer := io.Pipe()
	done := make(chan error, 1)
	go func() {
		err := remote.StreamInput(ctx, client, asRootUnlessSelf(lockScript(lock), self), input, answer, io.Discard)
		answer.CloseWithError(err)
		done <- err
	}()

	reader := bufio.NewReader(answers)
	line, err := reader.ReadString('\n')
	go func() { _, _ = io.Copy(io.Discard, reader) }()
	release := func() {
		_ = hold.Close()
		<-done
	}

	switch strings.TrimSpace(line) {
	case "locked":
		return release, nil
	case "busy":
		release()
		return nil, fmt.Errorf("%w on %s: wait for it to finish, then run this again", ErrSyncRunning, machine)
	}
	release()
	if err == nil || errors.Is(err, io.EOF) {
		err = fmt.Errorf("unexpected answer %q", line)
	}
	return nil, fmt.Errorf("taking the sync lock on %s: %w", machine, err)
}
