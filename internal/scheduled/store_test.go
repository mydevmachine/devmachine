package scheduled

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func openStore(t *testing.T) Store {
	t.Helper()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestPutGetListDelete(t *testing.T) {
	s := openStore(t)
	a, b := validTask(), validTask()
	a.ID, a.Name = "t_000001", "zeta"
	b.ID, b.Name = "t_000002", "alpha"
	for _, task := range []Task{a, b} {
		if err := s.Put(task); err != nil {
			t.Fatal(err)
		}
	}
	list, err := s.List()
	if err != nil || len(list) != 2 || list[0].Name != "alpha" {
		t.Fatalf("list = %+v err = %v", list, err)
	}
	got, err := s.Get("t_000001")
	if err != nil || got.Name != "zeta" {
		t.Fatalf("get = %+v err = %v", got, err)
	}
	if err := s.PutRun(Run{ID: NewRunID(time.Now()), TaskID: a.ID, Status: StatusSucceeded}); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(a.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get after delete: %v", err)
	}
	if _, err := os.Stat(s.runsDir(a.ID)); !os.IsNotExist(err) {
		t.Fatalf("runs left behind: %v", err)
	}
	if err := s.Delete(a.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete: %v", err)
	}
}

func TestRunsNewestFirstAndPrune(t *testing.T) {
	s := openStore(t)
	id := "t_000001"
	start := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	var oldest string
	for i := range 55 {
		r := Run{ID: NewRunID(start.Add(time.Duration(i) * time.Minute)), TaskID: id, Status: StatusSucceeded,
			StartedAt: start.Add(time.Duration(i) * time.Minute)}
		if i == 0 {
			oldest = r.ID
		}
		if err := s.PutRun(r); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(s.LogPath(id, r.ID), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Prune(id, 50); err != nil {
		t.Fatal(err)
	}
	runs, err := s.Runs(id)
	if err != nil || len(runs) != 50 {
		t.Fatalf("runs = %d err = %v", len(runs), err)
	}
	if !runs[0].StartedAt.After(runs[49].StartedAt) {
		t.Fatal("not newest first")
	}
	if _, err := os.Stat(s.LogPath(id, oldest)); !os.IsNotExist(err) {
		t.Fatal("the oldest log was kept")
	}
	last, err := s.LastRun(id)
	if err != nil || last == nil || last.ID != runs[0].ID {
		t.Fatalf("last = %+v err = %v", last, err)
	}
}

func TestLastRunNone(t *testing.T) {
	s := openStore(t)
	last, err := s.LastRun("t_000009")
	if err != nil || last != nil {
		t.Fatalf("last = %+v err = %v", last, err)
	}
}

func TestLastTickRoundTrip(t *testing.T) {
	s := openStore(t)
	if got, err := s.LastTick(); err != nil || !got.IsZero() {
		t.Fatalf("fresh = %v err = %v", got, err)
	}
	now := time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)
	if err := s.SetLastTick(now); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.LastTick(); !got.Equal(now) {
		t.Fatalf("got %v", got)
	}
}

func TestIDsCannotEscapeTheStore(t *testing.T) {
	s := openStore(t)
	if _, err := s.Get("../x"); err == nil || !strings.Contains(err.Error(), "task id") {
		t.Fatalf("get: %v", err)
	}
	bad := validTask()
	bad.ID = "../../etc"
	if err := s.Put(bad); err == nil {
		t.Fatal("put accepted a bad id")
	}
	if _, err := s.ReadLog("t_000001", "../../x"); err == nil {
		t.Fatal("read log accepted a bad run id")
	}
}
