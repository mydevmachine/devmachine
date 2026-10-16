package chat

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestTitleIsTheFirstLineCutTo60(t *testing.T) {
	long := strings.Repeat("é", 70)
	for in, want := range map[string]string{
		"  hello there\nsecond line": "hello there",
		long:                         strings.Repeat("é", 60),
		"\n\n":                       "New chat",
	} {
		if got := Title(in); got != want {
			t.Errorf("Title(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCreateListsNewestFirstAndHidesAsks(t *testing.T) {
	s := openTestStore(t)
	first, err := s.Create(Record{Title: "first", Harness: "claude", Workspace: "local", Folder: "~/chats"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Create(Record{Title: "second", Harness: "codex", Workspace: "alice", Folder: "~/chats"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(Record{Title: "ask", Kind: KindAsk, Session: "checkout"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Update(first.ID, func(r *Record) { r.HarnessSessionID = "sid-1" }); err != nil {
		t.Fatal(err)
	}

	visible, err := s.List(false)
	if err != nil {
		t.Fatal(err)
	}
	if len(visible) != 2 || visible[0].ID != first.ID || visible[1].ID != second.ID {
		t.Fatalf("visible = %+v", visible)
	}
	if visible[0].Kind != KindChat || !strings.HasPrefix(visible[0].ID, "c_") {
		t.Fatalf("record = %+v", visible[0])
	}
	all, err := s.List(true)
	if err != nil || len(all) != 3 {
		t.Fatalf("all = %+v, %v", all, err)
	}
}

func TestRenameAndDelete(t *testing.T) {
	s := openTestStore(t)
	r, err := s.Create(Record{Title: "old"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AppendMessage(r.ID, Message{Role: RoleUser, Text: "hi", At: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := s.Update(r.ID, func(r *Record) { r.Title = "new" }); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(r.ID)
	if err != nil || got.Title != "new" {
		t.Fatalf("got %+v, %v", got, err)
	}
	if err := s.Delete(r.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(r.ID); err == nil || !strings.Contains(err.Error(), "no chat") {
		t.Fatalf("Get after delete = %v", err)
	}
	if _, err := os.Stat(filepath.Join(s.dir, r.ID+".jsonl")); !os.IsNotExist(err) {
		t.Fatalf("messages file still there: %v", err)
	}
	if err := s.Delete(r.ID); err == nil {
		t.Fatal("deleting twice succeeded")
	}
}

func TestMessagesKeepTheirOrderAndTools(t *testing.T) {
	s := openTestStore(t)
	r, _ := s.Create(Record{Title: "t"})
	at := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	_ = s.AppendMessage(r.ID, Message{Role: RoleUser, Text: "where is it?", At: at})
	_ = s.AppendMessage(r.ID, Message{Role: RoleAssistant, Text: "here", Tools: []Tool{{Name: "Read", Summary: "main.go"}}, At: at})
	got, err := s.Messages(r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[1].Tools[0].Summary != "main.go" || got[0].Text != "where is it?" {
		t.Fatalf("messages = %+v", got)
	}
}

func TestTheStoreIsNeverCommitted(t *testing.T) {
	s := openTestStore(t)
	body, err := os.ReadFile(filepath.Join(s.dir, ".gitignore"))
	if err != nil || string(body) != "*\n" {
		t.Fatalf(".gitignore = %q, %v", body, err)
	}
}

func TestConcurrentWritersLoseNothing(t *testing.T) {
	dir := t.TempDir()
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() {
			s, err := Open(dir)
			if err != nil {
				t.Error(err)
				return
			}
			for range 20 {
				if _, err := s.Create(Record{Title: "x"}); err != nil {
					t.Error(err)
				}
			}
		})
	}
	wg.Wait()
	s, _ := Open(dir)
	all, err := s.List(true)
	if err != nil || len(all) != 40 {
		t.Fatalf("got %d records, %v", len(all), err)
	}
	entries, _ := os.ReadDir(s.dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".index") {
			t.Fatalf("temporary file left behind: %s", e.Name())
		}
	}
}
