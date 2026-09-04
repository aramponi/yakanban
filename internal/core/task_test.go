package core

import (
	"testing"
	"time"
)

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("parsing %q: %v", s, err)
	}
	return v
}

func TestClaimActive(t *testing.T) {
	now := mustTime(t, "2026-09-04T12:00:00Z")
	cases := []struct {
		name  string
		claim *Claim
		want  bool
	}{
		{"nil claim", nil, false},
		{"no agent", &Claim{Expires: now.Add(time.Hour)}, false},
		{"expired", &Claim{Agent: "frost-maple", Expires: now.Add(-time.Minute)}, false},
		{"held", &Claim{Agent: "frost-maple", Expires: now.Add(time.Minute)}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.claim.Active(now); got != tc.want {
				t.Fatalf("Active() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSortTasksUsesBoardOrder(t *testing.T) {
	statuses := []string{"Backlog", "Todo", "Done"}
	priorities := []string{"low", "medium", "high", "critical"}
	tasks := []Task{
		{ID: "10", Status: "Done", Priority: "low"},
		{ID: "9", Status: "Backlog", Priority: "critical"},
		{ID: "2", Status: "Todo", Priority: "medium"},
	}

	SortTasks(tasks, "priority", false, statuses, priorities)
	if tasks[0].Priority != "low" || tasks[2].Priority != "critical" {
		t.Fatalf("priority sort ignored the board order: %v", ids(tasks))
	}

	SortTasks(tasks, "status", false, statuses, priorities)
	if tasks[0].Status != "Backlog" || tasks[2].Status != "Done" {
		t.Fatalf("status sort ignored the column order: %v", ids(tasks))
	}

	SortTasks(tasks, "id", false, statuses, priorities)
	if got := ids(tasks); got[0] != "2" || got[1] != "9" || got[2] != "10" {
		t.Fatalf("id sort is not numeric: %v", got)
	}

	SortTasks(tasks, "id", true, statuses, priorities)
	if ids(tasks)[0] != "10" {
		t.Fatalf("reverse sort had no effect: %v", ids(tasks))
	}
}

func ids(tasks []Task) []string {
	out := make([]string, 0, len(tasks))
	for _, t := range tasks {
		out = append(out, t.ID)
	}
	return out
}

func TestFilterMatch(t *testing.T) {
	now := mustTime(t, "2026-09-04T12:00:00Z")
	terminal := func(s string) bool { return s == "Done" }
	board := map[string]string{"1": "Done", "2": "Todo"}
	statusOf := func(id string) (string, bool) {
		s, ok := board[id]
		return s, ok
	}
	task := Task{
		ID:        "7",
		Title:     "Fix the login redirect",
		Status:    "Todo",
		Priority:  "high",
		Tags:      []string{"bug", "auth"},
		Assignees: []string{"Alice"},
		DependsOn: []string{"1"},
		Claim:     &Claim{Agent: "frost-maple", Expires: now.Add(time.Hour)},
	}

	cases := []struct {
		name   string
		filter Filter
		want   bool
	}{
		{"status hit", Filter{Statuses: []string{"todo"}}, true},
		{"status miss", Filter{Statuses: []string{"done"}}, false},
		{"tag hit is case-insensitive", Filter{Tags: []string{"BUG"}}, true},
		{"all tags must match", Filter{Tags: []string{"bug", "ui"}}, false},
		{"assignee is case-insensitive", Filter{Assignee: "alice"}, true},
		{"claimed-by hit", Filter{ClaimedBy: "frost-maple"}, true},
		{"claimed-by miss", Filter{ClaimedBy: "tidal-vale"}, false},
		{"unclaimed excludes a held task", Filter{Unclaimed: true}, false},
		{"unblocked with a done dependency", Filter{Unblocked: true}, true},
		{"search hits the title", Filter{Search: "LOGIN"}, true},
		{"search miss", Filter{Search: "logout"}, false},
		{"blocked filter on an unblocked task", Filter{Blocked: true}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.filter.Match(task, now, terminal, statusOf); got != tc.want {
				t.Fatalf("Match() = %v, want %v", got, tc.want)
			}
		})
	}

	t.Run("unblocked with a pending dependency", func(t *testing.T) {
		pending := task
		pending.DependsOn = []string{"2"}
		if (Filter{Unblocked: true}).Match(pending, now, terminal, statusOf) {
			t.Fatal("a task depending on an open task should not be listed as unblocked")
		}
	})

	t.Run("a missing dependency counts as satisfied", func(t *testing.T) {
		orphan := task
		orphan.DependsOn = []string{"404"}
		if !(Filter{Unblocked: true}).Match(orphan, now, terminal, statusOf) {
			t.Fatal("an unknown dependency should not block the task")
		}
	})
}

func TestSummarize(t *testing.T) {
	now := mustTime(t, "2026-09-04T12:00:00Z")
	yesterday := now.Add(-24 * time.Hour)
	board := BoardInfo{
		Name: "demo",
		Statuses: []Status{
			{Name: "Todo"},
			{Name: "In Progress", WIPLimit: 1},
			{Name: "Done", Terminal: true},
		},
		Priorities: []string{"low", "high"},
	}
	tasks := []Task{
		{ID: "1", Status: "Todo", Priority: "high", Due: &yesterday},
		{ID: "2", Status: "In Progress", Priority: "low", Blocked: "waiting on review"},
		{ID: "3", Status: "In Progress", Priority: "high", Claim: &Claim{Agent: "a", Expires: now.Add(time.Hour)}},
		{ID: "4", Status: "Done", Priority: "low", Due: &yesterday},
	}

	s := Summarize(board, tasks, now)

	if s.Total != 4 {
		t.Fatalf("Total = %d, want 4", s.Total)
	}
	if s.Blocked != 1 {
		t.Fatalf("Blocked = %d, want 1", s.Blocked)
	}
	if s.Overdue != 1 {
		t.Fatalf("Overdue = %d, want 1 (a past due date in a terminal column is not overdue)", s.Overdue)
	}
	inProgress := s.Columns[1]
	if inProgress.Count != 2 || !inProgress.OverWIP {
		t.Fatalf("In Progress = %+v, want 2 tasks over its WIP limit of 1", inProgress)
	}
	if inProgress.Claimed != 1 || inProgress.Blocked != 1 {
		t.Fatalf("In Progress claimed/blocked = %d/%d, want 1/1", inProgress.Claimed, inProgress.Blocked)
	}
	if s.Priorities["high"] != 2 {
		t.Fatalf("priority histogram = %v", s.Priorities)
	}
}

func TestSummarizeKeepsUnknownColumns(t *testing.T) {
	now := time.Now()
	board := BoardInfo{Statuses: []Status{{Name: "Todo"}}}
	s := Summarize(board, []Task{{ID: "1", Status: "Blocked by legal"}}, now)
	if len(s.Columns) != 2 {
		t.Fatalf("got %d columns, want the unknown one to be kept: %+v", len(s.Columns), s.Columns)
	}
}
