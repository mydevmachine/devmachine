package chat

import (
	"os"
	"strings"
	"testing"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestContextOfACodexRollout(t *testing.T) {
	e := BuildContext(fixture(t, "codex-rollout.jsonl"), "codex", "/home/alice/acme-web", "", "Why did it change chargeservice?")
	for _, want := range []string{
		"The session ran in /home/alice/acme-web",
		"[user] turn 2", "[assistant] answer 4",
		"[user] Move the ChargeService rounding to cents",
		"[tool] shell",
		"[tool output] app/charge_service.rb:4: class ChargeService",
		"[assistant] ChargeService now rounds to cents before saving.",
		"Question: Why did it change chargeservice?",
	} {
		if !strings.Contains(e.Prompt, want) {
			t.Errorf("prompt is missing %q:\n%s", want, e.Prompt)
		}
	}
	for _, unwanted := range []string{"environment_context", "permissions instructions", "long instructions", "working"} {
		if strings.Contains(e.Prompt, unwanted) {
			t.Errorf("prompt holds %q", unwanted)
		}
	}
	if e.Count != 10 {
		t.Errorf("count = %d", e.Count)
	}
	if strings.Index(e.Prompt, "Move the ChargeService") > strings.Index(e.Prompt, "turn 2") {
		t.Error("the excerpts are not in the transcript's order")
	}
}

func TestContextOfAClaudeTranscript(t *testing.T) {
	e := BuildContext(fixture(t, "claude-session.jsonl"), "claude", "/home/alice/acme-web", "", "what changed in ChargeService?")
	for _, want := range []string{
		"[user] Make ChargeService round to cents",
		"[assistant] I will look at ChargeService first.",
		"[tool] Read app/charge_service.rb",
		"[tool] Edit app/charge_service.rb",
		"[assistant] ChargeService now rounds to cents.",
		"[tool output] class ChargeService HUGE_START",
	} {
		if !strings.Contains(e.Prompt, want) {
			t.Errorf("prompt is missing %q:\n%s", want, e.Prompt)
		}
	}
	for _, unwanted := range []string{"secret reasoning", "sub-agent's private note", "<command-name>", "HUGE_END", "queue-operation"} {
		if strings.Contains(e.Prompt, unwanted) {
			t.Errorf("prompt holds %q:\n%s", unwanted, e.Prompt)
		}
	}
}

func TestContextReflectsWhatTheSessionDidSince(t *testing.T) {
	body := fixture(t, "claude-session.jsonl")
	before := BuildContext(body, "claude", "/w", "", "and now?")
	body = append(body, []byte(`{"type":"user","message":{"role":"user","content":"remember KESTREL"}}`+"\n")...)
	after := BuildContext(body, "claude", "/w", "", "and now?")
	if strings.Contains(before.Prompt, "KESTREL") || !strings.Contains(after.Prompt, "[user] remember KESTREL") {
		t.Fatalf("after = %s", after.Prompt)
	}
}

func TestContextWithoutMatchesIsTheLastTurns(t *testing.T) {
	e := BuildContext(fixture(t, "codex-rollout.jsonl"), "codex", "/w", "", "zzzz?")
	if e.Count != 6 || strings.Contains(e.Prompt, "ChargeService") {
		t.Fatalf("count = %d\n%s", e.Count, e.Prompt)
	}
}

func TestContextStaysUnderItsSize(t *testing.T) {
	var b strings.Builder
	for range 200 {
		b.WriteString(`{"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"text":"needle ` + strings.Repeat("x", 3000) + `"}]}}` + "\n")
	}
	e := BuildContext([]byte(b.String()), "codex", "/w", "", "where is the needle")
	if len(e.Prompt) > contextMax+2048 {
		t.Fatalf("prompt is %d bytes", len(e.Prompt))
	}
	if e.Count == 0 {
		t.Fatal("no excerpt taken")
	}
}

func TestContextCarriesTheSideConversation(t *testing.T) {
	e := BuildContext(fixture(t, "codex-rollout.jsonl"), "codex", "/w", "Earlier in this side conversation:\n\nQ: a\nA: b\n\n", "and now?")
	if !strings.HasSuffix(e.Prompt, "</transcript>\n\nEarlier in this side conversation:\n\nQ: a\nA: b\n\nQuestion: and now?") {
		t.Fatalf("prompt ends with %q", e.Prompt[len(e.Prompt)-120:])
	}
}

func TestKeywordsDropShortAndCommonWords(t *testing.T) {
	got := strings.Join(keywords("Why did it change the ChargeService, and what about billing.go?"), ",")
	if got != "change,chargeservice,billing.go" {
		t.Fatalf("keywords = %s", got)
	}
}
