# DNS

`devmachine dns` points a domain name at your server, so people can reach
it by name instead of a bare IP address.

```
devmachine dns add example.com A 198.51.100.10
```

## Zone and record

A **zone** is a domain: `example.com`. A **record** is one name, one
type, and one value: `www.example.com A 198.51.100.10`.

`dns add` makes a name hold **exactly** the one value you give it — it
replaces whatever was there, and says what it is about to replace before
it does. A name that needs several values on purpose — two mail servers,
a round-robin of addresses — gets them with `--append`, which keeps what
is there and adds one more:

```
devmachine dns add example.com MX "10 mail.example.com"
devmachine dns add example.com MX "20 mail2.example.com" --append
```

`--ttl <seconds>` says how long a resolver may cache the record. Without
it the registrar chooses.

## `@` is the apex

`@` means the domain itself, with no `www` or other prefix — the way most
registrars write it:

```
devmachine dns add @ A 198.51.100.10 --zone example.com
```

Most of the time you type the full name, like `example.com` or
`www.example.com`, and the CLI works out the label on its own.

## Supported record types

`A`, `AAAA`, `CNAME`, `TXT`, `MX`, `SRV`, `NS` and `CAA`. A value is
written the way a zone file or `dig` writes it, with the numbers inside
it:

| Type | Value | Example |
| --- | --- | --- |
| `MX` | `<priority> <host>` | `10 mail.example.com` |
| `SRV` | `<priority> <weight> <port> <target>` | `10 5 5060 sip.example.com` |
| `CAA` | `<flags> <tag> <value>` | `0 issue ca.example.net` |

The CLI checks the value fits its type before it asks anyone — a
priority above 65535, a CAA tag that is not `issue`, `issuewild` or
`iodef`, an `A` that is not an IPv4 address — and spells host names one
way (lower case, no trailing dot) so the same record typed twice compares
equal. `SOA` stays with the registrar.

## A provider is a package

Talking to your domain provider — Hostinger, Cloudflare, GoDaddy, or
another — is a package, installed with `devmachine packages add` like any
other. Until one is installed, `dns add` can't write anywhere, and prints
the record for you to create by hand instead. See
[DNS providers](../how-it-works/dns-providers.md) for what each one does.

## Choosing a zone and a provider

A command picks which zone a name falls in, then which installed
provider handles that zone. Name one directly with `--dns-provider` to
skip the guessing; with two providers claiming the same zone, the CLI
asks you to pick. Every command prints which provider it picked, and why.

The command asks before it writes. In a script, pass `--publish` to
confirm you mean to create public DNS; `--yes` alone never does.

## `dns status` is not `dns list`

`dns list` asks your **provider** what's configured, whether or not it
has reached the rest of the internet yet. `dns status` instead asks **the
internet** directly — it resolves the name and checks its certificate,
the way anybody else would see it, with no provider or machine needed.

A record can be right in `dns list` and still fail `dns status` simply
because it hasn't spread yet. The two answer different questions on
purpose.
