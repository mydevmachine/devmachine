package dns

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func newExternalWith(c *recordingClient) *External {
	return NewExternal("hostinger", c, "/opt/devmachine/roles/hostinger/bin/provider",
		"hostinger", []string{"*"})
}

func TestExternalListReadsTheRecordsBack(t *testing.T) {
	c := &recordingClient{out: `{"records": [
		{"name": "www", "type": "A", "value": "198.51.100.10", "ttl": 3600},
		{"name": "@", "type": "A", "value": "198.51.100.10", "ttl": 3600}
	]}`}

	got, err := newExternalWith(c).List(context.Background(), "example.com")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != "www" || got[0].TTL != 3600 {
		t.Fatalf("got %#v", got)
	}
}

func TestExternalSourcesTheCredentialBeforeRunning(t *testing.T) {
	c := &recordingClient{out: `{"zones": []}`}

	if _, err := newExternalWith(c).Zones(context.Background()); err != nil {
		t.Fatal(err)
	}
	command := c.commands[0]
	// The value is already on the machine. Reading it here would mean
	// carrying a secret to the operator's computer and back for no reason.
	if !strings.Contains(command, ". /etc/devmachine/hostinger/env") {
		t.Fatalf("the credential was not sourced: %q", command)
	}
	if !strings.Contains(command, "set -a") {
		t.Fatalf("what the file sets is not exported: %q", command)
	}
	if !strings.Contains(command, "/opt/devmachine/roles/hostinger/bin/provider zones") {
		t.Fatalf("the entrypoint was not run: %q", command)
	}
}

func TestExternalPassesTheZoneAsAnArgument(t *testing.T) {
	c := &recordingClient{out: `{"records": []}`}

	if _, err := newExternalWith(c).List(context.Background(), "example.com"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(c.commands[0], "provider list example.com") {
		t.Fatalf("got %q", c.commands[0])
	}
}

func TestExternalQuotesAZoneThatWouldBreakOutOfTheShell(t *testing.T) {
	c := &recordingClient{out: `{"records": []}`}

	// The zone reaches this from a command line and goes into a shell string.
	// Nothing about that is safe by accident.
	_, _ = newExternalWith(c).List(context.Background(), "example.com; rm -rf /")
	if !strings.Contains(c.commands[0], `'example.com; rm -rf /'`) {
		t.Fatalf("the zone was not quoted: %q", c.commands[0])
	}
}

func TestExternalSendsTheRecordOnStdin(t *testing.T) {
	c := &recordingClient{out: `{}`}

	err := newExternalWith(c).Upsert(context.Background(), "example.com",
		Record{Name: "www", Type: "A", Value: "198.51.100.10", TTL: 3600})
	if err != nil {
		t.Fatal(err)
	}
	// One round trip, and the record travels in the command rather than in a
	// second call: a heredoc is what a shell has instead of stdin here.
	if !strings.Contains(c.commands[0], "198.51.100.10") {
		t.Fatalf("the record did not travel: %q", c.commands[0])
	}
}

func TestExternalPipelineActuallyDeliversTheRecordToTheEntrypoint(t *testing.T) {
	c := &recordingClient{out: `{}`}
	e := newExternalWith(c)
	record := Record{Name: "www", Type: "A", Value: "198.51.100.10", TTL: 3600}
	if err := e.Upsert(context.Background(), "example.com", record); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	envFile := filepath.Join(dir, "env")
	provider := filepath.Join(dir, "provider")
	if err := os.WriteFile(envFile, []byte("PROBE=value\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(provider, []byte("#!/bin/sh\ncat\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	command := strings.ReplaceAll(c.commands[0], "/etc/devmachine/hostinger/env", envFile)
	command = strings.ReplaceAll(command, "/opt/devmachine/roles/hostinger/bin/provider", provider)
	out, err := exec.Command("sh", "-c", command).Output()
	if err != nil {
		t.Fatal(err)
	}
	want := `{"name":"www","type":"A","value":"198.51.100.10","ttl":3600}`
	if string(out) != want {
		t.Fatalf("entrypoint read %q, want %q", out, want)
	}
}

// TestExternalAsksAProviderWhoseCredentialWasNeverPushed: help needs no
// credential, and dash, unlike bash, exits when `.` cannot read its file.
func TestExternalAsksAProviderWhoseCredentialWasNeverPushed(t *testing.T) {
	c := &recordingClient{out: `{"commands": []}`}
	if _, err := newExternalWith(c).Help(context.Background()); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	provider := filepath.Join(dir, "provider")
	if err := os.WriteFile(provider, []byte("#!/bin/sh\nprintf asked\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	command := strings.ReplaceAll(c.commands[0], "/etc/devmachine/hostinger/env", filepath.Join(dir, "missing", "env"))
	command = strings.ReplaceAll(command, "/opt/devmachine/roles/hostinger/bin/provider", provider)
	for _, shell := range []string{"sh", "bash", "dash"} {
		if _, err := exec.LookPath(shell); err != nil {
			continue
		}
		out, err := exec.Command(shell, "-c", command).CombinedOutput()
		if err != nil || string(out) != "asked" {
			t.Fatalf("%s: got %q (%v), want the provider asked", shell, out, err)
		}
	}
}

func TestExternalTurnsAReportedKindIntoASentinel(t *testing.T) {
	c := &recordingClient{
		out: `{"error": {"kind": "unauthenticated", "message": "Authentication error"}}`,
		err: errors.New("Process exited with status 1"),
	}

	_, err := newExternalWith(c).List(context.Background(), "example.com")
	if !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("got %v, want ErrUnauthenticated", err)
	}
	if !strings.Contains(err.Error(), "Authentication error") {
		t.Fatalf("the provider's own message was dropped: %v", err)
	}
}

func TestExternalReportsACrashWithWhatItPrinted(t *testing.T) {
	c := &recordingClient{
		out: "Traceback: something went very wrong",
		err: errors.New("Process exited with status 2"),
	}

	_, err := newExternalWith(c).List(context.Background(), "example.com")
	if err == nil {
		t.Fatal("a crash was reported as success")
	}
	// A provider that dies without the contract's error shape is a bug in
	// that provider, and what it printed is the only clue anybody has.
	if !strings.Contains(err.Error(), "something went very wrong") {
		t.Fatalf("the output was swallowed: %v", err)
	}
	if !strings.Contains(err.Error(), "hostinger") {
		t.Fatalf("the error does not say which provider: %v", err)
	}
}

func TestExternalRefusesAnUnsupportedTypeBeforeConnecting(t *testing.T) {
	c := &recordingClient{}

	err := newExternalWith(c).Upsert(context.Background(), "example.com",
		Record{Name: "@", Type: "MX", Value: "10 mail"})
	if !errors.Is(err, ErrUnsupportedType) {
		t.Fatalf("got %v", err)
	}
	if len(c.commands) != 0 {
		t.Fatalf("it opened a session before refusing: %#v", c.commands)
	}
}

func TestExternalZonesReadsTheListBack(t *testing.T) {
	c := &recordingClient{out: `{"zones": ["example.com", "client.example.net"]}`}

	got, err := newExternalWith(c).Zones(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[1] != "client.example.net" {
		t.Fatalf("got %#v", got)
	}
}

func TestExternalRefusesACommandThePackageDidNotDeclare(t *testing.T) {
	c := &recordingClient{}
	p := NewExternal("hostinger", c, "/opt/x/provider", "hostinger", []string{"zones", "list"})

	err := p.Call(context.Background(), []string{"drop-everything"}, io.Discard, io.Discard)
	if err == nil {
		t.Fatal("an undeclared command ran")
	}
	// The package said what it is for. Anything else does not reach it, and
	// does not cost a connection either.
	if len(c.commands) != 0 {
		t.Fatalf("it connected before checking: %#v", c.commands)
	}
	if !strings.Contains(err.Error(), "zones, list") {
		t.Fatalf("the error does not say what it does accept: %v", err)
	}
}

func TestExternalStarAcceptsAnything(t *testing.T) {
	c := &recordingClient{out: "whatever"}

	var out bytes.Buffer
	if err := newExternalWith(c).Call(context.Background(), []string{"anything-at-all"}, &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "whatever") {
		t.Fatalf("the output did not reach the caller: %q", out.String())
	}
}

func TestExternalCallShowsWhatTheEntrypointSaysOnStderrApart(t *testing.T) {
	c := &recordingClient{out: "{}\n", errOut: "token expires soon\n"}

	var out, errOut bytes.Buffer
	if err := newExternalWith(c).Call(context.Background(), []string{"anything-at-all"}, &out, &errOut); err != nil {
		t.Fatal(err)
	}
	if out.String() != "{}\n" {
		t.Fatalf("stdout = %q", out.String())
	}
	if errOut.String() != "token expires soon\n" {
		t.Fatalf("stderr = %q", errOut.String())
	}
}

func TestExternalHelpReadsWhatThePackageSaysAboutItself(t *testing.T) {
	c := &recordingClient{out: `{"commands": [
		{"name": "zones", "summary": "The zones this token can see."},
		{"name": "list", "summary": "Every record in a zone.", "args": "<zone>"}
	]}`}

	got, err := newExternalWith(c).Help(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[1].Args != "<zone>" {
		t.Fatalf("got %#v", got)
	}
}

func TestExternalNeverPutsTheCredentialValueInACommandArgument(t *testing.T) {
	// The contract sources the credential from a file already on the machine.
	// A provider or a future edit that instead interpolated the value itself
	// would put a secret where `ps` shows it to every account on the machine.
	c := &recordingClient{out: `{"zones": []}`}

	secret := "sk_live_super_secret_token_value"
	e := NewExternal("hostinger", c, "/opt/devmachine/roles/hostinger/bin/provider", secret, []string{"*"})
	if _, err := e.Zones(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(c.commands[0], "/etc/devmachine/"+secret+"/env") {
		t.Fatalf("the credential name is only ever a path, never a value: %q", c.commands[0])
	}
}
