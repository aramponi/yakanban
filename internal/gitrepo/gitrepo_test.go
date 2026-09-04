package gitrepo

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// newRepo builds a checkout with a bare remote called origin, one commit, and
// main tracking origin/main.
func newRepo(t *testing.T) (dir string, headOID string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	remote := filepath.Join(root, "remote.git")
	work := filepath.Join(root, "work")

	run := func(wd string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = wd
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return strings.TrimSpace(string(out))
	}

	run(root, "init", "--bare", "-b", "main", remote)
	run(root, "init", "-b", "main", work)
	if err := os.WriteFile(filepath.Join(work, "file.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(work, "add", ".")
	run(work, "commit", "-m", "first")
	run(work, "remote", "add", "origin", remote)
	run(work, "push", "-u", "origin", "main")
	return work, run(work, "rev-parse", "HEAD")
}

func TestResolveBaseDefaultsToTheUpstream(t *testing.T) {
	dir, head := newRepo(t)
	oid, ref, err := Open(dir).ResolveBase("")
	if err != nil {
		t.Fatalf("ResolveBase: %v", err)
	}
	if oid != head {
		t.Fatalf("oid = %s, want the pushed HEAD %s", oid, head)
	}
	if !strings.Contains(ref, "origin/main") {
		t.Fatalf("ref = %q, want the remote-tracking branch", ref)
	}
}

// The whole point of resolving against the remote: a commit that exists only
// locally is not a base the backend can branch from.
func TestResolveBaseIgnoresUnpushedCommits(t *testing.T) {
	dir, pushed := newRepo(t)
	cmd := exec.Command("git", "commit", "--allow-empty", "-m", "local only")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("commit: %v\n%s", err, out)
	}

	oid, _, err := Open(dir).ResolveBase("")
	if err != nil {
		t.Fatalf("ResolveBase: %v", err)
	}
	if oid != pushed {
		t.Fatalf("oid = %s, want the pushed commit %s, not the local one", oid, pushed)
	}
}

func TestResolveBaseAcceptsABranchName(t *testing.T) {
	dir, head := newRepo(t)
	oid, ref, err := Open(dir).ResolveBase("main")
	if err != nil {
		t.Fatalf("ResolveBase: %v", err)
	}
	if oid != head || !strings.Contains(ref, "origin/main") {
		t.Fatalf("ResolveBase(main) = %s at %q", oid, ref)
	}
}

func TestResolveBaseAcceptsAnExplicitSHA(t *testing.T) {
	dir, head := newRepo(t)
	oid, _, err := Open(dir).ResolveBase(head)
	if err != nil {
		t.Fatalf("ResolveBase: %v", err)
	}
	if oid != head {
		t.Fatalf("oid = %s, want %s", oid, head)
	}
}

func TestResolveBaseReportsAMissingBranch(t *testing.T) {
	dir, _ := newRepo(t)
	_, _, err := Open(dir).ResolveBase("develop")
	if err == nil {
		t.Fatal("a branch the remote does not have should be refused")
	}
	if !strings.Contains(err.Error(), "develop") || !strings.Contains(err.Error(), "push") {
		t.Fatalf("err = %v, want it to name the branch and say to push it", err)
	}
}

func TestNameIsTheCheckoutDirectory(t *testing.T) {
	dir, _ := newRepo(t)
	name, err := Open(dir).Name()
	if err != nil {
		t.Fatalf("Name: %v", err)
	}
	if name != "work" {
		t.Fatalf("Name = %q, want the checkout directory name", name)
	}
}

func TestResolveBaseWithoutARemote(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	dir := t.TempDir()
	cmd := exec.Command("git", "init", "-b", "main", dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	_, _, err := Open(dir).ResolveBase("main")
	if err == nil || !strings.Contains(err.Error(), "remote") {
		t.Fatalf("err = %v, want a checkout with no remote to be reported", err)
	}
}
