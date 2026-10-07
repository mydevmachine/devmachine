package commands

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/mydevmachine/devmachine/internal/network"
	"github.com/mydevmachine/devmachine/internal/packages"
	"github.com/mydevmachine/devmachine/internal/provision"
	"github.com/spf13/cobra"
)

// selfNamePattern is what a network name has to look like to be written into
// hosts as `<prefix>:<name>`: one word a person could have typed there.
var selfNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// joinable finds the package of that name on the machine, when it declares a
// network a login can join.
func joinable(plan packages.MachinePlan, name string) (network.Provider, bool) {
	for _, found := range plan.OnMachine.Ordered {
		if found.Manifest.Name != name {
			continue
		}
		p, ok := network.FromFound(found)
		if ok && p.Network.Join != "" {
			return p, true
		}
	}
	return network.Provider{}, false
}

// joinNetwork signs the machine in to a network through the package's own
// join script, in a real terminal, then asks the machine its name there and
// adds `<prefix>:<name>` above the other hosts.
//
// The package says how to join, with the machine's settings for it; the CLI
// knows only where the scripts are and what to do with the name.
func joinNetwork(cmd *cobra.Command, opts *options, found declared, p network.Provider) error {
	m := found.machine
	address, err := firstAddress(m, "login")
	if err != nil {
		return err
	}
	if _, err := lookPath("ssh"); err != nil {
		return fmt.Errorf("ssh is not installed on this machine")
	}
	if err := verifySystemHost(cmd.Context(), m); err != nil {
		return err
	}
	base, err := provision.Base(m)
	if err != nil {
		return err
	}
	settings, err := network.SettingsJSON(p.Settings(m.Settings))
	if err != nil {
		return err
	}
	onMachine := func(script string) string {
		return inMachinePath(found.dir, m, network.SettingsEnv+"="+quoteForShell(settings)+" "+
			quoteForShell(provision.RolePath(base, p.Source, p.Package, script)))
	}

	cmd.Printf("opening a session on %s to join it to %s\n", m.Name, p.Package)
	runErr := runInteractive("ssh", loginArgs(m, m.User, address, onMachine(p.Network.Join))...)
	record(opts, target{machine: m}, "login "+p.Package, runErr == nil)
	if runErr != nil {
		return fmt.Errorf("joining %s did not finish: %w. The package has to be on the machine first: run `devmachine sync`",
			p.Package, runErr)
	}

	hint := fmt.Sprintf("<name> is what %s's %s prints on %s.", p.Package, p.Network.SelfName, m.Name)
	client, _, err := dial(cmd.Context(), m, "")
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()

	out, err := client.Run(cmd.Context(), onMachine(p.Network.SelfName))
	if err != nil {
		cmd.Printf("logged in, but could not read its name on %s (%v).\n", p.Package, err)
		cmd.Print(networkHostsByHand(m, p.Network.Prefix, hint))
		return nil
	}
	name := firstLine(out)
	if !selfNamePattern.MatchString(name) {
		cmd.Printf("logged in, but %s printed %q, which is not a name to put in hosts.\n", p.Network.SelfName, name)
		cmd.Print(networkHostsByHand(m, p.Network.Prefix, hint))
		return nil
	}
	return addNetworkHost(cmd, found.dir, m, p.Network.Prefix+":"+name)
}

func firstLine(out string) string {
	for _, line := range strings.Split(out, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return ""
}
