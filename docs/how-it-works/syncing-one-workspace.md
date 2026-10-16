# Syncing one workspace

`devmachine sync --workspace <name>` applies one workspace and nothing
else. It finds the machine the workspace lives on, so `--machine` is not
needed. On a machine with many workspaces, a full sync runs every package
of every workspace; this runs only the packages of the one you name.

```
devmachine sync --workspace acme
devmachine sync --workspace acme --check
devmachine sync --workspace acme --tags claude-code
```

## What it runs

- The workspace's own packages, with its own settings.
- The skills its packages bring, and the shared logins it asked for.
- The files its packages add to another package's folder (`extends`).
- Its routes file in Caddy's `sites.d`, and a Caddy reload when that file
  changed.

## What it leaves alone

- **The machine's own packages.** They do not run. A workspace package
  that relies on one assumes it is already there, as it is after any
  full sync.
- **Every other workspace.** Its packages, its routes file and its
  extension files are not touched, and not even sent.
- **Removals of extension files.** A full sync removes the file a package
  left behind when it left the plan. A one-workspace sync does not: see
  below.

The host variables still list every workspace on the machine
(`devmachine_workspaces`). Some machine packages read that list, and a
package that ran with half of it would act on half of the machine.

## Why it removes no extension file

An extension file's name starts with its workspace, so `acme`'s files
look like `acme-<package>-<file>`. But a workspace named `acme-api`
writes `acme-api-<package>-<file>`, which starts with `acme-` too. A
narrowed sync could only tell its own old files from another
workspace's by guessing, and a wrong guess deletes somebody else's site.

So the narrowed sync writes, and never removes. The lock keeps every
path it already knew, so the next full `sync` still removes a file that
is no longer wanted. See [what sync removes](what-sync-removes.md).

## What the lock records

Only the workspace's entry in `packages.lock` is replaced. The machine's
entry, and every other workspace's, stay as the last sync left them. With
`--tags`, only the named packages of that workspace are recorded, as for
a tagged full sync. A dry run (`--check`) writes nothing.

The lock's format does not change, so an older CLI reads a lock a
one-workspace sync wrote, and its next full sync brings the rest of the
machine up to date.

## When to run a full sync instead

- After changing a machine package, or a machine's settings.
- After removing a package that added a file to another's folder, to have
  that file removed.
- On a new machine, where the machine packages have never run.
