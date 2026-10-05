# devmachine documentation

Code from anywhere. Any machine you own, an isolated workspace for every
project, no lock-in.

Three commands turn a server nobody has logged into into one that is yours:

```
devmachine setup      connect to the server, lock it down, get it ready
devmachine sync       apply your packages to the server
devmachine workspaces new acme && devmachine sync
```

No server yet? `devmachine machines create-local dev` makes one on your own
computer, while you wait for a real one.

## Start here

- [Getting started](getting-started.md) — install devmachine, connect it to a
  server, and see that it works.
- [Set up with a coding agent](agent-setup.md) — hand this page to your coding
  agent. It asks what it needs and does the setup, except the steps only you
  can do.
- [Day to day](day-to-day.md) — ask your agent from any session, and when to
  open one in the configuration folder instead.
- [Upgrade](upgrade.md) — keep the CLI, the packages and the skills up to
  date.
- [Changelog](changelog.md) — what changed in each release.

## Guides

Real setups, done in a few steps, by hand or with your agent. Start at [the
list](guides/index.md).

- [Your first devmachine, explained](guides/your-first-devmachine.md)
- [Try it on your own computer first](guides/a-local-vm-with-lima.md)
- [Express site with TLS](guides/express-site-with-tls.md)
- [Docker site on 8080](guides/docker-site-on-8080.md)
- [Expose a home server without port forwarding](guides/expose-home-server-without-port-forwarding.md)
- [Claude Code, controlled from your phone](guides/claude-code-remote-control.md)
- [Start Claude from your phone](guides/start-claude-from-your-phone.md)
- [OpenClaw, in its own sandbox](guides/openclaw-in-its-own-sandbox.md)
- [Keep your sessions running](guides/keep-sessions-running.md)
- [A Claude that never sleeps](guides/claude-that-never-sleeps.md)
- [One consultant, three startups](guides/one-consultant-three-startups.md)
- [Hermes Agent, in its own sandbox](guides/hermes-agent-in-its-own-sandbox.md)
- [Agno AgentOS with a control plane](guides/agno-agentos-with-control-plane.md)
- [wuzapi as your own package](guides/wuzapi-as-your-own-package.md)
- [Log in with your 1Password SSH key](guides/log-in-with-1password.md)
- [Pi as your coding agent](guides/pi-coding-agent.md)
- [opencode as your coding agent](guides/opencode.md)
- [Antigravity CLI as your coding agent](guides/antigravity-cli.md)
- [Kimi Code as your coding agent](guides/kimi-code.md)
- [Cline as your coding agent](guides/cline.md)
- [SSH or mosh?](guides/ssh-or-mosh.md)
- [Claude 24/7 in Telegram](guides/claude-24-7-in-telegram.md)
- [Logins and secrets](guides/logins-and-secrets.md)
- [Point your domain with Cloudflare](guides/cloudflare-dns.md)
- [Contribute to a Ruby on Rails project](guides/ruby-on-rails.md)
- [Your own Tailscale with Headscale](guides/headscale.md)
- [One set of skills for every workspace](guides/shared-skills-for-every-workspace.md)

## Concepts

- [Machines and workspaces](concepts/machines-and-workspaces.md) — the two
  ideas everything else builds on.
- [Configuration](concepts/configuration.md) — where your setup lives and how
  devmachine finds it.
- [Packages](concepts/packages.md) — what a server or workspace can get
  installed on it.
- [Credentials](concepts/credentials.md) — signing in to tools and services,
  and what devmachine can do for you.
- [DNS](concepts/dns.md) — pointing a domain at your server.
- [Publishing](concepts/publishing.md) — `expose` and `tunnel`: showing
  something running in a workspace to the outside world, or just to you.
- [Reaching your server](concepts/reaching-your-server.md) — the public address,
  a private network with Tailscale, and private access to your apps.
- [Tailscale and other private networks](concepts/private-networks.md) —
  reach your server privately, and how the options compare.
- [Agent Skills](concepts/agent-skills.md) — teaching a coding agent to use
  devmachine.

## How it works

The reasoning behind decisions that are not obvious from the outside. Read
these when something behaves in a way that surprises you.

- [SSH: logging in and knowing it is your server](how-it-works/ssh.md) — why
  the CLI shares one key and never your whole agent, and how it checks it is
  talking to the right server, every time.
- [Addresses and fallback](how-it-works/addresses-and-fallback.md) — how a
  server with more than one address is reached, how a private network's
  entry becomes an address, and why SSH aliases resolve when you connect.
- [Choosing a target](how-it-works/choosing-a-target.md) — why a command asks
  which server, instead of guessing.
- [Why nothing is embedded](how-it-works/why-nothing-is-embedded.md) — why
  devmachine downloads packages instead of shipping them.
- [Why a login cannot be automated](how-it-works/why-a-login-cannot-be-automated.md)
  — the first question everybody asks.
- [Sharing a login](how-it-works/sharing-a-login.md) — copying one sign-in
  into several workspaces, and keeping one workspace's login separate.
- [DNS providers](how-it-works/dns-providers.md) — what each built-in provider
  does, and its rough edges.
- [Setting up a server for the first time](how-it-works/trust-bootstrap.md) —
  how `setup` makes sure it is talking to your server, and locks it down.
- [Your computer as a machine](how-it-works/your-computer-as-a-machine.md) —
  using your own computer instead of a server.
- [Where a machine is](how-it-works/machine-location.md) — why a machine
  with no location is `external`, and why `create-local` writes `local`.
- [Versioning your configuration](how-it-works/versioning-your-configuration.md)
  — keeping your setup in git safely.
- [Why a published site lives in the configuration](how-it-works/published-sites.md)
  — why `expose` writes to your configuration, not straight to the server.
- [What sync removes](how-it-works/what-sync-removes.md) — what `sync` cleans
  up, and what it leaves alone.
- [How an upload lands](how-it-works/uploads.md) — why `upload` renames
  files, never overwrites one, and stays inside the home.
- [How a download lands](how-it-works/downloads.md) — why `download` keeps
  names, never overwrites a file here, and sends a folder as one archive.
- [What a new machine starts with](how-it-works/what-a-new-machine-starts-with.md)
  — what `essentials` holds, and why the macOS app's package is in it.
- [Updating](how-it-works/updating.md) — why `update` stops before your
  machines, hands over to the new CLI, and goes through Homebrew when
  Homebrew installed it.

## Reference

- [Commands](reference/commands.md) — every command, its flags and its
  output.
- [The package format](reference/package-format.md) — how to write a
  `package.yml`.
- [The DNS provider contract](reference/dns-provider-contract.md) — how to
  write a DNS provider.
- [The network package contract](reference/network-package-contract.md) —
  how to make a private network reachable through a package.
- [Settings](reference/settings.md) — every option the built-in packages
  accept.

## Fixing things

- [Troubleshooting](troubleshooting.md) — errors you may see, and what to do
  about them.

## Contributing

- [Development](development.md) — building devmachine and testing it against
  a real server.
- [Releasing](releasing.md) — how a new version reaches Homebrew.
