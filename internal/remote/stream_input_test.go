package remote

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStreamInputFeedsTheLocalClientsCommand(t *testing.T) {
	var out bytes.Buffer
	err := StreamInput(context.Background(), &localClient{}, "cat", strings.NewReader("hello\n"), &out, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if out.String() != "hello\n" {
		t.Fatalf("got %q", out.String())
	}
}

func TestStreamInputFeedsTheMuxClientsCommand(t *testing.T) {
	fakeCacheDir(t)
	m := muxTestMachine(t)
	fakeSSH := filepath.Join(t.TempDir(), "ssh")
	if err := os.WriteFile(fakeSSH, []byte("#!/bin/sh\ncat\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	origLookPath := lookPath
	lookPath = func(string) (string, error) { return fakeSSH, nil }
	t.Cleanup(func() { lookPath = origLookPath })

	client, _, err := DialMux(context.Background(), m, "")
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := StreamInput(context.Background(), client, "/bin/sh -s", strings.NewReader("exec 'true'\n"), &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	if out.String() != "exec 'true'\n" {
		t.Fatalf("got %q", out.String())
	}
}

type noInputClient struct{ Client }

func TestStreamInputRefusesAClientThatCannotSendInput(t *testing.T) {
	err := StreamInput(context.Background(), noInputClient{}, "/bin/sh -s", strings.NewReader("x"), io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "cannot send a program on its input") {
		t.Fatalf("got %v", err)
	}
}
