#!/usr/bin/env bash
# A throwaway VPS on this machine, for testing the CLI against something real.
#
#   scripts/fake-vps.sh up         start it, and authorize a throwaway key
#   scripts/fake-vps.sh port       print the forwarded SSH port
#   scripts/fake-vps.sh env        print the values a test needs, as exports
#   scripts/fake-vps.sh ssh        open a shell on it
#   scripts/fake-vps.sh down       destroy it
#
# It needs Lima (`brew install lima`). The VM is made by the CLI itself —
# `devmachine machines create-local` — so the harness and the feature are one
# thing and cannot drift apart. It reproduces a freshly bought server: root
# over SSH, a password, and no key installed.
#
# `up` then authorizes a key, and that is the one thing the CLI deliberately
# does NOT do. The integration tests need a machine they can already reach,
# without running `setup` first; `machines create-local` must leave the machine
# exactly as a bought one arrives, or the trust bootstrap is never exercised by
# the thing that runs on every test. Do not "fix" this difference — it is the
# point. The key is installed through `limactl shell`, not over SSH, so the
# password path stays untouched and belongs to whoever tests first contact.
#
# The key is generated here, lives outside the repo, and is worthless — the VM
# sits behind this machine's NAT and holds no data.
set -euo pipefail

cd "$(dirname "$0")/.."

VM="${DEVMACHINE_FAKE_VPS:-fakevps}"
KEY_DIR="${DEVMACHINE_TEST_HOME:-$HOME/.config/devmachine-test}/keys"
KEY="$KEY_DIR/fake_vps_ed25519"
KNOWN_HOSTS="${DEVMACHINE_TEST_HOME:-$HOME/.config/devmachine-test}/known_hosts"
# The admin login the tests use. root is a bought server; any other name is a
# server whose admin reaches root through passwordless sudo, which is how a
# home server or a provider's non-root image arrives. Both are first-class:
# `make test-vps` runs as root, `make test-vps-sudo` as an admin named
# opsadmin, which is no test workspace's account, so a push into a workspace
# really crosses accounts.
ADMIN="${DEVMACHINE_FAKE_VPS_ADMIN:-root}"
# The system the VM runs: ubuntu, or arch (x86_64 under qemu, slow on Apple
# Silicon). It only matters when `up` creates the VM; give an Arch one its own
# name with DEVMACHINE_FAKE_VPS so it does not take the place of the default.
DISTRO="${DEVMACHINE_FAKE_VPS_DISTRO:-ubuntu}"

# The CLI under test, not one installed somewhere else.
DEVMACHINE=(go run ./cmd/devmachine)

require_lima() {
  command -v limactl >/dev/null 2>&1 || {
    echo "limactl not found. Install it with: brew install lima" >&2
    exit 1
  }
}

# The port is a question about a machine that already exists, so it is asked of
# lima directly. Making the machine is the CLI's job, and only that.
vm_port() {
  limactl list --format '{{.SSHLocalPort}}' "$VM" 2>/dev/null
}

cmd_up() {
  require_lima
  if ! limactl list "$VM" >/dev/null 2>&1; then
    "${DEVMACHINE[@]}" machines create-local "$VM" --distro "$DISTRO"
  elif [ "$(limactl list --format '{{.Status}}' "$VM")" != "Running" ]; then
    "${DEVMACHINE[@]}" machines start "$VM"
  fi

  # A VM that has been synced is a poor test machine: `sync` turns on ufw and
  # fail2ban, and a harness that reconnects hundreds of times is exactly what
  # those are built to stop. Say that, instead of letting `limactl shell` fail
  # with `kex_exchange_identification`, which tells nobody anything.
  if ! limactl shell "$VM" -- true >/dev/null 2>&1; then
    echo "$VM is running but will not take a connection." >&2
    echo "It has probably been converged; a firewall or fail2ban is in the way." >&2
    echo "It exists to be destroyed: scripts/fake-vps.sh down && scripts/fake-vps.sh up" >&2
    exit 1
  fi

  mkdir -p "$KEY_DIR"
  chmod 700 "$KEY_DIR"
  [ -f "$KEY" ] || ssh-keygen -t ed25519 -N "" -C "devmachine fake vps" -f "$KEY" >/dev/null

  admin_home=/root
  if [ "$ADMIN" != root ]; then
    admin_home=/home/$ADMIN
    limactl shell "$VM" -- sudo sh -c "id -u '$ADMIN' >/dev/null 2>&1 || useradd -m -s /bin/bash '$ADMIN'"
    printf '%s ALL=(ALL) NOPASSWD:ALL\n' "$ADMIN" |
      limactl shell "$VM" -- sudo tee "/etc/sudoers.d/devmachine-$ADMIN" >/dev/null
    limactl shell "$VM" -- sudo chmod 440 "/etc/sudoers.d/devmachine-$ADMIN"
  fi
  limactl shell "$VM" -- sudo install -d -m 700 -o "$ADMIN" -g "$ADMIN" "$admin_home/.ssh"
  limactl shell "$VM" -- sudo tee "$admin_home/.ssh/authorized_keys" >/dev/null < "$KEY.pub"
  limactl shell "$VM" -- sudo chown "$ADMIN:$ADMIN" "$admin_home/.ssh/authorized_keys"
  limactl shell "$VM" -- sudo chmod 600 "$admin_home/.ssh/authorized_keys"

  # Read the key through Lima's control channel, before SSH is trusted. This
  # is deterministic test-fixture setup, not trust-on-first-use over the same
  # network path the integration tests are meant to verify.
  host_key=$(limactl shell "$VM" -- sudo cat /etc/ssh/ssh_host_ed25519_key.pub)
  key_type=${host_key%% *}
  key_body=${host_key#* }
  key_body=${key_body%% *}
  port=$(vm_port)
  lookup="[sandbox-devmachine]:$port"
  if [ "$port" = 22 ]; then
    lookup="sandbox-devmachine"
  fi
  mkdir -p "$(dirname "$KNOWN_HOSTS")"
  umask 077
  printf '%s %s %s\n' "$lookup" "$key_type" "$key_body" > "$KNOWN_HOSTS"

  echo "up: $ADMIN@127.0.0.1 port $port, key $KEY"
}

cmd_env() {
  require_lima
  echo "export DEVMACHINE_TEST_HOST=127.0.0.1"
  echo "export DEVMACHINE_TEST_PORT=$(vm_port)"
  echo "export DEVMACHINE_TEST_USER=$ADMIN"
  echo "export DEVMACHINE_TEST_KEY=$KEY"
  echo "export DEVMACHINE_TEST_KNOWN_HOSTS=$KNOWN_HOSTS"
}

case "${1:-}" in
  up) cmd_up ;;
  port) require_lima; vm_port ;;
  env) cmd_env ;;
  ssh)
    require_lima
    shift
    exec ssh -p "$(vm_port)" -i "$KEY" -o IdentitiesOnly=yes -o IdentityAgent=none \
      -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null \
      "$ADMIN@127.0.0.1" "$@"
    ;;
  down) require_lima; "${DEVMACHINE[@]}" machines delete-local --yes "$VM" ;;
  *) sed -n '2,12p' "$0"; exit 1 ;;
esac
