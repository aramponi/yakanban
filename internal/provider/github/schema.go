package github

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aramponi/yakanban/internal/core"
)

// The custom Projects v2 fields yakanban relies on. `Status` already exists on
// every project; the others are created by `yakanban init`. They are plain
// project fields, so a non-technical user can read and edit them in the
// GitHub web UI without knowing yakanban exists.
const (
	fieldStatus       = "Status"
	fieldPriority     = "Priority"
	fieldClass        = "Class"
	fieldEstimate     = "Estimate"
	fieldDue          = "Due"
	fieldStarted      = "Started"
	fieldCompleted    = "Completed"
	fieldBlocked      = "Blocked"
	fieldDependsOn    = "Depends On"
	fieldParent       = "Parent"
	fieldClaim        = "Claim"
	fieldClaimExpires = "Claim Expires"
)

// Projects v2 data types used by yakanban.
const (
	typeText         = "TEXT"
	typeDate         = "DATE"
	typeNumber       = "NUMBER"
	typeSingleSelect = "SINGLE_SELECT"
)

type option struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type field struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	DataType string   `json:"dataType"`
	Options  []option `json:"options,omitempty"`
}

// optionID resolves a single-select option by name, case-insensitively.
func (f field) optionID(name string) (string, bool) {
	for _, o := range f.Options {
		if strings.EqualFold(o.Name, name) {
			return o.ID, true
		}
	}
	return "", false
}

func (f field) optionNames() []string {
	out := make([]string, 0, len(f.Options))
	for _, o := range f.Options {
		out = append(out, o.Name)
	}
	return out
}

// schema is the resolved shape of the project: its node ID plus every field
// and single-select option, which mutations need by ID.
type schema struct {
	ProjectID string           `json:"project_id"`
	Number    int              `json:"number"`
	Title     string           `json:"title"`
	URL       string           `json:"url"`
	Fields    map[string]field `json:"fields"`
}

func (s *schema) field(name string) (field, bool) {
	f, ok := s.Fields[strings.ToLower(name)]
	return f, ok
}

// SchemaCache is the subset of the cache store the adapter uses to avoid
// re-discovering project field IDs on every command.
type SchemaCache interface {
	Get(key string, out any) (time.Time, bool)
	Put(key string, v any) error
}

const projectQuery = `
query($owner:String!, $number:Int!) {
  organization(login:$owner) { projectV2(number:$number) { ...P } }
  user(login:$owner)         { projectV2(number:$number) { ...P } }
}
fragment P on ProjectV2 {
  id number title url
  fields(first:100) { nodes {
    __typename
    ... on ProjectV2Field { id name dataType }
    ... on ProjectV2SingleSelectField { id name dataType options { id name } }
  } }
}`

type projectPayload struct {
	ID     int64  `json:"-"`
	Number int    `json:"number"`
	NodeID string `json:"id"`
	Title  string `json:"title"`
	URL    string `json:"url"`
	Fields struct {
		Nodes []field `json:"nodes"`
	} `json:"fields"`
}

type projectQueryResult struct {
	Organization *struct {
		ProjectV2 *projectPayload `json:"projectV2"`
	} `json:"organization"`
	User *struct {
		ProjectV2 *projectPayload `json:"projectV2"`
	} `json:"user"`
}

// loadSchema resolves the project once per process, reading the disk cache
// first: field IDs change only when someone edits the project's structure.
func (p *Provider) loadSchema(ctx context.Context) (*schema, error) {
	if p.schema != nil {
		return p.schema, nil
	}
	key := fmt.Sprintf("github:schema:%s/%s#%d", p.settings.Owner, p.settings.Repo, p.settings.ProjectNumber)
	if p.schemaCache != nil {
		var cached schema
		if _, ok := p.schemaCache.Get(key, &cached); ok && cached.ProjectID != "" {
			p.schema = &cached
			return p.schema, nil
		}
	}
	s, err := p.fetchSchema(ctx)
	if err != nil {
		return nil, err
	}
	if p.schemaCache != nil {
		_ = p.schemaCache.Put(key, s)
	}
	p.schema = s
	return s, nil
}

// fetchSchema asks GitHub for the project structure. The query looks the
// project up under both an organization and a user login, because the caller
// should not have to know which one the owner is.
func (p *Provider) fetchSchema(ctx context.Context) (*schema, error) {
	if err := p.settings.ValidateBoard(); err != nil {
		return nil, err
	}
	var res projectQueryResult
	vars := map[string]any{"owner": p.settings.Owner, "number": p.settings.ProjectNumber}
	if err := p.client.graphql(ctx, projectQuery, vars, &res); err != nil && !isNotFound(err) {
		return nil, err
	}
	var payload *projectPayload
	if res.Organization != nil && res.Organization.ProjectV2 != nil {
		payload = res.Organization.ProjectV2
	} else if res.User != nil && res.User.ProjectV2 != nil {
		payload = res.User.ProjectV2
	}
	if payload == nil {
		return nil, fmt.Errorf("%w: project #%d not found for owner %q (check `providers.github.project_number` in %s, and that your token has the `project` scope)",
			core.ErrNotFound, p.settings.ProjectNumber, p.settings.Owner, ".yakanban.yml")
	}
	s := &schema{
		ProjectID: payload.NodeID,
		Number:    payload.Number,
		Title:     payload.Title,
		URL:       payload.URL,
		Fields:    make(map[string]field, len(payload.Fields.Nodes)),
	}
	for _, f := range payload.Fields.Nodes {
		if f.ID == "" || f.Name == "" {
			continue
		}
		s.Fields[strings.ToLower(f.Name)] = f
	}
	if _, ok := s.field(fieldStatus); !ok {
		return nil, fmt.Errorf("project #%d has no Status field; add a Status column set in the GitHub UI", payload.Number)
	}
	return s, nil
}

// boardFromSchema turns the project's Status/Priority/Class options into the
// domain board description. The project, not the local file, decides which
// columns exist.
func boardFromSchema(name string, s *schema) *core.BoardInfo {
	b := &core.BoardInfo{Name: name, Capabilities: &core.CapabilitySet{Supported: (&Provider{}).Capabilities(), Reasons: map[core.Capability]string{core.CapDelete: "GitHub issues are closed and archived; permanent deletion is not supported by this adapter"}},
		Provider: ProviderName, URL: s.URL}
	if f, ok := s.field(fieldStatus); ok {
		for _, o := range f.Options {
			b.Statuses = append(b.Statuses, core.Status{Name: o.Name})
		}
	}
	if f, ok := s.field(fieldPriority); ok {
		b.Priorities = f.optionNames()
	}
	if f, ok := s.field(fieldClass); ok {
		for _, o := range f.Options {
			b.Classes = append(b.Classes, core.Class{Name: o.Name})
		}
	}
	b.Metadata = map[string]any{
		"project_id":     s.ProjectID,
		"project_number": s.Number,
		"project_title":  s.Title,
	}
	return b
}
