package chat

import (
	"fmt"
	"strings"
	"testing"
)

func pair(q, a string) []Message {
	return []Message{{Role: RoleUser, Text: q}, {Role: RoleAssistant, Text: a}}
}

func TestEarlierHoldsThePreviousPairs(t *testing.T) {
	var messages []Message
	messages = append(messages, pair("why ChargeService?", "It rounds to cents.")...)
	messages = append(messages, Message{Role: RoleUser, Text: "a question that failed"}, Message{Role: RoleAssistant, Error: &Failure{Kind: KindCancelled}})
	messages = append(messages, pair("and the tests?", "They pass.")...)

	got := Earlier(messages)
	want := "Earlier in this side conversation:\n\nQ: why ChargeService?\nA: It rounds to cents.\n\nQ: and the tests?\nA: They pass.\n\n"
	if got != want {
		t.Fatalf("got %q", got)
	}
	if Earlier(nil) != "" || Earlier([]Message{{Role: RoleUser, Text: "only a question"}}) != "" {
		t.Fatal("no pair should give no block")
	}
}

func TestEarlierKeepsTheLastSixPairsWithinItsSize(t *testing.T) {
	var messages []Message
	for i := range 10 {
		messages = append(messages, pair(fmt.Sprintf("question %d", i), fmt.Sprintf("answer %d", i))...)
	}
	got := Earlier(messages)
	if strings.Contains(got, "question 3") || !strings.Contains(got, "question 4") || !strings.Contains(got, "answer 9") {
		t.Fatalf("got %q", got)
	}

	messages = append(pair("old", strings.Repeat("x", 6000)), pair("new", strings.Repeat("y", 6000))...)
	got = Earlier(messages)
	if strings.Contains(got, "Q: old") || !strings.Contains(got, "Q: new") || len(got) > earlierMax {
		t.Fatalf("size %d, holds old: %v", len(got), strings.Contains(got, "Q: old"))
	}
}
