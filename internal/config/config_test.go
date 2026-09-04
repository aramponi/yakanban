package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)

	cfg := Default("demo", "github")
	cfg.Providers["github"] = map[string]any{"owner": "acme", "repo": "app", "project_number": 3}
	if err := cfg.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if loaded.Board.Name != "demo" || loaded.Provider != "github" {
		t.Fatalf("round trip lost the identity: %+v", loaded)
	}
	if loaded.ClaimTimeout.Duration() != time.Hour {
		t.Fatalf("claim timeout = %s, want 1h", loaded.ClaimTimeout.Duration())
	}
	if loaded.Cache.TTL.Duration() != 60*time.Second {
		t.Fatalf("cache ttl = %s", loaded.Cache.TTL.Duration())
	}
	board := loaded.BoardInfo()
	if !board.IsInitial("Backlog") || !board.IsTerminal("Done") {
		t.Fatalf("column semantics were lost: %+v", board.Statuses)
	}
	if loaded.Settings("github")["repo"] != "app" {
		t.Fatalf("provider settings were lost: %v", loaded.Settings("github"))
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "claim_timeout: 1h") {
		t.Fatalf("durations should serialise as strings:\n%s", raw)
	}
}

func TestFindWalksUp(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Default("demo", "github").Save(filepath.Join(root, FileName)); err != nil {
		t.Fatal(err)
	}

	found, err := Find(nested)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if found != filepath.Join(root, FileName) {
		t.Fatalf("Find = %q", found)
	}
}

func TestFindReportsAnActionableErrorWhenAbsent(t *testing.T) {
	_, err := Find(t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "yakanban init") {
		t.Fatalf("err = %v, want a hint to run init", err)
	}
}

func TestLoadRejectsANewerSchema(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	body := "version: 99\nprovider: github\nstatuses:\n  - name: Todo\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadFile(path); err == nil || !strings.Contains(err.Error(), "newer yakanban") {
		t.Fatalf("err = %v, want a version mismatch", err)
	}
}

func TestLoadRejectsAnIncompleteDescriptor(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	if err := os.WriteFile(path, []byte("version: 1\nprovider: github\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadFile(path); err == nil || !strings.Contains(err.Error(), "status") {
		t.Fatalf("err = %v, want the missing statuses to be reported", err)
	}
}

func TestCacheDirIsRelativeToTheDescriptor(t *testing.T) {
	dir := t.TempDir()
	cfg := Default("demo", "github")
	if err := cfg.Save(filepath.Join(dir, FileName)); err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, DirName, "cache"); cfg.CacheDir() != want {
		t.Fatalf("CacheDir = %q, want %q", cfg.CacheDir(), want)
	}
}
