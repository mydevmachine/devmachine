#!/usr/bin/env bash
# The coding agents acceptance proof: a workspace with every coding-agent
# package syncs, each CLI answers its version from the PATH a login shell
# gets, the agents that do not read ~/.agents/skills find each skill through
# a link, and a second sync installs nothing again. Nobody logs in: a login is a
# person's job, and the proof is that the CLI is there for them to do it.
set -uo pipefail

ROOT=$(cd "$(dirname "$0")/../.." && pwd)
source "$ROOT/scripts/accept/common.sh"

if [ -z "${ACCEPT_RUN_DIR:-}" ]; then
  make_run_dir >/dev/null
  ACCEPT_OWNS_RUN_DIR=1
else
  ACCEPT_OWNS_RUN_DIR=0
fi

VM="devmachine-accept-${ACCEPT_RUN_ID:?ACCEPT_RUN_ID is required}-agents"
require_accept_vm "$VM" || exit 1

SCENARIO_DIR=$(mktemp -d "$ACCEPT_RUN_DIR/coding-agents.XXXXXX")
DEVMACHINE_CONFIG=$(mktemp -d "$SCENARIO_DIR/config.XXXXXX")
SCENARIO_LOG_DIR="$SCENARIO_DIR/logs"
mkdir -p "$SCENARIO_LOG_DIR"
export DEVMACHINE_CONFIG

cleanup_coding_agents() {
  destroy_accept_vm "$VM" || true
  rm -rf "$SCENARIO_DIR"
  if [ "$ACCEPT_OWNS_RUN_DIR" -eq 1 ]; then
    rm -rf "$ACCEPT_RUN_DIR"
  fi
}
trap cleanup_coding_agents EXIT INT TERM

die() {
  echo "$1" >&2
  exit 1
}

if [ -z "${DEVMACHINE_ACCEPT_BIN:-}" ] || [ ! -x "$DEVMACHINE_ACCEPT_BIN" ]; then
  die "DEVMACHINE_ACCEPT_BIN must name the local acceptance CLI"
fi
if [ -z "${PACKAGES:-}" ] || [ ! -d "$PACKAGES/packages" ]; then
  die "PACKAGES must name a packages checkout"
fi

AGENTS="antigravity opencode pi kimi-code cline codex"

"$DEVMACHINE_ACCEPT_BIN" machines create-local "$VM" \
  >"$SCENARIO_LOG_DIR/create.log" 2>&1 || die "could not create $VM"
CLOUD_INIT=$(limactl shell "$VM" -- cloud-init status --wait 2>&1 || true)
printf '%s\n' "$CLOUD_INIT" > "$SCENARIO_LOG_DIR/cloud-init.log"
case "$CLOUD_INIT" in
  *"status: done"*) ;;
  *) die "$VM did not finish cloud-init: $CLOUD_INIT" ;;
esac

PORT=$(limactl list --format '{{.SSHLocalPort}}' "$VM")
[ -n "$PORT" ] || die "$VM never received an SSH port"
SETUP=$(printf '%s\n' "$VM" "127.0.0.1" "root" "$PORT" "example.com" "y" "1" "devmachine" "1" "n" \
  | "$DEVMACHINE_ACCEPT_BIN" setup --no-aliases 2>&1) || die "could not set up $VM: $SETUP"
printf '%s\n' "$SETUP" > "$SCENARIO_LOG_DIR/setup.log"

mkdir -p "$DEVMACHINE_CONFIG/packages"
for package in essentials base git firewall ssh_hardening caddy devmachine-app workspace zsh mise dev devmachine-skills $AGENTS; do
  [ -d "$PACKAGES/packages/$package" ] || die "package fixture is missing: $package"
  cp -R "$PACKAGES/packages/$package" "$DEVMACHINE_CONFIG/packages/"
done

PACKAGE_LIST="workspace,zsh,mise,dev,devmachine-skills,$(printf '%s' "$AGENTS" | tr ' ' ',')"
"$DEVMACHINE_ACCEPT_BIN" workspaces new acme --packages "$PACKAGE_LIST" --yes \
  >"$SCENARIO_LOG_DIR/workspace.log" 2>&1 || die "could not add acme"
"$DEVMACHINE_ACCEPT_BIN" sync --yes >"$SCENARIO_LOG_DIR/sync.log" 2>&1 \
  || die "the sync failed; see $SCENARIO_LOG_DIR/sync.log"

for cli in "agy --version" "opencode --version" "pi --version" "kimi --version" "cline --version" "codex --version"; do
  OUT=$("$DEVMACHINE_ACCEPT_BIN" run --workspace acme -- "$cli" 2>&1)
  printf '%s\n' "$OUT" >> "$SCENARIO_LOG_DIR/versions.log"
  if printf '%s' "$OUT" | grep -Eq '[0-9]+\.[0-9]+\.[0-9]+'; then
    pass "$cli answers in the workspace"
  else
    fail "$cli answers in the workspace (got: $OUT)"
  fi
done

for dir in .gemini/antigravity-cli/skills .cline/skills .agents/skills; do
  SKILL=$("$DEVMACHINE_ACCEPT_BIN" run --workspace acme -- "test -f ~/$dir/use-devmachine/SKILL.md && echo found" 2>&1)
  equals "$SKILL" "found" "the use-devmachine skill is readable from ~/$dir" || true
done

CREDENTIALS=$("$DEVMACHINE_ACCEPT_BIN" credentials list 2>&1)
printf '%s\n' "$CREDENTIALS" > "$SCENARIO_LOG_DIR/credentials.log"
for name in antigravity opencode pi kimi cline codex; do
  contains "$CREDENTIALS" "$name" "credentials list names the $name login" || true
done

AGAIN=$("$DEVMACHINE_ACCEPT_BIN" sync --yes 2>&1)
printf '%s\n' "$AGAIN" > "$SCENARIO_LOG_DIR/sync-again.log"
for task in "Install the Antigravity CLI" "Install the opencode CLI" "Install Pi" "Install the Kimi Code CLI" "Install the Cline CLI" "Install the Codex CLI"; do
  CHANGED=$(printf '%s\n' "$AGAIN" | grep -A1 "TASK \[.*$task" | grep -c '^changed' || true)
  equals "$CHANGED" "0" "a second sync leaves \"$task\" alone" || true
done

scenario_done 21 "coding-agents"
