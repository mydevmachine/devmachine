---
description: "Run a FastAPI app in a workspace with uv and serve it at your own domain, with HTTPS and its API docs."
category: Web apps
level: Beginner
needs:
  - "A Linux VPS (tested on Debian and Ubuntu)"
  - "A domain"
related:
  - express-site-with-tls.md
  - nextjs-site-with-tls.md
  - keep-sessions-running.md
---
# FastAPI app at your own domain

Write a small FastAPI app in a workspace, run it with uvicorn, and put it on
the internet at `api.example.com`, with HTTPS that renews itself. Its
interactive API docs come along at `https://api.example.com/docs`, and at
the end the app comes back on its own after the server restarts.

**You need:** a Linux VPS (tested on Debian and Ubuntu), and a domain such as `api.example.com`
pointed at it (or a DNS provider package installed, so `expose add` points it
for you — see [DNS](../concepts/dns.md)).

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

Caddy came with `setup` (it's in the `essentials` package), and it will
answer this site and get its own certificate. If your machine was set up
with `--no-essentials`, add it first: `devmachine packages add caddy`, then
`devmachine sync`.

### 1. Install uv

Inside the workspace:

```
devmachine ssh acme
mise use -g uv@latest
uv --version
```

The machine already has Python 3 (Ubuntu ships it), but no package
installs [uv](https://docs.astral.sh/uv/), the tool that makes the project,
its virtual environment and its dependencies. mise is in every workspace
(the `mise` package), so it installs uv into your own account, with no
`sudo`.

### 2. Write the app

Still inside the workspace:

```
mkdir -p ~/dev/items && cd ~/dev/items
uv init --bare
uv add "fastapi[standard]"
cat > main.py <<'EOF'
from fastapi import FastAPI

app = FastAPI()


@app.get("/")
def read_root():
    return {"hello": "acme"}


@app.get("/items/{item_id}")
def read_item(item_id: int, q: str | None = None):
    return {"item_id": item_id, "q": q}
EOF
```

`uv init --bare` writes only `pyproject.toml`; `uv add` creates the
virtual environment in `.venv` and installs FastAPI with uvicorn.

### 3. Start it

Still inside the workspace:

```
uv run uvicorn main:app --host 127.0.0.1 --port 8000
```

It listens on `127.0.0.1` because Caddy runs on the same machine and
reaches the app there; nothing else needs the port, so it stays off the
network.

### 4. Expose the port

On your computer:

```
devmachine expose add acme 8000 --host api.example.com --publish
```

`expose add` writes the Caddy route on the machine and reloads Caddy, which
gets the certificate for the host — no `sync` needed. If no DNS provider
package is installed, `expose add` prints the record to create by hand at
your registrar instead of writing it for you.

Open `https://api.example.com/docs` in your browser: FastAPI's Swagger UI
lists both routes, and "Try it out" calls them through the domain.

## With your agent

Open a session on your own computer (`devmachine skills add` taught it the
CLI) and say:

```text
In my devmachine workspace acme, install uv with mise, create a FastAPI app
in ~/dev/items with the routes / and /items/{item_id}, run it with uvicorn
on 127.0.0.1 port 8000 as a systemd user service that survives a reboot,
and expose port 8000 at api.example.com.
```

The agent runs the same commands over SSH: `mise use -g uv@latest`, the
project, `main.py`, the service from [Keep it running](#keep-it-running),
then `expose add` on your computer. You still point `api.example.com` at
the server yourself if no DNS provider package is installed — the agent
shows you the record. And if `setup` has not run yet, you check the
fingerprint yourself; the agent cannot do that part.

**Check it:** on your computer, `curl 'https://api.example.com/items/5?q=hi'`
prints `{"item_id":5,"q":"hi"}`, with a valid certificate.

## Keep it running

`devmachine ssh acme` opens inside tmux, so uvicorn keeps going after you
close the terminal — see [keep your sessions running](keep-sessions-running.md).
A reboot stops it. To bring it back on its own, run it as a systemd user
service: your own account's services, no `sudo` needed.

First stop the uvicorn from step 3 with `Ctrl-C`. Then, inside the
workspace:

```
mkdir -p ~/.config/systemd/user
cat > ~/.config/systemd/user/items.service <<'EOF'
[Unit]
Description=FastAPI app on port 8000

[Service]
WorkingDirectory=%h/dev/items
ExecStart=%h/dev/items/.venv/bin/uvicorn main:app --host 127.0.0.1 --port 8000
Restart=always

[Install]
WantedBy=default.target
EOF
systemctl --user daemon-reload
systemctl --user enable --now items
loginctl enable-linger
```

`enable --now` starts the service and marks it to start with your account.
`loginctl enable-linger` starts your account's services at boot, with
nobody logged in; without it they wait for your next login. The unit calls
uvicorn from `.venv` by its full path, so it does not need uv or mise on the
`PATH`. `Restart=always` also brings the app back if it crashes.

Once the code lives in a git repository you push to, ship a new version
with:

```
cd ~/dev/items
git pull && uv sync && systemctl --user restart items
```

`journalctl --user -u items -f` shows the app's output, and
`systemctl --user status items` shows whether it is running.

## Remove it

On your computer, take the site off Caddy:

```
devmachine expose rm api.example.com
```

Then, inside the workspace, stop the service:

```
systemctl --user disable --now items
rm ~/.config/systemd/user/items.service
```

Source: [FastAPI — Run a server manually](https://fastapi.tiangolo.com/deployment/manually/),
[uv — Working on projects](https://docs.astral.sh/uv/guides/projects/),
[systemd — user services](https://wiki.archlinux.org/title/Systemd/User)
