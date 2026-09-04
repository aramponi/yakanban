// Package core holds the provider-agnostic domain model of yakanban.
//
// Nothing in this package imports an adapter: it only defines what a task,
// a board and a filter are, plus the ports that adapters must implement.
package core

import (
	"sort"
	"strings"
	"time"
)

// Task is the unified representation of a ticket, whatever the backend.
//
// Fields that a given provider cannot express are simply left at their zero
// value; anything provider-specific goes into Metadata rather than growing
// this struct.
type Task struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Body     string `json:"body,omitempty"`
	Status   string `json:"status"`
	Priority string `json:"priority,omitempty"`
	Class    string `json:"class,omitempty"`

	Tags      []string `json:"tags,omitempty"`
	Assignees []string `json:"assignees,omitempty"`
	Estimate  string   `json:"estimate,omitempty"`

	Due       *time.Time `json:"due,omitempty"`
	Started   *time.Time `json:"started,omitempty"`
	Completed *time.Time `json:"completed,omitempty"`
	Created   time.Time  `json:"created"`
	Updated   time.Time  `json:"updated"`

	Parent    string   `json:"parent,omitempty"`
	DependsOn []string `json:"depends_on,omitempty"`

	// Blocked holds the reason a task is blocked. Empty means not blocked.
	Blocked string `json:"blocked,omitempty"`

	Claim *Claim `json:"claim,omitempty"`

	URL      string         `json:"url,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// Claim is a soft, expiring lock taken by an agent working on a task.
// GitHub has no native equivalent, so providers persist it however they can.
type Claim struct {
	Agent   string    `json:"agent"`
	Expires time.Time `json:"expires"`
}

// Active reports whether the claim still holds at time now.
func (c *Claim) Active(now time.Time) bool {
	return c != nil && c.Agent != "" && c.Expires.After(now)
}

// IsBlocked reports whether the task carries a block reason.
func (t Task) IsBlocked() bool { return strings.TrimSpace(t.Blocked) != "" }

// HasTag reports whether the task carries tag, case-insensitively.
func (t Task) HasTag(tag string) bool {
	for _, x := range t.Tags {
		if strings.EqualFold(x, tag) {
			return true
		}
	}
	return false
}

// HasAssignee reports whether login is among the task assignees.
func (t Task) HasAssignee(login string) bool {
	for _, x := range t.Assignees {
		if strings.EqualFold(x, login) {
			return true
		}
	}
	return false
}

// Age returns how long ago the task was last updated.
func (t Task) Age(now time.Time) time.Duration { return now.Sub(t.Updated) }

// Draft is the input of a task creation. Empty fields fall back to the
// board defaults resolved by the caller, not by the provider.
type Draft struct {
	Title     string
	Body      string
	Status    string
	Priority  string
	Class     string
	Tags      []string
	Assignees []string
	Estimate  string
	Due       *time.Time
	Parent    string
	DependsOn []string
	Claim     *Claim
	Metadata  map[string]any
}

// Patch describes a partial update. Pointer fields distinguish "leave alone"
// (nil) from "set to this value" (non-nil), and the Clear* flags express
// "unset this field" without overloading the zero value.
type Patch struct {
	Title      *string
	Body       *string
	AppendBody *string
	Status     *string
	Priority   *string
	Class      *string
	Estimate   *string

	Assignees  *[]string
	AddTags    []string
	RemoveTags []string
	AddDeps    []string
	RemoveDeps []string
	ClearDeps  bool

	Due            *time.Time
	ClearDue       bool
	Started        *time.Time
	ClearStarted   bool
	Completed      *time.Time
	ClearCompleted bool

	Parent      *string
	ClearParent bool

	// Blocked set to a non-empty string blocks the task; set to "" unblocks it.
	Blocked *string

	Claim        *Claim
	ReleaseClaim bool

	Metadata map[string]any
}

// IsEmpty reports whether the patch would change nothing.
func (p Patch) IsEmpty() bool {
	return p.Title == nil && p.Body == nil && p.AppendBody == nil && p.Status == nil &&
		p.Priority == nil && p.Class == nil && p.Estimate == nil && p.Assignees == nil &&
		len(p.AddTags) == 0 && len(p.RemoveTags) == 0 && len(p.AddDeps) == 0 &&
		len(p.RemoveDeps) == 0 && !p.ClearDeps && p.Due == nil && !p.ClearDue &&
		p.Started == nil && !p.ClearStarted && p.Completed == nil && !p.ClearCompleted &&
		p.Parent == nil && !p.ClearParent && p.Blocked == nil && p.Claim == nil &&
		!p.ReleaseClaim && len(p.Metadata) == 0
}

// SortTasks orders tasks in place by field, honouring the board's own
// status and priority ordering rather than alphabetical order.
func SortTasks(tasks []Task, field string, reverse bool, statuses, priorities []string) {
	rank := func(list []string, v string) int {
		for i, x := range list {
			if strings.EqualFold(x, v) {
				return i
			}
		}
		return len(list)
	}
	less := func(i, j int) bool { return tasks[i].ID < tasks[j].ID }
	switch strings.ToLower(field) {
	case "", "id":
		less = func(i, j int) bool { return NaturalLess(tasks[i].ID, tasks[j].ID) }
	case "title":
		less = func(i, j int) bool { return tasks[i].Title < tasks[j].Title }
	case "status":
		less = func(i, j int) bool {
			return rank(statuses, tasks[i].Status) < rank(statuses, tasks[j].Status)
		}
	case "priority":
		less = func(i, j int) bool {
			return rank(priorities, tasks[i].Priority) < rank(priorities, tasks[j].Priority)
		}
	case "created":
		less = func(i, j int) bool { return tasks[i].Created.Before(tasks[j].Created) }
	case "updated":
		less = func(i, j int) bool { return tasks[i].Updated.Before(tasks[j].Updated) }
	case "due":
		less = func(i, j int) bool {
			switch {
			case tasks[i].Due == nil:
				return false
			case tasks[j].Due == nil:
				return true
			default:
				return tasks[i].Due.Before(*tasks[j].Due)
			}
		}
	}
	sort.SliceStable(tasks, func(i, j int) bool {
		if reverse {
			return less(j, i)
		}
		return less(i, j)
	})
}

// NaturalLess compares IDs so that "9" sorts before "10" when both are
// numeric, and falls back to lexicographic order otherwise.
func NaturalLess(a, b string) bool {
	ai, aerr := parseInt(a)
	bi, berr := parseInt(b)
	if aerr == nil && berr == nil {
		return ai < bi
	}
	return a < b
}
