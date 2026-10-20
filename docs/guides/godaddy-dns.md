---
description: "Give devmachine a GoDaddy Personal Access Token, and expose add points your domain names for you."
category: Networking
level: Beginner
needs:
  - "A Linux VPS (tested on Debian and Ubuntu)"
  - "A domain on GoDaddy"
related:
  - cloudflare-dns.md
  - express-site-with-tls.md
  - logins-and-secrets.md
---
# Point your domain with GoDaddy

If your domain's DNS is on GoDaddy, devmachine can create and update
records for you: `expose add` points a name at your server without you
touching the GoDaddy dashboard again. This guide adds the GoDaddy
package, hands it a Personal Access Token, and publishes a site at a real
subdomain.

**You need:** a Linux VPS (tested on Debian and Ubuntu), a domain whose DNS is on GoDaddy
(here, `example.com`), and a GoDaddy account.

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

### 1. Create a Personal Access Token

Go to [developer.godaddy.com](https://developer.godaddy.com), sign in, and
create a Personal Access Token that may read your domains and change their
DNS records. Copy the token at once: GoDaddy shows it only once. The old
key and secret pair does not work here.

### 2. Add the package

```
devmachine packages add godaddy
devmachine sync
```

`godaddy` is a `machine`-scope package: one install serves every
workspace and every domain the token can see. See
[DNS providers](../how-it-works/dns-providers.md) for what it does under
the hood, including why one `PUT` replaces the whole set at a name.

### 3. Store the token and deliver it

```
devmachine secrets set godaddy
devmachine credentials push
```

`secrets set godaddy` asks for the token without echoing it, and stores
it in the OS keychain. `credentials push` writes it to
`/etc/devmachine/godaddy/env` on the machine, where the package reads
it as `GODADDY_API_TOKEN` — the value is never printed, and never lands
in `config.yml`.

### 4. Check the provider can see your zone

```
devmachine dns providers
```

This is the first thing to run when DNS does not do what you expect: it
lists every installed provider and the zones it can see. GoDaddy lists
only `ACTIVE` domains. If your zone is not there, the account has no
active domain, or GoDaddy does not allow it on the Domains API: see
[troubleshooting](../troubleshooting.md). `dns` and `expose` cannot reach
a zone this list does not show.

```
devmachine dns list example.com
```

shows the records the provider holds for that zone right now.

### 5. Expose a workspace's app, pointing DNS automatically

```
devmachine expose add acme 3000 --host app.example.com --publish
```

With `godaddy` installed, `expose add` creates the `A` record for
`app.example.com` itself, instead of printing one for you to add by hand.
It then writes the Caddy route on the machine and reloads Caddy, which
gets the certificate — no `sync` needed.

### 6. Check it from outside

```
devmachine dns status app.example.com
```

`dns list` asks GoDaddy what is configured; `dns status` asks the
internet — it resolves the name and checks the certificate the way a
visitor would. A record can be correct in `dns list` and still fail `dns
status` for a minute or two while it spreads.

## With your agent

Open a session on your own computer (`devmachine skills add` taught it the
CLI) and say:

```text
Add the godaddy package to my devmachine machine, then expose my
workspace acme's app on port 3000 at app.example.com using GoDaddy
for DNS.
```

The agent runs `packages add godaddy` and `sync`, then asks you to open
[developer.godaddy.com](https://developer.godaddy.com) and create a
Personal Access Token that may read your domains and change their DNS
records — that page needs your own GoDaddy login, so the agent cannot do
it. It then runs `secrets set godaddy` in a terminal it opens for you to
paste the token into, runs `credentials push`, and finally `expose add
acme 3000 --host app.example.com --publish`.

**Check it:** `curl https://app.example.com` returns your app's response
with a valid certificate, and `devmachine dns status app.example.com`
reports it healthy.

Source: [GoDaddy — Authentication](https://developer.godaddy.com/docs/api-users/auth),
[GoDaddy — Domains API v1](https://developer.godaddy.com/docs/references/rest/domains/v1)
