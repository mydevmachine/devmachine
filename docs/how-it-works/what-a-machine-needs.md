# What a machine needs

`devmachine` runs Ansible on the machine itself, so every machine needs a
few things before the first `sync`. This page lists them, says which ones
the CLI installs and which only you can do, and explains why installing
them needs its own yes.

## The list

| Prerequisite | Linux server | Mac | Who handles it |
| --- | --- | --- | --- |
| SSH you can reach | yes | Remote Login on | You. On a Mac: System Settings > General > Sharing > Remote Login. |
| An admin login with passwordless `sudo` (or root) | yes | yes | You. `setup` prints the exact command when `sudo -n true` fails. |
| Xcode Command Line Tools | — | yes | The CLI, after you agree (5 to 10 minutes). |
| Homebrew or MacPorts | — | one of them | The CLI, after you agree. You choose which. |
| Ansible | yes | yes | The CLI: `apt` or `pacman` on Linux, Homebrew or MacPorts on a Mac. |
| Python for Ansible | comes with Ansible | comes with Ansible | Nobody: the system Python on a Mac (3.9) is never used to run Ansible. |

`devmachine doctor` checks the same list. On a Mac, the installable part
shows up as one `prerequisite: <name>` check per missing item; with
`--format json` those are ordinary entries of `checks[]`.

## On a Mac, the package manager package does the work

The CLI holds nothing macOS-specific beyond running one script. A Mac
gets Ansible through `mac-brew` (Homebrew) or `mac-ports` (MacPorts),
like choosing between npm and pnpm. Each package carries a `bootstrap`
script with two actions: `check` changes nothing and lists what is
missing, and `apply` installs it, in order: the Command Line Tools, the
package manager, then Ansible through it. A second `apply` changes
nothing.

Which package: the one the machine lists. With none, the one for the
manager the Mac already has. With neither or both, `setup` asks, or takes
`--package-manager brew|ports`; with no terminal and no flag it stops:

```
the Mac has neither Homebrew nor MacPorts: run again with --package-manager brew or --package-manager ports
```

The package goes on the machine's list. On a new Mac, `setup` and
`machines add` also put `base` and `devmachine-app` there in place of
`essentials`, which runs only on Linux; see [what a new machine starts
with](what-a-new-machine-starts-with.md#on-a-mac).

`setup` copies the package to `/opt/devmachine/bootstrap/<package>/` and
runs the script there as the admin login, never as root: Homebrew refuses
root, so the script reaches root only through `sudo -n` for the steps
that need it. On your own computer (a `self` machine) it runs from the
package cache instead, with nothing copied to `/opt`.

`apply` reports the absolute path of `ansible-playbook`. `sync` calls
Ansible by that path, because a plain SSH command on a Mac gets
`PATH=/usr/bin:/bin:/usr/sbin:/sbin`, where neither Homebrew nor MacPorts
lives.

## Remote Login set to "Only these users"

Remote Login can allow all users or only some. With "Only these users",
macOS keeps the list in the group `com.apple.access_ssh` and refuses any
account outside it before it looks at the key: the server log says
`pam_sacl: denying '<account>' due to failed service ACL check`, and
`ssh` gets only "Connection closed" (exit 255).

So a workspace account has to be in that group. `sync` adds each one when
the group exists; with "All users" there is no group and nothing to add.
`workspaces destroy` takes the account out again before it deletes it.
`devmachine doctor` reports one `ssh access: <workspace>` check per
workspace, so an account left out shows as a warning and not as a
mystery. The CLI never switches Remote Login to "All users": that is the
Mac owner's choice.

## Why consent has its own flag

Installing the Command Line Tools takes up to ten minutes and puts
software on somebody's computer, so it happens only after a yes: an
answer at the terminal, or `--install-prerequisites`.

`--yes` is not that yes. It already means other things: in `setup` and
`machines add` it writes SSH host entries, and in `sync` and `packages
add` it skips the confirmation. The macOS app runs `sync … --yes` in the
background with nobody watching. If `--yes` also meant "install the
Command Line Tools", a background sync could start a ten-minute install
nobody asked for. So `--yes` never installs prerequisites, and `sync`
never installs them at all: that is `setup`'s and `machines add`'s job.

Without a terminal and without the flag, a Mac that lacks something
stops before anything is installed:

```
nothing was installed: run again with --install-prerequisites to install Xcode Command Line Tools and Homebrew
```
