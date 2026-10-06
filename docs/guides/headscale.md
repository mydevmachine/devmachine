---
description: "Run your own Tailscale control server with Headscale and put your devmachine on it."
category: Networking
level: Advanced
needs:
  - "A second small server, or your devmachine"
  - "Your devmachine reachable over SSH"
related:
  - ssh-or-mosh.md
  - log-in-with-1password.md
  - cloudflare-dns.md
---
# Your own Tailscale with Headscale

Tested end to end with Headscale 0.29.4 and Tailscale 1.102.4, on Ubuntu
24.04, with packages v23: `login tailscale` with a pre-auth key, `resolve`,
`run`, `doctor` and an SSH alias over the Headscale address with the public
one blocked, and the fallback to the public address once Tailscale stops on
your computer. `scripts/accept/headscale.sh` repeats that run on three local
VMs.

[Headscale](https://headscale.net) is an open source, self-hosted
replacement for Tailscale's control server. You get the same private
network, the same `tailscale` client on every device, but you run the
server that coordinates it — no third party in the loop. This guide puts
Headscale on a small server, joins your devmachine server to it, and joins
your own computer too.

**You need:** a second small Linux server for Headscale itself, tested on Ubuntu
(or reuse your devmachine server — see the note below), and your devmachine
server already reachable over SSH.

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
does. This guide does not need a new workspace — it only touches the
machine — but the block above is the standard starting point if you have
neither yet.

## By hand

### 1. Install Headscale

On the server that will run it (a separate small VPS, or your devmachine
server itself — Headscale is just one more service on it), install the
official `.deb` package from the
[Headscale releases page](https://github.com/juanfont/headscale/releases):

```
wget --output-document=headscale.deb \
  https://github.com/juanfont/headscale/releases/download/v<version>/headscale_<version>_linux_amd64.deb
sudo apt install ./headscale.deb
```

Edit `/etc/headscale/config.yaml` to set your server's URL, then start it:

```
sudo systemctl enable --now headscale
```

See [Headscale's own install docs](https://headscale.net/stable/setup/install/official/)
for the config fields and current version.

### 2. Create a user and a pre-auth key

```
headscale users create acme
headscale users list
headscale preauthkeys create --user <id> --expiration 1h
```

`--user` takes the user's number from the `ID` column of `users list`, not
its name. The key printed is what a device trades for a place on your
network. It expires — here, in one hour — and by default works once, so
generate a fresh one per device.

### 3. Join your devmachine server

Add the `tailscale` package:

```
devmachine packages add tailscale
```

Then tell it where your Headscale server is. In `config.yml`, under your
machine:

```yaml
machines:
  - name: main
    settings:
      tailscale.login_server: https://net.example.com
```

`login_server` needs a packages release whose `tailscale` package declares
it — see [settings](../reference/settings.md). An older release ignores it
and joins Tailscale's own service. Then:

```
devmachine sync
devmachine login tailscale
```

`login tailscale` runs the package's join on the server, as its admin, in a
real terminal. With `login_server` set, it runs `tailscale up
--login-server https://net.example.com` and first asks for a pre-auth key:
paste the one from step 2. Leave it empty to sign in through a URL instead,
which you then approve on the Headscale server with `headscale nodes
register`.

Once the server has joined, devmachine asks it for its name on your network
and adds it to `config.yml`, above the public address:

```yaml
machines:
  - name: main
    hosts:
      - tailscale:main
      - 203.0.113.10
```

The key goes into a file only root can read on the server, for as long as
`tailscale up` needs it, and never into a command line or your
configuration.

### 4. Join your own computer

Install Tailscale from [tailscale.com/download](https://tailscale.com/download),
then join the same Headscale server:

```
tailscale up --login-server https://net.example.com --authkey <a fresh key>
```

### 5. Check the private address

```
devmachine resolve
```

It lists the server's Headscale address, typically in the `100.64.0.0/10`
range, from `tailscale:main`, before the public one. `tailscale:<name>` asks
the `tailscale` command on your computer, which works the same whether it
talks to Tailscale's own service or to your Headscale server — see
[addresses and fallback](../how-it-works/addresses-and-fallback.md). When
it lists the entry under `skipped`, the reason says why: most often
Tailscale is not running on your computer, or it joined another network.

## With your agent

Open a session on your own computer (`devmachine skills add` taught it the
CLI) and say:

```text
My devmachine server needs to join my Headscale server at
https://net.example.com. Add the tailscale package with that login server,
sync, and run the login.
```

The agent adds the package and the `tailscale.login_server` setting, runs
`sync`, and then hands `devmachine login tailscale` to you: it needs a
terminal, and a pre-auth key from your Headscale server — generating one is
your call, since it decides who gets on your network. Joining your own
computer to Headscale is yours to do too: it needs the Tailscale app on your
machine, not the server's.

**Check it:** `devmachine resolve` lists `tailscale:main` first, with a
`100.64.x.x` address.

Source: [Headscale](https://headscale.net),
[Headscale — Official releases](https://headscale.net/stable/setup/install/official/),
[Headscale — Getting started](https://headscale.net/stable/usage/getting-started/)
