# Settings

Every variable the published packages accept, and what each one defaults to.

A setting is written `<package>.<name>` under a machine's or a workspace's
`settings:`, and it reaches the recipe as the variable it already reads.
**A setting is a default somebody overrode, not a new mechanism** — the recipe
cannot tell the difference and does not have to.

```yaml
machines:
  - name: main
    packages: [base, caddy]
    settings:
      base.timezone: Europe/Lisbon
      caddy.email: someone@example.com
```

Or without opening the file:

```
devmachine workspaces edit alice --set zsh.tmux_config=false
devmachine machines edit main --set caddy.email=someone@example.com
```

A setting for a package the target does not install is refused. A typo in a
package name would otherwise be silent: the value would reach nothing, the
recipe would keep its default, and the machine would not be what the
configuration says it is.

**This page is generated** by `make settings`. Do not edit it by hand.

| Setting | What it does | Default |
| --- | --- | --- |
| `antigravity.home` | Where the account's home is. | `<the account's own home>` |
| `base.hostname` | The machine's hostname. Empty leaves the one it already has. | *(empty)* |
| `base.swap` | Size of a swapfile at /swapfile, such as 8G. Empty leaves the machine alone. Nothing on macOS, which manages its own swap. | *(empty)* |
| `base.timezone` | The machine's timezone, as tzdata spells it. Empty leaves whatever the machine came with. | *(empty)* |
| `base.upgrade` | Upgrade every package already installed. Off, because that is the owner's decision and not a side effect of installing base tools. On Arch Linux it is also the only thing that refreshes the package lists. Nothing on macOS. | `false` |
| `caddy.email` | The address the certificate authority writes to about an expiring certificate. Empty means an anonymous account. | *(empty)* |
| `caddy.local_certs` | Sign certificates locally instead of asking Let's Encrypt. For a machine no name resolves to — a test VM, a private network — where the ACME challenge can never succeed. Off, because a certificate nobody else trusts is not what a public site wants. | `false` |
| `caddy.site` | Serve a one-page site straight from Caddy, no container behind it. It answers 200, which proves the name, the certificate and the proxy in one request. Off leaves the machine serving only what other packages add. | `true` |
| `caddy.site_domain` | The name the one-page site answers to, with its own certificate. Empty serves it on port 80 at the machine's address, over plain HTTP, because no public authority signs a certificate for a bare address. It creates no DNS record: pointing the name is `devmachine dns add`. | *(empty)* |
| `caddy.source` | Where Caddy is installed from. `auto` uses Caddy's apt repository and, when apt cannot check that repository's signature, installs the pinned GitHub release instead. `apt` uses only the repository and fails when it cannot be checked. `github` always installs the pinned GitHub release and removes the repository. Debian and Ubuntu only: elsewhere Caddy comes from the system's own repository, and on macOS from Homebrew or MacPorts. | `auto` |
| `caddy.version` | The Caddy release installed from GitHub, by `source: github` or by the `auto` fallback. The `.deb` is checked against the release's own sha512 checksums before it is installed. The apt repository, and every system other than Debian and Ubuntu, ignores it. | `2.11.4` |
| `claude-code.diff_sidebar` | Open the /diff panel. Unset leaves whatever Claude Code has. It is a preference Claude Code keeps in its own state file, so it is amended only where that file, and that preference, already exist. | *(none)* |
| `claude-code.env` | Environment variables every Claude Code session runs with, as a map. It is how a model-specific or terminal-specific workaround is turned on without this package having an opinion about it. | `map[]` |
| `claude-code.expanded_todos` | Show the task list under the footer. Unset leaves whatever Claude Code has, and the same condition applies. | *(none)* |
| `claude-code.home` | Where the account's home is. | `<the account's own home>` |
| `claude-code.remote_control_at_startup` | Connect every session to Remote Control as it opens, instead of waiting for somebody to type /rc. | `false` |
| `claude-code.session_name_prefix` | What each Remote Control session is called in the phone app. Empty means the workspace's name, which is what tells two workspaces' sessions apart. | *(empty)* |
| `claude-code.status_line` | The command Claude Code runs to draw its status line. Empty leaves the status line alone. It runs outside a login shell, so give it an absolute path. | *(empty)* |
| `claude-plugins.claude` | Where the Claude Code CLI is. It is installed into the account's own ~/.local/bin, so only a workspace that put it somewhere else says so. | `<the home>/.local/bin/claude` |
| `claude-plugins.home` | Where the account's home is. | `<the account's own home>` |
| `claude-plugins.marketplace` | Where the plugins come from: a GitHub repository written owner/name, a git URL, or a path on the machine. Empty installs nothing. | *(empty)* |
| `claude-plugins.plugins` | Which plugins to install, each written <plugin>@<marketplace>. The marketplace half is the name the marketplace gives itself, which is what Claude Code installs and reports against. | `[]` |
| `claude-plugins.update` | Re-fetch the marketplace and update every plugin on each run. It is off by default because it cannot tell whether anything moved, so a run with it on always reports a change. | `false` |
| `claude-remote-control.home` | Where the account's home is. | `<the account's own home>` |
| `claude-remote-control.restart_seconds` | How long to wait before reconnecting. The server gives up on its own after about ten minutes with no network, so the unit restarts for good. On macOS it is launchd's ThrottleInterval. | `30` |
| `claude-remote-control.session_name` | What this session is called in the phone app. Empty means the workspace's name, which is what tells two workspaces' sessions apart. | *(empty)* |
| `claude-remote-control.working_directory` | The directory a remote session starts in. | `<the home>/dev` |
| `cline.home` | Where the account's home is. | `<the account's own home>` |
| `codex.home` | Where the account's home is. | `<the account's own home>` |
| `dev.gh_version` | Which GitHub CLI release to install. It is a release asset rather than a distribution package because most distributions do not carry gh at all. | `2.63.2` |
| `dev.home` | Where the account's home is. | `<the account's own home>` |
| `dev.node_version` | Which Node mise installs globally. | `lts` |
| `devmachine-app.github_hosts` | GitHub Enterprise hosts the context panel resolves pull requests on, each as `{host, proxy}` with `proxy` optional. github.com always works; this adds more. | `[]` |
| `docker.vm_cpus` | macOS only. The CPUs each workspace's VM gets. | `2` |
| `docker.vm_memory` | macOS only. The memory each workspace's VM gets, in GiB. | `2` |
| `docker.workspaces` | macOS only: the workspaces that get a Docker VM of their own. Empty means every workspace on the machine. | `[]` |
| `fail2ban.bantime` | How long a ban lasts, in seconds. | `3600` |
| `fail2ban.ignoreip` | The addresses that are never banned, space separated. Loopback only by default: a wider range exempts everyone who shares it. | `127.0.0.1/8 ::1` |
| `fail2ban.maxretry` | How many failures from one address before it is banned. | `5` |
| `firewall.http` | Open 80 and 443. A machine that hosts nothing can turn this off. | `true` |
| `firewall.mosh_interface` | The one interface mosh's UDP range is opened on. Empty means it is not opened at all, which keeps the range off a public edge. | *(empty)* |
| `firewall.mosh_ports` | The UDP range mosh is given on that interface. | `60000:61000` |
| `git-key.home` | Where the account's home is. | `<the account's own home>` |
| `glab.home` | Where the account's home is. | `<the account's own home>` |
| `glab.version` | Which GitLab CLI release to install. It is a release asset rather than a distribution package because no distribution carries glab. | `1.111.0` |
| `hostinger.zones` | DNS zones this provider may manage. Set this when the token has DNS permission without Domains portfolio permission; an empty list asks the account portfolio instead. | `[]` |
| `kimi-code.home` | Where the account's home is. | `<the account's own home>` |
| `mac-brew.casks` | Homebrew casks to install. | `[]` |
| `mac-brew.formulae` | Homebrew formulae to install. | `[]` |
| `mac-brew.prefix` | Where Homebrew lives. Left unset, /opt/homebrew on Apple Silicon and /usr/local on an Intel Mac. | `/opt/homebrew` |
| `mac-brew.taps` | Homebrew taps to add. | `[]` |
| `mac-brew.trusted_casks` | Casks to trust, as full names `<tap>/<cask>`. Recent Homebrew refuses to load a cask from an unofficial tap until it is trusted, per item on purpose rather than per tap. | `[]` |
| `mac-brew.trusted_formulae` | Formulae to trust, as full names `<tap>/<formula>`, for the same reason as trusted_casks. | `[]` |
| `mac-mise.bin` | Where the mise binary is. | `~/.local/bin/mise` |
| `mac-mise.tools` | Tools to install globally, as `mise use -g` takes them, e.g. `node@lts`. | `[]` |
| `mac-ports.ports` | MacPorts ports to install, e.g. `git` or `ripgrep`. | `[]` |
| `mise.home` | Where the account's home is. | `<the account's own home>` |
| `opencode.home` | Where the account's home is. | `<the account's own home>` |
| `pi.home` | Where the account's home is. | `<the account's own home>` |
| `sentry.home` | Where the account's home is. | `<the account's own home>` |
| `ssh_hardening.service` | What systemd calls sshd. Empty means the name this distribution family uses, which is `ssh` on Debian and Ubuntu and `sshd` elsewhere. macOS ignores it: launchd starts sshd for each connection, so there is nothing to reload. | *(empty)* |
| `tailscale.exit_node` | Advertise this machine as an exit node. Off unless asked for. | `false` |
| `tailscale.login_server` | The control server `devmachine login tailscale` joins. Empty means Tailscale's own; a URL means your own, such as Headscale. | *(empty)* |
| `workspace.admin_home` | The home of the account the CLI provisions with. Whatever reaches that account over SSH is what reaches this workspace. On macOS it is the admin login's own home, read from the system. | `/root` |
| `workspace.git_email` | The address on this workspace's commits. | *(empty)* |
| `workspace.git_name` | The name on this workspace's commits. | *(empty)* |
| `workspace.groups` | Extra groups the account joins. The docker group is one of them and it is effectively root, so nobody joins it by accident. | `[]` |
| `workspace.home` | Where the account's home is. It is read from the account; a new account gets /home/<the account> unless this says otherwise. On macOS a new account always gets /Users/<the account>. | `<the account's own home>` |
| `workspace.known_hosts` | The hosts whose SSH host key is trusted in advance, so the first clone does not stop to ask a question nobody is there to answer. | `[github.com]` |
| `workspace.repos` | Repositories cloned into ~/dev/<name>, once. A clone is never pulled, reset or removed afterwards, and a folder already at ~/dev/<name> is left as it is. A clone over SSH waits for the workspace's key, which a package such as git-key delivers after this one runs, so it happens on the second pass of the same sync. Each entry: `branch`: The branch checked out on the clone. Left out, the remote's default; `name` (required): The folder under ~/dev. Letters, digits, dot, dash and underscore; `url` (required): Where it is cloned from, over SSH or HTTPS. | `[]` |
| `workspace.shell` | The login shell. Empty means the system's default for a new account (useradd's on Linux, /bin/bash on macOS), and the package that installs a shell is the one that sets it. | *(empty)* |
| `workspace.sign_commits` | Sign every commit and rebase, once a package such as git-key sets up a key. | `true` |
| `zsh.home` | Where the account's home is. | `<the account's own home>` |
| `zsh.tmux_auto_attach` | Open a tmux session on every SSH login, so a dropped connection loses nothing. A second connection while the first is live gets a session of its own instead of a second view of the same one. | `true` |
| `zsh.tmux_config` | Write the account's ~/.tmux.conf. Turn it off to keep a config of your own. | `true` |
