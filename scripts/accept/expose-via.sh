#!/usr/bin/env bash
# Publishing a workspace through another machine's Caddy (`expose add --via`).
#
# One VM plays the machine with Caddy. The machine the workspace lives on is
# a second entry whose only address is the VM's own non-loopback address, so
# Caddy proxies across a real network hop without needing a second VM: two
# Lima VMs on the default network cannot reach each other.
set -uo pipefail

ROOT=$(cd "$(dirname "$0")/../.." && pwd)
source "$ROOT/scripts/accept/common.sh"

if [ -z "${ACCEPT_RUN_DIR:-}" ]; then
  make_run_dir >/dev/null
  ACCEPT_OWNS_RUN_DIR=1
else
  ACCEPT_OWNS_RUN_DIR=0
fi

VM="devmachine-accept-${ACCEPT_RUN_ID:?ACCEPT_RUN_ID is required}-via"
require_accept_vm "$VM" || exit 1

SCENARIO_DIR=$(mktemp -d "$ACCEPT_RUN_DIR/via.XXXXXX")
DEVMACHINE_CONFIG=$(mktemp -d "$SCENARIO_DIR/config.XXXXXX")
SCENARIO_LOG_DIR="$SCENARIO_DIR/logs"
mkdir -p "$SCENARIO_LOG_DIR"
export DEVMACHINE_CONFIG

cleanup_via() {
  destroy_accept_vm "$VM" || true
  rm -rf "$SCENARIO_DIR"
  if [ "$ACCEPT_OWNS_RUN_DIR" -eq 1 ]; then
    rm -rf "$ACCEPT_RUN_DIR"
  fi
}
trap cleanup_via EXIT INT TERM

die() {
  echo "$1" >&2
  last=$(ls -t "$SCENARIO_LOG_DIR"/*.log 2>/dev/null | head -1)
  [ -n "$last" ] && { echo "--- last lines of $(basename "$last")" >&2; tail -40 "$last" >&2; }
  exit 1
}

if [ -z "${DEVMACHINE_ACCEPT_BIN:-}" ] || [ ! -x "$DEVMACHINE_ACCEPT_BIN" ]; then
  die "DEVMACHINE_ACCEPT_BIN must name the local acceptance CLI"
fi
if [ -z "${PACKAGES:-}" ] || [ ! -d "$PACKAGES/packages" ]; then
  die "PACKAGES must name a packages checkout"
fi

dm() {
  "$DEVMACHINE_ACCEPT_BIN" "$@"
}

on_vm() {
  dm run --machine "$VM" -- "$1" 2>&1
}

accept_create_local "$VM" \
  >"$SCENARIO_LOG_DIR/create.log" 2>&1 || die "could not create $VM"
CLOUD_INIT=$(limactl shell "$VM" -- cloud-init status --wait 2>&1 || true)
printf '%s\n' "$CLOUD_INIT" > "$SCENARIO_LOG_DIR/cloud-init.log"
[ "$CLOUD_INIT" = "status: done" ] || die "$VM did not finish cloud-init: $CLOUD_INIT"

PORT=$(limactl list --format '{{.SSHLocalPort}}' "$VM")
[ -n "$PORT" ] || die "$VM never received an SSH port"
printf '%s\n' "$VM" "127.0.0.1" "root" "$PORT" "example.com" "y" "1" "devmachine" \
  | dm setup --no-essentials --no-aliases >"$SCENARIO_LOG_DIR/setup.log" 2>&1 \
  || die "could not set up $VM"

mkdir -p "$DEVMACHINE_CONFIG/packages"
for package in base docker firewall caddy; do
  cp -R "$PACKAGES/packages/$package" "$DEVMACHINE_CONFIG/packages/" || die "package fixture is missing: $package"
done

LAB_ADDRESS=$(on_vm "ip -4 -o addr show scope global | awk '{print \$4}' | cut -d/ -f1 | head -1")
case "$LAB_ADDRESS" in
  *.*.*.*) ;;
  *) die "could not find the VM's network address: $LAB_ADDRESS" ;;
esac
echo "  lab is reached at $LAB_ADDRESS"

python3 - "$LAB_ADDRESS" <<'PY' || die "could not configure the via fixture"
import os
import re
import sys
from pathlib import Path

path = Path(os.environ["DEVMACHINE_CONFIG"]) / "config.yml"
text = path.read_text()
text = text.replace(
    "      key: ",
    "      packages: [base, docker, firewall, caddy]\n"
    "      settings:\n"
    "        caddy.local_certs: true\n"
    "      key: ",
    1,
)
# Follow whatever indentation setup wrote for the first machine.
dash, field = re.search(r"(?m)^( *)- name: .*\n( *)\w", text).group(1, 2)
lab = (f"{dash}- name: lab\n{field}hosts: [{sys.argv[1]}]\n{field}port: 22\n")
text = re.sub(r"(?m)^machines:\n", "machines:\n" + lab, text, count=1)
text = text.rstrip("\n") + f"\nworkspaces:\n{dash}- name: bob\n{field}machine: lab\n"
path.write_text(text)
PY

dm sync --machine "$VM" --yes >"$SCENARIO_LOG_DIR/sync.log" 2>&1 || die "could not sync $VM"

on_vm 'docker run -d --name accept-via -p 8081:80 nginx:alpine' >"$SCENARIO_LOG_DIR/nginx.log" \
  || die "could not start nginx"
for _ in $(seq 1 60); do
  [ "$(on_vm "curl -s -o /dev/null -w '%{http_code}' http://$LAB_ADDRESS:8081")" = "200" ] && break
  sleep 1
done

REFUSED=$(dm expose add bob 8081 --host via.example.com --publish 2>&1)
contains "$REFUSED" "--via $VM" "without --via, a machine with no caddy is refused with the one that has it" || true

ADDED=$(dm --format json expose add bob 8081 --host via.example.com --via "$VM" --publish 2>"$SCENARIO_LOG_DIR/add.err")
printf '%s\n' "$ADDED" >"$SCENARIO_LOG_DIR/add.log"
contains "$ADDED" '"status": "published"' "expose add --via publishes at once" || true
contains "$ADDED" "\"via\": \"$VM\"" "and says through which machine" || true
refutes "$ADDED" "cannot reach" "and the machine with Caddy reaches the port" || true

BLOCK=$(on_vm 'cat /etc/caddy/sites.d/bob-routes.caddy')
contains "$BLOCK" "reverse_proxy $LAB_ADDRESS:8081" "Caddy proxies to the other machine's address" || true

SERVED=""
for _ in $(seq 1 15); do
  SERVED=$(on_vm "curl -sk -o /dev/null -w '%{http_code}' --resolve via.example.com:443:127.0.0.1 https://via.example.com")
  [ "$SERVED" = "200" ] && break
  sleep 2
done
equals "$SERVED" "200" "the site answers over HTTPS through Caddy" || true

LISTED=$(dm --machine "$VM" --format json expose list 2>&1)
contains "$LISTED" '"from": "lab"' "expose list names the machine the port is on" || true
contains "$LISTED" '"status": "published"' "and the route reads as published" || true

dm sync --machine "$VM" --check --yes >"$SCENARIO_LOG_DIR/sync-check.log" 2>&1 \
  || die "sync --check failed after the fast path"
if python3 - "$SCENARIO_LOG_DIR/sync-check.log" <<'CHECK'
import re
import sys

text = open(sys.argv[1]).read()
blocks = re.split(r"(?m)^TASK \[", text)
routes = [b for b in blocks if b.startswith(("routes from the configuration]",
                                              "files the configuration no longer owns]",
                                              "reload caddy for the routes]"))]
if not routes or any(re.search(r"(?m)^changed:", b) for b in routes):
    sys.exit(1)
CHECK
then
  pass "a later sync finds nothing to change in Caddy"
else
  fail "a later sync finds nothing to change in Caddy" || true
fi

DOWN=$(dm --format json expose add bob 8099 --host down.example.com --via "$VM" --publish 2>/dev/null)
printf '%s\n' "$DOWN" >"$SCENARIO_LOG_DIR/down.log"
contains "$DOWN" "cannot reach" "a port nothing answers on is published with a note saying so" || true

dm expose rm via.example.com --yes >"$SCENARIO_LOG_DIR/rm.log" 2>&1 || die "could not remove via.example.com"
KEPT=$(on_vm 'cat /etc/caddy/sites.d/bob-routes.caddy')
refutes "$KEPT" "via.example.com" "expose rm takes the site off the machine that serves it" || true
contains "$KEPT" "down.example.com" "and keeps the workspace's other site" || true
dm expose rm down.example.com --yes >>"$SCENARIO_LOG_DIR/rm.log" 2>&1 || die "could not remove down.example.com"
GONE=$(on_vm 'ls /etc/caddy/sites.d')
refutes "$GONE" "bob-routes" "and the last one removes the file" || true

scenario_done 13 "expose-via"
