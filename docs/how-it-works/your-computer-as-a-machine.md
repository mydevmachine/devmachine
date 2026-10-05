# Your computer as a machine

A machine can declare `self: true`: your own computer, the one running
the CLI. This page explains why that exists, why it is called `self` and
not `local`, and what changes about `sync` and `setup`.

## Why `self`, not `local`

`machines create-local` already means something else: a small VM made on
your computer that stands in for a real server while you learn or test
the CLI. It has its own address, port, root login and key, and its own
`setup`, exactly like a real server — it only happens to live here.

`self` is different: your own computer, right now, with no address to
dial and no key to install. Reusing "local" for both would make two ideas
share one word, and the first time someone typed the wrong one, they
would find out the hard way.

## Why no address fields

`hosts`, `user`, `port` and `key` describe how to reach a machine that is
not your computer. A self machine has nothing to dial, no login, no key
to install, so devmachine refuses all four if you try to set them,
naming the field:

```
machine "mac" is your computer (self: true), so it has no hosts: remove it
```

The same reason is why a workspace can never live on a self machine: a
workspace is a Linux account reached over SSH with a key the CLI
installed, and your own computer has no account system or SSH server for
that.

## Why it never needs `sudo` for the work itself

Homebrew, mise and Claude all live under your own home folder on a Mac,
and none of them need root, so a self machine's setup never asks for
extra permissions. It only runs on macOS — nothing about this path has
been tried anywhere else — and stops with a clear error elsewhere.

## How `setup` gets Ansible onto your computer

Ansible cannot install itself. On a real server, `setup` bootstraps a
key, proves it, locks the server down, then installs Ansible. None of
that applies to your own computer, so `setup` here does only the last
part, the same way it does on a Mac reached over SSH: it runs the
`bootstrap` of a package manager package. That is `mac-brew` (Homebrew),
or `mac-ports` (MacPorts) when the machine lists it.

The bootstrap runs from the package cache on your computer, as you. It is
not copied to `/opt`, which would need `sudo` (see below). First it only
checks. When Homebrew and `ansible-playbook` are already there (from
Homebrew or pipx), nothing is installed and `setup` says so:

```
mac is already prepared: ansible-playbook is /opt/homebrew/bin/ansible-playbook.
```

When something is missing, `setup` lists it with how long it takes and
asks before it installs anything; `--install-prerequisites` is the yes
for a run with no terminal. `--yes` is never that yes. Installing the
Command Line Tools or Homebrew needs `sudo`; the bootstrap only uses
`sudo -n`, so on a Mac where `sudo` asks for a password it stops and says
which step failed. See [what a machine needs](what-a-machine-needs.md).

`sync` follows the same rule: with no `ansible-playbook` on `PATH`, it
stops before touching anything and points you to `setup`.

## Where files end up, and why not `/opt`

On a real server, devmachine's files live at `/opt/devmachine`, root's
territory, which is fine since `setup` already has full control of a
server it set up deliberately. On a Mac, `/opt` needs `sudo`, running
into the same rule as Homebrew — not this CLI's place to ask.

So on a self machine, those files live under your own home instead:

```
$HOME/.local/share/devmachine/bundle
```
