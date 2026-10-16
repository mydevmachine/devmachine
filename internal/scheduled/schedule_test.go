package scheduled

import (
	"testing"
	"time"
)

func mustLoc(t *testing.T, name string) *time.Location {
	t.Helper()
	l, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func TestNextDaily(t *testing.T) {
	sp := mustLoc(t, "America/Sao_Paulo")
	s := Schedule{Every: "daily", At: "09:00", Timezone: "America/Sao_Paulo"}
	got := s.Next(time.Date(2026, 10, 15, 8, 59, 0, 0, sp))
	if want := time.Date(2026, 10, 15, 9, 0, 0, 0, sp); !got.Equal(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	got = s.Next(time.Date(2026, 10, 15, 9, 0, 0, 0, sp))
	if want := time.Date(2026, 10, 16, 9, 0, 0, 0, sp); !got.Equal(want) {
		t.Fatalf("strictly after: got %v want %v", got, want)
	}
}

func TestNextUsesTheScheduleZoneNotTheCaller(t *testing.T) {
	s := Schedule{Every: "daily", At: "09:00", Timezone: "America/Sao_Paulo"}
	got := s.Next(time.Date(2026, 10, 15, 11, 0, 0, 0, time.UTC))
	if want := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Fatalf("got %v want %v", got.UTC(), want)
	}
}

func TestNextDailyAcrossDST(t *testing.T) {
	ny := mustLoc(t, "America/New_York")
	s := Schedule{Every: "daily", At: "09:00", Timezone: "America/New_York"}
	got := s.Next(time.Date(2026, 11, 1, 9, 30, 0, 0, ny)).In(ny)
	if got.Hour() != 9 || got.Minute() != 0 || got.Day() != 2 {
		t.Fatalf("fall back: got %v", got)
	}
	got = s.Next(time.Date(2026, 3, 8, 9, 30, 0, 0, ny)).In(ny)
	if got.Hour() != 9 || got.Day() != 9 {
		t.Fatalf("spring forward: got %v", got)
	}
}

func TestNextWeekdaysSkipsWeekend(t *testing.T) {
	s := Schedule{Every: "weekdays", At: "09:00", Timezone: "UTC"}
	got := s.Next(time.Date(2026, 10, 16, 10, 0, 0, 0, time.UTC))
	if got.Weekday() != time.Monday || got.Day() != 19 {
		t.Fatalf("got %v", got)
	}
}

func TestNextWeekly(t *testing.T) {
	s := Schedule{Every: "weekly", Weekday: "mon", At: "09:00", Timezone: "UTC"}
	if got := s.Next(time.Date(2026, 10, 19, 9, 0, 0, 0, time.UTC)); got.Day() != 26 {
		t.Fatalf("got %v", got)
	}
	s.Weekday = "sun"
	if got := s.Next(time.Date(2026, 10, 19, 9, 0, 0, 0, time.UTC)); got.Weekday() != time.Sunday || got.Day() != 25 {
		t.Fatalf("sunday: got %v", got)
	}
}

func TestNextHourly(t *testing.T) {
	m := 15
	s := Schedule{Every: "hourly", Minute: &m, Timezone: "UTC"}
	if got := s.Next(time.Date(2026, 10, 15, 10, 15, 0, 0, time.UTC)); !got.Equal(time.Date(2026, 10, 15, 11, 15, 0, 0, time.UTC)) {
		t.Fatalf("got %v", got)
	}
	if got := s.Next(time.Date(2026, 10, 15, 10, 14, 59, 0, time.UTC)); !got.Equal(time.Date(2026, 10, 15, 10, 15, 0, 0, time.UTC)) {
		t.Fatalf("same hour: got %v", got)
	}
}

func TestNextMonthlyClampsToLastDay(t *testing.T) {
	s := Schedule{Every: "monthly", Day: 31, At: "09:00", Timezone: "UTC"}
	got := s.Next(time.Date(2027, 1, 31, 10, 0, 0, 0, time.UTC))
	if !got.Equal(time.Date(2027, 2, 28, 9, 0, 0, 0, time.UTC)) {
		t.Fatalf("feb: got %v", got)
	}
	got = s.Next(got)
	if !got.Equal(time.Date(2027, 3, 31, 9, 0, 0, 0, time.UTC)) {
		t.Fatalf("mar: got %v", got)
	}
	got = s.Next(got)
	if !got.Equal(time.Date(2027, 4, 30, 9, 0, 0, 0, time.UTC)) {
		t.Fatalf("apr: got %v", got)
	}
}

func TestNextOnce(t *testing.T) {
	s := Schedule{Every: "once", Date: "2026-10-16", At: "09:00", Timezone: "UTC"}
	if got := s.Next(time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)); !got.Equal(time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC)) {
		t.Fatalf("got %v", got)
	}
	if got := s.Next(time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC)); !got.IsZero() {
		t.Fatalf("past once: got %v", got)
	}
}

func TestNextInvalidIsZero(t *testing.T) {
	if got := (Schedule{Every: "daily", At: "09:00", Timezone: "Mars/Base"}).Next(time.Now()); !got.IsZero() {
		t.Fatalf("got %v", got)
	}
}

func TestDescribe(t *testing.T) {
	m := 15
	cases := map[string]Schedule{
		"Daily at 09:00":              {Every: "daily", At: "09:00"},
		"Weekdays at 09:00":           {Every: "weekdays", At: "09:00"},
		"Every hour at :15":           {Every: "hourly", Minute: &m},
		"Every Monday at 09:00":       {Every: "weekly", Weekday: "mon", At: "09:00"},
		"Monthly on day 31 at 09:00":  {Every: "monthly", Day: 31, At: "09:00"},
		"Once on 2026-10-16 at 09:00": {Every: "once", Date: "2026-10-16", At: "09:00"},
	}
	for want, s := range cases {
		if got := s.Describe(); got != want {
			t.Errorf("got %q want %q", got, want)
		}
	}
}
