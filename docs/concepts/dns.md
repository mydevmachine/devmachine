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
replaces whatever was there. A name that needs several values on purpose
is out of scope for `dns add`; use `dns list` to see what's there, and
your provider's own tools for that case.

## `@` is the apex

`@` means the domain itself, with no `www` or other prefix — the way most
registrars write it:

```
devmachine dns add @ A 198.51.100.10 --zone example.com
```

Most of the time you type the full name, like `example.com` or
`www.example.com`, and the CLI works out the label on its own.

## Supported record types

`A`, `AAAA`, `CNAME`, `TXT`. Not `MX` or `SRV` — those need more than one
value, and `dns add` only ever sets one.

## A provider is a package

Talking to your domain provider — Hostinger, Cloudflare, GoDaddy, or another — is
a package, installed with `devmachine packages add` like any other. Until
one is installed, `dns add` can't write anywhere, and prints the record
for you to create by hand instead. See
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
