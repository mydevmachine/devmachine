# Setting up a server for the first time

Setting up a server you have never logged into is the hardest moment this
CLI handles, and the first thing a new user hits. This page explains what
`devmachine setup` does, and why the steps run in this order.

## It does not ask which situation you are in

Most providers let you paste an SSH key in when you create the server, so
a lot of people arrive with a key that already works — asking them for a
root password would ask for something they never had. So `setup` finds
out for itself: where the machine is, which account and port, which key
to use, then **tries the key first**. Only if that fails does it ask for
the password.

## It checks what the machine runs first

The first thing `setup` does on a connection is ask the machine what it
is: `uname -s` for the kernel, then `ID` in `/etc/os-release` on Linux.
It sets up `debian`, `ubuntu` and `arch` (and `archarm`, Arch on ARM).
Anything else stops the run **before the first change** — before the key
is installed, on a server you reached with a password:

```
this CLI does not set up "fedora" yet: it supports debian, ubuntu and arch
"FreeBSD" is not a system this CLI sets up: it supports Linux (debian, ubuntu, arch)
```

Why refuse instead of trying: every step after this one is written for a
system it has been run on. A guess gets you halfway through a first run —
a key installed, password login off — and then leaves you on a machine
that cannot install Ansible. Stopping early leaves the server as you
bought it. `doctor` makes the same check and accepts the same list.

## The proof is a new connection

Installing a key does not mean it works — a wrong file permission, a
world-writable home folder, SELinux, any of these can let a key install
cleanly and still refuse every login. The connection that installed the
key cannot prove it works either, since that connection used the
password. So `setup` closes it, opens a **brand new connection using only
the key**, and only counts that as proof.

## Locking down comes after the proof, never with it

Turning password login off before proving the key works is locking the
door with the key still inside. If the proof fails, `setup` stops and
says plainly:

```
password login is still on: you can still get in with the password
```

`--no-harden` skips locking the server down; the key is still installed
and proved.

## `machines add` writes the machine last

`machines add` records the new machine only after the whole bootstrap
worked: key proved, password login off, Ansible installed. Both files
wait for that moment — `config.yml` and `known_hosts`. Until then the
host key you approved is trusted in a file of the run's own, which is
thrown away when the run ends. If any step fails, both files are as they
were, so you fix the cause and run the same command again: it is not
refused as a name already configured, and a corrected address or a
rebuilt server that presents another host key is not refused as a
changed key either. The new key still has to pass `--fingerprint` or
your yes, like any first contact.

A host key already in `known_hosts` under a name that is not configured
— left by an older CLI, or by a machine you removed — is replaced, not
compared: a name that is not a machine has nothing to protect.

What a failed run leaves is harmless to the next one: the key it made is
reused, and a key it already installed now logs in, so no password is
asked for.

`setup` is different on purpose: it writes `config.yml` first, and
running it again resumes from that file instead of starting over.

## A local machine trusts its first host key

Everywhere else, an unattended run needs `--fingerprint`: trusting
whatever answers, with nobody looking, is how a wrong server gets your
key. `machines create-local <name> --add` is the one exception. It
trusts the key the VM presents, because the same command created that VM
a moment ago and the VM answers only on `127.0.0.1`, through a port Lima
forwards on your own computer — there is no network in between for
anybody to stand on.

Locking down means writing one file that turns password login off,
checking it is valid, and only reloading SSH if it passes — **an invalid
file is deleted**, since a bad file left behind would break the next SSH
reload, even a routine reboot.

### Why the file name starts with `00`

SSH uses the **first** value it finds for each setting, so a drop-in file
only wins if it sorts before others. A cloud image often ships its own
settings as `60-cloudimg-settings.conf`; a file named `99-` would read
after it and **silently do nothing** — no warning, password login still
on despite a successful run.

### Then it asks SSH, not the file

A valid file is not proof that SSH reads it. A server whose main
`sshd_config` has no `Include` line for `sshd_config.d`, or sets
`PasswordAuthentication yes` above that line, accepts the file and ignores
it. So after the reload `setup` runs `sshd -T`, which prints the settings
SSH is really running with, and requires `passwordauthentication no`. If
SSH still allows passwords, the file is removed, SSH is reloaded without
it, and `setup` stops — it never says "password login is off" about a
server that still takes passwords.

## An admin login that is not root

Many servers arrive with root closed and an ordinary account that has
`sudo`: a home server, a VM made by Lima or Multipass, some providers'
images. The CLI works the same on them, as long as that account's `sudo`
asks for no password.

Everything that changes the system — turning password login off,
installing Ansible, unpacking a sync's bundle under `/opt/devmachine`,
running the play — first asks `id -u`. Root runs it directly; any other
account runs it through `sudo -n`. A server where the admin is root never
sees `sudo` at all, and neither does your own computer as a `self`
machine.

Either way the step runs in `bash` when the server has it, and in `sh` only
when it does not. Root's login shell was `bash` before any of this existed,
and the two differ where it matters: `sh` on Debian and Ubuntu is `dash`,
which stops a whole command when it cannot read a file it was told to load.
A package's `help` loads its credential first, and before `credentials push`
that file is not there yet; `help` must still answer.

It asks, rather than trying without `sudo` and trying again with it. A
retry would run a half-finished step twice, and would read any failure at
all — a full disk, a refused SSH setting — as a missing permission.

`-n` because nobody is there to type a password. So before anything is
changed, `setup`, `machines add` and `sync` check that `sudo -n true`
works, and stop with one sentence when it does not:

```
the admin login cannot become root: "alice" is not root, and `sudo -n true` fails as it.
Log in as root, or give it passwordless sudo on the machine:
echo 'alice ALL=(ALL) NOPASSWD:ALL' | sudo tee /etc/sudoers.d/devmachine-alice
```

The whole play runs as root, not only the tasks that ask for it. A task
that becomes a workspace account would otherwise go from one unprivileged
account to another, which Ansible refuses without ACLs on its temporary
files.

## Tailscale SSH proves nothing about a key

A server with Tailscale SSH turned on answers port 22 on its tailnet
address with Tailscale, not with `sshd`. Tailscale lets anyone on your
tailnet in by tailnet identity and accepts any key, or none. "The key
already logs in" would be true and mean nothing: the key was never
checked, and may not be on the server at all.

So when the server identifies itself as Tailscale, `setup` and
`machines add` install the key anyway and say why, instead of taking the
login as proof — and leave password login as it was, since locking down
comes after the proof and there was none. Without the key, the machine is unreachable the day
Tailscale SSH is off, or from anywhere outside the tailnet. Running
`devmachine setup --machine <name>` again repairs a machine an older CLI
set up this way.

## The password is never stored

It lives in memory for one connection, then is gone — never written to
`config.yml`, the lock file, or the log. Typed at a terminal, it is never
shown on screen.

## Why this does not just run the `ssh` command

If your SSH agent holds several keys, a password login can fail **before
you see the prompt**: the agent offers key after key, and the server cuts
the connection after too many tries — on a server you are meeting for the
first time, that happens every time.

`setup` talks to SSH directly in code, so it offers exactly one thing at
a time — the key, or the password, never both. Sessions you use
yourself, `ssh` and `mosh`, still run the real command, since there you
want your own terminal and agent.

## After this step

`setup` installs `ansible` and `git` — the last thing done by hand. From
then on, every change goes through `devmachine sync`. It installs
`ansible` rather than `ansible-core`, since the smaller package leaves
out a piece the `firewall` package needs.

On Debian and Ubuntu that is `apt-get install ansible`. On Arch it is
`pacman -S --noconfirm --needed ansible`, with the package lists the
machine already has. It never runs `pacman -Sy`: refreshing the lists
without upgrading is a partial upgrade, which Arch does not support and
which can install an Ansible built for a Python the machine does not
have. A full `pacman -Syu` upgrades everything, and that is your call, not
a side effect of `setup`. On an Arch server whose lists are too old for
the mirrors, `setup` stops and says so — see
[troubleshooting](../troubleshooting.md#pacman-could-not-install-ansible).
