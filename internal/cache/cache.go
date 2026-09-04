// Package cache implements the local read-through cache.
//
// The backend stays the single source of truth: writes always go straight to
// it, and only reads are served from here, for speed and to stay well clear
// of API rate limits when agents list the board in a loop.
package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// Store is a tiny TTL-bounded JSON cache on disk.
type Store struct {
	dir     string
	ttl     time.Duration
	enabled bool
}

// New returns a store rooted at dir. A zero or negative ttl disables caching.
func New(dir string, ttl time.Duration, enabled bool) *Store {
	return &Store{dir: dir, ttl: ttl, enabled: enabled && ttl > 0 && dir != ""}
}

// Enabled reports whether the store will serve anything.
func (s *Store) Enabled() bool { return s != nil && s.enabled }

// TTL returns the freshness window.
func (s *Store) TTL() time.Duration { return s.ttl }

type entry struct {
	Key      string          `json:"key"`
	StoredAt time.Time       `json:"stored_at"`
	Payload  json.RawMessage `json:"payload"`
}

func (s *Store) file(key string) string {
	sum := sha256.Sum256([]byte(key))
	return filepath.Join(s.dir, hex.EncodeToString(sum[:8])+".json")
}

// Get decodes a fresh entry into out and reports whether it was a hit.
// A corrupt or stale entry is a miss, never an error.
func (s *Store) Get(key string, out any) (time.Time, bool) {
	if !s.Enabled() {
		return time.Time{}, false
	}
	raw, err := os.ReadFile(s.file(key))
	if err != nil {
		return time.Time{}, false
	}
	var e entry
	if err := json.Unmarshal(raw, &e); err != nil || e.Key != key {
		return time.Time{}, false
	}
	if time.Since(e.StoredAt) > s.ttl {
		return e.StoredAt, false
	}
	if err := json.Unmarshal(e.Payload, out); err != nil {
		return time.Time{}, false
	}
	return e.StoredAt, true
}

// Put stores v under key. Cache failures are reported but are never fatal to
// the caller: the value is already in hand.
func (s *Store) Put(key string, v any) error {
	if !s.Enabled() {
		return nil
	}
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	payload, err := json.Marshal(v)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(entry{Key: key, StoredAt: time.Now(), Payload: payload})
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.dir, ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	// Rename keeps concurrent readers from seeing a half-written entry.
	return os.Rename(tmp.Name(), s.file(key))
}

// Invalidate drops every cached entry.
func (s *Store) Invalidate() error {
	if s == nil || s.dir == "" {
		return nil
	}
	err := os.RemoveAll(s.dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
