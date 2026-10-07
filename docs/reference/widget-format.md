# The widget format

A widget is a folder in a package with one file in it, `widget.yml`. It
says where the data comes from and how the app draws it. There is no code
in a widget. See [Widgets](../concepts/widgets.md) for what a widget is,
and [why widgets come from packages](../how-it-works/widgets-come-from-packages.md).

## The layout

```
my-package/
  package.yml        widgets: widgets
  tasks/main.yml
  widgets/
    usage/
      widget.yml
```

`package.yml` names the folder with [`widgets:`](package-format.md#widgets).
Each folder directly inside it that holds a `widget.yml` is one widget. Its
full name is `<package>/<folder>`, for example `claude-code/usage`.

## `widget.yml`

```yaml
format: 1
name: usage
summary: Coding-harness usage windows.
requires: {engine: ">= 1.0"}
fits: [canvas, stack, slot]
context: {}
inputs:
  harness: {type: string, default: claude, summary: Which harness.}
source:
  kind: provider
  name: app/harness-usage
  with: {harness: "{{inputs.harness}}"}
  every: 60s
view: {kind: app.harness-usage}
sizes: [small, medium, wide]
default_size: medium
places: [home]
```

| Field | Required | What it is |
| --- | --- | --- |
| `format` | yes | The shape of this file. This CLI reads format 1. |
| `name` | yes | The folder's name: lower case letters, digits and dashes. |
| `summary` | yes | One line. It is what `widgets list` prints. |
| `requires.engine` | yes | The engine versions the widget works with, as `">= 1.0"`, `"> 1.0"` or `"= 1.0"`. |
| `fits` | yes | The layouts it can be drawn in: `canvas`, `stack`, `slot` (the menu bar), `tabs` (the menu bar popover). |
| `context` | no | The context keys it reads, each `required` or `optional`. A widget sees only the keys it declares. |
| `inputs` | no | Values a person sets on each copy: `type` (`string`, `number`, `boolean` or `choice`), `default`, `summary`. A choice also takes `from` and `many`: see [Choice inputs](#choice-inputs). |
| `source` | yes | Where the data comes from: `kind` and the fields of that kind. See [Sources](#sources). |
| `view` | yes | How it is drawn: `kind` and that view's fields. See [Views](#views). |
| `sizes` | yes | The presets it takes. |
| `default_size` | yes | The preset a new copy gets. One of `sizes`. |
| `places` | no | Surfaces the app adds it to once, the first time it is available. Each must be an area the widget fits. Removing it from there is final. |
| `single` | no | `true`: a board holds it at most once. The app's workspace list is one. |

A value in `source.with` can hold `{{inputs.<name>}}` or
`{{context.<key>}}`. The input or key it names has to be declared.

### Choice inputs

A `choice` input is picked from a list the app fills from live data, so
the app can offer a checklist or a picker on any widget without knowing
the widget:

```yaml
inputs:
  machines: {type: choice, from: machines, many: true, summary: Which machines; none shows all.}
```

- `from` says where the options come from: `machines` (the machines in
  `config.yml`), `workspaces` (the workspaces in `config.yml`) or
  `harnesses` (the coding harnesses that report usage: `claude`, `codex`).
- `many: true` makes the value a list of names; left out, or `[]`, it
  means all of them. Without `many` the value is one name; left out, it
  means the first option.
- A template sees a list joined with commas (`main,backup`), and so does
  `$DM_INPUT_<NAME>` in a shell line. An `app/…` provider gets the list
  itself.
- A widget with a choice input needs `requires.engine: ">= 1.5"`: an
  older app cannot show the choices.

## Sources

`source.kind` picks one of five kinds. A key that belongs to another kind
is a mistake, for example `source.url is not a field of a command source`.

### `provider`

Data the app already has: the clock, your sessions, your machines, harness
usage. `name`, `with` (the provider's arguments) and `every` (at least the
provider's minimum). It works without adding its package. An app provider
(`app/…`) takes no `target` and no `timeout`: it is the app's own data.

A provider can also be a package's own command, written
`<package>/<command>` — see [package providers](#package-providers) below.

#### Package providers

A package can let its widgets read one of its own commands, declared under
`providers` in its `package.yml` (see [the package
format](package-format.md#providers)):

```yaml
requires: {engine: ">= 1.3"}
inputs:
  machine: {type: string, summary: Which machine.}
source:
  kind: provider
  name: devmachine-app/stats
  with: {path: /}
  target: {machine: "{{inputs.machine}}"}
  every: 60s
view: {kind: gauge, value: "{{json.disk.used_percent}}", unit: "%"}
```

- `name` is `<package>/<command>`. A widget in a package reads only that
  package's own providers. A widget written in a board reads any
  package's.
- `target` is required, `{machine: <name>}` or `{workspace: <name>}`. A
  package's command runs on a machine, never on your computer, so `local`
  is refused.
- `every` is at least the provider's `min_every`, or `manual`.
- `with` becomes arguments after the command, one `--<key> <value>` pair
  per key, keys sorted: the widget above runs `stats --path /`. A key is
  lower case letters, digits, dashes and underscores; a value is any text,
  and may hold `{{inputs.x}}` or `{{context.x}}`.
- `timeout` stops a run that takes longer, default `30s`, at most `10m`.
- The answer is one JSON object, read like `parse: json`: draw it with a
  view that takes `json`, and pick the value with `view.value`.
- `requires.engine` must refuse engine 1.2, which cannot run it.
- The package has to be added to that machine or workspace and synced;
  until then `widgets list` names the command to add it.

### `command`

```yaml
source:
  kind: command
  run: df
  args: [-h, /]
  target: {machine: "{{inputs.machine}}"}
  every: 60s
  parse: text
```

- `run` is one program, or `script` is a file: one of the two. In a
  package widget `script` is relative to `package.yml` and must stay in the
  package, and every account must be able to run it (`chmod 755`): on a
  workspace it runs as that workspace's account. In a widget written in a
  board it is an absolute path on the target.
- `args` are passed to the program one by one; no shell reads them, so a
  space or a `;` in one is just a character. A `run` with spaces is refused:
  put the arguments in `args`, or set `shell: true`, and `run` becomes a
  shell line.
- `run` never holds a template: a value must not pick the program, and a
  value written into a shell line would run as code. Without a shell, put
  templates in `args`. For a program that takes options, put `--` before
  a templated argument (`args: [--, "{{inputs.path}}"]`), so a value that
  starts with `-` is not read as an option.
- A shell line takes no `args`. It reads each value from an environment
  variable the app sets: `DM_INPUT_` or `DM_CONTEXT_` followed by the name
  in upper case, with any character other than A–Z and 0–9 turned into
  `_`. So `inputs.max_lines` is `$DM_INPUT_MAX_LINES`, and
  `context.workspace` is `$DM_CONTEXT_WORKSPACE`. An input name holds only
  lower case letters, digits and `_`, so no two inputs share a variable.
  Write it in double quotes:
  `run: 'tail -n "$DM_INPUT_MAX_LINES" /var/log/syslog'`. The shell never
  reads the value as code, unless you hand it to a program that does:
  `eval`, `sh -c`, `ssh <host> …`, `awk`, `xargs`, `perl -e`,
  `python -c` — or bash arithmetic: `$(( ))`, `(( ))`, `let`,
  `[[ … -gt … ]]` and `declare -i` run a value such as `a[$(id)]` as a
  command. That is your own line's code, so keep values out of those.
  `/bin/sh` on a Mac is bash, so the arithmetic rule applies there too:
  check that a value is a number with `case` or `[ … ]` first, as in
  `case $DM_INPUT_MAX_LINES in ''|*[!0-9]*) exit 1;; esac`.
- A context key's variable holds text: a `machine`, `workspace` or
  `session` key holds its name, a `repo` key holds `owner/name`, and a
  `path` or `string` key holds the value itself.
- Without a shell, `run` cannot start with `-` or hold `=`: it would read
  as an option or a variable to set, not a program.
- `target` is `local` (the computer the app runs on, the default),
  `{machine: <name>}` or `{workspace: <name>}`. A machine or a workspace is
  reached through `devmachine run --no-log`; the app never opens its own
  SSH. Widget runs always use `--no-log`: their values would otherwise
  land in [the command log](commands.md#the-command-log).
- `every` is how often: a duration of at least `5s`, or `manual` for a ▶
  button. `timeout` is `30s` unless you say otherwise, at most `10m`.
- `parse` says how the output is read: `text`, `lines`, `number`, `json`
  or `ansi` (text with colours). A non-zero exit or output that does not
  parse is an error; the widget keeps its last good value and shows why.
- `mode: stream` runs a command that keeps printing, such as `tail -f`,
  while the widget is on screen. It takes no `every` and no `timeout`,
  parses `text`, `lines` or `ansi`, and keeps the last `keep` lines (200
  by default, at most 2000).

### `url`

```yaml
source: {kind: url, url: "https://example.com/health", every: 30s, parse: status}
```

A GET request. `parse` is `status` (the HTTP code and how long it took,
the default), `text` or `json`. `every` at least `5s`; `timeout` as for a
command. A url widget works without adding its package: it installs
nothing and runs nothing on a machine.

### `prompt`

```yaml
source:
  kind: prompt
  harness: claude
  prompt: Summarise what changed in the repository today.
  target: {workspace: alice}
```

Asks a coding harness, `claude` or `codex`, without a conversation, on the
target, and shows the answer as Markdown. `every` is `manual` unless you
set one, at least `15m`: every run costs tokens. `timeout` is `5m` by
default. Only one run of a widget happens at a time.

### `session`

```yaml
source: {kind: session, target: {workspace: alice}, session: main, every: 2s}
```

A read-only copy of what a session's screen shows, read every `every` (at
least `2s`). Draw it with the `terminal` view; a click opens the session in
the app.

### Templates

`{{inputs.<name>}}` and `{{context.<key>}}` can go in `args`, target
names, `url`, `prompt` and `session`, never in `run` (a shell line reads
them from `$DM_INPUT_…` and `$DM_CONTEXT_…` instead). The input or key has to be
declared. `{{item}}` works only inside a list view's `item`.

## Views

A view draws what a source gives. `devmachine widgets schema` lists which
outputs each view takes; a view that cannot draw the source is refused.

| View | Draws | Fields |
| --- | --- | --- |
| `text` | `text`, `lines`, `ansi` | `wrap` (true unless `false`), `tail` (last N lines, 1–2000) |
| `number` | `number`, `json`, `app/open-pull-requests` | `value`, `unit`, `format`: `plain`, `percent`, `bytes`, `duration`; `hide_zero` (draw nothing at 0) |
| `gauge` | `number`, `json` | `value`, `min` (0), `max` (100), `unit`, `warn`, `crit` |
| `status` | `status`, `number`, `json`, `text` | `value`, `ok`, `warn` rules |
| `list` | `lines`, `json` (an array) | `item: {title, subtitle, status, link}` |
| `sparkline` | `number`, `json` | `value`, `unit`, `max`; the app keeps the last 120 points |
| `markdown` | `text` (a prompt's answer, or any text) | none |
| `web` | any `url` source; it loads the page itself | `zoom` (0.5–2, default 1) |
| `terminal` | `ansi`, `text`, a `session` | `tail` |

**Picking a value out of JSON.** With `parse: json`, `number`, `gauge`,
`status` and `sparkline` need `value`, a template naming a field:
`value: "{{json.disk.used}}"`. Without `parse: json`, `value` is refused.

**Status rules.** `ok` and `warn` compare the value: `"< 300"`, `">= 99.5"`,
`'== "up"'`, `'!= "down"'`. Text compares only with `==` and `!=`, so a
source that gives text cannot use `<`, `<=`, `>` or `>=`. The first rule
that holds picks the colour; none holding means failing.

**List items.** Each line, or each element of a JSON array, is one item.
`{{item}}` is the whole item; `{{item.name}}` reads a field of a JSON
item. `title` is `{{item}}` unless you set it.

**Web.** The page is loaded in a private browser store that keeps no
cookies between launches, and reloaded on `every`. `source.parse` does
nothing here and is refused.

## A board

Where the widgets sit is a board: `<config>/boards/<surface>.yml`. The app,
the CLI and an agent all read and write it.

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

| Field | What it is |
| --- | --- |
| `id` | Unique on the board: lower case letters, digits and dashes. |
| `type` | The widget, `<package>/<widget>`. |
| `with` | Values for the widget's inputs. Left out when there are none. A choice of many takes a list: `with: {machines: [main, backup]}`; `[]` means all. |
| `frame` | Position and size in points. `x` and `y` are 0 or more; `w` and `h` are at least the smallest preset the widget takes. |
| `size` | A preset, or `custom` after a free resize. Left out, it is `custom`. |
| `minimized` | `true` draws a pill with the title instead. |
| `z` | Higher is in front. |

A `type` no package provides is not an error: the app keeps the entry and
shows a placeholder, so a missing package never loses a layout. A widget
can also be written in place, with no `type` and no package: see below. A
widget has either a `type` or a `source` and a `view`, never both.

A key the board does not know, at the top, in a widget or in a `frame`, is
a mistake, for example `unknown key "minimised" in a widget`. A typo is
caught instead of being dropped on the next write.

When the app or the CLI rewrites a board, comments in it are lost.

### A widget written in the board

```yaml
  - id: disk
    title: Disk on alice
    source: {kind: command, run: df, args: [-h, /], target: {workspace: alice}, every: 60s}
    view: {kind: text}
    sizes: [medium, wide]
    frame: {x: 24, y: 400, w: 320, h: 160}
    size: medium
    minimized: false
    z: 9
```

It needs a `title`, a `source` and a `view`, and takes `sizes` (all the
presets when left out) and `fits`. It has no `type`, no `with` and no
inputs, so a template can only name a context key the board's area gives.
A `script` is an absolute path on the target. Every source and view rule
above applies.

The app runs a `command`, `prompt` or `session` written in a board only
after you press **Allow** on it, and asks again whenever it changes. See
[why a widget an agent wrote waits for you](../how-it-works/why-an-agent-written-widget-waits.md).

### A board in a sidebar

The sidebar (`sidebar.yml`) and the context sidebar (`context-sidebar.yml`)
are stacks: a list, drawn top to bottom in the order it is written. A
widget there has no `frame`, no `minimized` and no `z`.

```yaml
format: 1
surface: context-sidebar
widgets:
  - id: todo
    type: devmachine-app/todo
    size: auto
  - id: usage
    type: claude-code/usage
    size: medium
    collapsed: true
```

| Field | What it is |
| --- | --- |
| `id`, `type`, `with` | As on Home. A widget written in place (`title`, `source`, `view`) works here too. |
| `size` | A preset the widget takes, or `auto`. In a sidebar a widget is as wide as the panel and each preset row is 40pt high (`medium` is 80pt, `large` 160pt). `auto` makes it as tall as what it shows, and only a view that grows takes it (the app's sidebar views). Left out, it is `auto` for a view that grows and the widget's `default_size` otherwise. A widget written in place has no `default_size`: the CLI accepts a missing `size` there and writes none, and the app draws it at the first preset in its `sizes`, or `medium` when it lists none. |
| `collapsed` | `true` shows only its header, and nothing runs. Left out when false. |

A widget fits a sidebar when `stack` is in its `fits`; a widget written in
place fits every layout. A widget marked `single: true` appears once per
board.

When the file is missing, the app writes, and the CLI reads, these:

```yaml
format: 1
surface: sidebar
widgets:
  - id: workspaces
    type: devmachine-app/workspaces
    size: auto
```

and a `context-sidebar.yml` with, in this order, `shortcuts`,
`publish-port`, `monitors`, `shells`, `sub-agents`, `todo`,
`pull-requests` and `links`, each `type: devmachine-app/<id>` and
`size: auto`.

### A board in the menu bar

The menu bar item is two boards, both lists drawn in the order they are
written.

`menubar.yml` is the title in the menu bar: at most 3 widgets, left to
right, each one line. An entry there has only `id`, `type` and `with`
(or `title`, `source` and `view` when written in place): no `frame`,
`size`, `minimized`, `collapsed` or `z`. Only four views draw there:
`text` (its first line, cut at 24 characters with "…"), `number`
(`hide_zero: true` draws nothing at 0), `status` (a dot and its label)
and `app.brand` ("❯_"). A widget fits it when `slot` is in its `fits` and
its view is one of those. Its widgets run while the app runs, with or
without a window, and never more often than every 30s: a shorter `every`
is raised to 30s.

```yaml
format: 1
surface: menubar
widgets:
  - id: brand
    type: devmachine-app/brand
  - id: open-pull-requests
    type: devmachine-app/open-pull-requests
```

`menubar-panel.yml` is the popover that opens when you click it: each
widget is one tab, titled with the widget's title. A tab fills the
popover, so a `size` there is kept but ignored, and `widgets validate`
warns about it; `frame`, `minimized`, `collapsed` and `z` are refused. A
widget fits it when `tabs` is in its `fits`. Its widgets run only while
the popover is open. The footer (Open, Stats, Subdomains, the color
picker, Quit) is not a widget.

```yaml
format: 1
surface: menubar-panel
widgets:
  - id: pull-requests-panel
    type: devmachine-app/pull-requests-panel
  - id: usage-panel
    type: devmachine-app/usage-panel
```

A widget in the menu bar that waits for your approval shows "!" in its
place; its Allow card is in the popover, under an Approvals tab.

When either file is missing, the app writes, and the CLI reads, the two
boards above: they draw the menu bar as it always looked, "❯_" and the
open pull request count, and the Pull Requests and Usage tabs.

## What the engine offers

`devmachine widgets schema --json` prints all of this as JSON.

<!-- generated from the engine contract by `make widget-format`: start -->

Engine **1.5**. Widget format 1, board format 1.

### Sizes

One unit is 80pt; positions and free resizes snap to 8pt.

One preset row in a sidebar is 40pt high, and a widget there is as wide as the panel. `size: auto` makes a view that grows as tall as what it shows.

The menu bar holds at most 3 widgets, left to right, each one line drawn by one of the views `text`, `number`, `status`, `app.brand`. Text shows its first line, cut at 24 characters. A widget there runs at most every 30s. A tab in the menu bar popover fills it, so a `size` there is ignored.

| Preset | Units | Points |
| --- | --- | --- |
| `small` | 2×2 | 160×160 |
| `medium` | 4×2 | 320×160 |
| `tall` | 2×4 | 160×320 |
| `large` | 4×4 | 320×320 |
| `wide` | 8×2 | 640×160 |

### Surfaces

| Surface | Layout | Status | Context it gives |
| --- | --- | --- | --- |
| `context-sidebar` | stack | available | `branch` (string, optional), `harness` (string, optional), `machine` (machine, always), `path` (path, optional), `repo` (repo, optional), `session` (session, always), `workspace` (workspace, optional) |
| `home` | canvas | available | none |
| `menubar` | slot | available | none |
| `menubar-panel` | tabs | available | none |
| `sidebar` | stack | available | `selected` (workspace, optional) |

### Context types

| Type | Fields |
| --- | --- |
| `machine` | `name` string |
| `path` | none |
| `repo` | `name` string, `owner` string |
| `session` | `harness` string?, `kind` string, `name` string |
| `string` | none |
| `workspace` | `machine` machine, `name` string, `path` path, `user` string |

### Inputs

| Type | Fields besides `type`, `default` and `summary` | A board's `with` value |
| --- | --- | --- |
| `boolean` | none | bool |
| `choice` | `from` enum, required, harnesses/machines/workspaces; `many` bool, default `false` | string, or a list of strings when many |
| `number` | none | number |
| `string` | none | string |

A choice takes its options from `harnesses` (claude, codex), `machines` (the machines in config.yml), `workspaces` (the workspaces in config.yml).

An entry with a `type` may also set `every` every; `title` string on a board: they change only that copy.

### Providers

| Provider | Arguments | Context it needs | Minimum `every` | Returns |
| --- | --- | --- | --- | --- |
| `app/brand` | none | none | 5s | `mark` string |
| `app/clock` | none | none | 5s | `date` string, `host` string, `time` string |
| `app/harness-usage` | `harness` string, required | none | 5s | `error` string?, `harness` string, `windows` list |
| `app/machines` | `machines` list, optional | none | 5s | `list` machine_stats |
| `app/open-pull-requests` | none | none | 5s | `count` number |
| `app/publish-port` | none | none | 5s | `available` bool |
| `app/pull-requests-panel` | none | none | 5s | `error` string?, `owners` list, `pull_requests` list |
| `app/session-context` | none | `session` required | 5s | `agents` list, `cwd` path, `harness` string?, `links` list, `monitors` list, `plan` plan?, `prs` list, `shells` list |
| `app/shortcuts` | none | `session` required | 5s | `shortcuts` list |
| `app/summary` | none | none | 5s | `harness_sessions` int, `sessions` int, `workspaces` int |
| `app/usage-panel` | none | none | 5s | `harnesses` list |
| `app/workspaces` | none | none | 5s | `workspaces` workspace_sessions |

### Package providers

A package declares providers in its `package.yml`; a widget names one `<package>/<command>`. `app/…` is the app's own. It runs on a machine or a workspace, never on your computer, and its answer is one JSON object, read like `parse: json`. Each `with` key becomes `--<key> <value>` after the command, keys sorted. Its `min_every` is at least 5s, and `returns` types are string, number, bool, list, object, with `?` for an optional field. Its widgets wait for approval when the package is third-party.

### Source kinds

| Kind | Minimum `every` | Waits for approval in a board | Fields besides `kind` |
| --- | --- | --- | --- |
| `provider` | the provider's | no | `every` every, required; `name` provider, required; `target` target; `timeout` duration, default `30s`, max 10m; `with` args |
| `command` | 5s | yes | `args` list; `every` every; `keep` int, default `200`, min 1, max 2000; `mode` enum, poll/stream, default `poll`; `parse` enum, text/lines/number/json/ansi, default `text`; `run` string; `script` path; `shell` bool, default `false`; `target` target, default `local`; `timeout` duration, default `30s`, max 10m |
| `url` | 5s | no | `every` every, required; `parse` enum, status/text/json, default `status`; `timeout` duration, default `30s`, max 10m; `url` url, required |
| `prompt` | 15m | yes | `every` every, default `manual`; `harness` enum, required, claude/codex; `prompt` string, required; `target` target, default `local`; `timeout` duration, default `5m`, max 10m |
| `session` | 2s | yes | `every` every, required; `session` string, required; `target` target, default `local` |

### Views

| View | Draws | Drawn in | Fields besides `kind` |
| --- | --- | --- | --- |
| `app.brand` | `app/brand` | slot | none |
| `app.clock` | `app/clock` | canvas, stack, tabs | none |
| `app.harness-usage` | `app/harness-usage` | canvas, stack, tabs | none |
| `app.links` | `app/session-context` | stack; grows with its content | none |
| `app.machines` | `app/machines` | canvas, stack, tabs | none |
| `app.monitors` | `app/session-context` | stack; grows with its content | none |
| `app.publish-port` | `app/publish-port` | stack; grows with its content | none |
| `app.pull-requests` | `app/session-context` | stack; grows with its content | none |
| `app.pull-requests-panel` | `app/pull-requests-panel` | canvas, stack, tabs | none |
| `app.shells` | `app/session-context` | stack; grows with its content | none |
| `app.shortcuts` | `app/shortcuts` | stack; grows with its content | none |
| `app.sub-agents` | `app/session-context` | stack; grows with its content | none |
| `app.summary` | `app/summary` | canvas, stack, tabs | none |
| `app.todo` | `app/session-context` | stack; grows with its content | none |
| `app.usage-panel` | `app/usage-panel` | tabs | none |
| `app.workspaces` | `app/workspaces` | stack; grows with its content | none |
| `gauge` | `number`, `json` | canvas, stack, tabs | `crit` number; `max` number, default `100`; `min` number, default `0`; `unit` string; `value` template; `warn` number |
| `list` | `lines`, `json` | canvas, stack, tabs | `item` object (`link` template; `status` template; `subtitle` template; `title` template, default `{{item}}`) |
| `markdown` | `text` | canvas, stack, tabs | none |
| `number` | `number`, `json`, `app/open-pull-requests` | any layout | `format` enum, plain/percent/bytes/duration, default `plain`; `hide_zero` bool, default `false`; `unit` string; `value` template |
| `sparkline` | `number`, `json` | canvas, stack, tabs | `max` number; `unit` string; `value` template |
| `status` | `status`, `number`, `json`, `text` | any layout | `ok` rule; `value` template; `warn` rule |
| `terminal` | `ansi`, `text`, `kind:session` | canvas, stack, tabs | `tail` int, min 1, max 2000 |
| `text` | `text`, `lines`, `ansi` | any layout | `tail` int, min 1, max 2000; `wrap` bool, default `true` |
| `web` | `kind:url` | canvas, stack, tabs | `zoom` number, default `1`, min 0.5, max 2 |

<!-- generated from the engine contract by `make widget-format`: end -->

## The rules, and what each one says

| Rule | The message |
| --- | --- |
| `format` is not 1 | `format 2, and this CLI reads widget format 1` |
| `requires.engine` missing | `every widget needs requires.engine, for example ">= 1.0"` |
| `requires.engine` unreadable | `requires.engine "X": write it as ">= 1.0", "> 1.0" or "= 1.0"` |
| a newer engine is required | ``requires engine >= 1.6, and this CLI implements engine 1.5: update with `devmachine update` `` |
| an unknown top-level field | `unknown field "X"` |
| `name` malformed or not the folder | `name is "X" but the folder is "Y": a widget is found by its folder` |
| `summary` missing | `every widget needs a one-line summary` |
| `fits` empty or unknown | `fits "X": the layouts are canvas, stack, slot, tabs` |
| `sizes` empty or unknown | `size "X": the presets are small, medium, tall, large, wide` |
| `default_size` not in `sizes` | `default_size "X" is not one of sizes` |
| `places` unknown | `places "X": the surfaces are context-sidebar, home, menubar, menubar-panel, sidebar` |
| `places` names an area it does not fit | `places "sidebar": the widget does not fit that area, so the app would never place it there` |
| `fits` names a layout its view is not drawn in | `fits canvas, and the app.todo view is drawn only in stack` |
| a provider's context not declared | `source.name app/session-context needs context.session: declare context: {session: required}` |
| `context` key no surface gives | `context key "X" is not given by any surface` |
| `context` value other than required/optional | `context key "X" is "Y": write required or optional` |
| input type unknown, or default of the wrong type | `input "X" has type "Y": the types are string, number, boolean, choice`, `input "X" is a string, and its default 3 is not` |
| a choice without `from`, or an unknown one | `input "X" is a choice, and needs from: one of harnesses, machines, workspaces`, `input "X" takes its options from "Y", which is not a source: the sources are harnesses, machines, workspaces` |
| `many` neither true nor false | `input "X": many is maybe: write true or false` |
| a choice's default of the wrong shape | `input "X" is a list of choices, and its default main is not: write [a, b], or [] for all`, `input "X" is one choice, and its default [main] is not a name` |
| `from` or `many` on another type | `input "X" is a string: from and many belong to a choice` |
| a choice input on an engine below 1.5 | `a widget with a choice input needs requires.engine ">= 1.5": an app on engine 1.4 cannot show its choices` |
| a board's `with` of the wrong shape for a choice | `machines: input "machines" takes a list of names, written [a, b], and main is not one`, `usage: input "harness" takes one name, and [claude, codex] is not one` |
| template names something undeclared | `template {{inputs.X}} in source.with.Y needs inputs.X` |
| `source.kind` unknown | `source.kind "X": engine 1.5 knows provider, command, url, prompt, session` |
| a key of another kind | `source.url is not a field of a command source: it takes …` |
| no `run`/`script`, or both | `a command source needs run or script`, `a command source has run or script, not both` |
| `run` with spaces and no `shell: true` | `source.run "df -h /" has spaces: put each argument in source.args, or set shell: true …` |
| a template in a `shell: true` line | `source.run is a shell line, so it cannot hold {{inputs.x}}: read it as "$DM_INPUT_X" instead` |
| `args` with `shell: true` | `a shell line takes no source.args: write the words in source.run, and read values as $DM_INPUT_<NAME>` |
| a template in `run` without a shell | `source.run cannot hold {{inputs.x}}: a value must not pick the program — name the program in run, and put the value in source.args` |
| `run` starting with `-` or holding `=`, no `shell: true` | `source.run "-df" starts with -: …`, `source.run "LANG=C" has =: …` |
| `script` outside the package, or not runnable | `source.script "X": a package widget names a file inside its package …`, `… is not executable: run chmod +x on it` |
| `every` missing, unreadable or too short | `a command source needs source.every …`, `source.every 1s is below the command minimum of 5s` |
| `timeout` above 10m | `source.timeout 11m is above the 10m maximum` |
| stream with `every`, `timeout` or a whole-output parse | `a stream runs while the widget is on screen: remove source.every` |
| `keep` without a stream, or out of 1–2000 | `source.keep only applies to mode: stream` |
| `target` malformed | `source.target "X": write local, {machine: <name>} or {workspace: <name>}` |
| `parse`, `mode` or `harness` unknown | `source.parse "yaml": a command source takes text, lines, number, json, ansi` |
| `url` not a full address | `source.url "X": write a full address starting with https:// or http://` |
| view does not draw the source | `view.kind "gauge" takes number, json, and this command source gives text` |
| `source.name` unknown | `source.name "X" is not a provider engine 1.5 knows` |
| `source.with` wrong | `source.with.X is not an argument of P`, `source.with.X is required by P` |
| an app provider with target or timeout | `source.target: app/clock is the app's own data, so it takes no target` |
| `source.every` missing, unreadable or too short | `source.every 1s is below the P minimum of 5s` |
| a package widget reading another package's provider | `source.name claude-code/usage: a package widget reads only its own package's providers, written devmachine-app/<command>` |
| a provider the package does not declare | `source.name devmachine-app/context: devmachine-app has no provider context; its providers: stats` |
| a package provider with no target, or `local` | `source.name devmachine-app/stats: a package provider runs on a machine or workspace: write source.target: {machine: <name>} or {workspace: <name>}` |
| `every` below the provider's `min_every` | `source.every 5s is below the devmachine-app/stats minimum of 10s` |
| a `with` key that is not lower case | `source.with.Path: devmachine-app/stats gets it as --Path, so write the key in lower case …` |
| a package provider with an engine that allows 1.2 | `a widget reading a package provider needs requires.engine ">= 1.3": an app on engine 1.2 cannot run it` |
| a widget outside any package reading a package provider | `source.name devmachine-app/stats: only a widget in a package, or one written in a board, reads a package provider` |
| `view.kind` unknown, or draws another provider | `view.kind "app.clock" draws app/clock, not P` |
| a view key that view does not take | `view.colour is not a field of the gauge view: it takes kind, …` |
| JSON without `value`, or `value` without JSON | `view.value picks what to show out of the JSON …`, `view.value only applies when source.parse is json` |
| a number field that is not a number or out of range | `view.warn is high, and it has to be a number`, `view.zoom 3 is above 2` |
| `gauge` `max` not above `min` | `view.max 0 must be above view.min 0` |
| `tail` out of 1–2000 | `view.tail 5000 is outside 1 to 2000 lines` |
| `format` unknown | `view.format "hex": the number view takes plain, percent, bytes, duration` |
| a status rule that does not read | `view.ok fine is not a rule: write it like "< 300" or '== "ok"'` |
| a status rule comparing text by size | `view.ok "< 300" compares by size, and this url source gives text: use == or !=` |
| `{{item.x}}` on lines, or an unknown item key | `template {{item.name}} in view.item.title reads a field, and only JSON items have fields` |
| `web` with `source.parse` | `the web view loads the page itself: remove source.parse` |
| a board widget with no `title`, `source` or `view` | `disk: a widget written in the board needs a title` |
| a 4th widget in the menu bar | `the menubar holds 3 widgets: take one off` |
| `frame`, `size`, `minimized`, `collapsed` or `z` in the menu bar | `brand: a widget in the menu bar has no size: it is one line of text` |
| a view the menu bar does not draw | `usage: the app.harness-usage view cannot be drawn in the menu bar` |
| `frame`, `z`, `minimized` or `collapsed` on a tab | `usage-panel: a tab in the menu bar popover has no frame; its place is its position in the list` |
| `size` on a tab (a warning, not a problem) | `usage-panel: size is ignored in the menu bar popover: a tab fills it` |
| a board widget with `with` or `{{inputs.x}}` | `disk: a widget written in the board has no inputs, so it takes no with` |
| a board widget's `script` not absolute | `disk: source.script "bin/df": a widget written in a board names an absolute path on the target` |
| a board widget's target is not in `config.yml` (checked by `widgets validate` only) | `disk: source.target names machine "X", which config.yml does not have` |
| a sidebar widget with `frame` or `z` | `todo: a widget in a sidebar has no frame; its place is its position in the list` |
| a sidebar widget with `minimized` | `usage: a widget in a sidebar folds with collapsed: true, not minimized` |
| a Home widget with `collapsed` | `clock: a widget on a canvas folds with minimized: true, not collapsed` |
| `size: auto` on a view that does not grow | `usage: size auto follows the content, and the app.harness-usage view does not grow: use small, medium, wide` |
| a sidebar size that is neither auto nor a preset it takes | `todo: size "custom" in a sidebar is auto or one of medium, large` |
| `size: auto` on a widget written in a sidebar board with no view | `notes: size auto follows the content, and a widget with no view does not grow: use small, medium, tall, large, wide` |
| a board widget whose `fits` lacks the area's layout | `todo: devmachine-app/clock does not fit the context-sidebar area: its fits has no stack` |
| a board widget that requires context the area lacks | `todo: devmachine-app/todo needs context.session, which the sidebar area does not give` |
| a `single` widget twice on one board | `workspaces-2: devmachine-app/workspaces goes on a board once, and workspaces already has it` |
| a widget written in a board whose provider needs context the area lacks | `keys: source.name app/shortcuts needs context.session, which this board's area does not give` |
| a widget written in a board whose view is drawn only in a stack, on Home | `port: the app.publish-port view is drawn only in stack, and this board's area is laid out as canvas` |

A choice that names a machine or workspace your `config.yml` does not
have, or a harness the engine does not know, is a warning (`warning: …`),
never a problem: see [choosing and editing a
widget](../how-it-works/choosing-and-editing-a-widget.md).

`devmachine widgets validate` and `devmachine packages validate` report
every problem at once, with the file and line.
