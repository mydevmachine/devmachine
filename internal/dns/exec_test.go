package dns

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"testing"

	"github.com/mydevmachine/devmachine/internal/config"
	"github.com/mydevmachine/devmachine/internal/remote"
)

// shClient stands in for a machine reached over SSH: it records each command
// and hands the stdin of `/bin/sh -s` to a real /bin/sh.
type shClient struct {
	recordingClient
}

func (c *shClient) StreamInput(ctx context.Context, command string, stdin io.Reader, stdout, stderr io.Writer) error {
	c.commands = append(c.commands, command)
	if command != "/bin/sh -s" {
		return fmt.Errorf("ran %q", command)
	}
	sh := exec.CommandContext(ctx, "/bin/sh", "-s")
	sh.Stdin, sh.Stdout, sh.Stderr = stdin, stdout, stderr
	return sh.Run()
}

var hostileArgs = []string{"x; rm -rf ~", "$(id)", "it's", "", `ends in \`, `; touch pwned ; #`, "two\nlines", `\'`, "`id`"}

func wantPrinted(words []string) string {
	var want strings.Builder
	for _, w := range words {
		want.WriteString("[" + w + "]\n")
	}
	return want.String()
}

func TestExecOverSSHSendsOnlyShellAndTheWordsOnItsInput(t *testing.T) {
	client := &shClient{}
	ext := NewExternal("mine", client, "/usr/bin/printf", "", nil)

	var out bytes.Buffer
	if err := ext.Exec(context.Background(), append([]string{"[%s]\n"}, hostileArgs...), &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	if len(client.commands) != 1 || client.commands[0] != "/bin/sh -s" {
		t.Fatalf("ran %q, want exactly /bin/sh -s", client.commands)
	}
	if out.String() != wantPrinted(hostileArgs) {
		t.Fatalf("got %q, want %q", out.String(), wantPrinted(hostileArgs))
	}
}

func TestExecOnYourOwnComputerRunsTheWordsDirectly(t *testing.T) {
	client, _, err := remote.Dial(context.Background(), config.Machine{Name: "here", Self: true}, "")
	if err != nil {
		t.Fatal(err)
	}
	ext := NewExternal("mine", client, "/usr/bin/printf", "", nil)

	var out bytes.Buffer
	if err := ext.Exec(context.Background(), append([]string{"[%s]\n"}, hostileArgs...), &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	if out.String() != wantPrinted(hostileArgs) {
		t.Fatalf("got %q, want %q", out.String(), wantPrinted(hostileArgs))
	}
}

func TestCallOverSSHSendsOnlyShellAndTheWordsOnItsInput(t *testing.T) {
	client := &shClient{}
	ext := NewExternal("mine", client, "/usr/bin/printf", "", []string{"[%s]\n"})

	var out bytes.Buffer
	if err := ext.Call(context.Background(), append([]string{"[%s]\n"}, hostileArgs...), &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	if len(client.commands) != 1 || client.commands[0] != "/bin/sh -s" {
		t.Fatalf("ran %q, want exactly /bin/sh -s", client.commands)
	}
	if out.String() != wantPrinted(hostileArgs) {
		t.Fatalf("got %q, want %q", out.String(), wantPrinted(hostileArgs))
	}
}

func TestCallSourcesTheCredentialOnTheShellsInput(t *testing.T) {
	client := &recordingClient{}
	ext := NewExternal("hostinger", client, "/opt/devmachine/roles/hostinger/bin/provider", "hostinger", []string{"*"})

	if err := ext.Call(context.Background(), []string{"zones"}, io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	if len(client.commands) != 1 || client.commands[0] != "/bin/sh -s" {
		t.Fatalf("ran %q", client.commands)
	}
	if !strings.Contains(client.inputs[0], "set -a") || !strings.HasSuffix(client.inputs[0], "exec /opt/devmachine/roles/hostinger/bin/provider zones\n") {
		t.Fatalf("sent %q", client.inputs[0])
	}
}

func TestCallOnYourOwnComputerRunsTheWordsDirectly(t *testing.T) {
	client, _, err := remote.Dial(context.Background(), config.Machine{Name: "here", Self: true}, "")
	if err != nil {
		t.Fatal(err)
	}
	ext := NewExternal("mine", client, "/usr/bin/printf", "", []string{"*"})

	var out bytes.Buffer
	if err := ext.Call(context.Background(), append([]string{"[%s]\n"}, hostileArgs...), &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	if out.String() != wantPrinted(hostileArgs) {
		t.Fatalf("got %q, want %q", out.String(), wantPrinted(hostileArgs))
	}
}
