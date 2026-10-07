# Widgets

The app's Home is a canvas of widgets: a clock, a summary of your sessions,
your machines, the usage of each coding harness. You move them, resize
them, minimize them, and add or remove them. Where each one sits is kept in
a plain file, so the app, the CLI and your coding agent can all change it.

## What a widget is

A widget is a small file, `widget.yml`, that says two things: where the
data comes from, and how the app draws it. It holds no code. The app does
the work; the file only picks from what the app offers.

```
devmachine widgets list
```

lists every widget you can use. `devmachine widgets help claude-code/usage`
says what one widget takes.

## Widgets come in packages

Every widget ships inside a [package](packages.md), next to the recipe
that installs the tool behind it. `claude-code/usage` is the `usage`
widget of the `claude-code` package. The app's own widgets, such as the
clock, are in the `devmachine-app` package.

A widget that only shows what the app already knows — the clock, your
machines, harness usage — works without adding its package anywhere. A
widget that reads something a package installs needs that package added
to a machine and synced first; `widgets list` says so, and names the
command. See [why widgets come from packages](../how-it-works/widgets-come-from-packages.md).

From engine 1.1 a widget can also show the output of a command, a web
address, an answer from a coding harness, or a live copy of a session —
on your computer, a machine or a workspace. A command, prompt or session
widget runs what its package installs, so it needs the package added and
synced; a web address needs nothing. See [the widget
format](../reference/widget-format.md#sources).

Your own packages can ship widgets too. Write a `widgets:` folder in the
package (see [the widget format](../reference/widget-format.md)), then
check it:

```
devmachine widgets validate ~/.config/devmachine/packages/my-package
```

A widget in your own package replaces the release's package of the same
name, the same way your packages always do.

A package can also feed its widgets with one of its own commands — a
package provider, such as `devmachine-app/stats` — run on a machine
through the CLI. And anybody can publish a package with widgets:
`devmachine packages install <git-address>` brings it in, and its widgets
that run code ask before they run. See [where a public widget comes
from](../how-it-works/where-a-public-widget-comes-from.md).

## Areas and boards

An area is a place in the app that holds widgets. There are five:

- **Home**, a free canvas: each widget has a place and a size.
- **The sidebar**, on the left: a list, top to bottom. The workspace list
  is one widget there, so you can put others above or below it.
- **The context sidebar**, the Context tab next to a session: a list too.
  Each of its sections (shortcuts, publish a port, monitors, shells,
  sub-agents, to-do, pull requests, links) is one widget.
- **The menu bar title**, at the top of the screen: up to three short
  widgets in a row, "❯_" and the number of open pull requests unless you
  change it.
- **The menu bar popover**, what opens when you click it: each widget is
  a tab, Pull Requests and Usage unless you change it.

The sidebar's top bar and footer, and the context panel's other tabs
(Preview, Files, Git), stay as they are: they are not widgets. Neither
is the popover's footer (Open, Stats, Subdomains, the color picker, Quit).

Each area has a board in `<config>/boards/`: `home.yml`, `sidebar.yml`,
`context-sidebar.yml`, `menubar.yml` and `menubar-panel.yml`. Home's board says where each widget is and how
big:

```yaml
format: 1
surface: home
widgets:
  - id: clock
    type: devmachine-app/clock
    frame: {x: 24, y: 24, w: 320, h: 160}
    size: medium
    minimized: false
    z: 1
```

A sidebar's board is just the list, in order:

```yaml
format: 1
surface: sidebar
widgets:
  - id: workspaces
    type: devmachine-app/workspaces
    size: auto
```

When a sidebar or menu bar board is missing, the app writes the board
that draws that area as it always looked, and the CLI reads a missing one the same way. Settings →
Appearance can reset each one.

The context sidebar hands its widgets the selected session's context:
`machine`, `session`, `workspace`, `path` (the session's folder), `repo`
and `branch` (from Git), and `harness`. Switching sessions runs each
widget again with the new values. A widget written in place that uses
them, such as a command reading `{{context.path}}`, asks you to allow it
again for each new set of values.

The menu bar title runs its widgets whenever the app runs, at most every
30 seconds; the popover runs its own only while it is open. See [what
runs in the menu bar](../how-it-works/what-runs-in-the-menu-bar.md).

Edit a board by hand, from the app, or with the CLI:

```
devmachine widgets add claude-code/usage --set harness=codex
devmachine widgets add claude-code/usage --board context-sidebar --after todo
devmachine widgets move usage --before shortcuts --board context-sidebar
devmachine widgets move usage-panel --before pull-requests-panel --board menubar-panel
devmachine widgets remove usage
```

The app sees a change to the file within a second. A board with a mistake
is never written over: the app keeps the last good layout and says which
line is wrong, and the CLI refuses to change it until it is fixed.

A board can also hold a widget written in place, with a title, a source
and a view and no package — a quick `df` on a workspace, say. One that
runs something waits until you press Allow. See [the widget
format](../reference/widget-format.md#a-widget-written-in-the-board).
