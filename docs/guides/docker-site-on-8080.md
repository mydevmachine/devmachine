---
description: "Run a site in a Docker container and serve it at your own domain, with HTTPS."
category: Web apps
level: Beginner
needs:
  - "A Debian or Ubuntu VPS"
  - "A domain"
related:
  - express-site-with-tls.md
  - cloudflare-dns.md
  - wuzapi-as-your-own-package.md
---
# Docker site on 8080

Run a site in a Docker container and put it on the internet at a real
domain, with HTTPS that renews itself. This is the container case: the app
only listens on `localhost`, and Caddy is the only thing the internet talks
to.

**You need:** a Debian or Ubuntu VPS, and a domain such as
`site.example.com` pointed at it (or a DNS provider package installed, so
`expose add` points it for you — see [DNS](../concepts/dns.md)).

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

### 1. Add Docker to the machine

```
devmachine packages add docker
devmachine sync
```

`setup` already installed Caddy as part of the `essentials` package. If your
machine was set up with `--no-essentials`, or before CLI v0.7.14, add it by
hand: `devmachine packages add caddy`. Docker is never in essentials, so you
always add it yourself. `devmachine packages list` shows each package with
your machine's name next to it.

### 2. Put the workspace in the docker group

```
devmachine workspaces edit acme --set 'workspace.groups=[docker]'
devmachine sync
```

A workspace in the `docker` group can do almost anything on the server, so
this is a deliberate step, not the default —
[workspaces](../reference/commands.md#workspaces) says why.

### 3. Run the container

```
devmachine ssh acme
docker run -d --name site -p 127.0.0.1:8080:80 nginx:alpine
```

Publishing on `127.0.0.1` is enough: Caddy reaches the container locally,
and the port itself is never open to the internet.

### 4. Expose it

Back on your own computer:

```
devmachine expose add acme 8080 --host site.example.com --publish
```

It is on Caddy in seconds; no `sync` needed for the route.

## With your agent

Open a session on your own computer (`devmachine skills add` taught it the
CLI) and say:

```text
Run nginx in a Docker container on my devmachine workspace acme,
publishing on 127.0.0.1:8080, and expose it at site.example.com.
```

If your machine is missing from the config, or you have more than one, the
agent asks which one to use. It adds `docker` (and `caddy` too, if `setup`
ran with `--no-essentials`), puts `acme` in the `docker` group, starts the
container over SSH, then runs `expose add`. You still approve the
`sync` for the packages when it asks, and putting a workspace in the `docker` group is worth
reading before you say yes — it is a lot of trust for one container.

**Check it:** `curl https://site.example.com` serves the nginx welcome
page, with a valid certificate.

Source: [Docker — `docker run`](https://docs.docker.com/engine/reference/run/),
[nginx image](https://hub.docker.com/_/nginx)
