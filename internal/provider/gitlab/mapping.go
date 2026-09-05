package gitlab

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/aramponi/yakanban/internal/core"
)

type user struct {
	ID       int    `json:"id"`
	Username string `json:"username"`
}
type timeStats struct {
	Estimate      int    `json:"time_estimate"`
	Spent         int    `json:"total_time_spent"`
	HumanEstimate string `json:"human_time_estimate"`
}
type issue struct {
	ID           int        `json:"id"`
	IID          int        `json:"iid"`
	ProjectID    int        `json:"project_id"`
	Title        string     `json:"title"`
	Description  string     `json:"description"`
	State        string     `json:"state"`
	WebURL       string     `json:"web_url"`
	Labels       []string   `json:"labels"`
	Assignees    []user     `json:"assignees"`
	DueDate      string     `json:"due_date"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	ClosedAt     *time.Time `json:"closed_at"`
	TimeStats    timeStats  `json:"time_stats"`
	Confidential bool       `json:"confidential"`
	Weight       *int       `json:"weight"`
	Epic         any        `json:"epic"`
}
type issueLink struct {
	ID        int    `json:"id"`
	IID       int    `json:"iid"`
	ProjectID int    `json:"project_id"`
	LinkID    int    `json:"issue_link_id"`
	Type      string `json:"link_type"`
}

func (s *schema) task(i issue) (*core.Task, error) {
	if i.IID <= 0 || i.ID <= 0 || i.ProjectID != s.Project.ID {
		return nil, fmt.Errorf("GitLab returned an invalid issue identity")
	}
	t := &core.Task{ID: strconv.Itoa(i.IID), Title: i.Title, Body: i.Description, URL: i.WebURL, Created: i.CreatedAt, Updated: i.UpdatedAt, Completed: i.ClosedAt, Estimate: i.TimeStats.HumanEstimate,
		Metadata: map[string]any{"issue_id": i.ID, "project_id": i.ProjectID, "state": i.State, "confidential": i.Confidential, "time_stats": i.TimeStats, "weight": i.Weight, "epic": i.Epic}}
	if i.TimeStats.Estimate > 0 && t.Estimate == "" {
		t.Estimate = strconv.Itoa(i.TimeStats.Estimate) + "s"
	}
	if i.DueDate != "" {
		date, err := time.Parse("2006-01-02", i.DueDate)
		if err != nil {
			return nil, fmt.Errorf("GitLab issue #%d has an invalid due_date: %w", i.IID, err)
		}
		t.Due = &date
	}
	for _, a := range i.Assignees {
		t.Assignees = append(t.Assignees, a.Username)
	}
	var statuses, priorities, classes []string
	for _, name := range i.Labels {
		if st, ok := s.Info.Status(name); ok && !st.Initial && !st.Terminal {
			statuses = append(statuses, name)
			continue
		}
		if v, ok := strings.CutPrefix(name, "priority::"); ok {
			priorities = append(priorities, v)
			continue
		}
		if v, ok := strings.CutPrefix(name, "class::"); ok {
			classes = append(classes, v)
			continue
		}
		t.Tags = append(t.Tags, name)
	}
	for _, field := range []struct {
		name   string
		values []string
	}{{"status", statuses}, {"priority", priorities}, {"class", classes}} {
		if len(field.values) > 1 {
			return nil, fmt.Errorf("%w: GitLab issue #%d has conflicting %s labels: %s; remove the conflict in GitLab", core.ErrInvalidInput, i.IID, field.name, strings.Join(field.values, ", "))
		}
	}
	t.Status = "Open"
	if len(statuses) == 1 {
		t.Status = statuses[0]
	}
	switch i.State {
	case "closed":
		t.Status = "Closed"
	case "opened":
	default:
		return nil, fmt.Errorf("GitLab issue #%d has unknown state %q", i.IID, i.State)
	}
	if len(priorities) == 1 {
		t.Priority = priorities[0]
	}
	if len(classes) == 1 {
		t.Class = classes[0]
	}
	return t, nil
}

func issueID(id string) (string, error) {
	n, err := strconv.Atoi(strings.TrimPrefix(strings.TrimSpace(id), "#"))
	if err != nil || n <= 0 {
		return "", fmt.Errorf("%w: %q is not a GitLab issue IID", core.ErrInvalidInput, id)
	}
	return strconv.Itoa(n), nil
}
