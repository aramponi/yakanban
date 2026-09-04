package core

import (
	"strings"
	"time"
)

// Status describes one column of the board.
type Status struct {
	Name     string `json:"name" yaml:"name"`
	WIPLimit int    `json:"wip_limit,omitempty" yaml:"wip_limit,omitempty"`
	// Terminal marks a column where work is finished (done, archived...).
	// Moving into it stamps Completed.
	Terminal bool `json:"terminal,omitempty" yaml:"terminal,omitempty"`
	// Initial marks the intake column. Moving out of it stamps Started.
	Initial bool `json:"initial,omitempty" yaml:"initial,omitempty"`
	// RequireClaim asks the CLI to refuse an unclaimed task in this column.
	RequireClaim bool `json:"require_claim,omitempty" yaml:"require_claim,omitempty"`
}

// Class is a class of service, borrowed from Kanban practice.
type Class struct {
	Name            string `json:"name" yaml:"name"`
	WIPLimit        int    `json:"wip_limit,omitempty" yaml:"wip_limit,omitempty"`
	BypassColumnWIP bool   `json:"bypass_column_wip,omitempty" yaml:"bypass_column_wip,omitempty"`
}

// BoardInfo is what a provider knows about the board it is backing.
type BoardInfo struct {
	Name       string   `json:"name"`
	Provider   string   `json:"provider"`
	URL        string   `json:"url,omitempty"`
	Statuses   []Status `json:"statuses"`
	Priorities []string `json:"priorities,omitempty"`
	Classes    []Class  `json:"classes,omitempty"`

	Metadata map[string]any `json:"metadata,omitempty"`
}

// StatusNames returns the ordered column names.
func (b BoardInfo) StatusNames() []string {
	out := make([]string, 0, len(b.Statuses))
	for _, s := range b.Statuses {
		out = append(out, s.Name)
	}
	return out
}

// ClassNames returns the configured classes of service.
func (b BoardInfo) ClassNames() []string {
	out := make([]string, 0, len(b.Classes))
	for _, c := range b.Classes {
		out = append(out, c.Name)
	}
	return out
}

// StatusIndex returns the position of name in the column order, or -1.
func (b BoardInfo) StatusIndex(name string) int {
	for i, s := range b.Statuses {
		if strings.EqualFold(s.Name, name) {
			return i
		}
	}
	return -1
}

// Status resolves a column by name, case-insensitively.
func (b BoardInfo) Status(name string) (Status, bool) {
	if i := b.StatusIndex(name); i >= 0 {
		return b.Statuses[i], true
	}
	return Status{}, false
}

// IsTerminal reports whether name is a terminal column.
func (b BoardInfo) IsTerminal(name string) bool {
	s, ok := b.Status(name)
	return ok && s.Terminal
}

// IsInitial reports whether name is the intake column.
func (b BoardInfo) IsInitial(name string) bool {
	s, ok := b.Status(name)
	return ok && s.Initial
}

// Column aggregates the tasks of one status for board rendering.
type Column struct {
	Status  Status   `json:"status"`
	Count   int      `json:"count"`
	Blocked int      `json:"blocked"`
	Claimed int      `json:"claimed"`
	OverWIP bool     `json:"over_wip"`
	TaskIDs []string `json:"task_ids,omitempty"`
}

// Summary is the aggregate view rendered by the board command.
type Summary struct {
	Board       BoardInfo      `json:"board"`
	Columns     []Column       `json:"columns"`
	Total       int            `json:"total"`
	Blocked     int            `json:"blocked"`
	Overdue     int            `json:"overdue"`
	Priorities  map[string]int `json:"priorities,omitempty"`
	GeneratedAt time.Time      `json:"generated_at"`
}

// Summarize folds a task list into a board summary.
func Summarize(board BoardInfo, tasks []Task, now time.Time) Summary {
	s := Summary{Board: board, Priorities: map[string]int{}, GeneratedAt: now}
	byStatus := map[string]*Column{}
	for i := range board.Statuses {
		st := board.Statuses[i]
		col := &Column{Status: st}
		byStatus[strings.ToLower(st.Name)] = col
	}
	for _, t := range tasks {
		s.Total++
		if t.Priority != "" {
			s.Priorities[t.Priority]++
		}
		if t.IsBlocked() {
			s.Blocked++
		}
		if t.Due != nil && t.Due.Before(now) && !board.IsTerminal(t.Status) {
			s.Overdue++
		}
		col, ok := byStatus[strings.ToLower(t.Status)]
		if !ok {
			col = &Column{Status: Status{Name: t.Status}}
			byStatus[strings.ToLower(t.Status)] = col
			board.Statuses = append(board.Statuses, col.Status)
		}
		col.Count++
		col.TaskIDs = append(col.TaskIDs, t.ID)
		if t.IsBlocked() {
			col.Blocked++
		}
		if t.Claim.Active(now) {
			col.Claimed++
		}
	}
	for _, st := range board.Statuses {
		col := byStatus[strings.ToLower(st.Name)]
		if col == nil {
			continue
		}
		col.OverWIP = col.Status.WIPLimit > 0 && col.Count > col.Status.WIPLimit
		s.Columns = append(s.Columns, *col)
	}
	return s
}
