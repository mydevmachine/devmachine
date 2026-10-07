# Where a public widget comes from, and why it asks

Anybody can publish a package with widgets in a git repository, and you
can install it with one command:

```
devmachine packages install https://example.com/alice/tools.git@v1
```

This page says what that brings onto your computer and your machines,
and why some of its widgets ask before they run.

## Three kinds of package

Every widget comes from a package, and the package says how far its
widgets are trusted (`trust` in `devmachine widgets list --format json`):

- **official** — the pinned packages release. You chose it with `packages
  pin`, and every change to it is reviewed before a release.
- **local** — a package you wrote in `<config>/packages/`.
- **third-party** — a package installed from a git address. Its folder
  has a `.devmachine-source.yml` saying where it came from and which
  commit.

## What waits for you

A third-party widget that runs something — a `command`, a `prompt`, a
`session`, or a package provider — shows **Allow** and **Deny** in the app
before it runs, the same as [a widget an agent wrote in a
board](why-an-agent-written-widget-waits.md). A widget that only reads the
app's own data or a web address does not ask.

What you approve covers the widget's source, filled in, and the package's
commit. `packages update` that moves to a new commit asks again, because
the code behind the widget changed.

Official and local widgets do not ask: you chose that release, or you
wrote the package.

## What installing does, and what it does not

`packages install` fetches one commit, checks it, and shows you what it
brings before writing anything: its widgets and which run code, the
commands of its entrypoint, its providers, every executable file, every
task file, every other file of the role that decides what those tasks do
(`meta/`, `library/`, `templates/`, `vars/` and the like) and the
credentials it asks for. It refuses:

- a name the pinned release has, because a package of yours always wins
  over the release's and would replace it on every machine;
- a name you already have in `<config>/packages/`, your own or one
  installed before (`packages update` is how a package from a git
  address changes);
- a file name or a line of what it brings holding a character that moves
  or hides text in a terminal, such as ESC or a Unicode bidirectional
  override: such a name could erase the lines above the prompt, the
  "runs as root" warning included, and you would answer a question you
  cannot read. A file name that is not UTF-8 is refused for the same
  reason. The prompt also prints any such character as an escape, in
  case one gets past, and so does every error that quotes the
  repository: git's output, a problem in its `package.yml`, a link's
  name;
- a link that leads outside the package, which would copy a file from your
  computer to your machines;
- a package that does not validate.

Installing touches no machine. **Adding it to a machine is the step that
matters**: `packages add <name> --machine <m>` and `sync` run its Ansible
tasks there as root, like every package's. The widget approval does not
cover that. Read its tasks before you add it; `install` lists them.

## When the release gains the same name later

`install` checks the name against the release pinned at that moment. A
later `packages pin` can bring a release that has a package of the same
name. Your copy would then win on every sync and replace the official
one without a word, so `sync` refuses instead, before it connects. It
does not pick one for you: the two packages share a name, not an
author. Take the third-party one off (`packages rm`, then `packages
remove`), or pin an earlier release.

## Why there is no signature

Nothing checks who wrote a third-party package. A signature would say who
published it, not whether its code is safe, and you would still have to
decide. So the CLI makes the decision visible instead: it shows what the
package brings and asks, and its widgets that run code ask again in the
app. A list of trusted publishers may come later.

## Why a package provider runs through the CLI

A widget that reads a package's own command, such as
`devmachine-app/stats`, runs `devmachine run --package <package>
--machine <m> --no-log -- <command> --<key> <value>`. The CLI already
knows the machine's address, account and key, and refuses any command the
package does not list in `commands`. The app never builds a shell line:
each value of the widget's `with` arrives as one word.
