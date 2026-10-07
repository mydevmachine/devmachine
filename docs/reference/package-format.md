# The package format

A package is an Ansible role plus one extra file, `package.yml`. Nothing
is translated on the way to the machine: what you write is what runs, so
a failure points at the exact line you wrote.

This page and the validator agree. When they disagree, trust the
validator — ask it with `devmachine packages schema --json`.

## The layout

```
<name>/
  package.yml
  tasks/main.yml
  defaults/main.yml
  handlers/, files/, templates/, vars/   (optional, as in any role)
```

The directory name **is** the package name. `devmachine packages new
<name>` writes a starting skeleton that already passes `devmachine
packages validate`.

## The fields

### `format` (required)

The shape of the file. This CLI reads format `1`. A validator that meets
a format it cannot read says so and stops, instead of misreading fields
it does not understand.

### `name` (required)

Lower case letters, digits, dashes and underscores, matching the
directory name — a package is found by its directory.

### `scope` (required)

`machine` or `workspace`, nothing else. A fact about the software, not a
preference: Docker installs once and serves everyone, so it is
`machine`; a tool with a login per person is `workspace`, running once
per workspace that asks for it.

### `summary` (required)

One line saying what the package installs. `packages list` prints it.

### `category`

A word or two grouping the package with others like it, such as
`Security`, `Coding agents` or `DNS`. The
[packages page](https://mydevmachine.sh/packages/) filters by it. The CLI
does not read it, so any value is accepted, and a package without one is
listed as `Other`.

### `platforms`

The operating systems the package runs on, as a list of `linux` and
`macos`:

```yaml
platforms: [macos]
```

Left out, the package runs anywhere. A machine runs Linux or macOS, and
macOS can also be your own computer, added as a [self machine](../how-it-works/your-computer-as-a-machine.md).
So a package written for Homebrew says `[macos]`, and an app stops
offering it for a Linux server — `packages list --format json` reports it
as `platforms`. The same holds for a `workspace` package: a workspace
lives on whatever its machine runs, so `[linux]`, `[macos]` and
`[linux, macos]` are all accepted.

How to write one package for several systems, and the traps on each, is in
[one package on many systems](../how-it-works/packages-on-many-systems.md).

### `requires.cli`

Which version of the CLI can run this package: `">= 0.2.0"`, `"> 0.2.0"`
or `"= 0.2.0"`. Different from `format`: `format` says whether the CLI can
*read* the file, `requires.cli` says whether it can *run* what it
describes — the binary and the packages release on their own schedules,
so the two can disagree. A CLI built from source calls itself `dev`, and
every constraint allows it.

### `needs`

Packages that must run before this one:

```yaml
needs: [base, firewall]
```

The only thing that decides run order — the order packages are listed in
your own configuration means nothing. A circular dependency is refused,
naming the packages involved.

### `provides`

Places other packages may write into, as a name and an absolute path on
the machine:

```yaml
provides:
  sites.d: /etc/caddy/sites.d
```

### `extends`

Adds a file to a place another package opened:

```yaml
extends:
  caddy.sites.d: files/sharing.caddy
```

The key is `<package>.<place>`, and the value is a path inside this
package. It can only add a file there, never change what is already
there or reach anywhere else. Extending a place nobody provides is
refused while devmachine plans, before anything runs. The file lands as
`<extending package>-<basename>`, so two packages adding a same-named
file never collide.

### `variables`

Values the package reads, each with a summary and a default:

```yaml
variables:
  port:
    summary: The port the container listens on.
    default: 53842
```

The package's Ansible role reads `devmachine_<package>_<name>`, so a
package called `tunnel` that declares `port` uses
`devmachine_tunnel_port` — the package name is part of it since Ansible
has one shared namespace, and two packages might both want a `port`.

That is the same name used for a target's
[settings](../concepts/configuration.md#settings): a setting is just a
default someone overrode, and the package does not know or care where
the value came from.

A dash works in a package name but never in a variable name, so `-`
becomes `_`, as does the `.` a package name may contain. Names that
collide this way are refused, rather than one silently winning.

#### Types

A variable may also say what shape its value has, with `type`: one of
`string`, `boolean`, `number`, `list` or `map`. A list whose entries are
mappings describes them with `items.fields`:

```yaml
variables:
  repos:
    summary: Repositories cloned into ~/dev/<name>.
    type: list
    default: []
    items:
      fields:
        name: {summary: The folder under ~/dev., required: true}
        url: {summary: Where it is cloned from., required: true}
        branch: {summary: The branch to check out.}
```

A field is a `string` unless it says `type: boolean` or `type: number`.
One level of structure is all a field gets: a list of things needs it, and
anything deeper would be a second configuration language.

The type is checked on your computer, never on the machine. `workspaces
edit` and `machines edit` refuse a `--set` that does not fit, `sync`
refuses a setting that does not fit before it reaches the machine, and
`packages validate` refuses a default that does not fit its own type. That
is the whole reason for a type: without one, a mistyped field such as
`nmae:` is carried to the machine and fails in Ansible halfway through a
sync, or worse, is quietly ignored by the recipe.

```console
$ devmachine workspaces edit alice --set 'workspace.repos=[{name: app}]'
error: workspace.repos[0]: "url" is required
```

A variable without a `type` takes any value, as every variable did before
types existed. Older CLIs ignore `type` and `items`, so a package that relies
on the check should also raise `requires.cli` to the release that brought it.

### `credentials`

What the package's tool needs to authenticate, **and how to get it** —
how belongs here because the package is the only thing that knows. Each
entry has a `name`, a `kind`, and a `scope` (`machine` or `workspace`),
plus what its kind needs:

| `kind` | also needs | what it means |
| --- | --- | --- |
| `manual` | `command`, `stored_at` | A person runs `command`; the tool leaves its session at `stored_at`. |
| `secret` | `env` or `path` | A value handed over once, delivered there. |
| `file` | `path` | A file placed on the machine at that path. |

```yaml
credentials:
  - name: claude
    kind: manual
    scope: workspace
    command: claude /login
    stored_at: ~/.claude/.credentials.json
```

`stored_at` is a claim, not a guarantee — it is what lets `doctor` check
whether the login worked.

A `manual` credential can also say `shareable: true`: a copy of
`stored_at` works on another account, the way one GitHub login can serve
every workspace. This is a fact about the tool, found by testing it — a
session file copies fine, a token tied to one device or browser does
not. Leave it out and it defaults to `false`. A credential recommending
`scope: machine` must say `shareable: true`, since `scope: machine`
means "one login, copied into every workspace" — recommending both
without it asks for something the package itself says cannot work.
A login that is shared needs a `stored_at` that starts with `~/`: each
copy lands in a workspace's own home, and `sync` refuses any other path.
See [Sharing a login](../how-it-works/sharing-a-login.md).

Only `manual` can be `shareable`. A `secret` or `file` is delivered
fresh to each place that needs it, never copied, so `shareable` on
either is refused. `scope` here is only a recommendation — whether a
shareable credential is actually shared is the operator's own choice,
per workspace — see [Configuration](../concepts/configuration.md).

### `requires_files`

Files that must already be on the machine before the package runs.

### `skills.path`

A package can ship complete Agent Skill directories:

```yaml
skills:
  path: skills
```

The path is relative to the package root, and cannot contain `..`, be
absolute, or escape through a symlink. Each direct child must be a
lower-case, dash-separated skill directory with a `SKILL.md`, whose
frontmatter `name` matches the directory and `description` is not empty.

This does not replace the Ansible role — a package with skills still has
`tasks/main.yml`, and may also have defaults, handlers, files and
templates.

### `widgets`

A package can ship widgets for the app:

```yaml
requires: {cli: ">= 0.9.0"}
widgets: widgets
```

The path is relative to the package root and cannot leave it. Each folder
directly inside it that holds a `widget.yml` is one widget, named
`<package>/<folder>`. See [the widget format](widget-format.md).

A CLI before 0.9.0 ignores `widgets:`, so a package that uses it needs a
`requires.cli` that refuses those CLIs, such as `">= 0.9.0"`. A package
with only widgets is still an Ansible role: its `tasks/main.yml` can be an
empty list.

### `kind`, `entrypoint`, `commands`

A package can ship an executable the CLI calls on the machine:

```yaml
kind: dns
entrypoint: bin/provider
commands: [zones, list, upsert, delete, help]
```

- `entrypoint` is a path inside the package. It must exist, be
  executable, and start with `#!/usr/bin/env python3` — Ansible already
  needs Python on any machine this CLI sets up.
- `commands` lists what it accepts: names, or `["*"]` for anything.
  Mixing `"*"` with named commands is refused.
- `kind` is a contract. The only one so far is `dns`, which must accept
  `zones`, `list`, `upsert`, `delete` and `help`.

`devmachine run --package <name> -- <command>` calls the entrypoint on a
machine, and refuses a command that is not in `commands`. A widget reads
the commands listed under `providers`.

### `providers`

A package can let widgets read some of its commands. Each one prints one
JSON object:

```yaml
entrypoint: bin/devmachine-app
commands: [context, stats]
providers:
  stats:
    returns:
      disk: object
      load: object
      errors: list
      note: string?
    min_every: 10s
```

- Each key is a command, and must be one of `commands` (any name, when
  `commands` is `["*"]`). A widget names it `<package>/<command>`, here
  `devmachine-app/stats`.
- `returns` lists the fields of the JSON object: `string`, `number`,
  `bool`, `list` or `object`, with `?` after the type when the field may be
  missing. It is required. Written on one line between `{` and `}`, a type
  with `?` needs quotes: `{note: "string?"}`.
- `min_every` is how often a widget may run the command at most, at least
  `5s`. It is required.
- A package with `providers` needs an `entrypoint`.

The app runs the command through `devmachine run --package <package>
--machine <m> --no-log -- <command> --<key> <value> …`, one `--<key>
<value>` pair per key of the widget's `with`, keys sorted. A value that
starts with `-` still arrives as the value after its `--<key>`; the command
reads its own arguments. See [the widget format](widget-format.md#package-providers).

### `network`

A machine package can answer for a private network's host entries:

```yaml
network:
  prefix: tailscale        # hosts entries written tailscale:<name> belong here
  resolve: bin/resolve     # runs on your computer: name in, IP addresses out
  join: bin/join           # runs on the machine, for `devmachine login <package>`
  self_name: bin/self-name # runs on the machine: prints its name on the network
```

- `prefix` is lower case letters, digits and dashes, starting with a
  letter. It is what a `hosts` entry is written with: `<prefix>:<name>`.
- `resolve` is required. `join` and `self_name` come together or not at
  all: a login that joins has to learn the name it joined under.
- Each script is a path inside the package, executable, and starts with
  `#!/usr/bin/env python3`, like an entrypoint.
- Only a `scope: machine` package can declare it: a network joins the
  machine, not one account on it.

What each script receives and must print is in
[the network package contract](network-package-contract.md).

### `bootstrap`

A machine package can carry the script that prepares a machine for
Ansible, where the CLI has no package manager it can drive by itself — a
Mac, through `mac-brew` or `mac-ports`:

```yaml
name: mac-brew
platforms: [macos]
requires:
  cli: ">= 0.8.0"
bootstrap: bin/bootstrap
```

- It is a path inside the package, and executable.
- It is POSIX `sh`, not Python: it runs before Ansible, and before the
  Python that Ansible brings. So it is not held to the Python shebang an
  entrypoint is.
- Only a `scope: machine` package can declare it: it prepares the
  machine, not one account on it.
- Set `requires.cli` to the first CLI release that runs it. An older CLI
  ignores a field it does not know, so `requires.cli` is what stops it
  from syncing that machine half-way.

## The rules, and what each one says

| Rule | The message |
| --- | --- |
| `format` missing | ``every package needs `format`, and this CLI reads 1`` |
| `format` unreadable | `format 99, and this CLI reads 1. Upgrade with brew upgrade devmachine` |
| `name` missing | ``every package needs a `name` `` |
| `name` malformed | `name "X": use lower case letters, digits, dashes and underscores` |
| `name` is not the directory | `name is "X" but the directory is "Y": a package is found by its directory` |
| `scope` unknown | `scope must be "machine" or "workspace", got "X"` |
| `summary` missing | ``every package needs a one-line `summary` `` |
| `requires.cli` unreadable | `requires.cli "X": write it as ">= 0.2.0", "> 0.2.0" or "= 0.2.0"` |
| `platforms` has an unknown value | `platform "X": the platforms are "linux" and "macos"` |
| `platforms` lists one twice | `platform "X" is listed twice` |
| `extends` key has no dot | `an extension point is written <package>.<place>` |
| `extends` file is not there | `extends "X" points at Y, which is not in the package` |
| `provides` path is relative | `an extension point is an absolute path on the machine` |
| no `tasks/main.yml` | `a package is an Ansible role, so it needs tasks/main.yml` |
| the `apt` module is used | ``the apt module is not allowed; use `package` so this works beyond Debian`` |
| a credential says too little | one line per credential, naming it and what it is missing |
| a `machine` login is not `shareable` | ``credential "X" recommends `scope: machine`, so it needs `shareable: true` `` |
| a `secret` or a `file` is `shareable` | ``credential "X" is a secret, so it cannot be `shareable` `` |
| an entrypoint is not executable | `entrypoint "X" is not executable: chmod +x it` |
| an entrypoint is not Python 3 | `entrypoint "X" starts with "Y": it is Python 3 and starts with #!/usr/bin/env python3` |
| `network` on a workspace package | ``network` belongs to a machine package`` |
| `network.prefix` missing or malformed | `network.prefix "X": use lower case letters, digits and dashes, starting with a letter` |
| `network.resolve` missing | `network.resolve is required` |
| `join` without `self_name`, or the reverse | `network.self_name is required beside network.join` |
| a network script outside the package | `network.resolve "X" must stay inside the package` |
| a network script missing, not executable or not Python 3 | the same messages as an entrypoint, naming the field |
| `bootstrap` on a workspace package | ``bootstrap` belongs to a machine package`` |
| `bootstrap` outside the package | `bootstrap "X" must stay inside the package` |
| `bootstrap` missing or not executable | `bootstrap "X" is not in the package`, `bootstrap "X" is not executable: chmod +x it` |
| `kind` or `commands` with no entrypoint | ``kind` and `commands` describe an `entrypoint`, and this package declares none`` |
| `widgets` with a `requires.cli` an older CLI meets | `a package with widgets needs requires.cli above 0.8.1, the last CLI that ignores them` |
| `widgets` outside the package | `widgets "X" must stay inside the package` |
| `widgets` folder missing or empty | `the widgets folder is not there`, `the widgets folder holds no folder with a widget.yml` |
| `providers` without an `entrypoint` | `` `providers` are commands of an `entrypoint`, and this package declares none `` |
| a provider that is not a command | `provider stats is not one of commands: add it to commands, or remove the provider` |
| a `returns` type it does not know | `provider stats returns.errors is "array": the types are string, number, bool, list, object, each with ? when optional` |
| `returns` missing or empty | `provider stats says what it returns: returns: {<field>: <type>}` |
| `min_every` missing, unreadable or below 5s | `provider stats min_every 2s is below the 5s floor` |
| a widget is wrong | the widget's own message, at `widgets/<name>/widget.yml` and its line; see [the widget format](widget-format.md#the-rules-and-what-each-one-says) |

`devmachine packages validate` reports every problem at once, not just
the first.

## Why `apt` is refused

A package that calls `apt` only works on Debian. `package:` picks the
machine's own package manager instead, turning a rule people have to
remember into an error the validator catches.
