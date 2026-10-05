---
description: "Try devmachine on a virtual machine on your own computer before you pay for a server."
category: Get started
level: Beginner
needs:
  - "A Mac or Linux computer"
  - "Lima"
related:
  - your-first-devmachine.md
  - keep-sessions-running.md
  - ssh-or-mosh.md
---
# Try it on your own computer first

Run a virtual machine on your computer and use it exactly like a VPS: set it
up, make workspaces, add packages, open them. Nothing to buy, and you can
throw it away when you are done.

**You need:** a Mac or Linux computer and [Lima](https://lima-vm.io), which
runs the virtual machine: `brew install lima`. The VM runs Ubuntu 24.04 with
2 CPUs, 4 GiB of memory and a 20 GiB disk; the first one downloads the image,
which takes a few minutes.

Need a bigger one? Give the size when you create it:

```
devmachine machines create-local lab --cpus 4 --memory 8 --disk 40 --add
```

Memory and disk are in GiB. The CPUs cannot be more than your computer has,
and the memory must be less than it has. devmachine sets the size only when
it creates the VM: to change it, delete the VM and create it again.

## Before you start: machine, skills, workspace

A local VM is not a bought server, so it is made and added differently, but
everything after that is the same.

```
curl -fsSL https://mydevmachine.sh/install.sh | sh
devmachine machines create-local sandbox --add
devmachine skills add
devmachine workspaces new acme --machine sandbox
devmachine sync
```

`--add` creates the machine and adds it in one go: it logs in with the
public root password, installs a key, proves it, turns password login off
and writes the machine to your configuration — a new one if you had none.

Without `--add`, `create-local` only creates the machine, and prints its
address, its port and the root password:

```
sandbox      127.0.0.1                    port 60022  admin: root
It has no key on it yet, and the root password is "devmachine".
```

The machine arrives the way a bought server does: reachable as root with a
password, and no key yet. The password is public on purpose — this machine
holds nothing real. Run `devmachine setup` if this is your first machine;
if you already have one configured, run `devmachine machines add` instead,
next to it. Either way, answer with what `create-local` printed: address
`127.0.0.1`, login `root`, the port it showed, and the password when asked.
Everything else is the same as on a real server — see
[getting started](../getting-started.md) for what each command does.

## By hand

### 1. Use it like a VPS

```
devmachine packages add claude-code --workspace acme
devmachine sync --machine sandbox
devmachine ssh acme
```

`--machine sandbox` is needed only when you have more than one machine.

### 2. What does not work on a local machine

The internet cannot reach a virtual machine on your computer, so `dns`,
HTTPS certificates and `expose` do not work there — see
[publishing](../concepts/publishing.md). Everything else does. To see an app
you run in the VM, use a tunnel instead of a URL:

```
devmachine tunnel acme 3000
```

### 3. Stop it, start it, throw it away

```
devmachine machines stop sandbox
devmachine machines start sandbox
devmachine machines delete-local sandbox
```

`delete-local` destroys the VM and everything in it. Then take it out of
your configuration: first its workspaces with `devmachine workspaces rm
acme`, then the machine with `devmachine machines rm sandbox` — it refuses
while a workspace still points at it.

## With your agent

Open a session on your own computer (`devmachine skills add` taught it the
CLI) and say:

```text
Create a local devmachine VM called sandbox and set it up as a machine,
then create a workspace acme on it with claude-code.
```

The agent runs `machines create-local sandbox --add`, which asks nothing:
the VM was made a moment ago and answers only on your computer, so there is
no fingerprint for you to check. It asks you where the machine is first and
passes `--location`, `local` unless you name another place. After that, it creates the workspace, adds
the package, and asks before running `sync`. The steps it follows are in
[set up devmachine with a coding agent](../agent-setup.md).

**Check it:** `devmachine doctor --machine sandbox` passes every line after
setup.

Source: [Lima](https://lima-vm.io/docs/)
