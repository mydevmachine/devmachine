package doctor

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/mydevmachine/devmachine/internal/config"
	"github.com/mydevmachine/devmachine/internal/facts"
	"github.com/mydevmachine/devmachine/internal/packages"
	"github.com/mydevmachine/devmachine/internal/remote"
)

// CheckPrerequisites is the one check that stands for a Mac's prerequisites
// when there is nothing to list: all present, or no way to ask yet.
const CheckPrerequisites = "prerequisites"

// PrerequisitePrefix names the check for one thing a Mac lacks, such as
// "prerequisite: Homebrew".
const PrerequisitePrefix = "prerequisite: "

// macAnsibleCommand finds ansible-playbook on a Mac. A plain SSH command there
// has neither Homebrew nor MacPorts on PATH, so it looks where the bootstrap
// said it put it first, then where each of them and pipx install it.
func macAnsibleCommand(reported string) string {
	places := []string{}
	if reported != "" {
		places = append(places, quote(reported))
	}
	places = append(places, `"$HOME/.local/bin/ansible-playbook"`,
		"/opt/homebrew/bin/ansible-playbook", "/usr/local/bin/ansible-playbook",
		"/opt/local/bin/ansible-playbook", "/opt/local/bin/ansible-playbook-3.*")
	return "for p in " + strings.Join(places, " ") +
		`; do if [ -x "$p" ]; then echo "$p"; exit 0; fi; done; command -v ansible-playbook`
}

func quote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func macAnsibleCheck(ctx context.Context, client remote.Client, dir, machine string) Check {
	reported := ""
	if f, found, err := facts.Load(dir, machine); err == nil && found {
		reported = f.AnsiblePlaybook
	}
	path, err := client.Run(ctx, macAnsibleCommand(reported))
	if err != nil || strings.TrimSpace(path) == "" {
		return Check{Name: CheckAnsible, Status: StatusFail,
			Detail: "ansible-playbook is not on the Mac; `devmachine setup` installs it through Homebrew or MacPorts"}
	}
	return Check{Name: CheckAnsible, Status: StatusPass, Detail: strings.TrimSpace(path)}
}

// prerequisiteChecks asks the machine's package manager package what the Mac
// lacks. Its check only reads, and its script reaches the Mac on stdin, so
// doctor leaves nothing behind.
func prerequisiteChecks(ctx context.Context, client remote.Client, dir string, cfg config.Config,
	m config.Machine) []Check {
	found, reason := macBootstrapPackage(ctx, client, dir, cfg, m)
	if reason != "" {
		return []Check{{Name: CheckPrerequisites, Status: StatusSkip, Detail: reason}}
	}
	body, err := os.ReadFile(found.Manifest.BootstrapPath())
	if err != nil {
		return []Check{{Name: CheckPrerequisites, Status: StatusWarn, Detail: err.Error()}}
	}
	missing, err := (remote.Bootstrap{Body: body}).Check(ctx, client)
	if err != nil {
		return []Check{{Name: CheckPrerequisites, Status: StatusWarn, Detail: err.Error()}}
	}
	name := found.Manifest.Name
	if len(missing) == 0 {
		return []Check{{Name: CheckPrerequisites, Status: StatusPass, Detail: "nothing missing (checked by " + name + ")"}}
	}
	out := make([]Check, 0, len(missing))
	for _, p := range missing {
		out = append(out, Check{Name: PrerequisitePrefix + p.Name, Status: StatusWarn,
			Detail: fmt.Sprintf("missing: `devmachine setup` installs it through %s once you agree (about %d minutes)",
				name, p.Minutes)})
	}
	return out
}

// macBootstrapPackage is the package setup would run, or why there is none
// to ask: the machine's own choice, else the manager the Mac has. A self
// machine defaults to mac-brew, as setup does.
func macBootstrapPackage(ctx context.Context, client remote.Client, dir string, cfg config.Config,
	m config.Machine) (packages.Found, string) {
	store := packages.OpenCached(dir, cfg.Packages)
	listed, err := store.Bootstrapping(m.Packages)
	if err != nil {
		return packages.Found{}, err.Error()
	}
	switch len(listed) {
	case 1:
		return listed[0], ""
	case 0:
	default:
		return packages.Found{}, fmt.Sprintf("machine %q lists more than one package manager package: keep one", m.Name)
	}

	name := packages.MacBrew
	if !m.Self {
		have, err := remote.MacManagers(ctx, client)
		if err != nil {
			return packages.Found{}, err.Error()
		}
		switch {
		case have.Brew && !have.Ports:
		case have.Ports && !have.Brew:
			name = packages.MacPorts
		default:
			return packages.Found{}, "no package manager chosen yet: `devmachine setup` asks, or takes --package-manager brew|ports"
		}
	}
	found, err := store.Get(name)
	if err != nil || found.Manifest.Bootstrap == "" {
		return packages.Found{}, fmt.Sprintf("no %s package with a bootstrap is available: run `devmachine packages pin`", name)
	}
	return found, ""
}

// SSHAccessPrefix names the check, one per workspace on a Mac, that its
// account may log in over SSH, such as "ssh access: alice".
const SSHAccessPrefix = "ssh access: "

// remoteLoginCommand asks, in one round trip, whether Remote Login lets each
// account in. It prints "all" when Remote Login allows all users, and
// otherwise one "<account> member|outside|absent" line per account.
func remoteLoginCommand(accounts []string) string {
	quoted := make([]string, len(accounts))
	for i, a := range accounts {
		quoted[i] = quote(a)
	}
	group := remote.MacRemoteLoginGroup
	return "if ! dscl . -read /Groups/" + group + " >/dev/null 2>&1; then echo all; exit 0; fi; " +
		"for u in " + strings.Join(quoted, " ") + "; do " +
		`if ! id -u "$u" >/dev/null 2>&1; then echo "$u absent"; ` +
		`elif dseditgroup -o checkmember -m "$u" ` + group + ` >/dev/null 2>&1; then echo "$u member"; ` +
		`else echo "$u outside"; fi; done`
}

// sshAccessChecks reports, for each workspace on a Mac, whether Remote Login
// lets its account in. With "Only these users" set, macOS refuses an account
// outside that list before the key is even looked at.
func sshAccessChecks(ctx context.Context, client remote.Client, workspaces []config.Workspace) []Check {
	if len(workspaces) == 0 {
		return nil
	}
	accounts := make([]string, len(workspaces))
	for i, w := range workspaces {
		accounts[i] = w.LinuxUser()
	}
	out, err := client.Run(ctx, remoteLoginCommand(accounts))
	if err != nil {
		checks := make([]Check, len(workspaces))
		for i, w := range workspaces {
			checks[i] = Check{Name: SSHAccessPrefix + w.Name, Status: StatusWarn,
				Detail: "could not read who Remote Login lets in: " + err.Error()}
		}
		return checks
	}

	allowAll := strings.TrimSpace(out) == "all"
	state := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		if account, answer, found := strings.Cut(strings.TrimSpace(line), " "); found {
			state[account] = answer
		}
	}

	checks := make([]Check, len(workspaces))
	for i, w := range workspaces {
		check := Check{Name: SSHAccessPrefix + w.Name}
		switch {
		case allowAll:
			check.Status, check.Detail = StatusPass, "Remote Login allows all users"
		case state[accounts[i]] == "member":
			check.Status, check.Detail = StatusPass, accounts[i]+" is in the users Remote Login allows"
		case state[accounts[i]] == "absent":
			check.Status, check.Detail = StatusSkip, "the account "+accounts[i]+" does not exist yet; `devmachine sync` creates it"
		default:
			check.Status = StatusWarn
			check.Detail = "Remote Login allows only some users, and " + accounts[i] + " is not one of them: " +
				"`devmachine sync` adds the account, or allow All users in System Settings > General > Sharing > Remote Login"
		}
		checks[i] = check
	}
	return checks
}
