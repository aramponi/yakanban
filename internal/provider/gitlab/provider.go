package gitlab

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/aramponi/yakanban/internal/cache"
	"github.com/aramponi/yakanban/internal/core"
)

type Provider struct {
	settings Settings
	client   *client
	store    *cache.Store
	schema   *schema
}

var _ core.Provider = (*Provider)(nil)
var _ core.Bootstrapper = (*Provider)(nil)

func New(settings Settings, agent string, store *cache.Store) (*Provider, error) {
	if err := settings.Validate(); err != nil {
		return nil, err
	}
	token, err := ResolveToken(settings.Host)
	if err != nil {
		return nil, err
	}
	return &Provider{settings: settings, client: newClient(settings.Host, token, agent), store: store}, nil
}
func (p *Provider) Name() string                   { return ProviderName }
func (p *Provider) ConfigSettings() map[string]any { return p.settings.ToMap() }
func (p *Provider) Board(ctx context.Context) (*core.BoardInfo, error) {
	p.schema = nil
	s, err := p.load(ctx)
	if err != nil {
		return nil, err
	}
	return &s.Info, nil
}

func (p *Provider) List(ctx context.Context, _ core.Filter) ([]core.Task, error) {
	s, err := p.load(ctx)
	if err != nil {
		return nil, err
	}
	issues, err := pages[issue](ctx, p.client, p.projectPath()+"/issues?scope=all&state=all&issue_type=issue")
	if err != nil {
		return nil, err
	}
	tasks := make([]core.Task, 0, len(issues))
	for _, i := range issues {
		t, err := s.task(i)
		if err != nil {
			return nil, err
		}
		if err := p.readDependencies(ctx, s, t); err != nil {
			return nil, err
		}
		tasks = append(tasks, *t)
	}
	return tasks, nil
}

func (p *Provider) Get(ctx context.Context, id string) (*core.Task, error) {
	id, err := issueID(id)
	if err != nil {
		return nil, err
	}
	s, err := p.load(ctx)
	if err != nil {
		return nil, err
	}
	var i issue
	if _, err := p.client.request(ctx, "GET", p.projectPath()+"/issues/"+id, nil, &i); err != nil {
		return nil, err
	}
	if strconv.Itoa(i.IID) != id {
		return nil, fmt.Errorf("GitLab returned a different issue IID")
	}
	t, err := s.task(i)
	if err != nil {
		return nil, err
	}
	if err := p.readDependencies(ctx, s, t); err != nil {
		return nil, err
	}
	return t, nil
}

func (p *Provider) readDependencies(ctx context.Context, s *schema, t *core.Task) error {
	if !s.Info.Capabilities.Has(core.CapDependencies) {
		return nil
	}
	links, err := pages[issueLink](ctx, p.client, p.projectPath()+"/issues/"+t.ID+"/links")
	if err != nil {
		return err
	}
	t.Metadata["issue_links"] = links
	for _, l := range links {
		if l.Type == "is_blocked_by" {
			if l.ProjectID != s.Project.ID {
				return fmt.Errorf("%w: GitLab issue #%s has a cross-project dependency; local task IDs cannot represent it", core.ErrUnsupported, t.ID)
			}
			t.DependsOn = append(t.DependsOn, strconv.Itoa(l.IID))
		}
	}
	return nil
}

var durationPattern = regexp.MustCompile(`^(?:[0-9]+(?:\.[0-9]+)?[wdhms]\s*)+$`)

func (p *Provider) preflight(s *schema, patch core.Patch) error {
	checks := []struct {
		requested bool
		cap       core.Capability
	}{
		{patch.Claim != nil || patch.ReleaseClaim, core.CapClaims}, {len(patch.AddDeps) > 0 || len(patch.RemoveDeps) > 0 || patch.ClearDeps, core.CapDependencies},
		{patch.Parent != nil || patch.ClearParent, core.CapParent}, {patch.Blocked != nil, core.CapBlocked},
		{patch.Estimate != nil, core.CapEstimate}, {patch.Class != nil, core.CapClass}, {patch.Due != nil || patch.ClearDue, core.CapDueDate},
		{patch.Started != nil || patch.ClearStarted || patch.Completed != nil || patch.ClearCompleted, core.CapWorkflowDates},
	}
	for _, check := range checks {
		if check.requested {
			if err := s.Info.Capabilities.Require(ProviderName, check.cap); err != nil {
				return err
			}
		}
	}
	if s.Project.Archived || !s.Project.IssuesEnabled {
		return fmt.Errorf("%w: GitLab project is archived or issues are disabled", core.ErrUnsupported)
	}
	if len(patch.Metadata) > 0 {
		return fmt.Errorf("%w: arbitrary GitLab metadata writes are unsupported", core.ErrUnsupported)
	}
	if patch.AppendBody != nil {
		return fmt.Errorf("%w: append-body must be resolved by the application service", core.ErrInvalidInput)
	}
	if patch.Estimate != nil && *patch.Estimate != "" && !durationPattern.MatchString(strings.TrimSpace(*patch.Estimate)) {
		return fmt.Errorf("%w: GitLab estimates need a duration such as 4h or 1d 30m", core.ErrInvalidInput)
	}
	if patch.Status != nil {
		if _, ok := s.Info.Status(*patch.Status); !ok {
			return fmt.Errorf("%w: GitLab board has no column %q", core.ErrUnknownStatus, *patch.Status)
		}
	}
	for _, field := range []struct {
		name    string
		value   *string
		allowed []string
	}{{"priority", patch.Priority, s.Info.Priorities}, {"class", patch.Class, s.Info.ClassNames()}} {
		if field.value != nil && *field.value != "" && !slices.Contains(field.allowed, *field.value) {
			return &core.InvalidValueError{Field: field.name, Value: *field.value, Allowed: field.allowed}
		}
	}
	for _, name := range append(slices.Clone(patch.AddTags), patch.RemoveTags...) {
		st, ok := s.Info.Status(name)
		if strings.Contains(name, ",") || strings.ContainsAny(name, "\r\n") || strings.HasPrefix(name, "priority::") || strings.HasPrefix(name, "class::") || (ok && !st.Initial && !st.Terminal) {
			return fmt.Errorf("%w: label %q is reserved or cannot be represented as a tag; edit its workflow field instead", core.ErrInvalidInput, name)
		}
	}
	if patch.Assignees != nil && len(*patch.Assignees) > 1 && !s.paid() {
		return fmt.Errorf("%w: multiple assignees need GitLab Premium; project namespace tier is %s", core.ErrUnsupported, s.Plan)
	}
	for _, id := range append(slices.Clone(patch.AddDeps), patch.RemoveDeps...) {
		if _, err := issueID(id); err != nil {
			return err
		}
	}
	return nil
}

func (p *Provider) assigneeIDs(ctx context.Context, names []string) ([]int, error) {
	ids := make([]int, 0, len(names))
	for _, name := range names {
		users, err := pages[user](ctx, p.client, "/users?username="+url.QueryEscape(name))
		if err != nil {
			return nil, err
		}
		found := false
		for _, u := range users {
			if strings.EqualFold(u.Username, name) && u.ID > 0 {
				ids = append(ids, u.ID)
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("%w: GitLab user %q", core.ErrNotFound, name)
		}
	}
	return ids, nil
}

func (p *Provider) Create(ctx context.Context, d core.Draft) (*core.Task, error) {
	s, err := p.load(ctx)
	if err != nil {
		return nil, err
	}
	patch := core.Patch{Title: &d.Title, Body: &d.Body, Status: &d.Status, Priority: &d.Priority, Class: &d.Class, Estimate: &d.Estimate, Due: d.Due, Assignees: &d.Assignees, AddTags: d.Tags, AddDeps: d.DependsOn, Claim: d.Claim, Metadata: d.Metadata}
	if d.Estimate == "" {
		patch.Estimate = nil
	}
	if d.Parent != "" {
		patch.Parent = &d.Parent
	}
	if err := p.preflight(s, patch); err != nil {
		return nil, err
	}
	if strings.TrimSpace(d.Title) == "" {
		return nil, fmt.Errorf("%w: a title is required", core.ErrInvalidInput)
	}
	body, err := p.issueBody(ctx, s, &core.Task{Status: "Open"}, patch)
	if err != nil {
		return nil, err
	}
	delete(body, "state_event")
	if labels, ok := body["add_labels"]; ok {
		body["labels"] = labels
		delete(body, "add_labels")
	}
	delete(body, "remove_labels")
	var created issue
	if _, err := p.client.request(ctx, "POST", p.projectPath()+"/issues", body, &created); err != nil {
		return nil, err
	}
	if created.IID <= 0 {
		return nil, fmt.Errorf("GitLab create succeeded without an issue IID; inspect the project before retrying")
	}
	id := strconv.Itoa(created.IID)
	if d.Status == "Closed" {
		var closed issue
		if _, err := p.client.request(ctx, "PUT", p.projectPath()+"/issues/"+id, map[string]any{"state_event": "close"}, &closed); err != nil {
			return nil, fmt.Errorf("GitLab issue #%s was created but could not be closed: %w", id, err)
		}
	}
	expectedEstimate, err := p.extraWrites(ctx, id, patch)
	if err != nil {
		return nil, fmt.Errorf("GitLab issue #%s was created but some fields failed: %w", id, err)
	}
	result, err := p.Get(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("GitLab issue #%s was created but readback failed: %w", id, err)
	}
	if err := verifyEstimate(result, expectedEstimate); err != nil {
		return nil, err
	}
	if err := verify(result, patch); err != nil {
		return nil, fmt.Errorf("GitLab issue #%s was created but verification failed: %w", id, err)
	}
	return result, nil
}

func (p *Provider) Update(ctx context.Context, id string, patch core.Patch) (*core.Task, error) {
	id, err := issueID(id)
	if err != nil {
		return nil, err
	}
	s, err := p.load(ctx)
	if err != nil {
		return nil, err
	}
	if err := p.preflight(s, patch); err != nil {
		return nil, err
	}
	current, err := p.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	body, err := p.issueBody(ctx, s, current, patch)
	if err != nil {
		return nil, err
	}
	if len(body) > 0 {
		var updated issue
		if _, err := p.client.request(ctx, "PUT", p.projectPath()+"/issues/"+id, body, &updated); err != nil {
			return nil, err
		}
		if strconv.Itoa(updated.IID) != id {
			return nil, fmt.Errorf("GitLab update returned an unexpected issue IID")
		}
	}
	expectedEstimate, err := p.extraWrites(ctx, id, patch)
	if err != nil {
		return nil, fmt.Errorf("GitLab issue #%s may be partially updated: %w", id, err)
	}
	result, err := p.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := verifyEstimate(result, expectedEstimate); err != nil {
		return nil, err
	}
	if err := verify(result, patch); err != nil {
		return nil, err
	}
	return result, nil
}

func (p *Provider) issueBody(ctx context.Context, s *schema, current *core.Task, patch core.Patch) (map[string]any, error) {
	body := map[string]any{}
	if patch.Title != nil {
		body["title"] = *patch.Title
	}
	if patch.Body != nil {
		body["description"] = *patch.Body
	}
	if patch.Due != nil {
		body["due_date"] = patch.Due.Format("2006-01-02")
	}
	if patch.ClearDue {
		body["due_date"] = ""
	}
	if patch.Assignees != nil {
		ids, err := p.assigneeIDs(ctx, *patch.Assignees)
		if err != nil {
			return nil, err
		}
		body["assignee_ids"] = ids
	}
	add, remove := slices.Clone(patch.AddTags), slices.Clone(patch.RemoveTags)
	if patch.Status != nil {
		body["state_event"] = "reopen"
		if *patch.Status == "Closed" {
			body["state_event"] = "close"
		} else {
			for _, st := range s.Info.Statuses {
				if !st.Initial && !st.Terminal && st.Name != *patch.Status {
					remove = append(remove, st.Name)
				}
			}
			if *patch.Status != "Open" {
				add = append(add, *patch.Status)
			}
		}
	}
	for _, field := range []struct {
		prefix, current string
		next            *string
	}{{"priority::", current.Priority, patch.Priority}, {"class::", current.Class, patch.Class}} {
		if field.next != nil {
			if field.current != "" && field.current != *field.next {
				remove = append(remove, field.prefix+field.current)
			}
			if *field.next != "" {
				add = append(add, field.prefix+*field.next)
			}
		}
	}
	if len(add) > 0 {
		body["add_labels"] = strings.Join(add, ",")
	}
	if len(remove) > 0 {
		body["remove_labels"] = strings.Join(remove, ",")
	}
	return body, nil
}

func (p *Provider) extraWrites(ctx context.Context, id string, patch core.Patch) (int, error) {
	expected := -1
	path := p.projectPath() + "/issues/" + id
	if patch.Estimate != nil {
		endpoint := "/time_estimate"
		var body any = map[string]any{"duration": *patch.Estimate}
		if *patch.Estimate == "" {
			endpoint = "/reset_time_estimate"
			body = nil
		}
		var stats struct {
			Estimate *int `json:"time_estimate"`
		}
		if _, err := p.client.request(ctx, "POST", path+endpoint, body, &stats); err != nil {
			return expected, err
		}
		if stats.Estimate == nil || *stats.Estimate < 0 {
			return expected, fmt.Errorf("GitLab did not return a stored estimate")
		}
		expected = *stats.Estimate
		if *patch.Estimate == "" && expected != 0 {
			return expected, fmt.Errorf("GitLab did not clear the estimate")
		}
	}
	if len(patch.AddDeps) > 0 || len(patch.RemoveDeps) > 0 || patch.ClearDeps {
		return expected, p.writeDependencies(ctx, id, patch)
	}
	return expected, nil
}

func verifyEstimate(task *core.Task, expected int) error {
	if expected < 0 {
		return nil
	}
	stats, ok := task.Metadata["time_stats"].(timeStats)
	if !ok || stats.Estimate != expected {
		return fmt.Errorf("GitLab issue #%s did not retain its estimate; inspect it before retrying", task.ID)
	}
	return nil
}

func (p *Provider) writeDependencies(ctx context.Context, id string, patch core.Patch) error {
	path := p.projectPath() + "/issues/" + id + "/links"
	links, err := pages[issueLink](ctx, p.client, path)
	if err != nil {
		return err
	}
	existing := map[string]bool{}
	for _, link := range links {
		if link.Type != "is_blocked_by" {
			continue
		}
		if link.ProjectID != p.schema.Project.ID {
			return fmt.Errorf("%w: cannot edit cross-project dependencies using local issue IDs", core.ErrUnsupported)
		}
		target := strconv.Itoa(link.IID)
		if patch.ClearDeps || slices.Contains(patch.RemoveDeps, target) {
			if link.LinkID <= 0 {
				return fmt.Errorf("GitLab dependency has no issue_link_id")
			}
			var result map[string]any
			if _, err := p.client.request(ctx, "DELETE", path+"/"+strconv.Itoa(link.LinkID), nil, &result); err != nil {
				return err
			}
		} else {
			existing[target] = true
		}
	}
	for _, dep := range patch.AddDeps {
		target, err := issueID(dep)
		if err != nil {
			return err
		}
		if existing[target] {
			continue
		}
		var result struct {
			ID   int    `json:"id"`
			Type string `json:"link_type"`
		}
		body := map[string]any{"target_project_id": p.schema.Project.ID, "target_issue_iid": target, "link_type": "is_blocked_by"}
		if _, err := p.client.request(ctx, "POST", path, body, &result); err != nil {
			return err
		}
		if result.ID <= 0 || result.Type != "is_blocked_by" {
			return fmt.Errorf("GitLab did not create the requested directional dependency")
		}
		existing[target] = true
	}
	return nil
}

func (p *Provider) Delete(ctx context.Context, id string) error {
	id, err := issueID(id)
	if err != nil {
		return err
	}
	s, err := p.load(ctx)
	if err != nil {
		return err
	}
	if err := s.Info.Capabilities.Require(ProviderName, core.CapDelete); err != nil {
		return err
	}
	_, err = p.client.request(ctx, "DELETE", p.projectPath()+"/issues/"+id, nil, nil)
	return err
}

func verify(t *core.Task, p core.Patch) error {
	fail := func(field string) error {
		return fmt.Errorf("GitLab issue #%s did not retain the requested %s; inspect it before retrying", t.ID, field)
	}
	for _, f := range []struct {
		name, stored string
		want         *string
	}{{"title", t.Title, p.Title}, {"description", t.Body, p.Body}, {"status", t.Status, p.Status}, {"priority", t.Priority, p.Priority}, {"class", t.Class, p.Class}} {
		if f.want != nil && f.stored != *f.want {
			return fail(f.name)
		}
	}
	if p.ClearDue && t.Due != nil {
		return fail("due date")
	}
	if p.Due != nil && (t.Due == nil || t.Due.Format(time.DateOnly) != p.Due.Format(time.DateOnly)) {
		return fail("due date")
	}
	if p.Assignees != nil {
		a, b := slices.Clone(t.Assignees), slices.Clone(*p.Assignees)
		slices.Sort(a)
		slices.Sort(b)
		if !slices.Equal(a, b) {
			return fail("assignees")
		}
	}
	for _, tag := range p.AddTags {
		if !slices.Contains(t.Tags, tag) {
			return fail("added tag")
		}
	}
	for _, tag := range p.RemoveTags {
		if slices.Contains(t.Tags, tag) {
			return fail("removed tag")
		}
	}
	for _, dep := range p.AddDeps {
		id, _ := issueID(dep)
		if !slices.Contains(t.DependsOn, id) {
			return fail("dependency")
		}
	}
	for _, dep := range p.RemoveDeps {
		id, _ := issueID(dep)
		if slices.Contains(t.DependsOn, id) && !slices.Contains(p.AddDeps, id) {
			return fail("removed dependency")
		}
	}
	if p.ClearDeps && len(p.AddDeps) == 0 && len(t.DependsOn) > 0 {
		return fail("cleared dependencies")
	}
	return nil
}
