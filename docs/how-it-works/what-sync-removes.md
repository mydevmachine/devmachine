# What sync removes

`sync` only removes a file it recorded writing. Never a guess, never a
whole directory swept clean — one path, remembered, removed once the
plan stops writing it.

## Two kinds of file, one rule

A workspace's routes and a package's `extends` both write into `sites.d`,
and `sync` removes both the same way: it keeps a list of exactly what it
wrote, and takes away whatever is no longer on that list.

- **Routes** come from `expose`. See [why a published app lives in the
  configuration](published-sites.md).
- **Extension files** come from `extends: caddy.sites.d: <file>`, a
  package's way of adding to a place another package opened. `sync`
  records every extension file's path in the lock, per machine, and
  removes one once the package that wrote it drops out of the plan.

## Why not a glob

`sites.d` can hold files nothing in the configuration wrote — one dropped
in by hand, one from before this feature existed. A glob over the
directory would delete those too, so `sync` only removes a path it
remembers writing itself.

## The first-run caveat

The list of extension paths lives in the lock. A lock from before this
feature has no such list, so the first sync after upgrading removes
nothing — there is nothing yet to compare against. A file left by a
package removed *before* this version never enters the tracked list
either, so no later sync catches it. Remove it by hand with `devmachine
run`; see [Troubleshooting](../troubleshooting.md).
