# Choosing and editing a widget

A widget can let you pick what it shows: which machines, which coding
harness. The widget only declares the input; the app draws the chooser.
This page says where the choices come from and why a wrong one never
breaks a board.

## Where the options come from

A `choice` input names one of three lists: `machines` and `workspaces`,
read from your `config.yml`, and `harnesses`, the coding harnesses that
report usage. The app fills the list from what it already knows, so any
widget with a choice gets a checklist (`many: true`) or a picker, with
no code written for that widget.

The board keeps the names you picked: one name, or a list for a choice
of many, such as `with: {machines: [main, backup]}`. An empty list means
all of them, so a widget you never touched follows your configuration as
it grows.

The app, not the CLI, builds the value a widget reads. A choice of many
reaches `{{inputs.x}}` and `$DM_INPUT_X` as its names joined with commas,
so `[main, backup]` arrives as `main,backup`. A comma is the separator, so
a name with a comma in it would read as two names. The CLI does not allow
one in a machine name, and `--set name=a,b` always splits at commas. It
does not check a workspace name, so keep commas out of those. What an
empty list (all of them) arrives as is the app's choice, so a widget
should not depend on it.

## Why a name the configuration lacks is only a warning

Machines come and go. If removing `backup` from `config.yml` made every
board that once picked it invalid, the CLI would refuse to change those
boards, and the app would refuse to draw them, until you edited each file
by hand. So `widgets validate`, `widgets add` and `widgets set` print a
warning, and the app leaves the name out. The board stays usable, and the
warning tells you which pick to clean up.

A name of the wrong shape is different: a single name where the widget
takes a list, or a list where it takes one name. That is a mistake in the
file, not a change in your setup, so it is a problem at its line.

## Why only a widget from a package takes title and every

A widget from a package is shared: its `widget.yml` is the same for
everybody, so a board entry is the only place to say "call this one
Claude, and check it every two minutes". A widget written in the board
has no such split: its title and its `source.every` are already in the
entry, so a second `every` next to them would only leave two answers to
the same question.

The override may not go below the source's minimum, for the same reason
the widget itself may not: the minimum protects the machine the source
runs on, not the widget.

A prompt widget whose `permission_mode` runs without any check takes only
`every: manual` there too: changing one copy cannot put it on a timer. See
[a prompt widget's permission mode](a-prompt-widgets-permission-mode.md).

## Why widgets set leaves a written widget's source alone

A widget written in a board runs what its `source` says only after you
press Allow, and the app asks again whenever that source changes. A
command that rewrites the source from flags would make that change easy
to make without looking at it, so `widgets set` changes a written
widget's title only. Its source is changed in the board file, where you
see the whole of it, or in the app's editor, which shows it and asks
again.
