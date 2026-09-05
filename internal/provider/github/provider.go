package github

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/aramponi/yakanban/internal/core"
)

// Provider is the GitHub Issues + Projects v2 adapter.
type Provider struct {
	settings    Settings
	boardName   string
	client      *client
	schema      *schema
	schemaCache SchemaCache
	tokenOrigin string
}

// compile-time checks that the adapter satisfies its ports.
var (
	_ core.Provider     = (*Provider)(nil)
	_ core.Bootstrapper = (*Provider)(nil)
)

// New builds the adapter. The token comes from the environment or from the
// `gh` CLI the user has already authenticated, so yakanban never stores one.
func New(settings Settings, boardName, userAgent string, sc SchemaCache) (*Provider, error) {
	if err := settings.Validate(); err != nil {
		return nil, err
	}
	tok, err := ResolveToken(settings.Host)
	if err != nil {
		return nil, err
	}
	return &Provider{
		settings:    settings,
		boardName:   boardName,
		client:      newClient(settings.Host, tok.Token, userAgent),
		schemaCache: sc,
		tokenOrigin: tok.Origin,
	}, nil
}

// Name identifies the provider in configuration and messages.
func (p *Provider) Name() string { return ProviderName }

// TokenOrigin says where the credentials came from, for `yakanban config`.
func (p *Provider) TokenOrigin() string { return p.tokenOrigin }

// Capabilities reports what this backend can store. Everything the domain
// model defines is expressible as a project field, so nothing is missing —
// but a real delete is not: GitHub issues are closed and archived instead.
func (p *Provider) Capabilities() core.Capability {
	return core.CapClaims | core.CapDependencies | core.CapParent | core.CapBlocked |
		core.CapEstimate | core.CapClass | core.CapDueDate | core.CapArchive |
		core.CapLinkedBranch | core.CapWorkflowDates
}

// Board returns the live board description, read from the project itself.
func (p *Provider) Board(ctx context.Context) (*core.BoardInfo, error) {
	s, err := p.loadSchema(ctx)
	if err != nil {
		return nil, err
	}
	name := p.boardName
	if name == "" {
		name = s.Title
	}
	return boardFromSchema(name, s), nil
}

// List returns every issue on the board. The filter is applied by the caller:
// Projects v2 cannot filter server-side on custom fields, so narrowing here
// would cost the same request and lose the cache hit.
func (p *Provider) List(ctx context.Context, _ core.Filter) ([]core.Task, error) {
	s, err := p.loadSchema(ctx)
	if err != nil {
		return nil, err
	}
	var (
		tasks  []core.Task
		cursor *string
	)
	for {
		var res struct {
			Node struct {
				Items struct {
					PageInfo struct {
						HasNextPage bool   `json:"hasNextPage"`
						EndCursor   string `json:"endCursor"`
					} `json:"pageInfo"`
					Nodes []itemNode `json:"nodes"`
				} `json:"items"`
			} `json:"node"`
		}
		vars := map[string]any{"project": s.ProjectID, "cursor": cursor}
		if err := p.client.graphql(ctx, listQuery, vars, &res); err != nil {
			return nil, err
		}
		for _, n := range res.Node.Items.Nodes {
			// Draft issues and pull requests have no Issue content: they are
			// visible in the project UI but out of scope for yakanban.
			if n.Content.TypeName != "Issue" || n.Content.Number == 0 {
				continue
			}
			if n.IsArchived {
				continue
			}
			tasks = append(tasks, n.toTask())
		}
		if !res.Node.Items.PageInfo.HasNextPage {
			break
		}
		next := res.Node.Items.PageInfo.EndCursor
		cursor = &next
	}
	return tasks, nil
}

// issueResult is the shape returned by getQuery.
type issueResult struct {
	Repository struct {
		Issue *struct {
			NodeID string `json:"id"`
			issueContent
			LinkedBranches struct {
				Nodes []linkedBranchNode `json:"nodes"`
			} `json:"linkedBranches"`
			ProjectItems struct {
				Nodes []struct {
					ID         string `json:"id"`
					IsArchived bool   `json:"isArchived"`
					Project    struct {
						ID string `json:"id"`
					} `json:"project"`
					FieldValues fieldValues `json:"fieldValues"`
				} `json:"nodes"`
			} `json:"projectItems"`
		} `json:"issue"`
	} `json:"repository"`
}

// Get returns one task by issue number, together with its project field values.
func (p *Provider) Get(ctx context.Context, id string) (*core.Task, error) {
	number, err := issueNumber(id)
	if err != nil {
		return nil, err
	}
	s, err := p.loadSchema(ctx)
	if err != nil {
		return nil, err
	}
	var res issueResult
	vars := map[string]any{"owner": p.settings.Owner, "repo": p.settings.Repo, "number": number}
	if err := p.client.graphql(ctx, getQuery, vars, &res); err != nil {
		return nil, err
	}
	issue := res.Repository.Issue
	if issue == nil {
		return nil, fmt.Errorf("%w: issue #%d in %s/%s", core.ErrNotFound, number, p.settings.Owner, p.settings.Repo)
	}
	node := itemNode{Content: issue.issueContent}
	node.Content.Number = number
	for _, it := range issue.ProjectItems.Nodes {
		if it.Project.ID == s.ProjectID {
			node.ID = it.ID
			node.IsArchived = it.IsArchived
			node.FieldValues = it.FieldValues
			break
		}
	}
	task := node.toTask()
	task.Metadata["node_id"] = issue.NodeID
	if names := branchNames(issue.LinkedBranches.Nodes); len(names) > 0 {
		task.Metadata[core.MetaLinkedBranches] = names
	}
	if node.ID == "" {
		// The issue exists but was never added to the board; it has no
		// workflow state yet. Adding it lazily happens on the first write.
		task.Metadata["on_board"] = false
	}
	return &task, nil
}

// branchNames lists the branch names attached to an issue, for the metadata
// the human view surfaces.
func branchNames(nodes []linkedBranchNode) []string {
	out := make([]string, 0, len(nodes))
	for _, n := range nodes {
		if n.Ref.Name != "" {
			out = append(out, n.Ref.Name)
		}
	}
	return out
}

// issuePayload is the REST body used to create or patch an issue.
type issuePayload struct {
	Title     *string   `json:"title,omitempty"`
	Body      *string   `json:"body,omitempty"`
	Labels    *[]string `json:"labels,omitempty"`
	Assignees *[]string `json:"assignees,omitempty"`
	State     *string   `json:"state,omitempty"`
}

type issueResponse struct {
	Number  int    `json:"number"`
	NodeID  string `json:"node_id"`
	HTMLURL string `json:"html_url"`
}

// Create opens an issue, adds it to the project and writes its fields.
func (p *Provider) Create(ctx context.Context, d core.Draft) (*core.Task, error) {
	s, err := p.loadSchema(ctx)
	if err != nil {
		return nil, err
	}
	payload := issuePayload{Title: &d.Title}
	if d.Body != "" {
		payload.Body = &d.Body
	}
	if len(d.Tags) > 0 {
		tags := d.Tags
		payload.Labels = &tags
	}
	if len(d.Assignees) > 0 {
		assignees := d.Assignees
		payload.Assignees = &assignees
	}
	var created issueResponse
	path := fmt.Sprintf("/repos/%s/%s/issues", p.settings.Owner, p.settings.Repo)
	if err := p.client.rest(ctx, "POST", path, payload, &created); err != nil {
		return nil, err
	}
	itemID, err := p.addToProject(ctx, s, created.NodeID)
	if err != nil {
		return nil, fmt.Errorf("issue #%d was created but could not be added to the project: %w", created.Number, err)
	}
	updates := fieldUpdates{}
	updates.setSelect(fieldStatus, d.Status)
	updates.setSelect(fieldPriority, d.Priority)
	updates.setSelect(fieldClass, d.Class)
	updates.setText(fieldEstimate, d.Estimate)
	updates.setText(fieldParent, d.Parent)
	updates.setText(fieldDependsOn, joinList(d.DependsOn))
	updates.setDate(fieldDue, d.Due)
	if d.Claim != nil && d.Claim.Agent != "" {
		updates.setText(fieldClaim, d.Claim.Agent)
		updates.setText(fieldClaimExpires, d.Claim.Expires.UTC().Format(time.RFC3339))
	}
	if err := p.applyFields(ctx, s, itemID, updates); err != nil {
		return nil, fmt.Errorf("issue #%d was created but its fields could not all be set: %w", created.Number, err)
	}
	return p.Get(ctx, strconv.Itoa(created.Number))
}

// Update applies a patch to the issue and to its project fields.
func (p *Provider) Update(ctx context.Context, id string, patch core.Patch) (*core.Task, error) {
	number, err := issueNumber(id)
	if err != nil {
		return nil, err
	}
	s, err := p.loadSchema(ctx)
	if err != nil {
		return nil, err
	}
	cur, err := p.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	if body := issuePatch(cur, patch); body != nil {
		path := fmt.Sprintf("/repos/%s/%s/issues/%d", p.settings.Owner, p.settings.Repo, number)
		if err := p.client.rest(ctx, "PATCH", path, body, nil); err != nil {
			return nil, err
		}
	}

	updates := projectPatch(cur, patch)
	if len(updates) > 0 {
		itemID := itemID(cur)
		if itemID == "" {
			nodeID, _ := cur.Metadata["node_id"].(string)
			if itemID, err = p.addToProject(ctx, s, nodeID); err != nil {
				return nil, err
			}
		}
		if err := p.applyFields(ctx, s, itemID, updates); err != nil {
			return nil, err
		}
	}
	return p.Get(ctx, id)
}

// Delete closes the issue and archives its project item. GitHub only lets a
// repository admin truly delete an issue, and losing the discussion thread is
// rarely what someone means by "delete a task".
func (p *Provider) Delete(ctx context.Context, id string) error {
	number, err := issueNumber(id)
	if err != nil {
		return err
	}
	s, err := p.loadSchema(ctx)
	if err != nil {
		return err
	}
	cur, err := p.Get(ctx, id)
	if err != nil {
		return err
	}
	closed := "closed"
	path := fmt.Sprintf("/repos/%s/%s/issues/%d", p.settings.Owner, p.settings.Repo, number)
	if err := p.client.rest(ctx, "PATCH", path, issuePayload{State: &closed}, nil); err != nil {
		return err
	}
	if item := itemID(cur); item != "" {
		vars := map[string]any{"project": s.ProjectID, "item": item}
		if err := p.client.graphql(ctx, archiveItemMutation, vars, nil); err != nil {
			return err
		}
	}
	return nil
}

// addToProject adds an issue node to the board and returns its item ID.
func (p *Provider) addToProject(ctx context.Context, s *schema, contentNodeID string) (string, error) {
	if contentNodeID == "" {
		return "", fmt.Errorf("%w: missing issue node id", core.ErrInvalidInput)
	}
	var res struct {
		AddProjectV2ItemByID struct {
			Item struct {
				ID string `json:"id"`
			} `json:"item"`
		} `json:"addProjectV2ItemById"`
	}
	vars := map[string]any{"project": s.ProjectID, "content": contentNodeID}
	if err := p.client.graphql(ctx, addItemMutation, vars, &res); err != nil {
		return "", err
	}
	return res.AddProjectV2ItemByID.Item.ID, nil
}

// issueNumber parses a task ID into a GitHub issue number.
func issueNumber(id string) (int, error) {
	n, err := strconv.Atoi(strings.TrimPrefix(strings.TrimSpace(id), "#"))
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("%w: %q is not a GitHub issue number", core.ErrInvalidInput, id)
	}
	return n, nil
}

// issuePatch builds the REST body for the issue-side part of a patch, or nil
// when nothing at that level changed.
func issuePatch(cur *core.Task, p core.Patch) *issuePayload {
	var body issuePayload
	touched := false
	if p.Title != nil {
		body.Title = p.Title
		touched = true
	}
	if p.Body != nil {
		body.Body = p.Body
		touched = true
	}
	if p.Assignees != nil {
		list := *p.Assignees
		body.Assignees = &list
		touched = true
	}
	if len(p.AddTags) > 0 || len(p.RemoveTags) > 0 {
		labels := mergeList(cur.Tags, p.AddTags, p.RemoveTags)
		body.Labels = &labels
		touched = true
	}
	if !touched {
		return nil
	}
	return &body
}
