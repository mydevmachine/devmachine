package provision

import (
	"context"
	"fmt"
	"io"

	"github.com/mydevmachine/devmachine/internal/packages"
)

// Options are the choices a caller makes about one run.
type Options struct {
	// Check asks for a dry run: the machine reports what would change and
	// changes nothing.
	Check bool
	// Tags narrow the run to the packages named, by the tag each one carries.
	Tags []string
	// Out is where the machine's own output is streamed as it arrives. A run
	// that is collected and printed at the end looks stuck when it is not.
	Out io.Writer
	// AnsiblePlaybook is the absolute path to call Ansible by, as a Mac's
	// bootstrap reported it. Empty means ansible-playbook from PATH.
	AnsiblePlaybook string
}

// Result is what the machine reported it did.
type Result struct {
	Changed int `json:"changed"`
	Failed  int `json:"failed"`
	Ok      int `json:"ok"`
}

// Provisioner puts a machine into the state a plan describes.
type Provisioner interface {
	Apply(ctx context.Context, plan packages.MachinePlan, opts Options) (Result, error)
}

// Summary renders what would happen, for a confirmation prompt.
//
// It is built from the plan rather than at the point of printing, so the table
// a person reads and the JSON a script reads cannot say different things.
func Summary(plan packages.MachinePlan) []string {
	var lines []string

	if len(plan.OnMachine.Ordered) > 0 {
		lines = append(lines, fmt.Sprintf("machine %s:", plan.Machine.Name))
		lines = append(lines, packageLines(plan.OnMachine)...)
	}
	for _, workspace := range plan.Workspaces {
		if len(workspace.Ordered) == 0 {
			continue
		}
		lines = append(lines, fmt.Sprintf("workspace %s (user %s):",
			workspace.Target.Name, workspace.Target.LinuxUser))
		lines = append(lines, packageLines(workspace)...)
	}

	if len(plan.Extensions) > 0 {
		lines = append(lines, "extensions:")
		for _, e := range plan.Extensions {
			lines = append(lines, fmt.Sprintf("  %s -> %s (%s)", e.From, e.Point, e.Into))
		}
	}

	if len(lines) == 0 {
		return []string{fmt.Sprintf("machine %s: nothing to apply", plan.Machine.Name)}
	}
	return lines
}

// packageLines names each package, and marks the ones whose copy came from the
// operator's own directory.
//
// Only the overridden ones are marked: a note beside every published package
// is noise, and the surprise worth catching is a local copy quietly replacing
// what everybody else runs.
func packageLines(resolved packages.Resolved) []string {
	lines := make([]string, 0, len(resolved.Ordered))
	for _, found := range resolved.Ordered {
		line := "  " + found.Manifest.Name
		if found.Source == packages.SourceLocal {
			line += " (local)"
		}
		lines = append(lines, line)
	}
	return lines
}
