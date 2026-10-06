# What a new machine starts with

`setup` and `machines add` write a new machine with one package,
`essentials`. It installs nothing itself; it pulls in the packages almost
every server wants:

| Package | What it gives the machine |
| --- | --- |
| `base` | the everyday tools, and a shared tmux setup |
| `git` | git |
| `firewall` | a firewall that lets SSH, HTTP and HTTPS in |
| `ssh_hardening` | password logins kept off on every sync |
| `caddy` | Caddy, to publish sites with HTTPS |
| `devmachine-app` | what the macOS app reads from the machine |

`--no-essentials` starts the machine with none of it.

## On a Mac

`essentials` runs only on Linux: a firewall, SSH hardening and Caddy have
no macOS version, and `sync` refuses the package on a Mac. So when the
machine turns out to be a Mac, `setup` and `machines add` start it with
`base` and `devmachine-app` instead, and with the package manager package
(`mac-brew` or `mac-ports`) that installs Ansible there. They print one
line that says so:

```
studio is a Mac and essentials runs only on Linux, so it starts with base and devmachine-app instead.
```

The system is not known until the CLI connects, which is after the
machine's packages are chosen, so the swap happens at that moment. It only
replaces the default: with `--no-essentials` the Mac starts with the
package manager package alone. Only what the pinned package release has
goes in, as with `essentials` itself.

## Why a default, and not a question

Nearly everyone wants these. A question at setup is one more thing a new
user has to understand before they have seen anything work, and a list they
must remember to add is a list they forget. So the choice is to leave them
out, not to put them in.

## Why Docker is not in it

Docker is large and runs a daemon all the time, and plenty of machines never
run a container. Add it with
`devmachine packages add docker` when you want it.

## Why the macOS app's package is in it

The macOS app does not read a machine over plain SSH. It calls three small
scripts through `devmachine run --package devmachine-app`:

- `context` — the sessions running in a workspace, for the session list;
- `stats` — memory, disk, load, containers and ports, for the health view;
- `caddy-logs` — the tail of Caddy's log.

Without the package, the app finds nothing to call and shows no sessions.
Nothing in the app tells you a package is missing on the server side, and
the app may be installed months after the machine was set up — long after
anybody remembers there was a package to add. Shipping it with every new
machine means the app just works, whenever it arrives.

The cost is low. The three scripts only read: they change nothing on the
machine, open no port, and run nothing in the background. They only run
when something calls them. They also work on a machine without Docker
(`stats` reports no containers instead of failing).

## A machine set up before

A machine set up before `essentials` held `devmachine-app` gets it on its
next `sync` after `devmachine packages pin` moves it to a package release
that has it. A machine started with `--no-essentials` does not: add it with
`devmachine packages add devmachine-app --machine <name>` if you use the app.
