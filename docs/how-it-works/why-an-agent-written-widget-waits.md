# Why a widget an agent wrote waits for you

A board is a plain file in your configuration folder, and that is on
purpose: your coding agent can add a widget to Home as easily as you can.
It also means anything that can write that file can put a command on your
Home. So a widget written in a board that runs something — a `command`, a
`prompt` or a `session` — shows **Allow** and **Deny** instead of running,
until you say yes.

## What waits and what does not

- **A `command`, `prompt` or `session` written in a board waits.** These
  run a program on your computer, a machine or a workspace, or read a
  session's screen.
- **A `url` does not wait.** It only reads a web page, the way a browser
  would.
- **A widget from an official or a local package does not wait.** You
  chose that packages release, or you wrote the package; `devmachine
  packages validate` checks its widgets like the rest of it.
- **A widget from a third-party package waits when it runs something** —
  a `command`, `prompt`, `session` or package provider. See [where a public
  widget comes from](where-a-public-widget-comes-from.md).

## What you approve

The card shows what will run, where and how often. What you approve is
that exact source: its kind, the command or script and its arguments, the
target, the harness, its permission mode and prompt, the session. The app
keeps a fingerprint of it (a SHA-256 of the source written in a fixed
order). Change any of those — even one argument — and the fingerprint
changes, so the widget asks again. Moving or resizing the widget changes nothing that runs, and
does not ask.

## Where the answer is kept

Approvals live in `~/Library/Application Support/Devmachine/widget-approvals.json`,
outside your configuration folder. If they lived next to the board, the
same agent that wrote a widget could write its approval too, and the
question would protect nothing.

**Deny** pauses the widget; it stays on the board, so you can change your
mind from its card. To get rid of it, remove it.

## Why widgets on a machine go through the CLI

A widget whose target is a machine or a workspace runs its command through
`devmachine run`, the same door you use. The app never opens an SSH
connection of its own: the CLI already knows the address, the account,
the key and the host key, and reuses one connection for five minutes, so
a widget polling every few seconds costs no new handshake. These runs use
`--no-log`, so they do not fill [the command log](../reference/commands.md#the-command-log).
