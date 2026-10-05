# Development

```
make build        compile to ./devmachine
make test         the whole suite
make test-vps     the suite against a real machine
make test-vps-sudo  the same, with an admin login that reaches root through sudo
make lint         golangci-lint
make fmt          gofmt
make surface      regenerate SURFACE.txt
```

## Testing against a real machine

Tests that touch a machine run against a throwaway VPS on your computer, never
against a real server. It needs Lima (`brew install lima`).

```
make test-vps
```

That starts the VM, authorizes a throwaway key and runs everything against it.
The VM reproduces a freshly bought server: root over SSH with a password and no
key installed.

The VM is made by the CLI itself — `devmachine machines create-local`, see
[the reference](reference/commands.md#machines) — so the harness and the
feature are one thing and cannot drift apart.

One difference is deliberate: the script then authorizes a key through
`limactl shell`, and `create-local` never does. The integration tests need a
machine they can already reach without running `setup` first, while
`create-local` has to leave the machine exactly as a bought one arrives, or the
trust bootstrap is never exercised by the thing that runs on every test.

Driving it by hand:

```
scripts/fake-vps.sh up      start it and authorize a key
scripts/fake-vps.sh env     the values a test needs, as exports
scripts/fake-vps.sh ssh     a shell on it
scripts/fake-vps.sh down    destroy it
```

`DEVMACHINE_FAKE_VPS_DISTRO=arch` makes `up` create an Arch VM instead
(`create-local --distro arch`). Give it its own name with
`DEVMACHINE_FAKE_VPS`, so it does not take the place of the default one:

```
DEVMACHINE_FAKE_VPS=fakearch DEVMACHINE_FAKE_VPS_DISTRO=arch scripts/fake-vps.sh up
```

Without the VM those tests **skip**, so `make test` stays green anywhere —
including CI, which has no VM.

`internal/local` goes further: where Lima is installed, its test creates a
machine and proves that root gets in with the password and no key does. That
costs a boot, so `go test -short` leaves it out.

## Release acceptance harness

The release acceptance harness exercises the CLI against disposable local
machines. It requires `bash`, `git`, `ssh`, `limactl`, `python3`, and `curl`,
plus a local checkout of the packages repository. Run the full harness with:

```
make accept
```

By default the packages checkout is `~/dev/packages`; set `PACKAGES` when it
lives elsewhere:

```
PACKAGES=/absolute/path/to/packages make accept
```

To run one scenario while developing it, pass its name to the harness directly:

```
scripts/accept/run.sh setup-git
scripts/accept/run.sh v05-packages-credentials-dns
scripts/accept/run.sh v06-expose-tunnel
scripts/accept/run.sh workspace-secrets
scripts/accept/run.sh upload
scripts/accept/run.sh download
scripts/accept/run.sh network-package
```

`network-package` uses a stand-in network package whose scripts are trivial,
so the real Tailscale never runs; it proves `login`, `resolve`, and an alias
that connects through `ssh-proxy` and falls through a dropped address.

`headscale` is the same proof against the real thing, and is not in the
default list because it boots three VMs (1 GiB, 2 GiB and 2 GiB of memory):
a Headscale server, a box set up as a fresh server, and a client that plays
your computer. The CLI is built for Linux and runs only inside the client,
which joins Headscale with the real Tailscale client, so the computer running
the harness keeps its own Tailscale and SSH configuration untouched. It needs
`go` on that computer, and installs the packages checkout's `tailscale`
package as a local package. Run it by name:

```
scripts/accept/run.sh headscale
```

`HEADSCALE_VERSION` picks the Headscale release (0.29.4 by default); its
`.deb` is checked against the release's `checksums.txt`. The three VMs share
Lima's `user-v2` network, because `vzNAT` keeps two VMs from reaching each
other.

`coding-agents` is not in the default list either: it downloads every
coding-agent CLI the packages offer (Antigravity, opencode, Pi, Kimi Code
and Cline, close to a gigabyte). It syncs one workspace with all of them,
checks each one answers its version, checks Antigravity's and Cline's
skill folders reach the shared skills, and checks a second sync installs
nothing again. Nobody logs in. Run it by name:

```
scripts/accept/run.sh coding-agents
```

Every run creates uniquely named `devmachine-accept-*` VMs and removes them by
default, including when a scenario fails. For investigation only,
`KEEP_ACCEPT_VM=1 make accept` retains its disposable acceptance VM; delete it
when the investigation is over.

The Go tests and the harness set `DEVMACHINE_KEYCHAIN=off`, so neither
ever writes to, or prompts about, the developer's own keychain. A test
that reaches the real keychain is a bug: with `HOME` pointed at a
temporary folder, macOS answers with a "Keychain Not Found" dialog that
offers to reset the login keychain.

The harness must never target a real machine. It may create, use, and delete
only its own uniquely named disposable acceptance VMs; do not point it at a
server, an existing local VM, or any shared infrastructure.

## Rules this repository holds itself to

**Everything written to disk is English.** Code, comments, commit messages,
documentation, fixtures.

**No real infrastructure or personal data, ever.** No real machine names,
hostnames, IPs, domains, emails or tokens — not in code, tests, fixtures, docs
or commit messages. Fixtures use `acme` and `bob`, `example.com`, and the
documentation IP ranges `203.0.113.x` and `198.51.100.7`.

`scripts/check-no-real-data.sh` enforces this against a pattern list kept
outside the repository, and runs in CI. It has already caught a real leak.

**A test must not depend on the machine running it.** A test that passes on a
laptop with an SSH agent and fails on a runner without one is testing the
environment. Generate what the test needs.

**Documentation is part of the change.** See below.

## Documentation

`docs/` is the manual, and it is updated in the same commit as the behaviour it
describes. Specifically:

- A new command or flag goes in [the reference](reference/commands.md).
- A decision that is **not obvious from the outside** goes in `how-it-works/`.
  If explaining something took a paragraph in a pull request or a conversation,
  that paragraph belongs here.
- An error somebody could meet goes in [troubleshooting](troubleshooting.md),
  with what it really means — not just what it says.
- Every new page is linked from [the index](index.md). A page nothing links to
  does not exist.
- A guide in `guides/` starts with frontmatter: `description`, `category`
  (Get started, Workspaces, Agents, Web apps, Networking or Security),
  `level` (Beginner, Intermediate or Advanced), `needs` and `related` (two
  or three other guides). No time estimate: nobody can say how long a guide
  takes for someone else. The website builds its cards and chips from it,
  and `scripts/check-docs.sh` checks it. Copy the block from any guide.
- Two ways to do the same step (Tailscale or Headscale, say) go in tabs:
  `<!-- tabs -->`, then one `####` heading per tab, then `<!-- /tabs -->`,
  with a blank line around each marker. The website draws them as tabs;
  on GitHub they read as plain subheadings. No deeper heading inside a tab.
- An image goes next to its page (`guides/images/`) with a relative link.
  The website copies it.

The reason for the second rule: the hard-won parts of this project are not the
code, they are the reasons. Why one key is offered and never the whole agent,
why a dropped address is not an error, why a command refuses to guess. That
knowledge is worth more written down than rediscovered.

## Specs and plans

Design documents and implementation plans are **not** in this repository. They
live in the operator's own repo and are gitignored here.

`docs/` is for people using the CLI. That is a different audience and a
different document.

## Coverage

```
make cover
```

The floor is **80%**, and it is a floor rather than a target: it is set at a
number the tree already clears, so it catches a regression instead of teaching
everybody to ignore a red gate. Raise it when the number earns it.

`internal/remote` is the low one, and honestly so — half of it only runs against
a real machine, and those tests skip without one. `make test-vps` is what
exercises them.
