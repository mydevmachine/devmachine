#!/usr/bin/env bash
# The download acceptance proof: files and folders come back from a
# workspace's home whole, under their own names, never over a file already
# here, and a path is never read as a command.
set -uo pipefail

ROOT=$(cd "$(dirname "$0")/../.." && pwd)
source "$ROOT/scripts/accept/common.sh"

if [ -z "${ACCEPT_RUN_DIR:-}" ]; then
  make_run_dir >/dev/null
  ACCEPT_OWNS_RUN_DIR=1
else
  ACCEPT_OWNS_RUN_DIR=0
fi

VM="devmachine-accept-${ACCEPT_RUN_ID:?ACCEPT_RUN_ID is required}-download"
require_accept_vm "$VM" || exit 1

SCENARIO_DIR=$(mktemp -d "$ACCEPT_RUN_DIR/download.XXXXXX")
DEVMACHINE_CONFIG=$(mktemp -d "$SCENARIO_DIR/config.XXXXXX")
SCENARIO_LOG_DIR="$SCENARIO_DIR/logs"
mkdir -p "$SCENARIO_LOG_DIR"
export DEVMACHINE_CONFIG

cleanup_download() {
  destroy_accept_vm "$VM" || true
  rm -rf "$SCENARIO_DIR"
  if [ "$ACCEPT_OWNS_RUN_DIR" -eq 1 ]; then
    rm -rf "$ACCEPT_RUN_DIR"
  fi
}
trap cleanup_download EXIT INT TERM

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
  || die "could not sync the download fixture; see $SCENARIO_LOG_DIR/sync.log"

TO="$SCENARIO_DIR/to"
mkdir -p "$TO"
on_vm "su - acme -c 'mkdir -p ~/proj/site/css && printf \"plain body\\n\" > ~/proj/report.pdf && printf \"accents and spaces\\n\" > ~/proj/\"relatório final.txt\" && printf \"<h1>hi</h1>\\n\" > ~/proj/site/index.html && printf \"body{}\\n\" > ~/proj/site/css/a.css && head -c 5242880 /dev/urandom > ~/proj/big.bin && chmod 600 ~/proj/report.pdf'" \
  >"$SCENARIO_LOG_DIR/fixture.log" || die "could not write the download fixture"

GOT=$("$DEVMACHINE_ACCEPT_BIN" download proj/report.pdf "~/proj/relatório final.txt" \
  --workspace acme --to "$TO" 2>"$SCENARIO_LOG_DIR/download.err")
printf '%s\n' "$GOT" > "$SCENARIO_LOG_DIR/download.log"
equals "$GOT" "$TO/report.pdf
$TO/relatório final.txt" "relative and ~/ paths land under their own names, accents and spaces kept" || true
equals "$(cat "$TO/report.pdf" "$TO/relatório final.txt")" "plain body
accents and spaces" "both files arrive with their content" || true

DECOMPOSED=$(printf 'proj/relato\314\201rio final.txt')
NFD_TO="$SCENARIO_DIR/nfd"
mkdir -p "$NFD_TO"
"$DEVMACHINE_ACCEPT_BIN" download "$DECOMPOSED" --workspace acme --to "$NFD_TO" >"$SCENARIO_LOG_DIR/nfd.log" 2>&1 || true
equals "$(cat "$NFD_TO"/* 2>/dev/null)" "accents and spaces" "a path a Mac app sends decomposed (NFD) still finds the composed file" || true

AGAIN=$("$DEVMACHINE_ACCEPT_BIN" download /home/acme/proj/report.pdf --workspace acme --to "$TO" 2>&1)
equals "$AGAIN" "$TO/report-2.pdf" "the same name twice gets -2 instead of overwriting" || true

REMOTE_SUM=$(on_vm "sha256sum /home/acme/proj/big.bin | cut -d' ' -f1")
"$DEVMACHINE_ACCEPT_BIN" download proj/big.bin --workspace acme --to "$TO" >"$SCENARIO_LOG_DIR/big.log" 2>&1 \
  || die "the big file did not download; see $SCENARIO_LOG_DIR/big.log"
LOCAL_SUM=$(shasum -a 256 "$TO/big.bin" | cut -d' ' -f1)
equals "$LOCAL_SUM" "$REMOTE_SUM" "a 5 MB binary file arrives byte for byte" || true

SITE=$("$DEVMACHINE_ACCEPT_BIN" download proj/site --workspace acme --to "$TO" 2>&1)
equals "$SITE" "$TO/site.tar.gz" "a folder arrives as <name>.tar.gz" || true
LISTED=$(tar tzf "$TO/site.tar.gz" | sort | tr '\n' ' ')
equals "$LISTED" "site/ site/css/ site/css/a.css site/index.html " "the archive holds the folder under its own name" || true

JSON=$("$DEVMACHINE_ACCEPT_BIN" --format json download proj/report.pdf proj/missing.txt --workspace acme --to "$TO" 2>/dev/null)
printf '%s\n' "$JSON" > "$SCENARIO_LOG_DIR/download-json.log"
SHAPE=$(printf '%s' "$JSON" | python3 -c 'import json,sys; e=json.load(sys.stdin); print(sorted(e[0]), e[0]["bytes"], "does not exist" in e[1]["error"])')
equals "$SHAPE" "['bytes', 'folder', 'local', 'remote'] 11 True" "--format json gives remote, local, bytes and folder, and an error per failure" || true

on_vm "printf x > /home/acme/proj/secret.txt && chmod 600 /home/acme/proj/secret.txt" >/dev/null \
  || die "could not write the unreadable fixture"
OTHER=$("$DEVMACHINE_ACCEPT_BIN" download proj/secret.txt --workspace acme --to "$TO" 2>&1)
contains "$OTHER" "cannot be read" "a file the workspace account cannot read is refused" || true

HOSTILE="proj/'; touch pwned; \$(touch pwned2) \`touch pwned3\`.txt"
"$DEVMACHINE_ACCEPT_BIN" download "$HOSTILE" --workspace acme --to "$TO" >"$SCENARIO_LOG_DIR/hostile.log" 2>&1 || true
PWNED=$(on_vm 'ls /home/acme /home/acme/proj | grep -c pwned; true')
equals "$PWNED" "0" "a hostile path never runs as a command" || true

LEFT=$(ls -A "$TO" | grep -c '^\.devmachine-download-'; true)
equals "$LEFT" "0" "no temporary file is left behind" || true

scenario_done 11 "download"
