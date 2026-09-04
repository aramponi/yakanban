package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aramponi/yakanban/internal/core"
)

// branchingProvider is a fake that also speaks the optional Brancher port.
type branchingProvider struct {
	*fakeProvider
	requests []core.BranchRequest
	branches []core.Branch
	failWith error
}

func (b *branchingProvider) CreateBranch(_ context.Context, _ string, req core.BranchRequest) (*core.Branch, error) {
	b.requests = append(b.requests, req)
	if b.failWith != nil {
		return nil, b.failWith
	}
	branch := core.Branch{ID: "LB_1", Name: req.Name, Ref: "refs/heads/" + req.Name, OID: req.BaseOID}
	b.branches = append(b.branches, branch)
	return &branch, nil
}

func (b *branchingProvider) Branches(context.Context, string) ([]core.Branch, error) {
	return b.branches, nil
}

func (b *branchingProvider) UnlinkBranch(_ context.Context, id string) error {
	for i, br := range b.branches {
		if br.ID == id {
			b.branches = append(b.branches[:i], b.branches[i+1:]...)
			return nil
		}
	}
	return core.ErrNotFound
}

// decorator stands in for the cache: a Provider that wraps another and does
// not itself implement Brancher.
type decorator struct {
	core.Provider
	inner core.Provider
}

func (d *decorator) Unwrap() core.Provider { return d.inner }

func newBranchService(t *testing.T, tasks ...core.Task) (*Service, *branchingProvider) {
	t.Helper()
	inner := &branchingProvider{fakeProvider: newFake(tasks...)}
	s := New(inner, testBoard, Options{
		ClaimTimeout:     time.Hour,
		Now:              time.Now,
		BranchTemplate:   "{{.ID}}-{{.Slug}}",
		WorktreeTemplate: "../{{.Repo}}-task-{{.ID}}",
	})
	return s, inner
}

func TestBranchNameComesFromTheTemplate(t *testing.T) {
	s, _ := newBranchService(t)
	task := core.Task{ID: "42", Title: "Fix the login redirect!", Priority: "high"}

	got, err := s.BranchName(task, BranchOptions{})
	if err != nil {
		t.Fatalf("BranchName: %v", err)
	}
	if got != "42-fix-the-login-redirect" {
		t.Fatalf("branch = %q", got)
	}
}

func TestBranchNameHonoursAnExplicitName(t *testing.T) {
	s, _ := newBranchService(t)
	got, err := s.BranchName(core.Task{ID: "42", Title: "x"}, BranchOptions{Name: "feature/custom"})
	if err != nil || got != "feature/custom" {
		t.Fatalf("BranchName = %q, %v", got, err)
	}
}

func TestBranchNameRejectsAnInvalidExplicitName(t *testing.T) {
	s, _ := newBranchService(t)
	for _, name := range []string{"has space", "ends/", "a..b", "~tilde", "trailing.lock", "-leading"} {
		if _, err := s.BranchName(core.Task{ID: "1", Title: "x"}, BranchOptions{Name: name}); err == nil {
			t.Fatalf("%q should have been rejected as a ref name", name)
		}
	}
}

func TestBranchTemplateCanUseEveryField(t *testing.T) {
	inner := &branchingProvider{fakeProvider: newFake()}
	s := New(inner, testBoard, Options{
		ClaimTimeout:   time.Hour,
		BranchTemplate: "{{.Class}}/{{.Priority}}/{{.ID}}-{{.Slug}}",
	})
	task := core.Task{ID: "7", Title: "Add search", Priority: "high", Class: "expedite"}

	got, err := s.BranchName(task, BranchOptions{})
	if err != nil {
		t.Fatalf("BranchName: %v", err)
	}
	if got != "expedite/high/7-add-search" {
		t.Fatalf("branch = %q", got)
	}
}

func TestWorktreePathUsesTheRepoName(t *testing.T) {
	s, _ := newBranchService(t)
	got, err := s.WorktreePath(core.Task{ID: "42", Title: "x"}, BranchOptions{Repo: "yakanban"})
	if err != nil || got != "../yakanban-task-42" {
		t.Fatalf("WorktreePath = %q, %v", got, err)
	}
}

func TestMissingTemplateIsReportedNotInvented(t *testing.T) {
	inner := &branchingProvider{fakeProvider: newFake()}
	s := New(inner, testBoard, Options{ClaimTimeout: time.Hour})
	_, err := s.BranchName(core.Task{ID: "1", Title: "x"}, BranchOptions{})
	if err == nil || !strings.Contains(err.Error(), "branching.templates.branch") {
		t.Fatalf("err = %v, want it to name the missing setting", err)
	}
}

func TestCreateBranchPassesTheNameToTheBackend(t *testing.T) {
	s, provider := newBranchService(t, core.Task{ID: "42", Status: "Todo", Title: "Fix the login redirect"})

	branch, err := s.CreateBranch(context.Background(), "42", BranchOptions{BaseOID: "abc123", Agent: "frost-maple"})
	if err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}
	if len(provider.requests) != 1 {
		t.Fatalf("provider saw %d requests", len(provider.requests))
	}
	req := provider.requests[0]
	if req.Name != "42-fix-the-login-redirect" {
		t.Fatalf("the backend must be told the name, got %q", req.Name)
	}
	if req.BaseOID != "abc123" {
		t.Fatalf("base = %q", req.BaseOID)
	}
	if branch.Name != req.Name {
		t.Fatalf("the returned branch renamed itself: %q", branch.Name)
	}
}

func TestCreateBranchRequiresABase(t *testing.T) {
	s, _ := newBranchService(t, core.Task{ID: "1", Status: "Todo", Title: "x"})
	if _, err := s.CreateBranch(context.Background(), "1", BranchOptions{}); !errors.Is(err, core.ErrInvalidInput) {
		t.Fatalf("err = %v, want a missing base to be refused", err)
	}
}

func TestCreateBranchRespectsAnotherAgentsClaim(t *testing.T) {
	now := time.Now()
	held := core.Task{ID: "1", Status: "Todo", Title: "x",
		Claim: &core.Claim{Agent: "frost-maple", Expires: now.Add(time.Hour)}}
	s, _ := newBranchService(t, held)

	_, err := s.CreateBranch(context.Background(), "1", BranchOptions{BaseOID: "abc", Agent: "tidal-vale"})
	if !errors.Is(err, core.ErrClaimed) {
		t.Fatalf("err = %v, want ErrClaimed", err)
	}
	if _, err := s.CreateBranch(context.Background(), "1", BranchOptions{BaseOID: "abc", Agent: "tidal-vale", Force: true}); err != nil {
		t.Fatalf("--force should override the claim, got %v", err)
	}
}

func TestBranchOnAProviderWithoutTheCapability(t *testing.T) {
	s := New(newFake(core.Task{ID: "1", Status: "Todo"}), testBoard, Options{ClaimTimeout: time.Hour})
	_, err := s.CreateBranch(context.Background(), "1", BranchOptions{BaseOID: "abc"})
	if !errors.Is(err, core.ErrUnsupported) {
		t.Fatalf("err = %v, want ErrUnsupported on a backend without branches", err)
	}
}

// The cache wraps the provider, so the optional port has to be looked for
// underneath the decorator rather than on the outermost value.
func TestBranchIsFoundThroughADecorator(t *testing.T) {
	inner := &branchingProvider{fakeProvider: newFake(core.Task{ID: "1", Status: "Todo", Title: "Add search"})}
	wrapped := &decorator{Provider: inner, inner: inner}
	s := New(wrapped, testBoard, Options{ClaimTimeout: time.Hour, BranchTemplate: "{{.ID}}-{{.Slug}}"})

	if _, err := s.CreateBranch(context.Background(), "1", BranchOptions{BaseOID: "abc"}); err != nil {
		t.Fatalf("CreateBranch through a decorator: %v", err)
	}
}

func TestDecoratorDoesNotAnswerForAProviderWithoutBranches(t *testing.T) {
	plain := newFake(core.Task{ID: "1", Status: "Todo"})
	wrapped := &decorator{Provider: plain, inner: plain}
	s := New(wrapped, testBoard, Options{ClaimTimeout: time.Hour, BranchTemplate: "{{.ID}}"})

	if _, err := s.CreateBranch(context.Background(), "1", BranchOptions{BaseOID: "abc"}); !errors.Is(err, core.ErrUnsupported) {
		t.Fatalf("err = %v, want the decorator not to claim a capability its inner provider lacks", err)
	}
}

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"Fix the login redirect":       "fix-the-login-redirect",
		"  Trim   me  ":                "trim-me",
		"Add `yakanban skill install`": "add-yakanban-skill-install",
		"Ünïcödé tïtle":                "ünïcödé-tïtle",
		"---":                          "",
		"UPPER Case":                   "upper-case",
	}
	for in, want := range cases {
		if got := Slugify(in, 0); got != want {
			t.Fatalf("Slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSlugifyCapsLengthWithoutATrailingDash(t *testing.T) {
	got := Slugify("aaaa bbbb cccc dddd eeee ffff", 10)
	if len(got) > 10 {
		t.Fatalf("Slugify = %q, longer than the limit", got)
	}
	if strings.HasSuffix(got, "-") {
		t.Fatalf("Slugify = %q, a truncated slug must not end with a dash", got)
	}
}

func TestUnlinkBranch(t *testing.T) {
	s, provider := newBranchService(t, core.Task{ID: "1", Status: "Todo", Title: "Add search"})
	if _, err := s.CreateBranch(context.Background(), "1", BranchOptions{BaseOID: "abc"}); err != nil {
		t.Fatal(err)
	}
	if err := s.UnlinkBranch(context.Background(), "LB_1"); err != nil {
		t.Fatalf("UnlinkBranch: %v", err)
	}
	if len(provider.branches) != 0 {
		t.Fatalf("branch list = %+v, want it detached", provider.branches)
	}
}
