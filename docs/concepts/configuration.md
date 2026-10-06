# Configuration

Your configuration is a folder of files that lists your machines, your
workspaces, and what's installed where. Look at it with:

```
devmachine config show
```

## Where it is

First match wins:

| Order | Rule |
| --- | --- |
| 1 | `--config <path>` |
| 2 | `DEVMACHINE_CONFIG` |
| 3 | `$XDG_CONFIG_HOME/devmachine` |
| 4 | `~/.config/devmachine` |

```
$ devmachine config path
/Users/you/.config/devmachine (from default)
```

Run a second, test setup next to a real one with one variable:

```
DEVMACHINE_CONFIG=~/.config/devmachine-test devmachine doctor
```

## What it holds

```
<config>/config.yml     machines, workspaces, domain, DNS provider
<config>/secrets.json   names of stored secrets, and values the keychain refused
<config>/keys/          keys the CLI generated, and the public half of a chosen agent key
<config>/history.log    one line per command that reached a machine
<config>/cache/config.lock   held by whichever command is writing config.yml
<config>/state/machines/<name>.json   what setup, sync and doctor last read from a machine
```

Two commands that change `config.yml` at the same moment — two
`machines add` from a script or an app, an `expose rm` while you run
`workspaces edit` — take turns: each holds `cache/config.lock` while it
reads the file, changes it and writes it back, and the other waits.
Without it, the slower one would write back what it read and silently
drop the other's change. The lock is released when the command finishes
writing, or dies. The file is rewritten in one step, so a command that
only reads never sees half of it, and a `config.yml` that is a link stays
a link.

## config.yml

```yaml
machines:
  - name: main
    hosts:
      - tailscale:vps      # tried first
      - 203.0.113.10       # the fallback
    user: root             # the account it logs in as
    port: 22
    key: /keys/main        # optional; without it the SSH agent serves
    # agent_key: ssh-ed25519 AAAA...  # optional; the one agent key to use instead of `key`
    location: hostinger    # optional; where it is. Left out: external
    packages: [base, docker, caddy, firewall, fail2ban, ssh_hardening, git]
    settings:
      base.timezone: Europe/Lisbon
      caddy.email: someone@example.com

workspaces:
  - name: acme
    machine: main
    packages: [dev, zsh, mise]
  - name: bob
    machine: sandbox
    user: bob-dev          # optional; the name is used by default
    packages: [dev]
    credentials:
      gh: own              # this one signs in to its own account

# What a new workspace gets when no flag says otherwise. `setup` sets this.
defaults:
  workspace: [dev, zsh, mise]

# Whether a login is shared across the machine. A workspace may override it.
credentials:
  gh: machine

packages: v0.0.1           # the pinned release the packages come from
domain: example.com
ssh_aliases: true          # keep the SSH aliases up to date
ssh_aliases_path: ~/.ssh/devmachine-aliases   # where they live; ~/.ssh/config when left out
```

`user` defaults to `root`, `port` to `22`. `location` defaults to
`external`, or `local` for your own computer (`self: true`); see [where a
machine is](../how-it-works/machine-location.md). Nothing here is a secret — a
token goes in `devmachine secrets`, never in this file.

`ssh_aliases` records the answer to the question `setup` and `machines add`
ask about writing SSH host entries. Left out, nothing is written
automatically — the behaviour every configuration had before this field
existed. `devmachine aliases --write` sets it to `true` the first time it
runs interactively and you say yes. See
[reaching your server](reaching-your-server.md#ssh-aliases).

`ssh_aliases_path` is the one file the aliases are kept in, for a
`~/.ssh/config` that something else generates and that `Include`s the
aliases from a file of their own. `devmachine aliases --write --path
<file>` sets it.

## Settings

A package reads its own settings, each with a default. `settings:`
overrides one, on a machine (as shown above) or on a workspace, written
`<package>.<name>`:

```yaml
workspaces:
  - name: acme
    packages: [dev, zsh, claude-plugins]
    settings:
      claude-plugins.marketplace: example.com/their-plugins
      claude-plugins.plugins: [their-plugin]
```

Only the first dot is the split, so `claude-plugins.marketplace.url` is
the package `claude-plugins` and the setting `marketplace.url`.

A setting is refused, not just ignored, when it has no `<package>.`
prefix, or names a package the target doesn't have — so a typo never
silently does nothing.

## Credentials

A package says how its login works, and whether a copy of it can be
shared across a machine. Whether you *want* it shared is your call, set
per workspace:

```yaml
credentials:
  gh: machine          # the default for this setup

workspaces:
  - name: acme
  - name: bob
    credentials:
      gh: own          # bob logs in for himself
```

`machine` means one login, copied into every workspace that wants it.
`own` means that workspace signs in for itself. See
[Credentials](credentials.md) for how each kind works, and
[Sharing a login](../how-it-works/sharing-a-login.md) for what the copy
does.

## What is validated

`config show`, and every command that touches a machine, refuse a
configuration that can't work, and say what to fix:

- no machine at all
- a machine with no name, no address, or a port outside 1–65535
- two machines, or two workspaces, with the same name
- a workspace on a machine that isn't configured
- a workspace with no machine when there are several
- a setting with no `<package>.` prefix, or for a package the target
  doesn't have
- a credential answer that is neither `machine` nor `own`
- a `location` with characters other than letters, numbers, spaces, dots,
  underscores and hyphens, or longer than 40

## The command log

Every command that reaches a machine appends a line to
`<config>/history.log`: when, which workspace or machine, whether it
worked, and the command. It's the answer to "what did that session do to
my machine" — a plain `ssh` session leaves no such trail here. The format
is in [commands](../reference/commands.md#the-command-log).

## Secrets

Tokens don't go in `config.yml`. They live in your operating system's
keychain:

```
devmachine secrets set cloudflare_token     # asks, without echoing
devmachine secrets list                     # names only, never a value
devmachine secrets rm cloudflare_token
```

With no keychain — a headless server, a locked-down container — the value
falls back to `secrets.json`, readable by nobody else.
