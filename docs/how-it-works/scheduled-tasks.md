# Scheduled tasks

`devmachine scheduled` runs a shell command, or a prompt for a coding agent,
in a folder of a workspace, on a schedule: once, every hour, every day, on
weekdays, every week or every month. The macOS app shows the same tasks on
its Scheduled page. For examples and how to set one up, see the
[scheduled tasks guide](../guides/scheduled-tasks.md).

## Each workspace runs its own tasks

A task lives in the workspace that runs it, under
`~/.devmachine/scheduled/`. Nothing on your computer has to be awake when it
runs, and no machine reaches into another machine to start it.

The other way, one machine that starts every task everywhere, needs that
machine to hold a login to every other workspace. Today only your computer
holds those logins, and that is on purpose. The list still reads as one,
because `devmachine scheduled list` asks every workspace at once, the way
`devmachine sessions` does.

## One timer, tasks in files

The `scheduled` package installs one timer per workspace that fires every
minute and runs `devmachine scheduled local tick`. On Linux it is a user
systemd timer, with linger so it runs with nobody logged in. On macOS it is a
launch daemon that runs as the workspace's account.

Each tick reads the task files and starts what became due since the last
tick. Creating, changing or pausing a task only writes a file: no root, no
reload, and the same on both systems. One timer per task would need root on
macOS for every change.

## The workspace runs the same devmachine

The package puts the devmachine binary in the workspace and links it at
`~/.devmachine/scheduled/devmachine`. The timer and your computer both call
that path. Deciding when a task is due, building the agent's command and
keeping the record of each run is the same Go code you run on your computer.

Your computer reaches it as `sessions` does: it logs in to the workspace and
sends a short program to `/bin/sh` on its input, so the account's own shell,
which may be fish, never reads a task's text.

## A setup that did not finish

The first task in a workspace adds the `scheduled` package to it and syncs
its machine. If that sync fails, the package stays in the configuration and
the binary is not in the workspace.

It stays on purpose. Taking it off would undo the one part that worked, and
the next attempt would add it again anyway. So the CLI never trusts the
configuration alone. Before each call it checks the binary is there. When it
is not, but the package is in the configuration, the error says
`setup-incomplete` and names the sync that finishes it:
`devmachine sync --machine <m> --tags scheduled`. `scheduled add` runs that
sync itself, with the same `--yes` rule as the first setup, and then creates
the task. `scheduled list` shows the workspace as not installed, with the
same error, so the app can offer to finish the setup.

When the setup sync fails, the error names the Ansible task that failed and
what it said. "1 task(s) failed" alone leaves you reading the whole run.

## A task runs as the workspace's account

The task runs in the folder you chose, as that account, through its login
shell, so the tools `mise` or Homebrew put on its PATH are there. A prompt
starts with that account's Claude or Codex login, skills, MCP servers and
connectors. A prompt that says "email me the summary" works when that
account has an email connector, and does nothing more than the account can
do.

The prompt reaches the agent as one argument. No shell reads it, so quotes,
`$(...)` and new lines arrive as you wrote them. A command, on the other
hand, is a shell line, and the shell reads it as one.

## Permission modes

A prompt runs with nobody there to approve a tool. A mode that asks before
it acts, such as Claude's `manual`, stops at the first tool it needs and the
run ends with nothing done. `acceptEdits` approves file edits only, so a call
to a connector such as Linear or email is refused; `auto` lets the agent use
its tools and connectors, and is what the app picks. A mode
that never asks at all, `bypassPermissions` for Claude or
`danger-full-access` for Codex, needs `--allow-dangerous`, so it is always a
choice somebody made.

`devmachine scheduled harnesses` lists each agent's modes and suggested
models.

## A missed run is recorded, not run

If the machine was off or asleep when a task was due, the next tick records
one `missed` run and does not start it. Work that runs hours late tends to
surprise more than it helps. A tick starts a task only when its time was in
the last two minutes.

A one-off task that was missed is done, like one that ran.

## No two runs of a task at once

A run holds a lock while it goes. If the next time comes while it still runs,
that time is recorded as `skipped`. A lock left by a run that died, after a
reboot for example, is taken over.

## Each run is kept

A run records how it started (`schedule` or `manual`), when, how long it
took, its exit code and how it ended: `succeeded`, `failed`, `timeout`,
`error`, `missed` or `skipped`. Its output, stdout and stderr together, is
kept up to the last mebibyte, where the error usually is. The last 50 runs of
each task are kept.

A run stops after its timeout, 30 minutes unless the task says otherwise, at
most 6 hours. Everything it started stops with it.

## Run now

`devmachine scheduled run` starts the task in the workspace and returns at
once with the run's id. The run goes on after the command returns, and after
you close the connection. Its schedule does not change, and a one-off task
run this way still runs at its time.

## Timezone

A task keeps the timezone it was created in, by default your computer's. 09:00
in `America/Sao_Paulo` runs at 09:00 there, whatever the server's own clock
says, and across daylight saving changes. A monthly task on day 31 runs on the
last day of shorter months.
