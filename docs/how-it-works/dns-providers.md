# DNS providers

Every fact here exists so you learn it from this page, not by damaging a
zone. Read it before you touch a real one.

## A provider is a package, not a built-in vendor

Talking to Hostinger, Cloudflare or GoDaddy is an ordinary package: `devmachine
packages add hostinger`, run on the machine, never on your computer.
There is no vendor list inside the CLI binary.

That gives one safety property: **a provider nobody installed cannot be
written to.** No code path reaches a registrar's write API unless its
package is on the lock file for that machine. Adding a provider is a
decision with a diff.

## What the provider gets from the shell

`PATH`, `HOME`, and the one token its own credential declares — nothing
else. The CLI sources `/etc/devmachine/<credential>/env` right before
running the entrypoint, so nothing else leaks in. A provider that reads a
variable its credential does not declare works by accident on one
machine and breaks everywhere else.

The token never appears in a command argument, where `ps` would show it
to every account on the machine. Only the credential's name does, and
that name is not a secret.

## One value string, whatever the registrar keeps

An MX record is a priority and a host. Hostinger stores them as one
string, `10 mail.example.com.`; Cloudflare keeps the priority in a field
of its own; GoDaddy keeps an SRV's service, protocol, priority, weight
and port in five fields. The CLI's contract has one `value` string, so
the same `dns add` works on all three and `dns list` prints the same
thing everywhere: the zone-file spelling, `10 mail.example.com`. The
provider is where the split and the join happen, because the provider is
the only part that knows its registrar's shape. The CLI checks the
grammar first (see [DNS](../concepts/dns.md)), so a provider never has
to guess at a value that does not fit.

## `append` is a separate command, not a flag on `upsert`

`upsert` means "exactly this one value". A flag that quietly changed it
to "this one too" would make the safe command and the additive one
differ by one word nobody reads. `append` is its own entry in the
provider's `commands`, so a package from before it exists simply does not
list it — and the CLI refuses `--append` for that provider before running
anything, instead of replacing the other values or failing on the
machine with "unknown command".

## Hostinger

### RRsets, not records

Hostinger's API has no idea of a single record. It has **RRsets**: one
entry per `(name, type)`, holding a list of values. `www A` is one RRset
that can carry several addresses at once. `list` splits each RRset into
one entry per value, but every write has to work in RRset terms.

### `overwrite: false` appends, and the default is `true`

Hostinger's `PUT` takes a whole RRset and an `overwrite` flag. With
`false`, the request's values are **added** to what is already there —
pointing an existing name at a new address this way leaves both, a
round-robin nobody asked for. Leave the field out and Hostinger's own
default is `overwrite: true`, the destructive mode, so the provider
always sends it explicitly, decided by the code, never left to
Hostinger's default.

### `append` is one `PUT` with `overwrite: false`

Adding a value is the one write Hostinger makes easy: `overwrite: false`
appends the request's values to the RRset and sets its TTL. The provider
sends the RRset's own TTL back when none was asked for, so appending a
value never changes how long the others are cached.

### Every host comes back with a trailing dot

Hostinger stores `mail.example.com.` for the `mail.example.com` it was
given, in CNAME, NS, MX and SRV content alike. The provider drops the dot
and lower-cases the host in `list` and in every comparison, so a record
is seen as already right instead of rewritten on every `dns add`.

### Replacing a value: delete, then append — not one call

`upsert` must make a name hold **exactly** the new value. Hostinger gives
two ways: one `PUT` with `overwrite: true`, or a `DELETE` of the RRset
then a `PUT` with `overwrite: false`. The provider uses the second, at
the cost of two calls: Hostinger's own docs say `overwrite: true`
replaces every record in the payload, so a one-RRset `PUT` would empty
the rest of the zone. The cost of the two-call path is real — **the name
holds nothing for the gap between them**, so a resolver asking in that
window gets NXDOMAIN — but that beats a write whose blast radius is the
whole zone.

### Why `expose rm` will not take one value out of several

The same two-call path is why `expose rm` and `workspaces destroy` leave
a name alone when it holds the machine's address **and** another one:
taking one value out means deleting the RRset and writing the rest back,
and a failure between the two calls would take down the value somebody
else added. The command prints the record to remove by hand instead,
and reports it in `dns_error`.

Removing several names at once — `workspaces destroy` with many sites —
asks each machine for its providers and each provider for its zones
once, and lists each zone once, not once per name.

### A DNS-only token needs its zones configured

Hostinger's DNS API answers about one zone at a time and has no
list-zones endpoint. Provider selection normally reads the account's
domains from the Domains portfolio endpoint, but a least-privilege DNS
token gets a 403 there even though it can read its own zone. Set
`hostinger.zones` on the machine for that case:

```
devmachine machines edit main --set hostinger.zones=[example.com]
```

### A delete filter takes the whole RRset

Hostinger's `DELETE` removes an entire `(name, type)`, not one value
inside it. Deleting one value out of several means: read the RRset,
delete it whole, then `PUT` back the values you meant to keep.

### Writes are asynchronous

A successful `PUT` or `DELETE` means "request accepted," not "done." A
`list` called right after can still show the old state — which is why
the provider never checks its own writes immediately. Give it a moment
before checking with `dns status` or `dns list`.

## Cloudflare

### `PUT` clears every field it was not given

Cloudflare's DNS record `PUT` replaces the whole record: send only
`content` and `ttl`, and `proxied`, `comment` and `tags` reset to
defaults. The provider always uses `PATCH` instead, which touches only
the fields in the request.

### No upsert: every write past a create needs an id

Cloudflare has no "make this name hold this value" call. Creating a
record returns an id; changing or deleting one needs that id, found by
listing first. So `upsert` always lists before it writes: create on no
match, `PATCH` by id on one match, refuse to guess on more than one
(`ambiguous`).

### An invisible zone is a 200, not a 404

Ask Cloudflare for a zone the token cannot see, and the answer is `200`
with an empty `result` array, not an error. The provider treats an empty
result as `zone_not_found` itself.

### MX, SRV and CAA are fields, not strings

Cloudflare's record has `priority` beside `content` for MX, and a `data`
object (`priority`, `weight`, `port`, `target` for SRV; `flags`, `tag`,
`value` for CAA) in place of `content`. The provider builds the value
`list` prints from those fields and splits the value `upsert` receives
back into them; the quotes around a CAA value belong to the zone-file
spelling and are not sent.

### `append` is a create

Every value at a Cloudflare name is its own record with its own id, so
adding one is a `POST` and the others are never touched. A value that
is already there is found by comparing the joined value, not the raw
`content`, so `10 Mail.Example.com.` matches the record Cloudflare holds.

### `proxied` is always `false`

Certificates on this machine renew through a challenge that needs the
request to reach the machine directly. A proxied record answers from
Cloudflare's edge instead, and the challenge fails.

## GoDaddy

### A Personal Access Token, not a key and secret

The provider sends the token as `Authorization: Bearer`. GoDaddy's older
key and secret pair (`sso-key`) is deprecated for domains, and the
provider does not support it.

### `PUT` on a type and name replaces that set

`PUT /v1/domains/{zone}/records/{type}/{name}` swaps every value at that
type and name for the ones you send. That is exactly what `upsert`
means, so it is one call. Deleting one value among several reads the set
and sends the rest with a `PUT`. Deleting the last value, or the whole
set, is a `DELETE`.

### An SRV is five fields and a split name

GoDaddy stores an SRV's `service`, `protocol`, `priority`, `weight` and
`port` apart from `data` (the target) and `name` (the host under the
zone, `@` for the apex). The CLI writes the owner the way a zone file
does, `_sip._tcp.voice`, and the provider splits it into `_sip`, `_tcp`
and `voice`. The `PUT` on `records/SRV/voice` replaces **every** SRV at
`voice`, whatever its service, so the provider writes the other
services back untouched on every `upsert`, `append` and `delete`.

An MX is `data` plus a `priority` field. The provider keeps every field
of the values it writes back — a set written back without its priorities
would turn each mail server into an unreachable one.

### `append` writes the set back whole

GoDaddy has no "add one value": the set at a type and name is written
whole. `append` reads it, adds the new value and `PUT`s all of them. A
value already there is a no-op.

`list` shows A, AAAA, CNAME, TXT, MX, SRV, NS and CAA; `zones` lists only
the account's `ACTIVE` domains.

### An account GoDaddy refuses answers 403 `ACCESS_DENIED`

GoDaddy lets only some accounts call the Domains API: those that hold a
domain, or a plan with management access. Any other account gets `403
ACCESS_DENIED` on every call, whatever the token. The provider reports it
as `forbidden`, with GoDaddy's `code` and `message`. See
[troubleshooting](../troubleshooting.md).

A `404` on a domain path means the account does not hold that zone. The
provider reports it as `zone_not_found`.

### 60 requests a minute per credential

A `429` is `rate_limited`, and the provider does not retry it. The
package has no switch for GoDaddy's test (OTE) environment.

## No provider retries a rate limit

A `429` is reported as `rate_limited` and the provider stops. Retrying
inside the entrypoint would hide how throttled the registrar really is,
and could turn one throttled request into a silent hang. Cloudflare
blocks a token for five minutes after a single `429`, so retrying would
not even help.
