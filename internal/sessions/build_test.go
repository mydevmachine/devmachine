package sessions

import (
	"reflect"
	"testing"
	"time"
)

const now = int64(1_000_000)

func session(name string, mutate func(*rawSession)) rawSession {
	s := rawSession{
		Name: name, Windows: 1, Created: now - 60, Activity: now - 60,
		Command: "zsh", Path: "/home/alice/acme-web",
	}
	if mutate != nil {
		mutate(&s)
	}
	return s
}

func buildOne(t *testing.T, snap Snapshot) Session {
	t.Helper()
	snap.Now = now
	got := Build(snap)
	if len(got) != 1 {
		t.Fatalf("Build returned %d sessions, want 1", len(got))
	}
	return got[0]
}

func TestBuildBusy(t *testing.T) {
	tests := []struct {
		name          string
		snap          Snapshot
		wantBusy      bool
		wantHarnesses []string
	}{
		{
			name: "harness in front with recent activity",
			snap: Snapshot{
				Sessions: []rawSession{session("web", func(s *rawSession) { s.Command = "claude"; s.Activity = now - 3 })},
				Panes:    []rawPane{{Session: "web", Command: "claude", PID: 10}},
			},
			wantBusy: true, wantHarnesses: []string{"claude"},
		},
		{
			name: "harness under a shell with old activity",
			snap: Snapshot{
				Sessions: []rawSession{session("web", func(s *rawSession) { s.Activity = now - 30 })},
				Panes:    []rawPane{{Session: "web", Command: "zsh", PID: 10}},
				Procs:    []proc{{PID: 10, PPID: 1, Comm: "zsh"}, {PID: 11, PPID: 10, Comm: "claude"}},
			},
			wantBusy: false, wantHarnesses: []string{"claude"},
		},
		{
			name: "harness deep under a shell, named by its full path",
			snap: Snapshot{
				Sessions: []rawSession{session("web", func(s *rawSession) { s.Activity = now - 5 })},
				Panes:    []rawPane{{Session: "web", Command: "zsh", PID: 10}},
				Procs: []proc{
					{PID: 10, PPID: 1, Comm: "-zsh"},
					{PID: 11, PPID: 10, Comm: "node"},
					{PID: 12, PPID: 11, Comm: "/usr/local/bin/codex"},
				},
			},
			wantBusy: true, wantHarnesses: []string{"codex"},
		},
		{
			name: "no harness, a program in front",
			snap: Snapshot{
				Sessions: []rawSession{session("web", func(s *rawSession) { s.Command = "make"; s.Activity = now - 600 })},
				Panes:    []rawPane{{Session: "web", Command: "make", PID: 10}},
			},
			wantBusy: true, wantHarnesses: []string{},
		},
		{
			name: "no harness, a shell in front",
			snap: Snapshot{
				Sessions: []rawSession{session("web", func(s *rawSession) { s.Activity = now })},
				Panes:    []rawPane{{Session: "web", Command: "zsh", PID: 10}},
			},
			wantBusy: false, wantHarnesses: []string{},
		},
		{
			name: "two harnesses in different panes, sorted",
			snap: Snapshot{
				Sessions: []rawSession{session("web", nil)},
				Panes: []rawPane{
					{Session: "web", Command: "codex", PID: 10},
					{Session: "web", Command: "zsh", PID: 20},
					{Session: "other", Command: "opencode", PID: 30},
				},
				Procs: []proc{{PID: 20, PPID: 1, Comm: "zsh"}, {PID: 21, PPID: 20, Comm: "claude"}},
			},
			wantBusy: false, wantHarnesses: []string{"claude", "codex"},
		},
		{
			name: "a program that only looks like a harness is not one",
			snap: Snapshot{
				Sessions: []rawSession{session("web", nil)},
				Panes:    []rawPane{{Session: "web", Command: "zsh", PID: 10}},
				Procs:    []proc{{PID: 10, PPID: 1, Comm: "zsh"}, {PID: 11, PPID: 10, Comm: "claude-helper"}},
			},
			wantBusy: false, wantHarnesses: []string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildOne(t, tt.snap)
			if got.Busy != tt.wantBusy {
				t.Errorf("Busy = %v, want %v", got.Busy, tt.wantBusy)
			}
			if !reflect.DeepEqual(got.Harnesses, tt.wantHarnesses) {
				t.Errorf("Harnesses = %#v, want %#v", got.Harnesses, tt.wantHarnesses)
			}
		})
	}
}

func TestBuildAttention(t *testing.T) {
	for alerts, want := range map[string]bool{"1!,2": true, "": false, "2#": false} {
		got := buildOne(t, Snapshot{Sessions: []rawSession{session("web", func(s *rawSession) { s.Alerts = alerts })}})
		if got.Attention != want {
			t.Errorf("alerts %q: Attention = %v, want %v", alerts, got.Attention, want)
		}
	}
}

func TestBuildAge(t *testing.T) {
	for created, want := range map[int64]int64{now - 7200: 7200, now + 50: 0} {
		got := buildOne(t, Snapshot{Sessions: []rawSession{session("web", func(s *rawSession) { s.Created = created })}})
		if got.AgeSeconds != want {
			t.Errorf("created %d: AgeSeconds = %d, want %d", created, got.AgeSeconds, want)
		}
	}
}

func TestBuildBranchComesFromTheActivePath(t *testing.T) {
	branches := map[string]string{"/home/alice/acme-web": "feature/checkout", "/home/alice/acme-api": "main"}
	got := buildOne(t, Snapshot{Sessions: []rawSession{session("web", nil)}, Branches: branches})
	if got.Branch != "feature/checkout" {
		t.Errorf("Branch = %q, want feature/checkout", got.Branch)
	}
	got = buildOne(t, Snapshot{
		Sessions: []rawSession{session("web", func(s *rawSession) { s.Path = "/tmp" })},
		Branches: branches,
	})
	if got.Branch != "" {
		t.Errorf("Branch for a path with no entry = %q, want empty", got.Branch)
	}
}

func TestBuildCopiesTheSessionInTmuxOrder(t *testing.T) {
	got := Build(Snapshot{Now: now, Sessions: []rawSession{
		session("web", func(s *rawSession) { s.Windows = 3; s.Created = 990_000; s.Activity = 999_000 }),
		session("api", nil),
	}})
	want := Session{
		Name: "web", Windows: 3, Path: "/home/alice/acme-web",
		CreatedAt: time.Unix(990_000, 0).UTC(), AgeSeconds: 10_000,
		LastActivityAt: time.Unix(999_000, 0).UTC(), Harnesses: []string{},
	}
	if len(got) != 2 || got[1].Name != "api" {
		t.Fatalf("Build = %#v, want web then api", got)
	}
	if !reflect.DeepEqual(got[0], want) {
		t.Errorf("Build()[0] =\n%#v\nwant\n%#v", got[0], want)
	}
}

func TestBuildWithNoSessionsIsEmptyNotNil(t *testing.T) {
	if got := Build(Snapshot{Now: now}); got == nil || len(got) != 0 {
		t.Errorf("Build = %#v, want an empty slice", got)
	}
}
