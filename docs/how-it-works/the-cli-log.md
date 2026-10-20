# The CLI's log

The CLI keeps its own log at `<config>/logs/devmachine.log`.
`devmachine logs path` prints where that is. When `setup`, `sync` or
another command that changes a machine fails, the last line it prints
says where too:

```
error: the bootstrap stopped at "install ansible"
log: /home/alice/.config/devmachine/logs/devmachine.log
```

That file is what to send when you ask for help.

## What goes in it

A command that changes a machine is logged whole: `setup`, `machines
add`, `sync`, `update`, `doctor`, `packages add` and `remove`, `run`,
`login`, `credentials push`, and the others that add, remove or start
something. Each run writes a start line, every line the command printed
(Ansible's output included), and an end line:

```
2026-10-20T12:00:00.000Z [4242] start  devmachine 0.16.0 darwin/arm64: sync --machine main
2026-10-20T12:00:03.120Z [4242] out    TASK [base : install packages] on main (203.0.113.10)
2026-10-20T12:00:41.900Z [4242] err    fatal: [main]: FAILED! => {"msg": "no package matching 'foo'"}
2026-10-20T12:00:42.010Z [4242] end    failed (exit 1) after 42.01s: sync on main failed
```

The number in brackets is the process. The macOS app can run several
commands at once, and their lines mix in the file; the number tells them
apart.

Every other command only writes a line when it fails, and never its
output. The app runs `sessions`, `stats`, `machines list` and `run
--package devmachine-app` every few seconds. Their output, logged whole,
would fill 5 MB in a day and push out the `setup` you wanted to read.
`run --no-log` counts as one of these reads.

A command that only reads does not print the `log:` line either. The app
reads what those commands print, and a line it did not expect is a line
it has to skip.

## What is taken out

Before a line reaches the file, the CLI replaces with `***`:

- the value of any `NAME=value` or `"name": "value"` whose name holds
  `token`, `secret`, `password`, `passwd`, `api_key`, `private_key` or
  `auth`
- a line shaped like a `.env` file, `NAME=value` in capitals
- `Authorization:` headers and `Bearer` tokens
- GitHub tokens (`ghp_`, `gho_`, `github_pat_`), keys starting with
  `sk-`, and Tailscale auth keys (`tskey-`)
- the password in an address like `https://alice:…@example.com`
- a private key, from its `-----BEGIN … PRIVATE KEY-----` line to its
  end
- on the command line, the value of a flag like `--token`, `--password`
  or `--value`, and the value given to `secrets set`

A password typed at a prompt or piped with `--password-stdin` never
reaches the log at all: the log copies what the CLI prints, not what it
reads.

What stays is what makes the log useful: addresses, host names, machine,
workspace and user names, and the commands that ran. Read the file
before you send it if any of those are private to you.

## Size

When the file passes 5 MB, it becomes `devmachine.log.1`, the old `.1`
becomes `.2`, `.2` becomes `.3`, and the old `.3` is deleted. So the
logs never take more than about 20 MB.

## Why it never fails a command

Writing the log can go wrong: a full disk, a folder you made read-only.
The CLI then carries on without it, and says nothing. A log that can
stop `sync` is a log somebody turns off the first time it does.

## Why it is never committed

`logs/` holds a `.gitignore` of its own that ignores everything in it.
A configuration folder that became a repository before the log existed
has a `.gitignore` without `logs/`, and its next commit would have
picked the log up. With its own `.gitignore`, the folder stays out
whatever the top one says. `devmachine setup git` also adds `logs/` to
the top one, and refuses a commit that tracks it, the same as
`history.log`. See
[versioning your configuration](versioning-your-configuration.md).
