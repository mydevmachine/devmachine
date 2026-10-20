package remote

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// ErrLoginWaiting means the machine answered and accepted the key, and the
// SSH agent has not signed with it yet: 1Password waits there for Touch ID or
// to be unlocked, as it does after a wake.
var ErrLoginWaiting = errors.New("the SSH agent has not approved the key yet")

const (
	// loginWatch is how long Login watches a new login before it answers.
	// A machine that does not answer fails sooner, on ConnectTimeout.
	loginWatch = 10 * time.Second
	// loginLife is how long a login may wait for the agent. sshd drops an
	// unfinished login on its own after LoginGraceTime, two minutes by
	// default; this ends one a machine kept longer.
	loginLife = 3 * time.Minute
)

// acceptedKey is the line ssh writes, at LogLevel DEBUG1 and no lower, when
// the machine accepts the key and ssh asks the agent to sign with it. A login
// still running after this line waits for the agent and nothing else.
const acceptedKey = "debug1: Server accepts key:"

// Login makes sure the shared connection to the machine is up before the
// commands that use it, so the machine sees one login, not one per command.
//
// A login the agent has not approved is left running on its own, and Login
// returns ErrLoginWaiting at once instead of waiting for someone to approve
// it. Killing it would close the agent's prompt and count on the machine as a
// failed login, and the next call would knock again. While it waits, every
// later Login answers ErrLoginWaiting without touching the network. Once it is
// approved the connection is up, and the next Login finds it.
func (c *muxClient) Login(ctx context.Context) error {
	for _, address := range c.addresses {
		if c.masterUp(ctx, address) {
			return nil
		}
	}
	for _, address := range c.addresses {
		waiting, err := c.pendingLogin(address)
		if waiting {
			return c.waitingError(address)
		}
		if err != nil {
			return err
		}
	}

	var (
		failures []string
		authErr  error
	)
	for _, address := range c.addresses {
		stderr, err := c.startLogin(ctx, address)
		if err != nil {
			return err
		}
		if stderr == "" {
			return nil
		}
		if strings.Contains(stderr, "Host key verification failed") {
			return fmt.Errorf("machine %q at %s: %w", c.machine, address, ErrHostKeyRejected)
		}
		if authErr == nil && loginRefused(stderr) {
			authErr = fmt.Errorf("%w: machine %q answered on %s but refused the login for %q: %s",
				ErrAuthRefused, c.machine, address, c.user, stderr)
		}
		failures = append(failures, fmt.Sprintf("%s (%s)", address, stderr))
	}
	if authErr != nil {
		return authErr
	}
	return fmt.Errorf("machine %q: no address answered: %s", c.machine, strings.Join(failures, "; "))
}

func (c *muxClient) waitingError(address string) error {
	return fmt.Errorf("%w: machine %q answered on %s and accepted the key for %q; approve it in the SSH agent (1Password asks for Touch ID or to be unlocked)",
		ErrLoginWaiting, c.machine, address, c.user)
}

func (c *muxClient) controlPath(address string) string {
	return controlSocketPath(c.controlDir, c.user, address, c.port)
}

// masterUp asks the local ssh whether a shared connection is open. It never
// touches the network.
func (c *muxClient) masterUp(ctx context.Context, address string) bool {
	argv := append(append([]string{}, c.args...),
		"-o", "ControlPath="+c.controlPath(address), "-O", "check", c.user+"@"+address)
	return exec.CommandContext(ctx, c.sshPath, argv...).Run() == nil
}

// pendingLogin says whether a login started earlier still waits for the
// agent. One that ended is cleared, and a refusal in it is returned once, so
// a refused prompt is answered as a refused login and not asked again.
func (c *muxClient) pendingLogin(address string) (bool, error) {
	state := loginStatePath(c.controlPath(address))
	pid, started, err := readPendingLogin(state)
	if err != nil {
		return false, nil
	}
	if syscall.Kill(pid, 0) == nil {
		if time.Since(started) < loginLife {
			return true, nil
		}
		_ = syscall.Kill(pid, syscall.SIGTERM)
		clearLoginState(state)
		return false, nil
	}
	log, _ := os.ReadFile(loginLogPath(state))
	clearLoginState(state)
	stderr := sshOwnLines(string(log))
	if loginRefused(stderr) {
		return false, fmt.Errorf("%w: machine %q answered on %s but refused the login for %q: %s",
			ErrAuthRefused, c.machine, address, c.user, stderr)
	}
	return false, nil
}

// startLogin opens the shared connection on address. It returns "" once the
// connection is up, ssh's own lines when it failed, and ErrLoginWaiting when
// the agent has the key to sign and the login is left running.
func (c *muxClient) startLogin(ctx context.Context, address string) (string, error) {
	controlPath := c.controlPath(address)
	state := loginStatePath(controlPath)
	logFile, err := os.OpenFile(loginLogPath(state), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return "", fmt.Errorf("opening the login log: %w", err)
	}
	defer logFile.Close()

	argv := append(append(append([]string{}, c.args...), muxArgs(controlPath)...),
		"-o", "LogLevel=DEBUG1", c.user+"@"+address, "true")
	// Not CommandContext, and in its own session: a login the agent has not
	// approved has to outlive this process.
	cmd := exec.Command(c.sshPath, argv...)
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("starting ssh: %w", err)
	}
	if err := writePendingLogin(state, cmd.Process.Pid, time.Now()); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return "", err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	watch := time.NewTimer(loginWatch)
	defer watch.Stop()
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case runErr := <-done:
			log, _ := os.ReadFile(loginLogPath(state))
			clearLoginState(state)
			if runErr == nil {
				return "", nil
			}
			if stderr := sshOwnLines(string(log)); stderr != "" {
				return stderr, nil
			}
			return runErr.Error(), nil
		case <-tick.C:
			log, _ := os.ReadFile(loginLogPath(state))
			if strings.Contains(string(log), acceptedKey) {
				return "", c.waitingError(address)
			}
		case <-watch.C:
			_ = cmd.Process.Kill()
			<-done
			clearLoginState(state)
			return fmt.Sprintf("no answer in %s", loginWatch), nil
		case <-ctx.Done():
			_ = cmd.Process.Kill()
			<-done
			clearLoginState(state)
			return "", ctx.Err()
		}
	}
}

// sshOwnLines drops ssh's debug lines and banner, so what is left can be
// read the same way as a run's stderr.
func sshOwnLines(log string) string {
	var kept []string
	for _, line := range strings.Split(log, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "debug") || strings.HasPrefix(line, "OpenSSH_") {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

// The state of a login lives beside its socket: a file with the process and
// when it started, and ssh's log.
func loginStatePath(controlPath string) string { return controlPath + ".login" }

func loginLogPath(state string) string { return state + ".log" }

func writePendingLogin(state string, pid int, started time.Time) error {
	data := strconv.Itoa(pid) + " " + strconv.FormatInt(started.Unix(), 10) + "\n"
	if err := os.WriteFile(state, []byte(data), 0o600); err != nil {
		return fmt.Errorf("recording the login: %w", err)
	}
	return nil
}

func readPendingLogin(state string) (int, time.Time, error) {
	data, err := os.ReadFile(state)
	if err != nil {
		return 0, time.Time{}, err
	}
	fields := strings.Fields(string(data))
	if len(fields) != 2 {
		return 0, time.Time{}, fs.ErrInvalid
	}
	pid, err := strconv.Atoi(fields[0])
	if err != nil {
		return 0, time.Time{}, err
	}
	unix, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil {
		return 0, time.Time{}, err
	}
	return pid, time.Unix(unix, 0), nil
}

func clearLoginState(state string) {
	_ = os.Remove(state)
	_ = os.Remove(loginLogPath(state))
}
