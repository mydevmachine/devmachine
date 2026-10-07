#!/usr/bin/env bash
# The v0.5 packages, credentials, and DNS acceptance proof.
set -uo pipefail

ROOT=$(cd "$(dirname "$0")/../.." && pwd)
source "$ROOT/scripts/accept/common.sh"

if [ -z "${ACCEPT_RUN_DIR:-}" ]; then
  make_run_dir >/dev/null
  ACCEPT_OWNS_RUN_DIR=1
else
  ACCEPT_OWNS_RUN_DIR=0
fi

VM="devmachine-accept-${ACCEPT_RUN_ID:?ACCEPT_RUN_ID is required}-v05"
require_accept_vm "$VM" || exit 1

SCENARIO_DIR=$(mktemp -d "$ACCEPT_RUN_DIR/v05.XXXXXX")
DEVMACHINE_CONFIG=$(mktemp -d "$SCENARIO_DIR/config.XXXXXX")
export DEVMACHINE_CONFIG

cleanup_v05() {
  destroy_accept_vm "$VM" || true
  rm -rf "$SCENARIO_DIR"
  if [ "$ACCEPT_OWNS_RUN_DIR" -eq 1 ]; then
    rm -rf "$ACCEPT_RUN_DIR"
  fi
}
trap cleanup_v05 EXIT INT TERM

if [ -z "${DEVMACHINE_ACCEPT_BIN:-}" ] || [ ! -x "$DEVMACHINE_ACCEPT_BIN" ]; then
  echo "DEVMACHINE_ACCEPT_BIN must name the local acceptance CLI" >&2
  exit 1
fi
if [ -z "${PACKAGES:-}" ] || [ ! -d "$PACKAGES/packages" ]; then
  echo "PACKAGES must name a packages checkout" >&2
  exit 1
fi

die() {
  echo "$1" >&2
  exit 1
}

# Return the three recap fields in a fixed order. This deliberately rejects a
# missing or malformed recap rather than trusting a substring in arbitrary log
# output.
ansible_recap() {
  printf '%s\n' "$1" | awk '
    /^devmachine[[:space:]]*:/ {
      seen = 1
      for (i = 1; i <= NF; i++) {
        split($i, field, "=")
        if (field[1] == "changed") changed = field[2]
        if (field[1] == "failed") failed = field[2]
        if (field[1] == "unreachable") unreachable = field[2]
      }
    }
    END {
      if (!seen || changed == "" || failed == "" || unreachable == "") exit 1
      print "changed=" changed " failed=" failed " unreachable=" unreachable
    }
  '
}

capture_sync() {
  sync_name=$1
  SYNC_OUTPUT=$("$DEVMACHINE_ACCEPT_BIN" sync --yes 2>&1)
  SYNC_STATUS=$?
  printf '%s\n' "$SYNC_OUTPUT" > "$ACCEPT_RUN_DIR/$sync_name.full.log"
  tail -40 "$ACCEPT_RUN_DIR/$sync_name.full.log" > "$ACCEPT_RUN_DIR/$sync_name.log"
  [ "$SYNC_STATUS" -eq 0 ] || die "$sync_name failed; see $ACCEPT_RUN_DIR/$sync_name.full.log"
  SYNC_RECAP=$(ansible_recap "$SYNC_OUTPUT") || die "$sync_name produced no complete Ansible recap"
}

recap_converged() {
  case "$1" in
    changed=[0-9]*' failed=0 unreachable=0') pass "$2" ;;
    *) fail "$2" || true ;;
  esac
}

recap_idempotent() {
  equals "$1" "changed=0 failed=0 unreachable=0" "$2" || true
}

require_converged_recap() {
  case "$1" in
    changed=[0-9]*' failed=0 unreachable=0') ;;
    *) die "$2 did not converge: $1" ;;
  esac
}

accept_create_local "$VM" || die "could not create $VM"
CLOUD_INIT=$(limactl shell "$VM" -- cloud-init status --wait 2>&1 || true)
case "$CLOUD_INIT" in
  *"status: done"*) ;;
  *) die "$VM did not finish cloud-init: $CLOUD_INIT" ;;
esac

PORT=$(limactl list --format '{{.SSHLocalPort}}' "$VM")
[ -n "$PORT" ] || die "$VM never received an SSH port"
printf '%s\n' "$VM" "127.0.0.1" "root" "$PORT" "example.com" "y" "1" "devmachine" \
  | "$DEVMACHINE_ACCEPT_BIN" setup --no-essentials --no-aliases || die "could not set up $VM"
"$DEVMACHINE_ACCEPT_BIN" run --machine "$VM" -- true > /dev/null 2>&1 \
  || die "$VM does not answer; downstream assertions would be meaningless"

mkdir -p "$DEVMACHINE_CONFIG/packages"
for package in base git tailscale workspace git-key hostinger devmachine-skills; do
  [ -d "$PACKAGES/packages/$package" ] || die "package fixture is missing: $package"
  cp -R "$PACKAGES/packages/$package" "$DEVMACHINE_CONFIG/packages/"
done

# Selecting claude-code is what asks the generator for the Claude adapter. The
# acceptance case needs that declaration, not a download of Claude itself.
mkdir -p "$DEVMACHINE_CONFIG/packages/claude-code/tasks"
python3 - <<'PY' || die "could not create the inert claude-code fixture"
import os
from pathlib import Path

root = Path(os.environ["DEVMACHINE_CONFIG"]) / "packages" / "claude-code"
(root / "package.yml").write_text(
    "format: 1\n"
    "name: claude-code\n"
    "scope: workspace\n"
    "summary: Acceptance-only Claude adapter marker.\n"
    "needs: [workspace]\n"
)
(root / "tasks" / "main.yml").write_text("---\n[]\n")
PY

python3 - <<'PY' || die "could not configure the v0.5 fixture"
import os
from pathlib import Path

path = Path(os.environ["DEVMACHINE_CONFIG"]) / "config.yml"
text = path.read_text()
text = text.replace(
    "      key: ",
    "      packages: [base, git, tailscale]\n"
    "      settings:\n"
    "        base.swap: 1G\n"
    "      key: ",
    1,
)
path.write_text(text)
PY

"$DEVMACHINE_ACCEPT_BIN" workspaces new alice --packages workspace --yes || die "could not add alice"
"$DEVMACHINE_ACCEPT_BIN" workspaces new bob --like alice --yes || die "could not add bob"
"$DEVMACHINE_ACCEPT_BIN" packages add git-key --workspace alice --yes || die "could not add alice's git-key"
"$DEVMACHINE_ACCEPT_BIN" packages add git-key --workspace bob --yes || die "could not add bob's git-key"
"$DEVMACHINE_ACCEPT_BIN" packages add claude-code --workspace alice --yes || die "could not add alice's Claude adapter"
"$DEVMACHINE_ACCEPT_BIN" packages add devmachine-skills --workspace alice --yes || die "could not add alice's skills"

capture_sync v05-sync1
recap_converged "$SYNC_RECAP" "sync converges"

ALICE_SKILL=$("$DEVMACHINE_ACCEPT_BIN" run --workspace alice -- \
  'test -f ~/.agents/skills/use-devmachine/SKILL.md && printf present' 2>&1)
equals "$ALICE_SKILL" "present" "a selected workspace receives the official skill" || true
BOB_SKILL=$("$DEVMACHINE_ACCEPT_BIN" run --workspace bob -- \
  'test ! -e ~/.agents/skills/use-devmachine && printf absent' 2>&1)
equals "$BOB_SKILL" "absent" "an unselected workspace receives no skill" || true
CLAUDE_LINK=$("$DEVMACHINE_ACCEPT_BIN" run --workspace alice -- \
  'readlink ~/.claude/skills/use-devmachine' 2>&1)
equals "$CLAUDE_LINK" "../../.agents/skills/use-devmachine" "Claude uses the relative canonical link" || true

capture_sync v05-sync2
recap_idempotent "$SYNC_RECAP" "a second sync changes nothing"

SWAP=$("$DEVMACHINE_ACCEPT_BIN" run --machine "$VM" -- 'swapon --show=NAME --noheadings' 2>&1)
contains "$SWAP" "/swapfile" "base.swap created a swapfile" || true

TAILSCALE=$("$DEVMACHINE_ACCEPT_BIN" run --machine "$VM" -- 'systemctl is-active tailscaled' 2>&1)
equals "$TAILSCALE" "active" "tailscaled runs" || true
CREDENTIALS=$("$DEVMACHINE_ACCEPT_BIN" credentials list 2>&1)
contains "$CREDENTIALS" "tailscale" "tailscale asks to be logged in" || true

KEYOUT=$("$DEVMACHINE_ACCEPT_BIN" login git-key < /dev/null 2>&1 || true)
printf '%s\n' "$KEYOUT" > "$ACCEPT_RUN_DIR/v05-key.log"
contains "$KEYOUT" "ssh-ed25519" "login git-key prints the public half" || true
contains "$KEYOUT" "github.com/settings/ssh/new" "and says where to register it" || true

capture_sync v05-sync3
require_converged_recap "$SYNC_RECAP" "sync after login"

KEY_COUNT=$("$DEVMACHINE_ACCEPT_BIN" run --machine "$VM" -- \
  'md5sum /home/alice/.ssh/id_ed25519 /home/bob/.ssh/id_ed25519 | awk "{print \$1}" | sort -u | wc -l' 2>&1)
equals "$KEY_COUNT" "1" "alice and bob share one key" || true

OWNER=$("$DEVMACHINE_ACCEPT_BIN" run --machine "$VM" -- 'stat -c "%a %U" /home/alice/.ssh/id_ed25519' 2>&1)
contains "$OWNER" "600 alice" "and alice owns hers, 0600" || true

capture_sync v05-sync4
recap_idempotent "$SYNC_RECAP" "workspace and git-key do not fight over the key"

"$DEVMACHINE_ACCEPT_BIN" run --machine "$VM" -- \
  'printf untouched > /etc/devmachine-accept-victim && chmod 0644 /etc/devmachine-accept-victim && runuser -u bob -- ln -sfn /etc/devmachine-accept-victim /home/bob/.ssh/id_ed25519' \
  || die "could not plant bob's link"
PLANTED_OUTPUT=$("$DEVMACHINE_ACCEPT_BIN" sync --yes 2>&1)
PLANTED_STATUS=$?
printf '%s\n' "$PLANTED_OUTPUT" > "$ACCEPT_RUN_DIR/v05-sync-planted.full.log"
if [ "$PLANTED_STATUS" -ne 0 ]; then
  pass "sync fails when a shared login's path is a link out of the home"
else
  fail "sync fails when a shared login's path is a link out of the home" || true
fi
contains "$PLANTED_OUTPUT" "reaches outside the home of bob through a symbolic link" \
  "and says which account's link it refused" || true
VICTIM=$("$DEVMACHINE_ACCEPT_BIN" run --machine "$VM" -- \
  'stat -c "%a %U" /etc/devmachine-accept-victim && cat /etc/devmachine-accept-victim' 2>&1)
equals "$VICTIM" "644 root
untouched" "the linked file keeps its mode, owner and content" || true

"$DEVMACHINE_ACCEPT_BIN" run --machine "$VM" -- \
  'runuser -u bob -- rm /home/bob/.ssh/id_ed25519 && rm /etc/devmachine-accept-victim' \
  || die "could not remove bob's link"
capture_sync v05-sync-unplanted
require_converged_recap "$SYNC_RECAP" "sync after removing the link"
KEY_COUNT=$("$DEVMACHINE_ACCEPT_BIN" run --machine "$VM" -- \
  'md5sum /home/alice/.ssh/id_ed25519 /home/bob/.ssh/id_ed25519 | awk "{print \$1}" | sort -u | wc -l' 2>&1)
equals "$KEY_COUNT" "1" "with the link gone, bob gets the shared key again" || true

"$DEVMACHINE_ACCEPT_BIN" workspaces edit bob --share git-key=own --yes || die "could not let bob own git-key"
"$DEVMACHINE_ACCEPT_BIN" run --machine "$VM" -- 'echo bobs-own > /home/bob/.ssh/id_ed25519' \
  || die "could not set bob's key fixture"
capture_sync v05-sync5
require_converged_recap "$SYNC_RECAP" "sync after opting out"
KEPT=$("$DEVMACHINE_ACCEPT_BIN" run --machine "$VM" -- 'cat /home/bob/.ssh/id_ed25519' 2>&1)
contains "$KEPT" "bobs-own" "a workspace that opted out keeps its own key" || true

PROVIDERS=$("$DEVMACHINE_ACCEPT_BIN" dns providers 2>&1)
contains "$PROVIDERS" "packages" "dns providers says where to get one" || true
ADDED=$("$DEVMACHINE_ACCEPT_BIN" dns add www.example.com A 198.51.100.10 --publish 2>&1)
contains "$ADDED" "198.51.100.10" "with no provider, dns add prints the record to create" || true

"$DEVMACHINE_ACCEPT_BIN" packages add hostinger --machine "$VM" --yes || die "could not add hostinger"
capture_sync v05-sync6
require_converged_recap "$SYNC_RECAP" "sync after adding hostinger"
HELP=$("$DEVMACHINE_ACCEPT_BIN" packages help hostinger 2>&1)
contains "$HELP" "zones" "the provider says what it accepts" || true
RAN=$("$DEVMACHINE_ACCEPT_BIN" run --machine "$VM" --package hostinger -- help 2>&1)
contains "$RAN" "zones" "run --package reaches a local package where sync unpacked it" || true

scenario_done 22 "v0.5"
