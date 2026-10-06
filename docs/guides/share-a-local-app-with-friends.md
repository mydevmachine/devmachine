---
description: "Let a friend open an app that runs in a VM on your computer, at your own domain or at a Tailscale Funnel address."
category: Web apps
level: Intermediate
needs:
  - "A local VM made with Lima"
  - "A Tailscale account or a Headscale server"
  - "A devmachine VPS with Caddy and a domain, for your own address"
related:
  - expose-home-server-without-port-forwarding.md
  - a-local-vm-with-lima.md
  - headscale.md
---
# Share an app on your computer with friends

You built something in a VM on your laptop, and a friend wants to open it
on their phone. The internet cannot reach that VM, and Lima forwards its
ports only to your own computer's `127.0.0.1`.

Two ways out. With a VPS and a domain, your VPS serves
`https://demo.example.com` and passes each visit to the VM over your
private network.
With no VPS, Tailscale Funnel gives the VM a public `ts.net` address.

**You need:** a [local VM](a-local-vm-with-lima.md) called `laptop`, a
[Tailscale](../concepts/private-networks.md) account (or your own
[Headscale](headscale.md)), and Tailscale on your own computer. For your
own domain: a devmachine VPS called `vps`, with Caddy, on the same network.

## Before you start: machine, skills, workspace

```
devmachine machines create-local laptop --add
devmachine skills add
devmachine workspaces new acme --machine laptop
devmachine sync --machine laptop
```

The [Lima guide](a-local-vm-with-lima.md) explains each line. `--machine
laptop` picks the VM, since you also have `vps`.

## By hand

### 1. Put the VM on your private network

On your computer. Your VPS must already be on the same network.

<!-- tabs -->

#### Tailscale

```
devmachine packages add tailscale --machine laptop
devmachine sync --machine laptop
devmachine login tailscale --machine laptop
```

`login` runs `tailscale up` on the VM: open the URL it prints and sign in.

#### Headscale

```
devmachine packages add tailscale --machine laptop
devmachine machines edit laptop --set tailscale.login_server=https://net.example.com
devmachine sync --machine laptop
devmachine login tailscale --machine laptop
```

When `login` asks, paste a pre-auth key made on your Headscale server
with `headscale preauthkeys create --user <id>`. See
[your own Tailscale with Headscale](headscale.md).

<!-- /tabs -->

Once the VM has joined, devmachine adds `tailscale:<name>` to the top of
`laptop`'s `hosts` in `config.yml`. A Lima VM's name is `lima-laptop`.

### 2. Start the app

Inside the workspace (`devmachine ssh acme`):

```
mkdir -p ~/demo && cd ~/demo
echo '<h1>Hello from acme</h1>' > index.html
python3 -m http.server 3000 --bind 0.0.0.0
```

Listen on `0.0.0.0`, not `127.0.0.1`: the VPS comes in over the VM's
Tailscale address, and `127.0.0.1` answers only the VM itself. Close the
terminal; the app keeps running in tmux.

### 3. Publish it through your VPS

On your computer:

```
devmachine expose add acme 3000 --host demo.example.com --via vps --publish
```

The VPS's Caddy answers `demo.example.com`, gets the certificate, and
proxies to the VM. Your friend opens `https://demo.example.com`.

How `--via` works, and what to do when it warns:
[expose a home server](expose-home-server-without-port-forwarding.md).

### 4. Take it down

On your computer:

```
devmachine expose rm demo.example.com
```

## No VPS? Use Tailscale Funnel

Funnel lets Tailscale's relays carry public traffic to the VM. It is
Tailscale's own service: Headscale does not offer it.

First allow it in your tailnet: MagicDNS, HTTPS and the `funnel` node
attribute (the [Funnel docs](https://tailscale.com/kb/1223/funnel) show how).

After steps 1 and 2, run it as the VM's admin, since the workspace account
has no `sudo`:

```
devmachine ssh --machine laptop
sudo tailscale funnel 3000
```

It prints `https://<name>.<tailnet>.ts.net`; send that to your friend.
Ctrl-C stops it. Funnel is in beta, gives no custom domain, and listens
only on ports 443, 8443 and 10000.

## With your agent

Open a session on your own computer (`devmachine skills add` taught it the
CLI) and say:

```text
Add tailscale to my local machine laptop, start a static page on port 3000
in workspace acme, and publish it at demo.example.com through my VPS.
```

The agent adds the package, asks before `sync`, starts the app and runs
`expose add --via vps`. You still finish `devmachine login tailscale`
yourself: open its URL, or paste the Headscale key.

**Check it:** from your phone, on mobile data, `https://demo.example.com`
shows "Hello from acme".

## Limits

- **Your computer has to be awake**, and the VM running. When it sleeps,
  your friend gets a 502 (or nothing, with Funnel).
- **Your computer has to be on the private network** when you run `expose
  add`, since it looks up the VM's address. If not, it stops with
  `no address of laptop another machine can reach: no address but a loopback one`.
- **It is public.** Anybody with the link gets in, so share no admin panels.
  `expose rm` (or Ctrl-C on Funnel) closes it.

Source: [Tailscale Funnel](https://tailscale.com/kb/1223/funnel),
[tailscale funnel command](https://tailscale.com/kb/1311/tailscale-funnel)
