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
| `fits` | yes | The layouts it can be drawn in: `canvas`, `stack`, `slot`. |
| `context` | no | The context keys it reads, each `required` or `optional`. A widget sees only the keys it declares. |
| `inputs` | no | Values a person sets on each copy: `type` (`string`, `number` or `boolean`), `default`, `summary`. |
| `source` | yes | Where the data comes from: `kind`, `name`, `with` (the provider's arguments) and `every` (how often, a duration like `60s`). |
| `view` | yes | How it is drawn: `kind`. |
| `sizes` | yes | The presets it takes. |
| `default_size` | yes | The preset a new copy gets. One of `sizes`. |
| `places` | no | Surfaces the app adds it to once, the first time it is available. Removing it from there is final. |

A value in `source.with` can hold `{{inputs.<name>}}` or
`{{context.<key>}}`. The input or key it names has to be declared.

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
| `with` | Values for the widget's inputs. Left out when there are none. |
| `frame` | Position and size in points. `x` and `y` are 0 or more; `w` and `h` are at least the smallest preset the widget takes. |
| `size` | A preset, or `custom` after a free resize. Left out, it is `custom`. |
| `minimized` | `true` draws a pill with the title instead. |
| `z` | Higher is in front. |

A `type` no package provides is not an error: the app keeps the entry and
shows a placeholder, so a missing package never loses a layout. A widget
written in place, with its own `source` and `view` and no `type`, needs
engine 1.1 and is refused for now. A widget has either a `type` or a
`source` and a `view`, never both.

A key the board does not know, at the top, in a widget or in a `frame`, is
a mistake, for example `unknown key "minimised" in a widget`. A typo is
caught instead of being dropped on the next write.

When the app or the CLI rewrites a board, comments in it are lost.

## What the engine offers

`devmachine widgets schema --json` prints all of this as JSON.

<!-- generated from the engine contract by `make widget-format`: start -->

Engine **1.0**. Widget format 1, board format 1.

### Sizes

One unit is 80pt; positions and free resizes snap to 8pt.

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
| `context-sidebar` | stack | planned | `branch` (string, optional), `harness` (string, optional), `machine` (machine, always), `path` (path, optional), `repo` (repo, optional), `session` (session, always), `workspace` (workspace, optional) |
| `home` | canvas | available | none |
| `sidebar` | stack | planned | `selected` (workspace, optional) |

### Context types

| Type | Fields |
| --- | --- |
| `machine` | `name` string |
| `path` | none |
| `repo` | `name` string, `owner` string |
| `session` | `harness` string?, `kind` string, `name` string |
| `string` | none |
| `workspace` | `machine` machine, `name` string, `path` path, `user` string |

### Providers

| Provider | Arguments | Minimum `every` | Returns |
| --- | --- | --- | --- |
| `app/clock` | none | 5s | `date` string, `host` string, `time` string |
| `app/harness-usage` | `harness` string, required | 5s | `error` string?, `harness` string, `windows` list |
| `app/machines` | none | 5s | `list` machine_stats |
| `app/summary` | none | 5s | `harness_sessions` int, `sessions` int, `workspaces` int |

### Views

| View | Draws |
| --- | --- |
| `app.clock` | `app/clock` |
| `app.harness-usage` | `app/harness-usage` |
| `app.machines` | `app/machines` |
| `app.summary` | `app/summary` |

<!-- generated from the engine contract by `make widget-format`: end -->

## The rules, and what each one says

| Rule | The message |
| --- | --- |
| `format` is not 1 | `format 2, and this CLI reads widget format 1` |
| `requires.engine` missing | `every widget needs requires.engine, for example ">= 1.0"` |
| `requires.engine` unreadable | `requires.engine "X": write it as ">= 1.0", "> 1.0" or "= 1.0"` |
| a newer engine is required | ``requires engine >= 1.1, and this CLI implements engine 1.0: update with `devmachine update` `` |
| an unknown top-level field | `unknown field "X"` |
| `name` malformed or not the folder | `name is "X" but the folder is "Y": a widget is found by its folder` |
| `summary` missing | `every widget needs a one-line summary` |
| `fits` empty or unknown | `fits "X": the layouts are canvas, stack, slot` |
| `sizes` empty or unknown | `size "X": the presets are small, medium, tall, large, wide` |
| `default_size` not in `sizes` | `default_size "X" is not one of sizes` |
| `places` unknown | `places "X": the surfaces are context-sidebar, home, sidebar` |
| `context` key no surface gives | `context key "X" is not given by any surface` |
| `context` value other than required/optional | `context key "X" is "Y": write required or optional` |
| input type unknown, or default of the wrong type | `input "X" has type "Y"`, `input "X" is a string, and its default 3 is not` |
| template names something undeclared | `template {{inputs.X}} in source.with.Y needs inputs.X` |
| `source.kind` from engine 1.1 | `source.kind "command" needs engine 1.1` |
| `source.name` unknown | `source.name "X" is not a provider engine 1.0 knows` |
| `source.with` wrong | `source.with.X is not an argument of P`, `source.with.X is required by P` |
| `source.every` missing, unreadable or too short | `source.every 1s is below the P minimum of 5s` |
| `view.kind` from engine 1.1 | `view.kind "gauge" needs engine 1.1` |
| `view.kind` unknown, or draws another provider | `view.kind "app.clock" draws app/clock, not P` |

`devmachine widgets validate` and `devmachine packages validate` report
every problem at once, with the file and line.
