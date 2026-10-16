---
description: "Have Claude, Codex or a plain command do recurring work in a workspace on a schedule: a monthly briefing, your standup, release notes, a nightly backup."
category: Agents
level: Beginner
needs:
  - "A workspace on a machine reached over SSH"
  - "claude-code or codex in that workspace, for a prompt"
related:
  - claude-that-never-sleeps.md
  - keep-sessions-running.md
  - one-consultant-three-startups.md
---
# Scheduled tasks: work that runs on its own

Write a prompt once, say when, and a coding agent does the work in your
workspace on that schedule: on the 1st of every month it briefs you on last
month, every weekday morning it writes your standup, every Friday it drafts
the release notes. A plain shell command works too. Each run keeps its status
and its output, and your computer can be off the whole time.

**You need:** a workspace, `acme` here, on a machine devmachine reaches over
SSH — see [Getting started](../getting-started.md). For a prompt,
`claude-code` or `codex` in that workspace, signed in once.

## Why run it in your own workspace

A scheduled task runs where you chose, with what is already there.

- **Your setup, as it is.** The task runs as the workspace's account, in the
  folder you pick, through its login shell. The repository, its tools from
  `mise` or Homebrew, its `.env`, its database: all where the task expects
  them. Nothing to rebuild in a cloud runner.
- **Your agent's connectors and skills.** A prompt starts with that account's
  Claude or Codex login, its skills and its MCP servers. If Linear, Gmail or
  Slack is connected there, "email me the summary" just works. No token is
  copied anywhere else.
- **The machine you pick.** Put it on an always-on VPS and it runs with your
  laptop closed. Put it on the machine next to the data, or the one with the
  GPU. Each workspace keeps and runs its own tasks.
- **Kept apart.** A task in `acme` sees only `acme`'s account: its files, its
  logins. A client's task never reaches another client's workspace — see
  [one consultant, three startups](one-consultant-three-startups.md).
- **Any agent, your choice of model.** Claude or Codex today, with the model
  and the permission mode you pick per task; or no agent at all, just a
  command.
- **Nothing new to pay for or trust.** It is a timer on your machine and the
  devmachine you already run. The record of every run stays in the
  workspace.

## From the app

1. Open **Scheduled**, the clock at the bottom of the sidebar.
2. **New task**: name it, pick the workspace and the folder.
3. Choose **Prompt**, write it, and pick the agent, its permission mode and
   the model. Or choose **Command** and write the command.
4. Pick when: once, hourly, daily, weekdays, weekly or monthly, and the time.
5. **Create**. The first task in a workspace sets it up for you: it adds the
   `scheduled` package and syncs the machine, which takes a minute or two.
6. Press **Run now** in the task's menu to try it. Click the task to see its
   runs; click a run to read what it printed.

## By hand

```
devmachine scheduled add --workspace acme --name "Monthly briefing" \
  --folder ~/acme-web --harness claude --model opus --permission-mode auto \
  --prompt "Brief me on last month in acme: what shipped, the pull requests still open, Sentry's new errors and the deploys. Keep it to one page and email it to me." \
  --every monthly --day 1 --at 09:00 --yes
```

`--yes` sets up the `scheduled` package on `acme` if it has none. Then:

```
devmachine scheduled list
devmachine scheduled run <id> --workspace acme      # try it now
devmachine scheduled runs <id> --workspace acme     # when it ran, how it ended
devmachine scheduled log <id> <run-id> --workspace acme
```

Every flag is in [the command reference](../reference/commands.md#scheduled).

## Ask your agent instead

With [devmachine's skills](../concepts/agent-skills.md) in any Claude Code
or Codex session:

```
Schedule a monthly briefing on acme: on the 1st at 9:00, Claude reads last
month's merged pull requests, open issues and Sentry errors in ~/acme-web
and emails me a one-page summary.
```

## Ideas to start from

| When | What it runs |
| --- | --- |
| 1st of the month, 09:00 | Brief me on last month in acme: what shipped, the pull requests still open, Sentry's new errors and the deploys. Email it to me. |
| Weekdays, 08:30 | Read yesterday's commits and Linear updates and write my standup in `standup.md`. |
| Fridays, 16:00 | Draft release notes from this week's merged pull requests, grouped by area, and open them as a pull request. |
| Daily, 07:00 | Read Sentry's new issues from the last day, open a Linear ticket for each one that is not a duplicate, and post the list in #eng. |
| Mondays, 07:00 (command) | `npm outdated && npm audit --omit=dev` |
| Daily, 02:00 (command) | `./scripts/backup.sh --to s3://beta-backups` |
| Sundays, 20:00 | Delete the local and remote branches already merged into main, and tell me which. |
| Once, a date you pick | Check that the certificate for shop.example.com renews and the site answers. |

## Good to know

- **Pick `auto` for a prompt that uses a connector.** Nobody is there to
  approve a tool. `acceptEdits` approves file edits only, so a call to Linear
  or email is refused. A mode that never asks at all, such as
  `bypassPermissions`, needs your explicit yes.
- **Times are in your timezone**, the one your computer had when you created
  the task, whatever the server's clock says.
- **A run the machine was off for is recorded as missed, not run late.** If
  the next time comes while a run still goes, it is skipped.
- **A run stops after 30 minutes** unless the task says longer (up to 6 h).

How it works, and why it is built this way, is in
[scheduled tasks](../how-it-works/scheduled-tasks.md). An error you meet is
explained in [troubleshooting](../troubleshooting.md).
