# A prompt widget's permission mode

A prompt widget asks a coding harness a question and shows the answer.
By default the harness answers without using tools, so a widget asked to
search the web says it has no permission to. `permission_mode` lets the
widget use them. This page says why it looks the way it does.

## The harness's own names

The values are the names Claude and Codex use themselves: Claude's
`--permission-mode` choices, and Codex's `--sandbox` values and two of its
flags. The CLI invents none of them, so what you read in a harness's own
help is what you write in the widget, and what the app passes on.

The lists live in the engine contract (`devmachine widgets schema`).
When a harness release adds a mode, a CLI release adds it to the list;
until then the CLI refuses the new name, the same way it refuses a typo.

## No mode means today's behaviour

A widget without `permission_mode` runs exactly the command it ran
before engine 1.6. Nothing about an existing widget changes.

## Why a mode with no checks never runs on a timer

`bypassPermissions`, `danger-full-access` and
`dangerously-bypass-approvals-and-sandbox` let the harness change files
and run commands without asking anyone. A widget on a timer runs while
you are away, so these modes take only `every: manual`: each run happens
because you pressed refresh. One rule with no exceptions is easy to
check, and easy to relax later if it proves too strict.

The contract marks these modes as `dangerous`, so the CLI, the app and
an agent all read the same list.

## What you approve

A prompt widget written in a board waits for **Allow**. The mode is part
of what you approve: change it and the widget asks again, and a mode with
no checks shows a red warning on the card. See [why a widget an agent
wrote waits for you](why-an-agent-written-widget-waits.md).

## Your login stays the same

A mode changes what the harness may do, not who runs it. It does not
change the account or the home folder, so the harness keeps the login it
already has on that computer, machine or workspace.
