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

## Areas and boards

An area is a place in the app that holds widgets. Today there is one,
**Home**, a free canvas. A sidebar and a sidebar next to each session come
later; a widget can already say it fits them.

Each area has a board: `<config>/boards/home.yml`. It lists each widget
on the area, where it is and how big:

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

Edit it by hand, from the app, or with the CLI:

```
devmachine widgets add claude-code/usage --set harness=codex
devmachine widgets remove usage
```

The app sees a change to the file within a second. A board with a mistake
is never written over: the app keeps the last good layout and says which
line is wrong, and the CLI refuses to change it until it is fixed.
