# Machines and workspaces

A **machine** is a server, with its own address and login. A **workspace**
is one person's account on a machine — usually the thing you actually work
in day to day.

```
devmachine workspaces new acme --machine main
devmachine sync
```

## Your computer

**Your computer** is the one you run `devmachine` on — usually your
laptop. The VPS is never "your computer" in this manual. You can also
list your computer itself as a machine, with `self: true`, so the CLI can
manage things on it directly, with no address or SSH involved:

```yaml
machines:
  - name: mac
    self: true
```

This is different from `machines create-local`, which makes a small
virtual machine that lives on your computer but is reached like a normal
remote server, with its own address and key. See
[Your computer as a machine](../how-it-works/your-computer-as-a-machine.md)
for when to use which.

A workspace can't run on a self machine — a workspace is an account
reached over SSH, and a self machine has neither an account nor SSH.

## Several machines, several workspaces

Each workspace names the machine it runs on:

```yaml
machines:
  - name: main
    hosts: [203.0.113.10]
  - name: sandbox
    hosts: [198.51.100.7]
    port: 2222
    key: /keys/sandbox

workspaces:
  - name: acme
    machine: main
  - name: bob
    machine: sandbox
```

```
devmachine ssh acme     # lands on main
devmachine ssh bob       # lands on the sandbox
```

Moving `bob` to another machine is one line of configuration; the command
you type never changes. `machine:` can be left out only when you have one
machine — with several, it's required.

## The account on the machine

A workspace's account uses its own name by default: `acme` owns the user
`acme`. Give it a different name with `user:`, for when that name is
already taken:

```yaml
workspaces:
  - name: bob
    machine: sandbox
    user: bob-dev
```

The `workspace` package creates the account: its home, which nobody else can
read, the keys you log in with, its groups and its git identity. Every
workspace gets it, first, whether its list names it or not, so a package
that forgets to wait for the account can never run before the account
exists. It cannot be removed; `devmachine workspaces rm` removes the whole
workspace instead. Its settings, such as `workspace.groups` and
`workspace.git_email`, work like any other package's.

## Making and removing a workspace

```
devmachine workspaces new acme
devmachine workspaces new bob --like acme
devmachine sync
```

`new` only writes a line in `config.yml` — `sync` is what actually
creates the account. A new workspace gets the packages listed in
`defaults.workspace`, unless you pass `--packages` or `--like <name>` to
copy another workspace's list.

```
devmachine workspaces rm acme
```

removes the entry from `config.yml`. **The account, its files, and its
home folder stay on the machine** — remove those yourself if you want them
gone.

## Reaching a workspace by name

```
devmachine aliases --write
ssh acme-devmachine
```

writes an SSH shortcut for every workspace into `~/.ssh/config`, inside a
marked block it can safely rewrite without touching anything else there:

```
# >>> devmachine — generated, do not edit
Host acme-devmachine
    HostName main
    User acme
    ProxyCommand /opt/homebrew/bin/devmachine ssh-proxy main %p
    HostKeyAlias main-devmachine
# <<< devmachine
```

`ProxyCommand` asks devmachine for the machine's address each time ssh
connects, so the shortcut keeps working when a private network goes up or
down.

`HostKeyAlias` keeps SSH from complaining when the same machine is reached
at two different addresses — it tells SSH the two addresses are the same
known machine. A `-pub` shortcut appears only when there's a second,
fallback address to use.
