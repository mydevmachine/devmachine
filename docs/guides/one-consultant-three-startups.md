---
description: "One server, one sandbox per client: separate stacks, logins and agents that never mix."
category: Workspaces
level: Intermediate
needs:
  - "A VPS with 12 GB of RAM"
  - "GitHub and Claude accounts per client"
related:
  - logins-and-secrets.md
  - shared-skills-for-every-workspace.md
  - ruby-on-rails.md
---
# One consultant, three startups

A freelance developer works for three clients at once: Acme, a Django shop;
Globex, a Next.js team; Initech, still on Rails. Three different stacks,
three different GitHub orgs, three Claude Code accounts billed to three
different people. One laptop, and until now, one tangled `~/dev` folder
where a stray `.env` from Acme could leak into a Globex repo without anyone
noticing.

The fix is one VPS, three sandboxes: the workspaces `acme`, `globex` and
`initech`. Each is an isolated environment, its own Linux account with its
own files, its own language runtime and its own logins. Nothing crosses
between them, because nothing is shared between them.

**You need:** a Linux VPS (tested on Debian and Ubuntu) with 12 GB of RAM or more, and GitHub and Claude accounts for
each client (invented here as `acme`, `globex`, `initech`).

## Before you start: machine and skills

```
curl -fsSL https://mydevmachine.sh/install.sh | sh
devmachine setup
devmachine skills add
```

Already have a machine? Skip `setup`. See [getting started](../getting-started.md)
for what each command does. The three workspaces come next, one per client.

## By hand

### 1. A workspace per client

```
devmachine workspaces new acme
devmachine workspaces new globex
devmachine workspaces new initech
devmachine sync
```

Each `workspaces new` only edits the configuration; `sync` builds all three
Linux accounts on the server in one pass. See
[machines and workspaces](../concepts/machines-and-workspaces.md) for what a
workspace actually isolates: files, shell history, environment variables,
and any credential you sign in with inside it.

### 2. Docker, for each client's services

Each startup runs its database and services in Docker: Postgres for Acme,
Redis for Globex, MySQL for Initech. Install Docker once on the machine, and
let each workspace use it:

```
devmachine packages add docker
devmachine workspaces edit acme --set 'workspace.groups=[docker]'
devmachine workspaces edit globex --set 'workspace.groups=[docker]'
devmachine workspaces edit initech --set 'workspace.groups=[docker]'
devmachine sync
```

Then each client's `docker compose up -d` runs inside its own workspace. One
thing is not separated: the three workspaces share one Docker engine, and the
`docker` group can do almost anything on the server, so a workspace in it can
see and stop the other clients' containers. Name each project after its
client (`docker compose -p acme up -d`) to keep them apart.

> **Hint: plan for memory.** This setup needs real compute and RAM. Three
> stacks with their own databases, plus a coding agent in each, add up fast:
> plan for **12 GB of RAM or more** once you run several Docker services at
> the same time. `devmachine stats` shows what the machine is spending.

### 3. Each stack, pinned inside its own workspace

Every workspace already has `mise` from the default packages. Inside each
one, pin the version the client's codebase needs:

```
devmachine ssh acme
mise use -g python@3.12
```

```
devmachine ssh globex
mise use -g node@22
```

```
devmachine ssh initech
mise use -g ruby@3.3
```

`mise use -g` writes the choice to that account's own global config, so
Acme's Python version has no effect on Globex or Initech — each workspace
keeps its own.

### 4. Sign in to GitHub, per client where it matters

The GitHub login is shared by every workspace on the machine unless a
workspace says otherwise. If a client's org needs its own GitHub account, tell
that workspace to keep its own, then sign in inside it:

```
devmachine workspaces edit acme --share gh=own
devmachine sync
devmachine login gh --workspace acme
```

If two clients happen to share your personal GitHub account, sign in once
and let every workspace share it instead — see
[sharing a login](../how-it-works/sharing-a-login.md).

### 5. Claude Code, one account per client

```
devmachine packages add claude-code --workspace acme
devmachine packages add claude-code --workspace globex
devmachine packages add claude-code --workspace initech
devmachine sync
devmachine login claude --workspace acme
devmachine login claude --workspace globex
devmachine login claude --workspace initech
```

Claude Code needs its own sign-in per workspace — it is not the kind of
login you share. `login` opens a real terminal for each one, so you can pick
the right account when it asks.

### 6. A preview URL, if a client wants one

```
devmachine expose add globex 3000 --host globex.example.com --publish
```

Caddy came with `setup` (it's in the `essentials` package) and serves every
client's preview from its own domain — see
[publishing](../concepts/publishing.md). If your machine was set up with
`--no-essentials`, or before CLI v0.7.14, add it first: `devmachine packages
add caddy` and `devmachine sync`.

## Why this works

**Nothing leaks between clients.** Files, shell history, environment
variables, and every credential live inside one workspace's own account.
Acme's `.env` cannot end up in a Globex `git status` by accident, because
Acme's files are not on Globex's filesystem at all.

**One broken setup stays broken in one place.** If Initech's Ruby version
mismatch breaks `bundle install`, that breaks inside `initech` only. `acme`
and `globex` never see it.

**Switching clients is one command:**

```
devmachine ssh globex
```

No `cd`, no unsetting environment variables, no `nvm use` juggling — the
workspace you land in already has the right runtime, the right logins, and
nothing else.

**Ending a contract is one command too:**

```
devmachine workspaces destroy initech
```

This deletes the Linux account, everything under its home directory, and
its Caddy routes — it asks you to type the workspace name again first,
since there is no undo. DNS records for anything you exposed are left as
they are; take those down by hand if the domain should stop pointing here.

## With your agent

Open a session on your own computer (`devmachine skills add` taught it the
CLI) and say:

```text
Set up a devmachine workspace called globex for a Next.js project: add
Claude Code, give it Docker for its Redis, pin Node 22 with mise, and expose
port 3000 at globex.example.com.
```

The agent creates the workspace, adds `claude-code` and `docker` (and
`caddy` too, if `setup` ran with `--no-essentials`), puts `globex` in the
`docker` group, runs `mise use -g node@22` over SSH, then `expose add` —
showing you the plan of each `sync` first. Signing in to GitHub and Claude Code
stays yours to do, since those are browser and device logins nobody else can
finish for you.

**Check it:** `devmachine ssh globex` drops you into a shell with `node -v`
reporting 22, while `devmachine ssh acme` on the same machine still reports
whatever Python version Acme's project needs — the two never mix.

Source: [mise — `use`](https://mise.jdx.dev/cli/use.html)
