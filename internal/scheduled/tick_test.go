package scheduled

import (
	"errors"
	"slices"
	"testing"
	"time"
)

type starts struct{ ids []string }

func (s *starts) start(taskID, runID string) error {
	if !ValidRunID(runID) {
		return errors.New("bad run id")
	}
	s.ids = append(s.ids, taskID)
	return nil
}

func dailyAt(t *testing.T, s Store, at string) Task {
	t.Helper()
	task := commandTask("true")
	task.Schedule = Schedule{Every: "daily", At: at, Timezone: "UTC"}
	mustPut(t, s, task)
	return task
}

func at(day, hour, minute, sec int) time.Time {
	return time.Date(2026, 10, day, hour, minute, sec, 0, time.UTC)
}

func TestTickStartsDueTaskOnce(t *testing.T) {
	_, s := setupHome(t)
	task := dailyAt(t, s, "09:00")
	if err := s.SetLastTick(at(15, 8, 59, 1)); err != nil {
		t.Fatal(err)
	}
	var st starts

	res, err := Tick(s, at(15, 9, 0, 2), st.start)
	if err != nil || !slices.Equal(st.ids, []string{task.ID}) || !slices.Equal(res.Started, []string{task.ID}) {
		t.Fatalf("res = %+v starts = %v err = %v", res, st.ids, err)
	}
	if _, err := Tick(s, at(15, 9, 1, 2), st.start); err != nil || len(st.ids) != 1 {
		t.Fatalf("second tick started again: %v", st.ids)
	}
}

func TestTickRecordsOneMissedAfterDowntime(t *testing.T) {
	_, s := setupHome(t)
	task := dailyAt(t, s, "09:00")
	if err := s.SetLastTick(at(13, 8, 0, 0)); err != nil {
		t.Fatal(err)
	}
	var st starts

	res, err := Tick(s, at(15, 12, 0, 0), st.start)
	if err != nil || len(st.ids) != 0 || !slices.Equal(res.Missed, []string{task.ID}) {
		t.Fatalf("res = %+v starts = %v err = %v", res, st.ids, err)
	}
	runs, _ := s.Runs(task.ID)
	if len(runs) != 1 || runs[0].Status != StatusMissed || runs[0].Message == "" {
		t.Fatalf("runs = %+v", runs)
	}

	if _, err := Tick(s, at(16, 9, 0, 1), st.start); err != nil || !slices.Equal(st.ids, []string{task.ID}) {
		t.Fatalf("next day did not start: %v", st.ids)
	}
}

func TestTickSkipsPausedAndDone(t *testing.T) {
	_, s := setupHome(t)
	paused := dailyAt(t, s, "09:00")
	paused.Paused = true
	mustPut(t, s, paused)
	done := dailyAt(t, s, "09:00")
	done.Done = true
	mustPut(t, s, done)
	if err := s.SetLastTick(at(15, 8, 59, 0)); err != nil {
		t.Fatal(err)
	}
	var st starts
	if _, err := Tick(s, at(15, 9, 0, 1), st.start); err != nil || len(st.ids) != 0 {
		t.Fatalf("starts = %v err = %v", st.ids, err)
	}
}

func TestTickFirstEverLooksBackOneMinute(t *testing.T) {
	_, s := setupHome(t)
	task := dailyAt(t, s, "09:00")
	var st starts
	if _, err := Tick(s, at(15, 9, 0, 30), st.start); err != nil || !slices.Equal(st.ids, []string{task.ID}) {
		t.Fatalf("starts = %v err = %v", st.ids, err)
	}
	if got, _ := s.LastTick(); !got.Equal(at(15, 9, 0, 30)) {
		t.Fatalf("last tick = %v", got)
	}
}

func TestTickStartFailureIsRecorded(t *testing.T) {
	_, s := setupHome(t)
	task := dailyAt(t, s, "09:00")
	other := dailyAt(t, s, "09:00")
	if err := s.SetLastTick(at(15, 8, 59, 0)); err != nil {
		t.Fatal(err)
	}
	calls := 0
	fail := func(taskID, runID string) error {
		calls++
		if taskID == task.ID {
			return errors.New("fork failed")
		}
		return nil
	}
	if _, err := Tick(s, at(15, 9, 0, 1), fail); err != nil || calls != 2 {
		t.Fatalf("calls = %d err = %v", calls, err)
	}
	last, _ := s.LastRun(task.ID)
	if last == nil || last.Status != StatusError {
		t.Fatalf("last = %+v", last)
	}
	if l, _ := s.LastRun(other.ID); l != nil {
		t.Fatalf("other = %+v", l)
	}
}

func TestTickMissedOnceIsDone(t *testing.T) {
	_, s := setupHome(t)
	task := commandTask("true")
	task.Schedule = Schedule{Every: "once", Date: "2026-10-14", At: "09:00", Timezone: "UTC"}
	mustPut(t, s, task)
	if err := s.SetLastTick(at(13, 0, 0, 0)); err != nil {
		t.Fatal(err)
	}
	var st starts
	if _, err := Tick(s, at(15, 9, 0, 0), st.start); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Get(task.ID); !got.Done {
		t.Fatal("a missed one-off task is not done")
	}
}
