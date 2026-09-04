// Package skills embeds the agent skills bundled with this binary.
//
// The go:embed directive can only reach files inside its own package's directory tree, so
// a package under internal/ (the usual home for Go code here) could never
// reach skills/*/SKILL.md at the repository root without a copy — and a copy
// is exactly what would drift out of sync with the prose people actually
// read and edit. Making this tiny package's directory *be* skills/ solves
// that: skills/<name>/SKILL.md stays the single source of truth, edited in
// place, and go:embed reads it directly. `yakanban skill install` (see
// internal/skill) imports this package to get that content into a release
// binary with no checkout required.
package skills

import (
	"embed"
	"fmt"
	"strings"
)

//go:embed yakanban/SKILL.md yakanban-based-development/SKILL.md
var files embed.FS

// Skill is one bundled agent skill, exactly as committed under skills/.
type Skill struct {
	// Name is the directory name under skills/ (and the name it is
	// installed under: <target>/<name>/SKILL.md).
	Name string
	// Content is the raw SKILL.md bytes, unmodified.
	Content []byte
}

// names lists the bundled skills. An explicit list (rather than walking
// files) keeps install order stable and makes an unembedded typo fail at
// Get/All time with a clear message instead of silently vanishing.
var names = []string{"yakanban", "yakanban-based-development"}

// Names returns the bundled skill names, in a stable order.
func Names() []string {
	return append([]string(nil), names...)
}

// All returns every bundled skill, in a stable order.
func All() ([]Skill, error) {
	out := make([]Skill, 0, len(names))
	for _, n := range names {
		s, err := Get(n)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

// Get returns one bundled skill by name.
func Get(name string) (Skill, error) {
	content, err := files.ReadFile(name + "/SKILL.md")
	if err != nil {
		return Skill{}, &UnknownError{Name: name}
	}
	return Skill{Name: name, Content: content}, nil
}

// UnknownError reports a skill name that is not bundled with this binary.
type UnknownError struct{ Name string }

func (e *UnknownError) Error() string {
	return fmt.Sprintf("unknown skill %q (available: %s)", e.Name, strings.Join(names, ", "))
}
