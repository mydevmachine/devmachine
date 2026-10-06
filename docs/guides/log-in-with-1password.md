---
description: "Keep devmachine's SSH key in 1Password and approve each use with Touch ID."
category: Security
level: Intermediate
needs:
  - "A VPS (tested on Debian and Ubuntu)"
  - "1Password 8"
  - "An SSH key in 1Password"
related:
  - your-first-devmachine.md
  - logins-and-secrets.md
  - ssh-or-mosh.md
---
# Log in with your 1Password SSH key

Keep the key devmachine logs in with inside 1Password, instead of a file on
your disk. 1Password's SSH agent hands the key over when devmachine needs
it, and asks you to approve with Touch ID or your password.

If you do not already keep SSH keys in 1Password, you do not need this: the
key `setup` makes for devmachine is simpler, and just as safe.

**You need:** a VPS (tested on Debian and Ubuntu), 1Password 8 on your computer, and an
SSH key saved in 1Password.

## Before you start: machine, skills, workspace

Do steps 1 to 3 below first. `setup` needs the agent running to offer its
keys.

```
curl -fsSL https://mydevmachine.sh/install.sh | sh
devmachine setup
devmachine skills add
devmachine workspaces new acme
devmachine sync
```

Already have a machine? See step 3 for moving it to the 1Password key. See
[getting started](../getting-started.md) for what each command does.

## By hand

### 1. Turn on the 1Password SSH agent

In 1Password, open **Settings → Developer** and turn on **Use the SSH
agent**.

### 2. Point your terminal at it

Add one line to `~/.zshrc` (or `~/.bashrc`), then open a new terminal.

On macOS:

```
export SSH_AUTH_SOCK=~/Library/Group\ Containers/2BUA8C4S2C.com.1password/t/agent.sock
```

On Linux:

```
export SSH_AUTH_SOCK=~/.1password/agent.sock
```

`ssh-add -l` now lists the keys 1Password offers.

### 3. Choose the key in `setup`

```
devmachine setup
```

When it asks how the CLI should log in, pick the line that ends in
`(from the SSH agent)` with your key's fingerprint. `setup` installs that
key on the server, proves it works, and turns password logins off, as usual.
It records the key's public half as `agent_key:` in `config.yml`, and that
is what tells devmachine to ask the agent for that one key from then on —
never every key in the vault.

For a machine you already set up with another key: add the 1Password
key's public half to the server's `/root/.ssh/authorized_keys`, then
delete the machine's `key:` line from `config.yml` and replace it with
`agent_key:`, set to that same public key line.

### 4. Optional: limit what the agent offers everywhere else

Step 3 is enough for devmachine itself. Plain `ssh` and anything else on
your computer still ask 1Password for every key in the vault, one after
another, and a vault with many keys can hit the server's limit — the
error says "Too many authentication failures".

Tell 1Password which key to offer, in `~/.config/1Password/ssh/agent.toml`:

```toml
[[ssh-keys]]
item = "devmachine"
vault = "Private"
```

`item` is the key's name in 1Password, `vault` the vault it is in. Add one
`[[ssh-keys]]` block per key you still want the agent to offer, such as
your GitHub key. Skip this if devmachine (`ssh`, `mosh`, `run`) is the
only thing you use this key for — it already offers only the one key.

### 5. Use it

```
devmachine ssh acme
```

1Password asks you to approve, and the session opens. `devmachine ssh` and
`mosh` run the system `ssh`, which reads the same `SSH_AUTH_SOCK`.
`devmachine run` keeps its connection open for five minutes, so a few
commands in a row ask only once.

A program you open from the Dock or a launcher does not read your
`~/.zshrc`. If it calls devmachine for you, it has to set `SSH_AUTH_SOCK`
to the path in step 2 itself.

## With your agent

Steps 1 and 4 happen in 1Password, and step 3's choice is yours, so do those
by hand. Then, in a new Claude Code or Codex session on your computer
(`devmachine skills add` taught it the CLI):

```text
Check that devmachine logs in with my 1Password SSH key: run
devmachine doctor and tell me what it says.
```

The agent runs `devmachine doctor`. 1Password asks you to approve, and every
line should pass. If one says the login was refused, the agent can list
the agent's keys with `ssh-add -l` and compare them with the key `setup`
installed.

**Check it:** `devmachine doctor` passes, and 1Password showed an approval
prompt for it.

Source: [1Password — SSH agent](https://www.1password.dev/ssh/agent/),
[1Password — advanced use](https://www.1password.dev/ssh/agent/advanced),
[1Password — agent config file](https://www.1password.dev/ssh/agent/config)
