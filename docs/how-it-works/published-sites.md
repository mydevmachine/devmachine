# Why a published site lives in the configuration

`expose add` used to write a Caddy file straight onto the machine and
reload Caddy. It worked, but it left the only record of the site on the
machine: rebuild from the configuration, and the sites `expose` had
written never came back.

So the record moved. A route is a field of the workspace that owns it:

    workspaces:
      - name: acme
        routes:
          - {host: app.example.com, port: 8080}

`expose add` writes that line. `sync` renders one file per workspace,
`acme-routes.caddy`, into the `sites.d` folder the `caddy` package
provides, and reloads Caddy when the file changed. `expose rm` deletes
the line (and the name's DNS record, see below). There is one direction — configuration to machine — devmachine
never reads the machine to learn what should be published, only to check
it agrees.

## Why `expose` does not wait for `sync`

`sync` runs the machine's whole play: every package, every workspace. That
takes minutes, and a new subdomain should take seconds. So `expose add`
and `expose rm` also apply the one file the route changes, right after
they record it:

1. Render the workspace's routes file from `config.yml` with the same
   code `sync` uses. Same bytes, same path, owner `root:root`, mode
   `0644` — so the next `sync` finds nothing to change.
2. Write it next to the real file, under a name the Caddyfile's
   `sites.d/*.caddy` import does not match.
3. Set the old file aside (and any one-host file the old `expose` wrote
   for one of these hosts), and move the new one in.
4. Run `caddy validate` on the whole `/etc/caddy/Caddyfile`. The new file
   has to be in place for this: nothing else would check the full
   configuration with it.
5. Reload Caddy (`systemctl reload caddy`, then `caddy reload` if that
   fails), and delete what was set aside. A Mac has no systemd: its Caddy
   runs under launchd, and `caddy reload` reaches it through Caddy's own
   admin endpoint. `sync` reloads it the same way there.

If Caddy refuses the file in step 4 or 5, the old files go back, Caddy
never loads the new one, and the command prints Caddy's own error. Caddy
reads the files only on a reload, so the short time the new file sits in
place changes nothing it serves.

Only that workspace's file is touched, and it is rendered from the whole
configuration, not patched: if the machine's copy has drifted, it comes
back to what `config.yml` says. Other workspaces' files wait for `sync`.

It runs over the same SSH connection `devmachine run` shares, as the
machine's admin login (through `sudo -n` when that login is not root). A
machine that cannot be reached, or a machine with `caddy` in the
configuration but not installed yet, is not an error: the route stays
`pending` and `sync` publishes it. A [self machine](your-computer-as-a-machine.md)
always waits for `sync`.

`--no-apply` skips all of this and only records the route.

## What `sync` takes away

When the configuration owns a host, `sync` also removes the one-host file
the old `expose` wrote for it, since Caddy refuses to reload with one
host named twice. A workspace whose last route was removed gets its
`<workspace>-routes.caddy` removed, not emptied — an empty file would
still serve.

Nothing else in `sites.d` is touched. A file another package added, or
written by hand, is not the configuration's to delete; `expose list`
reports it as `unmanaged` and says how to adopt it.

A file another package adds to `sites.d` — like the one `caddy-sites`
writes — also reloads Caddy when `sync` changes or removes it. Without that
reload, Caddy would keep serving the old file until something else
reloaded it.

## Why DNS is still pointed by `add`

The DNS record is not in the configuration, and `sync` does not write it.
A name that does not resolve fails minutes later, in Caddy's certificate
log, where nobody is looking — so `add` points the name in the same
breath, printing the record to create by hand if the machine is
unreachable.

## Why `rm` removes the record only while it points at the machine

A name left pointing at a machine that no longer serves it still
resolves: visitors reach Caddy, which has no site for it and refuses the
TLS handshake. So `rm` takes the record off too, right after Caddy.

But the record is not in the configuration, so `rm` cannot know it made
it. It works out the record `add` would have made — the serving
machine's first public address, the same one `add` uses — and asks the
provider that holds the zone for the name's A records. Only a record
with exactly that value is deleted. A different value means somebody
repointed the name, maybe at a new server, and deleting it would take
that down. A provider that cannot list the zone deletes nothing: a
delete it cannot check first is a guess.

The DNS step never undoes or blocks the Caddy step. When it fails, the
site is still off Caddy, and the record to remove by hand is printed.

`workspaces destroy` does the same for every site of the workspace, each
on the machine that serves it, after the account is gone: once the
workspace leaves the configuration, nothing would remember those names
pointed at the machine.

When a record is printed instead of written or removed, the line above
it says why: the machine could not be reached (the provider runs on
it), no DNS provider is installed there, or no installed provider holds
the zone.

`--no-apply` means "touch no machine", and a DNS provider runs on the
machine, so `rm --no-apply` leaves the record and prints the `dns rm`
to run later. `add --no-apply` still points the name: a name that
resolves early does no harm, and one that does not stops the
certificate on the next `sync`.

## Publishing through another machine

A machine the internet cannot reach — a VM on your desk, a box behind NAT —
can still have its port published, by a machine that the internet does
reach and that runs Caddy:

    workspaces:
      - name: acme
        machine: lab
        routes:
          - {host: app.example.com, port: 8080, via: edge}

`expose add acme 8080 --host app.example.com --via edge` writes that line.
The name points at `edge`, and `edge`'s Caddy proxies to `lab`. Nothing is
installed on `lab`.

**There is no "main" machine.** Which machine serves a site is written on
the route, so a configuration with three machines and two Caddys means
the same thing to everybody. Without `via`, a route is served by its
workspace's machine, as before. When that machine has no caddy, `expose`
refuses and names the machines that have it, instead of picking one.

**The address comes from `lab`'s `hosts`, not from a VPN.** The CLI
resolves them the way it does to connect (see
[addresses and fallback](addresses-and-fallback.md)) and takes the first
address a network package gave, since that is a private network both
machines can be on; then any other; never a loopback one, which would
name `edge` itself. Tailscale, Headscale, plain WireGuard or a LAN address
all work the same way, because the CLI only ever sees an address.

**It is resolved every time, never stored.** `expose` and `sync` resolve it
when they write `edge`'s file, so a machine that moves is followed by the
next sync. When the address cannot be found from your computer — the
private network is off here — `sync` leaves that workspace's file on `edge`
exactly as it is and says so: the site keeps answering the way it did,
instead of disappearing because of where you ran the command.

**`edge` owns the file.** It is `acme-routes.caddy` in `edge`'s `sites.d`, the
same name sync uses everywhere. A machine with caddy owns the routes file
of every workspace in the configuration: written when it serves one of
its routes, removed otherwise. Taking the last `via: edge` off a route
therefore also takes the file off `edge` on its next sync, and
`workspaces destroy` removes it there before the workspace leaves the
configuration — afterwards nothing would. For the same reason
`workspaces rm` refuses while one of the workspace's sites goes through
another machine, and `machines rm` refuses a machine that still serves a
site for another one: `expose rm` the host first.

**`expose add` checks the path once.** After Caddy has the site, `edge`
tries to open a connection to `lab`'s port. When it cannot, the site stays
published and the command says what to look at, because otherwise every
visitor gets a bare 502: the service listening only on `127.0.0.1`, a
firewall on `lab`, or the two machines not on the same private network.
