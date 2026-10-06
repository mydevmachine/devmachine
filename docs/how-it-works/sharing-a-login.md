# Sharing a login

One GitHub account usually serves every workspace on a machine. Logging
into it six times is six chances to end up on six different accounts, so
the login happens once and the result is copied.

**The CLI generates the copying** — a package never writes it. The CLI is
the only thing that knows both halves: a package says where its tool
keeps a session (`stored_at`) and whether a copy works elsewhere
(`shareable`); the configuration says which workspaces want the shared
one. Put the copying in a package, and every shareable tool grows its own
copy task, written again and slightly differently each time.

## What runs

For each login that resolves to `machine` scope, `sync` generates two
tasks:

1. Look for the master copy under `/etc/devmachine/<name>/`.
2. For each workspace that wants it, copy the master to `stored_at`, with
   mode `0600`, making the directories on the way.

Both skip when the master is not there. Running `sync` before anybody has
run `devmachine login` is normal, and the next run picks the session up.

`stored_at` has to start with `~/`. The copy lands in each workspace's own
home, so a path anywhere else has nowhere to go, and `sync` refuses it.

## Why the copy runs as the workspace, not as root

The rest of `sync` runs as root, and root could copy the file and then
give it to the account. The copy does not do that, because the workspace
account owns its home. It can replace `~/.ssh` or `~/.config/gh/hosts.yml`
with a link to `/etc` or `/etc/shadow`. Root would follow that link, and
write, `chmod` or `chown` a file outside the home.

So root only opens the master, which is its own file, and hands the bytes
to a shell that runs as the account (`runuser -u <account>` on Linux,
`sudo -u <account>` on a Mac, which has no `runuser`). That shell
makes the directories, writes a temporary file, and renames it into place.
It has only the account's own rights. A link can then reach only what the
account could already write, and there is no gap between a check and a
write for the account to slip a link into.

Before it writes, the shell also resolves the path. When a directory on
the way, or the file itself, is a link out of the home, it stops with
"reaches outside the home of <account> through a symbolic link". That
check is there for the message, not for the safety: see
[Troubleshooting](../troubleshooting.md).

Because the account makes the directories, they belong to it. The
directory that holds the login is set to `0700`; the others keep the mode
they had.

A copy that is already in place, with the same content and mode `0600`,
is left alone, and `sync` reports no change for it.

## The one that matters: opting out

A workspace that says `gh: own` is **not in the loop at all**.

```yaml
credentials:
  gh: machine

workspaces:
  - name: acme
  - name: bob
    credentials:
      gh: own
```

The copy overwrites `stored_at`. A shared login copied into bob's home
would replace the account he logged in with, with nothing saying why —
so the generated loop carries acme and nobody else, and bob's name
never appears near it.

## What beats what

| Order | Where |
| --- | --- |
| 1 | the workspace's `credentials:` |
| 2 | the configuration's `credentials:` |
| 3 | the package's own `scope:` |

A package that says `shareable: false` beats all three: asking for
`machine` on one is refused by name, because the copy would land, the
tool would reject it, and nothing would say why. A package that has not
said `shareable: true` is treated as one that does not.

Where each credential lives, and how a non-login value is delivered, is
in [Configuration](../concepts/configuration.md#credentials).
