package commands

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mydevmachine/devmachine/internal/config"
	"github.com/mydevmachine/devmachine/internal/credentials"
	"github.com/mydevmachine/devmachine/internal/doctor"
	"github.com/mydevmachine/devmachine/internal/hostkeys"
	"github.com/mydevmachine/devmachine/internal/packages"
	"github.com/mydevmachine/devmachine/internal/remote"
	"github.com/mydevmachine/devmachine/internal/secrets"
	"golang.org/x/crypto/ssh"
)

// recordingRemote answers like fakeRemote and keeps every command it was
// given, so a test can see what would have been written to the machine.
type recordingRemote struct {
	out string
	err error
	ran []string
	// delivered is what was written into a file on the machine, one entry per
	// value. A probe that only asks questions never adds to it.
	delivered []string
}

func (r *recordingRemote) Run(_ context.Context, command string) (string, error) {
	r.ran = append(r.ran, command)
	if command == remote.UnameCommand && r.err == nil {
		return "Linux\n", nil
	}
	return r.out, r.err
}

func (r *recordingRemote) RunInput(_ context.Context, command string, stdin io.Reader) (string, error) {
	r.ran = append(r.ran, command)
	body, _ := io.ReadAll(stdin)
	if isWrite(command) {
		r.delivered = append(r.delivered, string(body))
	}
	return r.out, r.err
}

func (r *recordingRemote) Stream(_ context.Context, command string, stdout, _ io.Writer) error {
	r.ran = append(r.ran, command)
	_, err := io.WriteString(stdout, r.out)
	if err != nil {
		return err
	}
	return r.err
}

func (r *recordingRemote) Upload(context.Context, string, io.Reader) error { return nil }

func (r *recordingRemote) Close() error { return nil }

func writeCredentialPackage(t *testing.T, configDir, name, scope, declared string) {
	t.Helper()
	dir := filepath.Join(packages.LocalDir(configDir), name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf("format: 1\nname: %s\nscope: %s\nsummary: A package written by a test.\ncredentials:\n%s",
		name, scope, declared)
	if err := os.WriteFile(filepath.Join(dir, packages.FileName), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// configWithCredentials is a machine with a login of its own, a workspace with
// a login of its own, and a secret named by the caller.
//
// The secret's name is a parameter because a secret lives in the operating
// system's keychain, which is shared by every test in this package: two tests
// using one name would answer each other's question.
func configWithCredentials(t *testing.T, secret string) string {
	t.Helper()
	dir := configWith(t, fmt.Sprintf(`
machines:
  - name: main
    hosts: [203.0.113.10]
    packages: [gh-login]
workspaces:
  - name: alice
    packages: [claude-code, %s]
`, secret))

	writeCredentialPackage(t, dir, "gh-login", packages.ScopeMachine,
		"  - name: gh\n    kind: manual\n    scope: machine\n"+
			"    command: gh auth login\n    stored_at: ~/.config/gh/hosts.yml\n")
	writeCredentialPackage(t, dir, "claude-code", packages.ScopeWorkspace,
		"  - name: claude\n    kind: manual\n    scope: workspace\n"+
			"    command: claude /login\n    stored_at: ~/.claude/.credentials.json\n")
	writeCredentialPackage(t, dir, secret, packages.ScopeWorkspace,
		fmt.Sprintf("  - name: %s\n    kind: secret\n    scope: workspace\n    env: PROBE_TOKEN\n", secret))
	key := commandHostKey(t)
	store, err := hostkeys.Open(filepath.Join(dir, config.KnownHostsFileName))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Put("main", 22, key); err != nil {
		t.Fatal(err)
	}
	wasScan := scanHostKey
	scanHostKey = func(context.Context, config.Machine) (ssh.PublicKey, string, error) {
		return key, "203.0.113.10", nil
	}
	t.Cleanup(func() { scanHostKey = wasScan })
	return dir
}

// answering makes the machine say which credentials are already there.
func answering(t *testing.T, lines ...string) *recordingRemote {
	t.Helper()
	client := &recordingRemote{out: strings.Join(lines, "\n") + "\n"}
	dialing(t, client)
	return client
}

// storeSecret puts a value in the store the CLI reads, and takes it out again
// afterwards so it does not outlive the test in the operator's keychain.
func storeSecret(t *testing.T, dir, name, value string) {
	t.Helper()
	if err := secrets.Set(dir, name, value); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = secrets.Delete(dir, name) })
}

func TestCredentialsListSaysHowToFixAMissingLogin(t *testing.T) {
	dir := configWithCredentials(t, "probe-login")
	answering(t, "gh\tno", "alice/claude\tno", "alice/probe-login\tno")

	out, err := execute(t, "--config", dir, "credentials", "list")
	if err != nil {
		t.Fatalf("credentials list returned %v", err)
	}
	for _, want := range []string{"devmachine login gh", "devmachine login claude --workspace alice"} {
		if !strings.Contains(out, want) {
			t.Fatalf("the report does not say how to fix it (%q):\n%s", want, out)
		}
	}
}

func TestCredentialsListSaysToStoreAMissingSecretFirst(t *testing.T) {
	dir := configWithCredentials(t, "probe-unstored")
	answering(t, "gh\tyes", "alice/claude\tyes", "alice/probe-unstored\tno")

	out, err := execute(t, "--config", dir, "credentials", "list")
	if err != nil {
		t.Fatalf("credentials list returned %v", err)
	}
	if !strings.Contains(out, "devmachine secrets set probe-unstored") {
		t.Fatalf("a secret nobody stored should say to store it:\n%s", out)
	}
	if !strings.Contains(out, "credentials push") {
		t.Fatalf("it does not say what delivers it:\n%s", out)
	}
}

func TestCredentialsListAsksOnlyForAPushWhenTheSecretIsStored(t *testing.T) {
	dir := configWithCredentials(t, "probe-stored")
	storeSecret(t, dir, "probe-stored", "value")
	answering(t, "gh\tyes", "alice/claude\tyes", "alice/probe-stored\tno")

	out, err := execute(t, "--config", dir, "credentials", "list")
	if err != nil {
		t.Fatalf("credentials list returned %v", err)
	}
	if strings.Contains(out, "secrets set probe-stored") {
		t.Fatalf("it asked for a secret that is already stored:\n%s", out)
	}
	if !strings.Contains(out, "devmachine credentials push") {
		t.Fatalf("it does not say what delivers it:\n%s", out)
	}
}

func TestCredentialsListNeverPrintsAValue(t *testing.T) {
	dir := configWithCredentials(t, "probe-quiet")
	storeSecret(t, dir, "probe-quiet", "s3cret-value")
	answering(t, "gh\tyes", "alice/claude\tyes", "alice/probe-quiet\tno")

	out, err := execute(t, "--config", dir, "credentials", "list")
	if err != nil {
		t.Fatalf("credentials list returned %v", err)
	}
	if strings.Contains(out, "s3cret-value") {
		t.Fatalf("the report carried a value:\n%s", out)
	}
}

func TestCredentialsListSaysWhatIsAlreadyThere(t *testing.T) {
	dir := configWithCredentials(t, "probe-present")
	answering(t, "gh\tyes", "alice/claude\tno", "alice/probe-present\tno")

	out, _ := execute(t, "--config", dir, "credentials", "list")
	line := lineWith(t, out, "gh ")
	if !strings.Contains(line, "present") {
		t.Fatalf("gh is on the machine and the report does not say so: %q", line)
	}
}

func TestCredentialsListAsJSONCarriesTheFixAndNoValue(t *testing.T) {
	dir := configWithCredentials(t, "probe-json")
	storeSecret(t, dir, "probe-json", "s3cret-value")
	answering(t, "gh\tno", "alice/claude\tno", "alice/probe-json\tno")

	out, err := execute(t, "--config", dir, "--format", "json", "credentials", "list")
	if err != nil {
		t.Fatalf("credentials list returned %v", err)
	}
	var got struct {
		Credentials []credentialJSON `json:"credentials"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output was not JSON: %v (%q)", err, out)
	}
	if len(got.Credentials) != 3 {
		t.Fatalf("got %#v", got.Credentials)
	}
	for _, c := range got.Credentials {
		if c.Status != "missing" || c.Fix == "" {
			t.Fatalf("every row should be missing and say what to do: %#v", c)
		}
	}
	if strings.Contains(out, "s3cret-value") {
		t.Fatalf("the JSON carried a value:\n%s", out)
	}
}

func TestCredentialsListSaysWhenNothingDeclaresACredential(t *testing.T) {
	dir := configWith(t, "machines:\n  - name: main\n    hosts: [203.0.113.10]\n")
	answering(t, "")

	out, err := execute(t, "--config", dir, "credentials", "list")
	if err != nil {
		t.Fatalf("credentials list returned %v", err)
	}
	if !strings.Contains(out, "no credentials") {
		t.Fatalf("a machine with nothing declared should say so: %q", out)
	}
}

// lineWith returns the one output line holding want.
func lineWith(t *testing.T, out, want string) string {
	t.Helper()
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, want) {
			return line
		}
	}
	t.Fatalf("no line holds %q:\n%s", want, out)
	return ""
}

var _ remote.Client = (*recordingRemote)(nil)

// wrote reports whether anything was delivered to a path on the machine. The
// write script is the only one that ends by copying standard input into a file.
func (r *recordingRemote) wrote() bool { return len(r.delivered) > 0 }

// isWrite says whether a command delivers a value. The write script is the
// only one that ends by copying standard input into a file — a machine or
// workspace credential's into `$path`, a workspace's own dotenv secret into a
// temporary file it then renames into place.
func isWrite(command string) bool {
	return strings.Contains(command, `cat > "$path"`) || strings.Contains(command, `cat > "$tmp"`)
}

// configWithTwoSecrets is one workspace asking for two secrets and a login, so
// a push has something to deliver, something to skip and something to name.
func configWithTwoSecrets(t *testing.T, first, second string) string {
	t.Helper()
	dir := configWith(t, fmt.Sprintf(`
machines:
  - name: main
    hosts: [203.0.113.10]
    packages: [gh-login]
workspaces:
  - name: alice
    packages: [%s, %s]
`, first, second))

	writeCredentialPackage(t, dir, "gh-login", packages.ScopeMachine,
		"  - name: gh\n    kind: manual\n    scope: machine\n"+
			"    command: gh auth login\n    stored_at: ~/.config/gh/hosts.yml\n")
	for _, name := range []string{first, second} {
		writeCredentialPackage(t, dir, name, packages.ScopeWorkspace,
			fmt.Sprintf("  - name: %s\n    kind: secret\n    scope: workspace\n    env: PROBE_TOKEN\n", name))
	}
	return dir
}

func TestPushDeliversOnlyWhatIsMissing(t *testing.T) {
	dir := configWithTwoSecrets(t, "probe-here", "probe-gone")
	storeSecret(t, dir, "probe-here", "one")
	storeSecret(t, dir, "probe-gone", "two")
	client := answering(t, "gh\tyes", "alice/probe-here\tyes", "alice/probe-gone\tno")

	out, err := execute(t, "--config", dir, "credentials", "push", "--yes")
	if err != nil {
		t.Fatalf("credentials push returned %v", err)
	}
	if len(client.delivered) != 1 {
		t.Fatalf("it delivered %d value(s), want 1: %#v", len(client.delivered), client.ran)
	}
	if !strings.Contains(out, "alice/probe-gone") {
		t.Fatalf("the report does not name what it delivered:\n%s", out)
	}
}

func TestPushSkipsALoginAndSaysWhy(t *testing.T) {
	dir := configWithTwoSecrets(t, "probe-a", "probe-b")
	storeSecret(t, dir, "probe-a", "one")
	storeSecret(t, dir, "probe-b", "two")
	answering(t, "gh\tno", "alice/probe-a\tno", "alice/probe-b\tno")

	out, err := execute(t, "--config", dir, "credentials", "push", "--yes")
	if err != nil {
		t.Fatalf("credentials push returned %v", err)
	}
	if !strings.Contains(out, "devmachine login gh") {
		t.Fatalf("a login should be skipped with the command that does it:\n%s", out)
	}
}

func TestPushNamesASecretThatWasNeverStored(t *testing.T) {
	dir := configWithTwoSecrets(t, "probe-kept", "probe-never")
	storeSecret(t, dir, "probe-kept", "one")
	answering(t, "gh\tyes", "alice/probe-kept\tno", "alice/probe-never\tno")

	out, err := execute(t, "--config", dir, "credentials", "push", "--yes")
	// A push that quietly does nothing because nobody ran `secrets set` is the
	// failure this command exists to prevent.
	if err == nil {
		t.Fatal("it reported success with nothing stored for one credential")
	}
	if !strings.Contains(out+err.Error(), "devmachine secrets set probe-never") {
		t.Fatalf("it does not name the secret nobody stored:\n%s\n%v", out, err)
	}
}

func TestPushWritesNothingUnderCheck(t *testing.T) {
	dir := configWithTwoSecrets(t, "probe-dry", "probe-dry-two")
	storeSecret(t, dir, "probe-dry", "one")
	storeSecret(t, dir, "probe-dry-two", "two")
	client := answering(t, "gh\tyes", "alice/probe-dry\tno", "alice/probe-dry-two\tno")

	out, err := execute(t, "--config", dir, "credentials", "push", "--check")
	if err != nil {
		t.Fatalf("credentials push --check returned %v", err)
	}
	if client.wrote() {
		t.Fatalf("a dry run wrote to the machine: %#v", client.ran)
	}
	if !strings.Contains(out, "alice/probe-dry") {
		t.Fatalf("a dry run should say what it would write:\n%s", out)
	}
}

func TestPushNeverPrintsTheValue(t *testing.T) {
	const value = `a b'c$d`
	dir := configWithTwoSecrets(t, "probe-quiet-one", "probe-quiet-two")
	storeSecret(t, dir, "probe-quiet-one", value)
	storeSecret(t, dir, "probe-quiet-two", value)
	client := answering(t, "gh\tno", "alice/probe-quiet-one\tno", "alice/probe-quiet-two\tno")

	out, errs, err := executeSplit(t, "--config", dir, "--format", "json", "credentials", "push", "--yes")
	if err != nil {
		t.Fatalf("credentials push returned %v", err)
	}
	if strings.Contains(out+errs, value) {
		t.Fatalf("the report carried the value:\n%s%s", out, errs)
	}
	// A command log that records a secret is worse than no log.
	for _, line := range historyLines(t, dir) {
		if strings.Contains(line, value) {
			t.Fatalf("the command log carried the value: %q", line)
		}
	}
	// And the value did reach the machine, so the test above is looking at a
	// push that really happened.
	if len(client.delivered) != 2 {
		t.Fatalf("it delivered %d value(s), want 2", len(client.delivered))
	}
}

func TestPushAsJSONSaysWhatItDidAndWhatItSkipped(t *testing.T) {
	dir := configWithTwoSecrets(t, "probe-json-one", "probe-json-two")
	storeSecret(t, dir, "probe-json-one", "one")
	storeSecret(t, dir, "probe-json-two", "two")
	answering(t, "gh\tno", "alice/probe-json-one\tyes", "alice/probe-json-two\tno")

	out, _, err := executeSplit(t, "--config", dir, "--format", "json", "credentials", "push", "--yes")
	if err != nil {
		t.Fatalf("credentials push returned %v", err)
	}
	var got pushJSON
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output was not JSON: %v (%q)", err, out)
	}
	if len(got.Delivered) != 1 || got.Delivered[0] != "alice/probe-json-two" {
		t.Fatalf("delivered = %#v", got.Delivered)
	}
	if len(got.Skipped) != 2 {
		t.Fatalf("skipped = %#v", got.Skipped)
	}
}

func TestPushAsksBeforeWriting(t *testing.T) {
	dir := configWithTwoSecrets(t, "probe-ask", "probe-ask-two")
	storeSecret(t, dir, "probe-ask", "one")
	storeSecret(t, dir, "probe-ask-two", "two")
	client := answering(t, "gh\tyes", "alice/probe-ask\tno", "alice/probe-ask-two\tno")

	if _, err := executeWithInput(t, "n\n", "--config", dir, "credentials", "push"); err == nil {
		t.Fatal("answering no still delivered")
	}
	if client.wrote() {
		t.Fatalf("it wrote after the answer was no: %#v", client.ran)
	}
}

func TestDoctorReportsEachCredentialAndHowToFixIt(t *testing.T) {
	dir := configWithCredentials(t, "probe-doctor")
	answering(t,
		"ID=ubuntu",
		"gh\tno",
		"alice/claude\tno",
		"alice/probe-doctor\tno",
	)

	out, err := execute(t, "--config", dir, "doctor")
	if err != nil {
		t.Fatalf("a missing credential is a warning, and a warning exits 0: %v\n%s", err, out)
	}
	for _, want := range []string{"credential: gh", "credential: alice/claude", "devmachine login gh"} {
		if !strings.Contains(out, want) {
			t.Fatalf("the report leaves out %q:\n%s", want, out)
		}
	}
	if line := lineWith(t, out, "credential: gh"); !strings.HasPrefix(line, "warn") {
		t.Fatalf("a missing login is not a warning: %q", line)
	}
}

func TestDoctorJSONCallsAMachineMissingOnlyCredentialsOK(t *testing.T) {
	dir := configWithCredentials(t, "probe-doctor-json")
	answering(t,
		"ID=ubuntu",
		"gh\tno",
		"alice/claude\tyes",
		"alice/probe-doctor-json\tyes",
	)

	out, err := execute(t, "--config", dir, "--format", "json", "doctor")
	if err != nil {
		t.Fatalf("doctor returned %v:\n%s", err, out)
	}
	var got struct {
		Checks []doctor.Check `json:"checks"`
		OK     bool           `json:"ok"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output was not JSON: %v (%q)", err, out)
	}
	if !got.OK {
		t.Fatalf("ok = false for a machine that only misses a login:\n%s", out)
	}
	for _, c := range got.Checks {
		if c.Name == "credential: gh" && c.Status != doctor.StatusWarn {
			t.Fatalf("credential: gh = %q, want warn", c.Status)
		}
	}
}

func TestDoctorExitsNonZeroWhenTheMachineIsUnreachable(t *testing.T) {
	dir := configWithTrustedKey(t, "")
	t.Cleanup(swap(&dial, func(context.Context, config.Machine, string) (remote.Client, string, error) {
		return nil, "", errors.New("connection refused")
	}))

	out, err := execute(t, "--config", dir, "doctor")
	if err == nil {
		t.Fatalf("an unreachable machine exited 0:\n%s", out)
	}
	if line := lineWith(t, out, "connection"); !strings.HasPrefix(line, "fail") {
		t.Fatalf("the connection is not a failure: %q", line)
	}
}

func TestDoctorSaysNothingAboutCredentialsOnAMachineWithNoPackages(t *testing.T) {
	dir := configWithTrustedKey(t, "")
	answering(t, "ID=ubuntu")

	out, err := execute(t, "--config", dir, "doctor")
	if err != nil {
		t.Fatalf("doctor returned %v:\n%s", err, out)
	}
	if strings.Contains(out, "credential:") {
		t.Fatalf("a machine with nothing declared reported a credential:\n%s", out)
	}
}

func TestPushReplacesAValueThatChangedAndLeavesAnEqualOneAlone(t *testing.T) {
	dir := configWithTwoSecrets(t, "probe-rotated", "probe-same")
	storeSecret(t, dir, "probe-rotated", "new-token")
	storeSecret(t, dir, "probe-same", "same-token")
	declared := func(name string) credentials.Declared {
		return credentials.Declared{
			Credential: packages.Credential{Name: name, Kind: packages.KindSecret, Env: "PROBE_TOKEN"},
			Workspace:  "alice",
		}
	}
	client := answering(t, "gh\tyes",
		"alice/probe-rotated\tyes\t"+credentials.Digest(declared("probe-rotated"), "old-token"),
		"alice/probe-same\tyes\t"+credentials.Digest(declared("probe-same"), "same-token"))

	out, err := execute(t, "--config", dir, "--format", "json", "credentials", "push", "--yes")
	if err != nil {
		t.Fatalf("credentials push returned %v\n%s", err, out)
	}
	if len(client.delivered) != 1 || !strings.Contains(client.delivered[0], "new-token") {
		t.Fatalf("only the rotated value is written again: %#v", client.delivered)
	}
	if !strings.Contains(out, `"replaced"`) || !strings.Contains(out, "alice/probe-rotated") {
		t.Fatalf("the report must say the value was replaced:\n%s", out)
	}
	if strings.Contains(out, "new-token") || strings.Contains(out, "old-token") {
		t.Fatalf("a value was printed:\n%s", out)
	}
}

func TestPushLeavesAValueAloneWhenTheMachineCannotSayWhatItHolds(t *testing.T) {
	dir := configWithTwoSecrets(t, "probe-old-machine", "probe-other")
	storeSecret(t, dir, "probe-old-machine", "x")
	storeSecret(t, dir, "probe-other", "y")
	client := answering(t, "gh\tyes", "alice/probe-old-machine\tyes", "alice/probe-other\tyes")

	if out, err := execute(t, "--config", dir, "credentials", "push", "--yes"); err != nil {
		t.Fatal(err, out)
	}
	if len(client.delivered) != 0 {
		t.Fatalf("nothing is known to differ, so nothing is written: %#v", client.delivered)
	}
}

func TestPushNeverRewritesAFileAToolKeeps(t *testing.T) {
	dir := configWith(t, "machines:\n  - name: main\n    hosts: [203.0.113.10]\n    packages: [npm-auth]\n")
	writeCredentialPackage(t, dir, "npm-auth", packages.ScopeMachine,
		"  - name: probe-npmrc\n    kind: file\n    scope: machine\n    path: /root/.npmrc\n")
	storeSecret(t, dir, "probe-npmrc", "//registry/:_authToken=stored")
	client := answering(t, "probe-npmrc\tyes\t0000000000000000000000000000000000000000000000000000000000000000")

	out, err := execute(t, "--config", dir, "credentials", "push", "--yes")
	if err != nil {
		t.Fatal(err, out)
	}
	if len(client.delivered) != 0 {
		t.Fatalf("a file the tool renews must stay as it is: %#v", client.delivered)
	}
}
