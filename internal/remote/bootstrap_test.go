package remote

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

// recordingClient keeps what was asked of the machine, so a test can read the
// shell the bootstrap writes instead of needing a machine to run it on.
type recordingClient struct {
	commands []string
	inputs   []string
	// failOn makes any command holding this text fail, which is how the
	// "sshd refused the config" branch is reached.
	failOn string
	output map[string]string
}

func (c *recordingClient) Run(_ context.Context, command string) (string, error) {
	c.commands = append(c.commands, command)
	if c.failOn != "" && strings.Contains(command, c.failOn) {
		return "", fmt.Errorf("running %q: exit status 255", command)
	}
	return c.output[command], nil
}

func (c *recordingClient) RunInput(ctx context.Context, command string, stdin io.Reader) (string, error) {
	body, err := io.ReadAll(stdin)
	if err != nil {
		return "", err
	}
	c.inputs = append(c.inputs, string(body))
	return c.Run(ctx, command)
}

func (c *recordingClient) Stream(ctx context.Context, command string, _, _ io.Writer) error {
	_, err := c.Run(ctx, command)
	return err
}

func (c *recordingClient) Upload(context.Context, string, io.Reader) error { return nil }
func (c *recordingClient) Close() error                                    { return nil }

func (c *recordingClient) transcript() string { return strings.Join(c.commands, "\n") }

// publicKeyLine returns an authorized_keys line for a key nothing else uses.
func publicKeyLine(t *testing.T, comment string) string {
	t.Helper()

	public, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewPublicKey(public)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer))) + " " + comment
}

func TestInstallKeyIsIdempotent(t *testing.T) {
	c := &recordingClient{}
	if err := InstallKey(context.Background(), c, publicKeyLine(t, "test")); err != nil {
		t.Fatal(err)
	}
	joined := c.transcript()
	// Running setup twice must not leave the key twice.
	if !strings.Contains(joined, "grep") {
		t.Fatalf("it appends without looking: %s", joined)
	}
	for _, want := range []string{"0700", "0600", ".ssh"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("it does not set %q: %s", want, joined)
		}
	}
}

func TestInstallKeyNeverPutsTheKeyInAnArgument(t *testing.T) {
	c := &recordingClient{}
	if err := InstallKey(context.Background(), c, "ssh-ed25519 SECRETLOOKING test"); err != nil {
		t.Fatal(err)
	}
	// A public key is not a secret, but the habit is: values go in on stdin.
	// An argument is in `ps` for every account on the machine to read.
	if strings.Contains(c.transcript(), "SECRETLOOKING") {
		t.Fatal("the key reached the command line")
	}
	if len(c.inputs) != 1 || !strings.Contains(c.inputs[0], "SECRETLOOKING") {
		t.Fatalf("the key did not go in on stdin: %#v", c.inputs)
	}
}

func TestInstallKeyRefusesAnEmptyKey(t *testing.T) {
	c := &recordingClient{}
	if err := InstallKey(context.Background(), c, "   \n"); err == nil {
		t.Fatal("an empty key was installed")
	}
	if len(c.commands) != 0 {
		t.Fatalf("it touched the machine anyway: %#v", c.commands)
	}
}

func TestProveKeyOpensANewConnection(t *testing.T) {
	// Reusing the session that installed the key proves nothing: that session
	// was authenticated by a password.
	server := sshServerAccepting(t, "publickey")
	m := machineAt(t, server)

	if err := ProveKey(context.Background(), m, "root", throwawayKey(t)); err != nil {
		t.Fatal(err)
	}
	if server.connectionCount() != 1 {
		t.Fatalf("made %d connections", server.connectionCount())
	}
	if got := server.methodsOffered(); len(got) != 1 || got[0] != "publickey" {
		t.Fatalf("the proof did not use the key alone: %v", got)
	}
}

func TestProveKeySaysWhatToCheckWhenItFails(t *testing.T) {
	server := sshServerRejecting(t)
	m := machineAt(t, server)

	err := ProveKey(context.Background(), m, "root", throwawayKey(t))
	if err == nil {
		t.Fatal("a refused key was reported as proved")
	}
	// This failure is the one that matters: hardening is about to be skipped
	// because of it, and the person needs to know why.
	for _, want := range []string{"authorized_keys", "AuthorizedKeysFile"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the error does not say where to look: %v", err)
		}
	}
	if !errors.Is(err, ErrAuthRefused) {
		t.Fatalf("a refused key is not reported as a refused login: %v", err)
	}
}

// TestInstallKeyAgainstTheThrowawayMachine runs the shell on a real sshd host.
// A script that is idempotent in a string comparison and not on a machine is
// the failure this whole task exists to catch.
func TestInstallKeyAgainstTheThrowawayMachine(t *testing.T) {
	m := testMachine(t)

	client, _, err := Dial(context.Background(), m, "")
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	key := publicKeyLine(t, "devmachine-install-key-test")
	defer client.Run(context.Background(),
		"sed -i '/devmachine-install-key-test/d' \"$HOME/.ssh/authorized_keys\"")

	for range 2 {
		if err := InstallKey(context.Background(), client, key); err != nil {
			t.Fatal(err)
		}
	}

	out, err := client.Run(context.Background(),
		"grep -c devmachine-install-key-test \"$HOME/.ssh/authorized_keys\"")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != "1" {
		t.Fatalf("the key is in authorized_keys %s times", strings.TrimSpace(out))
	}

	modes, err := client.Run(context.Background(),
		"stat -c %a \"$HOME/.ssh\" \"$HOME/.ssh/authorized_keys\"")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Fields(modes)[0] != "700" || strings.Fields(modes)[1] != "600" {
		t.Fatalf("got modes %q", strings.TrimSpace(modes))
	}
}

func TestProveKeyAgainstTheThrowawayMachine(t *testing.T) {
	m := testMachine(t)

	if err := ProveKey(context.Background(), m, m.User, m.Key); err != nil {
		t.Fatal(err)
	}

	absent := filepath.Join(t.TempDir(), "id_ed25519")
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(private, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(absent, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ProveKey(context.Background(), m, m.User, absent); err == nil {
		t.Fatal("a key the machine does not trust was reported as proved")
	}
}

// everything joins the commands and what was fed to them, because Harden puts
// the file's body on stdin rather than in the command.
func (c *recordingClient) everything() string {
	return strings.Join(append(append([]string(nil), c.commands...), c.inputs...), "\n")
}

func TestHardenWritesADropInThatSortsFirst(t *testing.T) {
	c := hardenClient()
	if err := Harden(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	joined := c.everything()
	// sshd uses the FIRST value it finds for each directive, and the Include
	// of sshd_config.d sits at the top. A cloud image ships
	// 60-cloudimg-settings.conf with PasswordAuthentication yes, so a 99-
	// prefix silently does nothing.
	if !strings.Contains(joined, "/etc/ssh/sshd_config.d/00-") {
		t.Fatalf("the drop-in does not sort first: %s", joined)
	}
	if !strings.Contains(joined, "PasswordAuthentication no") {
		t.Fatalf("got %s", joined)
	}
}

// TestHardenSaysWhyTheNameSortsFirst guards the reasoning, not the number. A
// later edit that renames the file has to read why before it can.
func TestHardenSaysWhyTheNameSortsFirst(t *testing.T) {
	for _, want := range []string{"first", "60-cloudimg-settings.conf"} {
		if !strings.Contains(hardeningDropIn, want) {
			t.Fatalf("the drop-in does not carry %q: %s", want, hardeningDropIn)
		}
	}
}

func TestHardenValidatesBeforeReloading(t *testing.T) {
	c := hardenClient()
	if err := Harden(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	joined := c.transcript()
	// A bad sshd_config plus a reload is a machine nobody can reach again.
	sshdT := strings.Index(joined, "sshd -t")
	reload := strings.Index(joined, "reload")
	if sshdT < 0 || reload < 0 || sshdT > reload {
		t.Fatalf("it reloads without validating: %s", joined)
	}
}

func TestHardenLeavesPasswordsOnWhenValidationFails(t *testing.T) {
	c := &recordingClient{failOn: "sshd -t"}
	err := Harden(context.Background(), c)
	if err == nil {
		t.Fatal("it carried on past a bad config")
	}
	if strings.Contains(c.transcript(), "reload") {
		t.Fatal("it reloaded a config sshd refused")
	}
}

// TestHardenTakesBackAConfigSshdRefused: a file the daemon will not accept,
// left on disk, breaks the next reload by anything at all, including a reboot.
func TestHardenTakesBackAConfigSshdRefused(t *testing.T) {
	c := &recordingClient{failOn: "sshd -t"}
	_ = Harden(context.Background(), c)

	last := c.commands[len(c.commands)-1]
	if !strings.Contains(last, "rm ") || !strings.Contains(last, hardeningDropInPath) {
		t.Fatalf("the refused drop-in was left behind: %s", c.transcript())
	}
}

// TestHardeningDropInIsValidToARealSshd writes the file on the throwaway
// machine, asks sshd to parse it and takes it away again. It never reloads:
// what is being proved is the content, not the restart.
func TestHardeningDropInIsValidToARealSshd(t *testing.T) {
	m := testMachine(t)

	client, _, err := Dial(context.Background(), m, "")
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	probe := "/etc/ssh/sshd_config.d/00-devmachine-probe.conf"
	defer client.Run(context.Background(), AsRoot("rm -f "+probe))

	if _, err := client.RunInput(context.Background(), AsRoot("cat > "+probe), strings.NewReader(hardeningDropIn)); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Run(context.Background(), AsRoot(validateScript)); err != nil {
		t.Fatalf("a real sshd refused the drop-in: %v", err)
	}
}

// TestProveAuthHandsBackTheConnectionItProved: what follows a proof — turning
// password login off included — has to run on the connection that was proved,
// not on the password session that is about to stop working.
func TestProveAuthHandsBackTheConnectionItProved(t *testing.T) {
	server := sshServerAccepting(t, "publickey")
	m := machineAt(t, server)

	client, err := ProveAuth(context.Background(), m, "root", Auth{KeyPath: throwawayKey(t)})
	if err != nil {
		t.Fatal(err)
	}
	if client == nil {
		t.Fatal("the proof returned no connection")
	}
	if err := client.Close(); err != nil {
		t.Fatalf("the proved connection was not open: %v", err)
	}
	if server.connectionCount() != 1 {
		t.Fatalf("made %d connections", server.connectionCount())
	}
}

// clientWithOsRelease answers the questions InstallAnsible asks before it
// decides anything: which kernel, then which distribution.
func clientWithOsRelease(body string) *recordingClient {
	return &recordingClient{output: map[string]string{
		UnameCommand:                  "Linux\n",
		OSReleaseCommand:              body,
		AsRoot(effectiveConfigScript): sshdTWithPasswords("no"),
	}}
}

// sshdTWithPasswords is what `sshd -T` prints, cut down to a few lines.
func sshdTWithPasswords(answer string) string {
	return "port 22\npermitrootlogin prohibit-password\npasswordauthentication " + answer +
		"\nkbdinteractiveauthentication no\n"
}

// hardenClient is a machine whose sshd reads the drop-in.
func hardenClient() *recordingClient {
	return &recordingClient{output: map[string]string{AsRoot(effectiveConfigScript): sshdTWithPasswords("no")}}
}

// TestHardenProvesPasswordLoginIsOff: a drop-in sshd never reads would let
// "password login is off" be said of a machine that still takes passwords.
func TestHardenProvesPasswordLoginIsOff(t *testing.T) {
	c := hardenClient()
	if err := Harden(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	joined := c.transcript()
	reload := strings.Index(joined, "reload")
	proof := strings.Index(joined, "sshd -T")
	if reload < 0 || proof < 0 || proof < reload {
		t.Fatalf("it does not ask sshd after the reload: %s", joined)
	}
	if !strings.Contains(c.commands[len(c.commands)-1], "sshd -T") {
		t.Fatalf("something ran after the proof: %s", joined)
	}
	if !strings.Contains(joined, "/usr/sbin:/sbin") {
		t.Fatalf("sshd -T runs without sbin on PATH: %s", joined)
	}
}

func TestHardenTakesTheDropInBackWhenSshdStillAllowsPasswords(t *testing.T) {
	c := &recordingClient{output: map[string]string{AsRoot(effectiveConfigScript): sshdTWithPasswords("yes")}}
	err := Harden(context.Background(), c)
	want := "the drop-in was written but sshd still allows passwords: " + hardeningDropInPath +
		" is not read by this sshd"
	if err == nil || err.Error() != want {
		t.Fatalf("got %v, want %q", err, want)
	}
	joined := c.transcript()
	proof := strings.Index(joined, "sshd -T")
	removed := strings.LastIndex(joined, "rm -f "+hardeningDropInPath)
	if removed < proof {
		t.Fatalf("the drop-in was left behind: %s", joined)
	}
	if !strings.Contains(joined[removed:], "reload") {
		t.Fatalf("sshd was not reloaded without it: %s", joined)
	}
}

// TestHardenReadsSshdTInEitherCase: OpenSSH 10 on Arch prints
// "PasswordAuthentication no"; older versions print it in lower case.
func TestHardenReadsSshdTInEitherCase(t *testing.T) {
	for _, answer := range []string{"PasswordAuthentication no\n", "passwordauthentication no\n"} {
		c := &recordingClient{output: map[string]string{AsRoot(effectiveConfigScript): "Port 22\n" + answer}}
		if err := Harden(context.Background(), c); err != nil {
			t.Fatalf("%q: %v", answer, err)
		}
	}
	c := &recordingClient{output: map[string]string{AsRoot(effectiveConfigScript): "Port 22\nPasswordAuthentication yes\n"}}
	if err := Harden(context.Background(), c); err == nil {
		t.Fatal("CamelCase yes was read as no")
	}
}

func TestHardenSaysSoWhenSshdCannotPrintItsConfiguration(t *testing.T) {
	c := &recordingClient{failOn: "sshd -T"}
	err := Harden(context.Background(), c)
	if err == nil || !strings.Contains(err.Error(), "sshd -T") {
		t.Fatalf("got %v", err)
	}
}

func TestInstallAnsiblePicksThePackageManagerFromOsRelease(t *testing.T) {
	for _, c := range []struct{ id, want string }{
		{"ubuntu", "apt-get"},
		{"debian", "apt-get"},
		{"arch", "pacman -S --noconfirm --needed ansible"},
		{"archarm", "pacman -S --noconfirm --needed ansible"},
	} {
		client := clientWithOsRelease("ID=" + c.id + "\n")
		if err := InstallAnsible(context.Background(), client, io.Discard); err != nil {
			t.Fatalf("%s: %v", c.id, err)
		}
		if !strings.Contains(client.transcript(), c.want) {
			t.Fatalf("%s: got %s", c.id, client.transcript())
		}
	}
}

func TestInstallAnsibleInstallsTheFullPackageNotTheCore(t *testing.T) {
	// ansible-core alone has no community.general, and the firewall package
	// needs it. This was found on the throwaway VM, not reasoned about.
	c := clientWithOsRelease("ID=ubuntu\n")
	if err := InstallAnsible(context.Background(), c, io.Discard); err != nil {
		t.Fatal(err)
	}
	joined := c.transcript()
	if strings.Contains(joined, "ansible-core") || !strings.Contains(joined, " ansible") {
		t.Fatalf("got %s", joined)
	}
}

func TestInstallAnsibleSaysSoOnADistributionItDoesNotKnow(t *testing.T) {
	// The table claims what has been run on a real machine and nothing more.
	// Guessing a package manager gets somebody halfway through a first run and
	// then leaves them there.
	for _, id := range []string{"plan9", "fedora", "alpine"} {
		c := clientWithOsRelease("ID=" + id + "\n")
		err := InstallAnsible(context.Background(), c, io.Discard)
		if err == nil {
			t.Fatalf("%s: it guessed a package manager", id)
		}
		if !strings.Contains(err.Error(), id) {
			t.Fatalf("the error does not name it: %v", err)
		}
		if len(c.commands) != 2 {
			t.Fatalf("%s: it ran something anyway: %#v", id, c.commands)
		}
	}
}

func TestInstallAnsibleLeavesAMachineThatAlreadyHasItAlone(t *testing.T) {
	// setup is run again on a machine it already owns more often than on a new
	// one, and apt-get on every run is minutes nobody asked for.
	c := clientWithOsRelease("ID=debian\n")
	if err := InstallAnsible(context.Background(), c, io.Discard); err != nil {
		t.Fatal(err)
	}
	joined := c.transcript()
	look := strings.Index(joined, "command -v ansible-playbook")
	install := strings.Index(joined, "apt-get")
	if look < 0 || look > install {
		t.Fatalf("it installs without looking first: %s", joined)
	}
}

func TestInstallAnsibleSaysWhenItCannotTellWhatTheMachineIs(t *testing.T) {
	c := clientWithOsRelease("ID=debian\n")
	c.failOn = "os-release"
	err := InstallAnsible(context.Background(), c, io.Discard)
	if err == nil {
		t.Fatal("it carried on without knowing the distribution")
	}
	if !strings.Contains(err.Error(), "os-release") {
		t.Fatalf("the error does not say what failed: %v", err)
	}
}

// TestInstallAnsibleAgainstTheThrowawayMachine is the only proof that matters:
// the package exists, the name is right, and running it twice is free.
func TestInstallAnsibleAgainstTheThrowawayMachine(t *testing.T) {
	m := testMachine(t)

	client, _, err := Dial(context.Background(), m, "")
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	for range 2 {
		if err := InstallAnsible(context.Background(), client, io.Discard); err != nil {
			t.Fatal(err)
		}
	}

	out, err := client.Run(context.Background(), "ansible-playbook --version")
	if err != nil {
		t.Fatalf("ansible-playbook is not there after installing it: %v", err)
	}
	if !strings.Contains(out, "ansible-playbook") {
		t.Fatalf("got %q", out)
	}
	// ansible-core alone would install the binary and leave the collection
	// out, which only shows up much later, inside a play.
	if _, err := client.Run(context.Background(),
		"ansible-doc -l community.general 2>/dev/null | head -1"); err != nil {
		t.Fatalf("community.general is missing: %v", err)
	}
}

// TestAsRootRunsDirectlyForRootAndThroughSudoOtherwise runs the wrapper in a
// real shell with a fake id, sudo and bash on PATH, so what is proved is the
// branch the shell takes, not the text of the command.
func TestAsRootRunsDirectlyForRootAndThroughSudoOtherwise(t *testing.T) {
	for _, uid := range []string{"0", "1000"} {
		bin := t.TempDir()
		writeScript(t, filepath.Join(bin, "id"), "#!/bin/sh\necho "+uid+"\n")
		writeScript(t, filepath.Join(bin, "sudo"), "#!/bin/sh\nprintf 'sudo:%s %s %s|' \"$1\" \"$2\" \"$3\"\nshift 2\nexec \"$@\"\n")
		writeScript(t, filepath.Join(bin, "bash"), "#!/bin/sh\nprintf 'bash|'\nexec /bin/sh \"$@\"\n")

		want := "bash|direct:it's here"
		if uid != "0" {
			want = "sudo:-n -H " + filepath.Join(bin, "bash") + "|" + want
		}
		got, err := runShell(t, bin, AsRoot(`printf "direct:%s" "it's here"`))
		if err != nil {
			t.Fatalf("uid %s: %v", uid, err)
		}
		if got != want {
			t.Fatalf("uid %s: got %q, want %q", uid, got, want)
		}
	}
}

// TestAsRootFallsBackToShWithoutBash: a minimal image may have no bash, and
// root must still be reached there.
func TestAsRootFallsBackToShWithoutBash(t *testing.T) {
	for _, c := range []struct {
		uid  string
		want string
	}{
		{"0", "direct:it's here"},
		{"1000", "sudo:-n -H sh|direct:it's here"},
	} {
		bin := t.TempDir()
		writeScript(t, filepath.Join(bin, "id"), "#!/bin/sh\necho "+c.uid+"\n")
		writeScript(t, filepath.Join(bin, "sudo"), "#!/bin/sh\nprintf 'sudo:%s %s %s|' \"$1\" \"$2\" \"$3\"\nshift 2\nexec \"$@\"\n")
		if err := os.Symlink("/bin/sh", filepath.Join(bin, "sh")); err != nil {
			t.Fatal(err)
		}

		cmd := exec.Command("/bin/sh", "-c", AsRoot(`printf "direct:%s" "it's here"`))
		cmd.Env = append(os.Environ(), "PATH="+bin)
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("uid %s: %v", c.uid, err)
		}
		if string(out) != c.want {
			t.Fatalf("uid %s: got %q, want %q", c.uid, out, c.want)
		}
	}
}

// TestAsRootKeepsBashLeniencyForRoot: before the wrapper, root's login shell
// ran the command, and bash goes on past a `.` of a missing file.
func TestAsRootKeepsBashLeniencyForRoot(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("no bash on this computer")
	}
	bin := t.TempDir()
	writeScript(t, filepath.Join(bin, "id"), "#!/bin/sh\necho 0\n")

	got, err := runShell(t, bin, AsRoot(". "+filepath.Join(bin, "missing")+" 2>/dev/null; printf ran"))
	if err != nil || got != "ran" {
		t.Fatalf("got %q (%v), want ran", got, err)
	}
}

// TestAsRootKeepsStdinForTheScript: the hardening drop-in arrives on stdin,
// so the wrapper must not swallow it.
func TestAsRootKeepsStdinForTheScript(t *testing.T) {
	bin := t.TempDir()
	writeScript(t, filepath.Join(bin, "id"), "#!/bin/sh\necho 1000\n")
	writeScript(t, filepath.Join(bin, "sudo"), "#!/bin/sh\nshift 2\nexec \"$@\"\n")

	cmd := exec.Command("/bin/sh", "-c", AsRoot("cat"))
	cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"))
	cmd.Stdin = strings.NewReader("PasswordAuthentication no\n")
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "PasswordAuthentication no\n" {
		t.Fatalf("got %q", out)
	}
}

func TestCheckRootAcceptsRootOrPasswordlessSudo(t *testing.T) {
	c := &recordingClient{}
	if err := CheckRoot(context.Background(), c, "alice"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(c.transcript(), "sudo -n true") {
		t.Fatalf("it never asked sudo: %s", c.transcript())
	}
}

// TestCheckRootSaysHowToFixIt: a sudo that wants a password is the one thing
// an admin who is not root can meet, and the fix is one line.
func TestCheckRootSaysHowToFixIt(t *testing.T) {
	c := &recordingClient{failOn: "sudo -n true"}
	err := CheckRoot(context.Background(), c, "alice")
	if !errors.Is(err, ErrNoRoot) {
		t.Fatalf("got %v", err)
	}
	for _, want := range []string{`"alice"`, "alice ALL=(ALL) NOPASSWD:ALL", "/etc/sudoers.d/"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the error does not carry %q: %v", want, err)
		}
	}
}

// TestBootstrapRunsAsRootForAnAdminWhoIsNot: every step that touches the
// system goes through AsRoot, so an admin with sudo gets the same machine as
// root does. Found on a real machine whose admin was not root.
func TestBootstrapRunsAsRootForAnAdminWhoIsNot(t *testing.T) {
	c := clientWithOsRelease("ID=ubuntu\n")
	if err := Harden(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	if err := InstallAnsible(context.Background(), c, io.Discard); err != nil {
		t.Fatal(err)
	}
	for _, command := range c.commands {
		if command == OSReleaseCommand || command == UnameCommand {
			continue
		}
		if !strings.Contains(command, `sudo -n -H "$devmachine_sh" -c`) {
			t.Fatalf("it runs without root: %s", command)
		}
	}
}

func writeScript(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

func runShell(t *testing.T, bin, command string) (string, error) {
	t.Helper()
	cmd := exec.Command("/bin/sh", "-c", command)
	cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"))
	out, err := cmd.Output()
	return string(out), err
}

// TestElevatedRunsEveryCommandAsRoot, and only once however often it is
// wrapped.
func TestElevatedRunsEveryCommandAsRoot(t *testing.T) {
	c := &recordingClient{}
	e := Elevated(Elevated(c))
	if _, err := e.Run(context.Background(), "id"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.RunInput(context.Background(), "cat", strings.NewReader("x")); err != nil {
		t.Fatal(err)
	}
	if err := e.Stream(context.Background(), "ls", io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	want := []string{AsRoot("id"), AsRoot("cat"), AsRoot("ls")}
	if !slices.Equal(c.commands, want) {
		t.Fatalf("got %q", c.commands)
	}
}

// TestCheckRootSuggestsAFileSudoReads: sudo skips a sudoers.d file with a dot
// in its name, so the suggested fix would do nothing for "first.last".
func TestCheckRootSuggestsAFileSudoReads(t *testing.T) {
	err := CheckRoot(context.Background(), &recordingClient{failOn: "sudo -n true"}, "first.last")
	if !strings.Contains(err.Error(), "/etc/sudoers.d/devmachine-first_last") {
		t.Fatalf("got %v", err)
	}
}

// TestHardenMakesSshdsRuntimeDirectoryBeforeValidating: a socket-activated
// sshd that never started has no /run/sshd, and sshd -t refuses without it.
// Found on a real machine reached only through Tailscale SSH.
func TestHardenMakesSshdsRuntimeDirectoryBeforeValidating(t *testing.T) {
	c := hardenClient()
	if err := Harden(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	joined := c.transcript()
	dir := strings.Index(joined, "install -d -m 0755 /run/sshd")
	check := strings.Index(joined, "sshd -t")
	if dir < 0 || dir > check {
		t.Fatalf("sshd -t runs without its runtime directory: %s", joined)
	}
}

// TestInstallAnsibleOnArchNeverUpgradesTheSystem: -Sy without -u is a partial
// upgrade, and -Syu upgrades everything as a side effect of setup.
func TestInstallAnsibleOnArchNeverUpgradesTheSystem(t *testing.T) {
	c := clientWithOsRelease("ID=arch\n")
	if err := InstallAnsible(context.Background(), c, io.Discard); err != nil {
		t.Fatal(err)
	}
	joined := c.transcript()
	for _, line := range strings.Split(joined, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "echo") || !strings.Contains(line, "pacman ") {
			continue
		}
		if !strings.Contains(line, "pacman -S --noconfirm --needed ansible;") {
			t.Fatalf("it runs pacman another way: %s", line)
		}
	}
	if strings.Contains(joined, "ansible-core") {
		t.Fatalf("it installs ansible-core: %s", joined)
	}
	look := strings.Index(joined, "command -v ansible-playbook")
	install := strings.Index(joined, "pacman -S ")
	if look < 0 || look > install {
		t.Fatalf("it installs without looking first: %s", joined)
	}
}

func TestInstallAnsibleRefusesASystemThatIsNotLinux(t *testing.T) {
	c := &recordingClient{output: map[string]string{UnameCommand: "FreeBSD\n"}}
	err := InstallAnsible(context.Background(), c, io.Discard)
	if err == nil || !strings.Contains(err.Error(), `"FreeBSD" is not a system this CLI sets up`) {
		t.Fatalf("got %v", err)
	}
	if len(c.commands) != 1 {
		t.Fatalf("it ran something anyway: %#v", c.commands)
	}
}

// TestInstallAnsibleLeavesAMacToItsBootstrap: a Mac gets Ansible from its
// package manager's package, never from a table in the CLI.
func TestInstallAnsibleLeavesAMacToItsBootstrap(t *testing.T) {
	c := &recordingClient{output: map[string]string{UnameCommand: "Darwin\n", MacVersionCommand: "15.7.9\n"}}
	err := InstallAnsible(context.Background(), c, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "bootstrap") {
		t.Fatalf("got %v", err)
	}
	if len(c.commands) != 2 {
		t.Fatalf("it ran something past detection: %#v", c.commands)
	}
}

// TestSshdTOnARealMachineNamesPasswordAuthentication: the proof reads one line
// of `sshd -T`, so that line has to be there, spelled as parsed.
func TestSshdTOnARealMachineNamesPasswordAuthentication(t *testing.T) {
	m := testMachine(t)

	client, _, err := Dial(context.Background(), m, "")
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	out, err := client.Run(context.Background(), AsRoot(effectiveConfigScript))
	if err != nil {
		t.Fatalf("sshd -T failed on a real machine: %v", err)
	}
	if !strings.Contains(strings.ToLower(out), "\npasswordauthentication ") {
		t.Fatalf("sshd -T does not print passwordauthentication:\n%s", out)
	}
}
