# Packages

A package adds something to your server or to a workspace: Claude Code,
Docker, your GitHub login.

Add one with `devmachine packages add <name>`, then run `devmachine sync`
to install it. `devmachine packages list` shows what exists. The
published packages live at `github.com/mydevmachine/packages`, and you can
write your own.

## Which packages exist

```
devmachine packages list
```

lists every package your configuration can use, with a short description
of what each one does. `devmachine packages help <name>` prints what a
package accepts, straight from the package itself.

## Server packages and workspace packages

A **server package** installs once and serves everyone on the machine —
Docker, the firewall, Caddy. A **workspace package** belongs to one person
— Claude Code, `zsh`, a dotfiles setup — and can go on as many workspaces
as you want.

```
devmachine packages add docker --machine main
devmachine packages add claude-code --workspace acme
```

Adding the wrong kind to the wrong target is refused, and says why.

## The essentials

`setup` gives a new server one package, `essentials`, which installs nothing
itself and pulls in others: `base`, `git`, `firewall`, `ssh_hardening`,
`caddy` and `devmachine-app`. `--no-essentials` starts the server bare. A
Mac starts with `base`, `devmachine-app` and its package manager package
instead, since `essentials` runs only on Linux. See
[what a new machine starts with](../how-it-works/what-a-new-machine-starts-with.md).

## Changing a package's settings

A package reads its own settings, each with a default. Change one on a
workspace with:

```
devmachine workspaces edit acme --set caddy.email=you@example.com
```

An empty value, `--set caddy.email=`, removes the override. On a machine,
edit `settings:` directly in `config.yml` — see
[Configuration](configuration.md#settings).

## What a package is made of

A package is a folder: an Ansible role, the part that installs things, plus
one file, `package.yml`, that tells devmachine what the package is. The
folder's name is the package's name.

```
sharing/
  package.yml          what the package is
  tasks/main.yml       what it installs
  defaults/main.yml    the default value of each setting
  files/               anything it copies to the machine
```

This is a complete `package.yml`, using the fields most packages need:

```yaml
format: 1                  # the shape of this file
name: sharing              # same as the folder
scope: machine             # machine or workspace
summary: File sharing, uploads by API key and downloads by public link.
category: Web              # groups it on the packages page
requires:
  cli: ">= 0.7.0"          # the oldest CLI that can run it
needs: [docker, caddy]     # installed before this one
extends:
  caddy.sites.d: files/sharing.caddy   # a site file handed to Caddy
variables:
  port:
    summary: The port the container listens on.
    default: 53842
credentials:
  - name: sharing_api_key  # something only you can give it
    kind: secret
    scope: machine
    env: SHARING_API_KEY
```

Only the first four fields are required. The official schema, straight
from the CLI:

| Field | Required | What it is |
| --- | --- | --- |
| `format` | yes | The shape of this file. This CLI reads format 1. |
| `name` | yes | The package's name, which has to be the directory it lives in. |
| `scope` | yes | Where it is installed: "machine" or "workspace". |
| `summary` | yes | One line saying what it installs. It is what `packages list` prints. |
| `category` |  | A word or two grouping it with packages like it, such as "Security" or "DNS". The packages page filters by it. |
| `platforms` |  | The operating systems it runs on: "linux", "macos", or both. Left out, any. |
| `requires` |  | Which CLI can run it, written as requires.cli: ">= 0.2.0". |
| `needs` |  | Packages that have to run before this one. It is the only thing that decides order. |
| `provides` |  | Places other packages may write into, as <place>: <absolute path on the machine>. |
| `extends` |  | Contributions to another package's place, as <package>.<place>: <path inside this package>. |
| `variables` |  | Values this package reads, each with a summary and a default. |
| `credentials` |  | What its tool cannot work without, and how each one is obtained. |
| `requires_files` |  | Files that have to be on the machine before it runs. |
| `skills` |  | A package-relative directory whose direct children are Agent Skills. |
| `kind` |  | The contract an entrypoint answers. The only one so far is "dns". |
| `entrypoint` |  | An executable in the package the CLI can call on the machine. |
| `commands` |  | What the entrypoint accepts: a list, or ["*"] for anything. |

The CLI is the source of truth for this list: `devmachine packages schema
--json` prints it, and `devmachine packages validate <folder>` checks a
package against it, reporting every problem at once. Each field is
explained in full, with its rules, in
[the package format](../reference/package-format.md).

## Writing your own

`<config>/packages/` holds packages you write yourself, in the same
format as the published ones, and a package there with the same name
replaces an official one.

Start one with `devmachine packages new <name>`: it writes a package that
already passes `packages validate`. Browse the official ones on the
[packages page](https://mydevmachine.sh/packages/) for examples, and read
[why packages work this way](../how-it-works/why-nothing-is-embedded.md)
for the reasoning behind it.
