package remote

// HomeLookup is a shell fragment that sets $home to the home directory of the
// account named in $user, empty when there is no such account.
//
// getent where there is one, which is every Linux, so the lookup there is what
// it always was. A Mac has no getent; its accounts are in Directory Services,
// which dscl reads. It holds no single quote, brace pair or percent sign, so
// it survives fmt, single quoting and Ansible's templating.
const HomeLookup = `if command -v getent >/dev/null 2>&1; then
  home=$(getent passwd "$user" | cut -d: -f6)
else
  home=$(dscl . -read "/Users/$user" NFSHomeDirectory 2>/dev/null | cut -d" " -f2-)
fi`
