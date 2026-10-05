# Releasing

## Releasing everything, in order

A release touches four repositories. Do them in this order.

### 1. CLI (this repo)

Before tagging: `main` clean and pushed, CI green, then run the two gates
that need a real machine:

```
make test-vps
make accept
```

`make test-vps` starts a throwaway Lima VM named `fakevps` and runs the full
test suite against it. `make accept` runs the CLI acceptance suite. Both must
be green.

Two things that trip people up here:

- An acceptance scenario that rewrites `config.yml` as text must run
  `setup --no-essentials`. Without it, `setup` adds packages to the file
  first, and the text rewrite no longer matches what is there.
- Tests never touch a developer's real git config. They set the
  `GIT_CONFIG_GLOBAL` environment variable to point at a throwaway file
  instead, so a local commit-signing setup can't leak into a test run.

Add the new version at the top of `docs/changelog.md`: what a user can see
changed since the last tag (`git log --format=%s vPREVIOUS..HEAD`), without
tests, CI or internal work. Commit it before the tag, so the release carries
its own entry.

Once both gates are green, tag and push:

```
git tag -s vX.Y.Z -m vX.Y.Z
git push origin vX.Y.Z
```

The release workflow builds the binaries with GoReleaser and updates the
Homebrew formula (see "What a release produces" below). Users get the new
version with:

```
brew update && brew upgrade mydevmachine/tap/devmachine
```

### 2. Packages (`~/dev/packages`)

The packages repo bundles a copy of the CLI's docs, under
`packages/devmachine-skills/skills/*/references`. Its CI compares that copy
against the docs in the **latest CLI release tag**, so sync it right after
any CLI release that changed docs:

```
cd ~/dev/packages
T=$(mktemp -d)
git -C ../devmachine-cli archive vX.Y.Z docs | tar -x -C $T
scripts/sync-skill-references.sh $T/docs
```

Commit and push, wait for packages CI to go green, then tag the packages
repo as its own release step. That process is documented in that repo; it
isn't repeated here.

### 3. Site (`~/dev/docs`, publishes mydevmachine.sh)

The site rebuilds on its own on push to its `main`, and on a schedule every
6 hours. After CLI docs change on this repo's `main`, trigger a rebuild by
hand instead of waiting:

```
gh workflow run deploy.yml -R mydevmachine/docs
```

A deploy that starts within seconds of the triggering push can still race
and pick up the previous commit. After it finishes, check that the new or
changed page returns HTTP 200 with the new content, and rerun the workflow
if it doesn't.

The site footer shows the latest CLI tag and the latest packages tag, read
at build time, so it reflects steps 1 and 2 only once this step runs.

### 4. Users update

```
brew upgrade
devmachine packages pin
devmachine skills update
devmachine sync --check
devmachine sync
```

## The credential in place today

A GitHub App called `devmachine-release`, owned by the `mydevmachine`
organisation, installed on `homebrew-tap` only, with `Contents: Read and write`
and no other permission. Its App ID and private key are already stored as
secrets on `mydevmachine/devmachine`, and neither expires.

Regenerate the private key only if it leaks: the original download cannot be
repeated, but a new key can be generated and the old one revoked.

The rest of this page is how to rebuild that from scratch.

## One-time setup: the tap credential

The token GitHub injects into a workflow can only write to the repository the
workflow runs in. The Homebrew formula lives in another repository
(`mydevmachine/homebrew-tap`), so the release needs a credential that reaches it.

A **GitHub App** provides that. Unlike a personal access token it does not
expire, so a release cannot break a year from now because nobody rotated
anything.

### Create the App

1. In the organisation's settings → Developer settings → GitHub Apps → **New
   GitHub App**.
2. Name it something like `devmachine release`. Homepage URL can be the repo.
3. Uncheck **Webhook → Active**. It receives nothing.
4. Permissions → Repository permissions → **Contents: Read and write**. Nothing
   else.
5. Create it, then note the **App ID**.
6. **Generate a private key** and download the `.pem`.
7. **Install App** → the organisation → **Only select repositories** →
   `homebrew-tap`.

### Store the credentials

```
gh secret set TAP_APP_ID --repo mydevmachine/devmachine
gh secret set TAP_APP_PRIVATE_KEY --repo mydevmachine/devmachine < path/to/key.pem
```

Organisation secrets work too, and are worth it if more repositories will
publish to the tap:

```
gh secret set TAP_APP_ID --org mydevmachine --visibility all
```

### Check it

```
gh secret list --repo mydevmachine/devmachine
```

Both names should be listed. Without them the release workflow fails at the
token step, before building anything.

## What a release produces

- `tar.gz` archives for darwin and linux, amd64 and arm64
- `checksums.txt`
- release notes from the commit subjects, with `docs:`, `test:` and `chore:`
  left out
- an updated formula in the tap, installing `devmachine` and `advm`

## Verify

```
brew install mydevmachine/tap/devmachine
devmachine version
```
