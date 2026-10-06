package provision

import (
	"fmt"
	"github.com/mydevmachine/devmachine/internal/remote"
	"strings"

	"github.com/mydevmachine/devmachine/internal/credentials"
	"github.com/mydevmachine/devmachine/internal/packages"
)

// credentialsTag runs the distribution of the shared logins on its own,
// without the packages that declared them.
const credentialsTag = "credentials"

// sharedLoopVar names the loop variable, for the reason loop_var is named
// everywhere else here: a role or a task with a loop of its own would rebind
// `item`.
const sharedLoopVar = "devmachine_shared"

// sharingTasks spread one login across the workspaces that share it.
//
// This is generated rather than written in a package because the CLI knows
// both halves and no package knows either: `stored_at` says where a tool keeps
// its session, and the configuration says which workspaces want the shared
// one. A package that wrote its own copy tasks would write them again, and
// slightly differently, for every shareable tool.
func sharingTasks(plan packages.MachinePlan) (string, error) {
	shared, err := credentials.Sharing(plan)
	if err != nil {
		return "", err
	}

	var out strings.Builder
	for _, s := range shared {
		register := "devmachine_shared_" + ansibleName(s.Name)
		tags := fmt.Sprintf("[%s, %s]", s.Package, credentialsTag)

		// Nobody can automate a browser login, so a run before it has happened
		// is ordinary rather than a mistake. It skips, and the next run picks
		// the session up.
		fmt.Fprintf(&out, "    - name: look for the shared %s login\n", s.Name)
		fmt.Fprintf(&out, "      stat:\n        path: %q\n", s.From)
		fmt.Fprintf(&out, "      register: %s\n", register)
		out.WriteString("      check_mode: false\n")
		fmt.Fprintf(&out, "      tags: %s\n\n", tags)

		rel, err := sharedHomePath(s.StoredAt)
		if err != nil {
			return "", fmt.Errorf("sharing the %s login: %w", s.Name, err)
		}

		fmt.Fprintf(&out, "    - name: the shared %s login, for the workspaces that want it\n", s.Name)
		out.WriteString("      shell: |\n")
		for _, line := range strings.Split(strings.TrimSuffix(sharedCopyCommand(s.From), "\n"), "\n") {
			out.WriteString("        " + line + "\n")
		}
		fmt.Fprintf(&out, "      register: %s_copy\n", register)
		fmt.Fprintf(&out, "      changed_when: \"'changed' in %s_copy.stdout\"\n", register)
		out.WriteString("      loop:\n")
		for _, account := range s.Into {
			fmt.Fprintf(&out, "        - {user: %q, path: %q}\n", account.LinuxUser, rel)
		}
		out.WriteString(loopControl())
		fmt.Fprintf(&out, "      when: %s.stat.exists\n", register)
		fmt.Fprintf(&out, "      tags: %s\n\n", tags)
	}
	return out.String(), nil
}

func loopControl() string {
	return "      loop_control:\n        loop_var: " + sharedLoopVar + "\n"
}

// sharedHomePath turns a package's `~/...` into the same path relative to
// any account's home. A shared login lands in each workspace's own home, so a
// `stored_at` anywhere else has nowhere to go.
func sharedHomePath(storedAt string) (string, error) {
	rest, inHome := strings.CutPrefix(storedAt, "~/")
	if !inHome {
		return "", fmt.Errorf("`stored_at` %q is not under ~/, so there is no copy of it for each workspace", storedAt)
	}
	return credentials.SafeWorkspacePath(rest)
}

// sharedCopyCommand copies the master at from into one account's home.
//
// Root only opens the master, which is its own. Everything inside the home —
// the directories, the file, the rename — runs as the account, because the
// account can put a link anywhere in its home: followed by root, that link
// would aim a root write, chmod or chown at any file on the machine. As the
// account, a link can only reach what the account could already write. The
// check before it is for the message, not the safety.
//
// A Mac has no runuser; root's sudo needs no password there.
func sharedCopyCommand(from string) string {
	return "set -eu\n" +
		"user={{ " + sharedLoopVar + ".user | quote }}\n" +
		"rel={{ " + sharedLoopVar + ".path | quote }}\n" +
		"if command -v runuser >/dev/null 2>&1; then\n" +
		"  set -- runuser -u \"$user\" --\n" +
		"else\n" +
		"  set -- sudo -n -u \"$user\" --\n" +
		"fi\n" +
		"\"$@\" /bin/sh -c '\n" +
		sharedCopyScript +
		"' devmachine-share \"$rel\" < " + shellQuote(from) + "\n"
}

// sharedCopyScript runs as the workspace account, with the path relative to
// its home as $1 and the master on stdin. It is wrapped in single quotes and
// goes through Ansible's templating, so it holds no single quote, and no
// brace followed by a brace, a percent sign or a hash.
const sharedCopyScript = `set -eu
umask 077
rel=$1
user=$(id -un)
` + remote.HomeLookup + `
outside() {
  echo "~/$rel reaches outside the home of $user through a symbolic link, so the shared login was not copied there" >&2
  exit 1
}
cd "$home"
real_home=$(pwd -P)
dir=$(dirname "$rel")
probe=$dir
while [ ! -e "$probe" ] && [ ! -L "$probe" ]; do
  probe=$(dirname "$probe")
done
if ! real_dir=$(cd -P "$probe" 2>/dev/null && pwd -P); then
  echo "~/$rel: ~/$probe is not a directory" >&2
  exit 1
fi
case "$real_dir/" in
"$real_home"/*) ;;
*) outside ;;
esac
if [ -L "$rel" ]; then
  outside
fi
mkdir -p "$dir"
if [ "$dir" != . ]; then
  chmod 0700 "$dir"
fi
tmp=$(mktemp "$dir/.devmachine.XXXXXX")
cat > "$tmp"
mode=$(stat -c %a "$rel" 2>/dev/null || stat -f %Lp "$rel" 2>/dev/null || true)
if [ -f "$rel" ] && [ "$mode" = 600 ] && cmp -s "$tmp" "$rel"; then
  rm -f "$tmp"
  exit 0
fi
chmod 0600 "$tmp"
mv -f "$tmp" "$rel"
echo changed
`

// ansibleName turns a credential's name into something a register accepts.
func ansibleName(name string) string {
	return strings.NewReplacer("-", "_", ".", "_").Replace(name)
}
