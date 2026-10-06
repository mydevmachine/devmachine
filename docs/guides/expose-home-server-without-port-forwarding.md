---
description: "Give an old laptop, a mini PC or a homelab box a public HTTPS subdomain through your VPS. No port forwarding, works behind CGNAT, no tunnel service."
category: Web apps
level: Intermediate
needs:
  - "A devmachine VPS with Caddy"
  - "A computer at home running Linux (tested on Debian and Ubuntu)"
  - "A domain"
  - "A private network: Tailscale, Headscale, WireGuard"
related:
  - headscale.md
  - docker-site-on-8080.md
  - cloudflare-dns.md
---
# Expose a home server to the internet without port forwarding

That old laptop in a drawer, the mini PC under the TV, the Raspberry Pi
running your homelab: each one can serve a site at a real domain, with
HTTPS, without opening a single port on your router. It works behind
CGNAT, behind a landlord's Wi-Fi, behind anything that lets the machine
connect out.

Your VPS does the public part. Its Caddy answers `media.example.com`, gets
the certificate, and passes each request to the machine at home over your
private network. The machine at home needs no public address and no Caddy.

```
visitor ──HTTPS──▶ VPS (Caddy, public IP) ──private network──▶ old laptop :8096
```

**You need:** a devmachine VPS with Caddy (`setup` installs it), a domain,
a computer at home running Linux (tested on Debian and Ubuntu), and a private network both
machines join — [Tailscale](../concepts/private-networks.md), your own
[Headscale](headscale.md), plain WireGuard, or a LAN the VPS can reach.
With a [DNS provider package](../concepts/dns.md) installed, the DNS record
is created for you too.

## Why you can't reach your home server from the internet

Most homes cannot take a visitor in. The provider shares one public
address among many customers (CGNAT), or changes it every few days, or
blocks ports 80 and 443. Port forwarding fixes some of that and opens a
door in your router you then have to watch.

The usual answers move the problem to a third party: Cloudflare Tunnel,
ngrok, Tailscale Funnel. Here the one machine that faces the internet is a
VPS you already run, and the domain is yours.

## Before you start: machine, skills, workspace

You already have a VPS called `vps` in devmachine. On the computer at home,
install Debian or Ubuntu Server, enable SSH, and join it to your private
network. With Tailscale:

```
curl -fsSL https://tailscale.com/install.sh | sh
sudo tailscale up
```

On a laptop, keep it awake with the lid closed: set
`HandleLidSwitch=ignore` in `/etc/systemd/logind.conf` and run
`sudo systemctl restart systemd-logind`.

The VPS has to be on the same private network. With Tailscale:

```
devmachine packages add tailscale --machine vps
devmachine sync --machine vps
devmachine login tailscale --machine vps
```

## By hand

### 1. Add the computer at home as a machine

```
devmachine machines add
```

When it asks for the address, give its private one — the name Tailscale
gave it, as `tailscale:homelab`, or an address such as `100.64.0.7`. Add
the network package to it too, so it stays on the network after a rebuild:

```
devmachine packages add tailscale --machine homelab
devmachine sync --machine homelab
```

A `tailscale:` name is resolved on your own computer, so your computer has
to be on the private network as well. `devmachine resolve --machine homelab`
shows the address the VPS will proxy to.

### 2. Make a workspace and start the service

```
devmachine workspaces new media --machine homelab
devmachine sync --machine homelab
devmachine ssh media
```

Start whatever you want to publish, listening on every address or on the
private one — not only on `127.0.0.1`, which the VPS cannot reach. Jellyfin
in Docker, for example:

```
docker run -d --name jellyfin -p 8096:8096 -v ~/media:/media jellyfin/jellyfin
```

### 3. Publish it through the VPS

Back on your own computer:

```
devmachine expose add media 8096 --host media.example.com --via vps --publish
```

In seconds, the VPS's Caddy serves `media.example.com` and proxies it to
`homelab:8096`. The DNS record points at the VPS's public address, and Caddy
gets the Let's Encrypt certificate on the first visit. If the VPS cannot
reach the port, the command says what to check instead of leaving visitors
a 502.

Without `--via`, `expose add` on a machine with no Caddy refuses and lists
the machines that have it: devmachine never picks one for you.

**In the macOS app:** right-click the workspace → **Publish a port…**.
"Published by" already shows the VPS, with a line saying why.

## With your agent

Open a session on your own computer (`devmachine skills add` taught it the
CLI) and say:

```text
Publish port 8096 of my workspace media, on homelab, at media.example.com
through my VPS.
```

The agent checks that `homelab` has no Caddy, picks `--via vps` because it
is the machine that has it, and runs `expose add`. You approve the publish
when it asks.

**Check it:** `curl -I https://media.example.com` answers from Jellyfin,
with a valid certificate, from any network.

## What people use it for

### An old computer as a staging server

An old desktop has more RAM and disk than a cheap VPS. Make it a
devmachine machine, give it a `staging` workspace, and publish
`staging.example.com` through the VPS. Your team sees every branch before
it ships, and the VPS bill stays the same.

### Jellyfin or Plex remote access, managed with Claude Code

Put the media server in a workspace on the box at home, publish it with
`--via`, and run Claude Code in that same workspace. From anywhere —
`devmachine ssh media`, the macOS app, or your phone — ask it to add a
library, fix a transcoding setting or update the container. Remote access
to Jellyfin, without port forwarding and without a tunnel service.

### A local dev server with a stable custom domain

A webhook, an OAuth callback, a client demo: they want a real HTTPS
address that does not change between sessions. `dev.example.com` through
your VPS stays the same address for as long as you keep the route.

### The rest of the homelab

Home Assistant, a wiki, a photo library: one `expose add --via` each, all
behind one Caddy, all listed by `devmachine expose list --machine vps`.

## Compared with Cloudflare Tunnel, ngrok and Tailscale Funnel

| | This | Cloudflare Tunnel | ngrok | Tailscale Funnel |
| --- | --- | --- | --- | --- |
| Your own domain | yes | yes | paid plans | no: `*.ts.net` only |
| Who terminates HTTPS | your VPS | Cloudflare | ngrok | your machine, via Tailscale's relays |
| Any port behind it | yes | yes | yes | 443, 8443, 10000 |
| Runs on | a VPS you already have | Cloudflare's network | ngrok's network | your tailnet |

Two things worth knowing before you choose. Cloudflare's terms restrict
serving video through its free CDN, which matters for a media server.
Tailscale Funnel is in beta and has no custom domains. Neither applies
here: the traffic goes through your VPS, under your domain.

## Limits

- **Every byte goes through the VPS.** It counts against the VPS's
  bandwidth and adds one hop of delay. Pick a VPS close to home for video.
- **The VPS still faces the internet** on ports 80 and 443. Only your home
  network needs no open port.
- **The site is public.** Whatever you publish needs its own login. Keep
  admin panels private: reach them over the private network with
  [`devmachine tunnel`](../reference/commands.md#tunnel) instead.
- **The machine at home has to be on.** When it sleeps or loses its
  network, visitors get a 502 until it is back.
- **Limit what the VPS can reach.** On Tailscale, an ACL that lets the VPS
  open only the published port on `homelab` means a VPS someone broke into
  reaches nothing else at home.

## When it does not work

- `expose add` warns that the VPS cannot reach the port: see "`expose add
  --via` warns …" in [troubleshooting](../troubleshooting.md).
- `sync` says a site is left as it is because an address is not known:
  turn the private network on, on your computer, and sync again.
- The name does not resolve: create the record `expose add` printed, or
  install a [DNS provider package](../concepts/dns.md).

How it works underneath, and why the address is never stored:
[publishing through another machine](../how-it-works/published-sites.md#publishing-through-another-machine).
