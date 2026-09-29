package config

import "testing"

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestDiskDefaults(t *testing.T) {
	c, err := Load(env(map[string]string{"ACME_DISK_ROOT": "/var/acme"}))
	if err != nil {
		t.Fatal(err)
	}
	if c.Provider != "disk" || c.Addr != ":8080" {
		t.Fatalf("got %+v", c)
	}
}

func TestS3NeedsBucket(t *testing.T) {
	if _, err := Load(env(map[string]string{"ACME_STORAGE": "s3"})); err == nil {
		t.Fatal("want an error")
	}
}

func TestGitDefaultsBranch(t *testing.T) {
	c, err := Load(env(map[string]string{"ACME_STORAGE": "git", "ACME_GIT_REPO": "r", "ACME_GIT_TOKEN": "t"}))
	if err != nil {
		t.Fatal(err)
	}
	if c.Branch != "main" {
		t.Fatalf("branch %q", c.Branch)
	}
}
