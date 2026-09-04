package core

import (
	"strings"
	"time"
)

// Filter narrows a task listing. Providers may push part of it down to the
// backend; whatever they cannot express is applied locally by Match.
type Filter struct {
	Statuses   []string
	Priorities []string
	Classes    []string
	Tags       []string
	Assignee   string
	ClaimedBy  string
	Parent     string
	Search     string

	Blocked    bool
	NotBlocked bool
	Unclaimed  bool
	Unblocked  bool // all dependencies satisfied
	Archived   bool

	Limit int
}

// Match reports whether a task satisfies the locally-evaluated part of the
// filter. terminal reports whether a status is a terminal column, and known
// reports whether a dependency ID exists on the board (missing dependencies
// are treated as satisfied, like kanban-md does).
func (f Filter) Match(t Task, now time.Time, terminal func(status string) bool, statusOf func(id string) (string, bool)) bool {
	if len(f.Statuses) > 0 && !containsFold(f.Statuses, t.Status) {
		return false
	}
	if len(f.Priorities) > 0 && !containsFold(f.Priorities, t.Priority) {
		return false
	}
	if len(f.Classes) > 0 && !containsFold(f.Classes, t.Class) {
		return false
	}
	for _, tag := range f.Tags {
		if !t.HasTag(tag) {
			return false
		}
	}
	if f.Assignee != "" && !t.HasAssignee(f.Assignee) {
		return false
	}
	if f.ClaimedBy != "" && (!t.Claim.Active(now) || !strings.EqualFold(t.Claim.Agent, f.ClaimedBy)) {
		return false
	}
	if f.Parent != "" && t.Parent != f.Parent {
		return false
	}
	if f.Blocked && !t.IsBlocked() {
		return false
	}
	if f.NotBlocked && t.IsBlocked() {
		return false
	}
	if f.Unclaimed && t.Claim.Active(now) {
		return false
	}
	if f.Unblocked {
		for _, dep := range t.DependsOn {
			st, ok := statusOf(dep)
			if ok && !terminal(st) {
				return false
			}
		}
	}
	if f.Search != "" {
		needle := strings.ToLower(f.Search)
		hay := strings.ToLower(t.Title + "\n" + t.Body + "\n" + strings.Join(t.Tags, " "))
		if !strings.Contains(hay, needle) {
			return false
		}
	}
	return true
}

func containsFold(list []string, v string) bool {
	for _, x := range list {
		if strings.EqualFold(strings.TrimSpace(x), v) {
			return true
		}
	}
	return false
}
