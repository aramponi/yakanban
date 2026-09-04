// Package registry wires provider names to their adapters.
//
// It is the only place that knows every backend, which keeps the CLI and the
// domain free of provider imports: adding Jira, Plane or Linear means adding
// one adapter package and one line here.
package registry

import (
	"fmt"
	"sort"
	"strings"

	"github.com/aramponi/yakanban/internal/cache"
	"github.com/aramponi/yakanban/internal/config"
	"github.com/aramponi/yakanban/internal/core"
	"github.com/aramponi/yakanban/internal/provider/github"
)

// Factory builds a provider from the board descriptor.
type Factory func(cfg *config.Config, store *cache.Store, userAgent string) (core.Provider, error)

var factories = map[string]Factory{
	github.ProviderName: func(cfg *config.Config, store *cache.Store, ua string) (core.Provider, error) {
		settings, err := github.ParseSettings(cfg.Settings(github.ProviderName))
		if err != nil {
			return nil, err
		}
		return github.New(settings, cfg.Board.Name, ua, store)
	},
}

// Names lists the registered providers, sorted.
func Names() []string {
	out := make([]string, 0, len(factories))
	for name := range factories {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// Open builds the provider named by the descriptor.
func Open(cfg *config.Config, store *cache.Store, userAgent string) (core.Provider, error) {
	f, ok := factories[strings.ToLower(cfg.Provider)]
	if !ok {
		return nil, fmt.Errorf("%w: unknown provider %q (available: %s)",
			core.ErrInvalidInput, cfg.Provider, strings.Join(Names(), ", "))
	}
	return f(cfg, store, userAgent)
}
