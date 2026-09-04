// Package gitrepo asks the local git checkout the few questions yakanban
// needs answered before it can create a branch on the backend.
//
// It shells out to git rather than linking a git library: the answers must
// match exactly what the user's own git says, including their remote and
// worktree configuration.
package gitrepo

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/aramponi/yakanban/internal/core"
)

// Repo is a local checkout.
type Repo struct {
	dir string
}

// Open returns a handle on the checkout containing dir.
func Open(dir string) *Repo { return &Repo{dir: dir} }

func (r *Repo) git(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = r.dir
	out, err := cmd.Output()
	if err != nil {
		stderr := ""
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = strings.TrimSpace(string(ee.Stderr))
		}
		if stderr == "" {
			stderr = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), stderr)
	}
	return strings.TrimSpace(string(out)), nil
}

// Name returns the repository name, used by the worktree template.
func (r *Repo) Name() (string, error) {
	top, err := r.git("rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	parts := strings.Split(strings.TrimSuffix(top, "/"), "/")
	return parts[len(parts)-1], nil
}

// ResolveBase turns a user-supplied base — a branch name, a remote-tracking
// ref, a SHA, or nothing at all — into a commit the remote already has.
//
// Resolving against a remote-tracking ref rather than the local branch is the
// whole point: a local HEAD that has not been pushed does not exist for the
// backend, and the linked-branch mutation would fail with an error that says
// nothing useful.
func (r *Repo) ResolveBase(ref string) (oid string, resolved string, err error) {
	if ref == "" {
		return r.defaultBase()
	}
	// An explicit remote-tracking ref, or a raw SHA, is taken as given.
	if oid, err := r.git("rev-parse", "--verify", "--quiet", ref+"^{commit}"); err == nil {
		if isRemoteRef(ref) || looksLikeSHA(ref) {
			return oid, ref, nil
		}
	}
	// A plain branch name means "that branch as the remote has it".
	remote, err := r.defaultRemote()
	if err != nil {
		return "", "", err
	}
	tracking := remote + "/" + ref
	oid, err = r.git("rev-parse", "--verify", "--quiet", tracking+"^{commit}")
	if err != nil {
		return "", "", fmt.Errorf("%w: %s does not exist on %s (push it first, or run `git fetch %s`)",
			core.ErrInvalidInput, ref, remote, remote)
	}
	return oid, tracking, nil
}

// defaultBase resolves the upstream of the current branch, falling back to the
// remote's default branch.
func (r *Repo) defaultBase() (oid string, resolved string, err error) {
	if upstream, err := r.git("rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}"); err == nil {
		if oid, err := r.git("rev-parse", "--verify", "--quiet", upstream+"^{commit}"); err == nil {
			return oid, upstream, nil
		}
	}
	remote, err := r.defaultRemote()
	if err != nil {
		return "", "", err
	}
	head, err := r.git("symbolic-ref", "--short", "refs/remotes/"+remote+"/HEAD")
	if err != nil {
		return "", "", fmt.Errorf("%w: cannot tell which branch to start from; pass --from BRANCH (git said: %v)",
			core.ErrInvalidInput, err)
	}
	oid, err = r.git("rev-parse", "--verify", "--quiet", head+"^{commit}")
	if err != nil {
		return "", "", err
	}
	return oid, head, nil
}

// defaultRemote prefers origin, and otherwise takes the only remote there is.
func (r *Repo) defaultRemote() (string, error) {
	out, err := r.git("remote")
	if err != nil {
		return "", err
	}
	remotes := strings.Fields(out)
	switch {
	case len(remotes) == 0:
		return "", fmt.Errorf("%w: this checkout has no git remote", core.ErrInvalidInput)
	case len(remotes) == 1:
		return remotes[0], nil
	}
	for _, name := range remotes {
		if name == "origin" {
			return "origin", nil
		}
	}
	return "", fmt.Errorf("%w: several remotes (%s) and none called origin; pass --from <remote>/<branch>",
		core.ErrInvalidInput, strings.Join(remotes, ", "))
}

func isRemoteRef(ref string) bool {
	return strings.Contains(ref, "/") && !strings.HasPrefix(ref, "refs/heads/")
}

// looksLikeSHA reports whether ref is an abbreviated or full object name.
func looksLikeSHA(ref string) bool {
	if len(ref) < 7 || len(ref) > 40 {
		return false
	}
	for _, c := range ref {
		if !strings.ContainsRune("0123456789abcdefABCDEF", c) {
			return false
		}
	}
	return true
}
