---
description: "Start Claude Code on your server and drive the same session from the Claude app on your phone."
category: Agents
level: Beginner
needs:
  - "A VPS (tested on Debian and Ubuntu)"
  - "The Claude app on your phone"
related:
  - start-claude-from-your-phone.md
  - claude-that-never-sleeps.md
  - keep-sessions-running.md
---
# Claude Code, controlled from your phone

Run Claude Code in a workspace and open the session from the Claude iPhone
or Android app, while it is running. You still start the session yourself
over SSH — for a session waiting for you with no SSH step at all, see
[start Claude from your phone](start-claude-from-your-phone.md).

**You need:** a VPS (tested on Debian and Ubuntu).

## Before you start: machine, skills, workspace

```
curl -fsSL https://mydevmachine.sh/install.sh | sh
devmachine setup
devmachine skills add
devmachine workspaces new acme
devmachine sync
```

Already have a machine? Skip `setup`. Already have the workspace? Skip the
last two. See [getting started](../getting-started.md) for what each command
does.

## By hand

### 1. Add Claude Code, with Remote Control on by default

```
devmachine packages add claude-code --workspace acme
devmachine workspaces edit acme --set claude-code.remote_control_at_startup=true
devmachine sync
```

`remote_control_at_startup` connects every session to Remote Control as it
opens, instead of waiting for somebody to type `/rc`.

### 2. Log in as acme, once

```
devmachine login claude --workspace acme
```

This opens a real terminal, so a person is there to finish the sign-in — see
[credentials](../concepts/credentials.md).

### 3. Start a session

```
devmachine ssh acme
claude
```

With `remote_control_at_startup` on, Claude Code prints a session URL and a
QR code as the session opens. The login is inside tmux, so you can close the
terminal and Claude keeps running. Without `remote_control_at_startup`, run
`/rc` inside the session to get the same thing.

### 4. Open it from the phone

Install the Claude app
([iOS](https://apps.apple.com/us/app/claude-by-anthropic/id6473753684),
[Android](https://play.google.com/store/apps/details?id=com.anthropic.claude)),
scan the QR code, or find the session by name under **Code** in the app.

## With your agent

Open a session on your own computer (`devmachine skills add` taught it the
CLI) and say:

```text
Install Claude Code on my devmachine workspace acme with Remote Control
on by default.
```

The agent adds the package and sets `remote_control_at_startup`, then runs
`sync`. Two steps stay yours: `devmachine login claude --workspace acme`
opens a real terminal for you to finish the sign-in, and scanning the QR
code on the phone.

**Check it:** a message sent from the phone appears in the terminal, and a
reply typed in the terminal appears on the phone.

Source: [Claude Code — Remote Control](https://code.claude.com/docs/en/remote-control)
