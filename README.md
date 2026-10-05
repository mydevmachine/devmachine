<h1 align="center">devmachine</h1>

<p align="center">
  <strong>Your own development server, one workspace per project, coding agents ready in each.</strong>
</p>

<p align="center">
  <a href="https://mydevmachine.sh/">Website</a> ·
  <a href="https://mydevmachine.sh/getting-started/">Getting started</a> ·
  <a href="https://mydevmachine.sh/guides/">Guides</a> ·
  <a href="https://mydevmachine.sh/packages/">Packages</a> ·
  <a href="https://mydevmachine.sh/app/">Mac app</a> ·
  <a href="https://mydevmachine.sh/changelog/">Changelog</a>
</p>

<p align="center">
  <a href="https://github.com/mydevmachine/devmachine/releases/latest"><img alt="Latest release" src="https://img.shields.io/github/v/release/mydevmachine/devmachine?label=release"></a>
  <a href="LICENSE"><img alt="MIT licence" src="https://img.shields.io/badge/licence-MIT-blue"></a>
  <img alt="macOS and Linux" src="https://img.shields.io/badge/runs%20on-macOS%20%7C%20Linux-lightgrey">
  <img alt="Debian and Ubuntu servers" src="https://img.shields.io/badge/servers-Debian%20%7C%20Ubuntu-lightgrey">
</p>

<p align="center">
  <img src=".github/readme/161-machines-map-selected.webp" alt="The network map in the Devmachine app: the internet at the top, published sites such as app.acme.example.com linked down to five machines — laptop and a Lima VM on this Mac, main and staging at a VPS provider, homebox at home over the tailnet — with the details of the main machine below." width="100%">
</p>

devmachine turns a VPS, an old laptop or a virtual machine on your Mac into a
place to code. Each project gets its own account on the machine, called a
**workspace**, with its own tools, logins and coding agent. Close the laptop
and your agents keep working on the server.

It is a single Go binary on your computer. Underneath it is plain Linux, SSH
and git: stop using devmachine and your server still works.

## Quick start

You need a Debian or Ubuntu VPS you can reach as root over SSH, and a Mac or
Linux computer. No server yet? `devmachine machines create-local dev` makes
one on your own computer.

```sh
curl -fsSL https://mydevmachine.sh/install.sh | sh   # or: brew install mydevmachine/tap/devmachine
devmachine setup                                     # connect, lock the server down, install the essentials
devmachine skills add                                # teach your coding agent the CLI
devmachine workspaces new acme && devmachine sync    # add a workspace and build it on the server
devmachine ssh acme                                  # step in
```

`setup` checks it talks to the right server, installs a key, turns off
password logins, and adds base tools, git, a firewall and Caddy. Then you can
ask for all of this from any Claude Code or Codex session instead of typing
it. Walk through it slowly in
[Your first devmachine, explained](https://mydevmachine.sh/guides/your-first-devmachine/),
or hand [the agent setup page](https://mydevmachine.sh/agent-setup/) to your
coding agent and let it do the work.

## What you get

- **One workspace per project.** Projects never see each other's files or
  tokens. One client per workspace, each with its own stack and AI
  subscription.
- **Agents ready to work.** Claude Code, Codex, Pi, opencode, Antigravity CLI,
  Kimi Code and Cline, each installed by one package. Sessions run in tmux, so they
  survive a closed terminal.
- **Your site online in one command.** `devmachine expose add acme 3000 --host app.example.com`
  puts a workspace's port on your domain with HTTPS. `dns` points the name
  for you through Cloudflare or Hostinger, or a provider you write.
- **Private until you publish.** Reach an app through `tunnel` or Tailscale.
  Only what you `expose` is public.
- **Locked down from the start.** No passwords, no workspace runs as root, and
  the CLI checks the server's identity on every connection.
- **Many machines, one config.** A VPS, a homelab box, your own computer
  (`self: true`), a Lima VM. Every command takes a workspace name; you never
  type an address.
- **Logins and secrets.** Sign in once and share the login across
  workspaces, or keep it separate. Tokens go to `devmachine secrets`, never into a file you commit.
- **Move in minutes.** Your whole setup is a folder you can keep in git.
  Point it at a new machine and `sync`.

## The model

```
your computer ── ssh ──▶ machine (VPS, home server, Lima VM, or this computer)
                          ├── workspace acme    → claude-code, docker, github login
                          ├── workspace globex  → codex, postgres
                          └── workspace umbrella → wuzapi, published at api.example.com
```

- A [**machine**](https://mydevmachine.sh/concepts/machines-and-workspaces/)
  is a server the CLI reaches over SSH, or your own computer.
- A [**workspace**](https://mydevmachine.sh/concepts/machines-and-workspaces/)
  is one account on one machine. Its name is all you type.
- A [**package**](https://mydevmachine.sh/concepts/packages/) adds one thing:
  a coding agent, Docker, a GitHub login, a reverse proxy. `sync` installs
  the packages your configuration asks for. Browse the ready-made ones at
  [mydevmachine/packages](https://github.com/mydevmachine/packages), or
  [write your own](https://mydevmachine.sh/reference/package-format/).

Your configuration lives in `~/.config/devmachine`, a folder you control.
Nothing personal is part of this repository, and nothing runs on the server
between commands.

## Devmachine for Mac

<p align="center">
  <img src=".github/readme/01-sessions.webp" alt="Four sessions of the acme workspace in a grid in the Devmachine app: Claude Code, Codex, tests and a dev server." width="100%">
</p>

An optional macOS app on top of the same CLI and configuration. Open sessions
in a grid, see the network map of your machines, publish a port in a few
clicks, and watch RAM, disk and agent usage from one window.

[See the app](https://mydevmachine.sh/app/) ·
[Download the .dmg](https://github.com/mydevmachine/app-releases/releases/latest/download/Devmachine.dmg) ·
`brew install --cask mydevmachine/tap/devmachine-app`

## Guides

Real setups, done in a few steps, by hand or as one request to your agent.
[See them all](https://mydevmachine.sh/guides/).

| Agents | Web apps | Workspaces and networking |
| --- | --- | --- |
| [Keep your sessions running](https://mydevmachine.sh/guides/keep-sessions-running/) | [Express site with TLS](https://mydevmachine.sh/guides/express-site-with-tls/) | [One consultant, three startups](https://mydevmachine.sh/guides/one-consultant-three-startups/) |
| [Claude Code from your phone](https://mydevmachine.sh/guides/claude-code-remote-control/) | [Docker site on 8080](https://mydevmachine.sh/guides/docker-site-on-8080/) | [Try it on your computer with Lima](https://mydevmachine.sh/guides/a-local-vm-with-lima/) |
| [Claude 24/7 in Telegram](https://mydevmachine.sh/guides/claude-24-7-in-telegram/) | [Home server without port forwarding](https://mydevmachine.sh/guides/expose-home-server-without-port-forwarding/) | [Point your domain with Cloudflare](https://mydevmachine.sh/guides/cloudflare-dns/) |
| [OpenClaw in its own sandbox](https://mydevmachine.sh/guides/openclaw-in-its-own-sandbox/) | [wuzapi as your own package](https://mydevmachine.sh/guides/wuzapi-as-your-own-package/) | [Your own Tailscale with Headscale](https://mydevmachine.sh/guides/headscale/) |
| [Hermes Agent in its own sandbox](https://mydevmachine.sh/guides/hermes-agent-in-its-own-sandbox/) | [Agno AgentOS with a control plane](https://mydevmachine.sh/guides/agno-agentos-with-control-plane/) | [Logins and secrets](https://mydevmachine.sh/guides/logins-and-secrets/) |

## Documentation

All of it is at **[mydevmachine.sh](https://mydevmachine.sh/)**. The same
pages live in [`docs/`](docs/index.md), and an LLM can read them in one file
at [llms-full.txt](https://mydevmachine.sh/llms-full.txt).

- [Getting started](https://mydevmachine.sh/getting-started/) — install, connect a server, see it work.
- [Concepts](https://mydevmachine.sh/concepts/machines-and-workspaces/) — machines, workspaces, packages, DNS, publishing, private networks.
- [How it works](https://mydevmachine.sh/how-it-works/ssh/) — the reasons behind choices that are not obvious from the outside.
- [Commands](https://mydevmachine.sh/reference/commands/) — every command, its flags and its output.
- [Troubleshooting](https://mydevmachine.sh/troubleshooting/) — errors you may see, and what they really mean.
- [Changelog](https://mydevmachine.sh/changelog/) — what changed in each release.

## From source

```sh
git clone https://github.com/mydevmachine/devmachine.git
cd devmachine && make build && ./devmachine help
```

Working on the CLI itself: [development](docs/development.md) and
[releasing](docs/releasing.md). Issues and pull requests are welcome.

## Licence

MIT. See [LICENSE](LICENSE).
