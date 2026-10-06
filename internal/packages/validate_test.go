package packages

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateAcceptsASkillContribution(t *testing.T) {
	dir := writePackage(t, "global-skills", `format: 1
name: global-skills
scope: workspace
summary: Shared skills.
skills:
  path: skills
`)
	write(t, filepath.Join(dir, "tasks", "main.yml"), "---\n[]\n")
	writePackageSkill(t, dir, "use-devmachine", "Use Devmachine.")

	problems, err := Validate(dir)
	if err != nil || len(problems) != 0 {
		t.Fatalf("%v %#v", err, problems)
	}
}

func TestValidateRejectsASkillPathOutsideThePackage(t *testing.T) {
	dir := writePackage(t, "global-skills", `format: 1
name: global-skills
scope: workspace
summary: Shared skills.
skills:
  path: ../skills
`)
	write(t, filepath.Join(dir, "tasks", "main.yml"), "---\n[]\n")

	problems, err := Validate(dir)
	if err != nil {
		t.Fatal(err)
	}
	problemAbout(t, problems, "stay inside the package")
}

func TestValidateRejectsASkillPathThroughAnEscapingParentSymlink(t *testing.T) {
	dir := writePackage(t, "global-skills", `format: 1
name: global-skills
scope: workspace
summary: Shared skills.
skills:
  path: linked/skills
`)
	write(t, filepath.Join(dir, "tasks", "main.yml"), "---\n[]\n")
	outside := t.TempDir()
	writePackageSkill(t, outside, "use-devmachine", "Use Devmachine.")
	if err := os.Symlink(outside, filepath.Join(dir, "linked")); err != nil {
		t.Fatal(err)
	}

	problems, err := Validate(dir)
	if err != nil {
		t.Fatal(err)
	}
	problemAbout(t, problems, "stay inside the package")
}

func TestValidateReportsEveryMalformedSkill(t *testing.T) {
	dir := writePackage(t, "global-skills", `format: 1
name: global-skills
scope: workspace
summary: Shared skills.
skills:
  path: skills
`)
	write(t, filepath.Join(dir, "tasks", "main.yml"), "---\n[]\n")
	writePackageSkill(t, dir, "wrong-name", "")
	write(t, filepath.Join(dir, "skills", "wrong-name", "SKILL.md"), "---\nname: something-else\ndescription: ''\n---\n")
	write(t, filepath.Join(dir, "skills", "missing-description", "SKILL.md"), "---\nname: missing-description\n---\n")

	problems, err := Validate(dir)
	if err != nil {
		t.Fatal(err)
	}
	p := problemAbout(t, problems, "wrong-name")
	if !strings.Contains(p.What, "missing-description") {
		t.Fatalf("did not report all malformed skills: %#v", problems)
	}
}

func writePackageSkill(t *testing.T, dir, name, description string) {
	t.Helper()
	path := filepath.Join(dir, "skills", name)
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: " + name + "\ndescription: " + description + "\n---\n\n# " + name + "\n"
	write(t, filepath.Join(path, "SKILL.md"), body)
}

func TestValidateRejectsAPackageWithNoFormat(t *testing.T) {
	dir := writePackage(t, "git", "name: git\nscope: machine\nsummary: x\n")

	problems, err := Validate(dir)
	if err != nil {
		t.Fatal(err)
	}
	problemAbout(t, problems, "format")
}

func TestValidateRejectsAFormatFromTheFuture(t *testing.T) {
	dir := writePackage(t, "git", "format: 99\nname: git\nscope: machine\nsummary: x\n")

	problems, err := Validate(dir)
	if err != nil {
		t.Fatal(err)
	}
	p := problemAbout(t, problems, "99")
	// A recipe written for a newer CLI has to say so, not fail somewhere
	// deeper with a message about a field.
	if !strings.Contains(p.What, "brew upgrade") {
		t.Fatalf("the message does not say what to do: %q", p.What)
	}
}

func TestValidateStopsAtAFormatItCannotRead(t *testing.T) {
	// Every other message assumes the fields mean what this CLI thinks they
	// mean. Reporting them on top of a format mismatch is noise at best.
	dir := writePackage(t, "git", "format: 99\nscope: nonsense\n")

	problems, err := Validate(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 1 {
		t.Fatalf("got %#v", problems)
	}
}

func TestValidateRejectsAnUnknownScopeAndSaysWhere(t *testing.T) {
	dir := writePackage(t, "sharing", "format: 1\nname: sharing\nscope: user\nsummary: x\n")

	problems, err := Validate(dir)
	if err != nil {
		t.Fatal(err)
	}
	p := problemAbout(t, problems, "scope")
	if p.Line != 3 {
		t.Fatalf("pointed at line %d", p.Line)
	}
	if !strings.Contains(p.What, `"machine"`) || !strings.Contains(p.What, `"user"`) {
		t.Fatalf("the message does not say what was got or wanted: %q", p.What)
	}
}

func TestValidateRejectsTheAptModule(t *testing.T) {
	dir := writePackage(t, "git", "format: 1\nname: git\nscope: machine\nsummary: x\n")
	write(t, filepath.Join(dir, "tasks", "main.yml"), `---
- name: Install git
  apt:
    name: git
    state: present
`)

	problems, err := Validate(dir)
	if err != nil {
		t.Fatal(err)
	}
	p := problemAbout(t, problems, "apt")
	if p.File != filepath.Join("tasks", "main.yml") || p.Line != 3 {
		t.Fatalf("pointed at %s:%d", p.File, p.Line)
	}
	if !strings.Contains(p.What, "package") {
		t.Fatalf("the message does not say what to use instead: %q", p.What)
	}
}

func TestValidateScansAPackageReachedThroughASymlink(t *testing.T) {
	dir := writePackage(t, "git", "format: 1\nname: git\nscope: machine\nsummary: x\n")
	write(t, filepath.Join(dir, "tasks", "main.yml"), "---\n- name: Install git\n  apt:\n    name: git\n")
	link := filepath.Join(t.TempDir(), "git")
	if err := os.Symlink(dir, link); err != nil {
		t.Fatal(err)
	}

	problems, err := Validate(link)
	if err != nil {
		t.Fatal(err)
	}
	problemAbout(t, problems, "apt")
}

func TestValidateRejectsAnExtendsWithoutADot(t *testing.T) {
	dir := writePackage(t, "sharing", `format: 1
name: sharing
scope: workspace
summary: x
extends:
  sitesd: files/sharing.caddy
`)

	problems, err := Validate(dir)
	if err != nil {
		t.Fatal(err)
	}
	p := problemAbout(t, problems, "extends")
	if !strings.Contains(p.What, "<package>.<place>") {
		t.Fatalf("the message does not give the shape: %q", p.What)
	}
}

func TestValidateRejectsAnExtendsFileThatIsNotThere(t *testing.T) {
	dir := writePackage(t, "sharing", `format: 1
name: sharing
scope: workspace
summary: x
extends:
  caddy.sites.d: files/sharing.caddy
`)

	problems, err := Validate(dir)
	if err != nil {
		t.Fatal(err)
	}
	if problemAbout(t, problems, "files/sharing.caddy").Line == 0 {
		t.Fatal("the missing file was not pointed at")
	}
}

func TestValidateRejectsAnExtensionPointThatIsNotAnAbsolutePath(t *testing.T) {
	dir := writePackage(t, "caddy", `format: 1
name: caddy
scope: machine
summary: x
provides:
  sites.d: etc/caddy/sites.d
`)
	write(t, filepath.Join(dir, "tasks", "main.yml"), "---\n- name: x\n  package: {name: caddy}\n")

	problems, err := Validate(dir)
	if err != nil {
		t.Fatal(err)
	}
	problemAbout(t, problems, "absolute path")
}

func TestValidateRejectsAPackageWithNoTasks(t *testing.T) {
	dir := writePackage(t, "git", "format: 1\nname: git\nscope: machine\nsummary: x\n")

	problems, err := Validate(dir)
	if err != nil {
		t.Fatal(err)
	}
	problemAbout(t, problems, "tasks/main.yml")
}

func TestValidateRejectsARequiresCLIItCannotRead(t *testing.T) {
	dir := writePackage(t, "git", "format: 1\nname: git\nscope: machine\nsummary: x\nrequires:\n  cli: latest\n")
	write(t, filepath.Join(dir, "tasks", "main.yml"), "---\n- name: x\n  package: {name: git}\n")

	problems, err := Validate(dir)
	if err != nil {
		t.Fatal(err)
	}
	problemAbout(t, problems, "requires.cli")
}

func TestValidateAcceptsAGoodPackage(t *testing.T) {
	dir := writePackage(t, "git", "format: 1\nname: git\nscope: machine\nsummary: Installs git.\nneeds: [base]\n")
	write(t, filepath.Join(dir, "tasks", "main.yml"), "---\n- name: Install git\n  package:\n    name: git\n    state: present\n")

	problems, err := Validate(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 0 {
		t.Fatalf("got %#v", problems)
	}
}

func TestValidateRejectsANameThatIsNotTheDirectory(t *testing.T) {
	dir := writePackage(t, "git", "format: 1\nname: not-git\nscope: machine\nsummary: x\n")
	write(t, filepath.Join(dir, "tasks", "main.yml"), "---\n- name: x\n  package: {name: git}\n")

	problems, err := Validate(dir)
	if err != nil {
		t.Fatal(err)
	}
	problemAbout(t, problems, "not-git")
}

// callableManifest is the smallest callable package that is otherwise
// correct, so each test below changes exactly one thing. It has no tasks, so
// every test using it also expects the tasks/main.yml problem.
const callableManifest = `format: 1
name: hostinger
scope: machine
kind: dns
summary: DNS zones on a registrar.
entrypoint: bin/provider
commands: ["*"]
`

func TestValidateAcceptsAProviderPackage(t *testing.T) {
	dir := writePackage(t, "hostinger", `format: 1
name: hostinger
scope: machine
kind: dns
summary: DNS zones on a registrar.
entrypoint: bin/provider
credentials:
  - name: hostinger
    kind: secret
    scope: machine
    env: HOSTINGER_TOKEN
commands: [zones, list, upsert, delete, help]
`)
	write(t, filepath.Join(dir, "tasks", "main.yml"), "---\n- name: x\n  package: {name: python3}\n")
	writeMode(t, filepath.Join(dir, "bin", "provider"), "#!/usr/bin/env python3\n", 0o755)

	problems, err := Validate(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 0 {
		t.Fatalf("got %#v", problems)
	}
}

func TestValidateRefusesAnEntrypointThatIsNotExecutable(t *testing.T) {
	dir := writePackage(t, "hostinger", callableManifest)
	writeMode(t, filepath.Join(dir, "bin", "provider"), "#!/usr/bin/env python3\n", 0o644)

	problems, err := Validate(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(problemAbout(t, problems, "entrypoint").What, "chmod") {
		t.Fatal("the message does not say how to fix it")
	}
}

func TestValidateRefusesAnEntrypointThatIsNotPython(t *testing.T) {
	dir := writePackage(t, "hostinger", callableManifest)
	writeMode(t, filepath.Join(dir, "bin", "provider"), "#!/bin/bash\n", 0o755)

	problems, err := Validate(dir)
	if err != nil {
		t.Fatal(err)
	}
	problemAbout(t, problems, "python3")
}

func TestValidateRefusesAnEntrypointThatIsNotThere(t *testing.T) {
	dir := writePackage(t, "hostinger", callableManifest)

	problems, err := Validate(dir)
	if err != nil {
		t.Fatal(err)
	}
	problemAbout(t, problems, "is not in the package")
}

func TestValidateRefusesADNSPackageMissingAContractCommand(t *testing.T) {
	dir := writePackage(t, "hostinger", `format: 1
name: hostinger
scope: machine
kind: dns
summary: x
entrypoint: bin/provider
commands: [list, upsert]
`)
	writeMode(t, filepath.Join(dir, "bin", "provider"), "#!/usr/bin/env python3\n", 0o755)

	problems, err := Validate(dir)
	if err != nil {
		t.Fatal(err)
	}
	// A kind is a contract. A dns package that cannot answer `zones` cannot
	// be discovered, and one that cannot answer `help` cannot be read.
	for _, want := range []string{"zones", "delete", "help"} {
		problemAbout(t, problems, want)
	}
}

func TestValidateRefusesAnUnknownKind(t *testing.T) {
	dir := writePackage(t, "hostinger", `format: 1
name: hostinger
scope: machine
kind: registrar
summary: x
entrypoint: bin/provider
commands: ["*"]
`)
	writeMode(t, filepath.Join(dir, "bin", "provider"), "#!/usr/bin/env python3\n", 0o755)

	problems, err := Validate(dir)
	if err != nil {
		t.Fatal(err)
	}
	problemAbout(t, problems, "registrar")
}

func TestValidateRefusesAnEntrypointWithNoCommands(t *testing.T) {
	dir := writePackage(t, "hostinger", "format: 1\nname: hostinger\nscope: machine\nsummary: x\nentrypoint: bin/provider\n")
	writeMode(t, filepath.Join(dir, "bin", "provider"), "#!/usr/bin/env python3\n", 0o755)

	problems, err := Validate(dir)
	if err != nil {
		t.Fatal(err)
	}
	problemAbout(t, problems, "what it accepts")
}

func TestValidateRefusesAStarBesideOtherCommands(t *testing.T) {
	dir := writePackage(t, "hostinger", `format: 1
name: hostinger
scope: machine
kind: dns
summary: x
entrypoint: bin/provider
commands: ["*", list]
`)
	writeMode(t, filepath.Join(dir, "bin", "provider"), "#!/usr/bin/env python3\n", 0o755)

	problems, err := Validate(dir)
	if err != nil {
		t.Fatal(err)
	}
	problemAbout(t, problems, "two things at once")
}

func TestValidateRefusesAKindWithNoEntrypoint(t *testing.T) {
	dir := writePackage(t, "git", "format: 1\nname: git\nscope: machine\nsummary: x\nkind: dns\n")
	write(t, filepath.Join(dir, "tasks", "main.yml"), "---\n- name: x\n  package: {name: git}\n")

	problems, err := Validate(dir)
	if err != nil {
		t.Fatal(err)
	}
	problemAbout(t, problems, "declares none")
}

func TestValidateRefusesALoginWithNoCommand(t *testing.T) {
	dir := writePackage(t, "claude-code", `format: 1
name: claude-code
scope: workspace
summary: x
credentials:
  - name: claude
    kind: manual
    scope: workspace
    stored_at: ~/.claude/.credentials.json
`)
	write(t, filepath.Join(dir, "tasks", "main.yml"), "---\n- name: x\n  package: {name: nodejs}\n")

	problems, err := Validate(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Nobody but the package knows how its tool logs in. A login credential
	// with no command is a credential nothing can act on.
	problemAbout(t, problems, "command")
}

func TestValidateRefusesALoginWithNoStoredAt(t *testing.T) {
	dir := writePackage(t, "claude-code", `format: 1
name: claude-code
scope: workspace
summary: x
credentials:
  - name: claude
    kind: manual
    scope: workspace
    command: claude /login
`)
	write(t, filepath.Join(dir, "tasks", "main.yml"), "---\n- name: x\n  package: {name: nodejs}\n")

	problems, err := Validate(dir)
	if err != nil {
		t.Fatal(err)
	}
	problemAbout(t, problems, "stored_at")
}

func TestValidateRefusesASecretWithNowhereToLand(t *testing.T) {
	dir := writePackage(t, "hostinger", `format: 1
name: hostinger
scope: machine
summary: x
credentials:
  - name: hostinger
    kind: secret
    scope: machine
`)
	write(t, filepath.Join(dir, "tasks", "main.yml"), "---\n- name: x\n  package: {name: python3}\n")

	problems, err := Validate(dir)
	if err != nil {
		t.Fatal(err)
	}
	problemAbout(t, problems, "env` or `path")
}

func TestValidateRefusesAFileCredentialWithNoPath(t *testing.T) {
	dir := writePackage(t, "vpn", `format: 1
name: vpn
scope: machine
summary: x
credentials:
  - name: vpn_profile
    kind: file
    scope: machine
`)
	write(t, filepath.Join(dir, "tasks", "main.yml"), "---\n- name: x\n  package: {name: openvpn}\n")

	problems, err := Validate(dir)
	if err != nil {
		t.Fatal(err)
	}
	problemAbout(t, problems, "the `path` it lands at")
}

func TestValidateRefusesACredentialWithAnUnknownKind(t *testing.T) {
	dir := writePackage(t, "vpn", `format: 1
name: vpn
scope: machine
summary: x
credentials:
  - name: vpn_profile
    kind: keychain
    scope: machine
`)
	write(t, filepath.Join(dir, "tasks", "main.yml"), "---\n- name: x\n  package: {name: openvpn}\n")

	problems, err := Validate(dir)
	if err != nil {
		t.Fatal(err)
	}
	problemAbout(t, problems, "keychain")
}

func TestValidateRefusesACredentialWithNoName(t *testing.T) {
	dir := writePackage(t, "vpn", `format: 1
name: vpn
scope: machine
summary: x
credentials:
  - kind: secret
    scope: machine
    env: VPN_TOKEN
`)
	write(t, filepath.Join(dir, "tasks", "main.yml"), "---\n- name: x\n  package: {name: openvpn}\n")

	problems, err := Validate(dir)
	if err != nil {
		t.Fatal(err)
	}
	problemAbout(t, problems, "needs a `name`")
}

// There is no third scope. A credential is held by the machine or by one
// workspace, and nothing else is a place it could live.
func TestValidateRefusesACredentialScopeThatIsNeitherMachineNorWorkspace(t *testing.T) {
	dir := writePackage(t, "vpn", `format: 1
name: vpn
scope: machine
summary: x
credentials:
  - name: vpn_profile
    kind: secret
    scope: local
    env: VPN_TOKEN
`)
	write(t, filepath.Join(dir, "tasks", "main.yml"), "---\n- name: x\n  package: {name: openvpn}\n")

	problems, err := Validate(dir)
	if err != nil {
		t.Fatal(err)
	}
	problemAbout(t, problems, `got "local"`)
}

func TestValidateAcceptsAFullyDeclaredLogin(t *testing.T) {
	dir := writePackage(t, "claude-code", `format: 1
name: claude-code
scope: workspace
summary: Claude Code, logged in per workspace.
credentials:
  - name: claude
    kind: manual
    scope: workspace
    command: claude /login
    stored_at: ~/.claude/.credentials.json
`)
	write(t, filepath.Join(dir, "tasks", "main.yml"), "---\n- name: x\n  package: {name: nodejs}\n")

	problems, err := Validate(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 0 {
		t.Fatalf("got %#v", problems)
	}
}

func TestProblemErrorPointsAtALine(t *testing.T) {
	p := Problem{File: FileName, Line: 3, What: "x"}
	if p.Error() != "package.yml:3: x" {
		t.Fatalf("got %q", p.Error())
	}
	if (Problem{File: FileName, What: "x"}).Error() != "package.yml: x" {
		t.Fatal("a problem with no line should not print one")
	}
}

func TestValidateRefusesAShareableSecret(t *testing.T) {
	dir := writePackage(t, "hostinger", `format: 1
name: hostinger
scope: machine
summary: x
credentials:
  - name: hostinger
    kind: secret
    scope: machine
    shareable: true
    env: HOSTINGER_TOKEN
`)
	write(t, filepath.Join(dir, "tasks", "main.yml"), "---\n- name: x\n  package: {name: python3}\n")

	problems, err := Validate(dir)
	if err != nil {
		t.Fatal(err)
	}
	// A secret is delivered to each place that wants it, not copied from one
	// of them. `shareable` is about copying a session file, and saying it
	// here means the author expects something that will not happen.
	problemAbout(t, problems, "shareable")
}

func TestValidateRefusesAShareableFile(t *testing.T) {
	dir := writePackage(t, "vpn", `format: 1
name: vpn
scope: machine
summary: x
credentials:
  - name: vpn
    kind: file
    scope: machine
    shareable: true
    path: /etc/openvpn/client.conf
`)
	write(t, filepath.Join(dir, "tasks", "main.yml"), "---\n- name: x\n  package: {name: openvpn}\n")

	problems, err := Validate(dir)
	if err != nil {
		t.Fatal(err)
	}
	problemAbout(t, problems, "shareable")
}

func TestValidateAllowsAShareableLogin(t *testing.T) {
	dir := writePackage(t, "dev", `format: 1
name: dev
scope: workspace
summary: x
credentials:
  - name: gh
    kind: manual
    scope: machine
    shareable: true
    command: gh auth login
    stored_at: ~/.config/gh/hosts.yml
`)
	write(t, filepath.Join(dir, "tasks", "main.yml"), "---\n- name: x\n  package: {name: gh}\n")

	problems, err := Validate(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 0 {
		t.Fatalf("a shareable login is ordinary: %#v", problems)
	}
}

func TestValidateRefusesAMachineLoginThatCannotTravel(t *testing.T) {
	dir := writePackage(t, "dev", `format: 1
name: dev
scope: workspace
summary: x
credentials:
  - name: gh
    kind: manual
    scope: machine
    command: gh auth login
    stored_at: ~/.config/gh/hosts.yml
`)
	write(t, filepath.Join(dir, "tasks", "main.yml"), "---\n- name: x\n  package: {name: gh}\n")

	problems, err := Validate(dir)
	if err != nil {
		t.Fatal(err)
	}
	// `scope: machine` recommends one login copied into every workspace, and
	// `shareable` left out says a copy does not work. The package is asking
	// for something it also says is impossible.
	problemAbout(t, problems, "shareable")
}

func TestValidateAllowsAKindNoCommandConsumes(t *testing.T) {
	// A kind classifies always. `vpn` is a label: no command reads it, so
	// demanding an entrypoint for it would only stop a package saying what
	// it is.
	dir := writePackage(t, "tailscale", "format: 1\nname: tailscale\nscope: machine\nkind: vpn\nsummary: x\n")
	write(t, filepath.Join(dir, "tasks", "main.yml"), "---\n- name: x\n  package: {name: tailscale}\n")

	problems, err := Validate(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 0 {
		t.Fatalf("a kind with no contract is ordinary: %#v", problems)
	}
}

func TestValidateRefusesCommandsWithNoEntrypoint(t *testing.T) {
	dir := writePackage(t, "git", "format: 1\nname: git\nscope: machine\nsummary: x\ncommands: [list]\n")
	write(t, filepath.Join(dir, "tasks", "main.yml"), "---\n- name: x\n  package: {name: git}\n")

	problems, err := Validate(dir)
	if err != nil {
		t.Fatal(err)
	}
	problemAbout(t, problems, "declares none")
}

func TestValidateAllowsAMachineCredentialThatCannotTravelOnAMachinePackage(t *testing.T) {
	// A tailnet's state names one machine, so a copy of it is worth nothing
	// anywhere else. The package never reaches a workspace, so nothing is
	// asking for the copy that `shareable: false` says cannot be made.
	dir := writePackage(t, "tailscale", `format: 1
name: tailscale
scope: machine
kind: vpn
summary: x
credentials:
  - name: tailscale
    kind: manual
    scope: machine
    command: tailscale up
    stored_at: /var/lib/tailscale/tailscaled.state
`)
	write(t, filepath.Join(dir, "tasks", "main.yml"), "---\n- name: x\n  package: {name: tailscale}\n")

	problems, err := Validate(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 0 {
		t.Fatalf("a machine's own credential is not shared with anybody: %#v", problems)
	}
}
