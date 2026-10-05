package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/mydevmachine/devmachine/internal/release"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// noUpdateHintEnv turns the update hint off when set to anything but "0".
const noUpdateHintEnv = "DEVMACHINE_NO_UPDATE_HINT"

const (
	hintStateFile     = "update-hint.json"
	hintLookupTimeout = 3 * time.Second
)

// hintCommands are the everyday commands after which the hint may appear.
// Each one is something a person runs often and reads at a terminal.
var hintCommands = map[string]bool{
	"devmachine sync":            true,
	"devmachine doctor":          true,
	"devmachine machines list":   true,
	"devmachine workspaces list": true,
}

// hintToTerminal says whether both of a command's outputs reach a person.
// A test replaces it; its own outputs are buffers, never a terminal.
var hintToTerminal = func(cmd *cobra.Command) bool {
	return isTerminal(cmd.OutOrStdout()) && isTerminal(cmd.ErrOrStderr())
}

func isTerminal(w any) bool {
	f, ok := w.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

// hintState is what the hint remembers between runs.
type hintState struct {
	CLIVersion string    `json:"cli_version"`
	ShownAt    time.Time `json:"shown_at"`
	AskedAt    time.Time `json:"asked_at"`
}

// showUpdateHint prints one line on stderr when a newer packages release than
// the pinned one is out. It never fails the command it follows.
func showUpdateHint(cmd *cobra.Command, opts *options) {
	if !hintCommands[cmd.CommandPath()] || opts.format == formatJSON || hintTurnedOff() || !hintToTerminal(cmd) {
		return
	}
	dir, err := releaseCacheDir()
	if err != nil || dir == "" {
		return
	}
	pinned, err := pinnedRelease(opts)
	if err != nil || pinned == "" {
		return
	}
	if line := packagesHint(cmd.Context(), dir, pinned); line != "" {
		fmt.Fprintln(cmd.ErrOrStderr(), line)
	}
}

func hintTurnedOff() bool {
	value := os.Getenv(noUpdateHintEnv)
	return value != "" && value != "0"
}

// packagesHint is the hint line, or "" when there is nothing to say or it
// was said in the last day. A new CLI version says it again at once: that is
// how somebody who upgraded with `brew upgrade` hears about the packages.
//
// GitHub is asked at most once a day, and the state is written before asking:
// a cache folder that cannot be written means never asking, not asking on
// every command.
func packagesHint(ctx context.Context, dir, pinned string) string {
	now := releaseClock()
	state := readHintState(dir)
	newCLI := state.CLIVersion != version
	if !newCLI && now.Sub(state.ShownAt) < packagesCheckTTL {
		return ""
	}
	state.CLIVersion = version

	cache := releaseCacheFor(packagesCheckTTL)
	latest, checkedAt, known := cache.Stored(cacheKeyPackages)
	if (!known || now.Sub(checkedAt) >= packagesCheckTTL) && now.Sub(state.AskedAt) >= packagesCheckTTL {
		state.AskedAt = now
		if writeHintState(dir, state) != nil {
			return ""
		}
		lookup, cancel := context.WithTimeout(ctx, hintLookupTimeout)
		if fresh, err := cache.Refresh(lookup, cacheKeyPackages, fetchLatestPackages); err == nil {
			latest, known = fresh, true
		}
		cancel()
	}

	line := ""
	if known && release.Newer(latest, pinned) {
		line = packagesHintLine(latest, pinned)
		state.ShownAt = now
	}
	_ = writeHintState(dir, state)
	return line
}

func readHintState(dir string) hintState {
	var state hintState
	body, err := os.ReadFile(filepath.Join(dir, hintStateFile))
	if err != nil {
		return state
	}
	_ = json.Unmarshal(body, &state)
	return state
}

func writeHintState(dir string, state hintState) error {
	body, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("rendering the update hint state: %w", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating the cache folder: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, hintStateFile), body, 0o600); err != nil {
		return fmt.Errorf("writing the update hint state: %w", err)
	}
	return nil
}
