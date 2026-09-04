package github

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/aramponi/yakanban/internal/core"
)

// ProviderName is the key used in .yakanban.yml under `provider:`.
const ProviderName = "github"

// Settings is the `providers.github` block of the board descriptor.
type Settings struct {
	// Host is the GitHub host, for GitHub Enterprise Server. Empty means github.com.
	Host string `json:"host,omitempty"`
	// Owner is the user or organization owning both the repo and the project.
	Owner string `json:"owner"`
	// Repo is the repository where issues are opened.
	Repo string `json:"repo"`
	// ProjectNumber is the Projects v2 number, as shown in the project URL.
	ProjectNumber int `json:"project_number"`
}

// ParseSettings reads the provider block of the config into Settings.
func ParseSettings(raw map[string]any) (Settings, error) {
	s := Settings{
		Host:  stringOf(raw["host"]),
		Owner: stringOf(raw["owner"]),
		Repo:  stringOf(raw["repo"]),
	}
	switch v := raw["project_number"].(type) {
	case nil:
	case int:
		s.ProjectNumber = v
	case int64:
		s.ProjectNumber = int(v)
	case float64:
		s.ProjectNumber = int(v)
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil {
			return s, fmt.Errorf("%w: providers.github.project_number %q is not a number", core.ErrInvalidInput, v)
		}
		s.ProjectNumber = n
	default:
		return s, fmt.Errorf("%w: providers.github.project_number has unexpected type %T", core.ErrInvalidInput, v)
	}
	return s, s.Validate()
}

// Validate reports missing required settings with an actionable message.
func (s Settings) Validate() error {
	var missing []string
	if s.Owner == "" {
		missing = append(missing, "owner")
	}
	if s.Repo == "" {
		missing = append(missing, "repo")
	}
	if len(missing) > 0 {
		return fmt.Errorf("%w: providers.github is missing %s in .yakanban.yml (run `yakanban init` to set it up)",
			core.ErrNotConfigured, strings.Join(missing, ", "))
	}
	return nil
}

// ValidateBoard additionally requires a project to have been selected. It is
// checked when reaching the board, not at construction, so `yakanban init`
// can run before a project number is known.
func (s Settings) ValidateBoard() error {
	if err := s.Validate(); err != nil {
		return err
	}
	if s.ProjectNumber <= 0 {
		return fmt.Errorf("%w: providers.github.project_number is not set in .yakanban.yml (run `yakanban init`)",
			core.ErrNotConfigured)
	}
	return nil
}

// ToMap renders the settings back into the config block.
func (s Settings) ToMap() map[string]any {
	m := map[string]any{
		"owner":          s.Owner,
		"repo":           s.Repo,
		"project_number": s.ProjectNumber,
	}
	if s.Host != "" && s.Host != defaultHost {
		m["host"] = s.Host
	}
	return m
}

func stringOf(v any) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}
