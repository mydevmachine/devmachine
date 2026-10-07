# Troubleshooting

## `ssh <workspace>-devmachine`: "Could not resolve hostname"

**What it means:** That name only exists as a Host entry in
`~/.ssh/config`, and it is missing — either you said no when `setup` asked
about it, or it was set up before that question existed.

**What to do:**

```
devmachine aliases --write
```

Say yes when it asks. `devmachine ssh <workspace>` and `mosh <workspace>`
still work either way — only the plain `ssh`/`mosh` form, and tools that dial
`ssh` themselves (VS Code Remote-SSH, Zed, the macOS app), need the alias.
See [SSH aliases](concepts/reaching-your-server.md#ssh-aliases).

## A new workspace's alias works for a moment, then "Could not resolve hostname"

**What it means:** The aliases ended up in two files. Something else owns
`~/.ssh/config` — a template, a dotfiles manager — and keeps the aliases
in a file it `Include`s, while the CLI was writing its block into
`~/.ssh/config` itself. The next time that tool rewrote `~/.ssh/config`,
the CLI's block went with it, and the included file never learned about
the new workspace.

**What to do:** Tell the CLI where the aliases live, once:
`devmachine aliases --write --path ~/.ssh/<that-file> --yes`. It records
the file as `ssh_aliases_path`, empties the block it had left in
`~/.ssh/config`, and every later change — a new workspace, a new machine —
is written there.

## "several machines are configured: say which one with --machine"

**What it means:** You have more than one server configured, and this command
needs to know which one to act on.

**What to do:** Add `--machine <name>`, or use a workspace name — that already
says which server it lives on.

`doctor` never says this: with several machines and no `--machine`, it
checks every one of them.

## "no address answered"

**What it means:** Nothing answered at the address devmachine tried. The error
lists every address it tried and what happened for each.

**What to do:**

- Check the port. A server on a non-standard port needs `port:` set in its
  configuration.
- Check for a second address — see
  [Addresses and fallback](how-it-works/addresses-and-fallback.md).

## "answered on … but refused the login"

**What it means:** The server is there, so the network is fine. The account
or the key is the problem.

**What to do:**

- Check that account exists on the server. A workspace in your configuration
  is not created on the server until you run `sync`.
- Check the key is allowed to log in to that account.

## "no key in the configuration and no SSH agent"

**What it means:** devmachine has nothing to log in with.

**What to do:** Either set `key:` on the server to a private key, or start an
SSH agent and load one.

## `machines add`: "the key does not log in yet and no password was given"

**What it means:** `machines add --address …` asks nothing, so it had no
way to get the server's password, and the key it chose is not in the
admin's `authorized_keys` yet. A server just bought usually takes only a
password. Nothing was changed, and nothing was written to `config.yml`.

**What to do:** Give the password on stdin, so it never appears in the
command line or the shell history:

```
printf '%s' "$PASSWORD" | devmachine machines add … --password-stdin
```

Or put the key's public half (`<key>.pub`) in the admin's
`authorized_keys` through the provider's console, and run the same
command again.

## `create-local` refuses `--cpus`, `--memory` or `--disk`

**What it means:** The size you asked for does not fit this computer, so no
VM was made. The CPUs can be at most the computer's cores. The memory must
be less than the computer's, because macOS or Linux needs some for itself
while the VM runs. The disk must be at least 10 GiB, or the first `sync`
fills it.

**What to do:** Run it again with a number inside the range the message
gives.

## `create-local --add`: "the local machine … is running, and was not added"

**What it means:** The VM was created and is running, but adding it
failed — the error after the colon says which step. Nothing was written
to `config.yml`, so nothing points at a half-added machine.

**What to do:** Fix the cause, then run the command the error ends with.
It is the whole `machines add` for the running VM — name, address, port,
the host key's fingerprint, the public password on stdin, and the flags
you gave — ready to copy:

```
printf '%s' devmachine | devmachine machines add --name sandbox --address 127.0.0.1 \
  --port 60022 --fingerprint SHA256:… --password-stdin
```

Or throw the VM away with `devmachine machines delete-local <name>` and
run `create-local --add` again.

## It used to connect, and now it does not

**What it means:** If you recently added keys to your SSH agent, that is very
likely the cause, and only on a machine with neither `key:` nor
`agent_key:` in its configuration. A server gives up after a few tries, and
an agent holding many keys can use them all up before it reaches the one
that works.

A machine with `key:` or `agent_key:` set is immune to this: devmachine
offers that one key and nothing else. Plain `ssh`, and anything else on
your computer, is not — it still asks the agent for everything it holds.

**What to do:** On a machine with neither field yet, `devmachine setup` and
`devmachine machines add` only choose a key the first time — run them again
on an existing machine and they resume, without asking. Set the field by
hand in `config.yml` instead: `key: <path>` for a file, or
`agent_key: <public key line>` (`ssh-add -L` lists what your agent holds,
in that format) for one from the agent. Full explanation:
[SSH: logging in and knowing it is your server](how-it-works/ssh.md).

## "the SSH agent does not hold the key … that this machine logs in with"

**What it means:** The machine's `agent_key:` names a key your SSH agent is
not currently offering — the password manager it lives in is locked, or
`SSH_AUTH_SOCK` points at a different agent than the one that key is in.

**What to do:** Unlock the password manager (1Password, say) and try
again. If that does not fix it, check `SSH_AUTH_SOCK`:

```
echo $SSH_AUTH_SOCK
ssh-add -l
```

Compare the fingerprint `ssh-add -l` lists against the one the error
names. If they never match, `agent_key:` in `config.yml` names the wrong
key: replace it by hand with the line `ssh-add -L` prints for the key you
meant.

## "ansible-playbook is not on the machine"

**What it means:** `sync` runs Ansible, the tool devmachine uses to apply
packages, on the server — so the server needs it installed.

**What to do:** Run `devmachine setup --machine <name>`, which installs
it, or install it once by hand: `apt install ansible` on Debian and
Ubuntu, `pacman -S ansible` on Arch. Everything after that is `sync`'s job. `doctor` still
tells you the truth about everything else without it.

## "this CLI does not set up "…" yet"

```
this CLI does not set up "fedora" yet: it supports debian, ubuntu and arch, and systems based on them
```

**What it means:** The machine runs Linux, and neither `ID` nor
`ID_LIKE` in its `/etc/os-release` names Debian, Ubuntu or Arch Linux.
`setup` and `machines add` stop before changing anything: no key was
installed, password login is as it was, nothing was written to
`config.yml`. `doctor` reports the same thing as a failed `operating
system` check.

**What to do:** Use a machine with Debian, Ubuntu or Arch Linux — most
providers offer all three. Installing Ansible by hand does not get past
the check: the packages that `sync` applies are written for those three.

## "… which is based on …: … devmachine is not tested on it"

```
203.0.113.10 runs manjaro, which is based on arch: it is set up the arch way, but devmachine is not tested on it.
```

**What it means:** Not an error. `ID` in `/etc/os-release` is not one
the CLI knows, but `ID_LIKE` says the system is based on one it does.
`setup` goes on, and installs Ansible and runs the packages as it would
on that base. `doctor` shows the same sentence as a warning.

**What to do:** Nothing, while it works. If a package fails on the
derivative and not on the base, the derivative has renamed or left out
something the base has: see [based on Debian, Ubuntu or
Arch](supported-systems.md#based-on-debian-ubuntu-or-arch-accepted-not-tested).

## "… is not a system this CLI sets up"

```
"FreeBSD" is not a system this CLI sets up: it supports Linux (debian, ubuntu, arch) and macOS
```

**What it means:** `uname -s` on the machine said neither `Linux` nor
`Darwin` (macOS). The CLI stopped there, before changing anything.

**What to do:** Point the command at a Linux server (Debian, Ubuntu or
Arch Linux) or a Mac. Your own Mac is set up differently: `devmachine
machines add --self <name>`, see [your computer as a
machine](how-it-works/your-computer-as-a-machine.md).

## "pacman could not install ansible"

```
pacman could not install ansible from the package lists this machine has.
They are probably older than the mirrors: bring the machine up to date with pacman -Syu, then run this again.
```

**What it means:** On Arch, `setup` installs Ansible with `pacman -S`
and the package lists the machine already has. The mirrors keep only the
newest version of each package, so on a server whose lists are weeks
old the version pacman asks for is gone, and the download fails. `setup`
does not refresh the lists on its own: `pacman -Sy` without `-u` is a
partial upgrade, which can leave Ansible built for a Python the machine
does not have. The key is installed and proved; if hardening ran, password
login is already off.

**What to do:** On the machine, as root, bring it up to date with
`pacman -Syu`, then run the same `setup` or `machines add` command again.
The key now logs in, so no password is asked for.

## "the admin login cannot become root"

**What it means:** The machine's `user` in `config.yml` is not root, and
`sudo -n true` fails as that account — its `sudo` wants a password, or it
has none. The CLI needs root to turn password login off, install Ansible
and run `sync`, and nobody is there to type a password, so it stops before
changing anything.

**What to do:** Either log in as root (`user: root`, with the key in
root's `authorized_keys`), or give the account passwordless sudo, once,
on the machine:

```
echo 'alice ALL=(ALL) NOPASSWD:ALL' | sudo tee /etc/sudoers.d/devmachine-alice
sudo chmod 440 /etc/sudoers.d/devmachine-alice
```

Then run the command again. See
[an admin login that is not root](how-it-works/trust-bootstrap.md#an-admin-login-that-is-not-root).

## `setup` fails with "Could not open lock file … Permission denied", or `sync` with "sending a directory to /opt/devmachine … mkdir: Permission denied"

**What it means:** The admin login is not root, and the CLI is older than
the one that runs system steps through `sudo -n`. Those versions ran
`apt-get`, the SSH hardening and the bundle upload as the admin itself.

**What to do:** Update the CLI (`devmachine update`) and run the same
command again. Nothing was half-applied: both errors happen before the
first change. If it then says "the admin login cannot become root", see
the entry above.

## `setup` says the machine "answered through Tailscale SSH"

**What it means:** Port 22 on the address you gave is Tailscale SSH, not
`sshd`. Tailscale lets tailnet members in without checking a key, so the
CLI cannot prove the key from there. It installs the key anyway, so the
machine stays reachable when Tailscale SSH is off or you are outside the
tailnet.

**What to do:** Nothing, usually. To prove the key on its own, connect to
the machine's address outside Tailscale (its LAN or public IP) with
`ssh -o IdentitiesOnly=yes -o IdentityAgent=none -i <key> <user>@<address>`.
A machine set up by an older CLI over Tailscale SSH may have no key
installed at all: `devmachine setup --machine <name>` installs it.

## `setup` shows a host key fingerprint that is not the one in `~/.ssh/known_hosts`

**What it means:** Usually not a different server. A server has several
host keys — ED25519, ECDSA, RSA — and the CLI asks for its own preferred
algorithm, which can be a different one from the key your own `ssh`
recorded. Two fingerprints of different types never match each other.

**What to do:** Compare like with like. The CLI names the type it was
shown (`presented ecdsa-sha2-nistp256 host key SHA256:…`). Over a
connection you already trust, list every key with
`for f in /etc/ssh/ssh_host_*_key.pub; do ssh-keygen -lf $f; done` and
check the line of the same type. Only a mismatch of the same type means
something changed.

`--fingerprint` (`machines add`) and `--expect` (`machines trust`) take
the fingerprint of any of the server's keys, so the one in your
`known_hosts` works too: when it is not the type the CLI was shown, the
CLI asks the server for that type, and trusts that key if it matches. A
fingerprint none of the server's keys has is still refused.

## "the drop-in was written but sshd still allows passwords"

```
the drop-in was written but sshd still allows passwords: /etc/ssh/sshd_config.d/00-devmachine-hardening.conf is not read by this sshd
```

**What it means:** `setup` wrote the file that turns password login off,
SSH accepted it and was reloaded, and then `sshd -T` — the settings SSH
really runs with — still said `passwordauthentication yes`. SSH never
reads that file. Usually the main `/etc/ssh/sshd_config` has no `Include
/etc/ssh/sshd_config.d/*.conf` line, or sets `PasswordAuthentication yes`
above it (SSH keeps the first value it finds). The CLI removed the file
again and reloaded SSH, so the server is as it was: the key is installed
and proved, and **password login is still on**.

**What to do:** On the server, look at the top of `/etc/ssh/sshd_config`.
Add `Include /etc/ssh/sshd_config.d/*.conf` as the first line if it is
missing, or remove the `PasswordAuthentication yes` above it. Then run
`setup` again. To go on without hardening for now, run it with
`--no-harden`; password login stays on until `sshd_config` is fixed.

## "Missing privilege separation directory: /run/sshd"

**What it means:** `sshd -t`, which checks the SSH configuration before
it is reloaded, needs `/run/sshd`. systemd makes that directory only when
`ssh.service` starts. On Ubuntu 24.04 and later sshd starts through
`ssh.socket`, and a machine reached only through Tailscale SSH may never
have started it, so the directory is not there. Nothing about the
configuration is wrong.

**What to do:** Update the CLI and the packages release: both now make the
directory before the check. Until then, `sudo install -d -m 0755
/run/sshd` on the machine and run the command again; the directory is
temporary and gone at the next reboot.

## "ansible-playbook is not on your computer"

**What it means:** The same check as above, for your own computer. `sync` and
`doctor` both refuse to touch anything when Ansible is not on your `PATH`.

**What to do:** Run `devmachine setup --machine <name>`. On your own computer
it runs the `mac-brew` package's bootstrap (or `mac-ports`'s, when the
machine lists it), which lists what is missing and installs it after you
agree — no key, no password, no lock-down involved. See [what a machine
needs](how-it-works/what-a-machine-needs.md).

## "the Mac has neither Homebrew nor MacPorts"

```
the Mac has neither Homebrew nor MacPorts: run again with --package-manager brew or --package-manager ports
the Mac has both Homebrew and MacPorts: run again with --package-manager brew or --package-manager ports
```

**What it means:** A Mac gets Ansible through a package manager, and the
machine lists no `mac-brew` or `mac-ports` package. `setup` looked at the
Mac (`/opt/homebrew/bin/brew`, `/usr/local/bin/brew`, `/opt/local/bin/port`)
and found none, or both, so it cannot pick one for you, and there was no
terminal to ask at. Nothing on the Mac was changed.

**What to do:** Choose one and run the same command again with
`--package-manager brew` (Homebrew) or `--package-manager ports`
(MacPorts). Or add the package yourself: `devmachine packages add
mac-brew --machine <name>`.

## "machine … lists mac-brew and mac-ports: keep one"

```
machine "studio" lists mac-brew and mac-ports: keep one
```

**What it means:** Both package manager packages are on the machine, and
each would install Ansible its own way. `setup` stopped before changing
anything.

**What to do:** Remove one from the machine's `packages:` in
`config.yml`, then run `setup` again.

## "nothing was installed; setup stops here"

**What it means:** `setup` listed what the Mac lacks (the Command Line
Tools, Homebrew or MacPorts, Ansible) and you answered no. Nothing was
installed, and without Ansible the Mac cannot be synced.

**What to do:** Run `setup` again and answer yes, or install the listed
items yourself and run `setup` again: it finds them and installs nothing.

## "nothing was installed: run again with --install-prerequisites"

```
nothing was installed: run again with --install-prerequisites to install Xcode Command Line Tools and Homebrew
```

**What it means:** The Mac lacks the items named, and `setup` had no
terminal to ask whether to install them. `--yes` does not count as that
answer: it means other things, and the macOS app passes it in the
background. See [why consent has its own
flag](how-it-works/what-a-machine-needs.md#why-consent-has-its-own-flag).

**What to do:** If you agree to install them, run the same command again
with `--install-prerequisites`. Installing the Command Line Tools takes 5
to 10 minutes.

## "the bootstrap stopped at …"

```
the bootstrap stopped at homebrew: the Homebrew installer failed; its output is above. It needs passwordless sudo for the admin login.
```

**What it means:** The package manager package's bootstrap failed at the
step it names (`command-line-tools`, `homebrew`, `macports`, `ansible`),
and the rest of the message is its own advice. Steps before it are done;
running `setup` again skips them.

**What to do:** Do what the message says, then run `setup` again. The
most common cause is an admin login whose `sudo` asks for a password:
the bootstrap only uses `sudo -n`.

## "no mac-brew package with a bootstrap is available here"

**What it means:** On your own computer, `setup` runs the `mac-brew`
package's bootstrap from the package cache, and the pinned packages
release has no such package (or no release is pinned).

**What to do:** Run `devmachine packages pin` to pin the latest release,
then `devmachine setup --machine <name>` again.

## "machine X is your computer (self: true), so it has no hosts"

**What it means:** A server marked `self: true` in `config.yml` also has
`hosts`, `user`, `port` or `key` set — whichever the message names. Your own
computer has no address, so it cannot carry these.

**What to do:** Remove the field the message names. The same error appears,
one field at a time, for `user`, `port` and `key`.

## `sync` on a self machine fails with "ESTABLISH LOCAL CONNECTION FOR USER: root"

**What it means:** Ansible asks the shell it runs in who is logged in, not
your configuration. Some shells — a login shell started by a GUI app, cron,
or a launcher — leave `LOGNAME` set to `root` with `USER` empty. Ansible
believed it, looked for `/var/root`, and failed there instead of in your
own home.

**What to do:** Nothing — this is fixed for you. `devmachine sync` on a
`self: true` machine pins `ansible_user` in the generated inventory to
the account devmachine itself runs as (read from the operating system, not
from `USER`/`LOGNAME`), and exports the right `USER`, `LOGNAME` and `HOME`
around the `ansible-playbook` run. If you still see this on a current
release, run `whoami` and `echo $HOME` in the terminal you launched
devmachine from, and check they say what you expect.

## "… is not a usable location"

**What it means:** A `--location` flag, the answer to the location question,
or a `location:` in `config.yml` has a character outside the allowed set, or
is longer than 40. Capitals and spaces at the ends are fine: they are
lowercased and trimmed.

**What to do:** Use letters, numbers, spaces, dots, underscores and hyphens,
starting with a letter or number — `home office`, `rack-2`, `hostinger`. To
go back to the default, clear it with `devmachine machines edit <name>
--location ""`.

## A `tailscale:` address is being ignored

**What it means:** devmachine drops a `<prefix>:<name>` entry when the network
package says the network is not reachable from your computer — Tailscale is
not installed, not running, or does not know that name — and tries the next
address instead. This is by design.

**What to do:** Run `devmachine resolve`. It lists every entry it skipped,
with the reason the package gave. Then run `tailscale status` and check the
server is listed under the name you wrote.

## "no package declares the prefix"

**What it means:** A `hosts` entry is written `<prefix>:<name>`, and no
package in your pinned release or your own `packages/` folder declares that
prefix in a `network:` block. The entry is skipped.

**What to do:** Add the network package to the machine
(`devmachine packages add <package>`) and `sync`, or check the prefix for a
typo. For `tailscale:`, a packages release from before network packages
still works: the CLI's built-in resolver answers when no package does.

## "the X package's resolve did not answer within 5s"

**What it means:** The package's `resolve` script, which runs on your
computer, took too long, so its entry was skipped. The network's own
command is probably stuck: for Tailscale, `tailscale status` hangs too.

**What to do:** Run the network's own status command. Restart its app if
that hangs as well.

## "joining X did not finish … run `devmachine sync`"

**What it means:** `devmachine login <package>` ran the package's `join`
script on the machine, and it failed. The most common reason is that the
package is not on the machine yet: `join` runs from where `sync` put it.
Otherwise, the script's own output, above the error, says what went wrong.

**What to do:** `devmachine sync`, then `devmachine login <package>` again.

## `ssh <workspace>-devmachine` says "no address answered" or "Connection closed by UNKNOWN"

**What it means:** The alias connects through `devmachine ssh-proxy`, and
none of the machine's addresses accepted a connection. ssh shows the
proxy's error, which lists every address and why.

**What to do:** Run `devmachine resolve` to see which addresses were tried.
If it says `devmachine: command not found` instead, the CLI moved since the
alias was written: run `devmachine aliases --write` again.

## `devmachine ssh <workspace>` on a Mac ends with "exit status 255" and "Remote Login may not allow"

```
exit status 255: the Mac's Remote Login may not allow alice; `devmachine doctor --machine studio` checks it
```

The Mac's own log (`log show --predicate 'process == "sshd"' --last 5m`)
says `pam_sacl: denying 'alice' due to failed service ACL check`.

**What it means:** Remote Login on that Mac is set to "Only these users",
and the workspace account is not on the list (the group
`com.apple.access_ssh`). macOS refuses the login before it checks the
key, so `ssh` only sees the connection close. The CLI adds this hint only
for a machine it last saw as a Mac; exit 255 has other causes too, such
as an address that does not answer.

**What to do:** Run `devmachine doctor --machine <name>`. An `ssh access:
<workspace>` warning confirms it. Run `devmachine sync`, which adds each
workspace account to the list, or, on the Mac, choose "All users" in
System Settings > General > Sharing > Remote Login. See [what a machine
needs](how-it-works/what-a-machine-needs.md#remote-login-set-to-only-these-users).

## `devmachine run` exits 255

**What it means:** The command never ran: `run` could not find the
machine or workspace, could not connect, found a changed host key, or
the package is not installed there. The line after `error:` says which.
`ssh` uses 255 for the same thing, so a script can tell it apart from
the command's own failure. A command that exits 255 by itself, or that
was stopped with Ctrl-C or SIGTERM, also ends with 255.

**What to do:** Read the `error:` line. For a connection, `devmachine
doctor --machine <name>` checks the way in.

## "with --argv, the program and its arguments follow `--`"

**What it means:** With `--argv`, everything after `--` is the program
and its arguments. Without `--`, the arguments would be read as flags of
`devmachine` itself.

**What to do:** `devmachine run --argv -- df -h /`.

## "--script "…" names a file inside the package" or "… has no file …"

**What it means:** `run --package <name> --script <path>` runs a file of
that package, relative to its `package.yml`. A path that leaves the
package (`../`, or starting with `/`) is refused before anything
connects, and `run` exits 1. "has no file" means
your copy of the package does not have that file, so the machine's copy,
which `sync` made from yours, does not either.

**What to do:** Use the path as it appears inside the package folder,
for example `widgets/disk/check.sh`. If you just added the file, run
`devmachine sync` so the machine gets it.

## `doctor` says an alias has "a fixed address, expected one resolved when ssh connects"

**What it means:** The alias was written with the address itself, by an
older version or while `devmachine` was not on your `PATH`. It works until
that address stops working.

**What to do:** `devmachine aliases --write`. See
[SSH aliases that resolve when you connect](how-it-works/addresses-and-fallback.md#ssh-aliases-that-resolve-when-you-connect).

## `doctor` says `skip  ssh aliases  managed outside devmachine (ssh_aliases: false)`

**What it means:** `config.yml` does not have `ssh_aliases: true`, so
devmachine does not keep your SSH aliases. You manage `~/.ssh/config`
yourself, and doctor does not judge a file it did not write. Nothing is
wrong.

**What to do:** Nothing, if you keep `~/.ssh/config` on purpose. To have
devmachine keep the aliases and check them, run `devmachine aliases
--write` once and say yes. See
[SSH aliases](concepts/reaching-your-server.md#ssh-aliases).

## `doctor` exits non-zero, but most machines say `pass`

**What it means:** With several machines, `doctor` checks all of them and
exits non-zero when any one has a `fail`. The error names which ones, for
example `some checks failed on far`.

**What to do:** Look at that machine's block, or run `devmachine doctor
--machine far` to see only its checks.

## "package X runs on linux; machine Y is macos"

```
package "firewall" runs on linux; machine "studio" is macos
```

**What it means:** The package's `package.yml` lists the systems it runs
on under `platforms`, and the machine's system is not one of them. `sync`
(and `update`, which syncs) stopped before changing anything on the
machine. The machine's system is what `setup`, `sync` or `doctor` last
read from it — `devmachine machines show <name>` prints it — and `sync`
checks again on what it reads as it connects. The reverse, `runs on macos;
machine … is linux`, is the same stop for a Mac-only package on a Linux
server.

**What to do:** Take the package off that machine or workspace with
`devmachine packages rm <package>` (add `--workspace <name>` for a
workspace package), then sync again. If the package is your own and does
run on that system, add the system to its `platforms`. If the machine was
rebuilt with another system, run `devmachine doctor --machine <name>` so
the CLI reads it again.

A Mac added by an older CLI may list `essentials`, which runs only on
Linux. Swap it for what a new Mac starts with:
`devmachine packages rm essentials --machine <name>`, then
`devmachine packages add base --machine <name>` and
`devmachine packages add devmachine-app --machine <name>`.

## A setting is accepted, but the package still uses its default

**What it means:** A setting reaches the server as
`devmachine_<package>_<name>`, with dashes and dots turned into underscores.
If the package reads a different variable name, your value arrives but
nothing looks at it.

**What to do:** See what was actually sent:

```
devmachine run --machine main -- "cat /opt/devmachine/host_vars/devmachine.yml"
```

If your variable is there under a different name than the package's own
`defaults/main.yml` uses, the package needs fixing — the name in its defaults
is the name a setting has to match.

## `machines edit` or `workspaces edit` says it "does not install the package"

**What it means:** The setting's first part, before the dot, names a
package that machine or workspace does not have. Nothing would read the
value: the recipe would keep its default, so the CLI refuses it instead.
It is usually a typo in the package name, or a package added to the
workspace when the setting is on the machine (or the other way round).

**What to do:** Check the name with `devmachine packages list`. If the
package really is missing, add it first (`devmachine packages add <name>
--machine main`), then set the value.

## `devmachine ssh` opens a session as the wrong user

**What it means:** `devmachine ssh` with no argument logs you in as the
server's **root** account, not a workspace.

**What to do:** Name the workspace: `devmachine ssh acme`.

## Changes to config.yml appear to be ignored

**What it means:** devmachine may be reading a different file than you think.

**What to do:** Check which one:

```
devmachine config path
```

`DEVMACHINE_CONFIG` in your shell beats the default location, and a
`--config` flag beats everything.

## `secrets list` shows nothing after storing one

**What it means:** The list of names lives in your configuration directory,
even though the values themselves live in your keychain.

**What to do:** Check you are using the same configuration directory you used
when storing it — see `devmachine config path` above.

## `secrets set --env-file` refuses the path

**What it means:** `--env-file` takes a path relative to the workspace's own
home, and the one given would land outside it — an absolute path
(`/etc/passwd`), or one with enough `../` to walk out of the home
directory.

**What to do:** Give a path inside the workspace, such as `app/.env` or
`.env`. There is no way to deliver a workspace secret outside that
workspace's own home.

## `credentials push` says a path "reaches outside the workspace's home through a symbolic link"

**What it means:** The path looked fine on your computer, but on the
machine one of its directories — or the file itself, or its
`.devmachine.bak` — is a symbolic link that points outside the workspace's
home. The path is either the `--env-file` of a workspace secret, or where
a package delivers a workspace credential (`~/.devmachine/<name>/env`, or
the package's own `path`). The push runs as the machine's admin, and the workspace's own
account can create links anywhere in its home, so following one would let
that account aim a root write at any file on the machine. The push stops
before it reads or writes anything.

**What to do:** Look at the path on the machine (`ls -la` each directory
on the way). If the link is yours and meant, point `--env-file` at the
real file inside the home instead, or replace the link with a real
directory or file. If nobody in the workspace made it,
treat it as a warning about what runs in that workspace. A link that stays
inside the home is followed as usual.

## `sync` says a path "reaches outside the home of <account> through a symbolic link, so the shared login was not copied there"

**What it means:** A login shared across workspaces (such as `gh` or
`git-key`) lands at its `stored_at` path in each workspace's home. In the
named account's home, one directory on that path, or the file itself, is a
symbolic link that points outside the home. The copy runs as that
account, never as root, so the link could never have let it write a file
the account could not already write. But a login copied to some other place
would be lost or leaked, so `sync` stops at that account instead. Nothing is
written there, and nothing outside the home changes.

**What to do:** Look at the path in that workspace (`ls -la` each directory
on the way, then the file). Replace the link with a real directory or file,
or remove it, and run `sync` again. If you want that workspace to keep a
login of its own, opt it out with `devmachine workspaces edit <name>
--share <login>=own`. If nobody in the workspace made the link, treat it as
a warning about what runs in that workspace. A link that stays inside the
home is followed as usual.

## A workspace secret was pushed, but the app never sees it

**What it means:** `devmachine credentials push` writes the value into
`~/.devmachine/env` (or the `--env-file` you gave), but nothing runs that
file for you — that is the shell's job, not the CLI's.

**What to do:** For the default `~/.devmachine/env`, check the workspace's
shell actually sources it: the `workspace` package loads it through
`~/.devmachine/shellenv`, which zsh, bash and sh all read, from packages v37
on. Run `devmachine sync` if the machine is on an older packages release. For `--env-file`, check the app reads that exact file and reloads
its process after the value changes — this only writes the file, it does
not restart anything running.

## `dns status` says a name does not resolve, but it works in the browser

**What it means:** The check runs from **your computer**, not the server, and
it ignores your browser's cache and any proxy you use. A name that works in
the browser but not here usually means DNS has not finished propagating
everywhere, or something local — a VPN, an `/etc/hosts` entry — is resolving
it just for you.

**What to do:** Wait a few minutes and check again, or check your own network
settings for something overriding DNS.

## `dns add`/`dns rm`/`dns list`/`dns check` fail with one of these

Every DNS provider reports one of a fixed set of errors, shown here in plain
words:

| Error | What it means |
| --- | --- |
| `zone not found, or the token cannot see it` | This domain does not exist under this provider, or your token cannot see it. |
| `the token was rejected` | Your credential is wrong or expired. Store the new one with `devmachine secrets set` and run `devmachine credentials push`: it replaces the old copy on the machine. On Hostinger, a 403 before packages v26 was the provider itself, not your token — see below. |
| `the token cannot change this zone` | Your token can read the domain but not write to it. |
| `the record was rejected` | The registrar refused the value — a bad type, a bad value, or a name it will not accept. |
| `rate limited` | The registrar's API is temporarily blocking this token from making more requests. devmachine never retries this on its own; wait and run the command again. |
| `this record type is not supported yet` | Only `A`, `AAAA`, `CNAME` and `TXT` work the same way across every provider. `MX` and `SRV` are refused by name rather than guessed at. |
| `the name holds several values` | Two installed providers both claim this domain. Say which one with `--dns-provider`. |

Two more lines that are not errors, but change what a command does:

- **"no installed provider holds this zone"** — none of the providers you
  have installed recognized this domain. This is normal for a registrar with
  no devmachine package yet: the command falls back to `manual` and prints
  the record for you to create by hand.
- **"no DNS provider is installed"** — the machine has no DNS provider
  package at all, so the record is printed. `devmachine packages list`
  shows the providers you can add.
- **"… could not be reached, so no DNS provider on it was asked"** — a
  provider runs on the machine, so with the machine down nothing can be
  written or removed, and the record is printed instead. Once the machine
  answers, `devmachine dns add` or `devmachine dns rm` does it for you.
- **"the provider failed"**, with something that looks like a crash — this is
  a bug in the provider package, not in devmachine itself. The package sent
  back something that does not match the [DNS provider
  contract](reference/dns-provider-contract.md).

## Hostinger answers every call with HTTP 403, but the token works with `curl`

**What it means:** Before packages `v26`, the `hostinger` provider sent
Python's default User-Agent, and Hostinger's firewall answers that with a
403 — the same status a token without DNS access gets. Every `dns` command
said the token was rejected, and `expose add` fell back to printing the
record by hand.

**What to do:** `devmachine packages pin v26` (or later) and
`devmachine sync --tags hostinger`. A token without "Domains portfolio"
permission also needs the zones listed: `devmachine machines edit main
--set hostinger.zones=[example.com]`.

## `expose add` said "the DNS record for … was not written"

**What it means:** The site is on Caddy, but the DNS provider refused the
record; the reason is on the same line. Until the record exists, visitors
cannot find the site and Caddy cannot get its certificate.

**What to do:** Create the record the command printed, or fix the provider
(see the table above) and run `devmachine dns add <name> A <address>`.

## `expose rm` or `workspaces destroy` said "the DNS record for … was not removed"

**What it means:** The site is off Caddy, but the record that pointed
the name at the machine is still there: the DNS provider could not list
the zone, refused the delete, or its setup is broken; no DNS provider
is installed, or the machine could not be reached to ask one; or the
name also points at another address, and taking one value out of
several is left to you. The reason is on the same line. The name still resolves, and visitors get a TLS error
instead of nothing.

**What to do:** Remove the record the command printed, or fix the
provider (see the table above) and run
`devmachine dns rm <name> A <address>`.

## `expose rm` or `workspaces destroy` says a name "points at …, not at …: its DNS record is left alone"

**What it means:** The name's A record no longer holds the serving
machine's address, so `expose rm` did not touch it. Somebody repointed
the name after `expose add`, maybe at a new server.

**What to do:** Nothing, if the new value is right. If the name should
go, remove it with `devmachine dns rm <name> A <value>`.

## A certificate never arrives after `expose add`

**What it means:** First check `devmachine expose list`: a `pending` site
never reached Caddy (the machine was out of reach, or `--no-apply` was
used), and `devmachine sync` publishes it. A `published` site is on Caddy,
and Caddy (the reverse proxy) only gets a certificate for a name that
already points at the server. Two common causes:

- **The name does not resolve yet.** DNS can take a few minutes to catch up,
  even if `expose add` already wrote the record (or printed it for you to add
  by hand). Caddy keeps retrying on its own; `devmachine dns status <host>`
  tells you when it has caught up.
- **Caddy has not noticed the new site yet.** It watches for changes, but can
  miss one on a busy server.

**What to do:** Wait for DNS, or force Caddy to reload:

```
devmachine run --machine <name> -- 'systemctl reload caddy'
```

## `expose add` says "caddy refused the new … routes file, so the old one stays"

**What it means:** The route is in `config.yml`, but `caddy validate`
refused the whole configuration with the new file in place, so nothing on
the machine changed and Caddy serves what it served before. The lines
after the message are Caddy's own words. Two common causes:

- **Another file in `sites.d` already names the host.** Caddy refuses one
  host in two files. `devmachine expose list` shows it as `unmanaged`;
  remove that file, or drop this route and adopt the other one.
- **The configuration was already broken** by a file that has nothing to do
  with this route — something written by hand, or by a package. Caddy
  checks everything, so any broken file stops the new one.

**What to do:** Fix what Caddy names, then run `devmachine sync` — the
route is still recorded as `pending`. `devmachine expose rm <host>` drops
it instead.

## `expose add` says the route is pending

**What it means:** The route is in `config.yml`, and the machine does not
serve it yet. The message says why: the machine could not be reached, or
`caddy` is in the configuration but not installed there yet. It is not an
error, so the command still succeeds.

**What to do:** `devmachine sync` when the machine is back, or once more to
install `caddy`.

## `expose add --via` says "no address of … another machine can reach"

**What it means:** The machine with Caddy has to reach the workspace's
machine, and every address that machine has is a loopback one, such as the
`127.0.0.1` a local VM starts with. Only your computer can use that address.

**What to do:** Put the machine on your private network and run `devmachine
login tailscale --machine <name>`, which adds its `tailscale:` name to
`hosts`. Your own computer must be on that network too, since that name is
resolved there. See [share an app on your computer with
friends](guides/share-a-local-app-with-friends.md).

## `expose add` served, and the response is "Blocked request"

**What it means:** This is your application's own check, not Caddy and not
`expose`. Many frameworks refuse a `Host` header they do not recognize by
default, as a safety guard — and a name you just published is exactly the
kind of header the app has never seen before.

**What to do:** Add the published hostname to your application's own list of
allowed hosts. `expose` has nothing to do with that list.

## A sync cannot fetch the release

```
fetching https://github.com/.../packages-v1.tar.gz: the server answered 404
```

**What it means:** The pin in `config.yml` names a release that does not
exist.

**What to do:** Check `packages:` in your configuration against the tags the
packages repository actually has — a pin is a release tag, never a branch. If
the URL looks right, the problem is your network: this is a plain anonymous
download, so a proxy or firewall that blocks GitHub blocks this too.

Once a release is downloaded, it is cached — a later sync at the same pin
needs no network at all.

## "the asset changed, which a pin exists to prevent"

**What it means:** The file downloaded does not match the checksum the
release published. devmachine refuses it and stops, rather than send it to
your server.

**What to do:** This means the release was changed after it was published, or
something altered it in transit. Get the release fixed, or pin a different
one.

## `update` says "package release v34 has no package named "devmachine-skills""

```
==> 3/3 Skills
  failed: package release v34 has no package named "devmachine-skills"
```

**What it means:** Usually not that the release lacks the package. Before
0.7.32, two devmachine commands that downloaded the same release at the same
moment could delete each other's copy. `update` pins the new release and then
downloads it for the skills step, while the Mac app runs `packages list` on
its own. The command that finished second removed the release the first one
was reading, so the package seemed missing for a moment.

**What to do:** Run `devmachine skills update --yes`. The pin has already
moved, and the release is now complete in the cache. From 0.7.32, each
download works in its own folder and never removes a complete release, so the
first one to finish is the copy every command uses.

If the error comes back, the release really lacks the package: check
`devmachine packages list`.

## "package X needs a CLI >= 0.3.0, and this one is 0.2.1"

**What it means:** The package says which CLI version can read it, and yours
is older.

**What to do:** Upgrade: `brew upgrade devmachine`. Or pin an older release of
the packages instead, if upgrading is not your call to make.

## "package X is a workspace package, and main is a machine"

**What it means:** Some packages belong on a server (shared by everyone, like
Docker); others belong to one workspace (like Claude Code, which needs its
own sign-in per person). You tried to add this one in the wrong place.

**What to do:** Use the command the error shows, for example:

```
devmachine packages add claude-code --workspace acme
```

## "package X extends caddy.sites.d, but caddy is not installed on machine main"

**What it means:** This package adds files into a place another package
manages, so that other package has to be on the same server.

**What to do:**

```
devmachine packages add caddy --machine main
```

The same error appears if the package is installed but does not declare that
it provides that place — check its `provides:` against the `extends:` that
names it.

## A file a removed package left behind is still on the machine

**What it means:** `sync` only removes a file it remembers writing — see
[What sync removes](how-it-works/what-sync-removes.md). If a package was
removed from your configuration before you upgraded to the version that
started tracking this, `sync` never recorded that file, so it never cleans it
up.

**What to do:** Find it under the directory the extending package used
(`sites.d` for Caddy) and remove it by hand:

```
devmachine run --machine <name> -- 'rm /etc/caddy/sites.d/<package>-<file>'
```

Then reload Caddy, if it is on the server:

```
devmachine run --machine <name> -- 'systemctl reload caddy'
```

## `sync --check` fails on a machine nothing has been applied to yet

A dry run against a server with no packages applied yet reports failures
like:

```
No package matching 'docker-ce' is available
Could not find the requested service caddy: host
```

**What it means:** Nothing is actually wrong. A dry run changes nothing, so a
package's software repository is never really added, and the package it
would have provided genuinely is not there to look at yet.

**What to do:** Run `devmachine sync` for real once. After that, `--check` is
meaningful, because there is something on the server to compare against. Use
a dry run to preview a change to a server you already built — not to preview
the first build itself.

## `sync` asks and I answered nothing

**What it means:** An empty answer counts as no, and so does closing the
input. A command that changes a server defaults to changing nothing.

**What to do:** Pass `--yes` to skip the question, or `--check` to see what
would happen without being asked at all.

## `expose add --yes` or `dns add --yes` says "nothing was changed"

**What it means:** `--yes` only skips questions about files on your
computer. It never says yes to putting something on the internet, so the
publish question was still asked. In a script or a pipe nobody answered,
an empty answer counts as no, and the command stopped. It exits non-zero
so a script does not take "I did not do it" for "done".

**What to do:** Pass `--publish` to `expose add` or `dns add` when the
command runs without a person to ask. `--check` shows what it would do
first.

## `ERROR! Invalid options for include_role: devmachine_<package>_<name>`

**What it means:** This was a bug in how devmachine generated its internal
Ansible playbook, and it is fixed. If you see it, your binary predates the
fix.

**What to do:** Build or install a newer devmachine. Nothing on the server
was changed — the run stopped before it started.

## `credential "X" cannot be shared`

**What it means:** You asked to share this login (`X: machine`), but the
package that declares it says it cannot be — usually because the tool's
session file is tied to one device or browser, and a copy of it would not
work anywhere else.

**What to do:** Ask for `own` instead, and sign in once in each workspace.
If you know a copy really does work for that tool, the fix belongs in the
package: add `shareable: true` to its declaration.

## The shared login did not reach a workspace

**What it means:** Two common reasons, in this order:

- **Nobody has signed in yet.** The copy comes from a login already stored on
  the server, and `sync` skips a workspace rather than fail when there is
  nothing to copy.
- **That workspace keeps its own login.** A workspace with `<name>: own` in
  its `credentials:` is deliberately left out of the copy, so it never loses
  the account it signed in with.

**What to do:** Run `devmachine login <name>` then
`devmachine sync --tags credentials`, or remove the `own` line if you meant to
share after all.

## `credentials list` says `unknown`

**What it means:** The package never said where its tool keeps this login,
so devmachine has nowhere to look. The login may well be there.

**What to do:** Add `stored_at:` to the package's declaration, and the row
starts giving a real answer.

## "credential X belongs to a workspace: name it with --workspace"

**What it means:** Two workspaces sign in to the same tool with different
accounts, so "where do I log in" has no single answer.

**What to do:** Name the workspace. The list in the error shows every
workspace that uses it.

## "the connection dropped after part of the input was sent"

A machine with several addresses in `hosts:` is normally tried one address
after another until one answers. That is safe only while nothing has been
sent yet. This error means the first connection took part of a file or a
secret and then dropped. Sending the rest to the next address would leave a
cut-off file on the machine, so the CLI stops instead.

Run the same command again. If one address keeps dropping, move the stable
one first in `hosts:`.

## The SSH host key is not trusted

**What it means:** Configurations made by v0.6 and earlier have no stored
server identity. devmachine will not learn one silently.

**What to do:** Run `devmachine machines trust <machine>`, compare the
fingerprint it shows with your provider's dashboard or another source you
trust, and approve it. This command reads the key without logging in.

## The SSH host key changed

**What it means:** Something about the server no longer matches what
devmachine trusted before. This can mean a deliberate rebuild, a
configuration mistake, or an attack — devmachine cannot tell which, and will
not connect until you decide.

**What to do:** Run `devmachine machines trust <machine> --check`. It reads
the presented key without logging in, writes nothing, and prints both
fingerprints, a command to print the same key on the server, and the fix.
Run that command from the machine's own console — the provider's web
console, or `limactl shell` on the Mac that runs a Lima VM — never
through the SSH connection you are trying to verify. If the fingerprints match, run
`devmachine machines trust <machine> --replace`. `--yes` skips confirmation
— that never substitutes for `--replace`.

`run` can keep working for up to five minutes after the key changes. It
reuses the connection it opened last time, which was checked when it was
opened and still goes to the same server. The first new connection after
that checks the key again and stops with this error.

## The host key changed after restarting a Lima VM

**What it means:** Usually nothing bad. Lima hands the VM a new cloud-init
instance ID (`iid-<time>`) on every `limactl start`, so cloud-init treats
each boot as a new instance and, by default (`ssh_deletekeys`), deletes the
SSH host keys and writes new ones. Restarting the VM — to change its
memory or CPUs, or for any other reason — gives it a new host key. The
disk, the users and the Tailscale address stay the same. A different local
network address underneath Tailscale does not matter either: the key is
pinned to the machine's name, not to an address.

**What to do:** Check the fingerprint from the Mac that runs the VM, with
`limactl shell <instance>` — a local path to the VM, not the network path
you are trying to verify — and compare it as in
[The SSH host key changed](#the-ssh-host-key-changed). The files under
`/etc/ssh/ssh_host_*` carry the time of the last boot. Then replace the
key.

To keep the keys across restarts, tell cloud-init not to delete them, on
the VM:

```
echo 'ssh_deletekeys: false' | sudo tee /etc/cloud/cloud.cfg.d/99-keep-host-keys.cfg
```

The next restart keeps the key you trusted.

## `machines trust --expect` says "presented …, not the expected …; nothing was written"

**What it means:** `machines trust` reads the key again each time it runs,
and the key it got now is not the one you checked and passed to
`--expect`. The server changed its key between the two runs — a rebuild,
or a Lima VM restarted in between — or this run reached a different
server. devmachine writes nothing rather than trust a key you did not
check.

**What to do:** Run `devmachine machines trust main --check` again and
compare the new fingerprint from the machine's own console, as in
[The SSH host key changed](#the-ssh-host-key-changed). Pass the new
fingerprint to `--expect` only if it matches.

## The SSH trust file is malformed

**What it means:** The error names `<config>/known_hosts` and the bad line.
This file may hold entries for other servers too, so do not delete the whole
thing.

**What to do:** Fix that one line, or remove just that server's entry, then
run `devmachine machines trust <machine>` to approve it again. The file uses
plain OpenSSH `known_hosts` syntax.

## `login` says "the login did not finish: exit status N"

**What it means:** `login` opens a plain `ssh` session on the machine and
runs the tool's own sign-in command there. The number is how that session
ended:

- **255** — `ssh` itself failed: the machine did not answer, or it
  refused the key. The tool's sign-in never started.
- **Any other number** — the sign-in command ran and exited with that
  code: you cancelled it (Ctrl-C), or the tool failed. Its own message is
  above this line.

**What to do:** For 255, run `devmachine doctor --machine main`; it says
whether the machine answers and takes the key. For any other number, read
the tool's message above and run `devmachine login <credential>` again.

## "the login left nothing at …"

**What it means:** The login command ran, but the file the package promised
is not there. Either the sign-in was cancelled, or the tool actually stores
its session somewhere else than the package says.

**What to do:** Check where the tool really writes its session, and fix the
package's `stored_at`.

## `credentials push` says "nothing to deliver" and the value is out of date

**What it means:** An env file `push` delivered itself is replaced when its
value changed (`replaced`). A file a tool keeps for itself (`path:` in the
package) is never rewritten once it is there, so the server keeps the old
value.

**What to do:** Remove that file on the server first — its path is in
`devmachine credentials list` — then push again.

## I already committed a key

**What it means:** Removing a file from git's index is not enough. It stays
in every past commit — anyone with a clone, or the history itself if the
repository is ever made public, can still find it.

**What to do:**

1. **Rotate the key first.** Generate a new one and get it authorized
   wherever the old one was, so the copy in your history stops being able to
   get in anywhere.
2. Then untrack it: `git rm --cached <path>`, commit that, and add it to
   `.gitignore` if `devmachine setup git` had not already.
3. If the repository was ever pushed anywhere, rewriting history (with
   `git filter-repo`, or by deleting and recreating the remote) removes the
   key from what other people can see — but it was compromised the moment it
   was committed, whatever you do to the history afterwards. Step 1 is the
   one that actually fixes anything.

`devmachine setup git` refuses to run against a directory that already
tracks one of these paths, and says so — see
[versioning your configuration](how-it-works/versioning-your-configuration.md).

## `expose add` said "recorded", and the site does not answer

**What it means:** `add` only writes to your configuration. The change
reaches Caddy on the next `devmachine sync`.

**What to do:** Run `devmachine sync`, then `devmachine expose list` should
say `published`.

## `expose list` says `unmanaged`

**What it means:** The server has a site your configuration does not know
about — written by hand, or by an old version of `expose`. It works today,
but is lost the day the server is rebuilt.

**What to do:** Run the `expose add` the row prints, to adopt it. After that,
`sync` writes the workspace's own file and removes the old one, so Caddy
sees the host only once.

## `expose list` says `differs`

**What it means:** Your configuration and the server disagree on the port or
the owner of a host.

**What to do:** Run `devmachine sync` — your configuration is treated as the
source of truth, and the server is made to match it.

## "workspace X publishes Y, but caddy is not on machine Z"

**What it means:** This route has nowhere to go.

**What to do:**

```
devmachine packages add caddy --machine Z
devmachine sync
```

## "caddy is not on X, where Y lives: publish it through a machine that has caddy"

**What it means:** The workspace's machine has no Caddy, and another
machine in your configuration does. `expose` does not pick one for you.

**What to do:** Publish through the machine it names:

```
devmachine expose add Y <port> --host <host> --via <machine>
```

Adding caddy to X instead only helps when the internet can reach X.

## `expose add --via` warns "edge cannot reach lab at …"

**What it means:** The site is on Caddy, but the machine serving it could
not open a connection to the port on the workspace's machine. Visitors get
a 502 until that changes.

**What to do:** On the workspace's machine:

- The service must listen on the address the warning names, or on every
  address — not only on `127.0.0.1`. A container published as
  `127.0.0.1:8080:80` cannot be reached from another machine.
- A firewall must let the serving machine in on that port.
- Both machines must be on the private network the address belongs to:
  `devmachine resolve --machine <machine>` shows which address is used.

Nothing needs publishing again: Caddy retries on every request.

## `sync` says "… is left as it is on edge, since lab's address is not known"

**What it means:** The private network that gives the workspace's machine
its address is off or signed out on your computer, so the file on the
serving machine could not be written. It was left exactly as it was, and
the site keeps answering the way it did.

**What to do:** Turn the network on here and run `devmachine sync` again.
`devmachine resolve --machine lab` shows why the address is missing.

## `sync` failed at "reload caddy for the routes"

**What it means:** Caddy refused the new set of site files. The most common
cause is the same host named in two files — one from another package, or one
written by hand — as one the configuration also publishes.

**What to do:** Find the duplicate:

```
devmachine run 'caddy validate --config /etc/caddy/Caddyfile'
```

Remove it from whichever side should not own that host.

## `workspaces destroy` failed at userdel

On a Mac the step is `sysadminctl -deleteUser`, as macOS has no `userdel`;
the same applies. Before it, `destroy` takes the account out of
`com.apple.access_ssh`, the group Remote Login uses when it allows only
some users, so no stale entry stays behind.

**What it means:** Something is still running as that account — a process
started outside its login session, or a container. `destroy` already
disabled the account and stopped everything it could find, but something
outlived that. Your configuration was left untouched, so you can retry.

**What to do:**

```
devmachine run "ps -u <user>"
```

Stop what is listed, then run `devmachine workspaces destroy <name>` again.

## `workspaces destroy` refuses: "destroying the account would leave Caddy serving it"

**What it means:** The workspace publishes a site, and `destroy` could not
take it off Caddy first. Without that step the account would go, and Caddy
would keep serving the name to a port nobody owns. The message says which
case it is:

- **"the file that serves it cannot be found"** — devmachine could not
  work out where Caddy keeps its site files: `caddy` is no longer in the
  machine's packages, or the packages could not be read. The reason is in
  the brackets.
- **"through edge, which could not be reached"** or **"whose Caddy cannot
  be found"** — the site goes through another machine (`--via edge`), and
  that machine did not answer, or has no Caddy now.
- **"taking alice's sites off edge: …; nothing was destroyed"** — `edge`
  answered, but removing the site failed there.

Nothing was destroyed in any of these cases.

**What to do:** Once the machine answers, run
`devmachine workspaces destroy alice` again. If Caddy is really gone,
`devmachine expose rm app.example.com` each site and `devmachine sync`
first.

## `workspaces rm` says it "would keep serving it with nothing left to take it off"

**What it means:** The workspace publishes a site through another machine
(`--via edge`). The route lives in this workspace's entry, so forgetting
the workspace would also forget the route — while `edge` keeps serving
the name, with no command left that knows to remove it.

**What to do:** `devmachine expose rm app.example.com` first, then
`devmachine workspaces rm alice`.

## `machines rm` says a machine "still serves … for another machine"

**What it means:** Another machine's workspace publishes a site through
this one (`--via`). Forgetting this machine would leave its Caddy serving
the name, with nothing in the configuration that points at it.

**What to do:** `devmachine expose rm app.example.com` for each name the
message lists, then `devmachine machines rm edge`.

## `run` hangs, or answers with a login from a machine that no longer exists

**What it means:** `run` keeps its SSH connection open for five minutes and
reuses it (see [the `run` reference](reference/commands.md#run)). If the
server it was talking to changed address, was rebuilt, or was deleted, that
old connection can be left open and never answer again.

**What to do:** Close it. The connection lives in a socket file in
`<user cache dir>/devmachine/cm/` — or in `/tmp/dm-<uid>/` when that path
would be too long (see the next section). Its name is the first 16
characters of the SHA-256 of `<user>@<address>:<port>`:

```
name=$(printf '%s' '<user>@<address>:<port>' | shasum -a 256 | cut -c1-16)
ssh -O exit -o ControlPath=<that directory>/$name <user>@<address>
```

If you do not have the exact address, delete the stale socket file
directly from that directory instead. Either way, the next `run` opens a
fresh connection.

## "unix_listener: path … too long for Unix domain socket"

Shown as `machine "x": the file ssh shares one connection through, …,
makes a path longer than this system allows for a socket`, or in older
versions as `no address answered: … (unix_listener: path "…" too long for
Unix domain socket)`.

**What it means:** `run` and the other short commands share one SSH
connection through a socket file. A socket's full path has a size limit
set by the operating system: 104 bytes on macOS, 108 on Linux. While ssh
creates the socket it adds 17 more characters to the name. With a long home
folder, the path went over the limit, and ssh gave up before it connected.
Nothing is wrong with the machine or the network.

Versions after 0.7.27 use a 16-character name, and when even that does not
fit under your cache directory they use `/tmp/dm-<uid>/` instead. So on a
current version you should not see this. If you do, it is a bug: report it
with the path from the message.

**What to do:** update devmachine (`devmachine update`). If it then says
`/tmp/dm-<uid>` "is owned by another account" or "has mode …", that folder
is not safe to hold your connections: remove it (or `chmod 700` it if it is
yours) and try again.

## `upload` refuses a file or a folder

**What it means:** one of these, checked on your computer before
anything connects, or on the machine before anything is written:

- "is a folder: upload sends files" — `upload` sends single files. Send
  an archive instead (`tar czf notes.tgz notes`) and unpack it with
  `devmachine run`.
- "no such file or directory" or "cannot be read" — the path is wrong, or
  your account cannot read that file.
- "--dir … reaches outside the home" or "is outside the home" — the
  folder climbs out with `../`, or is an absolute path in another home.
  `upload` only writes inside the home of the account it sends to.
- "reaches outside the home through a symbolic link" — the folder, or one
  on the way to it, is a link to somewhere outside the home. Following it
  would put the file wherever the link points.
- "is not a folder" — a file sits where the folder should be.

With several files, the others are still sent; only the refused ones are
missing, and the command exits non-zero.

**What to do:** Pick a folder inside the home (the default,
`~/.cache/devmachine/uploads`, always works), or replace the link on the
machine with a real folder.

## `download` refuses a path

**What it means:** one of these, checked on your computer before
anything connects, or on the machine before anything is written here:

- "--to … no such file or directory" or "is not a folder" — the folder
  you named with `--to` does not exist on your computer. `download`
  never creates it, so a typo cannot scatter files somewhere new.
- "… does not exist" — the path is wrong, or it sits inside a folder the
  account cannot enter, which looks the same from that account. A
  relative path starts at the home of the account that reads it: the
  workspace's with `--workspace`, the admin's without.
- "… cannot be read" — the file is there, but the account that reads it
  has no permission. Read it as another account (drop `--workspace` to
  read as the admin), or change its permissions on the machine.
- "… is not a regular file" — a device, a socket or a pipe. Only files
  and folders can be downloaded.
- "/ is the whole disk" — name a folder inside it instead.
- "reading … : tar: …" — a folder was packed, but tar could not read
  something inside it, usually a file the account has no permission for.
  The archive is dropped rather than saved incomplete.

With several paths, the others are still downloaded; only the refused
ones are missing, and the command exits non-zero.

**What to do:** Check the path with `devmachine run --workspace <w> -- ls -la <path>`,
which reads as the same account.

## `run --package` fails with "exit status 127"

**What it means:** the machine has no file at the entrypoint path. The CLI
builds that path from your configuration, the way `sync` does: a package in
your own `packages/` directory runs from `/opt/devmachine/roles.local/<name>`,
and one from the release runs from `/opt/devmachine/roles/<name>`. If you
added or removed a local copy since the last `sync`, the CLI looks in one
directory and the machine still holds the package in the other.

**What to do:** Run `devmachine sync`, so the machine matches your
configuration again.

## "no package named X, and none is available"

**What it means:** Either the name is wrong, or no packages release is
pinned. With no `packages:` line in `config.yml`, only your own local
packages exist.

**What to do:** Run `devmachine packages pin`, which pins the latest release,
then `devmachine sync`. A configuration made by `setup` from version 0.7.7 on
is already pinned.

## `update` says "checksum mismatch for devmachine_…tar.gz"

**What it means:** The archive that arrived is not the one the release
published — a proxy rewrote it, the download broke half way, or something is
in between you and GitHub that should not be. Nothing was replaced: the new
file is checked before anything is written, and your CLI is exactly as it was.

**What to do:** Run `devmachine update` again. If it keeps failing, install by
hand with `curl -fsSL https://mydevmachine.sh/install.sh | sh`, which checks
the same checksum, from another network.

## `update --cli-only` says "\"dev\" was built from source"

**What it means:** This `devmachine` was built from the source code, not
installed from a release, so it has no release version and no install that
`update` could replace. Plain `update` skips the CLI step for such a build;
`--cli-only` has nothing else to do, so it fails.

**What to do:** Build it again from the source (`git pull && make build`),
or install a release with `curl -fsSL https://mydevmachine.sh/install.sh |
sh` and remove the built copy from your `PATH`.

## `update` says "replacing …: permission denied"

**What it means:** The CLI lives in a folder your user cannot write, such as
`/usr/local/bin`, so the new version could not be put in its place. Nothing
was replaced.

**What to do:** Reinstall it where you can write, with
`curl -fsSL https://mydevmachine.sh/install.sh | sh` (it installs to
`~/.local/bin`), and remove the old copy. Running `update` with `sudo` would
also work, but then the rest of the update runs as root, with root's
configuration.

## "… was installed by Homebrew, but brew is not on PATH"

**What it means:** The CLI's real file is inside Homebrew's `Cellar`, so only
Homebrew should replace it — and `update` could not find `brew` to ask.

**What to do:** Put Homebrew on your `PATH` (`eval "$(/opt/homebrew/bin/brew
shellenv)"` on Apple silicon), or run `brew upgrade mydevmachine/tap/devmachine`
yourself, then `devmachine update --skip-cli`.

## `update` says "still 0.7.17 after the upgrade"

**What it means:** The upgrade finished, but the binary that started next is
the same version. Usually Homebrew did not know the new release yet, or
another `devmachine` earlier on your `PATH` is the one that ran.

**What to do:** Run `which -a devmachine` to see every copy. Remove the ones
you do not use, then `brew update && brew upgrade mydevmachine/tap/devmachine`
or the install script.

## `doctor` says `warn  credential: gh  missing`

**What it means:** A package on the machine needs a login or a secret that
is not there yet. The machine works; only the tool that needs the login does
not. It is a `warn`, not a `fail`, so `doctor` still exits `0`.

**What to do:** Run the command in the detail: `devmachine login <credential>`
for a login, or `devmachine secrets set <name>` then `devmachine credentials
push` for a secret.

## `update` summary says `doctor  machine X unreachable`, and update exited 0

**What it means:** `doctor` could not reach machine X (configuration, host key
or connection failed), so `update` skipped its sync check; the sync line says
`X skipped (unreachable)`. That is about the machine, not about `update`, so
it does not change `update`'s exit code.

**What to do:** Run `devmachine doctor --machine X` to see which check failed,
fix it, then run `devmachine update` again or `devmachine sync --machine X`.

## `doctor` says `skip  cli  could not find the latest release`

**What it means:** GitHub could not be asked: you are offline, or GitHub
answered `403` because this address used up its 60 unauthenticated requests
for the hour. It says nothing about your CLI, which is why it is a `skip` and
not a failure.

**What to do:** Nothing, usually — the next `doctor` asks again. Once an
answer arrives, it is kept for 6 hours.

## `update` found changes and did not ask

**What it means:** There was no terminal to ask in — a cron job, a CI step,
or input from a pipe — and `--yes` was not given. With nobody to answer,
`update` applies nothing.

**What to do:** Run the `devmachine sync` command it printed, or pass `--yes`
if this is automation you trust to change servers.

## `update` says "--format json is not supported"

**What it means:** `update` asks a question and prints progress for a person.
There is no single JSON document it could promise.

**What to do:** For a script, use `devmachine doctor --format json` and
`devmachine sync --check --format json`.

## `packages outdated`: "cannot tell the latest packages release, and none was read before"

GitHub could not be asked which packages release is the newest, and no
earlier answer is stored in your cache folder. It is about the network
(offline, a proxy, or GitHub's limit of 60 unauthenticated requests an
hour), not about your configuration. Try again when online. Once an answer
is stored, the command works offline with that answer.

## A line says "packages vN is out (you pin vM)"

A packages release newer than the one `config.yml` pins is out. Nothing is
wrong: `devmachine update` moves the pin, and `devmachine update
--no-machines` does it without touching a machine. The line shows at most
once a day; `DEVMACHINE_NO_UPDATE_HINT=1` turns it off. See
[Updating](how-it-works/updating.md#why-the-cli-tells-you-a-newer-packages-release-is-out).

## "source.run "…" has spaces"

**What it means:** Without `shell: true`, `run` names one program, and
`args` holds its arguments, each passed as it is. `df -h /` is read as a
program called `df -h /`, which does not exist. Nothing ran.

**What to do:** Write `run: df` and `args: [-h, /]`. If you need a pipe
or `&&`, set `shell: true`; then `run` is a shell line.

## "source.run is a shell line, so it cannot hold {{…}}"

**What it means:** A value written into a shell line becomes part of the
code the shell runs, so a value such as `a; rm -rf ~` or `$(id)` would
run as a command. No way of quoting it is safe in every case, so a shell
line holds no templates at all.

**What to do:** Read the value from the environment variable the app
sets, in double quotes: `{{inputs.max_lines}}` becomes
`"$DM_INPUT_MAX_LINES"`, `{{context.workspace}}` becomes
`"$DM_CONTEXT_WORKSPACE"` (the workspace's name). The name is upper case,
and any character other than A–Z and 0–9 turns into `_`. Do not pass that
variable on to `eval`, `sh -c`, `ssh <host> …`, `awk` or `xargs`: they run
their argument as code. Bash arithmetic does too — `$(( ))`, `(( ))`,
`let`, `[[ … -gt … ]]`, `declare -i` run a value such as `a[$(id)]` — and
`/bin/sh` on a Mac is bash. Check a number with `case` or `[ … ]` first:
`case $DM_INPUT_MAX_LINES in ''|*[!0-9]*) exit 1;; esac`.

## "a shell line takes no source.args"

**What it means:** With `shell: true`, `run` is the whole line, so there
is no program for `args` to go to.

**What to do:** Write the words in `run`, and read values from
`$DM_INPUT_<NAME>` or `$DM_CONTEXT_<KEY>`. Or drop `shell: true` and keep
`run` as the program and `args` as its arguments.

## "source.run cannot hold {{…}}: a value must not pick the program"

**What it means:** Without `shell: true`, `run` is the program that
starts. If a value could fill it in, whoever sets the value would choose
what runs.

**What to do:** Name the program in `run` and put the value in `args`:
`run: du`, `args: [-sh, --, "{{inputs.path}}"]`. The `--` stops a value
that starts with `-` from being read as an option.

## "source.run "…" starts with -" or "has ="

**What it means:** Without `shell: true`, `run` names the program to
start. A first word that starts with `-` reads as an option, and a word
with `=` as a variable to set, so the program you meant would not run.

**What to do:** Put the program's name in `run` and options in `args`.
To set a variable for the program, set `shell: true` and write
`run: LC_ALL=C df -h /`.

## "source.script "…" is not executable" or "only its owner can run it"

**What it means:** The file a package widget runs is copied to the
machine with the same mode it has in the package. Without the execute
bit nobody can run it; with `chmod 700` only the machine's admin can,
and a workspace runs it as its own account.

**What to do:** `chmod 755 <the file>`, then run `devmachine sync` so the
machine gets the new copy.

## "a stream runs while the widget is on screen: remove source.every"

**What it means:** `mode: stream` keeps one command running and shows its
lines as they come, so "how often" and "how long" do not apply. A stream
also reads line by line, so `number` and `json` do not apply either.

**What to do:** Remove `every` and `timeout`, and use `parse: text`,
`lines` or `ansi`. For a value checked every so often, leave out `mode`.

## "view.kind "…" takes …, and this … source gives …"

**What it means:** Each view draws some kinds of output. A `gauge` needs
a number; a command parsed as `text` gives text. `devmachine widgets
schema` lists what every view takes.

**What to do:** Set `source.parse` to what the view takes, or pick a view
that takes what the source gives.

## "view.value picks what to show out of the JSON"

**What it means:** The source parses its output as JSON, which can hold
many values, and the view shows one. It needs to be told which.

**What to do:** Add `value: "{{json.<field>}}"` to the view, for example
`value: "{{json.disk.used}}"` for `{"disk": {"used": 41}}`.

## "view.ok … is not a rule"

**What it means:** A status rule is a comparison and a value: `< 300`,
`>= 99.5`, `== "up"`. Text can only be compared with `==` or `!=`.

**What to do:** Rewrite the rule in one of those shapes. Quote the whole
rule in YAML when it holds a quote: `ok: '== "up"'`.

## "view.ok … compares by size, and this … source gives text"

**What it means:** The status view's source gives text (`parse: text`, or
a `command` source with no `parse`), and a rule uses `<`, `<=`, `>` or
`>=`. Text has no size to compare, so the rule could never hold.

**What to do:** Compare with `==` or `!=`, like `ok: '== "up"'`. If the
output is a number, say so: `parse: number` on a `command` source, or
leave `parse` out on a `url` source to compare its status code.

## "requires engine >= 1.6, and this CLI implements engine 1.5"

**What it means:** The widget says it needs a newer engine than this CLI
has. Nothing else in it was checked.

**What to do:** `devmachine update`.

## "input X is a choice, and needs from"

**What it means:** A `choice` input is picked from a list, and `from`
says which list: `machines`, `workspaces` or `harnesses`. Without it, or
with another name, the app has nothing to offer.

**What to do:** Add `from: machines` (or one of the other two). Add
`many: true` when the person may pick more than one.

## "a widget with a choice input needs requires.engine \">= 1.5\""

**What it means:** An app on engine 1.4 or older has no chooser, so it
could not show the input at all. The widget's `requires.engine` still
lets such an app read it.

**What to do:** Write `requires: {engine: ">= 1.5"}`. An older CLI then
says "update" instead of hiding the widget for a reason nobody can read.

## "source.target: app/… is the app's own data, so it takes no target"

**What it means:** The widget reads one of the app's own providers, such
as `app/clock`. The app has that data itself, so there is nothing to run on
a machine and no time limit to set.

**What to do:** Remove `source.target` and `source.timeout`. To read data
from a machine, use a package provider (`<package>/<command>`) or a
`command` source.

## "a package with widgets needs requires.cli above 0.8.1"

**What it means:** The package has `widgets:`, but its `requires.cli` lets
a CLI older than 0.9.0 use it. That CLI ignores `widgets:` without a word,
so the widgets would never be checked or listed.

**What to do:** Add `requires: {cli: ">= 0.9.0"}` to `package.yml`.

## `widgets list` fails with "finding the latest packages release"

**What it means:** Nothing is pinned in `config.yml`, or there is no
`config.yml` yet, so the CLI asked GitHub for the latest packages release
to read widgets from, and could not reach it. The app shows this as "could
not fetch packages".

**What to do:** Check the network and run it again. Once a release is
pinned — `setup` pins one, and so does `devmachine packages pin` — and in
the cache, `widgets list` works offline.

## `widgets validate` says "… is neither a widget, a package nor a board"

**What it means:** The path exists, but `validate` cannot tell what to
check. It knows a path by what it holds: a file named `widget.yml`, a folder
with a `widget.yml`, a folder with a `package.yml`, or any other file, which
it reads as a board. This is a folder with none of those in it, such as a
package's `widgets/` folder or `<config>/boards/` itself. Nothing was
checked.

**What to do:** Pass the package folder (the one with `package.yml`), one
widget folder, or one board file. To check every board and every widget of
your own packages, run `devmachine widgets validate` with no path.

## "X does not fit the home area" (or another area)

**What it means:** The widget's `fits` does not include the area's layout
(`canvas` for Home, `stack` for both sidebars, `slot` for the menu bar,
`tabs` for its popover), or it requires a context key the area does not
give. `widgets add` says it before writing; `widgets validate` says it for
a board you wrote by hand. `devmachine widgets help <name>` lists the
areas it fits. In the menu bar the view must also draw one line: "does
not fit the menubar area: the X view cannot be drawn in the menu bar".

**What to do:** Pick a widget that fits the area, or, for your own widget,
add the layout to `fits`. A widget that needs `session` fits only the
context sidebar.

## "X needs context.session, which the sidebar area does not give"

**What it means:** The widget reads the selected session, and only the
context sidebar has one. The same goes for a widget written in a board
whose provider needs it ("which this board's area does not give").

**What to do:** Put it on the context sidebar instead.

## "X goes on a board once"

**What it means:** The widget says `single: true`: the app's workspace
list is one, since two copies would fight over the same selection and
order. On a missing `sidebar.yml` the CLI reads the default board, which
already holds the workspace list.

**What to do:** Keep one. To move it, use `devmachine widgets move`; to
bring it back after removing it, `devmachine widgets add
devmachine-app/workspaces --board sidebar`.

## "--at places a widget on Home's canvas" or "--after and --before order a sidebar's list"

**What it means:** Home is a canvas: a widget there has a position, given
with `--at`. The sidebars, the menu bar and its popover are lists: a
widget there has a turn, given with `--after` or `--before`. Each flag
only means something in its own kind of area.

**What to do:** On Home use `--at x,y` or nothing; in a sidebar, the menu bar or its
popover use `--after <id>`, `--before <id>` or nothing (the end of the
list).

## "the home area is a canvas: a widget there has a place, not a turn in a list"

**What it means:** `widgets move` orders a sidebar. Home has no order:
each widget has its own position, and they may overlap.

**What to do:** Drag it in the app, or change its `frame` in
`home.yml`. To move a widget in a sidebar, pass `--board sidebar` or
`--board context-sidebar`.

## "X cannot move next to itself"

**What it means:** `--after` or `--before` named the widget being moved.

**What to do:** Name the widget it should sit next to.

## "--after needs an id" or "--before needs an id"

**What it means:** The flag was given with an empty value, often from an
unset shell variable (`--after "$ID"`). The CLI does not read that as "not
given": it would put the widget at the end without a word.

**What to do:** Give the id, or leave the flag out.

## "no widget with id "X" on the sidebar board"

**What it means:** `--after`, `--before`, `move` or `remove` named an id
the board does not have. On a missing sidebar board the ids are those of
the default board (`workspaces`; `shortcuts`, `todo` and the others).

**What to do:** Check the ids in the board file, or with `devmachine
widgets validate`, and run the command again.

## "port: the X view is drawn only in stack, and this board's area is laid out as canvas"

**What it means:** A widget written in a board uses one of the app's
sidebar views, such as `app.publish-port`, and those are drawn only in a
list. Home is a canvas.

**What to do:** Move the widget to a sidebar board, or pick a view that
is drawn anywhere, such as `list` or `text`.

## "source.name app/session-context needs context.session"

**What it means:** The provider reads the session that is selected in the
app, so it only works in the context sidebar, which hands every widget
there that session. A widget only sees the context keys it declares.

**What to do:** Add `context: {session: required}` to the `widget.yml`.
The widget then fits only the context sidebar, which is right: there is
no session to read anywhere else.

## "fits canvas, and the X view is drawn only in stack"

**What it means:** The view is one of the app's sidebar views
(`app.workspaces`, `app.todo` and the others). They are lists, drawn as
tall as their content, so they have no shape on Home's canvas.

**What to do:** Write `fits: [stack]`.

## "places "X": the widget does not fit that area"

**What it means:** `places` asks the app to add the widget to an area
once. The widget cannot sit there: its `fits` lacks the area's layout, or
it requires a context key the area does not give (`session`, for example,
only the context sidebar gives). The app would skip it without a word.

**What to do:** Remove the area from `places`, or change `fits` or
`context` so the widget fits it. `devmachine widgets validate` lists the
areas it fits.

## "X is not available yet: add the package"

**What it means:** The widget reads something its package installs, and
that package is not on any machine or workspace in `config.yml` — or it
is, and no sync has applied it yet (`packages.lock` does not list it).

**What to do:** Run the command the message names: `devmachine packages
add <package> --machine <name>`, then `devmachine sync`.

## "the board has a problem, so it was not changed"

**What it means:** `widgets add` or `widgets remove` read the board and
found a mistake, listed under the message with its line. The CLI never
rewrites a broken board: it would replace your text with its guess and
could drop widgets.

**What to do:** Fix the lines it names, check with `devmachine widgets
validate`, and run the command again. The app keeps the last good layout
meanwhile.

## "the board changed on disk since it was read; run the command again"

**What it means:** Something else — usually the app, after you moved a
widget — wrote the board between the moment the CLI read it and the moment
it was about to write. Writing anyway would have lost that change.

**What to do:** Run the same command again.

## "there is no home board at …"

**What it means:** `widgets remove` found no Home board file, so there
is nothing to take off. Nobody has placed a widget there from the CLI, or
the app has not saved a layout yet. `widgets add` creates the file;
`remove` never does on Home. A missing sidebar board is different: it is
read as the default board, so `remove` and `move` work on it.

**What to do:** Check `--board` and the config directory the path names.
To see what is on a board, open `<config>/boards/<board>.yml`.

## "a widget written in the board needs a title"

**What it means:** A board entry with a `source` or a `view` and no
`type` is a widget written in place. It needs a `title` (what the card
shows), a `source` and a `view`. An entry with a `type` and a `source`
mixes the two kinds and is refused too.

**What to do:** Add the missing key, or, for a widget from the gallery,
keep only `type` and `with`.

## "a widget written in a board names an absolute path on the target"

**What it means:** A widget in a board belongs to no package, so a
relative `script` has nothing to be relative to.

**What to do:** Write the full path on the target, for example
`/home/alice/bin/check-disk`, or use `run` with `args`.

## "a widget in a sidebar has no frame; its place is its position in the list"

**What it means:** A sidebar board is a list: the first widget is drawn at
the top, the next under it. `frame` and `z` place a widget on Home's
canvas and mean nothing here. The same message says `z` for a `z` key.

**What to do:** Delete the `frame` (or `z`) line. To change the order,
move the entry in the file, drag its header in the app, or run
`devmachine widgets move <id> --before <other> --board <area>`.

## "a widget in a sidebar folds with collapsed: true, not minimized"

**What it means:** Home folds a widget into a pill (`minimized`); a
sidebar folds it to its header (`collapsed`). Each area takes only its own
key. The opposite message, "a widget on a canvas folds with minimized:
true, not collapsed", is the same mistake on Home.

**What to do:** Rename the key.

The same message comes for `minimized: false`: a stack refuses the
`minimized` key whatever its value. Delete the line.

## "size auto follows the content, and the X view does not grow"

**What it means:** `auto` makes a widget as tall as what it shows. Only
the app's sidebar views (`app.workspaces`, `app.todo` and the others) work
that way; every other view has a fixed shape and needs a preset.

**What to do:** Use one of the sizes the message lists, or leave `size`
out to get the widget's `default_size`.

## "size "X" in a sidebar is auto or one of …"

**What it means:** A sidebar widget takes `auto` or a preset it lists in
`sizes`. `custom` exists only on Home, after a free resize.

**What to do:** Pick a size from the list in the message.

## "the menubar holds 3 widgets"

**What it means:** `menubar.yml` has more than three entries, or `widgets
add` would make it four. The menu bar title shares a thin strip with the
clock and every other app's items, so it holds three short widgets.
`widgets add` adds "take one off first with `devmachine widgets remove
<id> --board menubar`"; on a missing `menubar.yml` it counts the two
default widgets.

**What to do:** Take one off with `devmachine widgets remove <id> --board
menubar`, or put the widget in the popover instead (`--board
menubar-panel`) if it fits there.

## "a widget in the menu bar has no size: it is one line of text, so it takes no --size" or "a tab fills the menu bar popover, so it takes no --size"

**What it means:** `widgets add --size` sets how big a widget is on Home
or in a sidebar. A menu bar widget is one line and a tab fills the
popover, so neither has a size.

**What to do:** Leave `--size` out.

## "a widget in the menu bar has no size: it is one line of text"

**What it means:** A menu bar widget is one line, so it has no `size`.
It has no `frame` or `z` either (its place is its turn in the list), and
it does not fold (`collapsed`, `minimized`). The message names the key
it found.

**What to do:** Delete that line. To change the order, move the entry in
the file or run `devmachine widgets move <id> --before <other> --board
menubar`; to hide a widget, remove it.

## "the X view cannot be drawn in the menu bar"

**What it means:** The menu bar title draws text and at most one symbol,
so only the `text`, `number`, `status` and `app.brand` views work there.
A widget written for engine 1.0 may list `slot` in its `fits` with
another view, such as `app.clock`; it is still a good widget, it just
does not fit the menu bar. A widget with no `view.kind` at all gets "a
widget with no view cannot be drawn in the menu bar" instead.

**What to do:** Put it on Home, a sidebar or the popover
(`devmachine widgets help <name>` lists where it fits), or write a widget
with one of those four views.

## "a tab in the menu bar popover has no frame; its place is its position in the list"

**What it means:** Each widget in `menubar-panel.yml` is a tab, in the
order written. `frame` and `z` place a widget on Home and mean nothing
here; a tab does not fold either, so `collapsed` and `minimized` are
refused with "does not fold".

**What to do:** Delete the line. Reorder with `devmachine widgets move
<id> --before <other> --board menubar-panel`.

## "warning: … size is ignored in the menu bar popover: a tab fills it"

**What it means:** A tab always fills the popover, so its `size` does
nothing. It is only a warning: the board is valid, and the CLI keeps the
line when it rewrites the file.

**What to do:** Delete the `size` line if you want the warning gone.

## "source.target names machine "…", which config.yml does not have"

**What it means:** A widget written in a board runs on a machine or a
workspace your configuration does not have — removed, renamed, or a
typo. The app shows the widget's error instead of data. For a package
widget this is only a warning: its author's names are not yours.

**What to do:** Point `target` at a name `devmachine machines list` or
`devmachine workspaces list` shows, or remove the widget. `widgets add`
and `remove` still work on the board in the meantime.

## "provider X is not one of commands"

**What it means:** `package.yml` lists X under `providers`, but not in
`commands`. `devmachine run --package` refuses a command that is not in
`commands`, so a widget reading X would never get an answer.

**What to do:** Add X to `commands`, or remove it from `providers`.

## "provider "X": use lower case letters, digits, dashes and underscores, starting with a letter"

**What it means:** A key under `providers` in `package.yml` is not a name
a widget can use. A widget names a provider as `<package>/<command>`, and
the app runs it as `devmachine run --package <package> <command>`, so the
key must be a plain command word: no capital, space, dot, slash or leading
dash.

**What to do:** Rename the provider and the matching entry in `commands`,
for example `disk-usage` instead of `Disk Usage`.

## "`providers` are commands of an `entrypoint`, and this package declares none"

**What it means:** `package.yml` has a `providers` key but no
`entrypoint`. A provider is one command of the package's entrypoint: the
app runs the entrypoint with that command and reads the JSON it prints.
Without one there is nothing to run.

**What to do:** Add `entrypoint: bin/<name>` pointing at an executable in
the package, with each provider listed in `commands`. `devmachine packages
new` writes one. If the package has no command to offer, remove
`providers`.

## `package.yml` says "did not find expected ',' or '}'"

**What it means:** A line written between `{` and `}` holds a value YAML
cannot read there. The usual one is an optional type in `returns`:
`{note: string?}`. Inside braces, `?` needs quotes.

**What to do:** Write `{note: "string?"}`, or put each field on its own
line under `returns:`, where no quotes are needed.

## "provider X min_every … is below the 5s floor"

**What it means:** A widget may run a package's command at most every 5
seconds; a provider cannot promise more, because each run is a connection to
a machine.

**What to do:** Write `min_every: 5s` or more. Pick what the command can
really afford: one that reads every container on a machine wants `30s`.

## "a package widget reads only its own package's providers"

**What it means:** A widget in package A names `B/<command>`. The app
shows a widget only when its package is on the machine, so a widget that
read another package's command could look ready while that package is
missing.

**What to do:** Ship the widget in package B, or write it straight into a
board: a widget written in a board may read any package's provider.

## "X has no provider C" or "no package X"

**What it means:** The widget names `X/C`, and package X does not list C
under `providers` in its `package.yml` — or there is no package X in the
pinned release or your own packages. A board widget is checked by
`devmachine widgets validate`; `widgets add` and `remove` keep working, so
removing a package never locks a board.

**What to do:** Check the name against `devmachine widgets list --format
json` (its `providers` lists every one), or add C to the package's
`providers`.

## "a package provider runs on a machine or workspace"

**What it means:** A package's command lives on the machine its package
was synced to. There is nothing to run on your own computer.

**What to do:** Add `target: {machine: <name>}` or `target: {workspace:
<name>}`. To run something on your computer, use a `command` source.

## "source.with.X: … gets it as --X"

**What it means:** Each key of `with` becomes a flag, `--X`, before its
value. A key with capitals, spaces or a leading dash would turn into a
different flag than the one you wrote.

**What to do:** Write the key in lower case letters, digits, dashes and
underscores, starting with a letter. The value can be any text.

## "a widget reading a package provider needs requires.engine \">= 1.3\""

**What it means:** The app's engine 1.2 does not know package providers,
and its `requires.engine` lets that app try.

**What to do:** Write `requires: {engine: ">= 1.3"}`.

## "source.name X: is written <package>/<command>, …"

**What it means:** A provider name with a slash that does not start with
`app/` names a package's command. The CLI and the app turn that name into
a package folder and a command to run on a machine, so it must look like
one: a package name and a command name, each in lower case letters,
digits, dashes and underscores, starting with a letter. A template, a
space, a capital or a leading dash is refused. Unlike "no package X",
`widgets add`, `move` and `remove` refuse this too, because a board that
holds such a name is unsafe to hand to the app.

**What to do:** Fix the name in the board, for example
`devmachine-app/stats`. `devmachine widgets list --format json` lists
every provider under `providers`.

## Installing from a `git@` address fails with "Host key verification failed" or "Permission denied (publickey)"

**What it means:** A `git@host:path` address goes through `ssh`, and `ssh`
would normally ask its own questions: whether to trust a host it has
never seen, or the passphrase of your key. The CLI runs it with
`BatchMode=yes`, so it never asks — nobody may be at a terminal to answer,
the macOS app included — and fails at once instead. It also ignores a
`GIT_SSH_COMMAND` set in your shell for the same reason; `~/.ssh/config`
still applies.

**What to do:** Run `ssh -T git@<host>` once to trust the host, and load
your key into the SSH agent with `ssh-add`. Then install again. An
`https://` address of a public repository never needs either.

## "… its widgets are treated as third-party"

**What it means:** A package in `<config>/packages/` has a
`.devmachine-source.yml`, the file `packages install` writes, and it does
not read. The CLI cannot tell where the package came from, so it treats
it as the least trusted kind: its widgets that run code ask first.

**What to do:** Run `devmachine packages update <name>` to fetch it again
and rewrite the file. If you wrote the package yourself, delete the file.

## "X is an official package: a package from a git address cannot take its name"

**What it means:** The pinned packages release already has a package
called X. A package in your own folder always wins over the release's of
the same name, so installing this one would quietly replace the official
X on every machine that has it.

**What to do:** Ask the package's author to rename it. If you wrote it,
rename it: `name` in `package.yml`.

## "you already have your own package X"

**What it means:** `<config>/packages/X/` exists and you wrote it — it has
no `.devmachine-source.yml`. `install` never writes over your own work.

**What to do:** Rename one of the two, or move yours away first.

## "X is already installed from …"

**What it means:** You installed X from a git address before. When the
message says only "from a git address", its `.devmachine-source.yml` does
not read, but it is there, so the folder is still not yours to overwrite.

**What to do:** `devmachine packages update X` fetches it again.

## "… has no package.yml at its top"

**What it means:** The repository is not one package: `package.yml` is
not in its top folder. A repository holding several packages, or a
package in a subfolder, cannot be installed.

**What to do:** Ask the author to publish the package in its own
repository, or copy its folder into `<config>/packages/` yourself.

## "… is a link leading outside the package"

**What it means:** The repository holds a symbolic link to a file outside
the package. `sync` would copy whatever it points at — a key on your
computer, for instance — to every machine the package is added to.

**What to do:** Do not install it. Tell its author: a package holds its
own files.

## "the package at … has a character that moves or hides text in a terminal in …"

**What it means:** A file name in the repository, or the package's
summary, a command, a provider, a credential or a package it needs, holds
a control character (ESC, a carriage return, DEL and the like) or a
Unicode bidirectional mark or override, or a file name is not UTF-8.
Printed as it is, such a name can move
the cursor and erase the lines above it — the list of tasks, or the
warning that they run as root — so the prompt would show less than the
package brings. The message prints the name with the character written
as an escape, such as `\x1b` or `\u202e`. Nothing was written.

**What to do:** Do not install it. Tell its author which name it is: a
package names its files and what it brings in plain text. A
repository that does this on purpose is not one to trust with root.

## "fetching …: …"

**What it means:** `git` could not fetch that address or ref. Its own
message follows: a repository that does not exist, a ref that is not
there, no access to a private repository (git does not ask for a password
here, and `ssh` does not ask anything), or no network.

**What to do:** Check the address and the ref in a browser or with `git
ls-remote <address>`. For a private repository, use a `git@` address with
an SSH key that has access, loaded in your SSH agent.

## "git is not installed"

**What it means:** `packages install` and `update` fetch with `git`, and
it is not on your PATH.

**What to do:** Install git (on a Mac, `xcode-select --install`).

## "the package at … has N problem(s)"

**What it means:** The fetched package does not pass `packages validate`.
Every problem is listed with its file and line. Nothing was written.

**What to do:** Tell the package's author; each line says what to fix.

## "… appeared while X was being fetched: nothing was written"

**What it means:** `packages install` checked that `<config>/packages/X/`
did not exist, fetched the package, and found the folder there when it
went to move it in: another command or you made it in the meantime.
Install never writes over a folder, so it stopped.

**What to do:** Look at what is in `<config>/packages/X/`. If it is a
copy you do not want, delete it and install again.

## "checking whether X is an official package: …"

**What it means:** Before installing, the CLI reads the pinned packages
release to make sure X is not one of its names, and that read failed for
a reason other than "no package named X" — a damaged download in
`<config>/cache/`, for instance. Not knowing, it refuses rather than risk
replacing an official package.

**What to do:** Fix what the rest of the message names. For a damaged
cache, delete `<config>/cache/packages/<release>/` and run the command
again; it downloads the release afresh.

## "… putting the old copy back failed: …; it is in …"

**What it means:** An update moved the old copy of a package aside, could
not move the new one in, and then could not move the old one back. Both
moves are renames inside the same folder, so this means the disk or its
permissions changed under the command. The old copy is not deleted: it
stays at the path the message names, and the next fetch never sweeps it.

**What to do:** Fix the disk or the permissions, then move that folder
back to `<config>/packages/X/` yourself.

## "X is your own package, not one installed from a git address"

**What it means:** `packages update` and `packages remove` work only on a
package installed with `packages install`, which leaves a
`.devmachine-source.yml` in its folder. X has none: you wrote it, and the
CLI never fetches over it or deletes it.

**What to do:** Edit your package in `<config>/packages/X/`. To delete
it, delete the folder yourself.

## "X is still on machine …: take it off first"

**What it means:** `config.yml` still lists X on that machine or
workspace. Deleting the package would make the next `sync` fail to find
it.

**What to do:** `devmachine packages rm X --machine <name>` (or
`--workspace <name>`), `devmachine sync`, then `devmachine packages remove
X`.

## "X is in the future workspace defaults"

**What it means:** `defaults.workspace` in `config.yml` lists X, so every
workspace you create from now on starts with it. Deleting the package
would make creating the next workspace fail to find it.

**What to do:** `devmachine workspaces defaults --rm X`, then
`devmachine packages remove X`.

## "the fetched copy of X was swept away while waiting for your answer"

**What it means:** `packages install` or `update` fetches into
`<config>/packages/.install-…/` and then asks. A fetch folder older than
an hour is taken as one a Ctrl-C left behind, and the next `install` or
`update` deletes it. The question stayed open long enough for that to
happen, so there is nothing left to install.

**What to do:** Run the command again and answer the question.

## A package is gone after a Ctrl-C during packages update

**What it means:** `update` swaps the new copy in with two renames: the
old folder moves to `<config>/packages/.install-…/.previous`, then the new
one moves into its place. A Ctrl-C between the two leaves neither in
`<config>/packages/X/`. Nothing is lost: both copies are inside that
`.install-…` folder, and the hourly sweep never deletes a folder that holds
a `.previous`.

**What to do:** Find the folder with `ls -a <config>/packages/`. To keep
the version you had, move `.install-…/.previous` back to
`<config>/packages/X`. To take the new one, move `.install-…/X` there
instead; it was already checked and records its new commit. Then delete
the `.install-…` folder.

## "X is installed from …, and packages release vN has an official X"

**What it means:** `sync` found a package installed from a git address
whose name the pinned release now has too: a `packages pin` brought a
release that added it after you installed yours. A package in your own
folder always wins over the release's, so your copy would replace the
official one on the machine without a word. `sync` stops before it
connects instead of picking one, since the two share a name, not an
author.

**What to do:** To use the official one, take the third-party one off
with `devmachine packages rm X --machine <name>` (or `--workspace
<name>`), delete it with `devmachine packages remove X`, then add the
official one back and sync. To keep the third-party one for now, pin the
release you had before with `devmachine packages pin <release>`.

## "… now holds a package named Y, not X"

**What it means:** The repository X was installed from now holds a
package with another name. Updating would leave the configuration naming
X while the folder holds Y.

**What to do:** `devmachine packages remove X`, then `devmachine packages
install <address>`, and change X to Y wherever `config.yml` names it.

## "the address recorded in …/.devmachine-source.yml: …"

**What it means:** `packages update` checks the address it finds in the
package's `.devmachine-source.yml` the same way `packages install` checks
the one you type, and that address is not an `https://` or `git@` one.
Somebody edited the file by hand, so nothing was fetched.

**What to do:** Put back the address the package came from, or
`devmachine packages remove X` and install it again.
