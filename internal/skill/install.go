package skill

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/aramponi/yakanban/internal/core"
	bundled "github.com/aramponi/yakanban/skills"
)

// Target is one destination this package writes to or reads from: a bundled
// skill landing in one agent's directory, or an explicit --path directory
// (Agent is "" there — --path skips agent detection entirely).
type Target struct {
	Skill string `json:"skill"`
	Agent Agent  `json:"agent,omitempty"`
	Path  string `json:"path"`
}

// Action records what Write did to one Target.
type Action string

// Possible outcomes of Write.
const (
	ActionWrote        Action = "wrote"
	ActionUpToDate     Action = "up to date"
	ActionUpdated      Action = "updated"
	ActionSkipped      Action = "skipped: modified locally, use --force"
	ActionNotInstalled Action = "not installed"
)

// Result is the outcome of installing or updating one Target.
type Result struct {
	Target
	Action Action `json:"action"`
}

// Write installs or refreshes one bundled skill at target.Path, stamping it
// with version (normally version.String()).
//
// create controls what happens when nothing is installed yet: `install`
// passes true (write a new file); `update` passes false, so a skill that was
// never installed is left alone rather than created as a side effect.
//
// force overwrites a locally modified file; without it, Write only ever
// touches a file that does not exist yet or is byte-identical to what it
// would itself have written at some earlier version (see unmodified in
// marker.go) — never one a person has actually edited.
func Write(target Target, version string, force, create bool) (Result, error) {
	sk, err := bundled.Get(target.Skill)
	if err != nil {
		return Result{}, fmt.Errorf("%w: %v", core.ErrInvalidInput, err)
	}
	desired := stamp(version, sk.Content)

	existing, err := os.ReadFile(target.Path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		if !create {
			return Result{Target: target, Action: ActionNotInstalled}, nil
		}
		if err := os.MkdirAll(filepath.Dir(target.Path), 0o755); err != nil {
			return Result{}, err
		}
		if err := os.WriteFile(target.Path, desired, 0o644); err != nil {
			return Result{}, err
		}
		return Result{Target: target, Action: ActionWrote}, nil
	case err != nil:
		return Result{}, err
	}

	if bytes.Equal(existing, desired) {
		return Result{Target: target, Action: ActionUpToDate}, nil
	}
	if !force && !unmodified(existing) {
		return Result{Target: target, Action: ActionSkipped}, nil
	}
	if err := os.WriteFile(target.Path, desired, 0o644); err != nil {
		return Result{}, err
	}
	return Result{Target: target, Action: ActionUpdated}, nil
}
