// Package app holds the use cases: everything that is neither the domain
// model nor a backend adapter. It orchestrates a core.Provider and applies
// the board rules (defaults, validation, claim ownership, timestamps).
package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aramponi/yakanban/internal/core"
)

// Options are the board-level rules the service enforces on top of a provider.
type Options struct {
	DefaultStatus   string
	DefaultPriority string
	DefaultClass    string
	ClaimTimeout    time.Duration
	Now             func() time.Time

	// BranchTemplate and WorktreeTemplate name the branch and working
	// directory of a task. Empty means the feature is unconfigured, not that
	// a default should be invented behind the user's back.
	BranchTemplate   string
	WorktreeTemplate string

	// Branching is the board's branching model: where work starts, where it
	// merges back, and what a branch is called.
	Branching core.BranchPolicy
}

// Service is the application-side facade used by the CLI.
type Service struct {
	provider core.Provider
	board    core.BoardInfo
	opts     Options
}

// New builds a service around a provider and its board description.
func New(p core.Provider, board core.BoardInfo, opts Options) *Service {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.ClaimTimeout <= 0 {
		opts.ClaimTimeout = time.Hour
	}
	return &Service{provider: p, board: board, opts: opts}
}

// Branching returns the board's branching model.
func (s *Service) Branching() core.BranchPolicy { return s.opts.Branching }

// Board returns the board description the service was built with.
func (s *Service) Board() core.BoardInfo { return s.board }

// Provider exposes the underlying adapter, for commands that need it directly.
func (s *Service) Provider() core.Provider { return s.provider }

func (s *Service) now() time.Time { return s.opts.Now() }

// List returns the tasks matching filter, sorted by field.
func (s *Service) List(ctx context.Context, f core.Filter, sortField string, reverse bool) ([]core.Task, error) {
	if err := s.resolveFilter(&f); err != nil {
		return nil, err
	}
	tasks, err := s.provider.List(ctx, f)
	if err != nil {
		return nil, err
	}
	statusByID := make(map[string]string, len(tasks))
	for _, t := range tasks {
		statusByID[t.ID] = t.Status
	}
	statusOf := func(id string) (string, bool) {
		st, ok := statusByID[id]
		return st, ok
	}
	now := s.now()
	out := make([]core.Task, 0, len(tasks))
	for _, t := range tasks {
		if f.Match(t, now, s.board.IsTerminal, statusOf) {
			out = append(out, t)
		}
	}
	core.SortTasks(out, sortField, reverse, s.board.StatusNames(), s.board.Priorities)
	if f.Limit > 0 && len(out) > f.Limit {
		out = out[:f.Limit]
	}
	return out, nil
}

// resolveFilter maps the filter vocabulary onto the board's own spelling, so
// `--status in-progress` finds the column GitHub calls "In Progress" and a
// value that matches nothing is reported instead of silently returning an
// empty list.
func (s *Service) resolveFilter(f *core.Filter) error {
	resolve := func(field string, values []string, allowed []string) ([]string, error) {
		if len(values) == 0 || len(allowed) == 0 {
			return values, nil
		}
		out := make([]string, 0, len(values))
		for _, v := range values {
			resolved, err := ResolveVocabulary(v, allowed)
			if err != nil {
				return nil, &core.InvalidValueError{Field: field, Value: v, Allowed: allowed}
			}
			out = append(out, resolved)
		}
		return out, nil
	}
	var err error
	if f.Statuses, err = resolve("status", f.Statuses, s.board.StatusNames()); err != nil {
		return err
	}
	if f.Priorities, err = resolve("priority", f.Priorities, s.board.Priorities); err != nil {
		return err
	}
	if f.Classes, err = resolve("class", f.Classes, s.board.ClassNames()); err != nil {
		return err
	}
	return nil
}

// Get returns one task.
func (s *Service) Get(ctx context.Context, id string) (*core.Task, error) {
	return s.provider.Get(ctx, id)
}

// Create validates and applies board defaults, then creates the task.
func (s *Service) Create(ctx context.Context, d core.Draft) (*core.Task, error) {
	if strings.TrimSpace(d.Title) == "" {
		return nil, fmt.Errorf("%w: a title is required", core.ErrInvalidInput)
	}
	if d.Status == "" {
		d.Status = s.opts.DefaultStatus
	}
	if d.Priority == "" {
		d.Priority = s.opts.DefaultPriority
	}
	if d.Class == "" {
		d.Class = s.opts.DefaultClass
	}
	var err error
	if d.Status, err = s.resolveStatus(d.Status); err != nil {
		return nil, err
	}
	if d.Priority, err = s.resolveOne("priority", d.Priority, s.board.Priorities); err != nil {
		return nil, err
	}
	if d.Class, err = s.resolveOne("class", d.Class, s.board.ClassNames()); err != nil {
		return nil, err
	}
	if d.Claim != nil && d.Claim.Agent != "" {
		d.Claim.Expires = s.now().Add(s.opts.ClaimTimeout)
	}
	return s.provider.Create(ctx, d)
}

// EditOptions carries the claim-related intent of an edit, which the service
// resolves against the task's current claim before touching the provider.
type EditOptions struct {
	// Agent claims (or renews the claim on) the task.
	Agent string
	// Release drops the claim.
	Release bool
	// Force skips the "claimed by another agent" guard.
	Force bool
	// Timestamp prefixes appended body text with an ISO-8601 line.
	Timestamp bool
}

// Update validates a patch against the board vocabulary, resolves claims and
// stamps Started/Completed on status transitions.
func (s *Service) Update(ctx context.Context, id string, p core.Patch, eo EditOptions) (*core.Task, error) {
	cur, err := s.provider.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	now := s.now()

	if err := s.checkClaim(cur, eo, now); err != nil {
		return nil, err
	}
	if p.Status != nil {
		st, err := s.resolveStatus(*p.Status)
		if err != nil {
			return nil, err
		}
		p.Status = &st
		s.stampTransition(cur, &p, st, now)
	}
	if p.Priority != nil {
		v, err := s.resolveOne("priority", *p.Priority, s.board.Priorities)
		if err != nil {
			return nil, err
		}
		p.Priority = &v
	}
	if p.Class != nil {
		v, err := s.resolveOne("class", *p.Class, s.board.ClassNames())
		if err != nil {
			return nil, err
		}
		p.Class = &v
	}
	if p.AppendBody != nil {
		text := *p.AppendBody
		if eo.Timestamp {
			text = now.Format(time.RFC3339) + "\n" + text
		}
		body := cur.Body
		if strings.TrimSpace(body) != "" {
			body = strings.TrimRight(body, "\n") + "\n\n"
		}
		merged := body + text
		p.Body = &merged
		p.AppendBody = nil
	}
	switch {
	case eo.Release:
		p.ReleaseClaim = true
		p.Claim = nil
	case eo.Agent != "":
		p.Claim = &core.Claim{Agent: eo.Agent, Expires: now.Add(s.opts.ClaimTimeout)}
	}
	if p.IsEmpty() {
		return cur, nil
	}
	if err := s.checkCapabilities(p); err != nil {
		return nil, err
	}
	return s.provider.Update(ctx, id, p)
}

// Move changes a task status, supporting absolute names as well as relative
// --next / --prev navigation.
func (s *Service) Move(ctx context.Context, id, status string, next, prev bool, eo EditOptions) (*core.Task, error) {
	cur, err := s.provider.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	target := status
	if next || prev {
		idx := s.board.StatusIndex(cur.Status)
		if idx < 0 {
			return nil, fmt.Errorf("%w: current status %q is not a board column", core.ErrUnknownStatus, cur.Status)
		}
		if next {
			if idx+1 >= len(s.board.Statuses) {
				return nil, fmt.Errorf("%w: %s is already the last column", core.ErrInvalidInput, cur.Status)
			}
			target = s.board.Statuses[idx+1].Name
		} else {
			if idx == 0 {
				return nil, fmt.Errorf("%w: %s is already the first column", core.ErrInvalidInput, cur.Status)
			}
			target = s.board.Statuses[idx-1].Name
		}
	}
	resolved, err := s.resolveStatus(target)
	if err != nil {
		return nil, err
	}
	if col, ok := s.board.Status(resolved); ok && col.RequireClaim && eo.Agent == "" && !cur.Claim.Active(s.now()) {
		return nil, fmt.Errorf("%w: column %q requires a claim (pass --claim AGENT)", core.ErrInvalidInput, resolved)
	}
	return s.Update(ctx, id, core.Patch{Status: &resolved}, eo)
}

// Delete removes or archives a task.
func (s *Service) Delete(ctx context.Context, id string) error {
	return s.provider.Delete(ctx, id)
}

// Summary aggregates the board for the board command.
func (s *Service) Summary(ctx context.Context, f core.Filter) (core.Summary, error) {
	tasks, err := s.List(ctx, f, "id", false)
	if err != nil {
		return core.Summary{}, err
	}
	return core.Summarize(s.board, tasks, s.now()), nil
}

// checkClaim refuses an agent-driven write on a task another agent holds.
// A plain human edit (no --claim) is never blocked: claims coordinate agents,
// they do not lock people out.
func (s *Service) checkClaim(cur *core.Task, eo EditOptions, now time.Time) error {
	if eo.Force || eo.Agent == "" || !cur.Claim.Active(now) {
		return nil
	}
	if strings.EqualFold(cur.Claim.Agent, eo.Agent) {
		return nil
	}
	return fmt.Errorf("%w: %s holds it until %s (use --force to override)",
		core.ErrClaimed, cur.Claim.Agent, cur.Claim.Expires.Format(time.RFC3339))
}

// stampTransition fills Started on the first move out of the intake column and
// Completed on a move into a terminal column, mirroring kanban-md behaviour.
func (s *Service) stampTransition(cur *core.Task, p *core.Patch, target string, now time.Time) {
	if strings.EqualFold(cur.Status, target) {
		return
	}
	if cur.Started == nil && s.board.IsInitial(cur.Status) && !s.board.IsInitial(target) {
		t := now
		p.Started = &t
	}
	if s.board.IsTerminal(target) && cur.Completed == nil {
		t := now
		p.Completed = &t
	}
	if !s.board.IsTerminal(target) && cur.Completed != nil {
		p.ClearCompleted = true
	}
}

func (s *Service) checkCapabilities(p core.Patch) error {
	caps := s.provider.Capabilities()
	missing := func(c core.Capability, what string) error {
		if !caps.Has(c) {
			return fmt.Errorf("%w: provider %s cannot store %s", core.ErrUnsupported, s.provider.Name(), what)
		}
		return nil
	}
	if len(p.AddDeps) > 0 || len(p.RemoveDeps) > 0 || p.ClearDeps {
		if err := missing(core.CapDependencies, "dependencies"); err != nil {
			return err
		}
	}
	if p.Parent != nil || p.ClearParent {
		if err := missing(core.CapParent, "a parent task"); err != nil {
			return err
		}
	}
	if p.Blocked != nil {
		if err := missing(core.CapBlocked, "a blocked reason"); err != nil {
			return err
		}
	}
	if p.Estimate != nil {
		if err := missing(core.CapEstimate, "an estimate"); err != nil {
			return err
		}
	}
	if p.Claim != nil || p.ReleaseClaim {
		if err := missing(core.CapClaims, "claims"); err != nil {
			return err
		}
	}
	return nil
}

// resolveStatus maps a user-typed status onto a board column, accepting an
// unambiguous prefix so `move 12 in-p` works.
func (s *Service) resolveStatus(v string) (string, error) {
	if v == "" {
		return "", fmt.Errorf("%w: a status is required", core.ErrInvalidInput)
	}
	names := s.board.StatusNames()
	out, err := ResolveVocabulary(v, names)
	if err != nil {
		return "", &core.InvalidValueError{Field: "status", Value: v, Allowed: names}
	}
	return out, nil
}

func (s *Service) resolveOne(field, v string, allowed []string) (string, error) {
	if v == "" || len(allowed) == 0 {
		return v, nil
	}
	out, err := ResolveVocabulary(v, allowed)
	if err != nil {
		return "", &core.InvalidValueError{Field: field, Value: v, Allowed: allowed}
	}
	return out, nil
}

// ResolveVocabulary matches v against allowed: exactly first, then by
// unambiguous prefix.
//
// Matching ignores case and word separators, so the column GitHub calls
// "In Progress" answers to `in-progress`, `in_progress` and `inprogress` —
// the spellings anyone coming from a file-based board will type.
func ResolveVocabulary(v string, allowed []string) (string, error) {
	needle := normalizeTerm(v)
	if needle == "" {
		return "", core.ErrInvalidInput
	}
	for _, a := range allowed {
		if normalizeTerm(a) == needle {
			return a, nil
		}
	}
	var hits []string
	for _, a := range allowed {
		if strings.HasPrefix(normalizeTerm(a), needle) {
			hits = append(hits, a)
		}
	}
	if len(hits) == 1 {
		return hits[0], nil
	}
	return "", core.ErrInvalidInput
}

// normalizeTerm lowercases a vocabulary term and drops the separators people
// vary on.
func normalizeTerm(v string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(v)) {
		switch r {
		case ' ', '-', '_', '\t':
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// PickOptions narrows what `yakanban pick` is willing to take.
type PickOptions struct {
	// Agent is the claim name; required.
	Agent string
	// Status restricts the columns to pick from. Empty means every
	// non-terminal column.
	Status string
	// Move optionally sends the picked task to another column.
	Move string
	// Tags restricts candidates to tasks carrying any of them.
	Tags []string
}

// Pick claims the highest-priority task nobody else holds.
//
// The backend has no compare-and-swap, so this is an optimistic claim: the
// candidate is claimed, then read back to confirm the claim is ours. A task
// another agent took between the listing and the write comes back as
// ErrClaimed and the next candidate is tried, which is what makes the loop
// safe to run from several agents at once.
func (s *Service) Pick(ctx context.Context, o PickOptions) (*core.Task, error) {
	if strings.TrimSpace(o.Agent) == "" {
		return nil, fmt.Errorf("%w: pick needs an agent name (--claim)", core.ErrInvalidInput)
	}
	filter := core.Filter{
		Tags:       o.Tags,
		Unclaimed:  true,
		NotBlocked: true,
		Unblocked:  true,
	}
	if o.Status != "" {
		status, err := s.resolveStatus(o.Status)
		if err != nil {
			return nil, err
		}
		filter.Statuses = []string{status}
	}
	candidates, err := s.List(ctx, filter, "id", false)
	if err != nil {
		return nil, err
	}
	if filter.Statuses == nil {
		open := candidates[:0]
		for _, t := range candidates {
			if !s.board.IsTerminal(t.Status) {
				open = append(open, t)
			}
		}
		candidates = open
	}
	// Highest priority first, oldest first within a priority. The id sort
	// above already ordered ties, and SortTasks is stable.
	core.SortTasks(candidates, "priority", true, s.board.StatusNames(), s.board.Priorities)

	var lastErr error
	for _, candidate := range candidates {
		patch := core.Patch{}
		if o.Move != "" {
			status, err := s.resolveStatus(o.Move)
			if err != nil {
				return nil, err
			}
			patch.Status = &status
		}
		task, err := s.Update(ctx, candidate.ID, patch, EditOptions{Agent: o.Agent})
		if err != nil {
			if errors.Is(err, core.ErrClaimed) {
				lastErr = err
				continue // somebody beat us to it between the list and the write
			}
			return nil, err
		}
		if !task.Claim.Active(s.now()) || !strings.EqualFold(task.Claim.Agent, o.Agent) {
			lastErr = fmt.Errorf("%w: %s took %s first", core.ErrClaimed, claimHolder(task), task.ID)
			continue
		}
		return task, nil
	}
	if lastErr != nil {
		return nil, fmt.Errorf("no task could be claimed: %w", lastErr)
	}
	return nil, fmt.Errorf("%w: no unblocked, unclaimed task to pick", core.ErrNotFound)
}

func claimHolder(t *core.Task) string {
	if t.Claim == nil || t.Claim.Agent == "" {
		return "another agent"
	}
	return t.Claim.Agent
}

// HandoffOptions describes how a task is parked for someone else.
type HandoffOptions struct {
	// Agent is the claim name of the agent handing the task over.
	Agent string
	// To is the column to park the task in, already resolved by the caller.
	To string
	// Note is appended to the task body.
	Note string
	// Block, when set, marks the task blocked with this reason.
	Block string
	// Release drops the claim after the handoff.
	Release bool
	// Timestamp prefixes the note with an ISO-8601 line.
	Timestamp bool
}

// Handoff parks a task: it moves it to the waiting column, appends a note
// explaining where the work stands, and optionally blocks it and lets go of
// the claim — all in a single write.
func (s *Service) Handoff(ctx context.Context, id string, o HandoffOptions) (*core.Task, error) {
	if strings.TrimSpace(o.Agent) == "" {
		return nil, fmt.Errorf("%w: handoff needs an agent name (--claim)", core.ErrInvalidInput)
	}
	status, err := s.resolveStatus(o.To)
	if err != nil {
		return nil, err
	}
	patch := core.Patch{Status: &status}
	if o.Note != "" {
		note := o.Note
		patch.AppendBody = &note
	}
	if o.Block != "" {
		reason := o.Block
		patch.Blocked = &reason
	}
	// The claim is renewed for the write and released afterwards, so the
	// handoff itself cannot be raced by another agent.
	task, err := s.Update(ctx, id, patch, EditOptions{Agent: o.Agent, Timestamp: o.Timestamp})
	if err != nil {
		return nil, err
	}
	if !o.Release {
		return task, nil
	}
	return s.Update(ctx, id, core.Patch{}, EditOptions{Agent: o.Agent, Release: true})
}
