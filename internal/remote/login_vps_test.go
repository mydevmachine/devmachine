package remote

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const agentSignRequest = 13

// holdingAgent is an SSH agent that answers everything at once except a
// signature, which it holds until approve is called: what 1Password does
// while it waits for Touch ID.
type holdingAgent struct {
	socket   string
	approved chan struct{}
	approve  func()
	signs    chan struct{}
}

func startHoldingAgent(t *testing.T, key string) *holdingAgent {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "dma")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	upstream := filepath.Join(dir, "real.sock")
	agent := exec.Command("ssh-agent", "-D", "-a", upstream)
	if err := agent.Start(); err != nil {
		t.Fatalf("starting ssh-agent: %v", err)
	}
	t.Cleanup(func() { _ = agent.Process.Kill(); _ = agent.Wait() })
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(upstream); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("ssh-agent never made its socket")
		}
		time.Sleep(20 * time.Millisecond)
	}
	add := exec.Command("ssh-add", key)
	add.Env = append(os.Environ(), "SSH_AUTH_SOCK="+upstream)
	if out, err := add.CombinedOutput(); err != nil {
		t.Fatalf("ssh-add: %v: %s", err, out)
	}

	h := &holdingAgent{socket: filepath.Join(dir, "held.sock"), approved: make(chan struct{}), signs: make(chan struct{}, 8)}
	var once sync.Once
	h.approve = func() { once.Do(func() { close(h.approved) }) }
	t.Cleanup(h.approve)
	listener, err := net.Listen("unix", h.socket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go h.serve(conn, upstream)
		}
	}()
	return h
}

func (h *holdingAgent) serve(conn net.Conn, upstream string) {
	defer conn.Close()
	up, err := net.Dial("unix", upstream)
	if err != nil {
		return
	}
	defer up.Close()
	for {
		request, err := readAgentMessage(conn)
		if err != nil {
			return
		}
		if request[4] == agentSignRequest {
			h.signs <- struct{}{}
			<-h.approved
		}
		if _, err := up.Write(request); err != nil {
			return
		}
		reply, err := readAgentMessage(up)
		if err != nil {
			return
		}
		if _, err := conn.Write(reply); err != nil {
			return
		}
	}
}

func readAgentMessage(r io.Reader) ([]byte, error) {
	header := make([]byte, 4)
	if _, err := io.ReadFull(r, header); err != nil {
		return nil, err
	}
	body := make([]byte, binary.BigEndian.Uint32(header))
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, err
	}
	return append(header, body...), nil
}

func TestLoginWaitsForTheAgentAndConnectsOnceItApproves(t *testing.T) {
	m := testMachine(t)
	agent := startHoldingAgent(t, m.Key)
	m.AgentKeyFile = m.Key + ".pub"
	m.Key = ""
	t.Setenv("SSH_AUTH_SOCK", agent.socket)
	fakeCacheDir(t)

	client, _, err := DialMux(context.Background(), m, "")
	if err != nil {
		t.Fatalf("DialMux returned %v", err)
	}
	mux := client.(*muxClient)
	t.Cleanup(func() {
		killPendingLogins(t, mux)
		_ = exec.Command("ssh", "-o", "ControlPath="+mux.controlPath(mux.addresses[0]), "-O", "exit", "x").Run()
	})

	start := time.Now()
	if err := mux.Login(context.Background()); !errors.Is(err, ErrLoginWaiting) {
		t.Fatalf("Login returned %v, want ErrLoginWaiting", err)
	}
	if elapsed := time.Since(start); elapsed > loginWatch {
		t.Fatalf("Login took %v", elapsed)
	}
	if err := mux.Login(context.Background()); !errors.Is(err, ErrLoginWaiting) {
		t.Fatalf("the second Login returned %v, want ErrLoginWaiting", err)
	}
	if n := len(agent.signs); n != 1 {
		t.Fatalf("the agent was asked to sign %d times, want 1", n)
	}

	agent.approve()
	deadline := time.Now().Add(10 * time.Second)
	for {
		err := mux.Login(context.Background())
		if err == nil {
			break
		}
		if !errors.Is(err, ErrLoginWaiting) || time.Now().After(deadline) {
			t.Fatalf("Login after the approval returned %v", err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	out, err := mux.Run(context.Background(), "echo up")
	if err != nil || strings.TrimSpace(out) != "up" {
		t.Fatalf("Run over the shared connection = %q, %v", out, err)
	}
	if n := len(agent.signs); n != 1 {
		t.Fatalf("the agent was asked to sign %d times, want 1: the run should reuse the connection", n)
	}
}
