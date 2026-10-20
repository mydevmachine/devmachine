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

## A login waiting for your approval

After a wake, 1Password asks for Touch ID (or to be unlocked) before its SSH
agent signs with your key. `sessions` used to wait 30 seconds for that, then
kill the login and call the machine `unreachable`. Killing it closed the
prompt, counted as a failed login in the machine's SSH log, and the next poll
knocked again.

Now `sessions` opens the shared connection first, once per machine and Linux
user, not once per workspace. When the machine has accepted the key and only
the agent's signature is missing, it answers `login-waiting` within a few
seconds and leaves that login running on its own, with the prompt still up.
Every `sessions` call while it waits answers `login-waiting` without touching
the network. Once you approve, the connection is up and the next call lists
the sessions through it.

The machine ends a login nobody approves after its `LoginGraceTime`, two
minutes by default; the CLI ends one left longer than three. If you refuse the
prompt, the next call answers `login-refused` once, and does not ask again on
its own.

The CLI tells the two waits apart from ssh's own log: the line `Server
accepts key` comes after the machine answered and before the agent signs.
`sessions` still waits up to 30 seconds for a slow machine.

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

## The list does not depend on the locale

The command reads tmux's list with fields split by tabs. tmux prints a tab as
`_` when the login has no UTF-8 locale, and an ssh login to a Mac sets none.
So every tmux call asks for UTF-8 with `tmux -u`, whatever the locale. Without
it, a Mac's workspace read as having no sessions at all.
