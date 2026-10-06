---
description: "See what a workspace is missing, then sign in and hand over tokens with credentials, login and secrets."
category: Security
level: Beginner
needs:
  - "A VPS (tested on Debian and Ubuntu)"
  - "A Cloudflare account, for the last part"
related:
  - cloudflare-dns.md
  - one-consultant-three-startups.md
  - log-in-with-1password.md
---
# Logins and secrets

Every tool in a workspace needs something to prove who it is: GitHub needs
you signed in, Claude Code needs your account, a DNS provider needs an API
token. This guide walks through all three cases on one machine, using
`devmachine credentials list` to see what is missing and what fixes it.

**You need:** a VPS (tested on Debian and Ubuntu), and a Cloudflare account with a domain
on it (only for the last part, storing an API token).

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

### 1. Add the tools that need a login

```
devmachine packages add claude-code --workspace acme
devmachine packages add cloudflare
devmachine sync
```

`gh` is already on `acme`: the `dev` package, on every new workspace,
installs it. `claude-code` adds Claude Code. `cloudflare` adds the DNS
provider — see [concepts/credentials](../concepts/credentials.md) for the
three kinds of credential a package can declare.

### 2. See what is missing

```
devmachine credentials list
```

```
CREDENTIAL   SCOPE       STATUS    FIX
gh           machine     missing   devmachine login gh
claude       workspace   missing   devmachine login claude --workspace acme
cloudflare   machine     missing   devmachine secrets set cloudflare
```

`missing` means the package declared the credential and there is nothing
stored for it yet. `stored` means the fix already ran. `unknown` shows up
when a package never said where its tool keeps the result — there is
nowhere to look, so the CLI cannot tell you either way. Each row's `FIX`
column is the exact command that clears it.

### 3. Sign in to GitHub, shared by every workspace

```
devmachine login gh
```

This opens `gh auth login` on the machine, in a real terminal — you finish
it in your browser. `gh` is declared `scope: machine`, so the next `sync`
copies this one login into every workspace that has not opted out. See
[sharing a login](../how-it-works/sharing-a-login.md) for what the copy
actually does.

### 4. Sign in to Claude Code, one workspace at a time

```
devmachine login claude --workspace acme
```

Claude Code's credential is `scope: workspace`: each workspace signs in for
itself, because most people want a different account per client or project,
not one account shared everywhere.

### 5. Keep one workspace on its own GitHub account

Say a second workspace, `globex`, needs a different GitHub account — a
client's, not yours:

```
devmachine workspaces new globex
devmachine workspaces edit globex --share gh=own
devmachine sync
devmachine login gh --workspace globex
```

`--share gh=own` takes `globex` out of the machine-wide copy. Without it,
the next `sync` would overwrite whatever `globex` signed in with, and
nothing would say why. `--share gh=machine` puts it back.

### 6. Store the Cloudflare token and deliver it

```
devmachine secrets set cloudflare
```

This asks for the value without echoing it back, and stores it in the OS
keychain — never in a file you would commit. `devmachine secrets list`
shows only the name `cloudflare`, never the value.

```
devmachine credentials push
```

`push` writes what is missing, and a token it delivered before whose value
you have since changed with `secrets set`. It skips `gh` and `claude` — nobody can
push a browser login — and delivers the `cloudflare` token to
`/etc/devmachine/cloudflare/env` on the machine, where the package reads it.
Nothing is printed: not the plan, not the result.

### 7. Check it all landed

```
devmachine credentials list
devmachine doctor
```

`credentials list` now shows every row as `stored`. `doctor` checks that
the CLI can still reach and operate the machine — worth a look after any
round of logins, since a bad key or a locked-out account shows up there
first.

### 8. Store a secret your own app needs

The steps above are all credentials a *package* declared. Say `acme`'s
own code reads a `STRIPE_KEY` — nothing in this CLI knows that, and
nothing has to:

```
devmachine secrets set STRIPE_KEY --workspace acme
devmachine credentials push
```

This stores the value under `acme/STRIPE_KEY` and, on the next push,
writes it into `~/.devmachine/env` inside `acme`'s home — a file its
shell sources on login. If the app reads an actual `.env` file instead,
deliver straight into it:

```
devmachine secrets set STRIPE_KEY --workspace acme --env-file app/.env
devmachine credentials push
```

This edits `app/.env` in place: the existing `STRIPE_KEY=` line is
replaced, or a new one is appended, and every other line is left
exactly as it was. The first time it touches a file that already
existed, it keeps a copy at `app/.env.devmachine.bak`. See
[credentials: your app's own secrets](../concepts/credentials.md#your-apps-own-secrets).

## With your agent

Open a session on your own computer (`devmachine skills add` taught it the
CLI) and say:

```text
Set up my devmachine workspace acme with Claude Code, and make sure GitHub
and Claude are both signed in. Then add the cloudflare package and get its
API token stored.
```

The agent runs `packages add`, `sync`, and `credentials list` to see what is
missing. It cannot finish `login gh` or `login claude` for you — both open a
real browser sign-in — so it tells you which terminal to finish each one in.
For the Cloudflare token, it runs `secrets set cloudflare`, which asks you
to paste the value in the terminal it opened, then runs `credentials push`
and shows you the updated `credentials list`.

**Check it:** `devmachine credentials list` shows `stored` for `gh`,
`claude`, and `cloudflare`, and `devmachine doctor` reports the machine is
reachable.

Source: [GitHub CLI — `gh auth login`](https://cli.github.com/manual/gh_auth_login)
