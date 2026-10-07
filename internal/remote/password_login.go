package remote

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

// previousDropInPath holds the drop-in as it was before the last change, so a
// change sshd refuses puts back what was there instead of deleting it. It sits
// outside sshd_config.d on purpose: macOS includes sshd_config.d/* — every
// file, not only *.conf — so a copy kept in there would be read as
// configuration.
const previousDropInPath = "/etc/ssh/.devmachine-hardening.conf.previous"

// restoreScript puts back the drop-in a change replaced, or removes the new
// one when there was none before.
const restoreScript = `if [ -f ` + previousDropInPath + ` ]; then mv -f ` + previousDropInPath + ` ` +
	hardeningDropInPath + `; else rm -f ` + hardeningDropInPath + `; fi`

// accountNamePattern is the portable account name: what useradd and dscl both
// accept, and nothing sshd_config would read as a pattern, a negation or a
// list separator.
var accountNamePattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,63}$`)

// ValidAccountName refuses a name that would change what a Match User line
// means: a comma is a second account, a star or a ! a pattern.
func ValidAccountName(name string) error {
	if !accountNamePattern.MatchString(name) {
		return fmt.Errorf("%q is not an account name: use letters, digits, '_', '.' and '-', starting with a letter, digit or '_'", name)
	}
	return nil
}

// HardeningDropIn is the drop-in's body: password login off for every
// account, then back on for the accounts in keep.
//
// The Match block goes last because it runs to the end of the file it is in.
// It does not run on into the main sshd_config past the Include: checked
// with sshd -t and sshd -T on OpenSSH 9.6 (Ubuntu 24.04) and 9.9 (macOS),
// and TestAMatchBlockInADropInEndsWithItsFile checks it on every run.
func HardeningDropIn(keep []string) string {
	if len(keep) == 0 {
		return hardeningDropIn
	}
	return hardeningDropIn + `# These accounts still log in with a password. A Match block runs to the
# end of the file it is in, so it stays last here.
Match User ` + strings.Join(keep, ",") + `
	PasswordAuthentication yes
	KbdInteractiveAuthentication yes
`
}

// accountConfigScript prints the configuration sshd would use for one
// account's login, Match blocks applied.
func accountConfigScript(user string) string {
	return `set -eu
PATH="$PATH:/usr/sbin:/sbin"
export PATH
sshd -T -C user=` + user + `,host=localhost,addr=127.0.0.1
`
}

// HardenKeeping is Harden with some accounts left able to log in with their
// password. Each of them is proved the same way the rest are: by asking sshd.
func HardenKeeping(ctx context.Context, c Client, system System, keep []string) error {
	for _, user := range keep {
		if err := ValidAccountName(user); err != nil {
			return err
		}
	}
	if err := hardenWith(ctx, c, system, HardeningDropIn(keep)); err != nil {
		return err
	}
	steps := sshdStepsFor(system)
	for _, user := range keep {
		effective, err := c.Run(ctx, AsRoot(accountConfigScript(user)))
		if err != nil {
			return fmt.Errorf("asking sshd -T whether %q still logs in with a password: %w", user, err)
		}
		if passwordAuthentication(effective) == "yes" {
			continue
		}
		if _, err := c.Run(ctx, AsRoot(restoreScript)); err != nil {
			return fmt.Errorf("sshd does not let %q log in with a password, and the previous %s could not be put back (%w)",
				user, hardeningDropInPath, err)
		}
		if err := steps.reloadSshd(ctx, c); err != nil {
			return fmt.Errorf("reloading sshd: %w", err)
		}
		return fmt.Errorf("the drop-in was written but sshd still refuses a password for %q: "+
			"something earlier in sshd_config decides it, so the previous drop-in was put back", user)
	}
	return nil
}

// takeDropInAwayScript moves the drop-in aside, keeping it to put back.
const takeDropInAwayScript = `set -eu
rm -f ` + previousDropInPath + `
if [ -f ` + hardeningDropInPath + ` ]; then mv -f ` + hardeningDropInPath + ` ` + previousDropInPath + `; fi
`

// PasswordLoginOn takes the drop-in away, so sshd goes back to what the
// system itself says about passwords. On macOS, Debian and Ubuntu that is
// password login on; a system whose own sshd_config turns it off stays off.
func PasswordLoginOn(ctx context.Context, c Client, system System) error {
	steps := sshdStepsFor(system)
	if _, err := c.Run(ctx, AsRoot(takeDropInAwayScript)); err != nil {
		return fmt.Errorf("taking %s away: %w", hardeningDropInPath, err)
	}
	if out, err := c.Run(ctx, AsRoot(steps.validate)); err != nil {
		if _, restoreErr := c.Run(ctx, AsRoot(restoreScript)); restoreErr != nil {
			return fmt.Errorf("sshd refused the configuration without %s (%w), and it could not be put back (%w): "+
				"put it back by hand before sshd is reloaded", hardeningDropInPath, err, restoreErr)
		}
		return fmt.Errorf("sshd refused the configuration without %s, so it was put back: %w: %s",
			hardeningDropInPath, err, strings.TrimSpace(out))
	}
	if err := steps.reloadSshd(ctx, c); err != nil {
		return fmt.Errorf("reloading sshd: %w", err)
	}
	return nil
}

// AccountPasswordLogin says whether sshd takes a password for one account.
type AccountPasswordLogin struct {
	User    string `json:"user"`
	Allowed bool   `json:"password_login"`
}

// PasswordLoginState is what sshd says about passwords on a machine.
type PasswordLoginState struct {
	// Default is the answer for an account no Match block names.
	Default bool `json:"default"`
	// DropIn says whether the CLI's drop-in is there.
	DropIn bool `json:"drop_in"`
	// Accounts are the people on the machine: no system accounts.
	Accounts []AccountPasswordLogin `json:"accounts"`
}

const dropInPresentScript = `if [ -f ` + hardeningDropInPath + ` ]; then echo yes; else echo no; fi`

// accountsScript lists the accounts people log in with. macOS gives them a
// uid from 501 and names system accounts with a leading _; Linux starts at
// 1000 and gives services no login shell.
func accountsScript(system System) string {
	if system.MacOS() {
		return `dscl . -list /Users UniqueID | awk '$2 >= 501 && $1 !~ /^_/ {print $1}'`
	}
	return `getent passwd | awk -F: '$3 >= 1000 && $3 < 65534 && $7 !~ /(nologin|false)$/ {print $1}'`
}

// HumanAccounts lists the accounts people log in with, by name.
func HumanAccounts(ctx context.Context, c Client, system System) ([]string, error) {
	listed, err := c.Run(ctx, AsRoot(accountsScript(system)))
	if err != nil {
		return nil, fmt.Errorf("listing the machine's accounts: %w", err)
	}
	var names []string
	for _, user := range strings.Fields(listed) {
		if ValidAccountName(user) == nil {
			names = append(names, user)
		}
	}
	return names, nil
}

// ReadPasswordLogin asks sshd, account by account, who can log in with a
// password. It reads the answer sshd would give, not the files, because a
// file earlier in sshd_config can decide it first.
func ReadPasswordLogin(ctx context.Context, c Client, system System) (PasswordLoginState, error) {
	var state PasswordLoginState
	effective, err := c.Run(ctx, AsRoot(effectiveConfigScript))
	if err != nil {
		return state, fmt.Errorf("asking sshd -T about password login: %w", err)
	}
	state.Default = passwordAuthentication(effective) == "yes"

	present, err := c.Run(ctx, AsRoot(dropInPresentScript))
	if err != nil {
		return state, fmt.Errorf("looking for %s: %w", hardeningDropInPath, err)
	}
	state.DropIn = strings.TrimSpace(present) == "yes"

	names, err := HumanAccounts(ctx, c, system)
	if err != nil {
		return state, err
	}
	state.Accounts = []AccountPasswordLogin{}
	for _, user := range names {
		answer, err := c.Run(ctx, AsRoot(accountConfigScript(user)))
		if err != nil {
			return state, fmt.Errorf("asking sshd -T about %q: %w", user, err)
		}
		state.Accounts = append(state.Accounts, AccountPasswordLogin{
			User: user, Allowed: passwordAuthentication(answer) == "yes",
		})
	}
	return state, nil
}

// passwordAuthentication reads PasswordAuthentication from `sshd -T` output,
// in lower case, or "" when it is not there. OpenSSH 10 prints the names in
// CamelCase where older versions print them in lower case.
func passwordAuthentication(effective string) string {
	for _, line := range strings.Split(effective, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), " ")
		if ok && strings.EqualFold(key, "passwordauthentication") {
			return strings.ToLower(strings.TrimSpace(value))
		}
	}
	return ""
}
