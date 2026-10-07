package remote

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/mydevmachine/devmachine/internal/config"
)

// controlPersist is how long a multiplexed master connection lingers after
// the process that opened it exits. It is what lets a poll a few seconds
// later reuse the connection instead of paying for a new handshake.
const controlPersist = "5m"

// Seams, so a test can drive DialMux without a network or a real cache
// directory.
var (
	userCacheDir = os.UserCacheDir
	dialFallback = Dial
)

// DialMux is Dial for a short, repeated call such as `run`: it hands
// commands to the system OpenSSH client with connection multiplexing, so a
// second process a few seconds later reuses the first one's authenticated
// connection instead of paying for a new TCP handshake, key exchange and
// authentication.
//
// It falls back to Dial when the system ssh binary is not on the machine.
func DialMux(ctx context.Context, m config.Machine, user string) (Client, string, error) {
	if m.Self {
		return &localClient{}, SelfAddress, nil
	}
	sshPath, err := lookPath("ssh")
	if err != nil {
		return dialFallback(ctx, m, user)
	}
	addresses, err := Resolve(m)
	if err != nil {
		return nil, "", err
	}
	if user == "" {
		user = m.User
	}
	controlDir, err := controlPathDir()
	if err != nil {
		return nil, "", err
	}

	client := &muxClient{
		sshPath:    sshPath,
		args:       StrictSSHArgs(m),
		controlDir: controlDir,
		port:       m.Port,
		addresses:  addresses,
		user:       user,
		machine:    m.Name,
	}
	return client, addresses[0], nil
}

// controlPathDir is where every ControlMaster socket lives, one directory
// shared by every machine. See controlSocketDir for why it is not always the
// cache directory.
func controlPathDir() (string, error) {
	cache, err := userCacheDir()
	if err != nil {
		return "", fmt.Errorf("finding the cache directory: %w", err)
	}
	dir, err := controlSocketDir(cache, os.Getuid(), unixSocketPathLimit())
	if err != nil {
		return "", err
	}
	if dir == filepath.Join(cache, "devmachine", "cm") {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return "", fmt.Errorf("creating %s: %w", dir, err)
		}
		return dir, nil
	}
	if err := ensurePrivateDir(dir); err != nil {
		return "", err
	}
	return dir, nil
}

func muxArgs(controlPath string) []string {
	return []string{
		"-o", "ControlMaster=auto",
		"-o", "ControlPath=" + controlPath,
		"-o", "ControlPersist=" + controlPersist,
		"-o", "BatchMode=yes",
	}
}

// muxClient runs commands through the system ssh binary, one process per
// command, relying on ControlMaster/ControlPersist to make every process
// after the first reuse an already-authenticated connection.
type muxClient struct {
	sshPath    string
	args       []string
	controlDir string
	port       int
	addresses  []string
	user       string
	machine    string
}

func (c *muxClient) Run(ctx context.Context, command string) (string, error) {
	var stdout bytes.Buffer
	err := c.exec(ctx, command, nil, &stdout, nil)
	return stdout.String(), err
}

func (c *muxClient) RunInput(ctx context.Context, command string, stdin io.Reader) (string, error) {
	var stdout bytes.Buffer
	err := c.exec(ctx, command, stdin, &stdout, nil)
	return stdout.String(), err
}

func (c *muxClient) Stream(ctx context.Context, command string, stdout, stderr io.Writer) error {
	return c.exec(ctx, command, nil, stdout, stderr)
}

func (c *muxClient) Upload(context.Context, string, io.Reader) error {
	return errors.New("uploading a directory needs the programmatic SSH client: the multiplexed client only runs commands")
}

func (c *muxClient) Close() error { return nil }

// exec runs command over ssh, trying each resolved address in turn.
//
// ssh exits 255 when it never reached the machine (bad address, refused
// connection, failed handshake), but also when the remote command itself
// exits 255 or dies by a signal. Only the first may go to the next address,
// so an address is dropped only when nothing came back on stdout and stderr
// holds nothing but ssh's own lines saying it did not connect — unless ssh
// refused the host key: that is the answer, and trying another address would
// hide it. Any other failure is the remote command's own, and is returned the
// same way sshClient.Run returns one: the error wraps it, and stdout still
// holds whatever the command printed before it failed.
func (c *muxClient) exec(ctx context.Context, command string, stdin io.Reader, stdout, stderr io.Writer) error {
	var failures []string
	var sent countingReader
	if stdin != nil {
		sent.r = stdin
	}
	var received countingWriter
	if stdout != nil {
		received.w = stdout
	}
	for _, address := range c.addresses {
		controlPath := controlSocketPath(c.controlDir, c.user, address, c.port)
		argv := append(append(append([]string{}, c.args...), muxArgs(controlPath)...), c.user+"@"+address, command)
		cmd := exec.CommandContext(ctx, c.sshPath, argv...)
		if stdin != nil {
			cmd.Stdin = &sent
		}
		if stdout != nil {
			cmd.Stdout = &received
		}

		var errBuf bytes.Buffer
		if stderr != nil {
			cmd.Stderr = io.MultiWriter(stderr, &errBuf)
		} else {
			cmd.Stderr = &errBuf
		}

		runErr := cmd.Run()
		if runErr == nil {
			return nil
		}
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) && exitErr.ExitCode() == 255 {
			if strings.Contains(errBuf.String(), "Host key verification failed") {
				return fmt.Errorf("machine %q at %s: %w", c.machine, address, ErrHostKeyRejected)
			}
			if strings.Contains(errBuf.String(), "too long for Unix domain socket") {
				return fmt.Errorf("machine %q: the file ssh shares one connection through, %s, makes a path longer than this system allows for a socket (%d bytes), so ssh could not start; this is a devmachine bug, please report it: %s",
					c.machine, controlPath, unixSocketPathLimit(), strings.TrimSpace(errBuf.String()))
			}
			if sent.n > 0 {
				return fmt.Errorf("machine %q at %s: the connection dropped after part of the input was sent, so it is not sent again to another address: %s",
					c.machine, address, strings.TrimSpace(errBuf.String()))
			}
			if received.n > 0 {
				return fmt.Errorf("machine %q at %s: the connection dropped after part of the output arrived, so it is not asked again of another address: %s",
					c.machine, address, strings.TrimSpace(errBuf.String()))
			}
			if connectFailed(errBuf.String()) {
				failures = append(failures, fmt.Sprintf("%s (%s)", address, strings.TrimSpace(errBuf.String())))
				continue
			}
		}
		if reason := strings.TrimSpace(errBuf.String()); reason != "" && stderr == nil {
			return fmt.Errorf("running %q: %w: %s", command, runErr, reason)
		}
		return fmt.Errorf("running %q: %w", command, runErr)
	}
	return fmt.Errorf("machine %q: no address answered: %s", c.machine, strings.Join(failures, "; "))
}

// preSessionLines are the whole lines ssh writes when it never reached a
// session on an address, so the command cannot have run there. Whole lines,
// not words: "Connection reset by peer" also ends a session that ran, and a
// command's own stderr can say "Connection refused".
var preSessionLines = []*regexp.Regexp{
	regexp.MustCompile(`^ssh: connect to host \S+ port \d+: .+$`),
	regexp.MustCompile(`^ssh: Could not resolve hostname \S+: .+$`),
	regexp.MustCompile(`^kex_exchange_identification: .+$`),
	regexp.MustCompile(`^Connection (closed|reset) by \S+ port \d+$`),
	regexp.MustCompile(`^(\S+@\S+: )?Permission denied \([a-z,-]+\)\.$`),
	regexp.MustCompile(`^Received disconnect from \S+ port \d+:\d+: .+$`),
	regexp.MustCompile(`^Disconnected from \S+ port \d+$`),
}

// sshNotice is a line ssh writes before a session that says nothing failed.
var sshNotice = regexp.MustCompile(`^Warning: Permanently added .+$`)

// connectFailed says whether ssh's stderr holds only its own lines from
// before a session, at least one of them a failure. Any other line came from
// the command, or from a session that started, and the command may have run.
func connectFailed(stderr string) bool {
	failed := false
	for _, line := range strings.Split(stderr, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == "" || sshNotice.MatchString(line):
		case slices.ContainsFunc(preSessionLines, func(re *regexp.Regexp) bool { return re.MatchString(line) }):
			failed = true
		default:
			return false
		}
	}
	return failed
}

// countingReader lets exec tell a connection that failed before reading any
// input, which another address can safely retry, from one that already took
// part of it, where a retry would deliver only the rest.
type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

// countingWriter is countingReader's twin for output: a stream that already
// reached its writer cannot be asked of another address without the writer
// holding the first part twice.
type countingWriter struct {
	w io.Writer
	n int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}
