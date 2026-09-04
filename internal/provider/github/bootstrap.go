package github

import (
	"context"
	"fmt"
	"strings"

	"github.com/aramponi/yakanban/internal/core"
)

// requiredField describes a project field yakanban needs, and how to create it.
type requiredField struct {
	Name     string
	DataType string
	Options  []string
}

// yakanbanFields are the custom fields added to the project on init. They are
// ordinary project fields: a product owner can sort and filter on them in the
// GitHub UI without ever running yakanban.
func yakanbanFields(priorities, classes []string) []requiredField {
	return []requiredField{
		{Name: fieldPriority, DataType: typeSingleSelect, Options: priorities},
		{Name: fieldClass, DataType: typeSingleSelect, Options: classes},
		{Name: fieldEstimate, DataType: typeText},
		{Name: fieldDue, DataType: typeDate},
		{Name: fieldStarted, DataType: typeDate},
		{Name: fieldCompleted, DataType: typeDate},
		{Name: fieldBlocked, DataType: typeText},
		{Name: fieldDependsOn, DataType: typeText},
		{Name: fieldParent, DataType: typeText},
		{Name: fieldClaim, DataType: typeText},
		{Name: fieldClaimExpires, DataType: typeText},
	}
}

// optionColors cycles through the Projects v2 palette so a generated
// single-select field does not come out all grey.
var optionColors = []string{"GRAY", "BLUE", "GREEN", "YELLOW", "ORANGE", "RED", "PURPLE", "PINK"}

// Bootstrap provisions the board: it creates the project when none is given,
// links it to the repository and adds every missing yakanban field.
//
// It never rewrites an existing Status column set — the columns people already
// work with in the web UI stay authoritative.
func (p *Provider) Bootstrap(ctx context.Context, opts core.BootstrapOptions) (*core.BoardInfo, error) {
	if err := p.settings.Validate(); err != nil {
		return nil, err
	}
	freshProject := false
	if p.settings.ProjectNumber <= 0 {
		number, err := p.createProject(ctx, opts.Name)
		if err != nil {
			return nil, err
		}
		p.settings.ProjectNumber = number
		freshProject = true
	}
	p.schema = nil
	s, err := p.fetchSchema(ctx)
	if err != nil {
		return nil, err
	}

	if freshProject {
		// A brand new project has no items, so replacing the default
		// Todo/In Progress/Done columns is safe here and only here.
		if err := p.setStatusOptions(ctx, s, statusNames(opts.Statuses)); err != nil {
			return nil, err
		}
	}
	if err := p.ensureFields(ctx, s, opts); err != nil {
		return nil, err
	}
	p.schema = nil
	s, err = p.fetchSchema(ctx)
	if err != nil {
		return nil, err
	}
	board := boardFromSchema(orDefault(opts.Name, s.Title), s)
	board.Metadata["project_number"] = s.Number
	board.Metadata["owner"] = p.settings.Owner
	board.Metadata["repo"] = p.settings.Repo
	return board, nil
}

// Settings returns the (possibly updated) provider settings, so `init` can
// persist the project number it just created.
func (p *Provider) Settings() Settings { return p.settings }

// ConfigSettings renders the provider block to write back to .yakanban.yml.
func (p *Provider) ConfigSettings() map[string]any { return p.settings.ToMap() }

// createProject creates a Projects v2 board owned by the configured owner and
// links it to the repository.
func (p *Provider) createProject(ctx context.Context, title string) (int, error) {
	var owner struct {
		RepositoryOwner *struct {
			TypeName string `json:"__typename"`
			ID       string `json:"id"`
		} `json:"repositoryOwner"`
	}
	if err := p.client.graphql(ctx, ownerQuery, map[string]any{"login": p.settings.Owner}, &owner); err != nil {
		return 0, err
	}
	if owner.RepositoryOwner == nil {
		return 0, fmt.Errorf("%w: no GitHub user or organization named %q", core.ErrNotFound, p.settings.Owner)
	}
	var created struct {
		CreateProjectV2 struct {
			ProjectV2 struct {
				ID     string `json:"id"`
				Number int    `json:"number"`
				Title  string `json:"title"`
				URL    string `json:"url"`
			} `json:"projectV2"`
		} `json:"createProjectV2"`
	}
	vars := map[string]any{"owner": owner.RepositoryOwner.ID, "title": orDefault(title, p.settings.Repo)}
	if err := p.client.graphql(ctx, createProjectMutation, vars, &created); err != nil {
		return 0, err
	}
	project := created.CreateProjectV2.ProjectV2
	if project.Number == 0 {
		return 0, fmt.Errorf("github did not return a project number")
	}

	var repo struct {
		Repository *struct {
			ID string `json:"id"`
		} `json:"repository"`
	}
	repoVars := map[string]any{"owner": p.settings.Owner, "repo": p.settings.Repo}
	if err := p.client.graphql(ctx, repoQuery, repoVars, &repo); err != nil {
		return project.Number, err
	}
	if repo.Repository != nil {
		linkVars := map[string]any{"project": project.ID, "repo": repo.Repository.ID}
		if err := p.client.graphql(ctx, linkRepoMutation, linkVars, nil); err != nil {
			// A project that is not linked to the repo still works; it just
			// will not show up in the repository's Projects tab.
			return project.Number, fmt.Errorf("project #%d created but could not be linked to %s/%s: %w",
				project.Number, p.settings.Owner, p.settings.Repo, err)
		}
	}
	return project.Number, nil
}

// ensureFields creates every yakanban field the project does not already have.
func (p *Provider) ensureFields(ctx context.Context, s *schema, opts core.BootstrapOptions) error {
	classes := make([]string, 0, len(opts.Classes))
	for _, c := range opts.Classes {
		classes = append(classes, c.Name)
	}
	for _, want := range yakanbanFields(opts.Priorities, classes) {
		if _, exists := s.field(want.Name); exists {
			continue
		}
		if want.DataType == typeSingleSelect && len(want.Options) == 0 {
			continue
		}
		vars := map[string]any{
			"project": s.ProjectID,
			"type":    want.DataType,
			"name":    want.Name,
		}
		if want.DataType == typeSingleSelect {
			vars["options"] = optionInputs(want.Options)
		}
		if err := p.client.graphql(ctx, createFieldMutation, vars, nil); err != nil {
			return fmt.Errorf("creating project field %q: %w", want.Name, err)
		}
	}
	return nil
}

// setStatusOptions rewrites the Status column set of a freshly created project.
func (p *Provider) setStatusOptions(ctx context.Context, s *schema, names []string) error {
	if len(names) == 0 {
		return nil
	}
	f, ok := s.field(fieldStatus)
	if !ok {
		return nil
	}
	vars := map[string]any{"field": f.ID, "options": optionInputs(names)}
	if err := p.client.graphql(ctx, updateFieldOptionsMutation, vars, nil); err != nil {
		return fmt.Errorf("setting the Status columns: %w", err)
	}
	return nil
}

func optionInputs(names []string) []map[string]any {
	out := make([]map[string]any, 0, len(names))
	for i, n := range names {
		out = append(out, map[string]any{
			"name":        n,
			"color":       optionColors[i%len(optionColors)],
			"description": "",
		})
	}
	return out
}

func statusNames(statuses []core.Status) []string {
	out := make([]string, 0, len(statuses))
	for _, s := range statuses {
		out = append(out, s.Name)
	}
	return out
}

func orDefault(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}
