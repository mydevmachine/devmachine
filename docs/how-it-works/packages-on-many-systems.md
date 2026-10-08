# One package on many systems

A machine runs Debian, Ubuntu, Arch Linux or macOS. A package is written
once for all of them: never `gh-debian` and `gh-arch`. It carries a file
per system only where that system really differs, and says in
[`platforms`](../reference/package-format.md#platforms) which systems it
runs on. This page is for whoever writes a package: how the built-in
packages do it, and the traps they already hit.

## Load the names for this system

A package that needs different names or paths per system keeps them in
`vars/`, and loads the first file that matches, most specific first:

```yaml
# A loop over first_found, not a plain include_vars: when no file matches, the
# loop is empty and the task skips, on ansible-core 2.14 as on the latest.
- name: Load the names for this system
  ansible.builtin.include_vars: "{{ devmachine_vars_file }}"
  loop: "{{ query('ansible.builtin.first_found', devmachine_vars_chain) }}"
  loop_control:
    loop_var: devmachine_vars_file
  vars:
    devmachine_vars_chain:
      files:
        - "{{ ansible_facts['distribution'] }}.yml"
        - "{{ ansible_facts['pkg_mgr'] }}.yml"
        - "{{ ansible_facts['os_family'] }}.yml"
        - default.yml
      paths:
        - "{{ role_path }}/vars"
      skip: true
```

What each name matches:

| File | Matches |
| --- | --- |
| `Ubuntu.yml`, `Debian.yml`, `Archlinux.yml`, `MacOSX.yml` | one distribution (`distribution`) |
| `apt.yml`, `pacman.yml`, `homebrew.yml`, `macports.yml` | one package manager (`pkg_mgr`) |
| `Debian.yml`, `Archlinux.yml`, `Darwin.yml` | a family (`os_family`): `Debian.yml` serves Ubuntu too |
| `default.yml` | everything else |

A package with no difference between systems has no `vars/` file and no
such task. Defaults that hold everywhere stay in `defaults/main.yml`, and
the files in `vars/` only change what differs.

Read facts as `ansible_facts['...']`, never as the `ansible_*` variables.

## How to install a tool

Take the first of these that works for the tool:

1. The project's official installer, such as `curl -fsSL https://mise.run | sh`.
2. A release binary for the system and the architecture, from the
   project's releases.
3. `mise`, for a language or a tool mise knows.
4. The `package` module, with the names in `vars/`. Never `apt`: the CLI
   refuses a package that uses it.
5. `tasks/<System>.yml`, only when the steps themselves differ, not only
   the names.

A release binary is named the way Go names systems and architectures, not
the way the facts do. `ansible_facts['system'] | lower` gives `linux` or
`darwin`. The architecture needs a map: Linux reports `x86_64` or
`aarch64`, an Apple Silicon Mac reports `arm64`, and a release asset says
`amd64` or `arm64`. The `glab` package carries the map:

```yaml
devmachine_go_architectures:
  x86_64: amd64
  aarch64: arm64
  armv7l: armv6
devmachine_go_architecture: >-
  {{ devmachine_go_architectures.get(ansible_facts['architecture'], ansible_facts['architecture']) }}
```

## Names across systems

What the built-in packages install, by system. Check a name before you
use it (see below); this table is a start, not the source.

| What | Debian, Ubuntu | Arch Linux | Homebrew | MacPorts |
| --- | --- | --- | --- | --- |
| build toolchain | `build-essential` | `base-devel` | the Command Line Tools | the Command Line Tools |
| curl, git, tar, unzip, zsh | same name | same name | come with macOS | come with macOS |
| SSH client | `openssh-client` | `openssh` | comes with macOS | comes with macOS |
| ACLs for Ansible's `become` | `acl` | `acl` | not needed | not needed |
| tmux, mosh, ffmpeg | same name | same name | same name | same name |
| GnuPG | `gnupg` | `gnupg` | `gnupg` | `gnupg2` |
| OpenSSL headers | `libssl-dev` | `openssl` | `openssl@3` | `openssl3` |
| zlib headers | `zlib1g-dev` | `zlib` | `zlib` | `zlib` |
| YAML headers | `libyaml-dev` | `libyaml` | `libyaml` | `libyaml` |
| readline headers | `libreadline-dev` | `readline` | `readline` | `readline` |
| libffi headers | `libffi-dev` | `libffi` | `libffi` | `libffi` |
| GMP headers | `libgmp-dev` | `gmp` | `gmp` | `gmp` |
| Docker | Docker's repository | `docker`, `docker-buildx`, `docker-compose` | — | — |

Arch ships headers inside the library package, so there is no `-dev` to
add. Homebrew keeps `openssl@3`, `readline`, `zlib` and `libffi` out of its
prefix (keg-only): a build finds them through `brew --prefix <name>`.

## Traps already hit

Each of these broke a real run before the packages handled it.

**Swap on btrfs.** `fallocate` and `swapon` fail with `swapon: /swapfile:
swapon failed: Invalid argument` when `/` is btrfs, as on Arch. Make the
file with `btrfs filesystem mkswapfile` there, and `fallocate` elsewhere.
macOS manages its own swap: `base` makes none on a Mac.

**No `LANG` over SSH on Arch.** Nothing reads `/etc/locale.conf` for an SSH
login, and `mosh-server` stops with `mosh-server needs a UTF-8 native
locale to run`. `base` writes `LANG` to `/etc/environment`, which `pam_env`
reads for every login.

**`distribution_release` is `NA` on Arch.** Arch is a rolling release. A
repository line built from the release name (Docker's, Tailscale's) belongs
in `vars/Debian.yml`, never in a task every system runs.

**No cache refresh alone on Arch.** `pacman -Sy` without `-u` is a partial
upgrade, which Arch does not support. Refresh the lists only together with
an upgrade, and only when the operator asked for one.

**No group named after the account on macOS.** Every account is in `staff`
(gid 20); `group: alice` fails with `chgrp failed: failed to look up group
alice`. The `root` group does not exist either: a file root owns takes
`wheel`. Read the account's home and group from the account itself, with
the `user` module in check mode, which reads and never creates:

```yaml
# check_mode makes the user module only read: it never creates the account,
# and a missing one just leaves no home in the result.
- name: Read the account's home and group
  ansible.builtin.user:
    name: "{{ devmachine_workspace.user }}"
  check_mode: true
  changed_when: false
  register: devmachine_account
```

Then use `devmachine_account.home | default('/home/' ~ devmachine_workspace.user)`
for the home, and `devmachine_account.group | default(omit)` for the group.
Every package that reads `devmachine_account` registers it itself. A
registered variable outlives the role, so a package that skips this task
reads whatever account the previous package left, which can belong to
another workspace. `packages validate` warns about such a package.
macOS has no `getent`, and homes are in `/Users`.

**Homebrew refuses root.** On a Mac reached over SSH the play runs as root,
and `brew` refuses to. A task that installs through Homebrew becomes the
account that owns it:

```yaml
- name: Find who owns Homebrew
  ansible.builtin.stat:
    path: "{{ '/opt/homebrew' if ansible_facts['architecture'] == 'arm64' else '/usr/local' }}/bin/brew"
  register: devmachine_base_brew
  when: ansible_facts['pkg_mgr'] == 'homebrew'

- name: Install the base packages from Homebrew
  ansible.builtin.package:
    name: "{{ devmachine_base_packages }}"
    state: present
  become: "{{ devmachine_base_brew.stat.uid != ansible_facts['effective_user_id'] }}"
  become_user: "{{ devmachine_base_brew.stat.pw_name }}"
  when: ansible_facts['pkg_mgr'] == 'homebrew'
```

MacPorts is the other way round: it installs as root, so the play stays
root.

**MacPorts has no check mode.** `community.general.macports` does not
support check mode, so `sync --check` skips its tasks and shows nothing
they would install. Do not read an empty dry run as "nothing to do".

**A plain SSH command on a Mac has a short `PATH`.** It gets
`/usr/bin:/bin:/usr/sbin:/sbin`; Homebrew (`/opt/homebrew/bin` or
`/usr/local/bin`) and MacPorts (`/opt/local/bin`) are added only by a login
shell. Call a tool by its absolute path, or set `PATH` in the task's
`environment`.

**`platforms`.** A package that cannot work on a system says so:
`firewall`, `fail2ban`, `caddy` and `docker` declare `platforms: [linux]`,
`mac-brew` and `mac-ports` declare `[macos]`. `sync` refuses a package for
another system before it changes the machine, naming both. Left out, the
package claims to run everywhere, so leave it out only when that is true.

## Check, never guess

A module's options, a package name and a fact's value differ by system and
by version. Look them up:

| What | How |
| --- | --- |
| a module's options and its check-mode support | `ansible-doc <module>`, or docs.ansible.com |
| a Debian or Ubuntu package | `apt-cache policy <name>` |
| an Arch package | `pacman -Si <name>` |
| a Homebrew formula | `brew info <name>` |
| a MacPorts port | `port info <name>` |
| what the machine reports | `devmachine --format json machines show <machine>`, under `observed` |

## Prove it on every system it declares

A package that says `platforms: [linux, macos]` is proved on Debian or
Ubuntu, on Arch and on a Mac, not on one of them. Each proof is the same:
`devmachine sync` ends with `failed=0`, and a second `sync` reports
`changed=0`. A machine for this is cheap: `devmachine machines
create-local <name>` makes an Ubuntu VM, and `--distro arch` an Arch one.
