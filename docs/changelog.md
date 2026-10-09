# Changelog

What changed in each release of the CLI and the macOS app, newest first.
App releases are headed `App vX.Y.Z`. Only changes you can see are here;
tests, CI and internal work are left out. The full list of commits for each
CLI version is on the [GitHub releases
page](https://github.com/mydevmachine/devmachine/releases).

To get the latest version, see [Upgrade](upgrade.md).

## App v0.5.0 — 2026-10-09

Remote session indicators need CLI v0.13.0. With an older CLI, sessions
are listed as before, without them.

### Added

- Each session in the sidebar shows its git branch (or folder) under its
  name and how long it has run. A remote session shows a spinner while
  its agent works, and an orange dot when the agent rings the terminal
  bell to ask for you. Settings → Sidebar turns each one on or off. See
  [session indicators](how-it-works/session-indicators.md).

### Changed

- Every workspace's session list refreshes every 5 seconds through one
  `devmachine sessions` call, not only the selected workspace.

## v0.13.0 — 2026-10-09

### Added

- `devmachine sessions` lists every workspace's tmux sessions with their
  folder, git branch, age, coding agents, and whether each is busy or
  asks for attention. All workspaces are asked at once; one that cannot
  answer carries its own error and the others still answer. See
  [sessions](reference/commands.md#sessions).

## App v0.4.0 — 2026-10-09

Shows download progress with CLI v0.12.0. With an older CLI, downloads
work as before, without the progress bar.

### Added

- Downloading from the Files tab, or dragging a file to Finder, shows a
  card with the download's progress until the "Saved to" or error notice
  takes its place. A folder has no size until its archive ends, so its
  card says how much has arrived instead.

## v0.12.0 — 2026-10-09

### Added

- `download --progress` writes the bytes received so far to stderr, one
  JSON line at a time: `{"remote": "...", "done": N, "total": N}`. Every
  path is sized before the first byte moves, so the first lines carry
  each total. A folder's `total` is 0. See
  [download](reference/commands.md#download).

## App v0.3.0 — 2026-10-09

Needs CLI v0.11.0 and packages v49. With an older CLI or packages, a banner
in the corner says what to update.

### Added

- Share a session. Right-click a workspace session and choose Share…: pick a
  name, how long (15 minutes to 8 hours) and whether guests only watch or
  can type. The app publishes the workspace's share server at
  `share-<workspace>.<your domain>` the first time, so the link works for
  anyone; the password shows once, beside it. Needs the `session-share`
  package on the workspace. See [Share a session](guides/share-a-session.md).
- A shared session has a mark beside its name in the sidebar, orange when
  guests can type. Its menu has Open chat, Copy share link, Add 30 minutes
  and Stop sharing.
- Chat with the people watching. A Chat tab in the context panel shows what
  guests write on the shared page, and you answer there; new messages show
  as a count on the session's mark.
- Settings → Shares lists every session this Mac shared, with Stop and Copy
  link for the ones still open.
- ⌘K: "Share <session>…".
- Widgets can be as tall as what they show (engine 1.7): on Home, a widget
  set to auto height follows its content.

### Fixed

- Home widgets keep their place, in proportion, when the sidebar hides.
- A sidebar row that fits its height no longer widens the panel on every
  pass and hangs the app.
- A widget card in a sidebar keeps the inset of its title instead of
  touching the edges.
- Durations and list errors in a board read exactly as the CLI reads them.

## v0.11.0 — 2026-10-08

### Added

- Widget engine 1.7: a widget can be as tall as what it shows. On Home,
  `size: auto` keeps the frame to `{x, y, w}` and the height follows the
  content; a set height is a cap. `widgets add` gives a growing widget
  `size: auto` on Home. See [the widget format](reference/widget-format.md).
- A guide to [sharing a session](guides/share-a-session.md) with the
  `session-share` package: someone watches, or types into, one of your
  tmux sessions for a limited time, in a browser or over SSH.

## v0.10.1 — 2026-10-08

### Fixed

- A dry run (`sync --check`) that installs skills no longer fails with
  `getgrnam(): name not found` on machines with an older Ansible, such as
  the one Ubuntu 24.04 ships. A real sync was not affected.
- Only one sync runs on a machine at a time. A second one, from another
  terminal or the app's Prepare button, used to delete the first one's
  files halfway through. It now stops at once with "another sync is
  running on …". See [One sync per machine](how-it-works/one-sync-per-machine.md).
- `packages validate` warns when one of your packages reads
  `devmachine_account` without setting it first. Such a package can pick
  up another workspace's account on a machine with several workspaces.
  The warning says which task to add.

## App v0.2.0 — 2026-10-07

Needs CLI v0.10.0 and packages v41. With an older CLI or packages, a banner
in the corner says what to update.

### Added

- Widgets on Home, the sidebar, a session's context sidebar and the menu bar.
  Home is a canvas you drag and resize widgets on; the sidebars are stacks
  you reorder; the menu bar title shows one widget and the popover shows
  the rest as tabs. See [Widgets](concepts/widgets.md).
- A gallery lists the widgets you can add. "New widget" makes one from five
  templates: a command, a URL, a web page, a coding harness's answer to a
  prompt, or a session's screen.
- Each widget's menu has Edit…, to change its title, inputs and refresh
  time, and Choose…, to pick the machines, workspaces or harnesses it shows.
- A widget that runs code it did not ship with asks before it runs, and
  asks again when what it runs changes.
- The pull requests panel is a widget on Home and in both sidebars.
- A prompt widget can pick its harness's permission mode. A mode that runs
  without checks shows a red warning and runs only when you press refresh.
- Settings can reset Home, each sidebar and the menu bar to their default
  widgets.

### Changed

- The default Home shows what Home showed before, as widgets: the clock,
  the summary, every machine in a row and one usage card per harness, laid
  out for the window's size.
- Delete and Remove in the sidebar now ask with the same End session dialog
  as ⌘W.
- One workspace whose login is refused no longer pauses every workspace.
  Only that workspace stops, or its machine when the machine doesn't answer
  or several of its logins are refused. The sidebar says so under the
  workspace, with Try again.

### Fixed

- A deleted, renamed or new session shows in the sidebar at once, and a
  late list can no longer bring a deleted session back.
- Deleting a session that tmux had already closed no longer shows an error.

## v0.10.0 — 2026-10-07

### Added

- Widgets for the macOS app (engine 1.6). A board lists the widgets for one
  place: Home, the sidebar, a session's context sidebar, the menu bar title
  and the menu bar popover. See [Widgets](concepts/widgets.md) and [the
  widget format](reference/widget-format.md).
- `widgets list`, `schema`, `validate`, `add`, `move`, `remove` and `set`
  read and change boards. Each one answers in JSON with `--format json`.
  `widgets list --board` shows only the widgets that fit that place.
- A package can ship widgets in its `widgets` folder, and declare
  `providers`: commands its widgets may read on a machine or workspace.
- A widget can be written straight into a board, without a package. Its
  source is a command, a web address, a coding harness's answer to a
  prompt, or a session's screen.
- A widget input can be a choice of machines, workspaces or harnesses, one
  or many. A board fills it with a name or a list.
- A board entry can override its widget's `title` and `every`. `widgets set`
  changes them, and an empty value puts the widget's own back.
- A prompt widget takes `permission_mode`, with the names Claude and Codex
  use. A mode that runs without checks only runs when you press refresh
  (`every: manual`). See [A prompt widget's permission
  mode](how-it-works/a-prompt-widgets-permission-mode.md).
- `packages install <git-address>` fetches a package from git, shows what
  it brings and asks before it writes it. `packages update` fetches it
  again and says what changed; `packages remove` deletes it.
- `run --argv` runs a program with its words exactly as given, `run
  --package --script` runs one file of an installed package, and
  `--no-log` keeps a run out of the log.

### Changed

- `run` exits with the command's own exit code, 255 when it never ran, and
  stops when it gets SIGTERM.
- Over SSH, `run --argv` and `run --package` send the command on standard
  input to `/bin/sh`, so a fish login shell never reads it.

### Fixed

- `run` no longer adds its own error line under a command's own failure.
- When SSH tries a machine's addresses, it moves to the next one only when
  the first never connected.

## v0.9.0 — 2026-10-07

### Added

- A package variable can declare its `type`: `string`, `boolean`, `number`,
  `list` or `map`, and a list of mappings names its entries' fields with
  `items.fields`. See [Types](reference/package-format.md#types).
- `workspaces edit` and `machines edit` refuse a `--set` that does not fit
  its variable's type, and `sync` refuses such a setting before it reaches
  the machine. The error names the setting and the entry, such as
  `workspace.repos[0]: "url" is required`.
- `packages validate` refuses a default that does not fit its own type.
- A guide for an old Mac as a machine.

### Changed

- `packages list` reports a variable's declared type, and the settings
  reference names the fields a list's entries take.
- Getting started no longer assumes a VPS: a Linux box or a Mac you already
  have works too.

## App v0.1.22 — 2026-10-07

### Added

- Adding a machine asks what happens to SSH password login once the key
  works: off for every account, off except the accounts you pick, or left
  as it is.
- Settings → Machines → ⋯ → SSH password login shows which accounts can
  still log in with a password, and changes it.

### Changed

- A new workspace on a machine that has never synced shows one card that
  asks for the first sync, instead of two cards.
- The app calls a workspace an account on its machine, not a Linux account,
  and picks the packages that fit a machine by its real system.

## v0.8.2 — 2026-10-07

### Added

- `setup` and `machines add` ask before they turn SSH password login off,
  and can keep it for the accounts you name. `--keep-password-login a,b`
  answers without a terminal.
- `machines password-login <machine>` shows which accounts can log in with
  a password, and changes it later with `--off`, `--keep a,b` or `--on`.
  See [Password login](how-it-works/password-login.md).
- `machines list`, `machines show` and `config show` report each machine's
  known system as `platform` in JSON.
- With packages v39, `docker` (colima, one VM per workspace), `tailscale`,
  `claude-remote-control`, `caddy`, `firewall`, `ssh_hardening` and
  `hostinger` run on a Mac too.

### Fixed

- `workspaces new` leaves out a package the machine's system cannot run,
  and says so. `packages add` and `workspaces edit --add` refuse one before
  anything is written, instead of failing later in `sync`.
- `expose`, `login` and the Caddy reload in `sync` find tools that Homebrew
  or MacPorts installed on a Mac, and `login tailscale` finds the CLI inside
  the Tailscale app.

## v0.8.1 — 2026-10-06

### Added

- A distribution based on Debian, Ubuntu or Arch Linux is set up the way
  its base is: Linux Mint and Pop!_OS like Ubuntu, Manjaro, EndeavourOS and
  CachyOS like Arch. `setup` says it is not tested, and `doctor` shows it as
  a warning. Fedora, Rocky Linux, openSUSE and Alpine are still refused.
- The [Supported systems](supported-systems.md) page lists every system a
  machine can run and what works on each.

### Fixed

- Sharing a login into a workspace works on a Mac, which has no `runuser`.
- `stats` reads a Mac machine instead of failing.
- `setup` and `machines add --tailscale` no longer give a Mac the Linux-only
  `tailscale` package.
- The hint to remove an old site by hand reloads Caddy on a Mac too.

## v0.8.0 — 2026-10-06

### Added

- Machines can run Arch Linux or macOS, not only Debian and Ubuntu. `setup`
  and `machines add` read the system first and refuse one they do not know,
  before changing anything.
- On Arch, Ansible comes from `pacman` without a partial upgrade.
- On a Mac, the `mac-brew` or `mac-ports` package prepares the machine: the
  Command Line Tools, the package manager and Ansible. `--package-manager
  brew|ports` picks one when the Mac has neither or both, and
  `--install-prerequisites` agrees to install what is missing. `--yes` never
  installs them.
- A new Mac starts with `base` and `devmachine-app`; `essentials` is
  Linux-only.
- `machines show` prints a machine and what the CLI last observed about it:
  system, package manager, init system, where Ansible lives.
- `run` puts a Mac's package manager on `PATH`, and shows the remote
  command's errors on stderr.
- `doctor` checks a Mac's prerequisites, and warns when a Mac's Remote Login
  leaves a workspace account out; `devmachine sync` adds the account.
- `machines create-local --distro arch` makes an Arch Linux VM for testing.
- `--fingerprint` and `machines trust --expect` accept the fingerprint of any
  of the host's key types, not only the one the CLI negotiates.

### Changed

- After turning password login off, `setup` checks with `sshd -T` that it
  really is off, and takes its change back if sshd ignores it.
- `sync` refuses, before changing the machine, a package whose `platforms`
  leaves the machine's system out.
- A workspace package for macOS alone is now accepted.
- `machines rm` also forgets what the CLI observed about the machine.
- A package directory that is a symlink is followed when it is sent to the
  machine.

## App v0.1.21 — 2026-10-06

### Added

- With harness icons on, a session that runs no coding agent shows a
  terminal icon before its name, the same size as the agent icons.
- A session running [Herdr](https://herdr.dev/) shows the Herdr icon.

## App v0.1.20 — 2026-10-06

### Changed

- The Operator session asks which coding agent to start every time it
  starts, not only the first time. Your last pick is selected, so starting
  it again is one click. Only the first pick asks the agent what you can do
  here.

## v0.7.32 — 2026-10-06

### Fixed

- Two commands that download the same packages release at once no longer
  delete each other's copy. `update` could fail on its skills step with
  `package release v34 has no package named "devmachine-skills"` while the
  Mac app listed packages at the same moment.

## App v0.1.19 — 2026-10-05

### Added

- The Operator session, the first time you open it, asks which coding agent
  on your Mac to start in it, or a plain terminal.
- Settings → General → Enable Operator session hides the Operator without
  ending its shell.
- A newer packages release gets its own update card. Updating moves the
  packages pin and your skills, and never touches a machine.

### Fixed

- A packages update whose CLI reports nothing now shows as failed, not done.

## App v0.1.18 — 2026-10-05

### Added

- Drag the sidebar into your own order. A workspace's header moves the whole
  workspace, Local included; a session moves only inside its own workspace.
  The order survives a relaunch, and hiding a workspace forgets its place.

## v0.7.31 — 2026-10-05

### Added

- `update --no-machines` brings the CLI, the packages pin and the skills up
  to date and stops there: no doctor, no sync, no question. `--format json`
  reports each step.
- `packages outdated` says which packages release you pin and whether a
  newer one is out.
- A short hint, at most once a day and on the first run of a new CLI
  version, when a newer packages release is out: `run devmachine update`.
  `DEVMACHINE_NO_UPDATE_HINT=1` turns it off.

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
