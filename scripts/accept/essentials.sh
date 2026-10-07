#!/usr/bin/env bash
# The essentials acceptance proof: a machine fresh from `setup` gets the
# essentials, and once synced the macOS app's package answers with no other
# step. Before that, `sync --tags devmachine-app` locks only what it ran, so a
# plain `sync --check` still sees the rest as pending. The VM is created with a
# size other than the default, to prove create-local's --cpus, --memory and
# --disk reach Lima.
set -uo pipefail

ROOT=$(cd "$(dirname "$0")/../.." && pwd)
source "$ROOT/scripts/accept/common.sh"

if [ -z "${ACCEPT_RUN_DIR:-}" ]; then
  make_run_dir >/dev/null
  ACCEPT_OWNS_RUN_DIR=1
else
  ACCEPT_OWNS_RUN_DIR=0
fi

VM="devmachine-accept-${ACCEPT_RUN_ID:?ACCEPT_RUN_ID is required}-essentials"
require_accept_vm "$VM" || exit 1

SCENARIO_DIR=$(mktemp -d "$ACCEPT_RUN_DIR/essentials.XXXXXX")
DEVMACHINE_CONFIG=$(mktemp -d "$SCENARIO_DIR/config.XXXXXX")
SCENARIO_LOG_DIR="$SCENARIO_DIR/logs"
mkdir -p "$SCENARIO_LOG_DIR"
export DEVMACHINE_CONFIG

cleanup_essentials() {
  destroy_accept_vm "$VM" || true
  rm -rf "$SCENARIO_DIR"
  if [ "$ACCEPT_OWNS_RUN_DIR" -eq 1 ]; then
    rm -rf "$ACCEPT_RUN_DIR"
  fi
}
trap cleanup_essentials EXIT INT TERM

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

"$DEVMACHINE_ACCEPT_BIN" machines create-local "$VM" --cpus 3 --memory 5 --disk 25 \
  >"$SCENARIO_LOG_DIR/create.log" 2>&1 || die "could not create $VM"
CLOUD_INIT=$(limactl shell "$VM" -- cloud-init status --wait 2>&1 || true)
printf '%s\n' "$CLOUD_INIT" > "$SCENARIO_LOG_DIR/cloud-init.log"
case "$CLOUD_INIT" in
  *"status: done"*) ;;
  *) die "$VM did not finish cloud-init: $CLOUD_INIT" ;;
esac

contains "$(cat "$SCENARIO_LOG_DIR/create.log")" "Size: 3 CPUs, 5 GiB memory, 25 GiB disk." \
  "create-local reports the size it was given" || true
equals "$(limactl shell "$VM" -- nproc 2>&1)" "3" "the VM boots with the CPUs --cpus asked for" || true
equals "$(limactl list --format '{{.Memory}}' "$VM")" "$((5 * 1024 * 1024 * 1024))" \
  "the VM boots with the memory --memory asked for" || true
equals "$(limactl shell "$VM" -- lsblk -bdno SIZE /dev/vda 2>&1)" "$((25 * 1024 * 1024 * 1024))" \
  "the VM boots with the disk --disk asked for" || true

PORT=$(limactl list --format '{{.SSHLocalPort}}' "$VM")
[ -n "$PORT" ] || die "$VM never received an SSH port"
SETUP=$(printf '%s\n' "$VM" "127.0.0.1" "root" "$PORT" "example.com" "y" "1" "devmachine" "1" "n" \
  | "$DEVMACHINE_ACCEPT_BIN" setup --no-aliases 2>&1) || die "could not set up $VM: $SETUP"
printf '%s\n' "$SETUP" > "$SCENARIO_LOG_DIR/setup.log"
contains "$SETUP" "what the macOS app reads" "setup says the essentials carry what the macOS app reads" || true
contains "$(cat "$DEVMACHINE_CONFIG/config.yml")" "essentials" "setup writes the machine with the essentials" || true

mkdir -p "$DEVMACHINE_CONFIG/packages"
for package in essentials base git firewall ssh_hardening caddy devmachine-app workspace; do
  [ -d "$PACKAGES/packages/$package" ] || die "package fixture is missing: $package"
  cp -R "$PACKAGES/packages/$package" "$DEVMACHINE_CONFIG/packages/"
done

"$DEVMACHINE_ACCEPT_BIN" sync --tags devmachine-app --yes >"$SCENARIO_LOG_DIR/sync-tagged.log" 2>&1 \
  || die "the tagged sync failed; see $SCENARIO_LOG_DIR/sync-tagged.log"
LOCK=$(cat "$DEVMACHINE_CONFIG/packages.lock")
printf '%s\n' "$LOCK" > "$SCENARIO_LOG_DIR/lock-tagged.yml"
contains "$LOCK" "name: devmachine-app" "the tagged sync locks the package it ran" || true
refutes "$LOCK" "name: caddy" "the tagged sync does not lock caddy, which never ran" || true

# A dry run on a machine without caddy fails at caddy's reload handler, since
# check mode installs nothing to reload; the recap still counts what is pending.
CHECK=$("$DEVMACHINE_ACCEPT_BIN" sync --check 2>&1)
printf '%s\n' "$CHECK" > "$SCENARIO_LOG_DIR/check.log"
PENDING=$(printf '%s' "$CHECK" | sed -n 's/.*: ok=[0-9]* *changed=\([0-9]*\).*/\1/p' | tail -1)
[ "${PENDING:-0}" -gt 0 ] && pass "after the tagged sync, sync --check still reports the rest as pending ($PENDING changes)" \
  || fail "after the tagged sync, sync --check still reports the rest as pending" || true
CADDY=$("$DEVMACHINE_ACCEPT_BIN" run --machine "$VM" -- "command -v caddy || echo absent" 2>&1)
equals "$CADDY" "absent" "the tagged sync installed no caddy" || true

"$DEVMACHINE_ACCEPT_BIN" workspaces new acme --packages workspace --yes \
  >"$SCENARIO_LOG_DIR/workspace.log" 2>&1 || die "could not add acme"
"$DEVMACHINE_ACCEPT_BIN" sync --yes >"$SCENARIO_LOG_DIR/sync.log" 2>&1 \
  || die "the full sync failed; see $SCENARIO_LOG_DIR/sync.log"
LOCK=$(cat "$DEVMACHINE_CONFIG/packages.lock")
printf '%s\n' "$LOCK" > "$SCENARIO_LOG_DIR/lock.yml"
contains "$LOCK" "name: caddy" "the full sync locks caddy" || true

STATS=$("$DEVMACHINE_ACCEPT_BIN" run --package devmachine-app --machine "$VM" -- stats 2>"$SCENARIO_LOG_DIR/stats.err")
printf '%s\n' "$STATS" > "$SCENARIO_LOG_DIR/stats.json"
SHAPE=$(printf '%s' "$STATS" | python3 -c 'import json,sys; s=json.load(sys.stdin); print(s["memory"]["total_bytes"] > 0, s["docker"]["available"], s["docker"]["containers"])' 2>/dev/null)
equals "$SHAPE" "True False []" "stats answers on a machine without Docker" || true

CONTEXT=$("$DEVMACHINE_ACCEPT_BIN" run --package devmachine-app --workspace acme -- context --project /home/acme --no-resolve 2>"$SCENARIO_LOG_DIR/context.err")
printf '%s\n' "$CONTEXT" > "$SCENARIO_LOG_DIR/context.json"
PARSED=$(printf '%s' "$CONTEXT" | python3 -c 'import json,sys; print(type(json.load(sys.stdin)).__name__)' 2>/dev/null)
equals "$PARSED" "dict" "context answers for a workspace with one JSON document" || true

LOGS=$("$DEVMACHINE_ACCEPT_BIN" run --package devmachine-app --machine "$VM" -- caddy-logs --lines 5 2>&1)
printf '%s\n' "$LOGS" > "$SCENARIO_LOG_DIR/caddy-logs.log"
contains "$LOGS" "caddy" "caddy-logs reads Caddy's journal" || true

scenario_done 14 "essentials"
