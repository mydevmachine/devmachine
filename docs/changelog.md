# Changelog

What changed in each release of the CLI, newest first. Only changes you can
see are here; tests, CI and internal work are left out. The full list of
commits for each version is on the [GitHub releases
page](https://github.com/mydevmachine/devmachine/releases).

To get the latest version, see [Upgrade](upgrade.md).

## v0.7.30 — 2026-10-05

### Added

- Each machine has a location — where it is, such as `hostinger`, `home`
  or `local`. Set it with `--location` on `machines add`, `machines
  create-local` and `machines edit`; `machines add` asks for it, with
  `external` as the default. `machines list` shows a LOCATION column, and
  the JSON output reports it for every machine.

## v0.7.29 — 2026-10-03

### Added

- `--format json` prints workspace defaults, each package's variables and
  each workspace's login sharing.
- `packages list --format json` says whether each declared login can be
  shared from the machine.

## v0.7.28 — 2026-10-03

### Added

- Skills for the Antigravity, Kimi and Cline agents, linked where each agent
  looks for them, on your computer and in workspaces.
- `machines create-local` takes `--cpus`, `--memory` and `--disk`, checks
  them against this computer and reports them in its output.

### Fixed

- `run` names its SSH control sockets with a short hash, and falls back to
  `/tmp/dm-<uid>`, so a long home path fits the socket length limit.

## v0.7.27 — 2026-10-02

### Added

- `machines add` works without a terminal:
  - `--password-stdin` installs the key on a server that only takes a
    password.
  - `--key agent:<fingerprint>` uses a key the SSH agent holds.
  - `--address` with no `config.yml` writes the configuration `setup` would
    write.
- `machines scan` reports the SSH host key of an address you have not added,
  and writes nothing.
- `machines create-local --add` creates the VM and adds it in one step.
- `machines edit` sets and unsets a machine's package settings;
  `workspaces edit` takes `--unset`.
- `machines list --format json` reports each machine's packages.
- `packages list --format json` reports each package's kind, category,
  platforms, needs and declared credentials.
- A package manifest can declare the platforms it runs on.
- `expose rm` asks before it removes a DNS record. `--keep-dns`, on
  `expose rm` and `workspaces destroy`, leaves the record.

### Fixed

- `expose rm` removes the A record `expose add` made, and leaves a name that
  now points somewhere else alone. A record it could not remove shows in
  `dns_error`.
- `expose rm --no-apply` prints a `dns rm` that names the serving machine.
- `workspaces destroy` removes its sites' DNS records, like `expose rm`.
- `machines list`, `config show` and `packages list` answer when there is no
  `config.yml`, instead of failing.
- `machines add` writes the machine, and the host key, only after the key
  works. A failed run can simply run again.
- `machines add` with questions and no `config.yml` sends you to `setup`,
  which also asks for the domain.
- Every command that writes `config.yml` holds a file lock for the whole
  change, so two edits at once no longer lose one.
- Removing several sites asks each DNS provider and lists each zone once.

## v0.7.26 — 2026-10-02

### Fixed

- `credentials push` replaces a rotated value.
- `expose add` still publishes the site when the DNS provider refuses the
  record, and says the record was not written.
- Rotation touches only the env files the CLI owns, and never follows a link.

## v0.7.25 — 2026-10-02

### Added

- `expose add --via` publishes a workspace through another machine's Caddy.

### Fixed

- `expose` points the name at the machine's first public address, not at a
  private one listed first.
- `expose` refuses a removal that would leave a site served elsewhere without
  its route.

## v0.7.24 — 2026-10-01

### Added

- `machines add` adds a machine in one command, with no questions, given
  `--name`, `--address` and `--fingerprint`.
- `machines trust --check` reports a changed host key, and `--expect` pins the
  new one.

### Fixed

- System steps run through `sudo -n` when the admin login is not root.
- The key is installed when Tailscale SSH let the connection in.
- `sshd -t` works on a server whose socket-activated sshd never made
  `/run/sshd`.
- A failed remote command carries its stderr into the error, so a refusal
  says why.
- SSH aliases live in one file, recorded as `ssh_aliases_path`.
- `sync --tags` locks only the packages that ran.

## v0.7.23 — 2026-10-01

### Added

- `download` brings files and folders from a workspace to this computer. It
  never overwrites a file, and sends a folder as one `tar.gz`.
- `update --cli-only` replaces only the CLI binary.

### Fixed

- `upload` and `download` match a file name a Mac sends in decomposed form
  (NFD) to the name on the machine.

## v0.7.22 — 2026-10-01

### Added

- `doctor` without `--machine` checks every configured machine, one block
  each, and fails only when one fails.

### Fixed

- `doctor` skips the SSH aliases check when `ssh_aliases` is off.

## v0.7.21 — 2026-10-01

### Added

- `expose add` and `expose rm` apply the workspace's routes to Caddy at once,
  checked first, instead of waiting for the next `sync`.

## v0.7.20 — 2026-09-30

### Added

- Private networks are resolved, joined and aliased through the package that
  declares their prefix.

### Changed

- `doctor` warns about missing credentials, and fails only when a machine
  cannot be used.

## v0.7.19 — 2026-09-30

### Added

- `update` updates the CLI, the packages pin and the skills in one command,
  then checks your machines before any sync.

## v0.7.18 — 2026-09-30

### Added

- `upload` sends local files into a workspace's or a machine's home.
- A workspace's own app secrets, delivered as dotenv files.
- `doctor` checks SSH aliases by resolving them with `ssh -G`.

### Fixed

- Secrets and credentials refuse a path that leaves the workspace home
  through a symlink.
- A secret that moved to another env file leaves the old one on the next
  push.
- `login tailscale` prints the hosts block to paste when it cannot read the
  tailnet name.
- A command's input is never sent again, in part, to another address.

## v0.7.17 — 2026-09-29

### Added

- `setup` and `machines add` ask about SSH aliases and Tailscale.
- The CLI keeps SSH aliases up to date after workspace changes and `sync`.
- `login tailscale` adds the machine's tailnet name to `config.yml`.
- `doctor` warns when the SSH aliases block is missing or out of date.

## v0.7.16 — 2026-09-29

### Added

- `setup` writes an `AGENTS.md` into a new configuration.

## v0.7.15 — 2026-09-29

### Fixed

- `setup` remembers the agent key you chose, not every key the agent holds.

## v0.7.14 — 2026-09-29

### Added

- New machines start with the `essentials` package. `setup --no-essentials`
  starts bare.
- Every workspace gets the `workspace` package first, and it cannot be
  removed.
- Packages have a `category` field.

## v0.7.13 — 2026-09-29

### Fixed

- `sync` reloads Caddy when a site file changes.
- `run` says why the server refused the key, instead of trying the next
  address.

## v0.7.12 — 2026-09-28

### Fixed

- A local command that fails says why.

## v0.7.8 to v0.7.11 — 2026-09-28

No change to the CLI. These releases carry a plainer manual, the new GitHub
organisation `mydevmachine`, and the site at mydevmachine.sh.

## v0.7.7 — 2026-09-28

### Added

- Skills install right after `brew install`, before `setup`.

### Fixed

- `setup` pins the latest packages release.

## v0.7.6 — 2026-09-28

### Fixed

- `sync` removes what a package left behind when the package leaves the plan.

## v0.7.5 — 2026-09-28

### Changed

- `run` reuses one SSH connection across runs, so it is faster.

## v0.7.4 — 2026-09-28

### Fixed

- SSH aliases get the `-pub` fallback when the first address is literal.

## v0.7.3 — 2026-09-28

### Fixed

- Every `sync` starts from an empty bundle.

## v0.7.2 — 2026-09-28

### Fixed

- Answers go to stdout, where a program reads them.

## v0.7.1 — 2026-09-28

### Fixed

- This computer is never the machine a command picks on its own.
- A package needs no credential to be run.

## v0.7.0 — 2026-09-26

### Added

- `skills`: install and manage agent skills on this computer, and keep them
  in sync in workspaces.
- `workspaces edit` changes the default packages for future workspaces.
- `expose` keeps a workspace's published routes in the configuration, and
  `sync` writes them into Caddy.
- `expose list` reads the configuration and the machine.
- `workspaces destroy` deletes the account, its home and its configuration.
- A `self` machine is this computer, set up without SSH. `machines list`
  says which machine is this computer.
- `run` reaches a package's entrypoint as a workspace's account.

### Fixed

- `expose list` and `expose rm` work when Caddy is gone from the machine.
- `workspaces destroy` refuses when a route's file cannot be found.

## v0.6.3 — 2026-09-24

### Fixed

- `setup` resumes a machine it started to prepare.

## v0.6.2 — 2026-09-23

### Fixed

- A local recipe overrides a released package.

## v0.6.1 — 2026-09-23

### Added

- Host-key trust: the CLI checks the server's identity on every connection,
  interactive or not, from a trust store kept with your configuration.
- `setup` trusts the host before it logs in.
- `machines` can trust and rotate a host key on request.
- `doctor` reports SSH host trust before it connects.

### Fixed

- `setup --yes` never publishes anything; publishing without a terminal needs
  explicit consent.

## v0.6.0 — 2026-09-22

### Added

- Your configuration can be a git repository. `setup` writes the ignore file
  first, and every change is committed, but never a path that holds a
  secret.
- `secrets` lists what a machine needs, with no values.
- `expose add`, `expose list` and `expose rm` publish a site through Caddy,
  and point the hostname at the machine first.
- `tunnel` reaches a remote port privately over `ssh -L`.
- `machine` checks and sets up your own computer.

### Fixed

- A tunnel ends with the command.
- Whether a commit is signed follows your git configuration.

## v0.5.0 — 2026-09-22

### Added

- DNS: `dns providers`, `dns list`, `dns check`, `dns add` and `dns rm`. The
  CLI asks the installed providers which one holds the zone.
- `run --package` reaches a package, and asks it what it accepts.
- `packages` can generate a DNS provider that already meets the contract.
- `doctor` reports each DNS provider and the zones it holds.

### Fixed

- A package that reads a shared login picks it up in the same `sync`.
- `login` reaches a machine the CLI just made.

## v0.4.0 — 2026-09-21

### Added

- Workspaces, each with SSH aliases to reach it.
- Package variables, set per machine or workspace, or on the command line.
- `credentials`: list what a machine is missing and the command that fixes
  each one, then push values without them ever reaching a command argument.
- `login` runs a package's own login where it has to happen.
- Shared logins: one sign-in copied into several workspaces, with a way for
  a workspace to keep its own.
- `doctor` checks every declared credential.

## v0.3.0 — 2026-09-19

### Added

- `setup` finds out whether a key already works, falls back to a password,
  installs a key, proves it, then turns password login off.
- Keys: a dedicated key the CLI makes, or one the SSH agent holds.
- `setup` installs Ansible.
- `machines create-local` makes a machine on this computer.
- `machines add` adds a second machine; `machines rm` forgets one without
  touching it.

## v0.2.0 — 2026-09-19

### Added

- Packages: read and check `package.yml`, fetch a pinned release and verify
  its checksum, let a local recipe override a published one.
- `packages` lists what exists, and puts a package on a machine or a
  workspace.
- `sync` shows a plan, then applies the packages with Ansible on the
  machine.
- A history of every command that touched a machine.
- Writing the configuration back keeps your comments.

## v0.1.0 — 2026-09-18

First release.

- `setup` writes the configuration through a wizard.
- Machines and workspaces in the configuration; `machines list`.
- `doctor`, `stats`, `ssh`, `mosh` and `run`.
- Addresses with fallback, over SSH.
- Provider tokens in the OS keychain, with a file fallback.
- DNS provider interface, and a check of a name from outside.
- `help` prints the command surface as JSON.
