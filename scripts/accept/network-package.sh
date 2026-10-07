#!/usr/bin/env bash
# The network package proof: `login` runs a network package's join and names
# the machine, `resolve` asks the package, an alias connects through
# `ssh-proxy`, and an address the network cannot give falls through to the
# next one. The package is a stand-in whose scripts are trivial; the real
# Tailscale never runs here.
set -uo pipefail

ROOT=$(cd "$(dirname "$0")/../.." && pwd)
source "$ROOT/scripts/accept/common.sh"

if [ -z "${ACCEPT_RUN_DIR:-}" ]; then
  make_run_dir >/dev/null
  ACCEPT_OWNS_RUN_DIR=1
else
  ACCEPT_OWNS_RUN_DIR=0
fi

VM="devmachine-accept-${ACCEPT_RUN_ID:?ACCEPT_RUN_ID is required}-network"
require_accept_vm "$VM" || exit 1

SCENARIO_DIR=$(mktemp -d "$ACCEPT_RUN_DIR/network.XXXXXX")
DEVMACHINE_CONFIG=$(mktemp -d "$SCENARIO_DIR/config.XXXXXX")
SCENARIO_LOG_DIR="$SCENARIO_DIR/logs"
mkdir -p "$SCENARIO_LOG_DIR"
export DEVMACHINE_CONFIG

cleanup_network() {
  destroy_accept_vm "$VM" || true
  rm -rf "$SCENARIO_DIR"
  if [ "$ACCEPT_OWNS_RUN_DIR" -eq 1 ]; then
    rm -rf "$ACCEPT_RUN_DIR"
  fi
}
trap cleanup_network EXIT INT TERM

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

# The aliases name the devmachine found on PATH, so the acceptance build has
# to be the first one there.
BIN_DIR="$SCENARIO_DIR/bin"
mkdir -p "$BIN_DIR"
ln -s "$DEVMACHINE_ACCEPT_BIN" "$BIN_DIR/devmachine"
PATH="$BIN_DIR:$PATH"
export PATH

on_vm() {
  devmachine run --machine "$VM" -- "$1" 2>&1
}

accept_create_local "$VM" \
  >"$SCENARIO_LOG_DIR/create.log" 2>&1 || die "could not create $VM"
CLOUD_INIT=$(limactl shell "$VM" -- cloud-init status --wait 2>&1 || true)
printf '%s\n' "$CLOUD_INIT" > "$SCENARIO_LOG_DIR/cloud-init.log"
case "$CLOUD_INIT" in
  *"status: done"*) ;;
  *) die "$VM did not finish cloud-init: $CLOUD_INIT" ;;
esac

PORT=$(limactl list --format '{{.SSHLocalPort}}' "$VM")
[ -n "$PORT" ] || die "$VM never received an SSH port"
printf '%s\n' "$VM" "127.0.0.1" "root" "$PORT" "example.com" "y" "1" "devmachine" \
  | devmachine setup --no-essentials --no-aliases >"$SCENARIO_LOG_DIR/setup.log" 2>&1 \
  || die "could not set up $VM"

NET="$DEVMACHINE_CONFIG/packages/acme-net"
mkdir -p "$NET/bin" "$NET/tasks"
cat > "$NET/package.yml" <<'EOF'
format: 1
name: acme-net
scope: machine
kind: vpn
summary: A stand-in private network for the acceptance run.
variables:
  login_server:
    summary: Where the machine signs in.
    default: ""
network:
  prefix: acme
  resolve: bin/resolve
  join: bin/join
  self_name: bin/self-name
EOF
cat > "$NET/tasks/main.yml" <<'EOF'
---
- name: Nothing to install for a stand-in network
  ansible.builtin.debug:
    msg: acme-net has nothing to install
EOF
cat > "$NET/bin/join" <<'EOF'
#!/usr/bin/env python3
import os

os.makedirs("/var/lib/acme-net", exist_ok=True)
with open("/var/lib/acme-net/joined", "w") as f:
    f.write(os.environ.get("DEVMACHINE_SETTINGS", "") + "\n")
EOF
cat > "$NET/bin/self-name" <<'EOF'
#!/usr/bin/env python3
print("acme-box")
EOF
cat > "$NET/bin/resolve" <<'EOF'
#!/usr/bin/env python3
import os
import sys

mode = os.environ.get("ACME_NET_MODE", "up")
if sys.argv[1:] != ["acme-box"]:
    sys.exit(1)
if mode == "down":
    print("acme-net is not running", file=sys.stderr)
    sys.exit(3)
if mode == "dead":
    print("203.0.113.1")
    sys.exit(0)
print("127.0.0.1")
EOF
chmod +x "$NET/bin/join" "$NET/bin/self-name" "$NET/bin/resolve"
devmachine packages validate "$NET" >"$SCENARIO_LOG_DIR/validate.log" 2>&1 \
  || die "the stand-in package is not valid; see $SCENARIO_LOG_DIR/validate.log"

mkdir -p "$DEVMACHINE_CONFIG/packages"
[ -d "$PACKAGES/packages/workspace" ] || die "package fixture is missing: workspace"
cp -R "$PACKAGES/packages/workspace" "$DEVMACHINE_CONFIG/packages/"
devmachine packages add acme-net --machine "$VM" --yes >"$SCENARIO_LOG_DIR/add.log" 2>&1 \
  || die "could not add acme-net"
python3 - "$DEVMACHINE_CONFIG/config.yml" "$VM" <<'EOF' || die "could not add the login_server setting"
import sys

path, machine = sys.argv[1], sys.argv[2]
lines = open(path).read().splitlines(True)
for i, line in enumerate(lines):
    if line.rstrip() == "  - name: " + machine:
        lines.insert(i + 1, "    settings:\n      acme-net.login_server: https://net.example.com\n")
        break
else:
    sys.exit("machine not found")
open(path, "w").write("".join(lines))
EOF
devmachine workspaces new acme --packages workspace --yes >"$SCENARIO_LOG_DIR/workspace.log" 2>&1 \
  || die "could not add acme"
devmachine sync --yes >"$SCENARIO_LOG_DIR/sync.log" 2>&1 \
  || die "could not sync; see $SCENARIO_LOG_DIR/sync.log"

LOGIN=$(devmachine login acme-net --machine "$VM" </dev/null 2>&1)
printf '%s\n' "$LOGIN" > "$SCENARIO_LOG_DIR/login.log"
contains "$LOGIN" "added acme:acme-box above the public address" "login names the machine on the network" || true
HOSTS=$(sed -n "/- name: $VM/,/^  - name:/p" "$DEVMACHINE_CONFIG/config.yml" | grep -A2 '^    hosts:')
equals "$HOSTS" "    hosts:
      - acme:acme-box
      - 127.0.0.1" "the network's entry is written first, the address stays as a fallback" || true
JOINED=$(on_vm "cat /var/lib/acme-net/joined")
contains "$JOINED" '"login_server":"https://net.example.com"' "join ran on the machine with the machine's settings" || true

UP=$(ACME_NET_MODE=up devmachine resolve --machine "$VM" --format json 2>&1)
printf '%s\n' "$UP" > "$SCENARIO_LOG_DIR/resolve-up.log"
FIRST=$(printf '%s' "$UP" | python3 -c 'import json,sys; a=json.load(sys.stdin)["addresses"][0]; print(a["address"], a["source"], a["package"])')
equals "$FIRST" "127.0.0.1 acme:acme-box acme-net" "resolve asks the package that declares the prefix" || true

DOWN=$(ACME_NET_MODE=down devmachine resolve --machine "$VM" --format json 2>&1)
printf '%s\n' "$DOWN" > "$SCENARIO_LOG_DIR/resolve-down.log"
SKIPPED=$(printf '%s' "$DOWN" | python3 -c 'import json,sys; r=json.load(sys.stdin); print(r["dropped"][0]["source"], "|", r["dropped"][0]["reason"], "|", r["addresses"][0]["source"])')
equals "$SKIPPED" "acme:acme-box | acme-net is not running | 127.0.0.1" "a network that is off is skipped with its reason" || true

ALIASES="$SCENARIO_DIR/ssh_config"
devmachine aliases --write --path "$ALIASES" --yes >"$SCENARIO_LOG_DIR/aliases.log" 2>&1 \
  || die "could not write the aliases"
contains "$(cat "$ALIASES")" "ProxyCommand $BIN_DIR/devmachine --config $DEVMACHINE_CONFIG ssh-proxy $VM %p" \
  "the alias connects through ssh-proxy" || true
refutes "$(sed -n '/^Host acme-devmachine$/,/^$/p' "$ALIASES")" "HostName 127.0.0.1" \
  "the alias holds no address" || true

alias_ssh() {
  ACME_NET_MODE=$1 ssh -F "$ALIASES" -o BatchMode=yes -o ConnectTimeout=30 acme-devmachine whoami 2>&1
}
equals "$(alias_ssh up)" "acme" "ssh <alias> connects through the network's address" || true
equals "$(alias_ssh down)" "acme" "with the network off, the alias falls through to the next entry" || true
equals "$(alias_ssh dead)" "acme" "an address that does not answer falls through to the next one" || true

scenario_done 10 "network-package"
