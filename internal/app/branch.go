package app

import (
	"context"
	"fmt"
	"strings"
	"text/template"
	"unicode"

	"github.com/aramponi/yakanban/internal/core"
)

// BranchData is what the branch and worktree templates can refer to.
type BranchData struct {
	ID       string
	Slug     string
	Title    string
	Priority string
	Class    string
	Agent    string
	Board    string
	Repo     string
}

// BranchOptions describes a branch to attach to a task.
type BranchOptions struct {
	// Agent claims the task for the duration of the call.
	Agent string
	// Name overrides the configured template.
	Name string
	// BaseOID is the commit to start from; it must exist on the backend.
	BaseOID string
	// Repo feeds the templates.
	Repo string
	// Force writes even when another agent holds the claim.
	Force bool
}

// slugLimit keeps a generated branch name readable, and well inside the limits
// of every filesystem a worktree path might land on.
const slugLimit = 50

// BranchName renders the branch a task would get, without creating anything.
func (s *Service) BranchName(task core.Task, o BranchOptions) (string, error) {
	if strings.TrimSpace(o.Name) != "" {
		return o.Name, validateRefName(o.Name)
	}
	name, err := s.render("branch", s.opts.BranchTemplate, task, o)
	if err != nil {
		return "", err
	}
	return name, validateRefName(name)
}

// WorktreePath renders the working directory a task would get.
func (s *Service) WorktreePath(task core.Task, o BranchOptions) (string, error) {
	return s.render("worktree", s.opts.WorktreeTemplate, task, o)
}

func (s *Service) render(what, text string, task core.Task, o BranchOptions) (string, error) {
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("%w: no %s template configured (set branching.templates.%s in .yakanban.yml)",
			core.ErrInvalidInput, what, what)
	}
	tmpl, err := template.New(what).Option("missingkey=error").Parse(text)
	if err != nil {
		return "", fmt.Errorf("%w: the %s template does not parse: %v", core.ErrInvalidInput, what, err)
	}
	data := BranchData{
		ID:       task.ID,
		Slug:     Slugify(task.Title, slugLimit),
		Title:    task.Title,
		Priority: task.Priority,
		Class:    task.Class,
		Agent:    o.Agent,
		Board:    s.board.Name,
		Repo:     o.Repo,
	}
	var out strings.Builder
	if err := tmpl.Execute(&out, data); err != nil {
		return "", fmt.Errorf("%w: the %s template failed: %v", core.ErrInvalidInput, what, err)
	}
	rendered := strings.TrimSpace(out.String())
	if rendered == "" {
		return "", fmt.Errorf("%w: the %s template rendered to nothing", core.ErrInvalidInput, what)
	}
	return rendered, nil
}

// CreateBranch attaches a branch to a task and returns it.
//
// The name is decided here and passed to the backend, never read back from it:
// an agent must know the branch name before the branch exists, so it can
// create its local branch from the same commit without fetching.
func (s *Service) CreateBranch(ctx context.Context, id string, o BranchOptions) (*core.Branch, error) {
	brancher, ok := core.AsBrancher(s.provider)
	if !ok {
		return nil, fmt.Errorf("%w: provider %s cannot attach a branch to a task", core.ErrUnsupported, s.provider.Name())
	}
	if strings.TrimSpace(o.BaseOID) == "" {
		return nil, fmt.Errorf("%w: a base commit is required", core.ErrInvalidInput)
	}
	task, err := s.provider.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := s.checkClaim(task, EditOptions{Agent: o.Agent, Force: o.Force}, s.now()); err != nil {
		return nil, err
	}
	name, err := s.BranchName(*task, o)
	if err != nil {
		return nil, err
	}
	return brancher.CreateBranch(ctx, id, core.BranchRequest{Name: name, BaseOID: o.BaseOID})
}

// Branches lists the branches attached to a task.
func (s *Service) Branches(ctx context.Context, id string) ([]core.Branch, error) {
	brancher, ok := core.AsBrancher(s.provider)
	if !ok {
		return nil, fmt.Errorf("%w: provider %s does not track branches", core.ErrUnsupported, s.provider.Name())
	}
	return brancher.Branches(ctx, id)
}

// UnlinkBranch detaches a branch from its task without deleting the ref.
func (s *Service) UnlinkBranch(ctx context.Context, branchID string) error {
	brancher, ok := core.AsBrancher(s.provider)
	if !ok {
		return fmt.Errorf("%w: provider %s does not track branches", core.ErrUnsupported, s.provider.Name())
	}
	return brancher.UnlinkBranch(ctx, branchID)
}

// Slugify turns a task title into the kebab-case part of a branch name.
func Slugify(title string, limit int) string {
	var b strings.Builder
	lastDash := true // suppresses a leading dash
	for _, r := range strings.ToLower(title) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			lastDash = false
		case !lastDash:
			b.WriteRune('-')
			lastDash = true
		}
	}
	slug := strings.Trim(b.String(), "-")
	if limit > 0 && len(slug) > limit {
		slug = slug[:limit]
		// Cutting mid-word is fine; cutting on a dash leaves a trailing one.
		slug = strings.TrimRight(slug, "-")
	}
	return slug
}

// validateRefName rejects names git would refuse, before a round trip to the
// backend turns them into an opaque API error.
func validateRefName(name string) error {
	reject := func(reason string) error {
		return fmt.Errorf("%w: %q is not a valid branch name: %s", core.ErrInvalidInput, name, reason)
	}
	switch {
	case name == "":
		return reject("it is empty")
	case strings.HasPrefix(name, "/"), strings.HasSuffix(name, "/"):
		return reject("it starts or ends with a slash")
	case strings.HasPrefix(name, "-"):
		return reject("it starts with a dash")
	case strings.Contains(name, ".."):
		return reject("it contains ..")
	case strings.Contains(name, "//"):
		return reject("it contains an empty path segment")
	case strings.HasSuffix(name, ".lock"), strings.Contains(name, ".lock/"):
		return reject("a path component ends with .lock")
	case strings.HasSuffix(name, "."):
		return reject("it ends with a dot")
	case name == "@":
		return reject("@ alone is reserved")
	case strings.Contains(name, "@{"):
		return reject("it contains @{")
	}
	for _, r := range name {
		if unicode.IsSpace(r) || unicode.IsControl(r) || strings.ContainsRune("~^:?*[\\\x7f", r) {
			return reject(fmt.Sprintf("%q is not allowed in a ref name", r))
		}
	}
	return nil
}
