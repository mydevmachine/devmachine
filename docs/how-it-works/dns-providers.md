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

### `list` shows A, AAAA, CNAME and TXT only

Other types in the zone are left out of `list`. `zones` lists only the
account's `ACTIVE` domains.

### An account GoDaddy refuses answers 403 `ACCESS_DENIED`

GoDaddy lets only some accounts call the Domains API: those that hold a
domain, or a plan with management access. Any other account gets `403
ACCESS_DENIED` on every call, whatever the token. The provider reports it
as `forbidden`, with GoDaddy's `code` and `message`. See
[troubleshooting](../troubleshooting.md).

### 60 requests a minute per credential

A `429` is `rate_limited`, and the provider does not retry it. The
package has no switch for GoDaddy's test (OTE) environment.

## No provider retries a rate limit

A `429` is reported as `rate_limited` and the provider stops. Retrying
inside the entrypoint would hide how throttled the registrar really is,
and could turn one throttled request into a silent hang. Cloudflare
blocks a token for five minutes after a single `429`, so retrying would
not even help.
