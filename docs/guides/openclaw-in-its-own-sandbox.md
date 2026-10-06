---
description: "Run the OpenClaw agent around the clock in a workspace of its own, away from your laptop."
category: Agents
level: Beginner
needs:
  - "A VPS (tested on Debian and Ubuntu)"
related:
  - hermes-agent-in-its-own-sandbox.md
  - one-consultant-three-startups.md
  - keep-sessions-running.md
---
# OpenClaw, in its own sandbox

Give [OpenClaw](https://openclaw.ai/) a machine to act on, not your laptop.
It runs around the clock in a sandbox of its own: a workspace, which is a
separate account on your server with its own files and logins, no `sudo`,
and no way into your other workspaces. Whatever the agent installs, or
breaks, stays inside it.

**You need:** a VPS (tested on Debian and Ubuntu).

## Before you start: machine, skills, workspace

```
curl -fsSL https://mydevmachine.sh/install.sh | sh
devmachine setup
devmachine skills add
devmachine workspaces new agent
devmachine sync
```

Already have a machine? Skip `setup`. Already have the workspace? Skip the
last two. See [getting started](../getting-started.md) for what each command
does.

## By hand

### 1. Install OpenClaw

```
devmachine ssh agent
curl -fsSL https://openclaw.ai/install.sh | bash
```

The installer sets up Node itself if the account has none. A workspace is
its own account, so this never touches `acme` or any other workspace — see
[machines and workspaces](../concepts/machines-and-workspaces.md).

### 2. Keep it running

```
openclaw onboard
```

`openclaw onboard` walks through first-time setup, then runs the Gateway in
the foreground. You do not need to start tmux: the login is already in
tmux, and it stays alive after you log out. Detach with `Ctrl-b d`, or just
close the terminal. To make it survive a reboot too, see OpenClaw's own docs
on running it as a background service (`openclaw onboard --install-daemon`).

## With your agent

Open a session on your own computer (`devmachine skills add` taught it the
CLI) and say:

```text
Create a devmachine workspace called agent and install OpenClaw in it.
```

The agent creates the workspace, runs `sync`, then installs OpenClaw over
SSH and starts `openclaw onboard`. You still approve `sync` when it asks,
and OpenClaw's own onboarding — any account or key it asks for — is yours to
answer.

**Check it:** `devmachine ssh agent` again. You land back in the same
session, and the Gateway is still running.

Source: [OpenClaw — Install](https://docs.openclaw.ai/install)
