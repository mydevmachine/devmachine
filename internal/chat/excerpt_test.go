package chat

import (
	"os"
	"strings"
	"testing"
)

func rollout(t *testing.T) []byte {
	t.Helper()
	body, err := os.ReadFile("testdata/codex-rollout.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestExcerptHoldsTheLastTurnsAndTheMatches(t *testing.T) {
	e := BuildExcerpt(rollout(t), "/home/alice/acme-web", "Why did it change chargeservice?")
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

func TestExcerptWithoutMatchesIsTheLastTurns(t *testing.T) {
	e := BuildExcerpt(rollout(t), "/w", "zzzz?")
	if e.Count != 6 || strings.Contains(e.Prompt, "ChargeService") {
		t.Fatalf("count = %d\n%s", e.Count, e.Prompt)
	}
}

func TestExcerptStaysUnderItsSize(t *testing.T) {
	var b strings.Builder
	for range 200 {
		b.WriteString(`{"type":"response_item","payload":{"type":"function_call_output","output":"needle ` + strings.Repeat("x", 3000) + `"}}` + "\n")
	}
	e := BuildExcerpt([]byte(b.String()), "/w", "where is the needle")
	if len(e.Prompt) > excerptTotalMax+1024 {
		t.Fatalf("prompt is %d bytes", len(e.Prompt))
	}
	if e.Count == 0 {
		t.Fatal("no excerpt taken")
	}
}

func TestKeywordsDropShortAndCommonWords(t *testing.T) {
	got := strings.Join(keywords("Why did it change the ChargeService, and what about billing.go?"), ",")
	if got != "change,chargeservice,billing.go" {
		t.Fatalf("keywords = %s", got)
	}
}
