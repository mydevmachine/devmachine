# Password login, account by account

`setup` and `machines add` turn SSH password login off once the key is
proved. On a server that only you reach, that is the whole story: the key
is the only way in, and a password nobody can guess is better than one
somebody might.

A machine people use is different. A Mac has the accounts of the people
who sit at it, and some of them may SSH in with their password. Turning
password login off reaches **every account**, not only the admin login
the CLI uses: SSH has one setting for the whole machine. So before it
changes anything, the wizard asks:

```
SSH password login, now that the key is proved:
On a Mac this is every account on it. Whoever logs in over SSH with a password needs a key afterwards; the login window, sudo and Screen Sharing keep their passwords.
Other accounts on this machine: alice, bob.
  1) off for every account
  2) off, except for accounts you name
  3) leave it as it is
choice [1]:
```

Enter keeps the default, off for everybody. `--keep-password-login a,b`
answers it without a terminal, and `--no-harden` is the third choice.
Only SSH changes: the login window, `sudo`, Screen Sharing and FileVault
keep their passwords.

## How an account keeps its password

The file the CLI writes, `/etc/ssh/sshd_config.d/00-devmachine-hardening.conf`,
turns password login off for everybody, then ends with a `Match User`
block that turns it back on for the accounts you named:

```
PasswordAuthentication no
KbdInteractiveAuthentication no
PermitRootLogin prohibit-password
Match User alice,bob
	PasswordAuthentication yes
	KbdInteractiveAuthentication yes
```

A `Match` block runs to the end of the file it is in. It does **not** run
on into the main `sshd_config` past the `Include` line, which would make
every later line in that file apply to those accounts only. This was
checked, not assumed: with a `Subsystem` line after the `Include` —
which `sshd -t` refuses inside a `Match` — `sshd -t` accepts the
configuration, and `sshd -T -C user=…` gives `yes` for a kept account and
`no` for the rest, on OpenSSH 9.6 (Ubuntu 24.04) and 9.9 (macOS). The
CLI's own tests run the same check against the `sshd` they find. Still,
the block stays last in the file, so a different reading of the rule
could only affect lines the CLI does not write.

After the change the CLI asks `sshd -T` twice: once for an account no
exception names, which must say `no`, and once for each kept account,
which must say `yes`. A file that does not do what it says is taken back.

## One place for the exceptions

The accounts are written on the machine in `config.yml`:

```yaml
machines:
  - name: studio
    hosts: [203.0.113.20]
    password_login_keep: [alice, bob]
```

They live on the machine, not in a package setting, because `setup`
turns password login off before any package exists. The `ssh_hardening`
package writes the same file on every sync, and it reads this list, so a
sync keeps the same exceptions instead of wiping them.

## Changing it later

```
devmachine machines password-login studio                 # who can log in with a password
devmachine machines password-login studio --keep alice    # off, except alice
devmachine machines password-login studio --off           # off for every account
devmachine machines password-login studio --on            # the system's own setting
```

The change goes over the admin login's key, which is the proof that a
way in stays open. `--on` takes the CLI's file away, so the system's own
setting applies again: on macOS, Debian and Ubuntu that is password login
on. It is refused while the machine has `ssh_hardening`, which turns
password login off again on every sync.

Each change keeps a copy of the file it replaced in
`/etc/ssh/.devmachine-hardening.conf.previous`, outside `sshd_config.d`:
macOS reads every file in that folder, not only `*.conf`, so a copy kept
there would be read as configuration.
