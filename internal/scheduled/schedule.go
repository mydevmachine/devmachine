package scheduled

import (
	"fmt"
	"strconv"
	"time"
)

const lookAheadDays = 400

var weekdayNames = map[string]time.Weekday{
	"mon": time.Monday, "tue": time.Tuesday, "wed": time.Wednesday, "thu": time.Thursday,
	"fri": time.Friday, "sat": time.Saturday, "sun": time.Sunday,
}

// Next is the first time the schedule fires strictly after after, or the
// zero time when it never does again: a one-off already past, or a schedule
// that does not validate.
func (s Schedule) Next(after time.Time) time.Time {
	if s.validate() != nil {
		return time.Time{}
	}
	loc, _ := s.Location()
	after = after.In(loc)
	if s.Every == "hourly" {
		return s.nextHourly(after, loc)
	}
	hour, minute := s.clock()
	for i := 0; i <= lookAheadDays; i++ {
		day := time.Date(after.Year(), after.Month(), after.Day()+i, 0, 0, 0, 0, loc)
		if !s.firesOn(day) {
			continue
		}
		c := time.Date(day.Year(), day.Month(), day.Day(), hour, minute, 0, 0, loc)
		if c.After(after) {
			return c
		}
		if s.Every == "once" {
			return time.Time{}
		}
	}
	return time.Time{}
}

func (s Schedule) nextHourly(after time.Time, loc *time.Location) time.Time {
	c := time.Date(after.Year(), after.Month(), after.Day(), after.Hour(), *s.Minute, 0, 0, loc)
	for !c.After(after) {
		c = c.Add(time.Hour)
	}
	return c
}

func (s Schedule) clock() (hour, minute int) {
	hour, _ = strconv.Atoi(s.At[:2])
	minute, _ = strconv.Atoi(s.At[3:])
	return hour, minute
}

func (s Schedule) firesOn(day time.Time) bool {
	switch s.Every {
	case "daily":
		return true
	case "weekdays":
		return day.Weekday() != time.Saturday && day.Weekday() != time.Sunday
	case "weekly":
		return day.Weekday() == weekdayNames[s.Weekday]
	case "monthly":
		last := time.Date(day.Year(), day.Month()+1, 0, 0, 0, 0, 0, day.Location()).Day()
		return day.Day() == min(s.Day, last)
	case "once":
		return day.Format(time.DateOnly) == s.Date
	}
	return false
}

// Describe says the schedule in words, such as "Weekdays at 09:00".
func (s Schedule) Describe() string {
	switch s.Every {
	case "hourly":
		if s.Minute == nil {
			return "Every hour"
		}
		return fmt.Sprintf("Every hour at :%02d", *s.Minute)
	case "daily":
		return "Daily at " + s.At
	case "weekdays":
		return "Weekdays at " + s.At
	case "weekly":
		return fmt.Sprintf("Every %s at %s", weekdayNames[s.Weekday], s.At)
	case "monthly":
		return fmt.Sprintf("Monthly on day %d at %s", s.Day, s.At)
	case "once":
		return fmt.Sprintf("Once on %s at %s", s.Date, s.At)
	}
	return s.Every
}
