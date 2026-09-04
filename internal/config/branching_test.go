package config

import (
	"errors"
	"strings"
	"testing"

	"github.com/aramponi/yakanban/internal/core"
)

func TestGitFlowPresetBranchesOffDevelop(t *testing.T) {
	policy, err := Branching{Model: ModelGitFlow}.Policy()
	if err != nil {
		t.Fatalf("Policy: %v", err)
	}
	if policy.Base != "develop" || policy.Integration != "develop" {
		t.Fatalf("git flow = %s → %s, want develop → develop", policy.Base, policy.Integration)
	}
	if policy.DefaultType != "feature" {
		t.Fatalf("default type = %q", policy.DefaultType)
	}
	templates := Branching{Model: ModelGitFlow}.EffectiveTemplates()
	if templates.Branch != "{{.Type}}/{{.ID}}-{{.Slug}}" {
		t.Fatalf("branch template = %q", templates.Branch)
	}
}

func TestTrunkBasedPresetMatchesGitHubsOwnConvention(t *testing.T) {
	policy, err := Branching{Model: ModelTrunkBased}.Policy()
	if err != nil {
		t.Fatalf("Policy: %v", err)
	}
	if policy.Base != "main" || policy.Integration != "main" {
		t.Fatalf("trunk-based = %s → %s", policy.Base, policy.Integration)
	}
	templates := Branching{Model: ModelTrunkBased}.EffectiveTemplates()
	if templates.Branch != "{{.ID}}-{{.Slug}}" {
		t.Fatalf("branch template = %q, want GitHub's <number>-<slug>", templates.Branch)
	}
}

func TestAnOverrideBeatsThePreset(t *testing.T) {
	b := Branching{Model: ModelGitFlow, Base: "trunk", Integration: "trunk"}
	policy, err := b.Policy()
	if err != nil {
		t.Fatalf("Policy: %v", err)
	}
	if policy.Base != "trunk" || policy.Integration != "trunk" {
		t.Fatalf("overrides were ignored: %s → %s", policy.Base, policy.Integration)
	}
	if policy.DefaultType != "feature" {
		t.Fatalf("an override should not discard the rest of the preset, got %q", policy.DefaultType)
	}
}

func TestCustomModelBringsNoDefaults(t *testing.T) {
	policy, err := Branching{Model: ModelCustom}.Policy()
	if err != nil {
		t.Fatalf("Policy: %v", err)
	}
	if policy.Base != "" || policy.DefaultType != "" || len(policy.Rules) != 0 {
		t.Fatalf("custom should invent nothing, got %+v", policy)
	}
}

func TestUnknownModelIsRefusedWithTheList(t *testing.T) {
	_, err := Branching{Model: "gitflow-ish"}.Policy()
	var invalid *core.InvalidValueError
	if !errors.As(err, &invalid) {
		t.Fatalf("err = %v, want an InvalidValueError", err)
	}
	if !strings.Contains(err.Error(), ModelGitFlow) {
		t.Fatalf("the error should list the models, got %q", err)
	}
}

func TestARuleWithoutATypeIsRefused(t *testing.T) {
	b := Branching{Model: ModelCustom, Types: Types{Match: []core.BranchRule{{Tag: "bug"}}}}
	if _, err := b.Policy(); err == nil || !strings.Contains(err.Error(), "match[0]") {
		t.Fatalf("err = %v, want the offending rule to be named", err)
	}
}

func TestUserRulesReplaceThePresetRules(t *testing.T) {
	b := Branching{Model: ModelGitFlow, Types: Types{Match: []core.BranchRule{{Tag: "chore", Type: "chore"}}}}
	policy, err := b.Policy()
	if err != nil {
		t.Fatalf("Policy: %v", err)
	}
	if len(policy.Rules) != 1 || policy.Rules[0].Type != "chore" {
		t.Fatalf("rules = %+v, want the written ones to win outright", policy.Rules)
	}
}

func TestDefaultDescriptorIsTrunkBasedAndWrittenOut(t *testing.T) {
	cfg := Default("demo", "github")
	if cfg.Branching.Model != ModelTrunkBased {
		t.Fatalf("model = %q", cfg.Branching.Model)
	}
	if cfg.Branching.Base == "" || cfg.Branching.Templates.Branch == "" {
		t.Fatalf("the preset should be written out, not left implicit: %+v", cfg.Branching)
	}
}
