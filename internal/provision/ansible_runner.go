package provision

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/mydevmachine/devmachine/internal/packages"
	"github.com/mydevmachine/devmachine/internal/remote"
)

// Ansible provisions a machine by running ansible-playbook on it.
//
// Running it there means the operator's computer needs only this binary and
// ssh, and a run costs no round trip per task.
type Ansible struct {
	Client remote.Client
}

// Apply sends everything the plan needs and runs it.
//
// The machine's output is streamed as it arrives and copied at the same time,
// so the recap is read from what the person already watched rather than from a
// second run.
func (a *Ansible) Apply(ctx context.Context, plan packages.MachinePlan, opts Options) (Result, error) {
	base, err := Base(plan.Machine)
	if err != nil {
		return Result{}, err
	}

	files, err := GenerateAt(plan, base)
	if err != nil {
		return Result{}, err
	}

	tarball, err := Tar(files, roleDirs(plan))
	if err != nil {
		return Result{}, err
	}
	if base == "" || base == "/" {
		return Result{}, fmt.Errorf("refusing to empty %q as the bundle directory", base)
	}
	self := plan.Machine.Self
	if !self {
		if err := remote.CheckRoot(ctx, a.Client, plan.Machine.User); err != nil {
			return Result{}, err
		}
	}
	release, err := holdMachine(ctx, a.Client, plan.Machine.Name, base+".lock", self)
	if err != nil {
		return Result{}, err
	}
	defer release()
	// Unpacking never deletes, and Ansible searches roles.local before roles:
	// a copy an earlier sync left would keep running instead of this one.
	if _, err := a.Client.Run(ctx, asRootUnlessSelf("rm -rf "+base, self)); err != nil {
		return Result{}, fmt.Errorf("emptying %s: %w", base, err)
	}
	if err := a.Client.Upload(ctx, base, tarball); err != nil {
		return Result{}, err
	}

	out := opts.Out
	if out == nil {
		out = io.Discard
	}
	var seen bytes.Buffer
	watched := io.MultiWriter(&seen, out)

	command, err := playbookCommand(opts, base, self)
	if err != nil {
		return Result{}, err
	}
	// The whole run is root's, not only the tasks that ask for become: a task
	// that becomes a workspace account would otherwise go from one
	// unprivileged account to another, which Ansible refuses without ACLs on
	// its temporary files.
	command = asRootUnlessSelf(command, self)
	runErr := a.Client.Stream(ctx, command, watched, watched)
	result := readRecap(seen.String())

	if runErr != nil {
		if result.Failed > 0 {
			return result, fmt.Errorf("%d task(s) failed on %s: %w", result.Failed, plan.Machine.Name, runErr)
		}
		return result, runErr
	}
	return result, nil
}

// asRootUnlessSelf runs a command as root on a machine reached over SSH,
// whatever account the admin login is. A self machine is the operator's own
// computer: its bundle is theirs and its play runs without become.
func asRootUnlessSelf(command string, self bool) string {
	if self {
		return command
	}
	return remote.AsRoot(command)
}

// roleDirs maps each package in the plan onto the directory it is unpacked
// into, which is what decides whether the operator's copy or the published one
// runs.
//
// Only the packages the plan names are sent. Having a recipe in the cache that
// nothing asks for is the ordinary state, and sending the whole cache to every
// machine would make that state cost bandwidth and confusion.
func roleDirs(plan packages.MachinePlan) map[string]string {
	dirs := map[string]string{}
	for _, resolved := range append([]packages.Resolved{plan.OnMachine}, plan.Workspaces...) {
		for _, found := range resolved.Ordered {
			dirs[path.Join(rolesDir(found.Source), found.Manifest.Name)] = found.Manifest.Path
		}
	}
	return dirs
}

func playbookCommand(opts Options, base string, self bool) (string, error) {
	// Ansible searches a playbook-adjacent directory named roles before the
	// configured roles_path. Running a copied playbook from a fresh directory
	// keeps base/roles from silently beating base/roles.local.
	playbook := "ansible-playbook"
	if opts.AnsiblePlaybook != "" {
		playbook = shellQuote(opts.AnsiblePlaybook)
	}
	command := fmt.Sprintf("run=$(mktemp -d) && trap 'rm -rf \"$run\"' EXIT && "+
		"cp %[1]s/site.yml \"$run/site.yml\" && cd \"$run\" && "+
		"ANSIBLE_CONFIG=%[1]s/ansible.cfg %[2]s -i %[1]s/inventory.ini site.yml",
		base, playbook)
	if opts.Check {
		command += " --check"
	}
	if len(opts.Tags) > 0 {
		command += " --tags " + strings.Join(opts.Tags, ",")
	}
	if self {
		exported, err := selfEnvExport()
		if err != nil {
			return "", err
		}
		command = exported + " && " + command
	}
	return command, nil
}

// selfEnvExport exports USER, LOGNAME and HOME the way a self machine's own
// login shell would, ahead of the ansible-playbook run. The exec.Command that
// eventually runs this inherits devmachine's own environment, which is not
// always right — see inventory's comment on ansible_user.
func selfEnvExport() (string, error) {
	name, home, err := selfIdentity()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("export USER=%s LOGNAME=%s HOME=%s", shellQuote(name), shellQuote(name), shellQuote(home)), nil
}

// shellQuote wraps a value so the remote shell takes it as one word, matching
// remote.shellQuote: that one is not exported, and this package has no reason
// to import remote just for it.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// recapLine matches the play recap, which is the only line that opens with a
// host name and then a run of key=number counters.
var recapLine = regexp.MustCompile(`(?m)^\S+\s*:\s+(ok=\d+.*)$`)

var counter = regexp.MustCompile(`(\w+)=(\d+)`)

// readRecap turns the recap into counts. A run with no recap at all — one that
// died before the play started — leaves a zero Result, and the error from the
// run is what says what happened.
func readRecap(output string) Result {
	matches := recapLine.FindAllStringSubmatch(output, -1)
	if len(matches) == 0 {
		return Result{}
	}

	var result Result
	for _, field := range counter.FindAllStringSubmatch(matches[len(matches)-1][1], -1) {
		value, err := strconv.Atoi(field[2])
		if err != nil {
			continue
		}
		switch field[1] {
		case "ok":
			result.Ok = value
		case "changed":
			result.Changed = value
		case "failed":
			result.Failed = value
		}
	}
	return result
}
