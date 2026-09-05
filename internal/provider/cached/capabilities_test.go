package cached

import (
	"context"
	"github.com/aramponi/yakanban/internal/cache"
	"github.com/aramponi/yakanban/internal/core"
	"testing"
	"time"
)

type runtimeBoard struct {
	countingProvider
	caps core.CapabilitySet
}

func (p *runtimeBoard) Board(context.Context) (*core.BoardInfo, error) {
	p.boards++
	return &core.BoardInfo{Name: "runtime", Capabilities: &p.caps}, nil
}

func TestCapabilitiesSurviveCacheAndRefresh(t *testing.T) {
	store := cache.New(t.TempDir(), time.Minute, true)
	inner := &runtimeBoard{caps: core.CapabilitySet{Reasons: map[core.Capability]string{core.CapDependencies: "Premium required"}}}
	ctx := context.Background()
	first, err := core.ResolveCapabilities(ctx, New(inner, store))
	if err != nil || first.Has(core.CapDependencies) {
		t.Fatalf("first = %+v, %v", first, err)
	}
	inner.caps.Supported = core.CapDependencies
	// A new adapter instance must use the cached metadata, including the reason.
	second, err := core.ResolveCapabilities(ctx, New(inner, store))
	if err != nil || second.Has(core.CapDependencies) || second.Reasons[core.CapDependencies] != "Premium required" {
		t.Fatalf("cache lost capability information: %+v, %v", second, err)
	}
	if inner.boards != 1 {
		t.Fatalf("schema fetched %d times", inner.boards)
	}
	if err := store.Invalidate(); err != nil {
		t.Fatal(err)
	}
	third, err := core.ResolveCapabilities(ctx, New(inner, store))
	if err != nil || !third.Has(core.CapDependencies) || inner.boards != 2 {
		t.Fatalf("refresh did not resolve updated capabilities: %+v, %v", third, err)
	}
}

func TestOldBoardCacheIsNotUsedAsRuntimeCapabilities(t *testing.T) {
	store := cache.New(t.TempDir(), time.Minute, true)
	inner := &runtimeBoard{}
	if err := store.Put(inner.Name()+":board", core.BoardInfo{Name: "old"}); err != nil {
		t.Fatal(err)
	}
	_, err := core.ResolveCapabilities(context.Background(), New(inner, store))
	if err != nil || inner.boards != 1 {
		t.Fatalf("old schema was used: boards=%d, %v", inner.boards, err)
	}
}
