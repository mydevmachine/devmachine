# What the CLI knows about a machine

`setup`, `sync` and `doctor` already connect to a machine. While they are
there, they read what it runs — `uname`, `/etc/os-release`, `sw_vers` on a
Mac, where `ansible-playbook` is — in one extra command, and keep the
answer in `<config>/state/machines/<name>.json`.
`devmachine machines show <name>` prints it without connecting, and
`devmachine --format json machines show <name>` gives it to a script or an
agent under `observed`. `devmachine machines rm <name>` deletes the file
with the machine, so a new machine that reuses the name does not inherit
what the old one ran.

## Why it is not in `config.yml`

`config.yml` says what you want. This file says what a command saw, and
the next `sync` reads the machine again. Mixing the two would make a
reinstalled server look like a change to your configuration, and an
autocommit would record it as one. So the file lives under `state/`,
which `devmachine setup git` keeps out of the repository. A repository
made before this release gets the `.gitignore` line the next time you run
`devmachine setup git`.

It lives in the configuration directory, like `known_hosts`, and not in
`~/.local/state`, because a machine name is unique only inside one
configuration: two configurations can each have a machine called `vps`.

## Why it is written only when it changes

The macOS app watches the configuration directory and reloads on every
change. A `doctor` that rewrote the file on each run would make the app
reload for nothing. So the file is written only when something other than
the time differs, and `observed_at` is when the machine was first seen as
the file describes it — not the last time somebody looked. The file is
written to a temporary name in the same folder and then renamed, so a
reader never sees half of it.

## What reads it

- `run` puts `path_prefix` in front of `PATH`. A plain SSH command on a
  Mac gets `/usr/bin:/bin:/usr/sbin:/sbin` and nothing else, so `port`,
  `brew` and the Python they installed are not found without it. On Linux
  the list is empty and `run` sends the command as it is.
- `sync` refuses, before it changes anything, a package whose `platforms`
  leave out the machine's system — see
  [troubleshooting](../troubleshooting.md#package-x-runs-on-linux-machine-y-is-macos).
  A machine nobody has read yet is not refused: its first `sync` reads it.
- On a Mac reached over SSH, `sync` calls Ansible by `ansible_playbook`,
  the absolute path the package manager package's bootstrap reported
  during `setup`. A MacPorts install can name it `ansible-playbook-3.14`,
  which no `PATH` would find. On Linux and on your own computer it is
  `ansible-playbook` from `PATH`, as before.
- An agent reads `observed` before it writes a `run` command, so it uses
  `pacman` on Arch, `apt` on Debian and `port` on a Mac.

A command never fails because this file could not be written: it is a
by-product of a read that already worked.
