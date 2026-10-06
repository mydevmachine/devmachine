---
description: "Fork an open source Rails project into a workspace, run its tests and view its dev server privately."
category: Workspaces
level: Intermediate
needs:
  - "A VPS (tested on Debian and Ubuntu)"
  - "A GitHub account"
related:
  - one-consultant-three-startups.md
  - keep-sessions-running.md
  - shared-skills-for-every-workspace.md
---
# Contribute to a Ruby on Rails project

A workspace set up to build and test a real open source Rails app:
[RubyUI](https://github.com/ruby-ui/ruby_ui), a component library with a
Rails docs site. This guide forks it, installs Ruby with mise, runs its
test suite, and starts its dev server — viewed privately with `devmachine
tunnel`, with Claude Code on hand to help.

**You need:** a VPS (tested on Debian and Ubuntu), and a GitHub account.

## Before you start: machine, skills, workspace

```
curl -fsSL https://mydevmachine.sh/install.sh | sh
devmachine setup
devmachine skills add
devmachine workspaces new components
devmachine sync
```

Already have a machine? Skip `setup`. Already have the workspace? Skip the
last two. See [getting started](../getting-started.md) for what each command
does.

## By hand

### 1. Sign in to GitHub and add Claude Code

```
devmachine login gh
devmachine packages add claude-code --workspace components
devmachine sync
```

`gh` is already installed — it comes with every workspace's `dev` package.
`login gh` opens the sign-in in a real terminal; you finish it in your
browser. See [logins and secrets](logins-and-secrets.md) if this is your
first login.

### 2. Fork and clone RubyUI

```
devmachine ssh components
gh repo fork ruby-ui/ruby_ui --clone
cd ruby_ui
```

`gh repo fork --clone` forks the repo to your GitHub account, using the
login from step 1, and clones your fork.

### 3. Install Ruby with mise

```
mise use -g ruby@3.4
ruby -v
```

RubyUI's CI runs on Ruby 3.3 and 3.4. mise compiles the version you ask
for; the `base` package (part of `essentials`) already installed the
headers and libraries — `zlib1g-dev`, `libssl-dev`, `libyaml-dev`, and the
rest — that compiling Ruby needs, so this step does not fail on a missing
header.

### 4. Install dependencies and run the gem's tests

```
cd gem
bundle install
bundle exec rake
```

`rake` here runs RubyUI's test suite and its `standardrb` linting in one
step. This is the part of the repo you are most likely changing: the
components themselves.

### 5. Run the docs app

```
cd ../docs
bundle install
pnpm install
bin/dev
```

The docs app is a Rails 8 site that also serves as RubyUI's demo: it reads
the gem straight from the path next to it, so a change to a component shows
up in the docs app without reinstalling anything. `bin/dev` starts it on
port 3000 and keeps running after you close the terminal — the SSH login
was inside tmux.

### 6. View it privately

Back on your own computer:

```
devmachine tunnel components 3000
```

Open `localhost:3000`. Nothing is published — no DNS record, no open port —
so this is safe to leave running while you work on a fork nobody else
should see yet.

## With your agent

Open a session on your own computer (`devmachine skills add` taught it the
CLI) and say:

```text
In my devmachine workspace components, fork and clone ruby-ui/ruby_ui with my
GitHub login, install Ruby 3.4 with mise, run the gem's test suite, and
start the docs dev server on port 3000.
```

The agent runs `gh repo fork --clone`, `mise use -g ruby@3.4`, `bundle
install`, `bundle exec rake`, and `bin/dev` over SSH, in that order. It
needs `devmachine login gh` already done — a browser sign-in is not
something it can do for you — and if it is not, the agent tells you to run
it first. Once the dev server is up, it opens the tunnel with `devmachine
tunnel components 3000` and tells you to open `localhost:3000`.

**Check it:** `bundle exec rake` in `gem/` passes with no failures, and
`localhost:3000` (through the tunnel) shows the RubyUI docs and component
previews.

Source: [ruby-ui/ruby_ui](https://github.com/ruby-ui/ruby_ui),
[ruby-ui/ruby_ui — CONTRIBUTING.md](https://github.com/ruby-ui/ruby_ui/blob/main/CONTRIBUTING.md)
