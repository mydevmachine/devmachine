package remote

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"strings"
)

// MacBootstrapDir is where setup copies a package's directory on a Mac
// before it runs the package's bootstrap there.
const MacBootstrapDir = "/opt/devmachine/bootstrap"

// MacManagersCommand prints "brew" when Homebrew is on the Mac and "ports"
// when MacPorts is. It looks at absolute paths: a plain SSH command gets
// PATH=/usr/bin:/bin:/usr/sbin:/sbin, where neither is.
const MacManagersCommand = `for f in /opt/homebrew/bin/brew /usr/local/bin/brew; do
	if [ -x "$f" ]; then echo brew; break; fi
done
if [ -x /opt/local/bin/port ]; then echo ports; fi`

// Managers says which package managers a Mac already has.
type Managers struct {
	Brew, Ports bool
}

// MacManagers asks the Mac which package managers it has. It only reads.
func MacManagers(ctx context.Context, c Client) (Managers, error) {
	out, err := c.Run(ctx, MacManagersCommand)
	if err != nil {
		return Managers{}, fmt.Errorf("looking for Homebrew and MacPorts: %w", err)
	}
	var m Managers
	for _, line := range strings.Fields(out) {
		switch line {
		case "brew":
			m.Brew = true
		case "ports":
			m.Ports = true
		}
	}
	return m, nil
}

// Prerequisite is one thing a bootstrap would install, and about how long it
// takes.
type Prerequisite struct {
	Name    string `json:"name"`
	Minutes int    `json:"minutes"`
}

// Prepared is what a bootstrap's apply reports: the ansible-playbook to call
// by its absolute path, and the folders to put first on PATH.
type Prepared struct {
	AnsiblePlaybook string   `json:"ansible_playbook"`
	PathPrefix      []string `json:"path_prefix"`
}

// BootstrapError is a bootstrap that stopped and said where and why.
type BootstrapError struct {
	Step    string `json:"step"`
	Message string `json:"message"`
}

func (e *BootstrapError) Error() string {
	return fmt.Sprintf("the bootstrap stopped at %s: %s", e.Step, e.Message)
}

// Bootstrap is a package's bootstrap script as a machine runs it: by its path
// on the machine, or, when Path is empty, piped to sh, which leaves nothing
// behind. Only check runs from a body: apply changes the machine, so it runs
// from a file the person can read afterwards.
type Bootstrap struct {
	Path string
	Body []byte
}

// Command is the shell command that runs one action. The script is run
// through sh so it needs no execute bit, which a copy does not always keep.
func (b Bootstrap) Command(action string) string {
	if b.Path == "" {
		return "sh -s " + action
	}
	return "sh " + shellQuote(b.Path) + " " + action
}

// Check lists what the machine lacks. It changes nothing.
func (b Bootstrap) Check(ctx context.Context, c Client) ([]Prerequisite, error) {
	var (
		out string
		err error
	)
	if b.Path == "" {
		out, err = c.RunInput(ctx, b.Command("check"), bytes.NewReader(b.Body))
	} else {
		out, err = c.Run(ctx, b.Command("check"))
	}
	var answer struct {
		Missing *[]Prerequisite `json:"missing"`
	}
	if err := readBootstrapOutput("check", out, err, &answer); err != nil {
		return nil, err
	}
	if answer.Missing == nil {
		return nil, fmt.Errorf("the bootstrap's check printed no list of what is missing: %s", strings.TrimSpace(out))
	}
	return *answer.Missing, nil
}

// Apply installs what is missing. Its progress reaches progress as it
// arrives, since installing the Command Line Tools alone takes minutes.
func (b Bootstrap) Apply(ctx context.Context, c Client, progress io.Writer) (Prepared, error) {
	if b.Path == "" {
		return Prepared{}, fmt.Errorf("the bootstrap's apply runs from a file on the machine, and none was given")
	}
	var stdout bytes.Buffer
	err := c.Stream(ctx, b.Command("apply"), &stdout, progress)
	var prepared Prepared
	if err := readBootstrapOutput("apply", stdout.String(), err, &prepared); err != nil {
		return Prepared{}, err
	}
	if !path.IsAbs(prepared.AnsiblePlaybook) {
		return Prepared{}, fmt.Errorf("the bootstrap reported %q as ansible-playbook, which is not an absolute path",
			prepared.AnsiblePlaybook)
	}
	return prepared, nil
}

// readBootstrapOutput reads the one JSON document a bootstrap prints on
// stdout. A failure's own words win over the exit status: they say what to do.
func readBootstrapOutput(action, out string, runErr error, into any) error {
	var failure struct {
		Error *BootstrapError `json:"error"`
	}
	body := strings.TrimSpace(out)
	if json.Unmarshal([]byte(body), &failure) == nil && failure.Error != nil {
		return failure.Error
	}
	if runErr != nil {
		return fmt.Errorf("running the bootstrap's %s: %w", action, runErr)
	}
	if err := json.Unmarshal([]byte(body), into); err != nil {
		return fmt.Errorf("reading what the bootstrap's %s printed (%q): %w", action, body, err)
	}
	return nil
}
