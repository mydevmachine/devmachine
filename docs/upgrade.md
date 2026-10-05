# Upgrade

devmachine has three things that can be out of date: the CLI on your
computer, the packages release your configuration pins, and the skills that
teach your coding agent the CLI. One command brings all three up to date:

```
devmachine update
```

Then it checks your machines and shows what a sync would change. Nothing on
a server changes until you say yes.

## What you see

```
$ devmachine update
==> 1/5 CLI
  0.7.17 → v0.7.18
  upgrading through Homebrew
  ...
  starting the new version for the remaining steps
==> 1/5 CLI
  updated: 0.7.17 → 0.7.18

==> 2/5 Packages
  packages v16 → v17
  updated: v16 → v17

==> 3/5 Skills
  use-devmachine from packages v17 for claude
  updated: 1 filesystem change(s)

==> 4/5 Doctor
  machine main:
    pass  configuration       machine "main", 1 address(es), admin root, port 22, 1 workspace(s)
    pass  host key            SHA256:... at 203.0.113.10
    pass  connection          connected through 203.0.113.10
    ...
  ok: 1 machine(s)

==> 5/5 Sync check
  main: 3 change(s) would be made
    ~ zsh : Install zsh
    ~ workspace : Create the account
    ~ workspace : Write the SSH keys
Apply these changes with sync? [y/N]: n

  Nothing was applied. To apply it later:
    devmachine sync
  skipped: not applied; run `devmachine sync`

Summary
  cli       updated (0.7.17 → 0.7.18)
  packages  updated (v16 → v17)
  skills    updated (1 filesystem change(s))
  doctor    ok (1 machine(s))
  sync      skipped (not applied; run `devmachine sync`)
```

The five steps:

1. **The CLI.** With Homebrew, through `brew upgrade`; otherwise from the
   release archive, checked against its published checksum. The new version
   then runs the other steps.
2. **The packages.** Pins the newest release when yours is older — see the
   [releases page](https://github.com/mydevmachine/packages/releases) for
   what changed. Only `config.yml` changes.
3. **The skills.** Refreshes every skill your coding agents use, from the
   release just pinned. Skills only affect new sessions; one already running
   keeps what it loaded at the start.
4. **Doctor.** Checks every machine. A problem is shown and does not stop the
   update; a machine it cannot reach skips the last step.
5. **Sync check.** A dry run on every machine it reached, listing what would
   change, then one question. Answer `y` to apply it now.

With more than one machine, `--machine <name>` limits steps 4 and 5 to that
one. `--skip-cli` and `--skip-packages` leave those alone. `--yes` answers
the question for you — only for automation you trust to change servers.
`--cli-only` updates the CLI and stops there: nothing else is read or
changed. The Devmachine app's "Update CLI" button runs it.
`--no-machines` updates the CLI, the packages pin and the skills, and stops
before doctor and the sync check: your computer only, no question.

Without a terminal (a cron job, a pipe) `update` never asks and never
applies: it prints the `sync` command to run later. How and why each step
works: [Updating](how-it-works/updating.md). Every flag:
[`update`](reference/commands.md#update).

## Knowing when to update

When you upgrade the CLI with `brew upgrade` or the app, the packages pin
stays where it was. After `sync`, `doctor`, `machines list` and
`workspaces list`, a line says when a newer packages release is out, at
most once a day:

```
packages v33 is out (you pin v32): run devmachine update
```

`devmachine packages outdated` answers the same question on demand.
`DEVMACHINE_NO_UPDATE_HINT=1` turns the line off.

`devmachine doctor` warns when the CLI or the packages pin is behind:

```
warn  cli           this is 0.7.17, and v0.7.18 is out: run `devmachine update`
warn  packages pin  pinned v16, and v17 is out: run `devmachine update`
```

## By hand

`update` only runs commands you can run yourself, one at a time:

```
brew update && brew upgrade mydevmachine/tap/devmachine   # or: curl -fsSL https://mydevmachine.sh/install.sh | sh
devmachine packages pin
devmachine skills update
devmachine doctor
devmachine sync --check
devmachine sync
```

With more than one machine, add `--machine <name>` to `sync`; `doctor` checks
every machine unless you name one.
The lock file records exactly which package release each machine is running,
so `sync --check` always compares against what is really there, not against
what you last typed.

## Adding the essentials to an older machine

A machine set up before the `essentials` package existed has none of it:
`base`, `git`, `firewall`, `ssh_hardening`, `caddy`, `devmachine-app`. Add it without
rebuilding anything — what is already installed stays as it is:

```
devmachine update
devmachine packages add essentials
devmachine sync
```

## If you like, check the server's identity first

```
devmachine machines trust <name> --check
```

compares the SSH host key devmachine expects against the one the server
presents, without writing anything. Read the `status` it prints
(`matching`, `changed` or `missing`); it exits 0 either way. Worth running before a `sync` you are
not fully expecting, on a server you have not touched in a while.

## Nothing changes on a server until you run sync

`update` (when you answer no), `packages pin`, `packages add`, and
`workspaces edit` all edit `config.yml` only. The server keeps running
exactly what it was running until `devmachine sync` applies the change — so
pinning a new package release today and running `sync` next week is fine;
nothing happens in between.

Source: [devmachine releases](https://github.com/mydevmachine/devmachine/releases),
[packages releases](https://github.com/mydevmachine/packages/releases)
