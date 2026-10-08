# One sync per machine

Only one `sync` runs on a machine at a time. A second one, from another
terminal, another computer, or the app's Prepare button, stops at once
with:

```
another sync is running on main: wait for it to finish, then run this again
```

It changes nothing on the machine. Run it again when the first one ends.

## Why

Every sync empties the bundle directory (`/opt/devmachine`, or
`~/.local/share/devmachine/bundle` on your own computer) and sends its own
copy before it starts the play. Ansible reads some files from that
directory while the play runs, such as a file a `copy` task sends. Two
syncs at once would delete each other's files halfway through, and the
first one would fail on a file that was there when it started.

A dry run (`sync --check`) takes the lock too: it sends the bundle the
same way.

## How the lock works

The lock is a directory next to the bundle: `/opt/devmachine.lock`, or
`bundle.lock` on your own computer. Making a directory either works or
fails in one step, on Linux and on a Mac alike. A Mac has no `flock`
command, so the lock does not use it.

A sync makes the lock directory before it touches the bundle, and keeps
an SSH session open on the machine for the whole run. When the sync
ends, the session closes and removes the lock. If the connection drops,
or you stop the sync with Ctrl-C, the session ends and removes the lock
the same way.

The lock records the process that holds it and the machine's boot. If a
sync dies with no chance to clean up, or the machine reboots in the
middle, the next sync sees that nobody holds the lock any more and takes
it over. You never have to remove it by hand.

The lock waits for nothing: a second sync refuses instead of waiting, so
a stuck run never leaves a queue of runs behind it.
