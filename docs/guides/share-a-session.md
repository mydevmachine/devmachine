---
description: "Let someone watch, or type into, one of your tmux sessions for an hour, then they are out."
category: Workspaces
level: Beginner
needs:
  - "A workspace"
related:
  - keep-sessions-running.md
  - share-a-local-app-with-friends.md
  - ssh-or-mosh.md
---
# Share a session

Someone wants to watch Claude Code work, or help you in a shell. The
[`session-share`](https://github.com/mydevmachine/session-share) package
lets them join one of the workspace's tmux sessions, in a browser or over
SSH, read only or typing along. When the time you chose runs out, they are
disconnected and your session keeps running.

**You need:** a workspace, and a tmux session in it. Every
[`devmachine ssh`](keep-sessions-running.md) login already gives you one.

## Before you start: machine, skills, workspace

```
curl -fsSL https://mydevmachine.sh/install.sh | sh
devmachine setup
devmachine skills add
devmachine workspaces new acme
devmachine sync
```

Already have a machine? Skip `setup`. Already have the workspace? Skip the
last two.

## From the app

With the `session-share` package on the workspace (step 1 below), right-click
the session in the sidebar and choose **Share…**. Pick how long, and whether
they only watch or can type. The first share of a workspace publishes its
share server at `share-<workspace>.<your domain>`, so the link works for
anyone. Copy the link and the password: the password shows only there.

While it is shared, the session has a mark beside its name. Right-click it
for **Open chat**, **Add 30 minutes** and **Stop sharing**. The chat opens
in the context panel: guests write from the shared page, you answer there.
Settings → Shares lists every share this Mac started.

## By hand

### 1. Add the package

```
devmachine packages pin
devmachine packages add session-share --workspace acme
devmachine sync
```

`packages pin` moves you to the packages release that has `session-share`.

### 2. Find the session

```
devmachine run --argv --workspace acme -- tmux ls
```

The session `devmachine ssh acme` opens is called `main`.

### 3. Share it

```
devmachine run --package session-share --workspace acme -- start main --mode read --for 1h
```

```
Sharing "main" (read only) until 16:40.

  Link:      http://127.0.0.1:7690/s/k3j2abcdwxyz/
             Only this machine can open it. Publish it with: session-share expose
  Password:  v3bd-28s5-nave-dvcg
             Send it apart from the link.
  Stop:      session-share stop k3j2abcdwxyz
```

Always pass `--workspace`. The share runs as the workspace's account, which
owns its tmux sessions; without it the command would run as root.

`--mode write` lets them type too. `--for` takes anything from `1m` to
`24h`.

### 4. Give the link a public address

The link above only works on the machine. Publish the share server once,
at a name of your own:

```
devmachine expose add acme 7690 --host share.example.com
devmachine run --package session-share --workspace acme -- expose proxy --url https://share.example.com
```

From then on every share's link starts with `https://share.example.com`.
A machine with no public address can use Tailscale Funnel instead, at a
`ts.net` name: `-- expose funnel`.

Send the link and the password through different channels. They open the
link, type the password and see the session.

### 5. Or let them in over SSH

```
devmachine run --package session-share --workspace acme -- start main --ssh-github bob --no-web --for 1h
```

Their GitHub keys can log in to the workspace's account for that hour, and
land straight in the shared session: no shell, no tunnels, no other
command. They connect with `ssh -t` to the account and the machine's
address.

### 6. Talk to them

The shared page has a chat beside the session. Up to five people can be
there at once; `--max-viewers` changes it. Answer from your terminal:

```
devmachine run --package session-share --workspace acme -- chat k3j2abcdwxyz --message "I'll push the fix now"
devmachine run --package session-share --workspace acme -- chat k3j2abcdwxyz --follow
```

The chat is text only. Nothing written there reaches the session.

### 7. Stop, or give them more time

```
devmachine run --package session-share --workspace acme -- list
devmachine run --package session-share --workspace acme -- extend k3j2abcdwxyz --for 30m
devmachine run --package session-share --workspace acme -- stop k3j2abcdwxyz
```

`stop` disconnects them within a second.

## What they can do

| | read | write |
|---|---|---|
| See the shared session, live | yes | yes |
| See your other sessions | no | no |
| Run a tmux command: kill the session, open a window, detach you | no | no |
| Type into the program in the session | no | yes |

**Write means trust.** Someone who can type into a shell, or approve a
tool in Claude Code, runs commands as the workspace's account. Keep write
shares short, and for people you would hand your keyboard to.

## With your agent

```text
Share the main tmux session of my devmachine workspace acme with bob for
an hour, read only.
```

The agent adds the package if it is missing, runs `start`, and hands you
the link and the password to pass on.

**Check it:** open the link in a private window, sign in, and watch your
session move as you type in it. `-- list` shows one viewer.

Source: [session-share](https://github.com/mydevmachine/session-share),
[tmux](https://github.com/tmux/tmux/wiki)
