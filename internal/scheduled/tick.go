package scheduled

import (
	"fmt"
	"time"
)

// MissedAfter is how late a fire may be found and still run. Later than
// that, the machine was off or asleep, and running hours-old work then
// would surprise more than help.
const MissedAfter = 2 * time.Minute

// Starter starts a run of a task without waiting for it.
type Starter func(taskID, runID string) error

// TickResult names the tasks a tick started and the ones it found missed.
type TickResult struct {
	Started []string `json:"started"`
	Missed  []string `json:"missed"`
}

// Tick is what the timer runs every minute: it starts every task that
// became due since the last tick and records the ones it was too late for.
func Tick(s Store, now time.Time, start Starter) (TickResult, error) {
	res := TickResult{Started: []string{}, Missed: []string{}}
	last, err := s.LastTick()
	if err != nil {
		return res, err
	}
	if last.IsZero() {
		last = now.Add(-time.Minute)
	}
	tasks, err := s.List()
	if err != nil {
		return res, err
	}
	for _, t := range tasks {
		if t.Paused || t.Done {
			continue
		}
		late, due := firesSince(t.Schedule, last, now)
		if !late.IsZero() {
			if err := recordMissed(s, t, late); err != nil {
				return res, err
			}
			res.Missed = append(res.Missed, t.ID)
		}
		if due.IsZero() {
			continue
		}
		runID := NewRunID(now)
		if err := start(t.ID, runID); err != nil {
			failed := Run{ID: runID, TaskID: t.ID, Status: StatusError, Trigger: TriggerSchedule,
				StartedAt: now, EndedAt: now, Message: fmt.Sprintf("starting the run: %v", err)}
			if err := s.PutRun(failed); err != nil {
				return res, err
			}
			continue
		}
		res.Started = append(res.Started, t.ID)
	}
	return res, s.SetLastTick(now)
}

// maxFiresPerTick bounds the walk over a long gap: a year of an hourly task.
const maxFiresPerTick = 24 * 400

// firesSince walks the fires in (last, now]. late is the newest one found
// too late to run, zero when none; due is one still in time, zero when none.
func firesSince(s Schedule, last, now time.Time) (late, due time.Time) {
	for fire, i := s.Next(last), 0; !fire.IsZero() && !fire.After(now) && i < maxFiresPerTick; fire, i = s.Next(fire), i+1 {
		if now.Sub(fire) > MissedAfter {
			late = fire
		} else {
			due = fire
		}
	}
	return late, due
}

func recordMissed(s Store, t Task, at time.Time) error {
	loc, _ := t.Schedule.Location()
	run := Run{ID: NewRunID(at), TaskID: t.ID, Status: StatusMissed, Trigger: TriggerSchedule,
		StartedAt: at, EndedAt: at,
		Message: fmt.Sprintf("the machine was not running at %s", at.In(loc).Format("2006-01-02 15:04 MST"))}
	if err := s.PutRun(run); err != nil {
		return err
	}
	if t.Schedule.Every == "once" {
		t.Done = true
		return s.Put(t)
	}
	return s.Prune(t.ID, RunsKept)
}
