package chat

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"

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

func TestAskForksASessionOnTheTestMachine(t *testing.T) {
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
		"printf '%s\\n' '{\"type\":\"user\",\"cwd\":\"/tmp/dm-ask-repo\"}' > " + transcript + " && " +
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

	loc, failure := Locate(ctx, client, Target{Session: "dm-chat-ask", PathPrefix: []string{vpsFakeDir}})
	if failure != nil {
		t.Fatalf("Locate: %+v", failure)
	}
	if loc.Harness != "claude" || loc.SessionID != sid || loc.Folder != "/tmp/dm-ask-repo" {
		t.Fatalf("loc = %+v", loc)
	}
	out := Run(ctx, client, Request{
		Harness: Claude, Folder: loc.RunFolder, Message: "why?", Resume: loc.SessionID, Fork: true, PathPrefix: []string{vpsFakeDir},
	}, func(Event) {})
	if out.Failure != nil || out.SessionID == sid {
		t.Fatalf("out = %+v", out)
	}
	args, _ := client.Run(ctx, "cat "+vpsFakeDir+"/args")
	if !strings.Contains(args, "--resume\n"+sid+"\n--fork-session\n") {
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
