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

	// Capabilities reports the optional model features this backend supports.
	Capabilities() Capability

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
