# Why nothing is embedded in the binary

The CLI ships with no packages built in. Every package — even the ones
every machine gets — is fetched from a pinned release of
`github.com/mydevmachine/packages`, checked against a checksum, and
cached.

## What that buys

**Fixing how Docker is installed is a push, not a release.** Nobody
upgrades a binary to receive it, and the fix reaches every machine on the
next `sync`.

**The CLI stays small enough to reason about.** It knows how to fetch a
package, send it to a machine and run it — it does not know what Docker
is, and never needs to.

**One mechanism instead of two.** A package you wrote and one somebody
published are the same kind of thing, resolved by the same code — no
"built-in" set with different rules to learn.

## What it costs, plainly

**A machine cannot be set up for the first time while GitHub is
unreachable.** There is nothing embedded to fall back on:

```
error: fetching https://github.com/mydevmachine/packages/releases/download/v1/packages-v1.tar.gz: ...
```

Once a release is in the cache this stops mattering — the packages are on
your disk, and bringing a machine up to date needs no network at all. The
limit is only the **first** fetch of a **new** pinned version, not every
run, and it is worth the simplicity of one mechanism.

## Why the checksum is on a release asset

The CLI fetches `packages-v1.tar.gz` from the release, not the tarball
GitHub generates for the tag — a tag can be moved, and its generated
archive changes with it silently. A release asset is uploaded once, with
its checksum published beside it, so `packages.lock` can record what was
actually installed and refuse content that changed. Without that,
"pinned" would be a word rather than a guarantee.

## Why only one source

A package runs as root on a machine you own. Fetching one from anywhere
and running it is the shape of a supply-chain attack, so a third-party
source is not supported. Your own configuration folder is the
exception, and not really one: a package you wrote is a package you
already trust.
