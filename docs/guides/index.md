# Guides

Real setups, done in a few steps. Each guide says what you need and how long
it takes, then shows the commands two ways: by hand, and as one request to
your coding agent. The why lives in the concept pages it links to.

**New here? Start with [your first devmachine](your-first-devmachine.md).**

**Whatever you start keeps running.** Every login to a workspace opens inside
a tmux session. Start an app or an agent, close the terminal, and it keeps
running on the server. `devmachine ssh acme` puts you back in the same
session. Only a reboot stops it.

## Get started

- [Your first devmachine, explained](your-first-devmachine.md) — the first
  commands done slowly, with what each one asks and does.
- [Try it on your own computer first](a-local-vm-with-lima.md) — a local
  virtual machine with Lima, used exactly like a VPS.
- [An old Mac as a machine](an-old-mac-as-a-machine.md) — an old Mac mini
  or MacBook, Intel or Apple Silicon, set up for your workspaces.

## Workspaces

- [Keep your sessions running](keep-sessions-running.md) — close the laptop,
  and Claude Code keeps working on the server.
- [Share a session](share-a-session.md) — let someone watch, or type into,
  one of your tmux sessions for an hour, then they are out.
- [One consultant, three startups](one-consultant-three-startups.md) — a
  sandbox per client, each with its own stack and its own logins.
- [Contribute to a Ruby on Rails project](ruby-on-rails.md) — a Rails
  workspace for an open source gem, from fork to running tests.
- [One set of skills for every
  workspace](shared-skills-for-every-workspace.md) — write an agent skill
  once, and every workspace gets it on the next sync.

## Agents

- [Claude Code, controlled from your phone](claude-code-remote-control.md) —
  start a session on your server, drive it from the Claude app.
- [Start Claude from your phone](start-claude-from-your-phone.md) — a session
  that stays up on the server, so you never have to SSH in first.
- [OpenClaw, in its own sandbox](openclaw-in-its-own-sandbox.md) — an
  autonomous agent, always on, in a sandbox of its own.
- [A Claude that never sleeps](claude-that-never-sleeps.md) — `/rc` by hand,
  then drive the session from your phone or Claude Desktop.
- [Hermes Agent, in its own sandbox](hermes-agent-in-its-own-sandbox.md) —
  Nous Research's agent, always on, in a sandbox of its own.
- [Agno AgentOS with a control plane](agno-agentos-with-control-plane.md) — an
  agent API and its dashboard, each on its own subdomain.
- [Pi as your coding agent](pi-coding-agent.md) — install Pi in a workspace
  and use it for everyday work.
- [opencode as your coding agent](opencode.md) — try it on its free models,
  then connect your own provider.
- [Antigravity CLI as your coding agent](antigravity-cli.md) — Google's
  agent, signed in once from your own terminal.
- [Kimi Code as your coding agent](kimi-code.md) — Moonshot AI's agent,
  signed in with a device code.
- [Cline as your coding agent](cline.md) — the Cline agent in the
  terminal, with your Cline account or your own key.
- [Claude 24/7 in Telegram](claude-24-7-in-telegram.md) — message Claude Code
  from Telegram, while it runs on your server around the clock.
- [Send work to a Claude session from a POST/Telegram/WhatsApp using Channels](claude-channels.md)
  — a webhook, a chat message or a cron job sends work to a running Claude
  session, and it answers back.
- [A simple agent with LangChain and OpenAI](langchain-agent-with-openai.md) —
  a small Python agent with two tools, its key delivered by devmachine.

## Web apps

- [Express site with TLS](express-site-with-tls.md) — an Express app, live at
  your own domain with HTTPS.
- [Docker site on 8080](docker-site-on-8080.md) — a container, live at your
  own domain with HTTPS.
- [Next.js app at your own domain](nextjs-site-with-tls.md) — a production
  build under pm2, at your own domain with HTTPS; a Nest.js API works the
  same way.
- [FastAPI app at your own domain](fastapi-with-tls.md) — uvicorn as a
  service that survives a reboot, with Swagger UI at your domain.
- [LibreChat, your own AI chat ecosystem](librechat.md) — one self-hosted chat
  for many AI providers, agents, MCP and your files.
- [Open WebUI on your own server](open-webui.md) — a self-hosted AI chat in
  one container, with you as its only admin.
- [Expose a home server without port forwarding](expose-home-server-without-port-forwarding.md)
  — an old laptop or homelab box at your own domain with HTTPS, through
  your VPS, even behind CGNAT.
- [Share an app on your computer with friends](share-a-local-app-with-friends.md)
  — an app in a local VM, opened from a friend's phone through your VPS
  or Tailscale Funnel.
- [wuzapi as your own package](wuzapi-as-your-own-package.md) — a WhatsApp API
  written once as a package, running on two machines.

## Networking

- [SSH or mosh?](ssh-or-mosh.md) — which one to use, and what to install
  where.
- [Point your domain with Cloudflare](cloudflare-dns.md) — a scoped API token,
  and devmachine points your names for you.
- [Your own Tailscale with Headscale](headscale.md) — a private network with a
  control server you host yourself.

## Security

- [Log in with your 1Password SSH key](log-in-with-1password.md) — keep the
  key in your vault, and approve each use with Touch ID.
- [Logins and secrets](logins-and-secrets.md) — see what is missing, sign in,
  and hand over tokens with `credentials`, `login` and `secrets`.
