// Package harness knows the coding agents a prompt can run in: their
// binaries, permission modes, suggested models, and how to build the argv of
// a run that asks nobody anything.
package harness

import (
	"fmt"
	"slices"
	"sort"
)

// Harness is one coding agent. Models are suggestions; an empty model means
// the agent's own default, and any other name is passed through.
type Harness struct {
	Name            string
	Binary          string
	Package         string
	PermissionModes []string
	Dangerous       []string
	Models          []string
	argv            func(prompt, model, mode string) []string
}

var codexFlagModes = []string{"approve-for-me", "dangerously-bypass-approvals-and-sandbox"}

var registry = map[string]Harness{
	"claude": {
		Name:            "claude",
		Binary:          "claude",
		Package:         "claude-code",
		PermissionModes: []string{"manual", "dontAsk", "plan", "acceptEdits", "auto", "bypassPermissions"},
		Dangerous:       []string{"bypassPermissions"},
		Models:          []string{"fable", "opus", "sonnet", "haiku"},
		argv: func(prompt, model, mode string) []string {
			a := []string{"claude", "-p"}
			if model != "" {
				a = append(a, "--model", model)
			}
			if mode != "" {
				a = append(a, "--permission-mode", mode)
			}
			return append(a, "--", prompt)
		},
	},
	"codex": {
		Name:            "codex",
		Binary:          "codex",
		Package:         "codex",
		PermissionModes: []string{"read-only", "workspace-write", "danger-full-access", "approve-for-me", "dangerously-bypass-approvals-and-sandbox"},
		Dangerous:       []string{"danger-full-access", "dangerously-bypass-approvals-and-sandbox"},
		Models:          []string{},
		argv: func(prompt, model, mode string) []string {
			a := []string{"codex", "exec", "--skip-git-repo-check"}
			if model != "" {
				a = append(a, "-m", model)
			}
			switch {
			case mode == "":
			case slices.Contains(codexFlagModes, mode):
				a = append(a, "--"+mode)
			default:
				a = append(a, "--sandbox", mode)
			}
			return append(a, "--", prompt)
		},
	},
}

// Get finds a harness by name.
func Get(name string) (Harness, bool) {
	h, ok := registry[name]
	return h, ok
}

// Names lists every harness, sorted.
func Names() []string {
	names := make([]string, 0, len(registry))
	for n := range registry {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// Argv builds the command that runs prompt with no questions asked. The
// prompt is one argv word, so no shell ever reads it.
func (h Harness) Argv(prompt, model, mode string) ([]string, error) {
	if mode != "" && !slices.Contains(h.PermissionModes, mode) {
		return nil, fmt.Errorf("%s has no permission mode %q; it has %v", h.Name, mode, h.PermissionModes)
	}
	return h.argv(prompt, model, mode), nil
}

// IsDangerous says whether mode lets the agent act with no approval at all.
func (h Harness) IsDangerous(mode string) bool { return slices.Contains(h.Dangerous, mode) }

// ModesByHarness is every harness's permission modes, keyed by name.
func ModesByHarness() map[string][]string {
	m := map[string][]string{}
	for n, h := range registry {
		m[n] = slices.Clone(h.PermissionModes)
	}
	return m
}

// AllDangerous is every harness's dangerous modes, in harness order.
func AllDangerous() []string {
	var d []string
	for _, n := range Names() {
		d = append(d, registry[n].Dangerous...)
	}
	return d
}
