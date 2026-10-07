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
