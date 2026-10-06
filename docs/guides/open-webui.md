---
description: "Run Open WebUI in Docker on your server, at your own domain, with you as its only admin."
category: Web apps
level: Beginner
needs:
  - "A Debian or Ubuntu VPS with 1 GB of free memory and 8 GB of free disk"
  - "A domain"
  - "An API key from an OpenAI-compatible provider"
related:
  - docker-site-on-8080.md
  - logins-and-secrets.md
  - librechat.md
---
# Open WebUI on your own server

[Open WebUI](https://openwebui.com) is a chat app you host yourself. It
talks to any provider with an OpenAI-compatible API, and to local models
through Ollama. At the end of this guide it runs in one Docker container
in a workspace, answers at `https://ai.example.com`, and you are its admin.

In our test the container used about 650 MiB of memory, and the image took
about 6 GB of disk.

**You need:** a Debian or Ubuntu VPS, a domain such as `ai.example.com`
pointed at it (or a DNS provider package installed, so `expose add` points
it for you — see [DNS](../concepts/dns.md)), and an API key from a provider
with an OpenAI-compatible API.

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

### 1. Add Docker, and put the workspace in the docker group

On your computer:

```
devmachine packages add docker
devmachine packages add workspace --workspace acme
devmachine workspaces edit acme --set 'workspace.groups=[docker]'
devmachine sync
```

The `workspace` package owns the `groups` setting, so `edit` refuses it
until the workspace lists that package; if it is there already, `packages
add` says so and changes nothing. Keep the quotes: without them, zsh reads
`[docker]` as a file pattern and stops with `no matches found`. A workspace
in the `docker` group can do almost anything on the server — see
[Docker site on 8080](docker-site-on-8080.md) for why that is a deliberate
step.

### 2. Write the settings and the compose file

Inside the workspace (`devmachine ssh acme`):

```
mkdir open-webui && cd open-webui
cat > open-webui.env <<EOF
WEBUI_SECRET_KEY=$(openssl rand -hex 32)
WEBUI_URL=https://ai.example.com
EOF
chmod 600 open-webui.env
cat > compose.yaml <<'EOF'
services:
  open-webui:
    image: ghcr.io/open-webui/open-webui:main
    container_name: open-webui
    restart: always
    ports:
      - "127.0.0.1:3000:8080"
    volumes:
      - open-webui:/app/backend/data
    env_file: open-webui.env
volumes:
  open-webui:
EOF
```

`WEBUI_SECRET_KEY` signs the sign-in sessions. It lives in the file, not in
a command, so an update keeps the same key and nobody is signed out. The
port is bound to `127.0.0.1`: Docker's own firewall rules go
[around ufw](https://docs.docker.com/engine/network/packet-filtering-firewalls/#docker-and-ufw),
so a port published on every address would be open to the internet, while
this one only Caddy reaches. Your chats and settings live in the
`open-webui` volume.

This is a small compose file and not the one-line `docker run` from the
Open WebUI docs for one reason: `devmachine secrets` writes values in
quotes, and `docker compose` removes them, while `docker run --env-file`
keeps them as part of the value.

### 3. Give it your provider key, before the first start

On your computer:

```
devmachine secrets set OPENAI_API_KEY --workspace acme --env-file open-webui/open-webui.env
devmachine credentials push
```

`secrets set` asks for the value without showing it, so the key never
reaches your shell history. `credentials push` adds the line to
`open-webui/open-webui.env` and leaves the other lines as they were. For a
provider other than OpenAI, add its address the same way, by hand, to
`open-webui.env`: `OPENAI_API_BASE_URL=<the provider's /v1 address>`.

Do this before step 4. Open WebUI reads most settings from the environment
only the first time it starts, then keeps them in its own database, and
from then on ignores the environment. Skip this step if you prefer: you can
add the key later in the browser (step 6).

### 4. Start it

Inside the workspace, in `open-webui`:

```
docker compose up -d
```

The first start downloads the image and then takes a minute or two more
to set itself up. `docker ps` shows `(healthy)` when it is ready.

### 5. Expose it, and make the first account

On your computer:

```
devmachine expose add acme 3000 --host ai.example.com --publish
```

Open `https://ai.example.com` and choose **Create Admin Account**. That
first account is the admin. Do this right away: until then, the first
person to find the address gets that role.

Once the admin exists, Open WebUI closes sign-up by itself; a new sign-up
gets `You do not have permission to access this resource.` To let people
in later, turn sign-up on in **Settings → Admin → Authentication**. New
accounts start as `pending` until you approve them there. Setting
`ENABLE_SIGNUP` in `open-webui.env` does nothing after the first start, for
the reason in step 3.

### 6. Add or change a provider

In the browser, as the admin: **Settings → Admin → Connections**. Enter a
URL and an API key for any OpenAI-compatible provider, and its models show
up in the chat. This is also where you change the key from step 3 — a new
value in `open-webui.env` is ignored after the first start.

**Ollama, for local models.** Open WebUI can also talk to
[Ollama](https://ollama.com), which runs models on your own server, with
no API key. The cost is memory: the whole model sits in RAM while it
answers, so even a small model needs several GB of free memory on top of
Open WebUI, and a bigger model needs much more. On a small VPS, a provider
API is the better choice. Without a graphics card, answers are also slow.

## With your agent

Open a session on your own computer (`devmachine skills add` taught it the
CLI) and say:

```text
Run Open WebUI with a small docker compose file in my devmachine workspace
acme: image ghcr.io/open-webui/open-webui:main, port 127.0.0.1:3000:8080,
a volume for its data, and WEBUI_SECRET_KEY and WEBUI_URL in an env file.
Let me store my OpenAI key into that env file with devmachine secrets
before the first start, then expose it at ai.example.com.
```

The agent adds `docker` and the `docker` group, writes the two files over
SSH, starts the container and runs `expose add`. You approve the `sync`,
and the `docker` group is worth reading about before you say yes. When the
agent runs `devmachine secrets set OPENAI_API_KEY ...`, you paste the key
into the prompt it opens yourself — never into the chat with the agent.
You make the admin account yourself in the browser.

**Check it:** `curl -s https://ai.example.com | grep -o '<title>.*</title>'`
prints `<title>Open WebUI</title>`, with a valid certificate.

## Keep it running

`restart: always` brings the container back after a reboot. To update,
inside the workspace in `open-webui`:

```
docker compose pull && docker compose up -d
```

`up -d` replaces the container with one from the new image. Your data and
your settings stay in the volume.

## Remove it

Inside the workspace, in `open-webui`, `docker compose down -v` deletes the
container and the volume with every chat in it. On your computer,
`devmachine expose rm ai.example.com` takes the site off Caddy.

Source: [Open WebUI — Quick start](https://docs.openwebui.com/getting-started/quick-start/),
[environment variables](https://docs.openwebui.com/reference/env-configuration/)
