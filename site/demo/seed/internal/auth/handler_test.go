package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func get(t *testing.T, store *Store, token string) int {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	Handler(store).ServeHTTP(rec, req)
	return rec.Code
}

func TestValidSession(t *testing.T) {
	s := NewStore()
	s.Put("ok", Session{User: "ada", Expires: time.Now().Add(time.Hour)})
	if code := get(t, s, "ok"); code != http.StatusOK {
		t.Fatalf("got %d, want 200", code)
	}
}

func TestUnknownSessionIs401(t *testing.T) {
	if code := get(t, NewStore(), "nope"); code != http.StatusUnauthorized {
		t.Fatalf("got %d, want 401", code)
	}
}

func TestExpiredSessionIs401(t *testing.T) {
	s := NewStore()
	s.Put("old", Session{User: "ada", Expires: time.Now().Add(-time.Minute)})
	if code := get(t, s, "old"); code != http.StatusUnauthorized {
		t.Fatalf("got %d, want 401", code)
	}
}
