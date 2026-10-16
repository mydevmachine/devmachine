package commands

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mydevmachine/devmachine/internal/config"
	"github.com/mydevmachine/devmachine/internal/remote"
	"github.com/mydevmachine/devmachine/internal/scheduled"
)

// scheduledBinary is the devmachine the scheduled package links for the
// account. A fixed path, because the Mac reaches the workspace through a
// shell that has not read the account's profile.
const scheduledBinary = `"$HOME/.devmachine/scheduled/devmachine"`

// missingBinaryCheck gives a missing binary one exit status on every shell:
// bash's exec says 126 for it, and dash and zsh say 127.
const missingBinaryCheck = "[ -x " + scheduledBinary + " ] || exit 127\n"

// taskHeredoc ends the task JSON in the script. json.Marshal escapes every
// newline, so no line of the JSON can ever equal it.
const taskHeredoc = "DEVMACHINE_TASK_EOF"

const (
	scheduledListTimeout = 15 * time.Second
	scheduledCallTimeout = 60 * time.Second
)

// scheduledScript is the program /bin/sh reads on its input. Going through
// stdin keeps every word away from the account's login shell, which may be
// fish and quote differently.
func scheduledScript(args []string, stdin []byte) string {
	line := missingBinaryCheck + "exec " + scheduledBinary + " scheduled local " + shellWords(args)
	if stdin == nil {
		return line + " </dev/null\n"
	}
	return line + " <<'" + taskHeredoc + "'\n" + string(stdin) + "\n" + taskHeredoc + "\n"
}

// callWorkspace runs one `scheduled local` operation in w and decodes its
// reply into v. Every failure comes back as a *scheduledError.
func callWorkspace(ctx context.Context, cfg config.Config, w config.Workspace, timeout time.Duration,
	args []string, stdin []byte, v any) error {
	out, err := runWorkspaceScript(ctx, cfg, w, timeout, scheduledScript(args, stdin))
	if err != nil && out == "" {
		return classifyScheduledError(cfg, w, err)
	}
	return decodeScheduledReply(cfg, w, out, err, v)
}

func runWorkspaceScript(ctx context.Context, cfg config.Config, w config.Workspace, timeout time.Duration, script string) (string, error) {
	m, _, err := cfg.MachineFor(w.Name)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	client, _, err := dialMux(ctx, m, w.LinuxUser())
	if err != nil {
		return "", err
	}
	defer client.Close()
	return client.RunInput(ctx, remote.ShellOnInput, strings.NewReader(script))
}

func decodeScheduledReply(cfg config.Config, w config.Workspace, out string, runErr error, v any) error {
	var envelope struct {
		Protocol int             `json:"protocol"`
		Error    *scheduledError `json:"error"`
	}
	if json.Unmarshal([]byte(out), &envelope) != nil || envelope.Protocol == 0 {
		if runErr != nil {
			if binaryMissing(runErr) {
				return scheduledMissing(cfg, w)
			}
		}
		return scheduledOutdated(cfg, w)
	}
	if envelope.Protocol != scheduled.Protocol {
		return scheduledOutdated(cfg, w)
	}
	if envelope.Error != nil {
		return envelope.Error
	}
	if runErr != nil {
		return &scheduledError{Kind: "other", Message: runErr.Error()}
	}
	if err := json.Unmarshal([]byte(out), v); err != nil {
		return &scheduledError{Kind: "other", Message: fmt.Sprintf("reading the reply of %s: %v", w.Name, err)}
	}
	return nil
}

func classifyScheduledError(cfg config.Config, w config.Workspace, err error) *scheduledError {
	if binaryMissing(err) {
		return scheduledMissing(cfg, w)
	}
	kind := "other"
	switch {
	case errors.Is(err, remote.ErrHostKeyRejected):
		kind = "host-key-rejected"
	case errors.Is(err, remote.ErrAuthRefused):
		kind = "login-refused"
	case errors.Is(err, context.DeadlineExceeded), strings.Contains(err.Error(), "no address answered"):
		kind = "unreachable"
	}
	return &scheduledError{Kind: kind, Message: err.Error()}
}

func binaryMissing(err error) bool {
	code, ok := commandExitCode(err)
	return ok && (code == 126 || code == 127)
}

// scheduledMissing is the error for a workspace whose binary is not there.
// With the package in the configuration, the setup sync was cut short or
// failed, and syncing again finishes it.
func scheduledMissing(cfg config.Config, w config.Workspace) *scheduledError {
	if !isScheduledInstalled(w) {
		return scheduledNotInstalled(cfg, w)
	}
	return scheduledSetupIncomplete(cfg, w)
}

func scheduledSetupIncomplete(cfg config.Config, w config.Workspace) *scheduledError {
	machine := machineNameOf(cfg, w)
	return &scheduledError{Kind: "setup-incomplete", Message: fmt.Sprintf(
		"scheduled's setup on %s did not finish: the package is on the workspace, but its devmachine is not on %s. "+
			"Run devmachine sync --machine %s --tags scheduled, or add a task with --yes",
		w.Name, machine, machine)}
}

func scheduledNotInstalled(cfg config.Config, w config.Workspace) *scheduledError {
	return &scheduledError{Kind: "not-installed", Message: fmt.Sprintf(
		"scheduled is not set up on %s: add a task with --yes to set it up, or run "+
			"devmachine packages add scheduled --workspace %s --yes && devmachine sync --machine %s --tags scheduled --yes",
		w.Name, w.Name, machineNameOf(cfg, w))}
}

func scheduledOutdated(cfg config.Config, w config.Workspace) *scheduledError {
	return &scheduledError{Kind: "outdated", Message: fmt.Sprintf(
		"the devmachine on %s is older than this one: run devmachine sync --machine %s --tags scheduled",
		w.Name, machineNameOf(cfg, w))}
}
