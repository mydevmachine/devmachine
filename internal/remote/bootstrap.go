package remote

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/mydevmachine/devmachine/internal/config"
)

// installKeyScript adds one line to the admin account's authorized_keys.
//
// The key arrives on stdin rather than in the command, because an argument is
// in `ps` for every account on the machine to read while the command runs. A
// public key is not a secret; the habit is what carries over to the values
// that are.
const installKeyScript = `set -eu
umask 077
dir="$HOME/.ssh"
file="$dir/authorized_keys"
mkdir -p "$dir"
chmod 0700 "$dir"
touch "$file"
chmod 0600 "$file"
key=$(cat)
if printf '%s\n' "$key" | grep -qxFf - "$file"; then
	exit 0
fi
if [ -s "$file" ] && [ -n "$(tail -c 1 "$file")" ]; then
	printf '\n' >> "$file"
fi
printf '%s\n' "$key" >> "$file"
`

// InstallKey appends a public key to the admin account's authorized_keys.
//
// Running it twice leaves one line: setup is run again on a machine it already
// owns more often than it is run on a new one.
func InstallKey(ctx context.Context, c Client, publicKey string) error {
	key := strings.TrimSpace(publicKey)
	if key == "" {
		return fmt.Errorf("no public key to install")
	}
	if _, err := c.RunInput(ctx, installKeyScript, strings.NewReader(key+"\n")); err != nil {
		return fmt.Errorf("installing the public key: %w", err)
	}
	return nil
}

// ProveKey opens a new connection using only the key, and returns an error
// describing what to do when it fails.
//
// The session that installed the key cannot prove anything about it: that
// session was authenticated by a password. A wrong mode on authorized_keys, an
// AuthorizedKeysFile pointing elsewhere, or SELinux all let the key install and
// still refuse it.
func ProveKey(ctx context.Context, m config.Machine, user, keyPath string) error {
	client, err := ProveAuth(ctx, m, user, Auth{KeyPath: keyPath})
	if err != nil {
		return err
	}
	return client.Close()
}

// ProveAuth is ProveKey for a key nobody can point at: one an SSH agent holds
// and never writes to disk.
//
// It hands back the connection it proved, so everything that follows — turning
// password login off included — runs over the one that was proved rather than
// over the password session that is about to stop working.
func ProveAuth(ctx context.Context, m config.Machine, user string, a Auth) (Client, error) {
	client, _, err := DialWith(ctx, m, user, a)
	if err != nil {
		return nil, fmt.Errorf("%s is installed but does not log in as %q on machine %q. "+
			"Check that ~/.ssh/authorized_keys holds the line, that it is mode 0600 inside a 0700 ~/.ssh, "+
			"that sshd's AuthorizedKeysFile points at that file, and that PubkeyAuthentication is on: %w",
			a.describe(), user, m.Name, err)
	}
	return client, nil
}

// AsRoot wraps a script so it runs as root: directly when the admin login
// already is root, through `sudo -n` when it is not.
//
// It asks `id -u` first instead of trying without sudo and retrying with it.
// A retry would run a half-finished script twice, and would read any failure
// at all — a full disk, a refused sshd config — as a missing permission.
// `-H` so HOME is root's, as it is for an admin that is root: a script that
// writes under $HOME, and Ansible's own ~/.ansible, must not land in the
// admin's home owned by root.
//
// `-n` because nobody is there to type a password: a sudo that wants one
// fails at once, and CheckRoot is what says so in words.
//
// stdin is left to the script, because the hardening drop-in arrives on it.
//
// bash when there is one, as root's login shell was before this wrapper
// existed: dash ends a script at a `.` of a missing file where bash goes on,
// and a command written against one must not change meaning under the other.
func AsRoot(script string) string {
	quoted := shellQuote(script)
	return `devmachine_sh=$(command -v bash) || devmachine_sh=sh; ` +
		`if [ "$(id -u)" -eq 0 ]; then "$devmachine_sh" -c ` + quoted +
		`; else sudo -n -H "$devmachine_sh" -c ` + quoted + `; fi`
}

// ErrNoRoot is an admin login that is not root and has no passwordless sudo.
var ErrNoRoot = errors.New("the admin login cannot become root")

// rootCheckScript succeeds for root, and for an admin whose sudo needs no
// password.
const rootCheckScript = `if [ "$(id -u)" -eq 0 ]; then exit 0; fi
sudo -n true`

// CheckRoot proves the admin login can become root before anything is
// changed, so a missing sudo rule is one clear sentence and not an apt lock
// error halfway through a bootstrap.
func CheckRoot(ctx context.Context, c Client, user string) error {
	if _, err := c.Run(ctx, rootCheckScript); err != nil {
		return fmt.Errorf("%w: %q is not root, and `sudo -n true` fails as it. "+
			"Log in as root, or give it passwordless sudo on the machine: "+
			"echo '%[2]s ALL=(ALL) NOPASSWD:ALL' | sudo tee /etc/sudoers.d/devmachine-%[3]s: %[4]w",
			ErrNoRoot, user, sudoersFileName(user), err)
	}
	return nil
}

// sudoersFileName is a user name made safe for /etc/sudoers.d, which sudo
// reads only when the file name has no dot and does not end in a tilde.
func sudoersFileName(user string) string {
	return strings.NewReplacer(".", "_", "~", "_").Replace(user)
}

// hardeningDropInPath is where password login is turned off.
//
// The 00- prefix is the whole trick, and the reason travels with it in the
// file's own first lines.
const hardeningDropInPath = "/etc/ssh/sshd_config.d/00-devmachine-hardening.conf"

// hardeningDropIn is the file's body.
const hardeningDropIn = `# Written by the devmachine CLI.
#
# 00- so it sorts first among the drop-ins. sshd uses the first value it finds
# for each directive, and the Include of sshd_config.d sits at the top of the
# main sshd_config, so whatever comes first in the alphabetical glob is in
# charge. An image that ships 60-cloudimg-settings.conf with
# PasswordAuthentication yes is why a 99- prefix silently does nothing: it is
# read last, and by then the answer is already decided.
PasswordAuthentication no
# Without this a password can still come back through PAM, and "password login
# is off" would not be true.
KbdInteractiveAuthentication no
PermitRootLogin prohibit-password
`

// writeDropInScript puts the body on disk. It arrives on stdin, so nothing in
// the file has to survive a round trip through shell quoting.
const writeDropInScript = `set -eu
umask 022
mkdir -p /etc/ssh/sshd_config.d
cat > ` + hardeningDropInPath + `
chmod 0644 ` + hardeningDropInPath + `
`

// validateScript asks sshd whether it would accept the configuration.
//
// sshd lives in sbin, which a non-interactive SSH session does not always have
// on its PATH.
//
// sshd -t refuses to parse anything without /run/sshd, which systemd makes
// only when ssh.service starts. Where sshd starts through ssh.socket — Ubuntu
// 24.04 and later — a machine reached only through Tailscale SSH may never
// have started it. The directory is tmpfs, and the one systemd would make.
const validateScript = `set -eu
PATH="$PATH:/usr/sbin:/sbin"
export PATH
install -d -m 0755 /run/sshd
sshd -t
`

// reloadScript picks the unit name the distribution uses: Debian and Ubuntu
// call it ssh, everybody else calls it sshd.
const reloadScript = `set -eu
if systemctl reload ssh 2>/dev/null; then
	exit 0
fi
systemctl reload sshd
`

// Harden turns password login off, validates before reloading, and then asks
// sshd whether passwords really are off.
//
// A configuration sshd refuses plus a reload is a machine nobody can reach
// again, so validation is not a courtesy: it is the only thing between a typo
// and a rebuild. It runs after the key has been proved, never before.
func Harden(ctx context.Context, c Client) error {
	if _, err := c.RunInput(ctx, AsRoot(writeDropInScript), strings.NewReader(hardeningDropIn)); err != nil {
		return fmt.Errorf("writing %s: %w", hardeningDropInPath, err)
	}

	if out, err := c.Run(ctx, AsRoot(validateScript)); err != nil {
		// A file the daemon refused must not stay: the next reload by
		// anything at all, a reboot included, would fail on it.
		if _, rmErr := c.Run(ctx, AsRoot("rm -f "+hardeningDropInPath)); rmErr != nil {
			return fmt.Errorf("sshd refused %s (%w) and it could not be taken away again (%w): "+
				"remove it by hand before sshd is reloaded", hardeningDropInPath, err, rmErr)
		}
		return fmt.Errorf("sshd refused the configuration, so password login is still on: %w: %s",
			err, strings.TrimSpace(out))
	}

	if _, err := c.Run(ctx, AsRoot(reloadScript)); err != nil {
		return fmt.Errorf("reloading sshd: %w", err)
	}
	return provePasswordLoginOff(ctx, c)
}

// effectiveConfigScript prints the configuration sshd runs with, every
// drop-in applied.
const effectiveConfigScript = `set -eu
PATH="$PATH:/usr/sbin:/sbin"
export PATH
sshd -T
`

// provePasswordLoginOff asks sshd, not the file, whether passwords are off.
//
// A drop-in can be valid and still never read: an sshd_config with no Include
// of sshd_config.d, or one that sets PasswordAuthentication before the
// Include. Without this, "password login is off" would be said of a machine
// that still takes passwords.
func provePasswordLoginOff(ctx context.Context, c Client) error {
	effective, err := c.Run(ctx, AsRoot(effectiveConfigScript))
	if err != nil {
		return fmt.Errorf("asking sshd -T whether password login is off: %w", err)
	}
	if passwordLoginOff(effective) {
		return nil
	}

	// A file that does not do what it says misleads whoever reads it next.
	if _, err := c.Run(ctx, AsRoot("rm -f "+hardeningDropInPath)); err != nil {
		return fmt.Errorf("sshd still allows passwords and %s could not be taken away again (%w): "+
			"remove it by hand", hardeningDropInPath, err)
	}
	if _, err := c.Run(ctx, AsRoot(reloadScript)); err != nil {
		return fmt.Errorf("sshd still allows passwords, and reloading it without %s failed: %w",
			hardeningDropInPath, err)
	}
	return fmt.Errorf("the drop-in was written but sshd still allows passwords: %s is not read by this sshd",
		hardeningDropInPath)
}

// passwordLoginOff reads `sshd -T` output, which prints every directive in
// lower case, once, with the value sshd settled on.
func passwordLoginOff(effective string) bool {
	for _, line := range strings.Split(effective, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), " ")
		if ok && strings.EqualFold(key, "passwordauthentication") {
			return strings.EqualFold(strings.TrimSpace(value), "no")
		}
	}
	return false
}

// OSReleaseCommand reads the file that says which distribution a machine is.
const OSReleaseCommand = "cat /etc/os-release"

// OSReleaseID reads ID from an os-release file. ID_LIKE is deliberately not
// consulted: "like debian" is a family, not a promise that a package of the
// same name exists.
func OSReleaseID(body string) string {
	for _, line := range strings.Split(body, "\n") {
		value, ok := strings.CutPrefix(strings.TrimSpace(line), "ID=")
		if !ok {
			continue
		}
		return strings.Trim(strings.TrimSpace(value), `"`)
	}
	return "unknown"
}

// aptInstallAnsible installs Ansible on a Debian or an Ubuntu.
//
// It installs `ansible`, not `ansible-core`: the core package carries no
// community.general, the firewall package needs that collection, and the
// failure only shows up much later, inside a play. This was found on a real
// machine, not reasoned about.
const aptInstallAnsible = `set -eu
if command -v ansible-playbook >/dev/null 2>&1; then
	echo "ansible is already installed"
	exit 0
fi
export DEBIAN_FRONTEND=noninteractive
# A cloud image that booted a minute ago is usually still running
# unattended-upgrades, which holds the dpkg lock. Without the timeout a first
# run loses that race and fails for a reason nobody can see.
apt-get -o DPkg::Lock::Timeout=300 update
apt-get -o DPkg::Lock::Timeout=300 install -y ansible
`

// pacmanInstallAnsible installs Ansible on an Arch Linux.
//
// `ansible`, not `ansible-core`, for the same reason as on apt: only the full
// package carries community.general. Checked on a real Arch machine.
//
// No -y: refreshing the package lists without upgrading is a partial upgrade,
// which Arch does not support and which can pull an Ansible built for a Python
// the machine does not have. A full -Syu is the operator's call, not a side
// effect of setup.
const pacmanInstallAnsible = `set -eu
if command -v ansible-playbook >/dev/null 2>&1; then
	echo "ansible is already installed"
	exit 0
fi
if ! pacman -S --noconfirm --needed ansible; then
	echo "pacman could not install ansible from the package lists this machine has." >&2
	echo "They are probably older than the mirrors: bring the machine up to date with pacman -Syu, then run this again." >&2
	exit 1
fi
`

// ansibleInstall maps a distribution to the way Ansible is installed on it.
//
// It holds what has been run on a real machine and nothing else. A table with
// four entries nobody has tried gets somebody halfway through a first run and
// then leaves them there; an honest refusal that names their distribution at
// least tells them what to do next. archarm is Arch Linux ARM: the same
// pacman and the same package as arch.
//
// It is also the list of distributions this CLI supports: SupportedLinux reads
// it, and so does doctor.
var ansibleInstall = map[string]string{
	"debian":  aptInstallAnsible,
	"ubuntu":  aptInstallAnsible,
	"arch":    pacmanInstallAnsible,
	"archarm": pacmanInstallAnsible,
}

// InstallAnsible puts Ansible on the machine.
//
// It is the last thing done by hand. Everything after it is a play, which is
// why this is the whole imperative surface and not the start of one.
//
// The output goes to out as it arrives: installing Ansible is minutes of work,
// and minutes of silence look like a machine that has stopped answering.
func InstallAnsible(ctx context.Context, c Client, out io.Writer) error {
	system, err := DetectSystem(ctx, c)
	if err != nil {
		return err
	}
	script, ok := ansibleInstall[system.ID]
	if !ok {
		return fmt.Errorf("this CLI does not install Ansible on %s", system)
	}

	if err := c.Stream(ctx, AsRoot(script), out, out); err != nil {
		return fmt.Errorf("installing Ansible on %s: %w", system, err)
	}
	return nil
}
