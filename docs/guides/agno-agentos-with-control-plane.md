---
description: "Serve Agno AgentOS and its control plane dashboard, each at its own subdomain with HTTPS."
category: Agents
level: Advanced
needs:
  - "A Linux VPS (tested on Debian and Ubuntu)"
  - "A domain"
related:
  - hermes-agent-in-its-own-sandbox.md
  - express-site-with-tls.md
  - cloudflare-dns.md
---
# Agno AgentOS with a control plane

Run [Agno AgentOS](https://docs.agno.com/agent-platform/overview), the
runtime that serves your agents over a REST API, in an isolated workspace
`agents`, next
to the [AgentOS control plane](https://github.com/djalmaaraujo/agentos-control-plane),
a small open dashboard that talks to it. Both end up live at their own
subdomain, with HTTPS.

**You need:** a Linux VPS (tested on Debian and Ubuntu), and a domain such as
`agents.example.com` pointed at it (or a DNS provider package installed, so
`expose add` points it for you — see [DNS](../concepts/dns.md)).

## Before you start: machine, skills, workspace

```
curl -fsSL https://mydevmachine.sh/install.sh | sh
devmachine setup
devmachine skills add
devmachine workspaces new agents
devmachine sync
```

Already have a machine? Skip `setup`. Already have the workspace? Skip the
last two. See [getting started](../getting-started.md) for what each command
does.

## By hand

### 1. Add Docker and the docker group

```
devmachine packages add docker
devmachine workspaces edit agents --set 'workspace.groups=[docker]'
devmachine sync
```

Caddy came with `setup` (it's in the `essentials` package) and will answer
both exposed sites. If your machine was set up with `--no-essentials`, or
before CLI v0.7.14, add it too: `devmachine packages add caddy`. Docker runs
the control plane's container; putting `agents` in the `docker` group is a lot
of trust for one workspace — [workspaces](../reference/commands.md#workspaces)
says why.

### 2. Run AgentOS

```
devmachine ssh agents
mise use -g python@3.12
curl -LsSf https://astral.sh/uv/install.sh | sh
uv venv && source .venv/bin/activate
uv pip install -U 'agno[os]'
```

Write the app AgentOS's own quickstart gives — one agent, a database, and
the `AgentOS` object:

```python
# my_os.py
from agno.agent import Agent
from agno.db.sqlite import SqliteDb
from agno.os import AgentOS

assistant = Agent(
    name="Assistant",
    model="openai:gpt-5.5",
    db=SqliteDb(db_file="agno.db"),
)

agent_os = AgentOS(agents=[assistant], db=SqliteDb(db_file="agno.db"))
app = agent_os.get_app()

if __name__ == "__main__":
    agent_os.serve(app="my_os:app", reload=True)
```

Set a model key first (`export OPENAI_API_KEY=...`, per the provider you
picked), then start it, bound to this workspace only:

```
export OS_SECURITY_KEY=$(openssl rand -hex 32)
python my_os.py
```

`OS_SECURITY_KEY` turns on the shared-token check: every request needs
`Authorization: Bearer <key>`, or it gets a 401. AgentOS's own docs say to
use its JWT-based authorization instead for a production deployment — this
guide uses the security key because it is the simpler one, and the
control plane only needs the one token anyway. Leave the terminal running:
the SSH login is already inside tmux, so it keeps going after you log out —
`devmachine ssh agents` puts you back. AgentOS listens on port `7777` by
default.

### 3. Run the control plane

Still in the workspace, in a new pane or window:

```
git clone https://github.com/djalmaaraujo/agentos-control-plane.git
cd agentos-control-plane
cp .env.example .env
```

Edit `.env`: set `AGENTOS_UPSTREAM` to reach AgentOS, and `OS_SECURITY_KEY`
to the same value you started it with. The project's `compose.yaml` already
maps `host.docker.internal` to the server, so the container reaches AgentOS
on port `7777`:

```
AGENTOS_UPSTREAM=http://host.docker.internal:7777
OS_SECURITY_KEY=<the same key>
CP_AUTH_TOKEN=<a password for the dashboard itself>
```

```
docker compose up -d --build
```

In `compose.yaml`, change the port line from `"8810:80"` to
`"127.0.0.1:8810:80"`. Docker opens a published port past the firewall, so
without this the dashboard would also answer at `http://<server-ip>:8810`,
with no HTTPS. With it, the dashboard is reachable only through Caddy. Run
`docker compose up -d --build` again after the change.

The container listens on port `8810`. `CP_AUTH_TOKEN` is optional — set it
and the dashboard asks for that password before showing anything; leave it
empty and anyone who reaches the URL gets in.

### 4. Expose both, on their own subdomain

Back on your own computer:

```
devmachine expose add agents 7777 --host api.agents.example.com --publish
devmachine expose add agents 8810 --host console.agents.example.com --publish
```

AgentOS is the one with real access to your agents, so leaving
`OS_SECURITY_KEY` (or JWT authorization) on before this step matters — once
it is public, the token is the only thing standing between the internet and
your agents.

## With your agent

Open a session on your own computer (`devmachine skills add` taught it the
CLI) and say:

```text
In my devmachine workspace agents, set up Agno AgentOS with uv, running
on port 7777 with OS_SECURITY_KEY set, then run the agentos-control-plane
Docker container pointed at it on port 8810. Expose AgentOS at
api.agents.example.com and the control plane at console.agents.example.com.
```

The agent adds `docker` (and `caddy` too, if `setup` ran with
`--no-essentials`), puts `agents` in the `docker` group, installs `uv` and
Agno over SSH, writes the AgentOS app, starts both processes, then runs
`expose add` twice. You still approve `sync` when it asks, pick
and type in the model API key yourself, and choose the `OS_SECURITY_KEY` and
`CP_AUTH_TOKEN` values — the agent should not be generating the tokens that
guard your own agents.

**Check it:** `curl -H "Authorization: Bearer <OS_SECURITY_KEY>" https://api.agents.example.com/config`
returns AgentOS's own config, and `https://console.agents.example.com`
loads the dashboard, both with a valid certificate.

Source: [Agno AgentOS overview](https://docs.agno.com/agent-platform/overview),
[AgentOS security overview](https://docs.agno.com/agent-os/security/overview),
[agentos-control-plane README](https://github.com/djalmaaraujo/agentos-control-plane)
