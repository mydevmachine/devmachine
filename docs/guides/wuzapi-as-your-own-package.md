---
description: "Write a WhatsApp API once as your own package, then run it on two machines."
category: Web apps
level: Advanced
needs:
  - "One or two Linux VPS (tested on Debian and Ubuntu)"
  - "A domain"
related:
  - docker-site-on-8080.md
  - shared-skills-for-every-workspace.md
  - cloudflare-dns.md
---
# wuzapi, as your own package

[wuzapi](https://github.com/asternic/wuzapi) is a WhatsApp REST API: one
container, one admin token, and every number you connect gets its own
session. Instead of installing it by hand on every machine that needs it,
this guide writes it as a [package](../concepts/packages.md) — one file
kept in your own configuration, that any machine or workspace can add.

**You need:** two Linux VPS, tested on Debian and Ubuntu (or one, if you skip the second
workspace), and a domain such as `example.com`.

## Before you start: machine, skills, workspace

```
curl -fsSL https://mydevmachine.sh/install.sh | sh
devmachine setup
devmachine skills add
devmachine workspaces new whatsapp
devmachine sync
```

Already have a machine? Skip `setup`. Already have the workspace? Skip the
last two. See [getting started](../getting-started.md) for what each command
does.

## By hand

### 1. Write the package

```
devmachine packages new wuzapi --scope workspace --into ~/.config/devmachine/packages
```

This writes a skeleton that already passes `packages validate` at
`<config>/packages/wuzapi/`. `--scope workspace` is right here: a WhatsApp
number belongs to one workspace, not the whole machine, so wuzapi runs once
per workspace that adds it, same as the `dev` or `zsh` package.

Replace `package.yml` with:

```yaml
format: 1
name: wuzapi
scope: workspace
category: Messaging
summary: wuzapi, a WhatsApp REST API, running as a Docker container of its own.

needs: [workspace, docker]

requires:
  cli: ">= 0.4.0"

credentials:
  - name: wuzapi_admin_token
    kind: secret
    scope: workspace
    env: WUZAPI_ADMIN_TOKEN

variables:
  home:
    summary: Where the account's home is.
    default: /home/<the account>
  port:
    summary: The port wuzapi listens on, on 127.0.0.1 only.
    default: 8080
  image_tag:
    summary: Which asternic/wuzapi image tag to run.
    default: latest
```

`needs: [workspace, docker]` means the `docker` package runs on the machine
first. The credential is `kind: secret`: devmachine keeps the value in your
computer's keychain, and `credentials push` writes it into the workspace, at
`~/.devmachine/wuzapi_admin_token/env`. The role reads it from there and hands
it to the container without printing it (`no_log`), so it is never in a file
you'd commit. The container is named after the workspace, so two workspaces on
one machine can each run their own. wuzapi itself needs no database: it falls
back to SQLite when none is set, which is what this package leans on to
stay simple — see the [package format](../reference/package-format.md).

`defaults/main.yml`:

```yaml
---
devmachine_wuzapi_home: "/home/{{ devmachine_workspace.user }}"
devmachine_wuzapi_port: 8080
devmachine_wuzapi_image_tag: latest
```

`tasks/main.yml`:

```yaml
---
- name: Install the Docker SDK for Python
  ansible.builtin.package:
    name: python3-docker
    state: present

- name: Read the admin token credentials push delivered
  ansible.builtin.shell: |
    set -a
    . "{{ devmachine_wuzapi_home }}/.devmachine/wuzapi_admin_token/env"
    printf '%s' "$WUZAPI_ADMIN_TOKEN"
  register: devmachine_wuzapi_token
  changed_when: false
  no_log: true

- name: Run wuzapi
  community.docker.docker_container:
    name: "wuzapi-{{ devmachine_workspace.user }}"
    image: "asternic/wuzapi:{{ devmachine_wuzapi_image_tag }}"
    state: started
    restart_policy: unless-stopped
    ports:
      - "127.0.0.1:{{ devmachine_wuzapi_port }}:8080"
    env:
      WUZAPI_ADMIN_TOKEN: "{{ devmachine_wuzapi_token.stdout }}"
  no_log: true
```

### 2. Check it

```
devmachine packages validate ~/.config/devmachine/packages/wuzapi
```

This reports every problem at once, so fix everything it names before
moving on.

### 3. Add it to the first workspace

```
devmachine packages add docker
devmachine packages add wuzapi --workspace whatsapp
devmachine workspaces edit whatsapp --set 'workspace.groups=[docker]'
devmachine secrets set whatsapp/wuzapi_admin_token
devmachine credentials push
devmachine sync
```

Caddy came with `setup` (it's in the `essentials` package). If your machine
was set up with `--no-essentials`, or before CLI v0.7.14, add it too:
`devmachine packages add caddy`. `secrets set` asks for the token without
echoing it, so it never reaches your shell history. `credentials push`
delivers it to the machine; `sync` installs Docker and runs the wuzapi
container.

### 4. Publish it

```
devmachine expose add whatsapp 8080 --host wa.example.com --publish
```

### 5. A second machine, a second workspace

```
devmachine machines add
devmachine workspaces new whatsapp-eu --machine backup
devmachine packages add docker --machine backup
devmachine packages add wuzapi --workspace whatsapp-eu --machine backup
devmachine workspaces edit whatsapp-eu --machine backup --set 'workspace.groups=[docker]'
devmachine secrets set whatsapp-eu/wuzapi_admin_token
devmachine credentials push --machine backup
devmachine sync --machine backup
devmachine expose add whatsapp-eu 8080 --host wa-eu.example.com --publish --machine backup
```

This is what "shared between machines" means: `wuzapi` lives once, in
`<config>/packages/wuzapi/`. It never got written twice — `whatsapp-eu` on
`backup` used the exact same file `whatsapp` on `main` did. Fix a bug in
`tasks/main.yml` once, and the next `sync` on either machine picks it up.

### 6. Back it up

```
devmachine setup git
```

Your configuration, package included, is now a git repository devmachine
can push somewhere private. See [versioning your configuration](../how-it-works/versioning-your-configuration.md).

## With your agent

Open a session on your own computer (`devmachine skills add` taught it the
CLI) and say:

```text
Write a devmachine package called wuzapi that runs asternic/wuzapi as
a Docker container, scope workspace, with a secret credential for
WUZAPI_ADMIN_TOKEN. Add it to workspace whatsapp, sync, and expose it
at wa.example.com.
```

The agent writes the package files, runs `packages validate` until it is
clean, then `packages add`, `sync`, and `expose add`. You still type the
admin token into `secrets set` yourself, and approve `sync` when it asks —
running a container that answers to WhatsApp is worth reading before you
say yes.

**Check it:** open `https://wa.example.com/login` — wuzapi's own page for
connecting a number — and scan the QR code it shows with the WhatsApp app
on the phone you want connected.

Source: [wuzapi](https://github.com/asternic/wuzapi)
