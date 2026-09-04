package config

import (
	"fmt"
	"sort"
	"strings"

	"github.com/aramponi/yakanban/internal/core"
)

// The branching models yakanban ships. `custom` is the absence of a preset.
const (
	ModelCustom     = "custom"
	ModelTrunkBased = "trunk-based"
	ModelGitHubFlow = "github-flow"
	ModelGitFlow    = "git-flow"
	ModelGitLabFlow = "gitlab-flow"
	ModelOneFlow    = "oneflow"
)

// Preset describes a branching model, for the `init` prompt and for `config`.
type Preset struct {
	Name        string
	Description string
	policy      core.BranchPolicy
	templates   Templates
}

// presets are ordered as the init prompt shows them: the two most common
// first, then the ones that add long-lived branches.
var presets = []Preset{
	{
		Name:        ModelTrunkBased,
		Description: "short-lived branches off main, merged daily",
		policy: core.BranchPolicy{
			Base: "main", Integration: "main", DefaultType: "", DeleteAfterMerge: true,
		},
		templates: Templates{Branch: "{{.ID}}-{{.Slug}}", Worktree: "../{{.Repo}}-task-{{.ID}}"},
	},
	{
		Name:        ModelGitHubFlow,
		Description: "trunk-based, every change through a pull request",
		policy: core.BranchPolicy{
			Base: "main", Integration: "main", DefaultType: "", DeleteAfterMerge: true,
		},
		templates: Templates{Branch: "{{.ID}}-{{.Slug}}", Worktree: "../{{.Repo}}-task-{{.ID}}"},
	},
	{
		Name:        ModelGitFlow,
		Description: "develop plus feature/, fix/ and hotfix/ branches",
		policy: core.BranchPolicy{
			Base: "develop", Integration: "develop", DefaultType: "feature", DeleteAfterMerge: true,
			Rules: []core.BranchRule{
				{Priority: "critical", Type: "hotfix", Base: "main"},
				{Tag: "bug", Type: "fix"},
			},
			BackMergeWarning: map[string]string{
				"hotfix": "a git flow hotfix must also be merged back into develop; yakanban does not do that for you",
			},
		},
		templates: Templates{Branch: "{{.Type}}/{{.ID}}-{{.Slug}}", Worktree: "../{{.Repo}}-task-{{.ID}}"},
	},
	{
		Name:        ModelGitLabFlow,
		Description: "main plus long-lived environment branches downstream of it",
		policy: core.BranchPolicy{
			Base: "main", Integration: "main", DefaultType: "", DeleteAfterMerge: true,
		},
		templates: Templates{Branch: "{{.ID}}-{{.Slug}}", Worktree: "../{{.Repo}}-task-{{.ID}}"},
	},
	{
		Name:        ModelOneFlow,
		Description: "git flow without develop: everything branches off main",
		policy: core.BranchPolicy{
			Base: "main", Integration: "main", DefaultType: "feature", DeleteAfterMerge: true,
			Rules: []core.BranchRule{
				{Priority: "critical", Type: "hotfix"},
				{Tag: "bug", Type: "fix"},
			},
		},
		templates: Templates{Branch: "{{.Type}}/{{.ID}}-{{.Slug}}", Worktree: "../{{.Repo}}-task-{{.ID}}"},
	},
}

// Presets lists the shipped models, in prompt order.
func Presets() []Preset { return presets }

// ModelNames lists the accepted values of branching.model.
func ModelNames() []string {
	out := make([]string, 0, len(presets)+1)
	for _, p := range presets {
		out = append(out, p.Name)
	}
	out = append(out, ModelCustom)
	return out
}

// FindPreset returns a shipped model by name.
func FindPreset(name string) (Preset, bool) {
	for _, p := range presets {
		if strings.EqualFold(p.Name, name) {
			return p, true
		}
	}
	return Preset{}, false
}

// Apply writes the preset into a descriptor, leaving anything the user already
// set in place.
func (p Preset) Apply(b *Branching) {
	b.Model = p.Name
	if b.Base == "" {
		b.Base = p.policy.Base
	}
	if b.Integration == "" {
		b.Integration = p.policy.Integration
	}
	if b.Templates.Branch == "" {
		b.Templates.Branch = p.templates.Branch
	}
	if b.Templates.Worktree == "" {
		b.Templates.Worktree = p.templates.Worktree
	}
	if b.Types.Default == "" {
		b.Types.Default = p.policy.DefaultType
	}
	if len(b.Types.Match) == 0 {
		for _, r := range p.policy.Rules {
			b.Types.Match = append(b.Types.Match, r)
		}
	}
}

// Policy resolves the descriptor into the domain's branch policy: the preset
// named by `model` supplies the defaults, and every key written next to it is
// an override. `custom`, or no model at all, means no defaults.
func (b Branching) Policy() (core.BranchPolicy, error) {
	model := strings.TrimSpace(b.Model)
	policy := core.BranchPolicy{Model: model}
	if model != "" && !strings.EqualFold(model, ModelCustom) {
		preset, ok := FindPreset(model)
		if !ok {
			names := ModelNames()
			sort.Strings(names)
			return core.BranchPolicy{}, &core.InvalidValueError{Field: "branching.model", Value: model, Allowed: names}
		}
		policy = preset.policy
		policy.Model = preset.Name
	}
	if b.Base != "" {
		policy.Base = b.Base
	}
	if b.Integration != "" {
		policy.Integration = b.Integration
	}
	if b.Types.Default != "" {
		policy.DefaultType = b.Types.Default
	}
	if len(b.Types.Match) > 0 {
		policy.Rules = b.Types.Match
	}
	if b.DeleteAfterMerge != nil {
		policy.DeleteAfterMerge = *b.DeleteAfterMerge
	}
	if err := validateRules(policy.Rules); err != nil {
		return core.BranchPolicy{}, err
	}
	return policy, nil
}

// validateRules refuses a rule that assigns nothing: a rule with no type is
// almost certainly a typo, and silently ignoring it would send work onto a
// branch nobody expects.
func validateRules(rules []core.BranchRule) error {
	for i, r := range rules {
		if strings.TrimSpace(r.Type) == "" {
			return fmt.Errorf("%w: branching.types.match[%d] has no type", core.ErrInvalidInput, i)
		}
	}
	return nil
}

// EffectiveTemplates returns the templates, falling back to the model's.
func (b Branching) EffectiveTemplates() Templates {
	t := b.Templates
	if preset, ok := FindPreset(b.Model); ok {
		if t.Branch == "" {
			t.Branch = preset.templates.Branch
		}
		if t.Worktree == "" {
			t.Worktree = preset.templates.Worktree
		}
	}
	return t
}
