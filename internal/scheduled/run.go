package scheduled

import (
	"crypto/rand"
	"encoding/hex"
	"regexp"
	"time"
)

// The states a run can end in, plus running while it goes.
const (
	StatusRunning   = "running"
	StatusSucceeded = "succeeded"
	StatusFailed    = "failed"
	StatusTimeout   = "timeout"
	StatusError     = "error"
	StatusMissed    = "missed"
	StatusSkipped   = "skipped"
)

// What started a run.
const (
	TriggerSchedule = "schedule"
	TriggerManual   = "manual"
)

var runIDPattern = regexp.MustCompile(`^\d{8}T\d{6}Z-[0-9a-f]{4}$`)

// Run is the record of one run of a task.
type Run struct {
	ID         string    `json:"id"`
	TaskID     string    `json:"task_id"`
	Status     string    `json:"status"`
	Trigger    string    `json:"trigger"`
	StartedAt  time.Time `json:"started_at"`
	EndedAt    time.Time `json:"ended_at,omitzero"`
	DurationMS int64     `json:"duration_ms"`
	ExitCode   *int      `json:"exit_code,omitempty"`
	Message    string    `json:"message,omitempty"`
}

// NewRunID makes a run id that sorts by time, such as 20261015T120000Z-ab12.
func NewRunID(now time.Time) string {
	b := make([]byte, 2)
	_, _ = rand.Read(b)
	return now.UTC().Format("20060102T150405Z") + "-" + hex.EncodeToString(b)
}

// ValidRunID says whether id has the shape NewRunID makes.
func ValidRunID(id string) bool { return runIDPattern.MatchString(id) }
