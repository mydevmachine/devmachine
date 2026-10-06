# Commands

Every command accepts:

| Flag | Meaning |
| --- | --- |
| `--config <dir>` | the configuration directory to use |
| `--format table\|json` | how to print; JSON is the stable contract |
| `--machine <name>` | which machine to act on, for commands that act on a server |
| `--help` | what this command does |

`devmachine help --json` prints the whole tree, including every flag, in
one document — read this instead of parsing help text.

## setup

```
devmachine setup [--force] [--no-harden] [--no-essentials] [--no-aliases] [--yes]
                 [--package-manager brew|ports] [--install-prerequisites]
```

Connects to your server for the first time and gets it ready to use.

With no `config.yml` yet, it asks for a machine name, an address, the
admin account, a port, a domain, and how to log in: a key devmachine
makes for itself (recommended), a key file already on your computer, or
one your SSH agent holds (for a key in a password manager). Choosing an
agent key records its public half as `agent_key:`, so only that key is
offered from then on — never every key the agent holds.

It shows the server's fingerprint first and asks you to check it against
your provider's dashboard. Say no and nothing is sent or changed.

Then it gets the server ready, in order: try the key; if that fails, ask
for the password (never shown on screen); install the key; open a **new
connection using only the key** to prove it works; turn password login
off, and ask SSH (`sshd -T`) that it really is off; install Ansible. The new machine is written with the `essentials`
package (base, git, firewall, ssh_hardening, caddy and devmachine-app — see
[what a new machine starts with](../how-it-works/what-a-new-machine-starts-with.md)),
so the first `sync` installs them; `--no-essentials` leaves it with none. Only a pinned package
release that has `essentials` gets it — an older one starts empty and says so. On a Mac,
where `essentials` does not run, the machine starts with `base`, `devmachine-app` and the
package manager package instead, and `setup` says so; with `--no-essentials`, with the package
manager package alone. If the proof step fails, nothing is locked down and
the error says where to look. See [setting up a server for the first
time](../how-it-works/trust-bootstrap.md) for why the order matters.

Works on **Debian, Ubuntu, Arch Linux and macOS**. Before it changes anything it
asks the machine what it runs (`uname -s`, then `ID` in `/etc/os-release`,
or `sw_vers` on a Mac);
anything else is named and it stops, with the machine untouched. See [what
`setup` refuses](../how-it-works/trust-bootstrap.md#it-checks-what-the-machine-runs-first).
The password is used once and written nowhere.

**On a Mac** Ansible comes from a package manager package, `mac-brew`
(Homebrew) or `mac-ports` (MacPorts), not from the CLI. A machine that
lists one keeps it. Otherwise `setup` adds the one for the manager the Mac
already has, and asks when it has neither or both (`--package-manager`).
It copies the package to `/opt/devmachine/bootstrap/<package>/`, runs its
`bootstrap check` as the admin login, lists what is missing with the time
each takes, and installs only after you agree (or with
`--install-prerequisites`). `--yes` never installs them. See [what a
machine needs](../how-it-works/what-a-machine-needs.md).

Once the machine answers, it asks two more questions: whether to write SSH
host entries to `~/.ssh/config` (default yes — `ssh <workspace>-devmachine`
and `mosh` then work from any terminal, and the CLI keeps the entries up to
date on its own from then on; see [SSH
aliases](../concepts/reaching-your-server.md#ssh-aliases)), and whether to
reach the machine over Tailscale too (default no — adds the `tailscale`
package; `devmachine login tailscale` finishes the job, see [private
networks](../concepts/private-networks.md)).

With no `config.yml` yet, it also writes `AGENTS.md` if none exists, telling
a coding agent how to work in this folder — it never overwrites one already
there.

| Flag | Meaning |
| --- | --- |
| `--force` | discard the existing configuration and start over |
| `--no-harden` | leave password login on; the key is still installed and proved |
| `--no-essentials` | start the machine with no packages, instead of `essentials` (on a Mac, instead of `base` and `devmachine-app`) |
| `--no-aliases` | do not ask about SSH host entries, and do not write them |
| `--yes` | answer yes to the SSH host entries question, without asking; never installs prerequisites |
| `--package-manager brew\|ports` | on a Mac with neither or both, the package manager to install Ansible with |
| `--install-prerequisites` | on a Mac, install what is missing (Command Line Tools, Homebrew or MacPorts, Ansible) without asking |

Run again with a configuration in place, and it just makes sure Ansible
is installed — it never rewrites `config.yml`, a key, or SSH settings.

**Without a terminal.** `setup` only asks. A script, an agent or an app
runs `machines add --address …` instead: with no `config.yml` yet it
writes the same configuration `setup` would — the latest packages
release pinned, `defaults.workspace`, the machine with `essentials`, and
`AGENTS.md` — and runs the same bootstrap. Each question has a flag:

| `setup` asks | `machines add` flag |
| --- | --- |
| machine name | `--name` |
| address | `--address` |
| admin login | `--user` |
| SSH port | `--port` |
| location (`machines add` only) | `--location` (left out, `external`) |
| domain | `--domain` (only when it writes a new `config.yml`) |
| trust this fingerprint? | `--fingerprint` (read it first with `machines scan`) |
| how to log in | `--key new`, `--key <file>` or `--key agent:<SHA256:…>` |
| the password | `--password-stdin` |
| write SSH host entries? | yes, unless `--no-aliases` |
| reach it over Tailscale? | `--tailscale` |
| install the agent skills? | run `devmachine skills add` afterwards |
| which package manager? (a Mac with neither or both) | `--package-manager brew` or `--package-manager ports` |
| install what the Mac lacks? | `--install-prerequisites` (`--yes` never does) |

It creates that file only if it is still missing when the bootstrap
ends. If another `add` (or `create-local --add`) wrote one in the
meantime, this machine is added to it instead of overwriting it; a
`--domain` given then is not written, and the output says so.

Only `--address` does this. `machines add` with questions and no
`config.yml` stops and sends you to `setup`, which asks the same
questions plus the domain.

Unlike `setup`, it writes `config.yml` only once the bootstrap worked, so
a run that fails leaves no configuration behind. Workspaces then come
from `devmachine workspaces new`, and packages from `devmachine packages
add`, both without questions when given `--yes`.

**On a self machine** (`self: true`, your own computer — see
[`machines`](#machines)), setup runs the `mac-brew` package's bootstrap on
your computer (`mac-ports` when the machine lists it), from the package
cache, with no `sudo` and nothing under `/opt`. When Homebrew and
`ansible-playbook` are already there it installs nothing and prints `<name>
is already prepared: ansible-playbook is <path>.` Otherwise it lists what is
missing and asks first, as on a remote Mac. No fingerprint, key or password
involved.

## setup git

```
devmachine setup git [--yes] [--check]
```

Turns your configuration directory into a git repository, so you can push
it to a private remote. `config.yml`, `packages.lock`, `known_hosts` and
`AGENTS.md` are committed — see [versioning your
configuration](../how-it-works/versioning-your-configuration.md) for what
never is.

In order: writes `.gitignore` **before** `git init`, so a key can never
be picked up; `git init -b main`; commits the files above; checks what
got tracked and **refuses** if a key is already tracked, with the fix; if
`gh` is on `PATH` and logged in, offers to create a **private** repo and
push.

| Flag | Meaning |
| --- | --- |
| `--yes` | skip local-write questions; never create or push a remote |
| `--check` | say what would happen, write nothing |

Run again on an existing repository, and it skips straight to the
tracked-files check.

## doctor

```
devmachine doctor [--machine m]
```

Checks whether your machines are healthy and reports what is wrong.

With one machine, or with `--machine`, it checks that machine. With
several machines and no `--machine`, it checks every one of them, in the
order of `config.yml`, one block each:

```
machine main:
  pass  configuration       machine "main", 2 address(es), admin root, port 22, 1 workspace(s)
  pass  host key            SHA256:… at 203.0.113.10
  pass  connection          connected through 203.0.113.10
  …

machine laptop:
  pass  configuration       machine "laptop", your computer as a machine, 1 workspace(s)
  …

this computer:
  pass  cli                 0.7.21 is the latest
  pass  packages pin        v17 is the latest
```

Doctor only reads, so checking all of them is safe; it never guesses
which one you meant, because it does not have to. A machine that cannot be
reached gets its own `fail` and does not stop the others.

Six checks in order: configuration, SSH fingerprint, login, operating
system (the same check `setup` makes: Debian, Ubuntu, Arch Linux or
macOS, shown as `macos 15.7.9`), Ansible installed, SSH aliases. A broken
fingerprint stops the rest. Then one check per needed credential
(`credential: <key>`) and per installed DNS provider (`dns: <provider>`).

On a Mac, the Ansible check looks where the bootstrap said
`ansible-playbook` is, then in the Homebrew, MacPorts and pipx folders,
since a plain SSH command has none of them on `PATH`. Then the package
manager package's `bootstrap check` lists what the Mac lacks: one
`prerequisite: <name>` warning per item (`devmachine setup` installs it
once you agree), or one `prerequisites` check that passes when nothing is
missing, or is skipped when no package manager is chosen yet. The script
reaches the Mac on stdin, so doctor leaves nothing on it. Last, one
`ssh access: <workspace>` check per workspace on the Mac: it passes when
Remote Login allows all users or the account is in the list, warns when
Remote Login allows only some users and the account is not one of them,
and is skipped while the account does not exist yet. One SSH command
answers for every workspace. See [what a machine
needs](../how-it-works/what-a-machine-needs.md).

A missing or logged-out credential **warns**, with the fix (`devmachine
login <credential>`, or `secrets set` then `credentials push`); so does a
DNS provider whose token no longer works. The machine still works; only the
tool that needs the login does not. A **fail** is kept for what makes the
machine itself unusable: an invalid configuration, an unknown or changed
host key, a machine that cannot be reached, and a missing essential (an
unsupported operating system, no Ansible, or, on a self machine, a bundle
folder that cannot be written).

The exit code is `0` when every check passed, warned or was skipped, and
non-zero only when a check failed — on any machine, when there are several.
The error names the machines that failed.

`--format json` prints the same checks with the same statuses (`pass`,
`warn`, `fail`, `skip`); `ok` is `false` only when a check failed. For one
machine (or with `--machine`) the shape is:

```json
{"checks": [{"name": "configuration", "status": "pass", "detail": "…"}, …], "ok": true}
```

With several machines and no `--machine`, each machine gets its own entry
under `machines`, and the top-level `checks` holds only the checks about this
computer (`cli`, `packages pin`). The top-level `ok` is `false` when any
check anywhere failed:

```json
{
  "machines": [
    {"machine": "main", "checks": [ … ], "ok": true},
    {"machine": "laptop", "checks": [ … ], "ok": false}
  ],
  "checks": [{"name": "cli", "status": "pass", "detail": "…"}, …],
  "ok": false
}
```

The SSH aliases check runs only when `ssh_aliases: true` is set, that
is, when devmachine keeps the aliases for you. It runs `ssh -G <alias>`
for each workspace alias — a local lookup, no connection — and compares
the hostname, user, port and host key alias against what `devmachine
aliases` would write today. It passes as soon as every alias resolves
correctly, whatever file it actually lives in. Otherwise it **warns**,
never fails, naming the alias and what is wrong, with the fix
`devmachine aliases --write`.

With `ssh_aliases: false` (or left out) it reports `skip  ssh aliases
managed outside devmachine (ssh_aliases: false)`: you keep
`~/.ssh/config` yourself, so there is nothing for devmachine to compare
it to. It is not reported at all on a self machine — there is no
address to write a Host entry for.

Last come two checks about your computer, not the machine: `cli` (is this
the newest CLI release) and `packages pin` (does `config.yml` pin the newest
packages release). Either one **warns** when it is behind, with the fix
`devmachine update`, and never fails. When GitHub cannot be reached they
report `skip`. The answers are kept for 6 hours in your cache folder
(`~/Library/Caches/devmachine` on macOS, `~/.cache/devmachine` on Linux), so
running `doctor` often does not hit GitHub's rate limit.

No credential or DNS provider needed is not a failure. A check that could
not run reports `skip` and why — "I cannot tell" is not "it is not
there". A `warn` is the same idea for something worth fixing that does not
mean the machine is broken.

## config

```
devmachine config path      the directory in use, and the rule that chose it
devmachine config show      machines, workspaces and their effective values
```

Shows where your configuration lives and what is in it. `show` validates
as it prints, so it is the quickest way to find what is wrong.

With no `config.yml` yet, `show` is not an error: it says there is no
configuration, and `--format json` prints `{"machines": [], "workspaces":
[], "defaults": {"workspace": []}, "credentials": {}}`. `machines`,
`workspaces` and `defaults.workspace` are always arrays, never `null`.

`defaults.workspace` is the package list a new workspace gets (see
[workspaces](#workspaces)). `credentials` is the top-level answer per
login, `machine` or `own`; a workspace's own `credentials` (in `workspaces
list --format json`) wins over it.

## machines

```
devmachine machines list                  each machine, its addresses, port, location and workspaces
devmachine machines show [name]           one machine, and what it runs as setup, sync or doctor last read it
devmachine machines add [--location l] [--no-harden] [--no-essentials] [--no-aliases] [--yes] [--package-manager brew|ports] [--install-prerequisites]   set up another server and record it
devmachine machines add --self <name> [--location l] [--package-manager brew|ports] [--install-prerequisites]   add your computer as a machine, with no address
devmachine machines add --name <n> --address <a> --fingerprint <SHA256:…> [--user u] [--port p] [--key new|file|agent:<SHA256:…>] [--location l] [--password-stdin] [--tailscale]   the same, asking nothing
devmachine machines trust [name] [--check] [--replace] [--expect <fp>] [--yes]   check or update its SSH fingerprint
devmachine machines scan --address <a> [--port p]   the SSH fingerprint of a server not added yet; writes nothing
devmachine machines edit <name> [--set k=v] [--unset k] [--location l] [--check] [--yes]   change a machine's package settings or location
devmachine machines rm <name> [--yes]     forget a machine; the server keeps running
devmachine machines create-local <name> [--distro ubuntu|arch] [--cpus n] [--memory GiB] [--disk GiB] [--add [--key k] [--no-essentials] [--no-aliases] [--location l]]   a machine on your computer
devmachine machines start <name>          start a local machine
devmachine machines stop <name>           stop a local machine
devmachine machines delete-local <name> [--yes]   destroy it and everything on it
```

Manages the list of machines devmachine knows about.

With no `config.yml` yet, `list` is not an error: it says how to add the
first machine, and `--format json` prints `[]`.

`list --format json` prints each machine with `name`, `hosts`,
`admin_user`, `port`, `key`, `agent_key`, `workspaces`, `packages` (the
machine's own package list from `config.yml`, `[]` when it has none),
`location` (never empty: `external` or `local` when `config.yml` names
none), and `self: true` on your own computer. Without `--format json`,
`list` prints a table with a `LOCATION` column. `config show --format json` carries the
same machine entries. **Your computer is never picked by default** — a
command with no `--machine` still acts on the server, even with a self
machine also configured.

`show` prints one machine — the one named, or the one `--machine` or the
configuration picks — with what `setup`, `sync` or `doctor` last read
from it: the system, distribution and version, architecture, package
manager, init system, where `ansible-playbook` is, and the `PATH` prefix
`run` uses. It does not connect; `devmachine doctor --machine <name>`
reads the machine again. `devmachine --format json machines show <name>`
prints the same entry as `list --format json`, plus `observed`:

```json
{
  "name": "studio",
  "hosts": ["203.0.113.10"],
  "admin_user": "alice",
  "port": 22,
  "workspaces": [],
  "packages": ["mac-ports"],
  "location": "external",
  "observed": {
    "observed_at": "2026-10-05T15:22:00-03:00",
    "system": "Darwin",
    "os_family": "Darwin",
    "distribution": "MacOSX",
    "distribution_version": "15.7.9",
    "pkg_mgr": "macports",
    "service_mgr": "launchd",
    "architecture": "x86_64",
    "ansible_playbook": "/opt/local/bin/ansible-playbook-3.14",
    "path_prefix": ["/opt/local/bin", "/opt/local/sbin"]
  }
}
```

A machine no command has read yet has no `observed` key. The names
follow Ansible's facts; `path_prefix` is `[]` on Linux. Why this lives
beside `config.yml` and not in it: [what the CLI knows about a
machine](../how-it-works/what-the-cli-knows-about-a-machine.md).

`edit` changes a machine's `settings:` and nothing else, the way
`workspaces edit` does for a workspace. `--set <package>.<name>=<value>`
writes a package option, read as YAML (`--set
hostinger.zones=[example.com]` writes a list); an empty value or `--unset
<package>.<name>` removes it. Both repeat. A setting for a package the
machine does not install is refused. Comments in `config.yml` survive, and
`sync` applies the change. `--location <text>` sets or changes where the
machine is; `--location ""` clears it, back to `external` (or `local` for
your own computer).

**Location.** Every machine has one: where it is, such as `hostinger`,
`home`, `office` or `bedroom`. It is trimmed and lowercased, and must be at
most 40 letters, numbers, spaces, dots, underscores and hyphens, starting
with a letter or number. `add` asks for it, with `external` as the
default; with `--address` it takes `--location` or stays `external`.
`add --self` and `create-local` default to `local`. See [where a machine
is](../how-it-works/machine-location.md).

`add` sets up another server, same as [setup](#setup) — including the SSH
aliases and Tailscale questions. `add --self <name>` instead names the
computer devmachine runs on: no address, port or key, and neither question
is asked. Refuses if a self machine already exists, or the name is taken.
See [your computer as a machine](../how-it-works/your-computer-as-a-machine.md)
for how this differs from `machines create-local`.

`add` writes the machine to `config.yml` only once the key is proved and
the bootstrap finished. A run that fails leaves `config.yml` untouched,
so the same command can simply run again — see [`machines add` writes the
machine last](../how-it-works/trust-bootstrap.md#machines-add-writes-the-machine-last).

**Unattended.** `--address` makes `add` ask nothing, so a script or an
agent adds a machine in one command; every question has a flag, and what
is left out takes its default. With no `config.yml` yet, it writes a new
one the way `setup` does — see [setup without a terminal](#setup):

| Flag | Default | Meaning |
| --- | --- | --- |
| `--name` | (required) | the machine's name |
| `--address` | — | an IP, a hostname, or `tailscale:<name>` |
| `--user` | `root` | the admin login: root, or an account with passwordless sudo |
| `--port` | `22` | the SSH port |
| `--key` | `new` | a key of the CLI's own for this machine (made, or reused when it exists), a private key file, or `agent:<SHA256:…>` for a key your SSH agent holds |
| `--fingerprint` | — | the host key to trust on first contact |
| `--tailscale` | off | also add the `tailscale` package |
| `--domain` | — | the domain; only when there is no `config.yml` yet, and refused otherwise |
| `--password-stdin` | off | read the admin password from stdin, for a server that takes nothing else yet |
| `--package-manager` | — | on a Mac with neither or both managers, `brew` or `ports`; without it such a Mac stops |
| `--install-prerequisites` | off | on a Mac, consent to install what is missing; without it a Mac that lacks something stops |

`--key agent:SHA256:…` picks one key from the SSH agent by its
fingerprint (`ssh-add -l` lists them), the way choosing an agent key does
in the questions: its public half is recorded as `agent_key:`, and only
that key is offered from then on. If the agent does not hold it — a
locked password manager, another `SSH_AUTH_SOCK` — `add` stops before
changing anything and lists the fingerprints the agent does hold.

SSH aliases are written unless `--no-aliases`. `--fingerprint` is required
for a machine not trusted yet: with nobody to ask, trusting whatever
answers would be trust on first use with no one looking. Without it, or
with a different one, `add` stops before changing anything and prints the
fingerprint it was shown, to check through the provider console or a
connection you already trust. `machines scan --address <a>` shows the
same fingerprint before you run `add` at all.

A key that does not log in yet needs the password once, to install it.
With nobody to ask, it comes on stdin: `--password-stdin` reads all of
stdin as the password (only the final line ending is dropped), so it is
never in the command line, the shell history or a log:

```
printf '%s' "$PASSWORD" | devmachine machines add --name box --address 203.0.113.20 \
  --fingerprint SHA256:… --password-stdin
```

It is used exactly as the interactive password is: one connection that
installs the key, then a new connection that proves the key alone, then
password login is turned off (unless `--no-harden`). It is written
nowhere. When the key already logs in, the password is never used.
Without `--password-stdin`, a key that does not log in stops `add` and
says so; put the key's public half in the admin's `authorized_keys`
first, or pass the password. `--password-stdin` needs `--address`:
without it, stdin carries the answers to the questions instead.

Adding or removing a machine, like adding or removing a workspace, refreshes
the SSH aliases when `ssh_aliases: true` is set — see
[SSH aliases](../concepts/reaching-your-server.md#ssh-aliases).

A self machine has no `hosts`, `user`, `port` or `key`, and no workspace
can live on one. Any command needing a real SSH address — `ssh`, `mosh`,
`tunnel`, `login`, `expose`, `dns`, `machines trust`, `aliases` — refuses
on it, naming the reason.

`trust` reads the server's public fingerprint without logging in: asks
before saving a new one, does nothing if it matches, refuses a changed
one unless `--replace`. `--check` compares without writing and reports a
changed key instead of refusing it; it exits 0 for every status, so a
script reads `status`, not the exit code. `--expect <SHA256:…>` writes
only if the presented key has that fingerprint — the key the operator
verified, not whatever a second scan happens to meet. JSON fields:
`machine`, `address`, `status` (`matching`, `changed`, `missing`),
`key_type`, optional `current_key_type` and `current_fingerprint` (the
pinned key), `presented_fingerprint`, `check`, `changed`, and, while the
key is not yet trusted, `fix` (the command that trusts it, with
`--expect`) and `verify` (a command that prints the same key's
fingerprint on the server, to run from its own console).

`scan` reads the host key a server presents, for an address that is not
a machine yet — the step before `add --fingerprint`, so a person (or an
app) can compare it with the provider console before anything trusts it.
It does not log in, trusts nothing and writes nothing, and it needs no
`config.yml`. It reports the key the server offers for the same
negotiation `add` makes, so its fingerprint is the one `add` compares
against. JSON fields: `address` (the one that answered), `port`,
`key_type`, `fingerprint`, and `verify` — a command that prints the same
key's fingerprint on the server, to run from its own console (left out
for a key type with no standard file).

`rm` takes a machine out of `config.yml` and **does nothing to the server
itself**. Asks first unless `--yes`; refuses to leave a workspace
pointing at a gone machine. **Not `delete-local`**: `rm` only forgets a
server, `delete-local` erases a machine on your computer.

`create-local` builds a machine on your computer, arriving password-only
like a bought server — `devmachine setup` still has to run against it.
Root password `devmachine`, public on purpose: this VM holds no real
data.

By default the VM gets 2 CPUs, 4 GiB of memory and a 20 GiB disk.
`--cpus <n>`, `--memory <GiB>` and `--disk <GiB>` change that, in whole
numbers. Each is checked against this computer before Lima starts:
`--cpus` from 1 to its number of cores, `--memory` from 1 GiB to less than
its memory (the computer needs some for itself), `--disk` at least 10 GiB.
The disk is a sparse file, so a large one only uses the space the VM
writes. The output says the size the VM got.

`--distro` picks the system: `ubuntu` (the default, Ubuntu 24.04) or
`arch` (Arch Linux). The Arch VM is x86_64 under qemu on every computer,
because the only aarch64 Arch image Lima offers is an old third-party
build that does not boot under Apple's hypervisor. On Apple Silicon that
means emulation: it boots and works, but the first boot and `setup` take
many minutes. It arrives the same way as the Ubuntu one: root by
password, no key.

`create-local <name> --add` does both steps at once: it creates the VM,
then adds it the way `machines add --address` adds a server — installs a
key with that password, proves the key, turns password login off,
installs Ansible, and writes the machine (and, with no `config.yml` yet,
a new configuration). No second command, and no question. Its host key
is trusted as it answers, without `--fingerprint`: the command made the
VM a moment ago and it answers only on this computer's loopback. `--key`,
`--no-essentials`, `--no-aliases` and `--location` mean what they mean for
`machines add`, and are refused without `--add`; `--location` defaults to
`local` here. The name is checked against the
configuration before any VM is made. If adding fails, the VM keeps
running and nothing is written; the error ends with the exact `machines
add` command that adds it by hand, ready to copy. The host key is read
once, and that same key is the one trusted.
`--format json` prints the machine as `machines list` does, with its
`key` once added, its `location` and its `size` (`cpus`, `memory_gib`, `disk_gib`); the progress goes to stderr. `start`, `stop` and `delete-local` only act on a local machine.

Two limits: needs [Lima](https://lima-vm.io) (`brew install lima`), macOS
and Linux only; not reachable from the internet, so `dns`, HTTPS and
subdomains do not work on it.

## workspaces

```
devmachine workspaces list
devmachine workspaces new <name> [--machine m] [--like w] [--packages a,b] [--user u] [--check] [--yes]
devmachine workspaces edit <name> [--machine m] [--user u] [--add p] [--rm p] [--set k=v] [--unset k] [--share c=machine|own] [--check] [--yes]
devmachine workspaces defaults [--add p] [--rm p] [--check] [--yes]   no flag: print the list
devmachine workspaces rm <name> [--yes]
devmachine workspaces destroy <name> [--confirm <name>] [--check] [--keep-dns]
```

A workspace is one Linux account on one machine. See
[workspaces](../concepts/machines-and-workspaces.md).

These commands only edit `config.yml` — `devmachine sync` creates or
changes the account.

`new` takes its package list from `defaults.workspace: [pkg, ...]` in
`config.yml`. `--packages` overrides it for one workspace; `--like
<name>` copies another workspace's package list instead — packages only,
never the account or the machine.

`defaults` with no flag prints that list and changes nothing. With `--add`
or `--rm` it edits it; existing workspaces keep their own lists. With
`--format json`, both print `{"packages": ["dev", "zsh"], "changed":
false}` — `changed` is `true` after an edit, and `packages` is `[]` when
the list is empty.

`list --format json` prints `{"workspaces": [...]}`, one entry per
workspace:

```json
{
  "name": "alice",
  "machine": "main",
  "user": "alice",
  "packages": ["workspace", "dev"],
  "settings": {"zsh.theme": "plain"},
  "credentials": {"claude": "own"}
}
```

`user` is the Linux account. `packages` is `[]` when there are none, and
`settings` is left out. `credentials` is the workspace's own
`credentials:` answer per login — `own` keeps its own login, `machine`
shares the machine's — exactly what `edit --share` writes, and `{}` when
it has none. A login it does not name follows the top-level
`credentials:` (in `config show --format json`), then the package's own
`scope`.

**`new` refuses on a machine with no key**, since a workspace is reached
through the copied admin key. Run `devmachine setup` first. With several
machines, pass `--machine`.

`edit` changes one workspace. `--add`/`--rm` take a package name each,
repeatable. `--set <package>.<name>=<value>` writes a package option
(read as YAML — see [packages](../concepts/packages.md)); an empty value
or `--unset <package>.<name>` removes it. `--share <credential>=own` keeps this workspace's own login
instead of the shared one; `=machine` shares it again. A package option
for a package the workspace does not install is refused.

**Changing `--machine` does not move a workspace.** The next `sync`
creates the account on the new machine; the old one keeps everything.

**`rm` leaves the Linux account, home and files on the machine** — remove
those by hand if you want them gone.

`new`, `rm`, `destroy` and `edit --machine` all refresh `~/.ssh/config`'s
managed block afterwards, when `ssh_aliases: true` is set, and say so when
it changed. See [SSH aliases](../concepts/reaching-your-server.md#ssh-aliases).

**`destroy` deletes for real**: the account, its home, its Caddy routes,
and its `config.yml` entry. Asks you to retype the name first (or
`--confirm <name>` from a script). Needs the machine reachable; use `rm`
for one that is gone. After the account is gone, the DNS record of each
of its sites goes too, with the same rules as `expose rm`: only an A
record that still points at the machine serving the site (the `via`
machine for one published through it), through the provider that holds
the zone. Anything else is printed, and a DNS failure never stops the
destroy. `--check` also prints those DNS removals. The list it shows
before asking names each record it would remove; `--keep-dns` leaves
every one alone instead, and prints the `dns rm` that removes each
later.

`defaults` only changes `defaults.workspace`, which new workspaces
inherit; existing ones are unchanged.

## skills

```text
devmachine skills add [--package name] [--agent claude|codex|opencode|pi|antigravity|kimi|cline] [--yes]
devmachine skills list
devmachine skills update [--yes]
devmachine skills remove <name> [--yes]
```

Manages Agent Skills on your own computer; never touches a machine.

Bare `add` installs `devmachine-skills` from the pinned release (or the
latest, before `setup` has pinned one). `--package` only accepts a local
package. Without `--agent`, it detects installed agent tools and asks.

`list` shows each source, its skills and agent tools. `update` reinstalls
every recorded source. `remove` takes one exact skill name.

Real copy: `~/.agents/skills/<name>`. Codex, OpenCode, Pi and Kimi read
it there. Claude, Antigravity and Cline get a link to it:

| Agent | Link |
| --- | --- |
| `claude` | `~/.claude/skills/<name> -> ../../.agents/skills/<name>` |
| `antigravity` | `~/.gemini/antigravity-cli/skills/<name> -> ../../../.agents/skills/<name>` |
| `cline` | `~/.cline/skills/<name> -> ../../.agents/skills/<name>` |

Detection looks for `~/.claude`, `~/.codex`, `~/.config/opencode`,
`~/.pi`, `~/.gemini/antigravity-cli`, `~/.kimi-code` and `~/.cline`.

## aliases

```
devmachine aliases [--write] [--path p] [--check] [--yes]
```

Prints one SSH `Host` entry per workspace, so `ssh acme-devmachine` works
from an ordinary terminal, an editor or an app.

```
# >>> devmachine — generated, do not edit
Host acme-devmachine
    HostName main
    User acme
    Port 22
    ProxyCommand /opt/homebrew/bin/devmachine --config /home/you/.config/devmachine ssh-proxy main %p
    IdentityFile /home/you/.config/devmachine/keys/main
    IdentitiesOnly yes
    HostKeyAlias main-devmachine
    StrictHostKeyChecking yes
    UserKnownHostsFile "/home/you/.config/devmachine/known_hosts"
    GlobalKnownHostsFile /dev/null
    UpdateHostKeys no
    CheckHostIP no
    VerifyHostKeyDNS no
    KnownHostsCommand none
    HostKeyAlgorithms ssh-ed25519
# <<< devmachine
```

`--write` puts this block in the aliases' file, after asking first. **Only
the text between the two markers is ever replaced** — the rest of the
file may hold hosts devmachine knows nothing about.

The aliases live in **one file**: `ssh_aliases_path` in `config.yml`, or
`~/.ssh/config` when that is not set. Every writer uses it — `--write`,
the automatic refresh after a workspace or machine changes, `setup` and
`machines add` — and so does `doctor`'s check. `--write --path <file>`
moves them: it writes the block there, records the file as
`ssh_aliases_path`, and empties the block in the file they lived in
before, because ssh keeps the first value it reads and a stale copy read
first would win. Use it when `~/.ssh/config` is generated by something
else and `Include`s a file of its own.

- `ProxyCommand` runs [`ssh-proxy`](#ssh-proxy) when ssh connects, which
  picks the first of the machine's addresses that answers at that moment.
  The file holds no address, so a private network going up or down never
  makes it stale. `HostName` is the machine's name, and nothing dials it.
  The path is the `devmachine` on your `PATH`, written in full.
- **When `devmachine` is not on your `PATH`,** the entry has no
  `ProxyCommand`, and `HostName` is the first address that resolves now,
  as in older versions. Run `aliases --write` again when that changes.
- `HostKeyAlias` is the same for every alias of one machine, so ssh checks
  the pinned key whichever address it came through.
- `IdentitiesOnly yes` goes with `IdentityFile`, so ssh offers only this
  key and does not burn login attempts on others in your agent.
  `IdentityFile` points at `key:` when the machine has one, or at the
  recorded `agent_key:`'s public file otherwise — ssh matches a public-key
  `IdentityFile` against the agent instead of reading a private key from it.
  With neither, there is no `IdentityFile`, and ssh offers everything the
  agent holds.

A `-pub` alias goes straight to the machine's first literal address,
without `ProxyCommand`. With `ProxyCommand` it is written when the machine
has more than one `hosts` entry; without, only when a second address exists
and the first did not already resolve to it.

`mosh acme-devmachine` needs `--experimental-remote-ip=remote` with the
`ProxyCommand` form: mosh's default replaces the ProxyCommand with its own.
See [SSH aliases that resolve when you connect](../how-it-works/addresses-and-fallback.md#ssh-aliases-that-resolve-when-you-connect).

`--format json` adds `proxy_command` to each alias that has one.

## resolve

```
devmachine resolve [--machine m] [--format json]
```

Prints the machine's addresses in the order every connection tries them,
without connecting. A `<prefix>:<name>` entry is asked of the network package
that declares the prefix, and a DNS name is looked up, so every address is an
IP. An entry that gave no address is listed under `skipped`, with the reason.

```
ADDRESS                                  FROM                         PACKAGE
100.64.0.5                               tailscale:main               tailscale
203.0.113.10                             203.0.113.10                 -

skipped:
  tailscale:vps                tailscale is not running
```

The JSON is what the macOS app reads, and its shape is stable:

```json
{
  "machine": "main",
  "addresses": [
    {"address": "100.64.0.5", "source": "tailscale:main", "package": "tailscale"},
    {"address": "203.0.113.10", "source": "203.0.113.10", "package": ""}
  ],
  "dropped": [
    {"source": "tailscale:vps", "reason": "tailscale is not running"}
  ]
}
```

- `addresses` is in try order. `address` is always an IP address.
  `source` is the `hosts` entry it came from; `package` is the network
  package that resolved it, empty for a literal entry. An entry that
  gives several addresses appears once per address.
- `dropped` is every entry that gave nothing, with why. Both lists are
  `[]` when empty, never missing.
- A `tailscale:` entry resolved by the deprecated built-in resolver
  reports `"package": "tailscale"`.

A mosh client needs an IP for its UDP connection: this is where to get it.
Refused for a `self: true` machine, which has no address. See
[addresses and fallback](../how-it-works/addresses-and-fallback.md).

## ssh-proxy

```
devmachine ssh-proxy <machine> <port>
```

The `ProxyCommand` the SSH aliases run; hidden from `help`. It resolves the
machine's addresses like `resolve`, connects to the first that accepts a TCP
connection on `<port>`, and passes bytes between ssh (its standard input and
output) and the machine until either side closes. Only the connection is
retried on the next address — once one answers, the SSH handshake is under
way there. When none answers it exits 1 with every address and why on
standard error, which ssh shows as the reason the connection closed.

## stats

```
devmachine stats [--machine m]
```

Prints memory, swap, disk and load. The table rounds; JSON gives raw byte
counts.

## ssh, mosh

```
devmachine ssh [workspace]
devmachine mosh [workspace]
```

Opens an interactive session: a workspace's account if named, the
machine's admin if not.

Both run the real `ssh`/`mosh` program, so your terminal, agent and tmux
behave normally. Both connect to the first address [`resolve`](#resolve)
lists. `mosh` survives a dropped or roaming connection and
needs mosh on both sides. Both check `<config>/known_hosts` first.

## run

```
devmachine run "<command>" [--workspace w]
devmachine run --package <name> [--workspace w] -- <command> [args...]
```

Runs one command on a machine and prints its output; exits with the same
code. A failed command's output prints before the error, since it usually
explains the failure.

`--package` calls an installed package's entrypoint directly — everything
after `--` goes to the package; `commands:` in its manifest can limit
what it accepts. With `--workspace`, it runs as that workspace's own
account, for a command that needs that account's own files or logins.
See [packages](../concepts/packages.md).

`run` keeps its SSH connection open for five minutes and reuses it, so a
script calling it every few seconds skips the handshake each time.

On a machine whose package manager lives outside the system `PATH` — a
Mac with MacPorts or Homebrew — `run` puts the machine's `path_prefix`
(see `machines show`) in front of `PATH`, so `devmachine run 'port
installed'` finds `port` the way `run 'apt list --installed'` finds
`apt` on Debian. A Linux machine has no prefix, and the command goes as
written.

## upload

```
devmachine upload <file>... [--workspace w] [--dir path] [--mode 0600]
```

Sends local files into a home on a machine and prints, one per line, the
absolute path each one landed at. With `--format json` it prints
`[{"local": "...", "remote": "...", "bytes": N}]`; a file that failed has
an `error` field instead of `remote`.

- **Where.** `--workspace` sends into that workspace's home, as its own
  account. Without it, the files go to the home of the machine's admin,
  on the machine `--machine` names, or the only one configured. A
  workspace and a `--machine` it does not live on is refused.
- **Folder.** `~/.cache/devmachine/uploads` by default. `--dir` names
  another folder, relative to the home (`notes`, `~/notes`) or absolute
  inside it (`/home/acme/notes`). A folder outside the home is refused,
  and so is one that leaves it through a symbolic link. Missing folders
  are created, mode `0700`.
- **Names.** Each file keeps its name, with the local time before the
  extension: `report.pdf` becomes `report-20260930-143012.pdf`,
  `photo.final.png` becomes `photo.final-20260930-143012.png`, and
  `Makefile` and `.env` get it at the end. A name already taken gets
  `-2`, `-3` and so on: nothing is ever overwritten. Spaces and accents
  are kept; only `/`, line breaks and NUL become `_`.
- **Mode.** `0600` unless `--mode` says otherwise.
- **Failures.** A folder, a missing file or one you cannot read is
  refused before anything connects. With several files, every one is
  tried; the command exits non-zero if any failed, and names each on
  stderr.

stdout holds only the paths, so a script or an app can read them. Each
upload is one line in [the command log](#the-command-log). See
[how an upload lands](../how-it-works/uploads.md).

## download

```
devmachine download <remote-path>... [--workspace w] [--to dir]
```

Brings files or folders from a home on a machine to this computer and
prints, one per line, the local path each one was saved at. With
`--format json` it prints
`[{"remote": "...", "local": "...", "bytes": N, "folder": false}]`; a
path that failed has an `error` field instead of `local`.

- **Who reads.** `--workspace` reads as that workspace's own account, so
  it gets exactly what that account can read. Without it, the machine's
  admin reads, on the machine `--machine` names, or the only one
  configured. A workspace and a `--machine` it does not live on is
  refused.
- **Which path.** Relative to the home (`proj/report.pdf`), starting with
  `~/`, or absolute (`/home/acme/proj/report.pdf`). Put `--` before a
  path that starts with `-`.
- **Where to.** `~/Downloads` by default. `--to` names another folder,
  which must already exist: a typo is refused before anything connects.
- **Names.** Each file keeps its name. A name already taken gets `-2`,
  `-3` and so on before the extension: nothing is ever overwritten.
- **Folders.** A folder arrives as one `<name>.tar.gz`, with the folder
  at its top. Unpack it with `tar xzf <name>.tar.gz`, or double-click it
  in Finder.
- **Failures.** With several paths, every one is tried; the command exits
  non-zero if any failed, and names each on stderr. A failed or cut
  transfer leaves nothing behind.

stdout holds only the paths, so a script or an app can read them. Each
download is one line in [the command log](#the-command-log). See
[how a download lands](../how-it-works/downloads.md).

## dns

```
devmachine dns status [host]
devmachine dns providers
devmachine dns list [zone] [--dns-provider p] [--zone z]
devmachine dns check <name> [--dns-provider p] [--zone z]
devmachine dns add <name> <type> <value> [--dns-provider p] [--zone z] [--check] [--publish]
devmachine dns rm  <name> <type> [value] [--dns-provider p] [--zone z] [--check] [--yes]
```

Manages DNS records through an installed provider package (Hostinger,
Cloudflare). See [DNS providers](../how-it-works/dns-providers.md) for
what differs between them.

`status` checks a name from the outside — resolves, certificate accepted,
answers a request. Defaults to `domain`. A 4xx counts as serving; a 5xx
does not.

`providers` lists every installed provider and the zones it can see — run
first when a DNS command surprised you.

`list`/`check` ask the registrar, unlike `dns status` which asks the
public internet. `check` exits non-zero when a name is not set.
`--dns-provider` picks a provider directly; `--zone` only for a token
that cannot list its own zones. Which provider answered goes to stderr.

`add` makes a name hold **exactly** one value, replacing what was there,
after showing the zone, provider and value it replaces. `--check`
previews; `--publish` is the non-interactive consent (`--yes` alone never
grants it).

`rm` removes one value, or every value at that name and type with none
given — not always one atomic step on the registrar's side, so it warns
first. A name is always the full name (or the zone itself for the apex).

## expose

**HTTPS only.** Caddy handles HTTPS for you; anything else, or anything
only you should reach, uses [`devmachine tunnel`](#tunnel) instead.

```
devmachine expose add <workspace> <port> --host <host> [--via <machine>] [--check] [--publish] [--no-apply]
devmachine expose list
devmachine expose rm <host> [--check] [--yes] [--no-apply] [--keep-dns]
```

Publishes a workspace's port to the internet, over HTTPS, at a hostname
you choose.

`add` records the site in `config.yml`, points the hostname at the
machine's first public address in `hosts` (same as `dns add`; a private
network address listed first is skipped, since visitors cannot reach it), and then puts it on Caddy at once: it writes
the workspace's routes file on the machine and reloads Caddy. That takes
seconds, not a whole `sync` — and it is the very file `sync` writes, so the
next `sync` has nothing to change. **Refuses if `caddy` is not on the
machine.** It asks for confirmation first — the port becomes reachable by
anyone who learns the hostname; see [tunnel](#tunnel) for what should not
get a yes. `--publish` is the non-interactive way past that.

- When Caddy refuses the new file, the old one stays, Caddy's own error is
  printed, and the command fails. The route stays in `config.yml` as
  `pending`: fix it and run `sync`.
- When the machine cannot be reached, the route is recorded as `pending`
  and the command still succeeds. The next `sync` publishes it.
- `--no-apply` only records, the way `add` worked before: the next `sync`
  publishes it.
- `--check` changes nothing. It prints the line `config.yml` would gain
  and the routes file as it is on the machine against what it would
  become (`+` added, `-` removed).
- When a DNS provider refuses the record (a rejected token, a zone it
  cannot change) or its setup is broken, the site is still put on Caddy:
  the command says why, prints the record to create by hand, and its JSON
  carries `dns_error`.
- With `--format json` it prints `host`, `port`, `workspace`, `applied`
  (`true` once Caddy serves it) and `status` (`published` or `pending`),
  with a `note` saying why when it is pending. A site published with
  `--via` also carries `via`, and a `note` when that machine cannot reach
  the port.
- `--via <machine>` publishes through another machine's Caddy, for a
  workspace on a machine the internet cannot reach (a VM on your desk, a
  box behind NAT). The route is recorded with `via:`, the name points at
  that machine, and its Caddy proxies to the workspace's machine at the
  address its `hosts` give — one from a private network first, never a
  loopback one. Without `--via`, a workspace whose machine has no caddy is
  refused with the machines that have it. See
  [publishing through another machine](../how-it-works/published-sites.md#publishing-through-another-machine).

See [why a published site lives in the configuration](../how-it-works/published-sites.md).

`list` prints every host the machine's Caddy serves — those sent to it
with `--via` too, naming the machine they come from — with its port,
workspace, and one of four words: `published` (both agree), `pending`/`differs` (needs `sync`),
`unmanaged` (only the machine has it — adopt with the `add` shown).
Unreachable machine or missing `caddy`: rows print `unknown`.

`rm` takes a host out of the configuration and off Caddy at once, on the
machine that serves it whatever `--machine` says, the same way `add` puts
it on: the workspace's routes file is written without
it (or removed, with no route left) and Caddy reloads. With the machine
out of reach it keeps serving the site until the next `sync`.
`--no-apply` and `--check` work as for `add`; in JSON, `status` is
`removed` or `pending`. A host the configuration does not know is
refused, with how to adopt or remove it by hand.

After Caddy, `rm` removes the A record `add` created — through the
provider that holds the zone, and only while its value is still the
serving machine's address (the `via` machine for a site published
through one):

- A record that points somewhere else now is left alone, and the command
  says where it points. Somebody repointed the name; deleting it would
  take down whatever answers there.
- With no provider installed, or the machine out of reach, it prints the
  record to remove by hand, and the JSON carries `dns_error` saying so —
  the record is still there.
- A name that holds the machine's address **and** another value is not
  deleted through the provider: some providers take one value out by
  rewriting the whole set, and a failure halfway would take the other
  value down too. The record to remove by hand is printed, and the JSON
  carries `dns_error`.
- When the provider cannot list the zone or refuses the delete, nothing
  is deleted, the site still comes off Caddy, the record to remove by
  hand is printed, and the JSON carries `dns_error`.
- The question it asks says so: "Stop publishing https://<host>, and
  remove its DNS record while it points at <machine>?".
- `--keep-dns` takes the site off Caddy and leaves the record alone, for
  a name you will point somewhere else yourself. It prints the `devmachine
  dns rm … --machine <machine>` that removes it later, and the question
  no longer mentions DNS.
- `--check` also prints the DNS removal it would make.
- `--no-apply` touches no machine, so no DNS either: it prints the
  `devmachine dns rm <host> A <address> --machine <serving machine>` to
  run later — `--machine` names the machine whose DNS provider holds the
  zone, which is the serving one, not necessarily the only one. (`add --no-apply`
  still points the name, since a name that does not resolve yet stops
  the certificate on the next `sync`.) See
[Publishing](../concepts/publishing.md) for the cases this question
exists to catch.

## tunnel

```
devmachine tunnel <workspace> <port> [--local <port>]
```

Opens an SSH tunnel so a remote port shows up as `localhost:<port>` on
your computer. **Nothing is published** — no DNS, no certificate, no
Caddy, only you can reach it.

Use this instead of `expose` for anything not plain HTTP, or that only
you should see: a database tool with real data, an inbox with real mail,
a queue dashboard that can drain a queue.

| Traffic | Anyone | Only you |
| --- | --- | --- |
| **HTTP** | `expose` | `tunnel` |
| **Anything else** | nothing | `tunnel` |

`--local` picks the port on your computer, if the remote one is already
taken here. Holds your terminal open while the tunnel is up; Ctrl-C
closes it, nothing left running.

## machine

```
devmachine machine setup
devmachine machine doctor
```

Sets up and checks your own computer — the one thing you still install by
hand, once.

`doctor` checks `ssh`/`mosh` on `PATH` (mosh is a warning only), an SSH
agent or configured key, and whether `~/.ssh/config` matches what
`devmachine aliases --write` would produce now. That last one runs only
with `ssh_aliases: true`; otherwise it reports `skip` — you keep the file
yourself.

`setup` installs what is missing via Homebrew on a Mac; on Linux it names
what to install instead of guessing. Never installs an editor, shell
plugins or language runtimes — that stays your choice.

## secrets

```
devmachine secrets set <name> [value] [--stdin]
devmachine secrets set <name> [value] --workspace w [--env-file path] [--push]
devmachine secrets list [--workspace w]
devmachine secrets rm <name> [--workspace w] [--from-file]
devmachine secrets example
```

Stores values packages need that are not logins — API keys, tokens.

`set` with no value asks without echoing, so it never reaches your shell
history. `list` prints names only.

Values go to the OS keychain when there is one, and otherwise to
`secrets.json` in the configuration folder, readable only by you.
`DEVMACHINE_KEYCHAIN=off` skips the keychain and always uses the file —
the tests and the acceptance suite set it, so a run never writes to, or
prompts about, your own keychain.

`example` lists which `<NAME>=` a machine's packages need, no values,
always to stdout — never to a file, since `.env.example` sits one typo
from `.env`.

### A workspace's own secret

`--workspace` is a different thing from the plain form above: not a
value a package declared, but your own app's secret — a key your code
reads. It stores the value under `<workspace>/<name>` and delivers it
on the next `devmachine credentials push`, or right away with `--push`.

By default it lands in `~/.devmachine/env` — see
[the `~/.devmachine/env` contract](#the-devmachineenv-file). `--env-file
<path>` delivers into that dotenv file instead, relative to the
workspace's home: the existing `<NAME>=` line is replaced, or a new one
appended, and everything else in the file is left exactly as it was. A
path that would reach outside the workspace's home is refused — here
for `../` or an absolute path, and on the machine for a symbolic link
that leads out of it. The
first time it edits a file that already existed, it keeps a copy at
`<path>.devmachine.bak`. See [credentials: your app's own
secrets](../concepts/credentials.md#your-apps-own-secrets).

Setting the same name again with a different `--env-file` (or with none,
back to the default) moves it: the next push takes the `<NAME>=` line
out of the file it was in before, then writes it to the new one.

`list --workspace w` shows only that workspace's own secrets, with
where each is delivered. `rm --workspace w --from-file` also removes
the name from its file, on the next `credentials push` — it is not
edited here, so `rm` never needs to reach the machine.

### The `~/.devmachine/env` file

A sourceable file inside every workspace, `KEY='value'` per line,
0600, owned by the workspace's own account. It holds every workspace
secret delivered with no `--env-file`. A workspace's shell is expected
to source it on login — the `zsh` package does — so `export`ing
anything more is never necessary.

## login

```
devmachine login <credential or network package> [--workspace w] [--machine m]
```

Runs the login a package declared, in the account it belongs to, over a
real terminal session (`ssh -t`) — a device code or browser prompt has to
reach a person.

A workspace credential needs `--workspace`: no single session to copy
between accounts. A machine credential logs in once, as the admin, and
copies what the tool wrote to `/etc/devmachine/<name>/`; the next `sync`
spreads it to workspaces using that package. A `kind: secret` credential
is refused — use `devmachine secrets set`, then `devmachine credentials
push`.

**A network package** — one whose `package.yml` has a
[`network:` block](package-format.md#network) with `join` — is logged in to
by its package name: `devmachine login tailscale`. Instead of a declared
command, it runs the package's `join` script on the machine, as the admin,
over `ssh -t`, with the machine's settings for that package (for example
`tailscale.login_server`). Then it runs the package's `self_name` script
and, unless the machine's `hosts` already has it, adds `<prefix>:<name>`
above the other addresses in `config.yml`, writing `hosts:` as a block list,
one address per line, with any comment next to an address kept. The public
address stays as a fallback. When the name cannot be read, it prints the
machine's `hosts:` block to paste instead, with `- <prefix>:<name>` first.
The package must be synced first: `join` runs from where `sync` put it.
`--workspace` is refused: a network joins the machine.

With a packages release whose `tailscale` package has no `network:` block
yet, `login tailscale` runs its declared `tailscale up`, then reads the name
from `tailscale status --json` on the machine and adds `tailscale:<name>`
the same way. This path is deprecated. See
[private networks](../concepts/private-networks.md) and
[the network package contract](network-package-contract.md).

Same strict fingerprint check as every other command. See
[SSH: logging in and knowing it is your server](../how-it-works/ssh.md).

## credentials

```
devmachine credentials list [--machine m]
devmachine credentials push [--machine m] [--check] [--yes]
```

Shows what a machine's packages need to authenticate, and what is
missing. Every row names the fixing command. `unknown` means the package
never said where its tool keeps the result — not the same as missing.
Never prints a value.

`push` delivers the values that are missing, and a token it delivered as
an env file that the machine holds an older copy of: it asks the machine
only for a SHA-256 of that file, so a token rotated with `secrets set`
reaches the machine on the next push (reported as `replaced`) without a
value ever crossing back. A file a tool keeps for itself (`path:`) is never
rewritten once it is there. It skips
logins (nobody can push a browser session) and names any secret never
stored; exits non-zero if it found one. A workspace's own value (`<workspace>/<name>`) wins over
the shared one. `--check` previews and writes nothing.

It also delivers every workspace's own secret set with `secrets set
--workspace` (see above) whose workspace lives on the machine being
pushed to, and removes the ones marked with `secrets rm --from-file`.

Anything it delivers inside a workspace's home — a package's workspace
credential or a workspace's own secret — is refused when a symbolic link
on the way leads out of that home, and the file is replaced, never
written through, so a link in its place cannot redirect the write.

## packages

```
devmachine packages list
devmachine packages add <name> [--machine m | --workspace w] [--check] [--yes]
devmachine packages rm  <name> [--machine m | --workspace w] [--check] [--yes]
devmachine packages new <name> [--scope machine|workspace] [--into <dir>]
devmachine packages validate <dir>
devmachine packages schema [--json]
devmachine packages help <name> [--json]
devmachine packages pin [release]
devmachine packages outdated
```

Manages what is installed on your machines and workspaces. See [the
package format](package-format.md).

`list` shows each package once with every machine and workspace that
uses it; one nothing provides is listed as `missing`.

`list --format json` prints `{"release": "v17", "packages": [...]}`:
`release` is the packages release the list was read from (empty when
nothing is pinned, so only your own packages are listed), and one entry
per package:

```json
{
  "name": "hostinger",
  "scope": "machine",
  "source": "release",
  "summary": "DNS zones on Hostinger.",
  "category": "DNS",
  "kind": "dns",
  "platforms": [],
  "needs": [],
  "credentials": [
    {"name": "hostinger", "kind": "secret", "scope": "machine", "env": "HOSTINGER_API_TOKEN", "shareable": false}
  ],
  "variables": [
    {"name": "zones", "summary": "Zones to manage.", "default": [], "type": "list"}
  ],
  "installed_on": ["machine main"]
}
```

`kind` is the contract a callable package answers — `dns` marks a
[DNS provider](../how-it-works/dns-providers.md) — and is left out for an
ordinary package. `platforms` are the operating systems it runs on
(`linux`, `macos`), and `[]` means any — a package that says `["macos"]`
is for your own computer, never for a server. `needs` are the packages
it brings in before itself, `[]` when none: for `essentials` that is the
list it is made of. `category` is the manifest's grouping, left out when it
has none. `credentials` lists what the package declares, always an array:
each one's `name`, `kind` (`secret`, `file` or `manual`), `scope`, and
`env` or `path` where the value is delivered. **It never carries a
value** — it is there so a client knows the name to store with `secrets
set` before the package is added. `shareable` is `true` when a workspace
can take that login from the machine — `workspaces edit --share
<name>=machine` — because the package says a copy of its session works on
another account. When it is `false`, `sync` refuses that choice (see
[troubleshooting](../troubleshooting.md#credential-x-cannot-be-shared)), so
the only answer is `own`; it is always `false` for a secret or a file.
`variables` lists the settings the
manifest declares, sorted by name, always an array: each one's `name`,
`summary`, the manifest's `default` (`null` when it has none) and `type`
(`string`, `number`, `boolean`, `list` or `map`), read off the default and
left out when there is no default. It is what `--set <package>.<name>=…`
on `machines edit` or `workspaces edit` accepts. **It carries the
manifest's default, never a value you set** — your values are in
`workspaces list` and `config.yml`. `installed_on` is `[]` when nothing
uses the package.

With no `config.yml` yet, `list` reads the **latest** packages release —
the one `setup` would pin — so a new user sees what a first machine can
start with. With no network to find it, it fails and says so.

`add`/`rm` only edit `config.yml` — `sync` applies the change. Pass
`--machine` or `--workspace`; with one configured machine, that is the
target. Comments in `config.yml` survive.

`new` writes a package that already passes `validate`; refuses to
overwrite one that exists. `validate` reports every problem at once, with
file and line. `schema` prints the `package.yml` format this binary
reads. `pin` writes `packages: <release>` — with none given, the latest;
a release is a tag such as `v8`, never a branch. `help` asks an installed
package what it accepts, by running its own `help`.

`outdated` says whether a packages release newer than your pin is out. It
changes nothing; `devmachine update` moves the pin. `--format json`
prints:

```json
{"pinned": "v32", "latest": "v33", "newer": true, "notes_url": "https://github.com/mydevmachine/packages/releases/tag/v33"}
```

`pinned` is empty and `newer` is `false` when nothing is pinned, or there
is no `config.yml` yet. `newer` is `false` when your pin is the newest or
newer. `notes_url` is the release page for `latest`. The newest release is
asked of GitHub at most once a day and kept in your cache folder; `update`
and `packages pin` ask again and store the answer. Offline, the last answer
is used, however old. Offline with no answer ever stored is an error and a
non-zero exit (with `--format json`, nothing on stdout and the error on
stderr). Exits 0 whether or not a newer release is out.

## sync

```
devmachine sync [--machine m] [--check] [--yes] [--tags a,b]
```

Applies your configuration to a machine — installs what is missing,
updates what changed.

In order: checks the configuration, fetches the pinned release if needed,
plans what the machine and its workspaces should get, checks your own
packages, prints the plan, asks, then runs Ansible on the machine,
streaming output.

| Flag | Meaning |
| --- | --- |
| `--check` | dry run: reports what would change, changes nothing |
| `--yes` | apply without asking |
| `--tags a,b` | only the packages named |

One tag beyond package names: `credentials`, which only copies shared
logins — run after `devmachine login` instead of a full sync.

`--check` never writes the lock file. Only your own packages (in
`<config>/packages/`) are checked before the run. Every package's
`platforms` is checked against the machine's system before the machine is
changed — from what was last read, then again from what `sync` reads as it
connects — and a package for another system stops the sync, naming both
([troubleshooting](../troubleshooting.md#package-x-runs-on-linux-machine-y-is-macos)).
`sync` also keeps what it read in `<config>/state/machines/<name>.json`
(see `machines show`). With `--format json`,
stdout is the result; the plan and machine output go to stderr.

On success, `<config>/packages.lock` records what was applied, at which
release and checksum, for the machine synced. With `--tags`, it records only
the packages named: every other package keeps the entry the last full sync
left, and one that never ran stays out of the lock, so `run --package` and
the next `sync` see what is really there. The files a package added to
another's folder (`extends`) are recorded only for the packages named, and
none of the old ones is forgotten, since the step that removes them did not
run. The whole bundle is still sent, so every package's entrypoint comes from
the pinned release. It also refreshes
`~/.ssh/config`'s managed block, when `ssh_aliases: true` is set, and
prints how to reach each workspace on that machine: `devmachine ssh <ws>`
always, and `ssh <ws>-devmachine` when aliases are on.

## update

```
devmachine update [--machine m] [--skip-cli] [--skip-packages] [--yes]
devmachine update --no-machines [--skip-cli] [--skip-packages] [--format json]
devmachine update --cli-only [--format json]
```

Brings everything up to date, then stops before it changes a machine. Five
steps, in order, each with a short header:

1. **CLI** — asks GitHub for the newest release. If it is newer: when
   Homebrew installed the CLI, runs `brew update`, `brew trust --formula`
   (only on a Homebrew that has it) and `brew upgrade
   mydevmachine/tap/devmachine`; otherwise downloads the release archive for
   your system, checks it against the release's `checksums.txt`, and swaps
   it in for the running binary. Then the new binary runs the other steps.
   A development build (`devmachine version` says `dev`) is never replaced.
2. **Packages** — when `config.yml` pins an older packages release, pins the
   newest, like `packages pin`. Only `config.yml` changes (and, in a
   versioned configuration, one commit). Nothing is pinned when nothing was.
3. **Skills** — `skills update`, for the skills on your computer, from the
   release just pinned.
4. **Doctor** — `doctor` on every machine (or the one `--machine` names).
   Warnings and failed checks are shown, and the run goes on. A machine
   that cannot be reached skips step 5.
5. **Sync check** — `sync --check` on every machine it reached, and the
   tasks each one would change. If any would change, it asks once: `Apply
   these changes with sync? [y/N]`. Yes runs `sync` on those machines. No,
   an empty answer, or no terminal at all prints the exact `devmachine sync`
   command to run later.

| Flag | Meaning |
| --- | --- |
| `--skip-cli` | leave the CLI as it is |
| `--skip-packages` | leave the packages pin as it is |
| `--yes` | answer yes to the sync question — this changes machines; use it only in automation you trust |
| `--no-machines` | run steps 1 to 3 and stop: no doctor, no sync check, never a question. It takes `--skip-cli` and `--skip-packages`, and refuses `--yes`, `--cli-only` and `--machine` |
| `--cli-only` | run step 1 alone and stop: no packages pin, skills, doctor or sync, and no configuration is read. It cannot be combined with the other flags or `--machine` |

With `--cli-only`, a build from source (`devmachine version` says `dev`)
is an error, not a skip: there is no install to update, so it says what to
run instead. With `--cli-only --format json`, the log goes to stderr and
stdout gets one object at the end:

```json
{"from": "0.7.22", "to": "0.7.23", "method": "homebrew", "status": "updated", "ok": true}
```

`method` is `homebrew`, `download` (the release archive) or `source`;
`status` is `updated`, `already latest` or `failed`, and a failure adds
`error` with the reason and exits non-zero.

With `--no-machines --format json`, the log goes to stderr and stdout gets
one object at the end:

```json
{"cli": {"from": "0.7.30", "to": "0.7.31", "updated": true}, "packages": {"from": "v32", "to": "v33", "pinned": true}, "skills": {"updated": true}}
```

Every field is always there. A step that changed nothing has `to` equal to
`from` and `false`; so does a skipped one. `packages.from` is empty when
nothing is pinned, and then nothing is pinned. When a step fails, the
object is still printed, with what was done, and the command exits non-zero
with the reason on stderr. When the CLI was replaced, the object comes from
the new binary.

The output ends with one line per step: `updated`, `already latest`, `ok`,
`nothing to do`, `skipped` or `failed`, with the reason. The doctor line
instead says what doctor found, such as `2 warning(s)` or `machine far
unreachable`, and the sync line lists each unreachable machine as `far
skipped (unreachable)`.

The exit code is non-zero only when one of update's own steps failed: the
CLI update, the packages pin, the skills, the sync check, or a sync you
applied. What doctor finds never fails `update`; run `devmachine doctor`
when a script needs that exit code. Saying no to the sync is not a failure.

Without `--cli-only` or `--no-machines`, `update` prints for a person, so it refuses
`--format json`; use `doctor` and `sync --check` with `--format json`
instead. Why it works this way:
[Updating](../how-it-works/updating.md).

### The hint that a newer packages release is out

After `sync`, `doctor`, `machines list` and `workspaces list`, a line on
stderr says when a packages release newer than your pin is out:

```
packages v33 is out (you pin v32): run devmachine update
```

It appears at most once a day, and again on the first run of a new CLI
version. It never appears with `--format json`, when stdout or stderr is not
a terminal, or when nothing is pinned. It reads the same daily answer as
`packages outdated`, and asks GitHub at most once a day itself.
`DEVMACHINE_NO_UPDATE_HINT=1` turns it off.

## version, help

```
devmachine version
devmachine help [command] [--json]
```

## The command log

`run`, `upload`, `download` and `sync` each append one line to `<config>/history.log`, mode
`0600`:

```
2026-09-18T12:00:00Z  workspace acme   ok      "docker ps"
2026-09-18T12:01:00Z  machine main      failed  "sync --tags caddy"
```

UTC time, target, success or failure, then the quoted command. See
[configuration](../concepts/configuration.md#the-command-log).

**`setup`, `machines add` and `login` are not logged** — the first two
handle a root password, and `login` runs a command you watch yourself.
