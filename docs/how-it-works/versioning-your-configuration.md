# Versioning your configuration

`devmachine setup git` turns `<config>/` into a git repository, so it can
live in a private remote, be reviewed, and be restored. It never runs on
its own.

## What is committed, and what never is

| Path | Holds | Committed |
| --- | --- | --- |
| `config.yml` | machines, workspaces, packages, settings | **yes** |
| `packages.lock` | the resolved package versions | **yes** |
| `known_hosts` | public SSH host identities you approved | **yes** |
| `AGENTS.md` | rules for a coding agent working in this folder | **yes** |
| `keys/` | private SSH keys | **never** |
| `secrets.json` | the keyring fallback, in the clear | **never** |
| `cache/` | downloaded package trees | **never** |
| `history.log` | what was run, against which host | **never** |
| `*.env` | anything a package staged | **never** |
| `state/` | what each machine was last seen to run | no: it is read again on the next sync |
| `packages/.install-*` | a package being fetched from a git address | no: a fetch a Ctrl-C cut short is swept after an hour |

`config.yml` holds hostnames and usernames, which is exactly why the
remote must be private — and why the command says so out loud rather
than assuming you worked it out.

## Why the remote must be private

`config.yml` names every machine you run: its address, its admin login,
which workspaces live on it. Nothing in it lets somebody in, but it is a
map of your infrastructure, and a public repo is a map handed to whoever
finds it.

`devmachine setup git` offers to create the remote itself with `gh repo
create --private`, so the moment somebody chooses to publish this
directory is the moment they should not have to remember the flag.

## Why the `.gitignore` comes first

The order is: write `.gitignore` → `git init` → `git add` → guard →
commit.

Written after `git init`, there would be a window where `git add -A`
could pick up `keys/id_ed25519` or `secrets.json` before anything
excludes them. That window can be milliseconds, and the result is
permanent: a private key in a commit is not undone by removing it at the
tip. See
[troubleshooting](../troubleshooting.md#i-already-committed-a-key) if
that has already happened. Writing the file first removes the window
instead of shrinking it.

## The guard, and why it refuses rather than warns

Every commit this feature makes stages everything, checks what actually
got staged, and refuses if any of it is a path that must never be
committed. A warning printed above a commit that already happened is a
warning nobody reads — refusing before the commit is the only version
that matters.

The check is a closed list of paths — `keys/`, `secrets.json`, `cache/`,
`history.log`, `*.env` — not a scan for things that look like a token,
because the CLI wrote every one of these paths itself: nothing else
lands in the configuration directory.

## Why the CLI writes its own commit messages

Every write the CLI makes — `machines add`, `workspaces new`, `sync`
locking a machine's packages — commits itself, with a message the
command chose, such as `chore(config): add machine box`. A history is
only useful if every entry can be trusted to say what actually ran. A
commit message a language model wrote by looking at a diff is a guess
dressed as a fact; a commit message the CLI wrote is a report — the
command that ran is the only thing that could have written that exact
line.

The auto-commit never fails the command that triggered it: a broken
signing key, for instance, is written to `history.log` instead, and the
configuration change stands.

## Secrets never enter this history, by a different route

A secret's value never lives in the configuration directory — it is kept
in the OS keychain (`devmachine secrets set`), with `secrets.json` as the
fallback with no keychain, already in the first `.gitignore` this command
writes.

`devmachine secrets example` lists the `<NAME>=` a machine's packages
need, with no value — the *shape* of what a machine needs, safe to
review or hand to somebody else.
