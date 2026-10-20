package redact

import (
	"slices"
	"strings"
	"testing"
)

const fakeGitHubToken = "ghp_FAKEFAKEFAKEFAKEFAKEFAKEFAKEFAKE0000"

func TestStringHidesSecrets(t *testing.T) {
	cases := []struct {
		name, in, secret string
	}{
		{"github classic token", "cloning with " + fakeGitHubToken, fakeGitHubToken},
		{"github oauth token", "token gho_FAKEFAKEFAKEFAKEFAKEFAKE1111", "gho_FAKEFAKEFAKEFAKEFAKEFAKE1111"},
		{"github fine grained token", "github_pat_FAKE_FAKEFAKEFAKEFAKEFAKE_2222 used", "github_pat_FAKE_FAKEFAKEFAKEFAKEFAKE_2222"},
		{"anthropic key", "key sk-ant-api03-FAKEFAKEFAKEFAKEFAKE", "sk-ant-api03-FAKEFAKEFAKEFAKEFAKE"},
		{"openai key", "sk-proj-FAKEFAKEFAKEFAKEFAKE in use", "sk-proj-FAKEFAKEFAKEFAKEFAKE"},
		{"tailscale auth key", "tailscale up --authkey tskey-auth-kFAKE-FAKEFAKE", "tskey-auth-kFAKE-FAKEFAKE"},
		{"authorization header", "Authorization: Basic ZmFrZTpmYWtl", "ZmFrZTpmYWtl"},
		{"bearer token", `curl -H "x: Bearer fake.jwt.value"`, "fake.jwt.value"},
		{"token assignment", "GH_TOKEN=not-a-real-token", "not-a-real-token"},
		{"password assignment", "db_password='fake pass'", "fake pass"},
		{"api key assignment", "OPENAI_API_KEY=fakefake", "fakefake"},
		{"private key assignment", "private-key=fakefake", "fakefake"},
		{"auth assignment", "auth=fakefake", "fakefake"},
		{"json secret", `{"name": "alice", "client_secret": "fake-secret"}`, "fake-secret"},
		{"json api key", `{"apiKey":"fake-key-1"}`, "fake-key-1"},
		{"python dict password", `ok: [main] => (item={'user': 'alice', 'password': 'fake-pass'})`, "fake-pass"},
		{"env file line", "DATABASE_URL=postgres://db.example.com/app", "postgres://db.example.com/app"},
		{"exported env line", "export STRIPE_KEY=fake_live_value", "fake_live_value"},
		{"env line in a diff", "+SENTRY_DSN=https://fake@example.com/1", "https://fake@example.com/1"},
		{"url credentials", "fetching https://alice:fake-pass@git.example.com/repo.git", "fake-pass"},
		{"one-line private key", `"-----BEGIN OPENSSH PRIVATE KEY-----\nFAKEKEYBODY\n-----END OPENSSH PRIVATE KEY-----"`, "FAKEKEYBODY"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := String(c.in)
			if strings.Contains(got, c.secret) {
				t.Fatalf("String(%q) = %q, still holds %q", c.in, got, c.secret)
			}
			if !strings.Contains(got, Mask) {
				t.Fatalf("String(%q) = %q, has no %q", c.in, got, Mask)
			}
		})
	}
}

func TestStringHidesAMultiLinePrivateKey(t *testing.T) {
	in := "writing the key\n" +
		"-----BEGIN RSA PRIVATE KEY-----\n" +
		"FAKELINEONE\n" +
		"FAKELINETWO\n" +
		"-----END RSA PRIVATE KEY-----\n" +
		"done on 203.0.113.10"

	got := String(in)

	for _, secret := range []string{"FAKELINEONE", "FAKELINETWO"} {
		if strings.Contains(got, secret) {
			t.Fatalf("key body survived: %q", got)
		}
	}
	for _, keep := range []string{"writing the key", "done on 203.0.113.10", "BEGIN RSA PRIVATE KEY", "END RSA PRIVATE KEY"} {
		if !strings.Contains(got, keep) {
			t.Fatalf("lost %q: %q", keep, got)
		}
	}
}

func TestRedactorHidesAPrivateKeyStreamedOneLineAtATime(t *testing.T) {
	var r Redactor
	var got []string
	for _, line := range []string{"-----BEGIN PRIVATE KEY-----", "FAKELINE", "-----END PRIVATE KEY-----", "after"} {
		if out, keep := r.Line(line); keep {
			got = append(got, out)
		}
	}

	joined := strings.Join(got, "\n")
	if strings.Contains(joined, "FAKELINE") {
		t.Fatalf("key body survived: %q", joined)
	}
	if got[len(got)-1] != "after" {
		t.Fatalf("the line after the key is lost: %q", got)
	}
}

func TestStringKeepsWhatMakesTheLogUseful(t *testing.T) {
	lines := []string{
		"TASK [base : install packages] on main (203.0.113.10)",
		"connecting to alice@100.64.0.7:22",
		"ok: [vps.example.com] => workspace bob",
		"devmachine sync --machine main --workspace alice",
		"ssh: handshake failed: ssh: unable to authenticate, attempted methods [none publickey]",
		"PLAY RECAP *********",
	}
	for _, line := range lines {
		if got := String(line); got != line {
			t.Fatalf("String(%q) = %q, want it unchanged", line, got)
		}
	}
}

func TestArgvHidesFlagValues(t *testing.T) {
	cases := []struct {
		in, want []string
	}{
		{[]string{"login", "--token", "fake-token"}, []string{"login", "--token", Mask}},
		{[]string{"login", "--token=fake-token"}, []string{"login", "--token=" + Mask}},
		{[]string{"setup", "--password", "fake-pass", "--address", "203.0.113.10"}, []string{"setup", "--password", Mask, "--address", "203.0.113.10"}},
		{[]string{"x", "--value", "fake"}, []string{"x", "--value", Mask}},
		{[]string{"x", "--api-key=fake"}, []string{"x", "--api-key=" + Mask}},
		{[]string{"x", "--auth-key", "tskey-auth-FAKE"}, []string{"x", "--auth-key", Mask}},
		{[]string{"run", "--package", "github", "--", "login", "--token", "fake"}, []string{"run", "--package", "github", "--", "login", "--token", Mask}},
		{[]string{"x", "GH_TOKEN=fake"}, []string{"x", "GH_TOKEN=" + Mask}},
		{[]string{"x", fakeGitHubToken}, []string{"x", "ghp_" + Mask}},
	}
	for _, c := range cases {
		if got := Argv(c.in); !slices.Equal(got, c.want) {
			t.Fatalf("Argv(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestArgvLeavesBooleanPasswordFlagsAlone(t *testing.T) {
	in := []string{"machines", "add", "--password-stdin", "--address", "203.0.113.10", "--name", "main"}
	if got := Argv(in); !slices.Equal(got, in) {
		t.Fatalf("Argv(%q) = %q", in, got)
	}
}

func TestArgvHidesTheValueGivenToSecretsSet(t *testing.T) {
	cases := []struct {
		in, want []string
	}{
		{[]string{"secrets", "set", "OPENAI", "fake-value"}, []string{"secrets", "set", "OPENAI", Mask}},
		{[]string{"--config", "/tmp/alice", "secrets", "set", "--workspace", "bob", "X", "fake"},
			[]string{"--config", "/tmp/alice", "secrets", "set", "--workspace", "bob", "X", Mask}},
		{[]string{"secrets", "set", "X", "--stdin"}, []string{"secrets", "set", "X", "--stdin"}},
		{[]string{"secrets", "rm", "X"}, []string{"secrets", "rm", "X"}},
	}
	for _, c := range cases {
		if got := Argv(c.in); !slices.Equal(got, c.want) {
			t.Fatalf("Argv(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestArgvDoesNotChangeItsInput(t *testing.T) {
	in := []string{"login", "--token", "fake"}
	Argv(in)
	if in[2] != "fake" {
		t.Fatalf("input changed: %q", in)
	}
}

func TestRedactingTwiceChangesNothingMore(t *testing.T) {
	in := `printf -- "-----BEGIN OPENSSH PRIVATE KEY-----\nFAKEBODY\n-----END OPENSSH PRIVATE KEY-----\n"; echo done on 203.0.113.10`

	once := String(in)
	if twice := String(once); twice != once {
		t.Fatalf("once  %q\ntwice %q", once, twice)
	}
	if !strings.Contains(once, "echo done on 203.0.113.10") {
		t.Fatalf("lost what came after the key: %q", once)
	}

	var r Redactor
	streamed, _ := r.Line("-----BEGIN PRIVATE KEY----- " + Mask)
	if r.inKey {
		t.Fatalf("an already masked key opened a key block: %q", streamed)
	}
}
