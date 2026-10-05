// Package local creates a machine on your computer.
//
// It exists so the CLI can be used, and tested, without buying a server. What
// comes back is an ordinary machine in the model, and everything after that is
// the same code a real server gets.
//
// A local machine is created the way a bought one arrives: root reachable over
// SSH with a password and no key installed. Installing a key here would mean
// the trust bootstrap is never exercised by the thing that runs on every test,
// which is the one path a stranger cannot avoid.
package local

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/mydevmachine/devmachine/internal/config"
)

// limactl is the only thing this package shells out to.
const limactl = "limactl"

// Password is the root password a local machine is created with.
//
// It is public on purpose: the VM sits behind your computer's NAT, holds no
// data, and exists to be destroyed.
const Password = "devmachine"

// Address is where a local machine answers. Lima forwards a port on the
// loopback to the VM's sshd, and that forward is the whole address.
const Address = "127.0.0.1"

// AdminUser is the account the bootstrap logs in as.
const AdminUser = "root"

//go:embed machine.yaml
var machineTemplate []byte

// Template is the Lima configuration a local machine of that distro is
// created from.
//
// It is the one recipe carried inside the binary, because it describes the
// CLI's own machine rather than anything on a server.
func Template(d Distro) []byte { return templates[d] }

// Seams, so the tests can drive every branch without a VM.
var (
	realLookPath = exec.LookPath
	lookPath     = realLookPath
	realRun      = runLimactl
	run          = realRun
)

// name is what Lima accepts for an instance. Checking it here turns a name
// Lima refuses halfway through a boot into an error before anything starts —
// and stops a value that begins with a dash from arriving as a flag.
var name = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// Available reports whether Lima is installed, and says how to get it.
func Available() error {
	if _, err := lookPath(limactl); err != nil {
		return fmt.Errorf("lima is not installed: install it with `brew install lima`")
	}
	return nil
}

// Create starts a machine on your computer and returns it as a machine the
// rest of the CLI already knows how to use.
//
// Progress goes to out as it arrives: the first run downloads an image and
// boots it, which looks stuck when nothing is printed.
func Create(ctx context.Context, instance string, distro Distro, size Size, out io.Writer) (config.Machine, error) {
	if err := Available(); err != nil {
		return config.Machine{}, err
	}
	if !name.MatchString(instance) {
		return config.Machine{}, fmt.Errorf(
			"%q cannot be a machine name: use lower-case letters, digits and dashes", instance)
	}
	if err := checkHostSize(size); err != nil {
		return config.Machine{}, err
	}
	if exists(ctx, instance) {
		return config.Machine{}, fmt.Errorf(
			"a local machine named %q is already there: remove it with `devmachine machines delete-local %s`",
			instance, instance)
	}

	path, remove, err := writeTemplate(distro, size)
	if err != nil {
		return config.Machine{}, err
	}
	defer remove()

	if _, err := run(ctx, out, "start", "--name="+instance, "--tty=false", path); err != nil {
		return config.Machine{}, fmt.Errorf("creating the local machine %q: %w", instance, err)
	}
	return Machine(ctx, instance)
}

// Machine describes a local machine as the configuration would.
func Machine(ctx context.Context, instance string) (config.Machine, error) {
	if err := Available(); err != nil {
		return config.Machine{}, err
	}

	out, err := run(ctx, nil, "list", "--format", "{{.SSHLocalPort}}", instance)
	if err != nil {
		return config.Machine{}, fmt.Errorf("asking lima about %q: %w", instance, err)
	}

	port, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return config.Machine{}, fmt.Errorf("lima reported no port for %q: %s", instance, strings.TrimSpace(out))
	}
	// Lima reports port 0 for a machine that is not running, and an address
	// with port 0 sends somebody to read a connection error instead.
	if port == 0 {
		return config.Machine{}, fmt.Errorf(
			"the local machine %q is not running: start it with `devmachine machines start %s`", instance, instance)
	}

	return config.Machine{
		Name:  instance,
		Hosts: []config.Host{{Address: Address}},
		User:  AdminUser,
		Port:  port,
	}, nil
}

// Start boots a local machine that was stopped.
func Start(ctx context.Context, instance string) error {
	return simple(ctx, "starting", instance, "start", instance)
}

// Stop shuts a local machine down, keeping its disk.
func Stop(ctx context.Context, instance string) error {
	return simple(ctx, "stopping", instance, "stop", instance)
}

// Delete destroys a local machine and everything on it.
func Delete(ctx context.Context, instance string) error {
	// Without -f, limactl asks, and a CLI run has nothing to answer it with.
	return simple(ctx, "deleting", instance, "delete", "-f", instance)
}

func simple(ctx context.Context, doing, instance string, args ...string) error {
	if err := Available(); err != nil {
		return err
	}
	if !name.MatchString(instance) {
		return fmt.Errorf("%q cannot be a machine name: use lower-case letters, digits and dashes", instance)
	}
	if _, err := run(ctx, nil, args...); err != nil {
		return fmt.Errorf("%s the local machine %q: %w", doing, instance, err)
	}
	return nil
}

// exists reports whether Lima already knows that name. Lima fails for a name
// nothing uses, which is the answer rather than a problem.
func exists(ctx context.Context, instance string) bool {
	_, err := run(ctx, nil, "list", "--format", "{{.Status}}", instance)
	return err == nil
}

// writeTemplate puts the template, at size, where limactl can read it, and
// returns the function that takes it away again.
//
// The extension matters: it is how limactl knows what the file is.
func writeTemplate(distro Distro, size Size) (string, func(), error) {
	body, err := Render(distro, size)
	if err != nil {
		return "", nil, err
	}
	dir, err := os.MkdirTemp("", "devmachine-local-")
	if err != nil {
		return "", nil, fmt.Errorf("writing the machine template: %w", err)
	}
	remove := func() { _ = os.RemoveAll(dir) }

	path := filepath.Join(dir, "machine.yaml")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		remove()
		return "", nil, fmt.Errorf("writing the machine template: %w", err)
	}
	return path, remove, nil
}

// runLimactl runs one limactl command.
//
// What limactl says about its own progress goes to out as it arrives; what
// comes back is its standard output, which is where an answer is printed.
func runLimactl(ctx context.Context, out io.Writer, args ...string) (string, error) {
	if out == nil {
		out = io.Discard
	}

	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, limactl, args...)
	cmd.Stdout = &stdout
	cmd.Stderr = io.MultiWriter(out, &stderr)

	if err := cmd.Run(); err != nil {
		if message := strings.TrimSpace(lastLine(stderr.String())); message != "" {
			return stdout.String(), fmt.Errorf("%w: %s", err, message)
		}
		return stdout.String(), err
	}
	return stdout.String(), nil
}

// lastLine is what limactl leaves as the reason it stopped, after however many
// lines of progress came before it.
func lastLine(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	return lines[len(lines)-1]
}
