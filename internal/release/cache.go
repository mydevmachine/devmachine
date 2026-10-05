package release

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// CacheTTL is how long a looked-up version is believed. GitHub allows 60
// unauthenticated requests an hour per address, and `doctor` runs often.
const CacheTTL = 6 * time.Hour

// Fetcher looks a version up for real.
type Fetcher func(context.Context) (string, error)

// Cache remembers the answer to "what is latest" on disk, one file per key.
//
// A zero Cache, or one whose directory cannot be written, still answers: it
// asks every time. Remembering is an optimisation, never a reason to fail.
type Cache struct {
	Dir string
	TTL time.Duration
	Now func() time.Time
}

type entry struct {
	Version   string    `json:"version"`
	CheckedAt time.Time `json:"checked_at"`
}

// Lookup answers from disk when the stored answer is younger than the TTL,
// and asks otherwise. A failed lookup is returned and never stored.
func (c Cache) Lookup(ctx context.Context, key string, fetch Fetcher) (string, error) {
	if e, ok := c.read(key); ok && c.now().Sub(e.CheckedAt) < c.TTL {
		return e.Version, nil
	}
	return c.Refresh(ctx, key, fetch)
}

// LookupOrStale is Lookup that falls back to a stored answer of any age when
// asking fails: an old answer is still better than none.
func (c Cache) LookupOrStale(ctx context.Context, key string, fetch Fetcher) (string, error) {
	version, err := c.Lookup(ctx, key, fetch)
	if err == nil {
		return version, nil
	}
	if stored, _, ok := c.Stored(key); ok {
		return stored, nil
	}
	return "", err
}

// Stored is the answer on disk and when it was read, whatever its age.
func (c Cache) Stored(key string) (string, time.Time, bool) {
	e, ok := c.read(key)
	return e.Version, e.CheckedAt, ok
}

// Refresh always asks, and stores the answer for the next Lookup.
func (c Cache) Refresh(ctx context.Context, key string, fetch Fetcher) (string, error) {
	version, err := fetch(ctx)
	if err != nil {
		return "", err
	}
	c.write(key, entry{Version: version, CheckedAt: c.now()})
	return version, nil
}

func (c Cache) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func (c Cache) path(key string) string {
	return filepath.Join(c.Dir, fmt.Sprintf("latest-%s.json", key))
}

func (c Cache) read(key string) (entry, bool) {
	if c.Dir == "" {
		return entry{}, false
	}
	body, err := os.ReadFile(c.path(key))
	if err != nil {
		return entry{}, false
	}
	var e entry
	if err := json.Unmarshal(body, &e); err != nil || e.Version == "" {
		return entry{}, false
	}
	return e, true
}

func (c Cache) write(key string, e entry) {
	if c.Dir == "" {
		return
	}
	body, err := json.Marshal(e)
	if err != nil {
		return
	}
	if err := os.MkdirAll(c.Dir, 0o700); err != nil {
		return
	}
	_ = os.WriteFile(c.path(key), body, 0o600)
}
