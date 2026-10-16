package chat

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mydevmachine/devmachine/internal/chat/chattest"
	"github.com/mydevmachine/devmachine/internal/config"
	"github.com/mydevmachine/devmachine/internal/remote"
)

const vpsFakeDir = "/tmp/dm-chat-fake"

func vpsClient(t *testing.T) remote.Client {
	t.Helper()
	host, port := os.Getenv("DEVMACHINE_TEST_HOST"), os.Getenv("DEVMACHINE_TEST_PORT")
	key, knownHosts := os.Getenv("DEVMACHINE_TEST_KEY"), os.Getenv("DEVMACHINE_TEST_KNOWN_HOSTS")
	if host == "" || port == "" || key == "" || knownHosts == "" {
		t.Skip("no test VPS: run `eval \"$(scripts/fake-vps.sh env)\"` first")
	}
	n, err := strconv.Atoi(port)
	if err != nil {
		t.Fatalf("DEVMACHINE_TEST_PORT is not a number: %v", err)
	}
	user := os.Getenv("DEVMACHINE_TEST_USER")
	if user == "" {
		user = "root"
	}
	m := config.Machine{Name: "sandbox", Hosts: []config.Host{{Address: host}}, User: user, Port: n, Key: key, KnownHostsFile: knownHosts}
	client, _, err := remote.DialMux(context.Background(), m, "")
	if err != nil {
		t.Fatalf("DialMux returned %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func uploadFakes(t *testing.T, client remote.Client) {
	t.Helper()
	ctx := context.Background()
	for _, name := range []string{"claude", "codex"} {
		body := chattest.Script(name)
		cmd := "mkdir -p " + vpsFakeDir + " && cat > " + vpsFakeDir + "/" + name + " && chmod 755 " + vpsFakeDir + "/" + name
		if _, err := client.RunInput(ctx, cmd, strings.NewReader(body)); err != nil {
			t.Fatalf("uploading the fake %s: %v", name, err)
		}
	}
	t.Cleanup(func() { _, _ = client.Run(context.Background(), "rm -rf "+vpsFakeDir) })
}

func TestSendAgainstTheTestMachine(t *testing.T) {
	client := vpsClient(t)
	uploadFakes(t, client)
	message := `it's "quoted" $HOME and a \ backslash`
	out := Run(context.Background(), client, Request{
		Harness: Claude, Folder: "~/dm-chat-test", Message: message, PathPrefix: []string{vpsFakeDir},
	}, func(Event) {})
	t.Cleanup(func() { _, _ = client.Run(context.Background(), "rm -rf ~/dm-chat-test") })
	if out.Failure != nil {
		t.Fatalf("failure = %+v", out.Failure)
	}
	if out.Text != "echo: "+message || out.SessionID == "" {
		t.Fatalf("out = %+v", out)
	}
	cwd, err := client.Run(context.Background(), "cat "+vpsFakeDir+"/cwd")
	if err != nil || !strings.HasSuffix(strings.TrimSpace(cwd), "/dm-chat-test") {
		t.Fatalf("cwd = %q, %v", cwd, err)
	}

	out = Run(context.Background(), client, Request{Harness: Codex, Folder: "~/dm-chat-test", Message: "hi", PathPrefix: []string{vpsFakeDir}}, func(Event) {})
	if out.Failure != nil || out.Text != "codex says hi" {
		t.Fatalf("codex out = %+v", out)
	}

	out = Run(context.Background(), client, Request{Harness: Codex, Folder: "~/dm-chat-test", Message: "hi"}, func(Event) {})
	if out.Failure == nil || out.Failure.Kind != KindHarnessMissing {
		t.Fatalf("without the fake on PATH: %+v", out.Failure)
	}
}

func TestAskReadsASessionOnTheTestMachine(t *testing.T) {
	client := vpsClient(t)
	ctx := context.Background()
	if _, err := client.Run(ctx, "command -v tmux >/dev/null 2>&1"); err != nil {
		t.Skip("tmux not on the test machine")
	}
	uploadFakes(t, client)
	const sid = "55555555-aaaa-4bbb-8ccc-000000000001"
	transcript := "$HOME/.claude/projects/" + ClaudeSlug("/tmp/dm-ask-repo") + "/" + sid + ".jsonl"
	cleanup := "tmux kill-session -t dm-chat-ask 2>/dev/null; rm -rf /tmp/dm-ask-repo \"$HOME/.claude/projects/" + ClaudeSlug("/tmp/dm-ask-repo") + "\""
	t.Cleanup(func() { _, _ = client.Run(context.Background(), cleanup+"; true") })
	setup := "mkdir -p /tmp/dm-ask-repo \"$(dirname " + transcript + ")\" && " +
		"printf '%s\\n' '{\"type\":\"user\",\"cwd\":\"/tmp/dm-ask-repo\",\"message\":{\"content\":\"rename the ChargeService\"}}' > " + transcript + " && " +
		"{ tmux kill-session -t dm-chat-ask 2>/dev/null; true; } && " +
		"{ for i in 1 2 3 4 5; do tmux new-session -d -s dm-chat-ask -c /tmp/dm-ask-repo && break; sleep 0.3; done; } && " +
		"tmux has-session -t dm-chat-ask"
	if _, err := client.Run(ctx, setup); err != nil {
		t.Fatalf("setting up the session: %v", err)
	}
	before, err := client.Run(ctx, "sha256sum "+transcript)
	if err != nil {
		t.Fatal(err)
	}

	loc, failure := Locate(ctx, client, Target{Session: "dm-chat-ask", PathPrefix: []string{vpsFakeDir}, Question: "why ChargeService?"})
	if failure != nil {
		t.Fatalf("Locate: %+v", failure)
	}
	if loc.Harness != "claude" || loc.SessionID != sid || loc.Folder != "/tmp/dm-ask-repo" {
		t.Fatalf("loc = %+v", loc)
	}
	prompt := BuildContext(loc.Body, loc.Harness, loc.Folder, "", "why ChargeService?").Prompt
	out := Run(ctx, client, Request{
		Harness: Claude, Folder: "~/dm-chat-test", Message: prompt, Ask: true, PathPrefix: []string{vpsFakeDir},
	}, func(Event) {})
	t.Cleanup(func() { _, _ = client.Run(context.Background(), "rm -rf ~/dm-chat-test") })
	if out.Failure != nil || out.Text != "echo: Question: why ChargeService?" {
		t.Fatalf("out = %+v", out)
	}
	stdin, _ := client.Run(ctx, "cat "+vpsFakeDir+"/stdin")
	if !strings.Contains(stdin, "[user] rename the ChargeService") {
		t.Fatalf("stdin = %q", stdin)
	}
	if args, _ := client.Run(ctx, "cat "+vpsFakeDir+"/args"); !strings.Contains(args, "--no-session-persistence\n") || strings.Contains(args, "--resume") {
		t.Fatalf("args = %q", args)
	}
	after, err := client.Run(ctx, "sha256sum "+transcript)
	if err != nil || after != before {
		t.Fatalf("the transcript changed: %q -> %q (%v)", before, after, err)
	}

	if _, failure := Locate(ctx, client, Target{Session: "dm-chat-no-such-session"}); failure == nil || failure.Kind != KindSessionNotFound {
		t.Fatalf("missing session: %+v", failure)
	}
}

func TestWarmOnTheTestMachine(t *testing.T) {
	client := vpsClient(t)
	uploadFakes(t, client)
	ctx := context.Background()
	t.Cleanup(func() { _, _ = client.Run(context.Background(), "rm -rf ~/dm-chat-warm "+vpsFakeDir+"/mode") })
	w, failure := StartWarm(ctx, client, Request{Harness: Claude, Folder: "~/dm-chat-warm", PathPrefix: []string{vpsFakeDir}}, func(Event) {})
	if failure != nil {
		t.Fatalf("StartWarm: %+v", failure)
	}
	defer w.Close()
	first := w.Turn("one", nil, func(Event) {})
	second := w.Turn("two", nil, func(Event) {})
	if first.Failure != nil || first.Text != "turn 1: one" || second.Failure != nil || second.Text != "turn 2: two" {
		t.Fatalf("first = %+v, second = %+v", first, second)
	}
	if _, err := client.Run(ctx, "echo slow > "+vpsFakeDir+"/mode"); err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	time.AfterFunc(time.Second, func() { close(stop) })
	started := time.Now()
	if out := w.Turn("three", stop, func(Event) {}); out.Failure == nil || out.Failure.Kind != KindCancelled {
		t.Fatalf("stopped turn = %+v", out)
	}
	if time.Since(started) > 10*time.Second || !w.Dead() {
		t.Fatal("the stop did not end the remote process")
	}
}

func TestRunCancelStopsTheHarnessOnTheTestMachine(t *testing.T) {
	client := vpsClient(t)
	uploadFakes(t, client)
	t.Cleanup(func() { _, _ = client.Run(context.Background(), "rm -rf ~/dm-chat-cancel "+vpsFakeDir+"/mode") })
	if _, err := client.Run(context.Background(), "echo slow > "+vpsFakeDir+"/mode"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(time.Second, cancel)
	started := time.Now()
	out := Run(ctx, client, Request{Harness: Claude, Folder: "~/dm-chat-cancel", Message: "q", PathPrefix: []string{vpsFakeDir}}, func(Event) {})
	if out.Failure == nil || out.Failure.Kind != KindCancelled {
		t.Fatalf("out = %+v", out)
	}
	if time.Since(started) > 10*time.Second {
		t.Fatalf("the remote harness outlived the cancel: %s", time.Since(started))
	}
}
