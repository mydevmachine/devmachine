package remote

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestHardeningDropInWithoutAccountsIsTheGlobalOne(t *testing.T) {
	if got := HardeningDropIn(nil); got != hardeningDropIn {
		t.Fatalf("got %q", got)
	}
}

func TestHardeningDropInKeepsPasswordsForTheNamedAccountsOnly(t *testing.T) {
	got := HardeningDropIn([]string{"alice", "bob"})
	if !strings.HasPrefix(got, hardeningDropIn) {
		t.Fatalf("the global lines do not come first: %s", got)
	}
	want := "Match User alice,bob\n\tPasswordAuthentication yes\n\tKbdInteractiveAuthentication yes\n"
	if !strings.HasSuffix(got, want) {
		t.Fatalf("got %s", got)
	}
}

func TestValidAccountNames(t *testing.T) {
	for _, name := range []string{"alice", "bob.smith", "_svc", "a-b", "Alice9"} {
		if err := ValidAccountName(name); err != nil {
			t.Errorf("%q: %v", name, err)
		}
	}
	for _, name := range []string{"", "a,b", "a b", "-a", "a\nb", "*", "!root", strings.Repeat("a", 65)} {
		if err := ValidAccountName(name); err == nil {
			t.Errorf("%q was accepted", name)
		}
	}
}

// keepClient is a machine whose sshd reads the drop-in: passwords are off for
// everybody except the accounts in keep.
func keepClient(keep ...string) *recordingClient {
	out := map[string]string{AsRoot(effectiveConfigScript): sshdTWithPasswords("no")}
	for _, user := range []string{"alice", "bob", "root"} {
		answer := "no"
		for _, k := range keep {
			if k == user {
				answer = "yes"
			}
		}
		out[AsRoot(accountConfigScript(user))] = sshdTWithPasswords(answer)
	}
	return &recordingClient{output: out}
}

func TestHardenKeepingWritesTheMatchBlockAndProvesEachAccount(t *testing.T) {
	c := keepClient("alice")
	if err := HardenKeeping(context.Background(), c, debian, []string{"alice"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(c.inputs, ""), "Match User alice") {
		t.Fatalf("the Match block was not written: %v", c.inputs)
	}
	if !strings.Contains(c.transcript(), "sshd -T -C user=alice") {
		t.Fatalf("alice was not asked about: %s", c.transcript())
	}
}

func TestHardenKeepingRestoresWhatWasThereWhenAnAccountStillCannotUseItsPassword(t *testing.T) {
	c := keepClient()
	err := HardenKeeping(context.Background(), c, debian, []string{"alice"})
	if err == nil || !strings.Contains(err.Error(), `"alice"`) {
		t.Fatalf("got %v", err)
	}
	if !strings.Contains(c.transcript(), restoreScript) {
		t.Fatalf("the previous drop-in was not put back: %s", c.transcript())
	}
}

func TestHardenKeepsACopyOfThePreviousDropInOutsideTheIncludedDirectory(t *testing.T) {
	c := hardenClient()
	if err := Harden(context.Background(), c, macOS); err != nil {
		t.Fatal(err)
	}
	// macOS includes sshd_config.d/* — every file, not only *.conf — so a copy
	// left in there would be read as configuration.
	if strings.Contains(previousDropInPath, "sshd_config.d") {
		t.Fatalf("the copy sits where sshd reads it: %s", previousDropInPath)
	}
	if !strings.Contains(c.transcript(), previousDropInPath) {
		t.Fatalf("no copy was taken: %s", c.transcript())
	}
}

func TestPasswordLoginOnRemovesTheDropInValidatesAndReloads(t *testing.T) {
	c := &recordingClient{}
	if err := PasswordLoginOn(context.Background(), c, debian); err != nil {
		t.Fatal(err)
	}
	joined := c.transcript()
	remove := strings.Index(joined, hardeningDropInPath)
	validate := strings.Index(joined, "sshd -t")
	reload := strings.Index(joined, "reload")
	if remove < 0 || validate < remove || reload < validate {
		t.Fatalf("not remove, validate, reload in that order: %s", joined)
	}
}

func TestPasswordLoginOnPutsTheDropInBackWhenSshdRefusesWhatIsLeft(t *testing.T) {
	c := &recordingClient{failOn: "sshd -t"}
	if err := PasswordLoginOn(context.Background(), c, debian); err == nil {
		t.Fatal("it carried on past a config sshd refused")
	}
	if !strings.Contains(c.transcript(), restoreScript) {
		t.Fatalf("the drop-in was not put back: %s", c.transcript())
	}
	if strings.Contains(c.transcript(), "reload") {
		t.Fatalf("it reloaded a config sshd refused: %s", c.transcript())
	}
}

func TestReadPasswordLoginAsksSshdForEveryPersonOnTheMachine(t *testing.T) {
	out := map[string]string{
		AsRoot(effectiveConfigScript):        sshdTWithPasswords("no"),
		AsRoot(accountsScript(macOS)):        "alice\nbob\n",
		AsRoot(dropInPresentScript):          "yes\n",
		AsRoot(accountConfigScript("alice")): sshdTWithPasswords("yes"),
		AsRoot(accountConfigScript("bob")):   sshdTWithPasswords("no"),
	}
	got, err := ReadPasswordLogin(context.Background(), &recordingClient{output: out}, macOS)
	if err != nil {
		t.Fatal(err)
	}
	if got.Default || !got.DropIn {
		t.Fatalf("got %+v", got)
	}
	want := []AccountPasswordLogin{{User: "alice", Allowed: true}, {User: "bob", Allowed: false}}
	if len(got.Accounts) != 2 || got.Accounts[0] != want[0] || got.Accounts[1] != want[1] {
		t.Fatalf("got %+v", got.Accounts)
	}
}

func TestAccountsScriptsListPeopleNotSystemAccounts(t *testing.T) {
	if s := accountsScript(macOS); !strings.Contains(s, "dscl") || !strings.Contains(s, "501") {
		t.Fatalf("macOS: %s", s)
	}
	if s := accountsScript(debian); !strings.Contains(s, "getent passwd") || !strings.Contains(s, "1000") {
		t.Fatalf("Linux: %s", s)
	}
}

// TestAMatchBlockInADropInEndsWithItsFile is the finding the layout rests on:
// a Match at the end of an included file does not swallow the lines of the
// main sshd_config that follow the Include. Subsystem is refused inside a
// Match, so `sshd -t` passing proves the block ended with the file.
func TestAMatchBlockInADropInEndsWithItsFile(t *testing.T) {
	sshd, err := exec.LookPath("sshd")
	if err != nil {
		sshd = "/usr/sbin/sshd"
	}
	if _, err := os.Stat(sshd); err != nil {
		t.Skip("no sshd on this computer")
	}
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "d"), 0o755); err != nil {
		t.Fatal(err)
	}
	hostKey := filepath.Join(dir, "hk")
	if out, err := exec.Command("ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", hostKey).CombinedOutput(); err != nil {
		t.Skipf("no ssh-keygen: %v %s", err, out)
	}
	main := "Include " + filepath.Join(dir, "d", "*") + "\nHostKey " + hostKey +
		"\nSubsystem sftp /usr/libexec/sftp-server\n"
	mainPath := filepath.Join(dir, "sshd_config")
	if err := os.WriteFile(mainPath, []byte(main), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "d", "00-devmachine-hardening.conf"),
		[]byte(HardeningDropIn([]string{"alice"})), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(sshd, "-t", "-f", mainPath).CombinedOutput(); err != nil {
		t.Fatalf("sshd -t refused it: %v %s", err, out)
	}
	for user, want := range map[string]string{"alice": "yes", "bob": "no"} {
		out, err := exec.Command(sshd, "-T", "-f", mainPath, "-C", "user="+user+",host=x,addr=203.0.113.1").CombinedOutput()
		if err != nil {
			t.Fatalf("sshd -T for %s: %v %s", user, err, out)
		}
		if got := passwordAuthentication(string(out)); got != want {
			t.Fatalf("%s: passwordauthentication %q, want %q", user, got, want)
		}
	}
}

// TestPasswordLoginRoundTripOnTheThrowawayMachine keeps a password for one
// account, reads it back from sshd, then turns password login on and back to
// what the machine had.
func TestPasswordLoginRoundTripOnTheThrowawayMachine(t *testing.T) {
	m := testMachine(t)
	ctx := context.Background()
	client, _, err := Dial(ctx, m, "")
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	system, err := DetectSystem(ctx, client)
	if err != nil {
		t.Fatal(err)
	}

	saved, _ := client.Run(ctx, AsRoot("cat "+hardeningDropInPath+" 2>/dev/null || true"))
	t.Cleanup(func() {
		_, _ = client.RunInput(ctx, AsRoot(writeDropInScript), strings.NewReader(saved))
		if saved == "" {
			_, _ = client.Run(ctx, AsRoot("rm -f "+hardeningDropInPath))
		}
		_ = sshdStepsFor(system).reloadSshd(ctx, client)
	})
	if _, err := client.Run(ctx, AsRoot("id alice >/dev/null 2>&1 || useradd -m -s /bin/bash alice")); err != nil {
		t.Fatal(err)
	}

	if err := HardenKeeping(ctx, client, system, []string{"alice"}); err != nil {
		t.Fatal(err)
	}
	state, err := ReadPasswordLogin(ctx, client, system)
	if err != nil {
		t.Fatal(err)
	}
	if state.Default || !state.DropIn {
		t.Fatalf("got %+v", state)
	}
	found := false
	for _, a := range state.Accounts {
		if a.User == "alice" {
			found = a.Allowed
		}
	}
	if !found {
		t.Fatalf("alice cannot use her password: %+v", state.Accounts)
	}

	if err := PasswordLoginOn(ctx, client, system); err != nil {
		t.Fatal(err)
	}
	state, err = ReadPasswordLogin(ctx, client, system)
	if err != nil {
		t.Fatal(err)
	}
	if state.DropIn {
		t.Fatalf("the drop-in is still there: %+v", state)
	}
}
