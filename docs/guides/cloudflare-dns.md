---
description: "Give devmachine a scoped Cloudflare token, and expose add points your domain names for you."
category: Networking
level: Beginner
needs:
  - "A Linux VPS (tested on Debian and Ubuntu)"
  - "A domain on Cloudflare"
related:
  - express-site-with-tls.md
  - docker-site-on-8080.md
  - logins-and-secrets.md
---
# Point your domain with Cloudflare

If your domain's DNS is on Cloudflare, devmachine can create and update
records for you: `expose add` points a name at your server without you
touching the Cloudflare dashboard again. This guide adds the Cloudflare
package, hands it a scoped token, and publishes a site at a real subdomain.

**You need:** a Linux VPS (tested on Debian and Ubuntu), a domain whose DNS is on Cloudflare
(here, `example.com`), and a Cloudflare account.

## Before you start: machine, skills, workspace

```
curl -fsSL https://mydevmachine.sh/install.sh | sh
devmachine setup
devmachine skills add
devmachine workspaces new acme
devmachine sync
```

Already have a machine? Skip `setup`. Already have the workspace? Skip the
last two. See [getting started](../getting-started.md) for what each command
does.

## By hand

### 1. Create a scoped Cloudflare API token

Go to [dash.cloudflare.com/profile/api-tokens](https://dash.cloudflare.com/profile/api-tokens)
and select **Create Token**. Use the **Edit zone DNS** template, and scope
it to the one zone you want devmachine to manage (`example.com`). This
gives the token `Zone / DNS / Edit` on that zone only — not your whole
account.

### 2. Add the package

```
devmachine packages add cloudflare
devmachine sync
```

`cloudflare` is a `machine`-scope package: one install serves every
workspace and every zone the token can see. See
[DNS providers](../how-it-works/dns-providers.md) for what it does under
the hood, including why it always sends `proxied: false` for a record
devmachine manages — the certificate needs the request to reach your
server directly, not Cloudflare's edge.

### 3. Store the token and deliver it

```
devmachine secrets set cloudflare
devmachine credentials push
```

`secrets set cloudflare` asks for the token without echoing it, and stores
it in the OS keychain. `credentials push` writes it to
`/etc/devmachine/cloudflare/env` on the machine, where the package reads
it — the value is never printed, and never lands in `config.yml`.

### 4. Check the provider can see your zone

```
devmachine dns providers
```

This is the first thing to run when DNS does not do what you expect: it
lists every installed provider and the zones it can see. A token from the
**Edit zone DNS** template sees exactly the zones it is scoped to, so there
is nothing to list by hand. If your zone is not there, the token does not
cover it: every `cloudflare` command finds a zone by its name through that
same lookup, so `dns` and `expose` cannot reach it either. Edit the token
to include the zone, then `secrets set cloudflare` and `credentials push`
again if you made a new one.

```
devmachine dns list example.com
```

shows every record the provider holds for that zone right now.

### 5. Expose a workspace's app, pointing DNS automatically

```
devmachine expose add acme 3000 --host app.example.com --publish
```

With `cloudflare` installed, `expose add` creates the `A` record for
`app.example.com` itself, instead of printing one for you to add by hand.
It then writes the Caddy route on the machine and reloads Caddy, which
gets the certificate — no `sync` needed.

### 6. Check it from outside

```
devmachine dns status app.example.com
```

`dns list` asks Cloudflare what is configured; `dns status` asks the
internet — it resolves the name and checks the certificate the way a
visitor would. A record can be correct in `dns list` and still fail `dns
status` for a minute or two while it spreads.

## With your agent

Open a session on your own computer (`devmachine skills add` taught it the
CLI) and say:

```text
Add the cloudflare package to my devmachine machine, then expose my
workspace acme's app on port 3000 at app.example.com using Cloudflare
for DNS.
```

The agent runs `packages add cloudflare` and `sync`, then asks you to open
[dash.cloudflare.com/profile/api-tokens](https://dash.cloudflare.com/profile/api-tokens)
and create a scoped **Edit zone DNS** token for your zone — that page needs
your own Cloudflare login, so the agent cannot do it. It then runs `secrets
set cloudflare` in a terminal it opens for you to paste the token into,
runs `credentials push`, and finally `expose add acme 3000 --host
app.example.com --publish`.

**Check it:** `curl https://app.example.com` returns your app's response
with a valid certificate, and `devmachine dns status app.example.com`
reports it healthy.

Source: [Cloudflare — Create an API token](https://developers.cloudflare.com/fundamentals/api/get-started/create-token/),
[Cloudflare — DNS record settings (proxy status, SSL/TLS)](https://developers.cloudflare.com/dns/manage-dns-records/reference/proxied-dns-records/)
