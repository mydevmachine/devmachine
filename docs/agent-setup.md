# Set up devmachine with a coding agent

This page is for a coding agent — Claude Code, Codex, Cursor or any other —
that a person asked to set devmachine up for them. If you are that agent,
follow it in order. Talk to the person in their language, ask one question at
a time, and never guess an answer only they know.

devmachine sets up a machine for the person to code on: a VPS they rent, a
Linux server or a Mac they already have, or a virtual machine on their own
computer. The machine runs Debian, Ubuntu, Arch Linux or macOS. Each project gets its own account on
that machine, called a workspace, with its own tools, logins and coding
agent. devmachine runs on the person's own computer and reaches the machine
over SSH.

## Rules

- **Your shell has no terminal.** A command that asks a question waits
  forever for an answer you cannot type. Use the flags this page gives, which
  ask nothing, and hand a question-asking command such as `devmachine setup`
  to the person.
- **Only the person decides what gets installed.** On a Mac, setting up may
  install the Xcode Command Line Tools, Homebrew or MacPorts, and Ansible.
  Ask the person which package manager they want, Homebrew or MacPorts, and
  whether devmachine may install what the Mac lacks. Never choose for them.
  Pass their answers as `--package-manager brew|ports` and
  `--install-prerequisites`, and only then. `--yes` never means yes to an
  install.
- **Only the person confirms a server's identity.** A server shows a code
  called the fingerprint. The person checks it against their provider's
  dashboard. Pass `--fingerprint` only after they say it matches; never take
  it from `machines scan` alone, and never answer a fingerprint prompt
  yourself.
- **Only the person types a password or signs in to a service.** When a step
  needs one, hand them the command.
- **Show before you change a machine.** Run `devmachine sync --check`, show
  the person what it will do, and run `devmachine sync --yes` only after they
  agree.
- **Use commands, not files.** Change the configuration with `devmachine`
  commands. Never edit `config.yml` by hand. To know where the configuration
  is, run `devmachine config path` — do not guess a folder.
- When something fails, read the error, then check
  [troubleshooting](troubleshooting.md), before trying anything else.

## 1. Install the CLI

Check with `devmachine version`. If it is missing, run:

```
curl -fsSL https://mydevmachine.sh/install.sh | sh
```

On macOS with Homebrew, that installs through the tap
(`brew install mydevmachine/tap/devmachine`); otherwise it downloads and
verifies the release binary for you.

Then install the skills that teach you the whole CLI, naming your own
harness — `claude`, `codex`, `pi`, `opencode`, `antigravity`, `kimi` or
`cline`:

```
devmachine skills add --agent claude --yes
```

Without `--agent` and `--yes` it asks a question for each agent it finds, and
you cannot answer it. If your harness is none of those, skip this step.
The skills take effect only in a new session, so keep following this page
either way.

## 2. Ask what you need

First ask: **do they have a server, or do they want a machine on this
computer?** A machine on this computer is free and nothing to buy, a good
way to try devmachine; but the internet cannot reach it, so it cannot show an
app at a public URL.

Then ask one question at a time.

For a server:

1. The server's address — an IP or a hostname. It must run Debian, Ubuntu,
   Arch Linux or macOS; ask which.
2. How they reach it over SSH: the login, and whether with a key already on
   the server or with a password. The login is root, or an admin login with
   passwordless sudo. A Mac has no root login: it needs an admin login with
   passwordless sudo, and Remote Login on in System Settings > General >
   Sharing. Only the person can do those two things on a Mac.
3. A name for the server, for example `vps`.
4. Where the server is, for example `hostinger`, `home` or `office`. If they
   do not say, use `external`.
5. Only if they want an app visible at a URL: a domain, and whether it is at
   Hostinger or Cloudflare. Otherwise skip this.
6. Only for a Mac: Homebrew or MacPorts, and whether devmachine may install
   what the Mac lacks (the Command Line Tools take 5 to 10 minutes). See
   [what a machine needs](how-it-works/what-a-machine-needs.md).

For a machine on this computer:

1. A name for it, for example `sandbox`: lower-case letters, digits and
   dashes.
2. Where it is. A VM on this computer is `local`; take another name, such
   as `laptop`, only if they give one.

For both:

1. A name for the first workspace, for example the project they will work on.
2. Do they want a coding agent inside that workspace? Claude Code is a
   package called `claude-code`.

## 3a. A machine on this computer

It needs [Lima](https://lima-vm.io), which runs the virtual machine. Check
with `limactl --version`. If it is missing, install it with
`brew install lima`; without Homebrew, follow
[Lima's install guide](https://lima-vm.io/docs/installation/).

Tell the person what the machine takes: Ubuntu 24.04 with 2 CPUs, 4 GiB of
memory and a 20 GiB disk. The first run downloads the image and boots it,
which takes a few minutes. Then run:

```
devmachine machines create-local <name> --add --location <location>
```

This creates the VM and adds it in one step, with no questions: it logs in
with the VM's root password (public on purpose — the VM holds nothing real),
installs a key, proves it, turns password login off, and writes the machine
to the configuration. There is no fingerprint to check here: the VM answers
only on this computer, a moment after this command made it.

Go on to [step 4](#4-check-the-machine).

## 3b. A server

Read the server's fingerprint, without logging in and without trusting it:

```
devmachine machines scan --address <address>
```

Show the person the `SHA256:…` it prints and ask them to compare it with
the one in their provider's dashboard or console. Go on only when they say
it matches.

**If their own key already logs in**, add the server yourself. Find
the key's fingerprint with `ssh-add -l` and pass it as `agent:<fingerprint>`,
or pass the path of its private key file instead. Pass `--user <login>` when
the login is not root, as on every Mac:

```
devmachine machines add --address <address> --name <name> \
  --fingerprint SHA256:… --key agent:SHA256:… --location <location> \
  [--user <login>] [--domain <domain>]
```

On a Mac, add the person's answers: `--package-manager brew` or
`--package-manager ports`, and `--install-prerequisites` only if they said
yes to the install. Without it, a Mac that lacks something stops before
anything is installed, and the error names what is missing.

**If only a password logs in**, the password must not pass through you.
Hand the person the command, to run themselves after copying the password —
in Claude Code they can type it after `!` in this session:

```
pbpaste | devmachine machines add --address <address> --name <name> \
  --fingerprint SHA256:… --location <location> --password-stdin \
  [--domain <domain>]
```

`pbpaste` is macOS; on Linux, `xclip -o -selection clipboard` does the same.

Either way, `machines add` asks nothing: it installs the key (a new one of
the CLI's own when `--key` is left out), proves it works, turns off password logins, installs
Ansible (on a Mac, through Homebrew or MacPorts), gives the machine the
`essentials` package, and writes SSH host entries so
`ssh <workspace>-devmachine` and `mosh` work from any terminal and from an
editor like VS Code Remote-SSH. Add `--tailscale` only if the person wants to
reach the machine over Tailscale too. `--domain` is accepted only for the
first machine.

`--location` only says where the machine is, so a view such as the macOS
app's network map can group machines by place; it changes nothing about how
the CLI reaches it. To change it later, run
`devmachine machines edit <name> --location <location>`. See
[where a machine is](how-it-works/machine-location.md).

**A person at a terminal of their own** can run `devmachine setup` instead:
it asks the same questions one by one, shows the fingerprint to check, and
on a Mac asks which package manager to use and whether to install what is
missing. That is the right command for a person, not for you.

## 4. Check the machine

Run `devmachine doctor`. Every line should pass or warn; fix a warning about
SSH aliases with `devmachine aliases --write`, and a missing credential with
the `devmachine login` command it names.

On a Mac, read `devmachine --format json doctor --machine <name>` before you
run `setup` again or `sync`. Each `prerequisite: <name>` entry of `checks[]`
is something the Mac still lacks. Tell the person what is missing, ask
before you install it (`devmachine setup --install-prerequisites`), and name
the steps only they can do: Remote Login, passwordless sudo, and "Allow full
disk access for remote users" when a package needs it.

The machine starts with the `essentials` package: base tools, git, a
firewall, Caddy, and `devmachine-app`, what the macOS app reads from a
machine. If they asked for a bare machine, add `--no-essentials` to
`create-local --add` or `machines add`; if they then use the macOS app, add
it alone with `devmachine packages add devmachine-app --machine <name>`.

`essentials`, `firewall` and `caddy` run on Linux only, and `sync` refuses
them on a Mac before it changes anything. For a Mac, add `--no-essentials`
to `machines add`, then add `base` and, for the macOS app, `devmachine-app`
with `devmachine packages add <package> --machine <name>`.

## 5. Create the workspace

```
devmachine workspaces new <workspace>
devmachine packages add claude-code --workspace <workspace>
devmachine sync --check
```

Skip the `claude-code` line if they wanted no coding agent. With more than
one machine configured, add `--machine <name>` to each of these. Show them
the plan, and once they agree run `devmachine sync --yes`. The first sync
takes a few minutes.

## 6. Sign in to GitHub

If they use GitHub, hand them `devmachine login gh`. It opens a terminal on
the machine with GitHub's own sign-in page, which only they can finish. Then
run `devmachine sync --yes`, which copies that login into every workspace
that uses GitHub.

## 7. Hand it back

Tell them to enter the workspace with `devmachine ssh <workspace>`, or with
`ssh <workspace>-devmachine` from any terminal or editor. If they asked for a
coding agent, they run `claude` there once to sign in.

Offer what they may want next, each in one short line:
[guides](guides/index.md) — a site with its own domain and HTTPS, a
Docker app, Claude Code opened from the phone, an agent in its own workspace.
On a machine on this computer, an app is seen through
`devmachine tunnel <workspace> <port>` instead of a URL.
