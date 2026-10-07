#!/usr/bin/env bash
# The update acceptance proof: `update --skip-cli` moves the packages pin,
# runs doctor, shows what a sync would change, applies nothing on "n" or with
# no terminal, and applies it on "y".
set -uo pipefail

ROOT=$(cd "$(dirname "$0")/../.." && pwd)
source "$ROOT/scripts/accept/common.sh"

if [ -z "${ACCEPT_RUN_DIR:-}" ]; then
  make_run_dir >/dev/null
  ACCEPT_OWNS_RUN_DIR=1
else
  ACCEPT_OWNS_RUN_DIR=0
fi

VM="devmachine-accept-${ACCEPT_RUN_ID:?ACCEPT_RUN_ID is required}-update"
require_accept_vm "$VM" || exit 1

SCENARIO_DIR=$(mktemp -d "$ACCEPT_RUN_DIR/update.XXXXXX")
DEVMACHINE_CONFIG=$(mktemp -d "$SCENARIO_DIR/config.XXXXXX")
SCENARIO_LOG_DIR="$SCENARIO_DIR/logs"
mkdir -p "$SCENARIO_LOG_DIR"
export DEVMACHINE_CONFIG

# The skills step reads which skills are managed from here. An empty folder
# keeps it away from the skills on the computer running the scenario.
XDG_STATE_HOME="$SCENARIO_DIR/state"
mkdir -p "$XDG_STATE_HOME"
export XDG_STATE_HOME

cleanup_update() {
  destroy_accept_vm "$VM" || true
  rm -rf "$SCENARIO_DIR"
  if [ "$ACCEPT_OWNS_RUN_DIR" -eq 1 ]; then
    rm -rf "$ACCEPT_RUN_DIR"
  fi
}
trap cleanup_update EXIT INT TERM

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

on_vm() {
  "$DEVMACHINE_ACCEPT_BIN" run --machine "$VM" -- "$1" 2>&1
}

# in_terminal answers the one question update asks through a real terminal,
# which is the only place update asks it.
in_terminal() {
  answer=$1
  shift
  python3 - "$answer" "$@" <<'PY'
import os, pty, sys

answer = sys.argv[1].encode() + b"\n"
pid, fd = pty.fork()
if pid == 0:
    os.execv(sys.argv[2], sys.argv[2:])

seen = b""
answered = False
while True:
    try:
        chunk = os.read(fd, 4096)
    except OSError:
        break
    if not chunk:
        break
    seen += chunk
    if not answered and b"[y/N]" in seen:
        os.write(fd, answer)
        answered = True
sys.stdout.write(seen.decode(errors="replace").replace("\r\n", "\n"))
_, status = os.waitpid(pid, 0)
sys.exit(os.waitstatus_to_exitcode(status))
PY
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
  | "$DEVMACHINE_ACCEPT_BIN" setup --no-essentials --no-aliases >"$SCENARIO_LOG_DIR/setup.log" 2>&1 \
  || die "could not set up $VM"

LATEST=$(curl -fsSL https://api.github.com/repos/mydevmachine/packages/releases/latest \
  | python3 -c 'import json,sys; print(json.load(sys.stdin)["tag_name"])') \
  || die "could not find the latest packages release"
OLDER="v$(( ${LATEST#v} - 1 ))"
"$DEVMACHINE_ACCEPT_BIN" packages pin "$OLDER" >"$SCENARIO_LOG_DIR/pin.log" 2>&1 \
  || die "could not pin $OLDER"

mkdir -p "$DEVMACHINE_CONFIG/packages"
for package in workspace zsh; do
  [ -d "$PACKAGES/packages/$package" ] || die "package fixture is missing: $package"
  cp -R "$PACKAGES/packages/$package" "$DEVMACHINE_CONFIG/packages/"
done
"$DEVMACHINE_ACCEPT_BIN" workspaces new acme --packages workspace,zsh --yes \
  >"$SCENARIO_LOG_DIR/acme.log" 2>&1 || die "could not add acme"
"$DEVMACHINE_ACCEPT_BIN" sync --yes >"$SCENARIO_LOG_DIR/sync.log" 2>&1 \
  || die "could not sync the update fixture; see $SCENARIO_LOG_DIR/sync.log"

"$DEVMACHINE_ACCEPT_BIN" workspaces new bob --packages workspace --yes \
  >"$SCENARIO_LOG_DIR/bob.log" 2>&1 || die "could not add bob"

DECLINED=$(in_terminal n "$DEVMACHINE_ACCEPT_BIN" update --skip-cli 2>&1)
printf '%s\n' "$DECLINED" > "$SCENARIO_LOG_DIR/update-no.log"
contains "$DECLINED" "packages $OLDER → $LATEST" "update moves the packages pin to the latest release" || true
PINNED=$(sed -n 's/^packages: *//p' "$DEVMACHINE_CONFIG/config.yml")
equals "$PINNED" "$LATEST" "config.yml pins $LATEST" || true
contains "$DECLINED" "pass  connection" "update runs doctor on the machine" || true
contains "$DECLINED" "$VM: " "update shows what sync --check would change" || true
contains "$DECLINED" "change(s) would be made" "the new workspace is a change sync would make" || true
contains "$DECLINED" "Apply these changes with sync? [y/N]" "update asks once before applying" || true
contains "$DECLINED" "Nothing was applied. To apply it later:" "answering n says how to apply later" || true
BOB=$(on_vm 'id bob >/dev/null 2>&1 && echo present || echo absent')
equals "$BOB" "absent" "answering n leaves the machine unchanged" || true

QUIET=$("$DEVMACHINE_ACCEPT_BIN" update --skip-cli </dev/null 2>&1)
printf '%s\n' "$QUIET" > "$SCENARIO_LOG_DIR/update-no-terminal.log"
refutes "$QUIET" "[y/N]" "without a terminal update never asks" || true
contains "$QUIET" "devmachine sync" "without a terminal update prints the sync command" || true
BOB=$(on_vm 'id bob >/dev/null 2>&1 && echo present || echo absent')
equals "$BOB" "absent" "without a terminal the machine stays unchanged" || true

APPLIED=$(in_terminal y "$DEVMACHINE_ACCEPT_BIN" update --skip-cli 2>&1)
printf '%s\n' "$APPLIED" > "$SCENARIO_LOG_DIR/update-yes.log"
contains "$APPLIED" "==> sync $VM" "answering y runs the sync" || true
contains "$(printf '%s\n' "$APPLIED" | grep '^  sync ')" "updated" "the summary says the sync was applied" || true
BOB=$(on_vm 'id bob >/dev/null 2>&1 && echo present || echo absent')
equals "$BOB" "present" "answering y applies the change" || true

CONVERGED=$("$DEVMACHINE_ACCEPT_BIN" sync --check 2>&1)
printf '%s\n' "$CONVERGED" > "$SCENARIO_LOG_DIR/converged.log"
contains "$CONVERGED" "changed=0" "after the sync the machine has converged" || true

scenario_done 15 "update"
