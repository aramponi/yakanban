package core

import "strings"

// BranchRule maps a task onto a branch type. Rules are evaluated in order and
// the first whose conditions all hold wins, so the file reads top-down like
// the policy a team would write down anyway.
type BranchRule struct {
	// Tag, Priority and Class are the conditions. An empty one is ignored,
	// and a rule with no condition at all matches everything.
	Tag      string `yaml:"tag,omitempty" json:"tag,omitempty"`
	Priority string `yaml:"priority,omitempty" json:"priority,omitempty"`
	Class    string `yaml:"class,omitempty" json:"class,omitempty"`

	// Type is the branch type the rule assigns, e.g. fix or hotfix.
	Type string `yaml:"type,omitempty" json:"type"`
	// Base overrides where this kind of work branches from — a hotfix starts
	// from production even on a board that otherwise branches off develop.
	Base string `yaml:"base,omitempty" json:"base,omitempty"`
}

// Matches reports whether every condition of the rule holds for a task.
func (r BranchRule) Matches(t Task) bool {
	if r.Tag != "" && !t.HasTag(r.Tag) {
		return false
	}
	if r.Priority != "" && !strings.EqualFold(r.Priority, t.Priority) {
		return false
	}
	if r.Class != "" && !strings.EqualFold(r.Class, t.Class) {
		return false
	}
	return true
}

// BranchPolicy is the board's branching model, resolved from a preset and the
// overrides written next to it.
type BranchPolicy struct {
	// Model names the preset this was resolved from, for diagnostics.
	Model string `json:"model,omitempty"`
	// Base is where new work branches from.
	Base string `json:"base,omitempty"`
	// Integration is where finished work merges back.
	Integration string `json:"integration,omitempty"`
	// DefaultType applies when no rule matches.
	DefaultType string `json:"default_type,omitempty"`
	// Rules assign a branch type to a task, first match wins.
	Rules []BranchRule `json:"rules,omitempty"`
	// DeleteAfterMerge says whether the branch is expected to be removed once
	// merged. yakanban does not delete anything itself; this drives what
	// `init` offers to configure on the backend and what the workflow says.
	DeleteAfterMerge bool `json:"delete_after_merge,omitempty"`
	// BackMergeWarning is set by models where finishing a branch requires a
	// second merge a tool must not attempt on its own.
	BackMergeWarning map[string]string `json:"back_merge_warning,omitempty"`
}

// TypeFor returns the branch type of a task and the base it should start from.
// An empty base means "use the policy's own base".
func (p BranchPolicy) TypeFor(t Task) (typ string, base string) {
	for _, rule := range p.Rules {
		if rule.Matches(t) {
			return rule.Type, rule.Base
		}
	}
	return p.DefaultType, ""
}

// BaseFor returns the branch a task should start from.
func (p BranchPolicy) BaseFor(t Task) string {
	if _, base := p.TypeFor(t); base != "" {
		return base
	}
	return p.Base
}

// BackMergeNote returns the warning attached to a task's branch type, if the
// model has one. Git flow's hotfix has to land in main *and* develop, and a
// silently skipped back-merge loses the fix — so this is said out loud rather
// than automated.
func (p BranchPolicy) BackMergeNote(t Task) string {
	typ, _ := p.TypeFor(t)
	if typ == "" || p.BackMergeWarning == nil {
		return ""
	}
	return p.BackMergeWarning[strings.ToLower(typ)]
}
