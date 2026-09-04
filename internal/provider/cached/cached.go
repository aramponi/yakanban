// Package cached decorates a core.Provider with the local read cache.
//
// Only List and Board are cached. Get stays a live call because the service
// reads a task right before patching it, and a stale read there would
// silently clobber someone else's change.
package cached

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/aramponi/yakanban/internal/cache"
	"github.com/aramponi/yakanban/internal/core"
)

// Provider wraps another provider with a cache store.
type Provider struct {
	inner core.Provider
	store *cache.Store
}

// New returns inner decorated with store. A disabled store is a pass-through.
func New(inner core.Provider, store *cache.Store) *Provider {
	return &Provider{inner: inner, store: store}
}

// Name returns the wrapped provider name, so configuration keys still match.
func (p *Provider) Name() string { return p.inner.Name() }

// Capabilities delegates to the wrapped provider.
func (p *Provider) Capabilities() core.Capability { return p.inner.Capabilities() }

// Unwrap exposes the decorated provider.
func (p *Provider) Unwrap() core.Provider { return p.inner }

// Board serves the board description from cache when fresh.
func (p *Provider) Board(ctx context.Context) (*core.BoardInfo, error) {
	key := p.inner.Name() + ":board"
	var hit core.BoardInfo
	if _, ok := p.store.Get(key, &hit); ok {
		return &hit, nil
	}
	board, err := p.inner.Board(ctx)
	if err != nil {
		return nil, err
	}
	_ = p.store.Put(key, board)
	return board, nil
}

// List serves the task list from cache when fresh.
func (p *Provider) List(ctx context.Context, filter core.Filter) ([]core.Task, error) {
	key, err := listKey(p.inner.Name(), filter)
	if err != nil {
		return nil, err
	}
	var hit []core.Task
	if _, ok := p.store.Get(key, &hit); ok {
		return hit, nil
	}
	tasks, err := p.inner.List(ctx, filter)
	if err != nil {
		return nil, err
	}
	_ = p.store.Put(key, tasks)
	return tasks, nil
}

// Get always hits the backend: it feeds read-modify-write updates.
func (p *Provider) Get(ctx context.Context, id string) (*core.Task, error) {
	return p.inner.Get(ctx, id)
}

// Create writes through and drops the cache.
func (p *Provider) Create(ctx context.Context, d core.Draft) (*core.Task, error) {
	t, err := p.inner.Create(ctx, d)
	if err != nil {
		return nil, err
	}
	_ = p.Invalidate()
	return t, nil
}

// Update writes through and drops the cache.
func (p *Provider) Update(ctx context.Context, id string, patch core.Patch) (*core.Task, error) {
	t, err := p.inner.Update(ctx, id, patch)
	if err != nil {
		return nil, err
	}
	_ = p.Invalidate()
	return t, nil
}

// Delete writes through and drops the cache.
func (p *Provider) Delete(ctx context.Context, id string) error {
	if err := p.inner.Delete(ctx, id); err != nil {
		return err
	}
	return p.Invalidate()
}

// Invalidate drops every cached entry for this board.
func (p *Provider) Invalidate() error { return p.store.Invalidate() }

// Bootstrap forwards to the wrapped provider when it can provision a board.
func (p *Provider) Bootstrap(ctx context.Context, opts core.BootstrapOptions) (*core.BoardInfo, error) {
	b, ok := p.inner.(core.Bootstrapper)
	if !ok {
		return nil, fmt.Errorf("%w: %s cannot create a board", core.ErrUnsupported, p.inner.Name())
	}
	board, err := b.Bootstrap(ctx, opts)
	if err != nil {
		return nil, err
	}
	_ = p.Invalidate()
	return board, nil
}

// ConfigSettings forwards the wrapped provider's persistable settings.
func (p *Provider) ConfigSettings() map[string]any {
	if w, ok := p.inner.(core.SettingsWriter); ok {
		return w.ConfigSettings()
	}
	return nil
}

// listKey derives a stable cache key from the filter, so two different
// listings cannot read each other's entry.
func listKey(name string, f core.Filter) (string, error) {
	raw, err := json.Marshal(f)
	if err != nil {
		return "", err
	}
	return name + ":list:" + string(raw), nil
}
