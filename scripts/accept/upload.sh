#!/usr/bin/env bash
# The upload acceptance proof: files land in a workspace's home, owned by
# its account, under a new name, and never outside that home.
set -uo pipefail

ROOT=$(cd "$(dirname "$0")/../.." && pwd)
source "$ROOT/scripts/accept/common.sh"

if [ -z "${ACCEPT_RUN_DIR:-}" ]; then
  make_run_dir >/dev/null
  ACCEPT_OWNS_RUN_DIR=1
else
  ACCEPT_OWNS_RUN_DIR=0
fi

VM="devmachine-accept-${ACCEPT_RUN_ID:?ACCEPT_RUN_ID is required}-upload"
require_accept_vm "$VM" || exit 1

SCENARIO_DIR=$(mktemp -d "$ACCEPT_RUN_DIR/upload.XXXXXX")
DEVMACHINE_CONFIG=$(mktemp -d "$SCENARIO_DIR/config.XXXXXX")
SCENARIO_LOG_DIR="$SCENARIO_DIR/logs"
mkdir -p "$SCENARIO_LOG_DIR"
export DEVMACHINE_CONFIG

cleanup_upload() {
  destroy_accept_vm "$VM" || true
  rm -rf "$SCENARIO_DIR"
  if [ "$ACCEPT_OWNS_RUN_DIR" -eq 1 ]; then
    rm -rf "$ACCEPT_RUN_DIR"
  fi
}
trap cleanup_upload EXIT INT TERM

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
"$DEVMACHINE_ACCEPT_BIN" run --machine "$VM" -- true \
  >"$SCENARIO_LOG_DIR/connect.log" 2>&1 || die "$VM does not answer"

mkdir -p "$DEVMACHINE_CONFIG/packages"
for package in workspace zsh; do
  [ -d "$PACKAGES/packages/$package" ] || die "package fixture is missing: $package"
  cp -R "$PACKAGES/packages/$package" "$DEVMACHINE_CONFIG/packages/"
done
"$DEVMACHINE_ACCEPT_BIN" workspaces new acme --packages workspace,zsh --yes \
  >"$SCENARIO_LOG_DIR/workspace.log" 2>&1 || die "could not add acme"
"$DEVMACHINE_ACCEPT_BIN" sync --yes >"$SCENARIO_LOG_DIR/sync.log" 2>&1 \
  || die "could not sync the upload fixture; see $SCENARIO_LOG_DIR/sync.log"

FILES="$SCENARIO_DIR/files"
mkdir -p "$FILES"
printf 'plain body\n' > "$FILES/report.pdf"
printf 'accents and spaces\n' > "$FILES/relatório final.txt"

UPLOADED=$("$DEVMACHINE_ACCEPT_BIN" upload "$FILES/report.pdf" "$FILES/relatório final.txt" \
  --workspace acme 2>"$SCENARIO_LOG_DIR/upload-default.err")
printf '%s\n' "$UPLOADED" > "$SCENARIO_LOG_DIR/upload-default.log"
REPORT=$(printf '%s\n' "$UPLOADED" | sed -n 1p)
UNICODE=$(printf '%s\n' "$UPLOADED" | sed -n 2p)
STAMP='[0-9]{8}-[0-9]{6}'
contains "$(printf '%s' "$REPORT" | grep -E "^/home/acme/\.cache/devmachine/uploads/report-$STAMP\.pdf$")" \
  "/home/acme/.cache/devmachine/uploads/report-" "the default folder holds report-<time>.pdf" || true
contains "$(printf '%s' "$UNICODE" | grep -E "^/home/acme/\.cache/devmachine/uploads/relatório final-$STAMP\.txt$")" \
  "relatório final-" "a name with spaces and accents keeps them" || true

BODIES=$(on_vm "cat '$REPORT' '$UNICODE'")
equals "$BODIES" "plain body
accents and spaces" "both files arrive with their content" || true
OWNERS=$(on_vm "stat -c '%a %U' '$REPORT' '$UNICODE' /home/acme/.cache/devmachine/uploads")
equals "$OWNERS" "600 acme
600 acme
700 acme" "the files are 0600, the folder 0700, all owned by acme" || true

NOTES=$("$DEVMACHINE_ACCEPT_BIN" upload "$FILES/report.pdf" "$FILES/report.pdf" \
  --workspace acme --dir notes 2>"$SCENARIO_LOG_DIR/upload-notes.err")
printf '%s\n' "$NOTES" > "$SCENARIO_LOG_DIR/upload-notes.log"
FIRST=$(printf '%s\n' "$NOTES" | sed -n 1p)
SECOND=$(printf '%s\n' "$NOTES" | sed -n 2p)
contains "$(printf '%s' "$FIRST" | grep -E "^/home/acme/notes/report-$STAMP\.pdf$")" \
  "/home/acme/notes/" "--dir notes lands in ~/notes" || true
equals "$SECOND" "${FIRST%.pdf}-2.pdf" "the same name twice gets -2 instead of overwriting" || true
NOTES_OWNER=$(on_vm "stat -c '%a %U' '$SECOND' /home/acme/notes")
equals "$NOTES_OWNER" "600 acme
700 acme" "--dir creates the folder as acme" || true

JSON=$("$DEVMACHINE_ACCEPT_BIN" --format json upload "$FILES/report.pdf" --workspace acme 2>&1)
printf '%s\n' "$JSON" > "$SCENARIO_LOG_DIR/upload-json.log"
SHAPE=$(printf '%s' "$JSON" | python3 -c 'import json,sys; e=json.load(sys.stdin)[0]; print(sorted(e), e["bytes"])')
equals "$SHAPE" "['bytes', 'local', 'remote'] 11" "--format json gives local, remote and bytes" || true

on_vm "su - acme -c 'mkdir -p ~/\$(printf \"relat\\303\\263rios\")'" >"$SCENARIO_LOG_DIR/nfc-folder.log" \
  || die "could not make the composed folder"
"$DEVMACHINE_ACCEPT_BIN" upload "$FILES/report.pdf" --workspace acme --dir "$(printf 'relato\314\201rios')" \
  >"$SCENARIO_LOG_DIR/upload-nfd.log" 2>&1 || true
FOLDERS=$(on_vm "ls /home/acme | grep -c '^relat'; true")
equals "$FOLDERS" "1" "--dir sent decomposed (NFD) lands in the composed folder, not a twin" || true

CLIMB=$("$DEVMACHINE_ACCEPT_BIN" upload "$FILES/report.pdf" --workspace acme --dir ../bob 2>&1)
contains "$CLIMB" "outside the home" "--dir ../bob is refused" || true

on_vm "su - acme -c 'ln -s /tmp ~/escape'" >"$SCENARIO_LOG_DIR/symlink.log" \
  || die "could not plant the symlink fixture"
ESCAPE=$("$DEVMACHINE_ACCEPT_BIN" upload "$FILES/report.pdf" --workspace acme --dir escape 2>&1)
contains "$ESCAPE" "outside the home through a symbolic link" "a folder linked out of the home is refused" || true
BEHIND=$(on_vm 'ls -A /tmp | grep -c -e "^report-" -e "^\.devmachine-upload"; true')
equals "$BEHIND" "0" "and nothing was written behind the link" || true

FOLDER=$("$DEVMACHINE_ACCEPT_BIN" upload "$FILES" --workspace acme 2>&1)
contains "$FOLDER" "upload sends files" "a folder is refused with a hint" || true

HOSTILE="$FILES/'; touch pwned; \$(touch pwned2) \`touch pwned3\`.txt"
printf 'x\n' > "$HOSTILE"
"$DEVMACHINE_ACCEPT_BIN" upload "$HOSTILE" --workspace acme >"$SCENARIO_LOG_DIR/hostile.log" 2>&1 \
  || die "the hostile name was not uploaded; see $SCENARIO_LOG_DIR/hostile.log"
PWNED=$(on_vm 'ls /home/acme | grep -c pwned; true')
equals "$PWNED" "0" "a hostile file name never runs as a command" || true

scenario_done 14 "upload"
