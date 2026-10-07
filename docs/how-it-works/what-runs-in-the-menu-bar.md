# What runs in the menu bar

The menu bar item is two boards: the title you always see (`menubar.yml`)
and the popover that opens when you click it (`menubar-panel.yml`). They
run at different times, on purpose.

## The title runs whenever the app runs

The title is on screen even when every window is closed, so its widgets
run while the app runs, window or not. That is also why they are held
back: a widget there runs at most every 30 seconds, whatever its `every`
says. A command or a provider that reaches a machine over SSH every 5
seconds, all day, costs battery and the machine's time for a number you
glance at.

## The popover runs only while it is open

Nothing in the popover shows when it is closed, so its widgets start when
it opens and stop when it closes. A tab is the whole popover, which is why
a `size` there does nothing.

## Why three widgets, one line and 24 characters

macOS draws the menu bar title as a small picture, in the same colour as
the other icons, and the strip is shared with the clock and every other
app. So the title holds text and at most one symbol per widget (the
status dot), never a chart or an image. Three widgets of at most 24
characters keep it from pushing other items off the screen. Longer text is
cut with "…". That is also why only `text`, `number`, `status` and
`app.brand` draw there.

## A widget that waits for your approval

A widget that runs something you have not allowed yet cannot ask in one
line. It shows "!" in its place; clicking the menu bar item opens the
popover with an Approvals tab, where its Allow card is. See [why a widget
an agent wrote waits for you](why-an-agent-written-widget-waits.md).
