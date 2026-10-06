# Supported systems

devmachine deals with two kinds of computer, and they have different
lists:

- **Your computer** runs the CLI (and, on a Mac, the app). It sends
  commands over SSH and keeps your configuration.
- **A machine** is what the CLI sets up: a VPS, a server at home, a Mac,
  or a virtual machine. Ansible runs there, and your workspaces live
  there.

The two can be the same computer: see [your computer as a
machine](how-it-works/your-computer-as-a-machine.md).

## Your computer

| System | What runs there |
| --- | --- |
| macOS | The CLI and the [macOS app](https://mydevmachine.sh/app/) (macOS 14 or later). |
| Linux | The CLI. |
| Windows | The CLI, inside WSL 2 (tested with Ubuntu 24.04). Not in PowerShell or cmd. |

The CLI needs only `ssh` beside it. Ansible never runs on your computer.

## The machines

| System | Tested on | How Ansible gets there |
| --- | --- | --- |
| Debian | 12 | `setup` runs `apt-get install ansible`. |
| Ubuntu | 24.04 | `setup` runs `apt-get install ansible`. 22.04 is accepted but not tested: its apt Ansible is older than ansible-core 2.14, the oldest the packages are checked against. |
| Arch Linux | rolling | `setup` runs `pacman -S --needed ansible`, never a partial upgrade. |
| Arch Linux ARM | — | The same path as Arch Linux. Accepted, but not tested as much. |
| macOS | 15 on Intel, 27 on Apple Silicon | The `mac-brew` or `mac-ports` package installs the Command Line Tools, Homebrew or MacPorts, then Ansible, after you agree. |

`setup`, `machines add` and `doctor` ask the machine what it runs before
they change anything, and stop on a system that is not in this list. See
[it checks what the machine runs
first](how-it-works/trust-bootstrap.md#it-checks-what-the-machine-runs-first).

## What works on each

| | Debian, Ubuntu | Arch Linux | macOS |
| --- | --- | --- | --- |
| `setup`: install and prove a key | yes | yes | yes |
| Password login turned off | yes | yes | yes, with an sshd drop-in |
| Login it starts with | root, or an admin with passwordless `sudo` | the same | an admin with passwordless `sudo`; a Mac has no root login |
| Workspaces | yes | yes | yes, with Remote Login on |
| `essentials` | yes | yes | no: a Mac starts with `base` and `devmachine-app` |
| `firewall`, `fail2ban`, `ssh_hardening`, `caddy`, `docker`, `tailscale` | yes | yes | no, Linux only |
| `claude-remote-control`, `hostinger` | yes | yes | no, Linux only |
| `mac-brew`, `mac-ports`, `mac-mise` | no | no | yes, macOS only |
| Every other package | yes | yes | yes |

Each package says where it runs in
[`platforms`](reference/package-format.md#platforms). `sync` refuses a
package that leaves out the machine's system, before it changes the
machine.

On a Mac, Remote Login can allow only some users. Then macOS keeps the
list in the group `com.apple.access_ssh`, and `sync` adds each workspace
account to it, so you can log in to the workspace. See [Remote Login set
to "Only these users"](how-it-works/what-a-machine-needs.md#remote-login-set-to-only-these-users).

A workspace works with any login shell: zsh, bash or plain sh. From
packages v37 on, the `workspace` package keeps the tools on `PATH`, `mise`
and the workspace's secrets in `~/.devmachine/shellenv`, and makes every
shell read it. The `zsh` package adds the tmux session on login. One limit
on a Mac: its bash 3.2 reads no startup file for a command piped into
`ssh -T` or run with `su - <user> -c`, so those see a bare environment.

## What a machine needs first

- SSH you can reach. On a Mac: System Settings > General > Sharing >
  Remote Login.
- A login with passwordless `sudo`, or root on Linux.
- On a Mac: the Xcode Command Line Tools and Homebrew or MacPorts. The
  CLI installs them only after you say yes, or with
  `--install-prerequisites`. `--yes` never installs them.

The full list, and why installing needs its own yes, is in [what a
machine needs](how-it-works/what-a-machine-needs.md).

## Python

Ansible is written in Python, and recent ansible-core needs Python 3.11
or later where it runs. You never install it yourself:

- On Linux, the system's `ansible` package brings the Python it needs.
- On a Mac, the Ansible from Homebrew or MacPorts brings its own Python.
  The Python that comes with macOS (3.9) is too old, and devmachine never
  uses it to run Ansible.

## Not supported yet

Fedora, Rocky Linux, AlmaLinux, CentOS Stream, Amazon Linux, openSUSE,
Alpine, FreeBSD and any other system are not supported yet. A
distribution based on Debian or Arch is not accepted either: `setup`
reads `ID` in `/etc/os-release`, not the family, because a derivative
may not have the same packages. It stops before the first change with:

```
this CLI does not set up "fedora" yet: it supports debian, ubuntu and arch
```

## Guides

Most [guides](guides/index.md) were tested on Debian and Ubuntu. Each one
says what it needs at the top. A guide that uses a Linux-only package,
such as `caddy` or `docker`, needs a Linux machine.

## Writing a package for several systems

How one package runs on Debian, Ubuntu, Arch Linux and macOS, and the
traps the built-in packages hit, is in [one package on many
systems](how-it-works/packages-on-many-systems.md).
