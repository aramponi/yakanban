package core

import "testing"

var gitFlow = BranchPolicy{
	Model: "git-flow", Base: "develop", Integration: "develop", DefaultType: "feature",
	Rules: []BranchRule{
		{Priority: "critical", Type: "hotfix", Base: "main"},
		{Tag: "bug", Type: "fix"},
	},
	BackMergeWarning: map[string]string{"hotfix": "merge it back into develop yourself"},
}

func TestTypeForIsFirstMatchWins(t *testing.T) {
	cases := []struct {
		name string
		task Task
		want string
	}{
		{"nothing matches", Task{ID: "1", Title: "x"}, "feature"},
		{"a bug becomes a fix", Task{Tags: []string{"bug"}}, "fix"},
		{"critical wins over the bug rule below it", Task{Priority: "critical", Tags: []string{"bug"}}, "hotfix"},
		{"tag matching ignores case", Task{Tags: []string{"BUG"}}, "fix"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got, _ := gitFlow.TypeFor(tc.task); got != tc.want {
				t.Fatalf("TypeFor = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestBaseForLetsARuleOverrideTheModel(t *testing.T) {
	if got := gitFlow.BaseFor(Task{Priority: "critical"}); got != "main" {
		t.Fatalf("a hotfix should branch off %q, got %q", "main", got)
	}
	if got := gitFlow.BaseFor(Task{Tags: []string{"bug"}}); got != "develop" {
		t.Fatalf("an ordinary fix should follow the model's base, got %q", got)
	}
}

func TestAllConditionsOfARuleMustHold(t *testing.T) {
	rule := BranchRule{Tag: "bug", Priority: "high", Type: "fix"}
	if rule.Matches(Task{Tags: []string{"bug"}}) {
		t.Fatal("a rule with two conditions must not match on one of them")
	}
	if !rule.Matches(Task{Tags: []string{"bug"}, Priority: "high"}) {
		t.Fatal("both conditions holding should match")
	}
}

func TestARuleWithoutConditionsMatchesEverything(t *testing.T) {
	if !(BranchRule{Type: "feature"}).Matches(Task{}) {
		t.Fatal("a rule with no condition should match")
	}
}

func TestBackMergeNoteIsOnlyForTheTypesThatNeedIt(t *testing.T) {
	if note := gitFlow.BackMergeNote(Task{Priority: "critical"}); note == "" {
		t.Fatal("a hotfix should carry the back-merge warning")
	}
	if note := gitFlow.BackMergeNote(Task{Tags: []string{"bug"}}); note != "" {
		t.Fatalf("an ordinary fix should carry no warning, got %q", note)
	}
}
