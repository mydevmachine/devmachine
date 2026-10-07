#!/usr/bin/env bash

# Shared assertions and safety guards for the release acceptance scenarios.
# Keep this file compatible with the Bash shipped by macOS (3.2).

# A scenario never writes to, or prompts about, the developer's own keychain.
export DEVMACHINE_KEYCHAIN=off

accept_reset() {
  ACCEPT_CHECKS=0
  ACCEPT_FAILURES=0
}

pass() {
  ACCEPT_CHECKS=$((ACCEPT_CHECKS + 1))
  printf '  PASS  %s\n' "$1"
}

fail() {
  ACCEPT_CHECKS=$((ACCEPT_CHECKS + 1))
  ACCEPT_FAILURES=$((ACCEPT_FAILURES + 1))
  printf '  FAIL  %s\n' "$1"
  return 1
}

show_got() {
  printf '%s\n' "$1" | sed 's/^/        got: /'
}

# Remove only line-ending characters. In particular, spaces and tabs inside or
# around a short value remain significant (" active" is not "active").
_accept_trim_line_endings() {
  ACCEPT_TRIMMED=$1
  while :; do
    case "$ACCEPT_TRIMMED" in
      $'\n'*|$'\r'*) ACCEPT_TRIMMED=${ACCEPT_TRIMMED#?} ;;
      *) break ;;
    esac
  done
  while :; do
    case "$ACCEPT_TRIMMED" in
      *$'\n'|*$'\r') ACCEPT_TRIMMED=${ACCEPT_TRIMMED%?} ;;
      *) break ;;
    esac
  done
}

equals() {
  _accept_trim_line_endings "$1"
  accept_left=$ACCEPT_TRIMMED
  _accept_trim_line_endings "$2"
  accept_right=$ACCEPT_TRIMMED
  if [ "$accept_left" = "$accept_right" ]; then
    pass "$3"
  else
    fail "$3"
    show_got "$1"
    return 1
  fi
}

contains() {
  if printf '%s' "$1" | grep -Fq -- "$2"; then
    pass "$3"
  else
    fail "$3"
    show_got "$1"
    return 1
  fi
}

refutes() {
  if printf '%s' "$1" | grep -Fq -- "$2"; then
    fail "$3"
  else
    pass "$3"
  fi
}

scenario_done() {
  expected=$1
  name=$2
  [ "$ACCEPT_CHECKS" -eq "$expected" ] || fail "$name ran $ACCEPT_CHECKS checks; expected $expected"
  [ "$ACCEPT_FAILURES" -eq 0 ]
}

require_accept_vm() {
  accept_vm=$1
  case "$accept_vm" in
    fakevps)
      echo "refusing to operate on protected VM: fakevps" >&2
      return 1
      ;;
    devmachine-accept-*)
      return 0
      ;;
    *)
      echo "refusing non-acceptance VM: $accept_vm" >&2
      return 1
      ;;
  esac
}

make_run_dir() {
  ACCEPT_RUN_DIR=$(mktemp -d "${TMPDIR:-/tmp}/devmachine-accept.XXXXXX")
  export ACCEPT_RUN_DIR
  printf '%s\n' "$ACCEPT_RUN_DIR"
}

destroy_accept_vm() {
  accept_vm=$1
  require_accept_vm "$accept_vm" || return 1
  ACCEPT_KEEP_VM=${KEEP_ACCEPT_VM:-0}
  export ACCEPT_KEEP_VM
  if [ "$ACCEPT_KEEP_VM" = 1 ]; then
    printf 'keeping acceptance VM %s\n' "$accept_vm" >&2
    return 0
  fi
  if [ -z "${DEVMACHINE_ACCEPT_BIN:-}" ] || [ ! -x "$DEVMACHINE_ACCEPT_BIN" ]; then
    echo "DEVMACHINE_ACCEPT_BIN must name an executable local CLI" >&2
    return 1
  fi
  accept_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." 2>/dev/null && pwd) || {
    echo "cannot determine the acceptance repository root" >&2
    return 1
  }
  case "$DEVMACHINE_ACCEPT_BIN" in
    /*) ;;
    *)
      echo "DEVMACHINE_ACCEPT_BIN must be an absolute repository-local path" >&2
      return 1
      ;;
  esac
  accept_bin_dir=$(cd "$(dirname "$DEVMACHINE_ACCEPT_BIN")" 2>/dev/null && pwd) || {
    echo "cannot resolve DEVMACHINE_ACCEPT_BIN" >&2
    return 1
  }
  accept_bin="$accept_bin_dir/$(basename "$DEVMACHINE_ACCEPT_BIN")"
  case "$accept_bin" in
    "$accept_root"/*) ;;
    *)
      echo "DEVMACHINE_ACCEPT_BIN must be inside the acceptance repository" >&2
      return 1
      ;;
  esac
  if [ ! -f "$accept_bin" ] || [ ! -x "$accept_bin" ] || [ -L "$accept_bin" ]; then
    echo "DEVMACHINE_ACCEPT_BIN must be a regular executable in the repository" >&2
    return 1
  fi
  "$accept_bin" machines delete-local --yes "$accept_vm"
}

# A scenario's VM is a clone of one base VM that `machines create-local` built
# and stopped right after cloud-init, before anything else touched it, so the
# clone still looks freshly bought to the CLI: root with a password, no key, no
# Ansible. Creating each VM from the image costs about 40 seconds; a clone
# boots in about 12. host-key-pinning and essentials still call create-local
# themselves, so that command stays tested. DEVMACHINE_ACCEPT_FRESH=1 makes
# every scenario do the same.
accept_create_local() {
  accept_vm=$1
  require_accept_vm "$accept_vm" || return 1
  if [ "${DEVMACHINE_ACCEPT_FRESH:-0}" = 1 ]; then
    "$DEVMACHINE_ACCEPT_BIN" machines create-local "$accept_vm"
    return
  fi
  _accept_lock_base || return 1
  if ! _accept_base_vm || ! limactl clone --tty=false "$ACCEPT_BASE_VM" "$accept_vm"; then
    _accept_unlock_base
    return 1
  fi
  _accept_unlock_base
  printf 'cloned %s from %s\n' "$accept_vm" "$ACCEPT_BASE_VM"
  limactl start --tty=false "$accept_vm"
}

_accept_base_dir() {
  printf '%s/devmachine-accept\n' "${XDG_CACHE_HOME:-$HOME/.cache}"
}

# The base is kept between runs. Its name carries a hash of what decides how
# create-local builds a VM, so changing the template or Lima builds a new one.
_accept_base_vm() {
  accept_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd) || return 1
  accept_key=$({ cat "$accept_root/internal/local/machine.yaml" "$accept_root/internal/local/size.go"
    limactl --version; } | shasum -a 256 | cut -c1-12)
  ACCEPT_BASE_VM="devmachine-accept-base-$accept_key"
  accept_ready="$(_accept_base_dir)/$ACCEPT_BASE_VM.ready"
  if [ -f "$accept_ready" ] \
    && [ "$(limactl list --format '{{.Status}}' "$ACCEPT_BASE_VM" 2>/dev/null)" = "Stopped" ]; then
    return 0
  fi

  for accept_old in $(limactl list --quiet 2>/dev/null | grep '^devmachine-accept-base-' || true); do
    limactl delete -f "$accept_old" >/dev/null 2>&1 || true
    rm -f "$(_accept_base_dir)/$accept_old.ready"
  done
  printf 'building the acceptance base VM %s\n' "$ACCEPT_BASE_VM"
  "$DEVMACHINE_ACCEPT_BIN" machines create-local "$ACCEPT_BASE_VM" \
    >"$(_accept_base_dir)/$ACCEPT_BASE_VM.log" 2>&1 || {
    echo "could not create the base VM; see $(_accept_base_dir)/$ACCEPT_BASE_VM.log" >&2
    return 1
  }
  accept_cloud_init=$(limactl shell "$ACCEPT_BASE_VM" -- cloud-init status --wait 2>&1 || true)
  case "$accept_cloud_init" in
    *"status: done"*) ;;
    *)
      echo "the base VM did not finish cloud-init: $accept_cloud_init" >&2
      return 1
      ;;
  esac
  # An empty machine-id makes every clone generate its own on first boot, the
  # way a cloud image does. Host keys need nothing: Lima gives each start a new
  # cloud-init instance-id, so cloud-init makes new ones.
  limactl shell "$ACCEPT_BASE_VM" -- sudo sh -c \
    'truncate -s 0 /etc/machine-id && rm -f /var/lib/dbus/machine-id' || return 1
  limactl stop "$ACCEPT_BASE_VM" >/dev/null 2>&1 || return 1
  touch "$accept_ready"
}

# Two runs at once must not build or delete the base under each other. A lock
# left by a run that died is taken over.
_accept_lock_base() {
  accept_lock="$(_accept_base_dir)/base.lock"
  mkdir -p "$(_accept_base_dir)" || return 1
  accept_waited=0
  until mkdir "$accept_lock" 2>/dev/null; do
    accept_holder=$(cat "$accept_lock/pid" 2>/dev/null || true)
    if [ -n "$accept_holder" ] && ! kill -0 "$accept_holder" 2>/dev/null; then
      rm -rf "$accept_lock"
      continue
    fi
    if [ -z "$accept_holder" ] && [ -n "$(find "$accept_lock" -maxdepth 0 -mmin +1 2>/dev/null)" ]; then
      rm -rf "$accept_lock"
      continue
    fi
    accept_waited=$((accept_waited + 1))
    if [ "$accept_waited" -gt 600 ]; then
      echo "the acceptance base VM is locked by process $accept_holder" >&2
      return 1
    fi
    sleep 1
  done
  printf '%s\n' "$$" > "$accept_lock/pid"
}

_accept_unlock_base() {
  rm -rf "$(_accept_base_dir)/base.lock"
}

accept_reset
