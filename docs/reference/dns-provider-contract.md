# The DNS provider contract

What a DNS provider's entrypoint is called with, and what it must answer
back. Follow this page and the CLI works with your provider on the first
try; guess, and it does not.

`devmachine packages new <name> --scope machine --kind dns` writes an
entrypoint that already follows this contract, with placeholders to
replace rather than writing one from nothing.

## How it is called

```
<entrypoint> list|upsert|append|delete <zone>
```

`<entrypoint>` is the package's `entrypoint`, for example `bin/provider`.
The command is `argv[1]`, the zone is `argv[2]`.

`zones` and `help` take no zone:

```
<entrypoint> zones
<entrypoint> help
```

`zones` answers which zones the credential can see, so the CLI can work
out which registrar holds a name. A provider that cannot answer it can
never be chosen.

```json
{"zones": ["example.com", "example.net"]}
```

`help` answers what the entrypoint accepts, and is what `devmachine
packages help <name>` prints. Leave `args` out for a command that takes
none.

```json
{"commands": [
  {"name": "zones",  "summary": "The zones this token can see."},
  {"name": "list",   "summary": "Every record in a zone.", "args": "<zone>"},
  {"name": "upsert", "summary": "Make a name hold exactly one value.", "args": "<zone>"},
  {"name": "append", "summary": "Add one value next to the ones a name holds.", "args": "<zone>"},
  {"name": "delete", "summary": "Remove one value from a name.", "args": "<zone>"},
  {"name": "help",   "summary": "This list."}
]}
```

Both `zones` and `help` are required. A manifest listing a command its
entrypoint actually refuses is a lie no validator can catch.

## What it receives

`list` takes nothing beyond the zone.

`upsert`, `append` and `delete` take one record as JSON **on stdin**:

```json
{"name": "www", "type": "A", "value": "198.51.100.10", "ttl": 300}
```

- `name` is a **label**, never a full name: `www`, or `@` for the apex
  (the root domain of your server). The provider adds the zone itself.
- `ttl` of `0` means "choose": use whatever the registrar defaults to.
- `type` is one of `A`, `AAAA`, `CNAME`, `TXT`, `MX`, `SRV`, `NS`, `CAA`.
- `value` is written the way a zone file writes it, and the CLI has
  already checked and normalised it:

  | Type | Value | Example |
  | --- | --- | --- |
  | `MX` | `<priority> <host>` | `10 mail.example.com` |
  | `SRV` | `<priority> <weight> <port> <target>` | `10 5 5060 sip.example.com` |
  | `CAA` | `<flags> <tag> "<value>"` | `0 issue "ca.example.net"` |
  | the rest | the content itself | `198.51.100.10` |

  Host names arrive in lower case with no trailing dot. A registrar that
  keeps a priority, a port or a CAA tag in a field of its own splits the
  value on the way in and joins it on the way out, so `list` prints the
  same spelling `upsert` was given. Comparing a value the registrar
  stores (`10 mail.example.com.`) with one the CLI sends is the
  provider's job too.

An SRV `name` is the whole owner label, `_sip._tcp` or `_sip._tcp.voice`.
A registrar that stores the service and protocol apart from the name
splits it.

## What it must print

On success, one JSON document on stdout:

- `list` → `{"records": [{"name": "...", "type": "...", "value": "...", "ttl": 300}, ...]}`
- `upsert`, `append` and `delete` → `{}`

## What a failure looks like

Exit non-zero, and print one JSON document on stdout:

```json
{"error": {"kind": "zone_not_found", "message": "no zone answers for example.com"}}
```

`kind` must be one of exactly these:

| `kind` | When |
| --- | --- |
| `zone_not_found` | The zone does not exist, or the token cannot see it. |
| `unauthenticated` | The token was rejected. |
| `forbidden` | The token can see the zone but cannot change it. |
| `invalid_record` | The record was rejected — a bad type, a bad value, a name the registrar refuses. |
| `rate_limited` | The registrar's API is throttling this token. |
| `ambiguous` | The name holds several values and the request does not say which one. |

A `kind` outside this list is treated as a bug in the provider, not a new
kind the CLI learns.

## What the environment holds

Whatever `/etc/devmachine/<credential>/env` sets for this provider's
credential, exported, **and nothing else the CLI adds**. A provider
reading a variable its own credential does not declare will work by
accident on one machine and break everywhere else.

## Where and as whom it runs

On the machine, as root, from `/opt/devmachine/roles/<package>/`. Its
working directory is not guaranteed — use absolute paths, never a path
relative to the entrypoint's location.

## The rules that matter most

- `upsert` makes the name hold **exactly** that one value — adding to
  what is already there is wrong, even if the registrar's API makes that
  the easy path.
- `append` adds that one value next to whatever the name and type
  already hold, and never removes or changes the others. A value that is
  already there is left alone, so the call is safe to repeat. With a
  `ttl`, the set's TTL becomes that value where the registrar keeps one
  TTL per set; without one, the set keeps its own.
- `delete` removes exactly that one value, leaving every other value at
  that name untouched.
- A package that predates `append` leaves it out of `commands`. The CLI
  then refuses `dns add --append` for that provider before running
  anything, and says to update the package.
- An empty `value` on either call means the whole set, not a value that
  happens to be an empty string.

## Python 3, standard library only

Ansible already needs Python on any machine this CLI sets up, so an
entrypoint with no extra dependencies always works. No `requests`, no
`pip install`, no shelling out to `jq` — a provider needing a package the
machine lacks fails during `sync`, far from where that dependency was
declared.

`format: 1` in `package.yml` lets an older CLI refuse a package it
cannot read cleanly. Leave the number the skeleton already put there.

## Never retry a rate limit

A provider that hits a rate limit reports `rate_limited` and stops.
Retrying inside the entrypoint hides how throttled the registrar really
is, and can turn one slow request into a hang with no visible cause.
