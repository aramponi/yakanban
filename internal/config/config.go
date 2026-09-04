// Package config loads and saves the .yakanban.yml board descriptor.
//
// The file is meant to be committed: it pins the board vocabulary (columns,
// priorities, classes) and says which backend holds the source of truth.
// Nothing secret goes in it — credentials come from the environment or gh.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/aramponi/yakanban/internal/core"
)

// FileName is the descriptor looked up from the working directory upwards.
const FileName = ".yakanban.yml"

// DirName is the local working directory holding the read cache.
const DirName = ".yakanban"

// Version is the current descriptor schema version.
const Version = 1

// Config is the on-disk board descriptor.
type Config struct {
	Version  int    `yaml:"version"`
	Provider string `yaml:"provider"`

	Board      Board         `yaml:"board"`
	Statuses   []core.Status `yaml:"statuses"`
	Priorities []string      `yaml:"priorities,omitempty"`
	Classes    []core.Class  `yaml:"classes,omitempty"`
	Defaults   Defaults      `yaml:"defaults"`

	ClaimTimeout Duration `yaml:"claim_timeout,omitempty"`
	Cache        Cache    `yaml:"cache,omitempty"`

	// Providers holds the settings of each backend, keyed by provider name,
	// so a board can be re-pointed without rewriting the whole file.
	Providers map[string]map[string]any `yaml:"providers,omitempty"`

	// path is where this config was loaded from; not serialised.
	path string `yaml:"-"`
}

// Board names the board.
type Board struct {
	Name string `yaml:"name"`
}

// Defaults are applied to newly created tasks.
type Defaults struct {
	Status   string `yaml:"status,omitempty"`
	Priority string `yaml:"priority,omitempty"`
	Class    string `yaml:"class,omitempty"`
}

// Cache configures the local read-through cache.
type Cache struct {
	Enabled bool     `yaml:"enabled"`
	TTL     Duration `yaml:"ttl,omitempty"`
}

// Duration is a time.Duration that marshals as "90s" rather than nanoseconds.
type Duration time.Duration

// UnmarshalYAML parses a Go duration string.
func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	var s string
	if err := value.Decode(&s); err != nil {
		return err
	}
	if strings.TrimSpace(s) == "" {
		*d = 0
		return nil
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}
	*d = Duration(parsed)
	return nil
}

// MarshalYAML renders the duration back as a string.
func (d Duration) MarshalYAML() (any, error) { return time.Duration(d).String(), nil }

// Duration converts back to a time.Duration.
func (d Duration) Duration() time.Duration { return time.Duration(d) }

// Default returns the descriptor written by `yakanban init`, modelled on the
// kanban-md default board so muscle memory carries over.
func Default(name, provider string) *Config {
	return &Config{
		Version:  Version,
		Provider: provider,
		Board:    Board{Name: name},
		Statuses: []core.Status{
			{Name: "Backlog", Initial: true},
			{Name: "Todo"},
			{Name: "In Progress", RequireClaim: true},
			{Name: "Review", RequireClaim: true},
			{Name: "Done", Terminal: true},
		},
		Priorities: []string{"low", "medium", "high", "critical"},
		Classes: []core.Class{
			{Name: "expedite", WIPLimit: 1, BypassColumnWIP: true},
			{Name: "fixed-date"},
			{Name: "standard"},
			{Name: "intangible"},
		},
		Defaults:     Defaults{Status: "Backlog", Priority: "medium", Class: "standard"},
		ClaimTimeout: Duration(time.Hour),
		Cache:        Cache{Enabled: true, TTL: Duration(60 * time.Second)},
		Providers:    map[string]map[string]any{},
	}
}

// BoardInfo projects the descriptor onto the domain board description.
func (c *Config) BoardInfo() core.BoardInfo {
	return core.BoardInfo{
		Name:       c.Board.Name,
		Provider:   c.Provider,
		Statuses:   c.Statuses,
		Priorities: c.Priorities,
		Classes:    c.Classes,
	}
}

// Path returns the file this config was loaded from.
func (c *Config) Path() string { return c.path }

// Root returns the directory holding the descriptor.
func (c *Config) Root() string { return filepath.Dir(c.path) }

// CacheDir returns the local cache directory for this board.
func (c *Config) CacheDir() string { return filepath.Join(c.Root(), DirName, "cache") }

// Settings returns the settings block of a provider, never nil.
func (c *Config) Settings(provider string) map[string]any {
	if c.Providers == nil {
		c.Providers = map[string]map[string]any{}
	}
	if c.Providers[provider] == nil {
		c.Providers[provider] = map[string]any{}
	}
	return c.Providers[provider]
}

// Find walks up from dir looking for a descriptor and returns its path.
func Find(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	for {
		candidate := filepath.Join(abs, FileName)
		if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
			return candidate, nil
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			return "", fmt.Errorf("%w: no %s found in %s or any parent directory (run: yakanban init)",
				core.ErrNotConfigured, FileName, dir)
		}
		abs = parent
	}
}

// Load reads the descriptor found from dir upwards.
func Load(dir string) (*Config, error) {
	path, err := Find(dir)
	if err != nil {
		return nil, err
	}
	return LoadFile(path)
}

// LoadFile reads a descriptor from an explicit path.
func LoadFile(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Config
	if err := yaml.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	if c.Version > Version {
		return nil, fmt.Errorf("%s was written by a newer yakanban (version %d, this binary understands %d)",
			path, c.Version, Version)
	}
	if c.Provider == "" {
		return nil, fmt.Errorf("%s: provider is required", path)
	}
	if len(c.Statuses) == 0 {
		return nil, fmt.Errorf("%s: at least one status is required", path)
	}
	c.path = path
	return &c, nil
}

// Save writes the descriptor to path, creating parent directories.
func (c *Config) Save(path string) error {
	if path == "" {
		path = c.path
	}
	if path == "" {
		return fmt.Errorf("no path to save the configuration to")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	out, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	header := "# yakanban board descriptor — https://github.com/aramponi/yakanban\n" +
		"# The backend below is the source of truth; this file only pins the vocabulary.\n"
	if err := os.WriteFile(path, append([]byte(header), out...), 0o644); err != nil {
		return err
	}
	c.path = path
	return nil
}
