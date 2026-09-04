package cached

import (
	"context"
	"testing"
	"time"

	"github.com/aramponi/yakanban/internal/cache"
	"github.com/aramponi/yakanban/internal/core"
)

// countingProvider counts the calls that actually reach the backend.
type countingProvider struct {
	lists, gets, boards int
}

func (c *countingProvider) Name() string                  { return "counting" }
func (c *countingProvider) Capabilities() core.Capability { return core.CapClaims }

func (c *countingProvider) Board(context.Context) (*core.BoardInfo, error) {
	c.boards++
	return &core.BoardInfo{Name: "b", Statuses: []core.Status{{Name: "Todo"}}}, nil
}

func (c *countingProvider) List(context.Context, core.Filter) ([]core.Task, error) {
	c.lists++
	return []core.Task{{ID: "1", Status: "Todo"}}, nil
}

func (c *countingProvider) Get(_ context.Context, id string) (*core.Task, error) {
	c.gets++
	return &core.Task{ID: id, Status: "Todo"}, nil
}

func (c *countingProvider) Create(context.Context, core.Draft) (*core.Task, error) {
	return &core.Task{ID: "2"}, nil
}

func (c *countingProvider) Update(_ context.Context, id string, _ core.Patch) (*core.Task, error) {
	return &core.Task{ID: id}, nil
}

func (c *countingProvider) Delete(context.Context, string) error { return nil }

func newCached(t *testing.T) (*Provider, *countingProvider) {
	t.Helper()
	inner := &countingProvider{}
	return New(inner, cache.New(t.TempDir(), time.Minute, true)), inner
}

func TestListIsServedFromTheCache(t *testing.T) {
	p, inner := newCached(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if _, err := p.List(ctx, core.Filter{}); err != nil {
			t.Fatalf("List: %v", err)
		}
	}
	if inner.lists != 1 {
		t.Fatalf("backend was called %d times, want 1", inner.lists)
	}
}

func TestDifferentFiltersAreCachedApart(t *testing.T) {
	p, inner := newCached(t)
	ctx := context.Background()

	if _, err := p.List(ctx, core.Filter{Statuses: []string{"Todo"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.List(ctx, core.Filter{Statuses: []string{"Done"}}); err != nil {
		t.Fatal(err)
	}
	if inner.lists != 2 {
		t.Fatalf("backend was called %d times, want one per distinct filter", inner.lists)
	}
}

func TestGetAlwaysHitsTheBackend(t *testing.T) {
	p, inner := newCached(t)
	ctx := context.Background()

	for i := 0; i < 2; i++ {
		if _, err := p.Get(ctx, "1"); err != nil {
			t.Fatalf("Get: %v", err)
		}
	}
	if inner.gets != 2 {
		t.Fatalf("Get was served from the cache (%d backend calls); read-modify-write would race", inner.gets)
	}
}

func TestWritesInvalidateTheCache(t *testing.T) {
	ctx := context.Background()
	cases := map[string]func(p *Provider) error{
		"create": func(p *Provider) error { _, err := p.Create(ctx, core.Draft{Title: "x"}); return err },
		"update": func(p *Provider) error { _, err := p.Update(ctx, "1", core.Patch{}); return err },
		"delete": func(p *Provider) error { return p.Delete(ctx, "1") },
	}
	for name, write := range cases {
		t.Run(name, func(t *testing.T) {
			p, inner := newCached(t)
			if _, err := p.List(ctx, core.Filter{}); err != nil {
				t.Fatal(err)
			}
			if err := write(p); err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			if _, err := p.List(ctx, core.Filter{}); err != nil {
				t.Fatal(err)
			}
			if inner.lists != 2 {
				t.Fatalf("the cache survived a %s; stale reads would follow", name)
			}
		})
	}
}

func TestDisabledCacheIsAPassThrough(t *testing.T) {
	inner := &countingProvider{}
	p := New(inner, cache.New(t.TempDir(), time.Minute, false))
	ctx := context.Background()

	for i := 0; i < 2; i++ {
		if _, err := p.List(ctx, core.Filter{}); err != nil {
			t.Fatal(err)
		}
		if _, err := p.Board(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if inner.lists != 2 || inner.boards != 2 {
		t.Fatalf("calls = %d lists / %d boards, want no caching", inner.lists, inner.boards)
	}
}
