package skill

import (
	"errors"
	"os"

	bundled "github.com/aramponi/yakanban/skills"
)

// State is what `check` found at one target.
type State string

// The states a target can be in.
const (
	// StateCurrent means the file on disk is exactly what this binary carries.
	StateCurrent State = "current"
	// StateStale means it was installed by another version and untouched
	// since, so `update` can refresh it safely.
	StateStale State = "stale"
	// StateModified means somebody edited it; refreshing needs --force.
	StateModified State = "modified"
	// StateMissing means nothing is installed there.
	StateMissing State = "missing"
)

// Status is the outcome of checking one target.
type Status struct {
	Target
	State State `json:"state"`
	// Installed is the version stamped in the file, empty when unknown.
	Installed string `json:"installed,omitempty"`
	// Expected is the version this binary carries.
	Expected string `json:"expected"`
}

// Stale reports whether the target needs attention. A missing skill does not
// count: `check` judges what is installed, and gating CI on skills nobody
// installed would fail every fresh clone.
func (s Status) Stale() bool { return s.State == StateStale || s.State == StateModified }

// Check compares what is on disk with what this binary carries.
func Check(targets []Target, version string) ([]Status, error) {
	out := make([]Status, 0, len(targets))
	for _, t := range targets {
		status := Status{Target: t, Expected: version}
		sk, err := bundled.Get(t.Skill)
		if err != nil {
			return nil, err
		}
		content, err := os.ReadFile(t.Path)
		switch {
		case errors.Is(err, os.ErrNotExist):
			status.State = StateMissing
		case err != nil:
			return nil, err
		default:
			if installed, ok := installedVersion(content); ok {
				status.Installed = installed
			}
			switch {
			case string(content) == string(stamp(version, sk.Content)):
				status.State = StateCurrent
			case unmodified(content):
				// It matches a marker yakanban wrote, just not this version.
				status.State = StateStale
			default:
				// No marker, an unparseable one, or prose that has changed:
				// unknown provenance, never overwritten without --force.
				status.State = StateModified
			}
		}
		out = append(out, status)
	}
	return out, nil
}
