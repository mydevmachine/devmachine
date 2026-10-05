package release

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
		ok   bool
	}{
		{"0.7.18", "v0.7.18", 0, true},
		{"v0.7.18", "v0.7.9", 1, true},
		{"v0.7.9", "v0.7.18", -1, true},
		{"v0.8.0", "v0.7.99", 1, true},
		{"v1.0.0", "v0.99.99", 1, true},
		{"v16", "v17", -1, true},
		{"v17", "v16", 1, true},
		{"v17", "v17.0", 0, true},
		{"v0.8.0-rc1", "v0.8.0", 0, true},
		{"dev", "v0.7.18", 0, false},
		{"v0.7.18", "", 0, false},
		{"main", "v1", 0, false},
	}
	for _, c := range cases {
		got, ok := Compare(c.a, c.b)
		if ok != c.ok || (ok && sign(got) != c.want) {
			t.Errorf("Compare(%q, %q) = %d, %v; want %d, %v", c.a, c.b, got, ok, c.want, c.ok)
		}
	}
}

func sign(n int) int {
	switch {
	case n > 0:
		return 1
	case n < 0:
		return -1
	}
	return 0
}

func TestNewer(t *testing.T) {
	if !Newer("v0.7.19", "0.7.18") {
		t.Fatal("v0.7.19 is newer than 0.7.18")
	}
	if Newer("v0.7.18", "0.7.18") {
		t.Fatal("the same version is not newer")
	}
	if Newer("v0.7.19", "dev") {
		t.Fatal("nothing is newer than a version that cannot be read")
	}
}

func TestLatestReadsTheTag(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			t.Error("no User-Agent header was sent")
		}
		_, _ = w.Write([]byte(`{"tag_name": "v0.8.0"}`))
	}))
	defer server.Close()

	got, err := Latest(context.Background(), server.URL)
	if err != nil || got != "v0.8.0" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestLatestReportsAnError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()

	if _, err := Latest(context.Background(), server.URL); err == nil {
		t.Fatal("a 403 is not a release")
	}
}

func TestLatestReportsAnEmptyTag(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	if _, err := Latest(context.Background(), server.URL); err == nil {
		t.Fatal("an empty tag is not a release")
	}
}

type counter struct {
	calls  int
	answer string
	err    error
}

func (c *counter) fetch(context.Context) (string, error) {
	c.calls++
	return c.answer, c.err
}

func TestCacheAnswersFromDiskWithinTheTTL(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	cache := Cache{Dir: t.TempDir(), TTL: 6 * time.Hour, Now: func() time.Time { return now }}
	source := &counter{answer: "v17"}

	for range 3 {
		got, err := cache.Lookup(context.Background(), "packages", source.fetch)
		if err != nil || got != "v17" {
			t.Fatalf("got %q, %v", got, err)
		}
	}
	if source.calls != 1 {
		t.Fatalf("asked the network %d times within the TTL", source.calls)
	}

	now = now.Add(6*time.Hour + time.Minute)
	source.answer = "v18"
	got, err := cache.Lookup(context.Background(), "packages", source.fetch)
	if err != nil || got != "v18" || source.calls != 2 {
		t.Fatalf("after the TTL: got %q, %v, %d calls", got, err, source.calls)
	}
}

func TestCacheKeepsKeysApart(t *testing.T) {
	cache := Cache{Dir: t.TempDir(), TTL: time.Hour}
	cli := &counter{answer: "v0.8.0"}
	pkgs := &counter{answer: "v17"}

	if got, _ := cache.Lookup(context.Background(), "cli", cli.fetch); got != "v0.8.0" {
		t.Fatalf("cli: %q", got)
	}
	if got, _ := cache.Lookup(context.Background(), "packages", pkgs.fetch); got != "v17" {
		t.Fatalf("packages: %q", got)
	}
}

func TestCacheNeverStoresAFailure(t *testing.T) {
	cache := Cache{Dir: t.TempDir(), TTL: time.Hour}
	source := &counter{err: errors.New("offline")}

	if _, err := cache.Lookup(context.Background(), "cli", source.fetch); err == nil {
		t.Fatal("a failed lookup must say so")
	}
	source.err, source.answer = nil, "v0.8.0"
	if got, err := cache.Lookup(context.Background(), "cli", source.fetch); err != nil || got != "v0.8.0" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestCacheRefreshAlwaysAsksAndStores(t *testing.T) {
	cache := Cache{Dir: t.TempDir(), TTL: time.Hour}
	source := &counter{answer: "v16"}
	if _, err := cache.Lookup(context.Background(), "packages", source.fetch); err != nil {
		t.Fatal(err)
	}
	source.answer = "v17"
	if got, err := cache.Refresh(context.Background(), "packages", source.fetch); err != nil || got != "v17" {
		t.Fatalf("got %q, %v", got, err)
	}
	if got, _ := cache.Lookup(context.Background(), "packages", source.fetch); got != "v17" || source.calls != 2 {
		t.Fatalf("the refresh was not stored: %q after %d calls", got, source.calls)
	}
}

func TestCacheIgnoresAGarbledFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "latest-cli.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	cache := Cache{Dir: dir, TTL: time.Hour}
	source := &counter{answer: "v0.8.0"}
	if got, err := cache.Lookup(context.Background(), "cli", source.fetch); err != nil || got != "v0.8.0" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestCacheWithNoDirectoryStillAnswers(t *testing.T) {
	source := &counter{answer: "v0.8.0"}
	if got, err := (Cache{}).Lookup(context.Background(), "cli", source.fetch); err != nil || got != "v0.8.0" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestCacheStoredReadsAnAnswerOfAnyAge(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	cache := Cache{Dir: t.TempDir(), TTL: time.Hour, Now: func() time.Time { return now }}
	if _, _, ok := cache.Stored("packages"); ok {
		t.Fatal("an empty cache has nothing stored")
	}
	if _, err := cache.Refresh(context.Background(), "packages", (&counter{answer: "v17"}).fetch); err != nil {
		t.Fatal(err)
	}
	now = now.Add(30 * 24 * time.Hour)
	got, at, ok := cache.Stored("packages")
	if !ok || got != "v17" || !at.Equal(time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("got %q at %v, %v", got, at, ok)
	}
}

func TestCacheLookupOrStaleFallsBackToAnOldAnswer(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	cache := Cache{Dir: t.TempDir(), TTL: time.Hour, Now: func() time.Time { return now }}
	source := &counter{answer: "v17"}
	if _, err := cache.Lookup(context.Background(), "packages", source.fetch); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Hour)
	source.err = errors.New("offline")
	got, err := cache.LookupOrStale(context.Background(), "packages", source.fetch)
	if err != nil || got != "v17" || source.calls != 2 {
		t.Fatalf("got %q, %v after %d calls", got, err, source.calls)
	}
}

func TestCacheLookupOrStaleFailsWithNothingStored(t *testing.T) {
	cache := Cache{Dir: t.TempDir(), TTL: time.Hour}
	source := &counter{err: errors.New("offline")}
	if _, err := cache.LookupOrStale(context.Background(), "packages", source.fetch); err == nil {
		t.Fatal("no answer at all must be an error")
	}
}
