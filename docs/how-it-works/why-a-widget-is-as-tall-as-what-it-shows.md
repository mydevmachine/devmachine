# Why a widget is as tall as what it shows

On Home, a widget used to be a box of a fixed size. A one-line summary sat
in a box 160pt high, and no preset was shorter than that. Engine 1.7 turns
this round: a widget whose view has a natural height is as tall as its
content, and a height you set can only make it shorter. This page says why
it works this way.

## A box is never taller than its content

Empty space under a widget says nothing and hides the widget below it. So
for a view that grows, the app draws `min(content, cap)`, where the cap is
the height you chose. `size: auto` means "no cap". A board file that still
says `size: custom` with `h: 160` for a summary keeps working: 160 is now a
cap, and the content is shorter.

Which views have a natural height is in the engine contract: a view
carries `grows: true` (`devmachine widgets schema --json`). A clock, a
gauge, a web page or a terminal has no such height, so it keeps its preset
or the size you gave it.

## The cap after a resize

When you drag a growing widget shorter, the stored height is the height
drawn when you let go, so it is never above the content. If the drag ends
within one snap (8pt) of the content, the app writes `size: auto` instead.
That way a widget you pulled to "about its size" goes on following its
content, and one you made clearly shorter stays shorter.

## Why `auto` has no `h`

An `h` next to `auto` would be a number nobody keeps true: the content
changes (a machine is added), and the file would be wrong at once. So the
frame of an `auto` widget is `{x, y, w}`. An `h` written by hand is read
and ignored, and the next write by the CLI or the app drops it. Without
`auto`, the `h` is required, as before.

## Pushing down is only on screen

When a widget grows, the ones below it move down so nothing is covered.
That happens when the app draws: each widget is placed in the order of its
stored `y`, and it is moved down only as far as needed to keep 8pt below
the widgets above it. The stored `y` does not change, and nothing is ever
pulled up. If it were written to the file, a machine added at noon would
rewrite your layout, and a machine removed would leave a hole in it.

## Why the CLI estimates

The CLI cannot measure what the app draws. To find a free spot for a new
widget (`widgets add` without `--at`), it counts an `auto` widget at the
height of its `default_size` (160pt for a widget written in the board).
This is a guess, and it is made on purpose: a guess that keeps new widgets
apart is enough, the app draws the real height, and `--at` places a widget
exactly where you say.

## What the CLI checks

A growing view has no least height on Home, only a least width (the width
of the smallest preset it takes), so the CLI refuses a frame that is too
narrow with "frame width … is narrower than …". A view that does not
grow keeps both limits. See [the widget format](../reference/widget-format.md)
for the rules, and [troubleshooting](../troubleshooting.md) for the messages.
