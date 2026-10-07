#!/usr/bin/env bash
# The workspace secrets acceptance proof: `secrets set --workspace`, its
# dotenv delivery, and the shell that loads it.
set -uo pipefail

ROOT=$(cd "$(dirname "$0")/../.." && pwd)
source "$ROOT/scripts/accept/common.sh"

if [ -z "${ACCEPT_RUN_DIR:-}" ]; then
  make_run_dir >/dev/null
  ACCEPT_OWNS_RUN_DIR=1
else
  ACCEPT_OWNS_RUN_DIR=0
fi

VM="devmachine-accept-${ACCEPT_RUN_ID:?ACCEPT_RUN_ID is required}-secrets"
require_accept_vm "$VM" || exit 1

SCENARIO_DIR=$(mktemp -d "$ACCEPT_RUN_DIR/secrets.XXXXXX")
DEVMACHINE_CONFIG=$(mktemp -d "$SCENARIO_DIR/config.XXXXXX")
SCENARIO_LOG_DIR="$SCENARIO_DIR/logs"
mkdir -p "$SCENARIO_LOG_DIR"
export DEVMACHINE_CONFIG

# Every name this scenario stores is removed again, even when it fails half
# way.
SECRET_NAMES="API_KEY APP_TOKEN NEW_ONE LEAK"

cleanup_secrets() {
  for name in $SECRET_NAMES; do
    "$DEVMACHINE_ACCEPT_BIN" secrets rm "$name" --workspace acme >/dev/null 2>&1 || true
  done
  "$DEVMACHINE_ACCEPT_BIN" secrets rm robot >/dev/null 2>&1 || true
  destroy_accept_vm "$VM" || true
  rm -rf "$SCENARIO_DIR"
  if [ "$ACCEPT_OWNS_RUN_DIR" -eq 1 ]; then
    rm -rf "$ACCEPT_RUN_DIR"
  fi
}
trap cleanup_secrets EXIT INT TERM

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
  || die "could not sync the workspace secrets fixture; see $SCENARIO_LOG_DIR/sync.log"

API_VALUE="sk-test 'quoted' \$HOME"
SET=$(printf '%s\n' "$API_VALUE" | "$DEVMACHINE_ACCEPT_BIN" secrets set API_KEY \
  --workspace acme --stdin --push 2>&1)
printf '%s\n' "$SET" > "$SCENARIO_LOG_DIR/set-push.log"
contains "$SET" "delivered acme/API_KEY -> .devmachine/env" "set --push delivers into ~/.devmachine/env" || true

MODE=$(on_vm 'stat -c "%a %U" /home/acme/.devmachine/env')
equals "$MODE" "600 acme" "the env file is 0600 and owned by acme" || true

SHELL_OF=$(on_vm 'getent passwd acme | cut -d: -f7')
equals "$SHELL_OF" "/usr/bin/zsh" "acme logs in to zsh" || true

LOGIN_SEES=$(on_vm 'su - acme -c '"'"'printf %s "$API_KEY"'"'")
equals "$LOGIN_SEES" "$API_VALUE" "a zsh login shell for acme sees API_KEY, quotes and all" || true

SSH_SEES=$("$DEVMACHINE_ACCEPT_BIN" run --workspace acme -- 'printf %s "$API_KEY"' 2>&1)
equals "$SSH_SEES" "$API_VALUE" "a command run over ssh as acme sees it too" || true

on_vm "su - acme -c 'mkdir -p ~/app && printf \"# app config\nAPP_TOKEN=old\nKEEP=me\n\" > ~/app/.env && chmod 0640 ~/app/.env'" \
  >"$SCENARIO_LOG_DIR/app-env.log" || die "could not write acme's app/.env"

"$DEVMACHINE_ACCEPT_BIN" secrets set APP_TOKEN new-token --workspace acme --env-file app/.env \
  >"$SCENARIO_LOG_DIR/set-app-token.log" 2>&1 || die "could not store APP_TOKEN"
UNTOUCHED=$(on_vm 'cat /home/acme/app/.env')
contains "$UNTOUCHED" "APP_TOKEN=old" "set without --push leaves the file alone" || true

"$DEVMACHINE_ACCEPT_BIN" credentials push --yes >"$SCENARIO_LOG_DIR/push.log" 2>&1 \
  || die "credentials push failed; see $SCENARIO_LOG_DIR/push.log"
EDITED=$(on_vm 'cat /home/acme/app/.env')
equals "$EDITED" "# app config
APP_TOKEN='new-token'
KEEP=me" "credentials push replaces APP_TOKEN in place and keeps every other line" || true

BACKUP=$(on_vm 'cat /home/acme/app/.env.devmachine.bak')
equals "$BACKUP" "# app config
APP_TOKEN=old
KEEP=me" "the first edit keeps the original as .devmachine.bak" || true

APP_MODE=$(on_vm 'stat -c "%a %U" /home/acme/app/.env')
equals "$APP_MODE" "640 acme" "the edited file keeps its mode and owner" || true

"$DEVMACHINE_ACCEPT_BIN" secrets set NEW_ONE appended --workspace acme --env-file app/.env --push \
  >"$SCENARIO_LOG_DIR/set-new-one.log" 2>&1 || die "could not deliver NEW_ONE"
APPENDED=$(on_vm 'cat /home/acme/app/.env')
equals "$APPENDED" "# app config
APP_TOKEN='new-token'
KEEP=me
NEW_ONE='appended'" "a new name is appended at the end" || true
BACKUP_AGAIN=$(on_vm 'cat /home/acme/app/.env.devmachine.bak')
contains "$BACKUP_AGAIN" "APP_TOKEN=old" "a later edit never overwrites the first backup" || true

ESCAPE=$("$DEVMACHINE_ACCEPT_BIN" secrets set LEAK x --workspace acme --env-file ../bob/.env 2>&1)
contains "$ESCAPE" "outside the workspace's home" "a path escaping the home is refused" || true
ABSOLUTE=$("$DEVMACHINE_ACCEPT_BIN" secrets set LEAK x --workspace acme --env-file /etc/leak.env 2>&1)
contains "$ABSOLUTE" "absolute path" "an absolute path is refused" || true

on_vm "su - acme -c 'ln -s /tmp ~/escape'" >"$SCENARIO_LOG_DIR/symlink.log" \
  || die "could not plant the symlink fixture"
TMP_BEFORE=$(on_vm 'stat -c "%a %U" /tmp')
"$DEVMACHINE_ACCEPT_BIN" secrets set LEAK x --workspace acme --env-file escape/leak.env \
  >"$SCENARIO_LOG_DIR/set-leak.log" 2>&1 || die "could not store LEAK"
SYMLINK_PUSH=$("$DEVMACHINE_ACCEPT_BIN" credentials push --yes 2>&1)
printf '%s\n' "$SYMLINK_PUSH" > "$SCENARIO_LOG_DIR/push-symlink.log"
contains "$SYMLINK_PUSH" "outside the workspace's home" "a symlink out of the home is refused on the machine" || true
TMP_AFTER=$(on_vm 'stat -c "%a %U" /tmp; test ! -e /tmp/leak.env && echo absent')
equals "$TMP_AFTER" "$TMP_BEFORE
absent" "and nothing outside the home was written or chowned" || true
"$DEVMACHINE_ACCEPT_BIN" secrets rm LEAK --workspace acme >/dev/null 2>&1 \
  || die "could not drop LEAK"

LIST=$("$DEVMACHINE_ACCEPT_BIN" secrets list --workspace acme 2>&1)
printf '%s\n' "$LIST" > "$SCENARIO_LOG_DIR/list.log"
contains "$LIST" "acme/API_KEY -> .devmachine/env" "list shows the default destination" || true
contains "$LIST" "acme/APP_TOKEN -> app/.env" "list shows the env-file destination" || true
refutes "$LIST" "new-token" "list never prints a value" || true
refutes "$LIST" "sk-test" "not even the default file's" || true

"$DEVMACHINE_ACCEPT_BIN" secrets rm APP_TOKEN --workspace acme --from-file \
  >"$SCENARIO_LOG_DIR/rm.log" 2>&1 || die "could not remove APP_TOKEN"
STILL=$(on_vm 'cat /home/acme/app/.env')
contains "$STILL" "APP_TOKEN=" "rm --from-file does not reach the machine" || true
"$DEVMACHINE_ACCEPT_BIN" credentials push --yes >"$SCENARIO_LOG_DIR/push-rm.log" 2>&1 \
  || die "credentials push after rm failed; see $SCENARIO_LOG_DIR/push-rm.log"
REMOVED=$(on_vm 'cat /home/acme/app/.env')
equals "$REMOVED" "# app config
KEEP=me
NEW_ONE='appended'" "the next credentials push removes the line and keeps the rest" || true
AFTER_LIST=$("$DEVMACHINE_ACCEPT_BIN" secrets list --workspace acme 2>&1)
refutes "$AFTER_LIST" "APP_TOKEN" "and the name is gone from the list" || true

"$DEVMACHINE_ACCEPT_BIN" secrets set API_KEY moved --workspace acme --env-file app/.env \
  >"$SCENARIO_LOG_DIR/set-move.log" 2>&1 || die "could not move API_KEY"
"$DEVMACHINE_ACCEPT_BIN" credentials push --yes >"$SCENARIO_LOG_DIR/push-move.log" 2>&1 \
  || die "credentials push after the move failed; see $SCENARIO_LOG_DIR/push-move.log"
DEFAULT_AFTER=$(on_vm 'cat /home/acme/.devmachine/env')
refutes "$DEFAULT_AFTER" "API_KEY=" "moving API_KEY to app/.env takes it out of ~/.devmachine/env" || true
MOVED=$(on_vm 'cat /home/acme/app/.env')
contains "$MOVED" "API_KEY='moved'" "and puts it in app/.env" || true

mkdir -p "$DEVMACHINE_CONFIG/packages/robot/tasks"
printf -- '- name: nothing to install\n  ansible.builtin.debug:\n    msg: robot\n' \
  > "$DEVMACHINE_CONFIG/packages/robot/tasks/main.yml"
printf 'format: 1\nname: robot\nscope: workspace\nsummary: A package that only asks for a token.\n%s\n' \
  'credentials:
  - name: robot
    kind: secret
    scope: workspace
    env: ROBOT_TOKEN' > "$DEVMACHINE_CONFIG/packages/robot/package.yml"
"$DEVMACHINE_ACCEPT_BIN" packages add robot --workspace acme --yes \
  >"$SCENARIO_LOG_DIR/add-robot.log" 2>&1 || die "could not add robot to acme"
"$DEVMACHINE_ACCEPT_BIN" secrets set robot robot-token \
  >"$SCENARIO_LOG_DIR/set-robot.log" 2>&1 || die "could not store robot"

on_vm "su - acme -c 'ln -s /tmp ~/.devmachine/robot'" >"$SCENARIO_LOG_DIR/robot-symlink.log" \
  || die "could not plant the credential symlink fixture"
TMP_BEFORE=$(on_vm 'stat -c "%a %U" /tmp')
ROBOT_PUSH=$("$DEVMACHINE_ACCEPT_BIN" credentials push --yes 2>&1)
printf '%s\n' "$ROBOT_PUSH" > "$SCENARIO_LOG_DIR/push-robot-symlink.log"
contains "$ROBOT_PUSH" "outside the workspace's home" "a package credential behind a symlink out of the home is refused" || true
TMP_AFTER=$(on_vm 'stat -c "%a %U" /tmp; test ! -e /tmp/env && echo absent')
equals "$TMP_AFTER" "$TMP_BEFORE
absent" "and root wrote and chowned nothing outside the home" || true

on_vm "su - acme -c 'rm ~/.devmachine/robot'" >/dev/null || die "could not drop the symlink fixture"
"$DEVMACHINE_ACCEPT_BIN" credentials push --yes >"$SCENARIO_LOG_DIR/push-robot.log" 2>&1 \
  || die "credentials push for robot failed; see $SCENARIO_LOG_DIR/push-robot.log"
ROBOT_MODE=$(on_vm 'stat -c "%a %U" /home/acme/.devmachine/robot /home/acme/.devmachine/robot/env')
equals "$ROBOT_MODE" "700 acme
600 acme" "with the link gone the credential lands in the home, owned by acme" || true

scenario_done 27 "workspace secrets"
