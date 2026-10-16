package scheduled

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/mydevmachine/devmachine/internal/harness"
)

// How long a run may take: the default, and the most a task may ask for.
const (
	DefaultTimeout = 30 * time.Minute
	MaxTimeout     = 6 * time.Hour
)

// Every is the list of recurrences a schedule can have.
var Every = []string{"once", "hourly", "daily", "weekdays", "weekly", "monthly"}

// Weekdays are the names a weekly schedule takes, Monday first.
var Weekdays = []string{"mon", "tue", "wed", "thu", "fri", "sat", "sun"}

var (
	clockPattern  = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d$`)
	taskIDPattern = regexp.MustCompile(`^t_[0-9a-f]{6}$`)
)

// Schedule says when a task runs, in the person's timezone.
type Schedule struct {
	Every    string `json:"every"`
	At       string `json:"at,omitempty"`
	Minute   *int   `json:"minute,omitempty"`
	Weekday  string `json:"weekday,omitempty"`
	Day      int    `json:"day,omitempty"`
	Date     string `json:"date,omitempty"`
	Timezone string `json:"timezone"`
}

// Action is what a task runs: a shell command, or a prompt for a harness.
type Action struct {
	Kind           string `json:"kind"`
	Command        string `json:"command,omitempty"`
	Harness        string `json:"harness,omitempty"`
	Prompt         string `json:"prompt,omitempty"`
	Model          string `json:"model,omitempty"`
	PermissionMode string `json:"permission_mode,omitempty"`
}

// Task is one scheduled task, stored as one file in the workspace.
type Task struct {
	Version   int       `json:"version"`
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Paused    bool      `json:"paused"`
	Done      bool      `json:"done"`
	Folder    string    `json:"folder"`
	Timeout   string    `json:"timeout,omitempty"`
	Schedule  Schedule  `json:"schedule"`
	Action    Action    `json:"action"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// NewID makes a task id, such as t_7f3a9c.
func NewID() string {
	b := make([]byte, 3)
	_, _ = rand.Read(b)
	return "t_" + hex.EncodeToString(b)
}

// ValidTaskID says whether id has the shape NewID makes, which is also what
// keeps an id from naming a path outside the store.
func ValidTaskID(id string) bool { return taskIDPattern.MatchString(id) }

// Validate refuses a task that could not run as written. A dangerous
// permission mode is refused unless allowDangerous says the person chose it.
func (t Task) Validate(allowDangerous bool) error {
	if strings.TrimSpace(t.Name) == "" {
		return errors.New("a task needs a name")
	}
	if strings.TrimSpace(t.Folder) == "" {
		return errors.New("a task needs a folder to run in")
	}
	if t.Timeout != "" {
		d, err := time.ParseDuration(t.Timeout)
		if err != nil || d <= 0 || d > MaxTimeout {
			return fmt.Errorf("timeout %q must be a duration such as 30m, up to %s", t.Timeout, MaxTimeout)
		}
	}
	if err := t.Schedule.validate(); err != nil {
		return err
	}
	return t.Action.validate(allowDangerous)
}

// TimeoutDuration is how long a run may take before it is stopped.
func (t Task) TimeoutDuration() time.Duration {
	if d, err := time.ParseDuration(t.Timeout); err == nil && d > 0 {
		return d
	}
	return DefaultTimeout
}

// Location is the schedule's timezone.
func (s Schedule) Location() (*time.Location, error) {
	if s.Timezone == "" {
		return nil, errors.New("a schedule needs a timezone, such as America/Sao_Paulo")
	}
	loc, err := time.LoadLocation(s.Timezone)
	if err != nil {
		return nil, fmt.Errorf("unknown timezone %q: %w", s.Timezone, err)
	}
	return loc, nil
}

func (s Schedule) validate() error {
	if !slices.Contains(Every, s.Every) {
		return fmt.Errorf("every %q must be one of %s", s.Every, strings.Join(Every, ", "))
	}
	if s.Every == "hourly" {
		if s.Minute == nil || *s.Minute < 0 || *s.Minute > 59 {
			return errors.New("an hourly task needs a minute from 0 to 59")
		}
	} else if !clockPattern.MatchString(s.At) {
		return fmt.Errorf("at %q must be a time such as 09:00", s.At)
	}
	switch s.Every {
	case "weekly":
		if !slices.Contains(Weekdays, s.Weekday) {
			return fmt.Errorf("a weekly task needs a weekday, one of %s", strings.Join(Weekdays, ", "))
		}
	case "monthly":
		if s.Day < 1 || s.Day > 31 {
			return errors.New("a monthly task needs a day from 1 to 31")
		}
	case "once":
		if _, err := time.Parse(time.DateOnly, s.Date); err != nil {
			return fmt.Errorf("a one-off task needs a date such as 2026-10-16, not %q", s.Date)
		}
	}
	_, err := s.Location()
	return err
}

func (a Action) validate(allowDangerous bool) error {
	switch a.Kind {
	case "command":
		if strings.TrimSpace(a.Command) == "" {
			return errors.New("a command task needs a command")
		}
		return nil
	case "prompt":
		h, ok := harness.Get(a.Harness)
		if !ok {
			return fmt.Errorf("unknown harness %q; it can be %s", a.Harness, strings.Join(harness.Names(), ", "))
		}
		if strings.TrimSpace(a.Prompt) == "" {
			return errors.New("a prompt task needs a prompt")
		}
		if _, err := h.Argv(a.Prompt, a.Model, a.PermissionMode); err != nil {
			return err
		}
		if h.IsDangerous(a.PermissionMode) && !allowDangerous {
			return fmt.Errorf("%s runs with no approvals at all; pass --allow-dangerous to schedule it", a.PermissionMode)
		}
		return nil
	default:
		return fmt.Errorf("kind %q must be command or prompt", a.Kind)
	}
}
