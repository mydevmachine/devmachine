# Session indicators

`devmachine sessions` lists the tmux sessions of each workspace.
For each session it says whether the agent in it is busy and whether it wants you.
Both are best guesses from what tmux shows, not facts from the agent.

## Why the CLI reads the sessions

The reading lives in one place, in Go. The macOS app and the Linux app both
call `devmachine sessions` and show the answer, so they cannot disagree.

The command uses the multiplexed SSH connection that `run` uses. The
connection stays open, so asking every 5 seconds costs one small command
each time, not a new login.

## After the computer sleeps

The shared connection can outlive a sleep or a change of network. Its TCP
link is dead, but nothing closes it, so every command sent through it waits.
Before, that wait lasted minutes. A new login, and the SSH agent's prompt
that comes with it, only happened once the system finally gave up on the
link.

Now ssh checks the link every 3 seconds and drops it after three missed
answers, about 10 seconds. A shorter limit would also cut a long `sync` on
a network that drops for a few seconds. The next command logs in again on a fresh connection. A machine
that does not answer at all fails after 5 seconds.

`sessions` waits up to 30 seconds for each workspace. A login that needs your
approval, as 1Password asks after a wake, has time to get it.

## Busy is a guess

A session is busy when its screen changed in the last 5 seconds. The CLI reads
the time of the last change from tmux.

An agent that is thinking and draws nothing shows as idle. A plain terminal that
prints a log shows as busy. When no agent runs in the session, any program
other than a shell counts as busy.

## Attention needs the bell

A session needs attention when tmux holds a bell mark in one of its windows.
The mark appears only when the agent rings the terminal bell.

Claude Code does not ring it by default. To turn it on, set
`"preferredNotifChannel": "terminal_bell"` in `~/.claude.json`, or run
`claude config set --global preferredNotifChannel terminal_bell`.

tmux clears the mark when you open the window.

## Times come from the machine

Age and busy use the clock of the machine that runs tmux, not the clock of your
computer. The command reads that clock in the same call as the sessions. A
computer whose clock is wrong still shows the right ages.
