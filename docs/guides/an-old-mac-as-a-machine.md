---
description: "Turn an old Mac mini or MacBook, Intel or Apple Silicon, into a machine for your workspaces. No VPS to rent."
category: Get started
level: Beginner
needs:
  - "A Mac with Remote Login on, Intel or Apple Silicon"
  - "An admin account on it"
  - "A Mac or Linux computer to run the CLI"
related:
  - your-first-devmachine.md
  - a-local-vm-with-lima.md
  - expose-home-server-without-port-forwarding.md
---
# An old Mac as a machine

An old Mac mini on a shelf, or a MacBook you replaced, is a good machine to
code on: it is already paid for, it is fast enough for a few workspaces,
and it sits on your own network. devmachine sets it up the same way it
sets up a VPS. Intel and Apple Silicon both work; the CLI reads which one
it is and finds Homebrew or MacPorts in the right place.

**You need:** a Mac you can reach over SSH, an admin account on it, and a
Mac or Linux computer to run the CLI. Tested on macOS 15 on Intel and
macOS 27 on Apple Silicon. Older versions are not tested.

What a Mac does not get: `caddy`, `docker`, `firewall`, `tailscale` and
the other Linux-only packages. A site with HTTPS at your own domain, or
Docker, needs a Linux machine. See [what works on
each](../supported-systems.md#what-works-on-each).

## Before you start: on the Mac

Do these once, sitting at the Mac.

1. **Turn on Remote Login.** System Settings > General > Sharing > Remote
   Login. With "Only these users", leave your admin account in the list;
   `sync` adds each workspace account to it later.
2. **Give the admin account passwordless sudo.** A Mac has no root login,
   so the CLI works through `sudo -n`. In Terminal, with your own account
   name in place of `alice`:

   ```
   echo 'alice ALL=(ALL) NOPASSWD:ALL' | sudo tee /etc/sudoers.d/devmachine-alice
   ```

3. **Keep it awake.** A Mac that sleeps drops every session. On a Mac mini,
   System Settings > Energy: turn on "Prevent automatic sleeping when the
   display is off" and "Start up automatically after a power failure". On
   a MacBook, keep it on power; with the lid closed it sleeps unless an
   external display is connected, so leave the lid open.
4. **Find its address.** System Settings > General > Sharing shows a name
   such as `studio.local` under Local hostname. That works from any
   computer on the same network.

**An old Intel Mac stuck on an old macOS?** Pick MacPorts in the next step.
Homebrew supports only recent macOS versions and builds everything from
source on older ones; MacPorts supports much older releases.

## By hand

### 1. Add the Mac

From your own computer. On your first machine, run `devmachine setup`;
next to a machine you already have, run:

```
devmachine machines add
```

Give it the address (`studio.local`), the admin login (`alice`) and its
password, once. Check the fingerprint it shows against the Mac's. Then it:

- installs a key and proves it;
- asks which accounts keep SSH password login. On a Mac that is every
  account on it, so name the people who still SSH in with a password. The
  login window, `sudo` and Screen Sharing keep their passwords either way.
  See [password login, account by
  account](../how-it-works/password-login.md);
- asks Homebrew or MacPorts, then asks before it installs the Xcode
  Command Line Tools (5 to 10 minutes), the package manager and Ansible;
- starts the machine with `base` and `devmachine-app` in place of
  `essentials`, which runs only on Linux.

Name it `studio` when it asks. Why each step needs its own yes is in [what
a machine needs](../how-it-works/what-a-machine-needs.md).

### 2. Make a workspace

```
devmachine workspaces new acme --machine studio
devmachine packages add claude-code --workspace acme
devmachine sync --machine studio
devmachine ssh acme
```

Each workspace is its own macOS account on the Mac, with its own tools and
logins. `--machine studio` is needed only when you have more than one
machine.

### 3. Reach it away from home

`studio.local` works only on your own network. To reach the Mac from
anywhere, put it on Tailscale. The `tailscale` package is Linux only, so on
a Mac you install Tailscale yourself:

- On a Mac someone logs in to, the [Tailscale
  app](https://tailscale.com/download/mac) is enough.
- On a Mac that sits at the login window, the app does not start. Use the
  open-source `tailscaled`, which starts at boot:
  `brew install tailscale`, then `sudo brew services start tailscale` and
  `sudo tailscale up`. See [Tailscale's macOS
  variants](https://tailscale.com/kb/1065/macos-variants).

Then put its Tailscale name first in its addresses. Open `config.yml`
(`devmachine config path` prints where it is):

```yaml
machines:
  - name: studio
    hosts:
      - tailscale:studio
      - studio.local
```

`<name>` after `tailscale:` is what `tailscale status` lists for the Mac.
Your computer needs Tailscale too, on the same account. devmachine tries
the addresses in order; see [reaching your
server](../concepts/reaching-your-server.md).

### 4. After a restart

With FileVault on, a Mac that restarts waits at its screen for a password
before Remote Login starts. For a restart you plan, run `sudo fdesetup
authrestart` on the Mac: it unlocks the disk once, by itself. After a power
cut, somebody has to type the password at the Mac.

## With your agent

Open a session on your own computer (`devmachine skills add` taught it the
CLI) and say:

```text
Add my Mac at studio.local as a devmachine machine called studio. Log in as
alice; it has Remote Login on and passwordless sudo. Use MacPorts. Then
create a workspace acme on it with claude-code.
```

The agent shows you the fingerprint to check and asks before it installs
the Command Line Tools. It never answers that question for you: `--yes`
does not install prerequisites. The steps it follows are in [set up
devmachine with a coding agent](../agent-setup.md).

**Check it:** `devmachine doctor --machine studio` passes every line, and
`devmachine machines show studio` says `Darwin` with the Mac's
architecture.
