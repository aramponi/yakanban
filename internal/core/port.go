package core

import "context"

// Capability advertises the optional parts of the model a provider supports,
// so the CLI can fail fast with a clear message instead of silently dropping
// data on a backend that cannot store it.
type Capability uint32

const (
	CapClaims Capability = 1 << iota
	CapDependencies
	CapParent
	CapBlocked
	CapEstimate
	CapClass
	CapDueDate
	CapDelete
	CapArchive
	CapLinkedBranch
	CapWorkflowDates
)

// Has reports whether c includes every capability in want.
func (c Capability) Has(want Capability) bool { return c&want == want }

// Provider is the driven port: everything yakanban needs from a ticketing
// backend. Adapters live under internal/provider/<name>.
//
// Implementations must return errors wrapping the sentinels in errors.go so
// the CLI can react to them.
type Provider interface {
	// Name is the provider key used in configuration, e.g. "github".
	Name() string

	// Board returns the live board description (columns, field vocabulary).
	Board(ctx context.Context) (*BoardInfo, error)

	// List returns the tasks matching filter. Providers may ignore parts of
	// the filter; the caller re-applies Filter.Match locally.
	List(ctx context.Context, filter Filter) ([]Task, error)

	// Get returns a single task by its provider-native ID.
	Get(ctx context.Context, id string) (*Task, error)

	// Create opens a new task and returns it as stored by the backend.
	Create(ctx context.Context, draft Draft) (*Task, error)

	// Update applies a partial change and returns the stored result.
	Update(ctx context.Context, id string, patch Patch) (*Task, error)

	// Delete removes a task, or archives it when the backend has no real
	// delete. It must return ErrUnsupported if it can do neither.
	Delete(ctx context.Context, id string) error
}

// Bootstrapper is implemented by providers that can provision their own
// board (create the project, the custom fields, the labels...).
type Bootstrapper interface {
	Bootstrap(ctx context.Context, opts BootstrapOptions) (*BoardInfo, error)
}

// BootstrapOptions carries what `yakanban init` knows about the board to set up.
type BootstrapOptions struct {
	Name       string
	Statuses   []Status
	Priorities []string
	Classes    []Class
	// Options holds provider-specific switches parsed from --set key=value.
	Options map[string]string
}

// Branch is a source branch attached to a task by the backend — GitHub's
// linked branches, shown in an issue's Development section.
type Branch struct {
	// ID is the backend's handle on the link, needed to undo it.
	ID string `json:"id"`
	// Name is the branch name without its ref prefix.
	Name string `json:"name"`
	// Ref is the fully qualified ref, e.g. refs/heads/12-fix-login.
	Ref string `json:"ref,omitempty"`
	// OID is the commit the branch starts at.
	OID string `json:"oid,omitempty"`
}

// BranchRequest asks a backend to attach a branch to a task.
type BranchRequest struct {
	// Name is chosen by the caller. Backends that would otherwise invent one
	// must use this instead: an agent has to know the name before the branch
	// exists, so it never has to fetch to discover it.
	Name string
	// BaseOID is the commit the branch starts at. It must already exist on
	// the backend.
	BaseOID string
}

// Brancher is implemented by providers whose backend can attach a branch to a
// task. It is deliberately not part of Provider: Jira and Linear have branch
// integrations, but not this model, and the domain must not assume every
// tracker sits on top of a git forge.
type Brancher interface {
	// CreateBranch creates the branch and attaches it to the task.
	CreateBranch(ctx context.Context, id string, req BranchRequest) (*Branch, error)
	// Branches lists the branches attached to a task.
	Branches(ctx context.Context, id string) ([]Branch, error)
	// UnlinkBranch detaches a branch from a task. It does not delete the ref;
	// callers must say so, because the two are easy to confuse.
	UnlinkBranch(ctx context.Context, branchID string) error
}

// Unwrapper is implemented by decorators such as the cache, so an optional
// interface can be looked for on the provider they wrap.
type Unwrapper interface {
	Unwrap() Provider
}

// AsBrancher returns the Brancher behind p, seeing through decorators.
//
// A decorator must not answer for a capability its inner provider lacks, so
// the check walks down rather than testing the outermost value.
func AsBrancher(p Provider) (Brancher, bool) {
	for p != nil {
		if b, ok := p.(Brancher); ok {
			return b, true
		}
		u, ok := p.(Unwrapper)
		if !ok {
			return nil, false
		}
		p = u.Unwrap()
	}
	return nil, false
}

// SettingsWriter is implemented by providers that can hand back the
// configuration block to persist after a bootstrap — typically the identifier
// of a resource they just created.
type SettingsWriter interface {
	ConfigSettings() map[string]any
}

// Invalidator is implemented by adapters wrapping a cache.
type Invalidator interface {
	Invalidate() error
}
