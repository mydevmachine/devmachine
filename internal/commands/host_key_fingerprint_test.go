package commands

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mydevmachine/devmachine/internal/config"
	"github.com/mydevmachine/devmachine/internal/hostkeys"
	"golang.org/x/crypto/ssh"
)

func ecdsaHostKey(t *testing.T) ssh.PublicKey {
	t.Helper()
	private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ssh.NewPublicKey(&private.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func rsaHostKey(t *testing.T) ssh.PublicKey {
	t.Helper()
	private, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ssh.NewPublicKey(&private.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

// hostAlsoHolds makes the machine hold more host keys than the one
// negotiation shows, as a real server does.
func hostAlsoHolds(t *testing.T, held ...ssh.PublicKey) {
	t.Helper()
	t.Cleanup(swap(&scanHostKeyMatching, func(_ context.Context, _ config.Machine, _, fingerprint string) (ssh.PublicKey, error) {
		for _, key := range held {
			if hostkeys.Fingerprint(key) == fingerprint {
				return key, nil
			}
		}
		return nil, nil
	}))
}

func TestMachinesTrustExpectAcceptsTheFingerprintOfAnyHostKeyType(t *testing.T) {
	for _, other := range []ssh.PublicKey{ecdsaHostKey(t), rsaHostKey(t)} {
		t.Run(other.Type(), func(t *testing.T) {
			dir, _ := trustCommandFixture(t)
			hostAlsoHolds(t, other)

			out, err := execute(t, "--config", dir, "machines", "trust", "main", "--yes", "--expect", hostkeys.Fingerprint(other))
			if err != nil {
				t.Fatalf("got %v (%s)", err, out)
			}
			assertTrustedKey(t, dir, other)
		})
	}
}

func TestMachinesTrustExpectAcceptsTheNegotiatedEd25519Key(t *testing.T) {
	dir, presented := trustCommandFixture(t)
	hostAlsoHolds(t, ecdsaHostKey(t), rsaHostKey(t))

	if out, err := execute(t, "--config", dir, "machines", "trust", "main", "--yes", "--expect", hostkeys.Fingerprint(presented)); err != nil {
		t.Fatalf("got %v (%s)", err, out)
	}
	assertTrustedKey(t, dir, presented)
}

func TestMachinesTrustExpectRefusesAFingerprintNoHostKeyHas(t *testing.T) {
	dir, presented := trustCommandFixture(t)
	hostAlsoHolds(t, ecdsaHostKey(t), rsaHostKey(t))

	_, err := execute(t, "--config", dir, "machines", "trust", "main", "--yes", "--expect", hostkeys.Fingerprint(ecdsaHostKey(t)))
	if err == nil || !strings.Contains(err.Error(), hostkeys.Fingerprint(presented)) {
		t.Fatalf("got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, config.KnownHostsFileName)); !os.IsNotExist(statErr) {
		t.Fatal("a refusal wrote known_hosts")
	}
}

func TestMachinesAddFingerprintAcceptsAnotherHostKeyTypeAndPinsIt(t *testing.T) {
	for _, other := range []ssh.PublicKey{ecdsaHostKey(t), rsaHostKey(t)} {
		t.Run(other.Type(), func(t *testing.T) {
			dir := writeConfigDir(t, oneMachine)
			steps := stubBootstrap(t, bootstrapStubs{keyWorks: true})
			negotiated := steps.hostKey
			t.Cleanup(swap(&scanHostKey, func(_ context.Context, m config.Machine) (ssh.PublicKey, string, error) {
				return negotiated, m.Hosts[0].Address, nil
			}))
			hostAlsoHolds(t, other)
			steps.hostKey = other

			if out, err := executeWithInput(t, "", unattendedAdd(dir, hostkeys.Fingerprint(other))...); err != nil {
				t.Fatalf("got %v (%s)", err, out)
			}
			store, err := hostkeys.Open(filepath.Join(dir, config.KnownHostsFileName))
			if err != nil {
				t.Fatal(err)
			}
			if err := store.Check("sandbox", 22, other); err != nil {
				t.Fatalf("the %s key the fingerprint named is not the one trusted: %v", other.Type(), err)
			}
		})
	}
}

func TestMachinesAddFingerprintRefusesOneNoHostKeyHas(t *testing.T) {
	dir := writeConfigDir(t, oneMachine)
	before := readConfigFile(t, dir)
	stubBootstrap(t, bootstrapStubs{keyWorks: true})
	hostAlsoHolds(t, ecdsaHostKey(t), rsaHostKey(t))

	_, err := executeWithInput(t, "", unattendedAdd(dir, hostkeys.Fingerprint(rsaHostKey(t)))...)
	if err == nil || !strings.Contains(err.Error(), "--fingerprint expects") {
		t.Fatalf("got %v", err)
	}
	if after := readConfigFile(t, dir); after != before {
		t.Fatalf("a refused add changed config.yml:\n%s", after)
	}
}
