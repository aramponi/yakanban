package cache

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

type payload struct {
	Tasks []string `json:"tasks"`
}

func TestPutThenGet(t *testing.T) {
	store := New(t.TempDir(), time.Minute, true)
	if err := store.Put("list", payload{Tasks: []string{"1", "2"}}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	var got payload
	if _, ok := store.Get("list", &got); !ok {
		t.Fatal("a fresh entry should be a hit")
	}
	if len(got.Tasks) != 2 {
		t.Fatalf("payload = %+v", got)
	}
}

func TestStaleEntryIsAMiss(t *testing.T) {
	store := New(t.TempDir(), time.Nanosecond, true)
	if err := store.Put("list", payload{Tasks: []string{"1"}}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Millisecond)
	var got payload
	if _, ok := store.Get("list", &got); ok {
		t.Fatal("an entry older than the TTL should not be served")
	}
}

func TestDifferentKeysDoNotCollide(t *testing.T) {
	store := New(t.TempDir(), time.Minute, true)
	if err := store.Put("a", payload{Tasks: []string{"a"}}); err != nil {
		t.Fatal(err)
	}
	var got payload
	if _, ok := store.Get("b", &got); ok {
		t.Fatal("key b should miss")
	}
}

func TestDisabledStoreNeverStores(t *testing.T) {
	dir := t.TempDir()
	store := New(dir, time.Minute, false)
	if err := store.Put("list", payload{}); err != nil {
		t.Fatalf("Put on a disabled store should be a no-op, got %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("a disabled store wrote %d files", len(entries))
	}
	var got payload
	if _, ok := store.Get("list", &got); ok {
		t.Fatal("a disabled store should always miss")
	}
}

func TestInvalidateRemovesEverything(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cache")
	store := New(dir, time.Minute, true)
	if err := store.Put("list", payload{Tasks: []string{"1"}}); err != nil {
		t.Fatal(err)
	}
	if err := store.Invalidate(); err != nil {
		t.Fatalf("Invalidate: %v", err)
	}
	var got payload
	if _, ok := store.Get("list", &got); ok {
		t.Fatal("the cache should be empty after Invalidate")
	}
	if err := store.Invalidate(); err != nil {
		t.Fatalf("Invalidate on an empty cache should succeed, got %v", err)
	}
}

func TestCorruptEntryIsAMissNotAnError(t *testing.T) {
	dir := t.TempDir()
	store := New(dir, time.Minute, true)
	if err := store.Put("list", payload{Tasks: []string{"1"}}); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	if err := os.WriteFile(filepath.Join(dir, entries[0].Name()), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	var got payload
	if _, ok := store.Get("list", &got); ok {
		t.Fatal("a corrupt entry should not be served")
	}
}
