---
description: "Run LibreChat with Docker Compose on your server: one chat app for many AI providers, agents and your files."
category: Web apps
level: Intermediate
needs:
  - "A Debian or Ubuntu VPS with 2 GB of free memory and 15 GB of free disk"
  - "A domain"
  - "An API key from at least one AI provider"
related:
  - docker-site-on-8080.md
  - logins-and-secrets.md
  - open-webui.md
---
# LibreChat, your own AI chat ecosystem

[LibreChat](https://www.librechat.ai) is a chat app you host yourself. One
screen talks to OpenAI, Anthropic, Google and many more; it can run agents,
call MCP servers and search the files you upload. At the end of this guide
it runs in a workspace with Docker Compose, answers at
`https://chat.example.com`, you are its admin, and nobody else can sign up.

The stack is six containers: the app, MongoDB, Meilisearch, a RAG service,
its vector database, and an admin panel. In our test they used about
750 MiB of memory together, and the images took about 10 GB of disk.

**You need:** a Debian or Ubuntu VPS, a domain such as `chat.example.com`
pointed at it (or a DNS provider package installed, so `expose add` points
it for you — see [DNS](../concepts/dns.md)), and an API key from at least
one AI provider. Or none: each user can paste their own key (step 6).

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

### 2. Get LibreChat

Inside the workspace (`devmachine ssh acme`):

```
git clone https://github.com/LibreChat-AI/LibreChat.git
cd LibreChat
cp .env.example .env
```

`.env` holds every setting. Git ignores it, so `git pull` never touches
your copy.

### 3. Set the address and the secrets

Inside the workspace, in `LibreChat`:

```
sed -i \
  -e "s|^DOMAIN_CLIENT=.*|DOMAIN_CLIENT=https://chat.example.com|" \
  -e "s|^DOMAIN_SERVER=.*|DOMAIN_SERVER=https://chat.example.com|" \
  -e "s|^CREDS_KEY=.*|CREDS_KEY=$(openssl rand -hex 32)|" \
  -e "s|^CREDS_IV=.*|CREDS_IV=$(openssl rand -hex 16)|" \
  -e "s|^JWT_SECRET=.*|JWT_SECRET=$(openssl rand -hex 32)|" \
  -e "s|^JWT_REFRESH_SECRET=.*|JWT_REFRESH_SECRET=$(openssl rand -hex 32)|" \
  -e "s|^MEILI_MASTER_KEY=.*|MEILI_MASTER_KEY=$(openssl rand -hex 32)|" \
  -e "s|^ADMIN_PANEL_SESSION_SECRET=.*|ADMIN_PANEL_SESSION_SECRET=$(openssl rand -hex 32)|" \
  .env
```

These values are empty in `.env.example`. Left empty, LibreChat makes
temporary ones, and upstream says to set your own before real use.
`CREDS_KEY` and `CREDS_IV` encrypt the keys users store, so keep them: new
values make the stored keys unreadable. The admin panel container does not
start without `ADMIN_PANEL_SESSION_SECRET`.

### 4. Keep the ports on localhost, and start it

Inside the workspace, in `LibreChat`:

```
cat > docker-compose.override.yml <<'EOF'
services:
  api:
    ports: !override
      - "127.0.0.1:3080:3080"
  admin-panel:
    ports: !override
      - "127.0.0.1:3000:3000"
EOF
docker compose up -d
```

The upstream `docker-compose.yml` publishes port 3080 (and the admin
panel's 3000) on every address of the server. Docker writes its own
firewall rules, and they go
[around ufw](https://docs.docker.com/engine/network/packet-filtering-firewalls/#docker-and-ufw),
so the `firewall` package does not block those ports. This override binds
both to `127.0.0.1`: Caddy reaches LibreChat, the internet does not reach
the port. `!override` replaces the upstream port list instead of adding to
it. The first start downloads about 10 GB of images, so give it a few
minutes. The admin panel stays private; open it with `devmachine tunnel
acme 3000` when you need it.

### 5. Expose it, and make the first account

On your computer:

```
devmachine expose add acme 3080 --host chat.example.com --publish
```

Open `https://chat.example.com` and sign up. The first account becomes the
admin. Do this right away: until step 6, anyone who finds the address can
sign up too.

### 6. Close sign-up

Inside the workspace, in `LibreChat`:

```
sed -i "s/^ALLOW_REGISTRATION=.*/ALLOW_REGISTRATION=false/" .env
docker compose down && docker compose up -d
```

LibreChat reads `.env` only when it starts, so every change to it needs
this restart. Your account still signs in; a new sign-up gets
`Registration is not allowed.`

### 7. Give it provider keys

`.env.example` already sets `OPENAI_API_KEY`, `ANTHROPIC_API_KEY` and
`GOOGLE_KEY` to `user_provided`: each user pastes their own key in the chat
screen, and LibreChat stores it encrypted with `CREDS_KEY`. Nothing more to
do for that.

To pay for everyone with one key of yours, put the real key in `.env`.
Do it with devmachine, so the key never sits in your shell history or in a
file on your computer. On your computer:

```
devmachine secrets set ANTHROPIC_API_KEY --workspace acme --env-file LibreChat/.env
devmachine credentials push
```

`secrets set` asks for the value without showing it. `credentials push`
replaces the `ANTHROPIC_API_KEY=` line in `LibreChat/.env` and leaves every
other line as it was; the first time, it keeps a copy at
`LibreChat/.env.devmachine.bak`. Do the same with `OPENAI_API_KEY` or
`GOOGLE_KEY`. Then restart, inside the workspace in `LibreChat`:

```
docker compose down && docker compose up -d
```

See [your app's own secrets](../concepts/credentials.md#your-apps-own-secrets)
for how the file is edited. To change a key later, run `secrets set` again,
then `credentials push` and the restart.

## The ecosystem

- **Many providers in one place.** OpenAI, Anthropic and Google are built
  in; others that speak an OpenAI-style API go in `librechat.yaml` as
  [custom endpoints](https://www.librechat.ai/docs/configuration/librechat_yaml/ai_endpoints).
- **Agents.** Build an assistant without code, on any provider, with file
  search, a code interpreter, MCP tools and actions from an OpenAPI spec —
  [Agents](https://www.librechat.ai/docs/features/agents).
- **MCP servers.** List them under `mcpServers:` in `librechat.yaml` —
  [MCP servers](https://www.librechat.ai/docs/configuration/librechat_yaml/object_structure/mcp_servers).
  LibreChat reads `librechat.yaml` only when you mount it: add a `volumes:`
  entry under `api:` in `docker-compose.override.yml`, as the
  [Docker guide](https://www.librechat.ai/docs/local/docker) shows, then
  restart.
- **Your files (RAG).** The `rag_api` service and its `vectordb` already
  run in the stack. They turn uploaded files into something agents can
  search, and they need an embeddings key on the server —
  `RAG_OPENAI_API_KEY` or a real `OPENAI_API_KEY` in `.env`; a key a user
  pastes is not used for this —
  [RAG API](https://www.librechat.ai/docs/configuration/rag_api).

## With your agent

Open a session on your own computer (`devmachine skills add` taught it the
CLI) and say:

```text
Install LibreChat with Docker Compose from its GitHub repository in my
devmachine workspace acme. Fill the empty secrets in .env, bind its ports
to 127.0.0.1 with a docker-compose.override.yml, and expose port 3080 at
chat.example.com. Stop after it is exposed so I can make the first account.
```

The agent adds `docker` and the `docker` group, clones the repository over
SSH, fills `.env`, writes the override, starts the stack and runs `expose
add`. You approve the `sync`, and the `docker` group is worth reading about
before you say yes. You make the first account yourself in the browser —
then tell the agent to close sign-up. For a provider key, the agent runs
`devmachine secrets set ... --env-file LibreChat/.env`, and you paste the
key into the prompt it opens yourself — never into the chat with the agent.

**Check it:** `curl -s https://chat.example.com | grep -o '<title>.*</title>'`
prints `<title>LibreChat</title>`, with a valid certificate.

## Keep it running

Every service has `restart: always`, so the stack comes back after a
reboot. To update, inside the workspace in `LibreChat`:

```
git pull && docker compose pull && docker compose up -d
```

`.env`, `docker-compose.override.yml` and the data folders are ignored by
git, so the pull never conflicts with them.

## Troubleshooting

**`chat-mongodb` or `chat-meilisearch` keeps restarting, and the logs say
`Permission denied`.** You set `UID` and `GID` in `.env`. Docker creates the
data folders (`data-node`, `meili_data_*`, `logs`, `uploads`) as root the
first time, and a container running as your workspace's user cannot write
to them. Leave `UID` and `GID` commented out, as `.env.example` has them.

**The page shows the old setting after you edit `.env`.** LibreChat read
`.env` when it started. Run `docker compose down && docker compose up -d`.

**`docker compose logs api`** shows why the app stopped.

## Remove it

Inside the workspace, in `LibreChat`, `docker compose down -v` stops the
stack and deletes its volumes. The data folders inside `LibreChat` belong to
root, so delete them as the admin: `devmachine run 'rm -rf
/home/acme/LibreChat'`. On your computer, `devmachine expose rm
chat.example.com` takes the site off Caddy.

Source: [LibreChat — Docker](https://www.librechat.ai/docs/local/docker),
[.env configuration](https://www.librechat.ai/docs/configuration/dotenv),
[librechat.yaml](https://www.librechat.ai/docs/configuration/librechat_yaml)
