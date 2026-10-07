# Why widgets come from packages

A widget can only come from a package: the pinned packages release, or a
package in your configuration folder. There is no widget store, no folder
of loose widgets, and no copy inside the app.

## One place for a tool and its widget

The usage widget for Claude Code belongs next to the recipe that installs
Claude Code. When the recipe changes, the widget can change in the same
release, and both arrive together on the next update. A second source of
widgets would be a second thing to pin, fetch, check and explain.

It also means your own widgets work the same way as the published ones.
A package in your configuration folder with a `widgets:` folder is all it
takes, and it replaces a release package of the same name, as packages
always do.

## When a widget is available

A widget whose data comes only from the app — `app/clock`,
`app/summary`, `app/machines`, `app/harness-usage` — is available at once.
The app already has that data; adding a package to a machine would install
nothing it needs.

Any other widget reads something its package installs on a machine. It is
available only when that package is added to a machine or workspace **and**
a sync has applied it. Until then `widgets list` shows it with
`available: false` and the command that fixes it:

```
add the package: devmachine packages add github-prs --machine <name>
sync to install the package: devmachine sync
```

"Synced" is read from `packages.lock`, which records what the last sync
applied. The CLI does not connect to a machine to check.

## Why the app has no built-in copy

The app ships no `widget.yml` of its own, not even for the clock. A
built-in copy would be a second version of the same widget that drifts
from the published one, and nobody could tell which one they are looking
at. This is the same choice as [nothing embedded in the
CLI](why-nothing-is-embedded.md).

The cost: the first time the app starts with no network and no packages
release in the cache, Home has nothing to draw and says it could not fetch
packages. Once a release is in the cache, it works offline.

## Which release

`widgets list` reads the release pinned in `config.yml`. With nothing
pinned yet — or no `config.yml` at all — it uses the latest release, the
one `setup` would pin, so the app has widgets before the first machine
exists. If the latest release cannot be found, the command fails and says
why, rather than showing an empty list.

## Why the CLI refuses a broken board

`widgets add` and `widgets remove` read the board, change it, and write it
back in one step that cannot leave half a file. If the board has a mistake,
they stop: writing it back would replace your typo with what the CLI
guessed you meant, and could drop widgets. If the file changes while the
command runs — the app saved it a moment before — the CLI stops too, and
running it again works on the new file. A save between the final check and
the write could still be lost. Both the app and the CLI write the board
rarely.
