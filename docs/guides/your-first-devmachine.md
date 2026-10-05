---
description: "Install devmachine, connect a server and open your first workspace, one command at a time."
category: Get started
level: Beginner
needs:
  - "A Debian, Ubuntu or Arch Linux VPS"
  - "A Mac or Linux computer"
related:
  - a-local-vm-with-lima.md
  - keep-sessions-running.md
  - express-site-with-tls.md
---
# Your first devmachine, explained

The first commands, one at a time: what each asks and what it does. At the
end you have a workspace called `acme` on your server.

**You need:** a VPS running Debian, Ubuntu or Arch Linux that you reach over
SSH as root, or as an admin login with passwordless sudo, and a Mac or Linux
computer. A Mac can be the machine too: see [what a machine
needs](../how-it-works/what-a-machine-needs.md).

## By hand

### 1. Install the CLI

```
curl -fsSL https://mydevmachine.sh/install.sh | sh
```

### 2. Connect the server

```
devmachine setup
```

It asks for the server's address and login, then shows its **fingerprint**.
Check it against your provider's dashboard before you say yes: that is how
you know it is your server. Then it installs a key, proves the key works,
turns password logins off, and picks the `essentials` package for the
machine (`--no-essentials` skips it). See
[how setup locks the server](../how-it-works/trust-bootstrap.md).

Once the machine answers, it asks whether to write SSH host entries to
`~/.ssh/config` — say yes, and `ssh acme-devmachine` works from any terminal
or editor from here on, kept up to date automatically. It also asks about
Tailscale, a private address that keeps working when the public one does
not; see [reaching your server](../concepts/reaching-your-server.md).

### 3. Teach your agent the CLI

```
devmachine skills add
```

Any new Claude Code or Codex session can now run devmachine for you.

### 4. Create the workspace

```
devmachine workspaces new acme
devmachine sync
```

`workspaces new` adds `acme` to your configuration. `sync` builds it on the
server, together with the essentials `setup` chose for the machine (base
tools, git, a firewall, Caddy and what the macOS app reads): `acme` is its own account, with git, the
GitHub CLI, Node, bun and zsh. It shows the plan and asks before changing
anything.

### 5. Open it

```
devmachine ssh acme
```

You land inside tmux, so what you start keeps running after you close the
terminal. See [keep your sessions running](keep-sessions-running.md).

Said yes to SSH aliases? `ssh acme-devmachine` reaches the same workspace,
from any terminal, editor or app that dials `ssh` directly.

## With your agent

Install the CLI (step 1), then say to Claude Code or Codex on your computer:

```text
Set up devmachine for me with a workspace called acme:
read https://mydevmachine.sh/agent-setup.md and follow it.
```

It asks for what it needs, one question at a time. You still check the
fingerprint in `devmachine setup`, and you approve the `sync`.

**Check it:** `devmachine doctor` passes, and `devmachine ssh acme` opens a
shell on the server.
