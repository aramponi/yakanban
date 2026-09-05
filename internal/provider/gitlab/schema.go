package gitlab

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/aramponi/yakanban/internal/core"
)

type access struct {
	AccessLevel int `json:"access_level"`
}
type project struct {
	ID            int    `json:"id"`
	Name          string `json:"name"`
	WebURL        string `json:"web_url"`
	Archived      bool   `json:"archived"`
	IssuesEnabled bool   `json:"issues_enabled"`
	Namespace     struct {
		ID int `json:"id"`
	} `json:"namespace"`
	Permissions struct {
		Project *access `json:"project_access"`
		Group   *access `json:"group_access"`
	} `json:"permissions"`
}
type label struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Priority *int   `json:"priority"`
	Archived bool   `json:"archived"`
}
type boardList struct {
	ID            int    `json:"id"`
	Label         *label `json:"label"`
	Position      int    `json:"position"`
	MaxIssueCount int    `json:"max_issue_count"`
}
type board struct {
	ID               int         `json:"id"`
	Name             string      `json:"name"`
	Lists            []boardList `json:"lists"`
	HideOpen         bool        `json:"hide_backlog_list"`
	HideClosed       bool        `json:"hide_closed_list"`
	Milestone        any         `json:"milestone"`
	Assignee         any         `json:"assignee"`
	Labels           []label     `json:"labels"`
	Weight           any         `json:"weight"`
	Iteration        any         `json:"iteration"`
	IterationCadence any         `json:"iteration_cadence"`
}
type schema struct {
	Project    project
	Board      board
	Labels     []label
	Plan       string
	PlanReason string
	Info       core.BoardInfo
}

func (p *Provider) projectPath() string { return "/projects/" + url.PathEscape(p.settings.Project) }

func (p *Provider) load(ctx context.Context) (*schema, error) {
	if p.schema != nil {
		return p.schema, nil
	}
	key := "gitlab:schema:v1:" + p.settings.Host + ":" + p.settings.Project + ":" + strconv.Itoa(p.settings.BoardID)
	var s schema
	if _, ok := p.store.Get(key, &s); ok {
		p.schema = &s
		return &s, nil
	}
	if _, err := p.client.request(ctx, "GET", p.projectPath(), nil, &s.Project); err != nil {
		return nil, err
	}
	if s.Project.ID == 0 {
		return nil, fmt.Errorf("GitLab returned no project ID")
	}
	if p.settings.BoardID <= 0 {
		return nil, fmt.Errorf("%w: set providers.gitlab.board_id or run yakanban init", core.ErrNotConfigured)
	}
	path := p.projectPath() + "/boards/" + strconv.Itoa(p.settings.BoardID)
	if _, err := p.client.request(ctx, "GET", path, nil, &s.Board); err != nil {
		return nil, err
	}
	if s.Board.ID != p.settings.BoardID {
		return nil, fmt.Errorf("GitLab returned an unexpected board ID")
	}
	var err error
	s.Labels, err = pages[label](ctx, p.client, p.projectPath()+"/labels")
	if err != nil {
		return nil, err
	}
	s.Plan = "unknown"
	var namespace struct {
		Plan string `json:"plan"`
	}
	_, err = p.client.request(ctx, "GET", "/namespaces/"+strconv.Itoa(s.Project.Namespace.ID), nil, &namespace)
	if err != nil {
		var api *apiError
		if !errors.As(err, &api) || (api.status != 403 && api.status != 404) {
			return nil, err
		}
		s.PlanReason = "namespace subscription is not visible to this caller"
	} else if namespace.Plan != "" {
		s.Plan = strings.ToLower(namespace.Plan)
	} else {
		s.PlanReason = "namespace API did not disclose a subscription"
	}
	if err := s.mapBoard(p.settings.Host); err != nil {
		return nil, err
	}
	_ = p.store.Put(key, &s)
	p.schema = &s
	return &s, nil
}

func (s *schema) paid() bool {
	return s.Plan == "premium" || s.Plan == "ultimate" || s.Plan == "silver" || s.Plan == "gold"
}

func (s *schema) capabilities() core.CapabilitySet {
	caps := core.CapabilitySet{Supported: core.CapEstimate | core.CapClass | core.CapDueDate, Reasons: map[core.Capability]string{
		core.CapClaims:        "GitLab has no selected native storage for an agent claim and expiry (all tiers)",
		core.CapParent:        "GitLab work-item hierarchy is outside this issue-only REST mapping (all tiers)",
		core.CapBlocked:       "GitLab labels cannot store a blocking reason; no hidden issue-body metadata is used (all tiers)",
		core.CapArchive:       "GitLab has no per-issue archive in this mapping; delete permanently removes the issue",
		core.CapLinkedBranch:  "GitLab linked-branch operations are outside this issue-board adapter (all tiers)",
		core.CapWorkflowDates: "GitLab owns closed_at and has no writable Started field; explicit workflow date edits are unsupported (all tiers)",
	}}
	if s.paid() {
		caps.Supported |= core.CapDependencies
	} else if s.Plan == "free" {
		caps.Reasons[core.CapDependencies] = "dependencies need GitLab Premium; this project namespace is Free"
	} else {
		caps.Reasons[core.CapDependencies] = "dependencies need GitLab Premium; entitlement is unknown: " + s.PlanReason
	}
	level := 0
	for _, a := range []*access{s.Project.Permissions.Project, s.Project.Permissions.Group} {
		if a != nil && a.AccessLevel > level {
			level = a.AccessLevel
		}
	}
	if level >= 40 && !s.Project.Archived {
		caps.Supported |= core.CapDelete
	} else {
		caps.Reasons[core.CapDelete] = "permanent issue deletion requires GitLab Maintainer or Owner access to an active project"
	}
	return caps
}

func (s *schema) mapBoard(host string) error {
	b := s.Board
	if b.HideOpen || b.HideClosed || b.Milestone != nil || b.Assignee != nil || len(b.Labels) > 0 || b.Weight != nil || b.Iteration != nil || b.IterationCadence != nil {
		return fmt.Errorf("%w: this GitLab board has hidden endpoints or scope filters; use an unfiltered label board with Open and Closed visible", core.ErrUnsupported)
	}
	caps := s.capabilities()
	s.Info = core.BoardInfo{Name: b.Name, Provider: ProviderName, URL: fmt.Sprintf("%s/-/boards/%d", s.Project.WebURL, b.ID), Capabilities: &caps,
		Metadata: map[string]any{"project_id": s.Project.ID, "board_id": b.ID, "host": host, "tier": s.Plan}}
	s.Info.Statuses = []core.Status{{Name: "Open", Initial: true}}
	sort.SliceStable(b.Lists, func(i, j int) bool { return b.Lists[i].Position < b.Lists[j].Position })
	seen := map[string]bool{"open": true, "closed": true}
	for _, list := range b.Lists {
		if list.Label == nil || list.Label.ID == 0 || list.Label.Name == "" {
			return fmt.Errorf("%w: GitLab board list %d is not a label list", core.ErrUnsupported, list.ID)
		}
		name := list.Label.Name
		if seen[strings.ToLower(name)] || strings.HasPrefix(name, "priority::") || strings.HasPrefix(name, "class::") {
			return fmt.Errorf("%w: ambiguous/reserved GitLab board label %q", core.ErrUnsupported, name)
		}
		seen[strings.ToLower(name)] = true
		s.Info.Statuses = append(s.Info.Statuses, core.Status{Name: name, WIPLimit: list.MaxIssueCount})
	}
	s.Info.Statuses = append(s.Info.Statuses, core.Status{Name: "Closed", Terminal: true})
	// GitLab lower priority numbers are more important; core orders low to high.
	sort.SliceStable(s.Labels, func(i, j int) bool {
		a, b := s.Labels[i], s.Labels[j]
		if a.Priority == nil && b.Priority != nil {
			return true
		}
		if a.Priority != nil && b.Priority == nil {
			return false
		}
		if a.Priority != nil && b.Priority != nil && *a.Priority != *b.Priority {
			return *a.Priority > *b.Priority
		}
		return a.Name < b.Name
	})
	for _, l := range s.Labels {
		if l.Archived {
			continue
		}
		if v, ok := strings.CutPrefix(l.Name, "priority::"); ok && v != "" {
			s.Info.Priorities = append(s.Info.Priorities, v)
		}
		if v, ok := strings.CutPrefix(l.Name, "class::"); ok && v != "" {
			s.Info.Classes = append(s.Info.Classes, core.Class{Name: v})
		}
	}
	return nil
}
