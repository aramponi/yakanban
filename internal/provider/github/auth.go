// Package github implements the yakanban provider backed by GitHub Issues
// and GitHub Projects v2.
//
// Issues carry the content (title, body, labels, assignees); a Projects v2
// board carries the workflow state (status column, priority, class, claim,
// dependencies). Non-technical users work in the GitHub web UI, agents work
// through this adapter, and GitHub remains the single source of truth.
package github

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// TokenSource explains where a token came from, for diagnostics.
type TokenSource struct {
	Token  string
	Origin string
}

// ResolveToken finds a GitHub token, preferring an explicit environment
// variable and falling back to the `gh` CLI the user has already set up.
func ResolveToken(host string) (TokenSource, error) {
	for _, env := range []string{"YAKANBAN_GITHUB_TOKEN", "GH_TOKEN", "GITHUB_TOKEN"} {
		if v := strings.TrimSpace(os.Getenv(env)); v != "" {
			return TokenSource{Token: v, Origin: "$" + env}, nil
		}
	}
	gh, err := exec.LookPath("gh")
	if err != nil {
		return TokenSource{}, fmt.Errorf("%w: set GH_TOKEN, or install the GitHub CLI and run `gh auth login`", errAuth)
	}
	args := []string{"auth", "token"}
	if host != "" && host != defaultHost {
		args = append(args, "--hostname", host)
	}
	out, err := exec.Command(gh, args...).Output()
	if err != nil {
		var stderr string
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = strings.TrimSpace(string(ee.Stderr))
		}
		if stderr == "" {
			stderr = err.Error()
		}
		return TokenSource{}, fmt.Errorf("%w: `gh auth token` failed: %s (run `gh auth login`)", errAuth, stderr)
	}
	token := strings.TrimSpace(string(out))
	if token == "" {
		return TokenSource{}, fmt.Errorf("%w: `gh auth token` returned nothing (run `gh auth login`)", errAuth)
	}
	return TokenSource{Token: token, Origin: "gh auth token"}, nil
}
