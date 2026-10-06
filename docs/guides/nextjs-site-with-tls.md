---
description: "Build a Next.js app in a workspace, run its production server, and serve it at your own domain with HTTPS."
category: Web apps
level: Beginner
needs:
  - "A Linux VPS (tested on Debian and Ubuntu)"
  - "A domain"
related:
  - express-site-with-tls.md
  - fastapi-with-tls.md
  - keep-sessions-running.md
---
# Next.js app at your own domain

Create a Next.js app in a workspace, build it for production, and put it on
the internet at `app.example.com`, with HTTPS that renews itself. At the end
it also comes back on its own after the server restarts.

The same steps work for a Nest.js API; see [Nest.js instead](#nestjs-instead)
at the end.

**You need:** a Linux VPS (tested on Debian and Ubuntu), and a domain such as `app.example.com`
pointed at it (or a DNS provider package installed, so `expose add` points it
for you — see [DNS](../concepts/dns.md)).

## Before you start: machine, skills, workspace

```
curl -fsSL https://mydevmachine.sh/install.sh | sh
devmachine setup
devmachine skills add
devmachine workspaces new acme
devmachine sync
```

Already have a machine? Skip `setup`. Already have the workspace? Skip the
last two. See [getting started](../getting-started.md) for what each command
does.

## By hand

Node is already there: every new workspace gets the `dev` package, which
installs the Node LTS (with `npm` and `npx`) through mise. If you made the
workspace with your own `--packages` list and left `dev` out, add it first
on your computer: `devmachine packages add dev --workspace acme`, then
`devmachine sync`.

Caddy came with `setup` (it's in the `essentials` package), and it will
answer this site and get its own certificate. If your machine was set up
with `--no-essentials`, add it first: `devmachine packages add caddy`, then
`devmachine sync`.

### 1. Create the app

Inside the workspace:

```
devmachine ssh acme
cd ~/dev
npx create-next-app@latest web --yes --use-npm
```

The first time, `npx` asks to install `create-next-app`: answer `y`.
`--yes` takes the defaults for every other question (TypeScript, Tailwind,
ESLint, the App Router), and `--use-npm` installs the packages with npm.

### 2. Build it and start it

Still inside the workspace:

```
cd ~/dev/web
npm run build
npm start
```

`npm run build` makes the optimized production build, and `npm start` serves
it on port 3000. Do not serve the site with `npm run dev`: the dev server
is slower, rebuilds pages on each request, and shows its error overlay to
visitors.

### 3. Expose the port

On your computer:

```
devmachine expose add acme 3000 --host app.example.com --publish
```

`expose add` writes the Caddy route on the machine and reloads Caddy, which
gets the certificate for the host — no `sync` needed. If no DNS provider
package is installed, `expose add` prints the record to create by hand at
your registrar instead of writing it for you.

## With your agent

Open a session on your own computer (`devmachine skills add` taught it the
CLI) and say:

```text
In my devmachine workspace acme, create a Next.js app in ~/dev/web with
create-next-app, build it with npm run build, and run it with npm start on
port 3000 under pm2, so it comes back after a reboot. Then expose port 3000
at app.example.com.
```

The agent runs the same commands over SSH: `create-next-app`, the build,
pm2 and the cron line from [Keep it running](#keep-it-running), then
`expose add` on your computer. You still point `app.example.com` at the
server yourself if no DNS provider package is installed — the agent shows
you the record. And if `setup` has not run yet, you check the fingerprint
yourself; the agent cannot do that part.

**Check it:** on your computer,
`curl -s https://app.example.com | grep -o '<title>[^<]*</title>'` prints
`<title>Create Next App</title>`, with a valid certificate.

## Keep it running

`devmachine ssh acme` opens inside tmux, so `npm start` keeps going after
you close the terminal — see [keep your sessions running](keep-sessions-running.md).
A reboot stops it. To bring it back on its own, run it under
[pm2](https://pm2.keymetrics.io/docs/usage/quick-start/) instead.

First stop the `npm start` from step 2 with `Ctrl-C`. Then, inside the
workspace:

```
npm install -g pm2
cd ~/dev/web
pm2 start "npm start" --name web
pm2 save
(crontab -l 2>/dev/null; echo "@reboot $HOME/.local/share/mise/shims/pm2 resurrect") | crontab -
```

`pm2 save` writes down the apps pm2 runs now, and the cron line starts them
again at every boot. pm2's own `pm2 startup` does not work here: it only
prints a `sudo` command, and a workspace account has no `sudo`. The cron
line does the same job without it. It uses the mise shim, the full path to
`pm2`, because cron runs with almost nothing on its `PATH`.

Once the code lives in a git repository you push to, ship a new version
with:

```
cd ~/dev/web
git pull && npm run build && pm2 restart web
```

`pm2 logs web` shows the app's output, and `pm2 ls` shows whether it is
`online`.

## Nest.js instead

A Nest.js API follows the same flow. Inside the workspace:

```
cd ~/dev
npx @nestjs/cli@latest new api --package-manager npm --no-observe
cd api
npm run build
node dist/main
```

`--package-manager npm` and `--no-observe` answer the two questions
`nest new` would ask. The API listens on port 3000 by default, and
`curl localhost:3000` prints `Hello World!`. Expose it the same way, on your
computer:

```
devmachine expose add acme 3000 --host app.example.com --publish
```

To keep it running, stop `node dist/main` with `Ctrl-C` and run it under pm2
instead, then save and add the same cron line as above:

```
cd ~/dev/api
pm2 start dist/main.js --name api
pm2 save
```

Port 3000 holds one app at a time. To run the Next.js site and the API side
by side, start the API on another port — the generated `src/main.ts` reads
it from `PORT`: `PORT=3001 pm2 start dist/main.js --name api` — and expose
that port at another host, such as `api.example.com`.

## Troubleshooting

**`pm2 start npm --name web -- start` fails with `SyntaxError: Unexpected
identifier 'pipefail'`.** The `npm` that mise installs is a shell script,
and that form makes pm2 run it as JavaScript. `pm2 ls` shows the app as
`errored`. Delete it with `pm2 delete web` and start it as a command
string, as above: `pm2 start "npm start" --name web`.

## Remove it

On your computer, take the site off Caddy:

```
devmachine expose rm app.example.com
```

Then, inside the workspace, stop the app and drop the cron line:

```
pm2 delete web
pm2 save --force
crontab -l | grep -v 'pm2 resurrect' | crontab -
```

Source: [Next.js — Deploying](https://nextjs.org/docs/app/getting-started/deploying),
[create-next-app](https://nextjs.org/docs/app/api-reference/cli/create-next-app),
[NestJS — First steps](https://docs.nestjs.com/first-steps),
[PM2 — Quick start](https://pm2.keymetrics.io/docs/usage/quick-start/)
