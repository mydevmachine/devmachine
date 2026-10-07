package dns

import (
	"context"
	"io"
)

// recordingClient answers a command with canned output and remembers what it
// was asked to run.
//
// It implements the full remote.Client interface, not just Run: the other
// methods are never exercised by these tests, but a fake that only satisfies
// part of the interface would not compile against the real one.
type recordingClient struct {
	out      string
	errOut   string
	err      error
	commands []string
	inputs   []string
}

func (c *recordingClient) Run(_ context.Context, command string) (string, error) {
	c.commands = append(c.commands, command)
	return c.out, c.err
}

func (c *recordingClient) RunInput(_ context.Context, command string, _ io.Reader) (string, error) {
	c.commands = append(c.commands, command)
	return c.out, c.err
}

func (c *recordingClient) Stream(_ context.Context, command string, stdout, stderr io.Writer) error {
	c.commands = append(c.commands, command)
	if c.out != "" {
		_, _ = io.WriteString(stdout, c.out)
	}
	if c.errOut != "" && stderr != nil {
		_, _ = io.WriteString(stderr, c.errOut)
	}
	return c.err
}

func (c *recordingClient) StreamInput(ctx context.Context, command string, stdin io.Reader, stdout, stderr io.Writer) error {
	body, err := io.ReadAll(stdin)
	if err != nil {
		return err
	}
	c.inputs = append(c.inputs, string(body))
	return c.Stream(ctx, command, stdout, stderr)
}

func (c *recordingClient) Upload(context.Context, string, io.Reader) error { return nil }

func (c *recordingClient) Close() error { return nil }
