package app

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aramponi/yakanban/internal/core"
)

// fakeProvider is an in-memory backend: it records the patches it receives so
// tests can assert on what the service decided to write.
type fakeProvider struct {
	tasks    map[string]*core.Task
	caps     core.Capability
	patches  []core.Patch
	drafts   []core.Draft
	nextID   int
	failNext error
}

func newFake(tasks ...core.Task) *fakeProvider {
	f := &fakeProvider{
		tasks: map[string]*core.Task{},
		caps: core.CapClaims | core.CapDependencies | core.CapParent | core.CapBlocked |
			core.CapEstimate | core.CapClass | core.CapDueDate,
		nextID: 100,
	}
	for i := range tasks {
		t := tasks[i]
		f.tasks[t.ID] = &t
	}
	return f
}

func (f *fakeProvider) Name() string                  { return "fake" }
func (f *fakeProvider) Capabilities() core.Capability { return f.caps }

func (f *fakeProvider) Board(context.Context) (*core.BoardInfo, error) {
	return &core.BoardInfo{Name: "fake"}, nil
}

func (f *fakeProvider) List(_ context.Context, _ core.Filter) ([]core.Task, error) {
	out := make([]core.Task, 0, len(f.tasks))
	for _, t := range f.tasks {
		out = append(out, *t)
	}
	return out, nil
}

func (f *fakeProvider) Get(_ context.Context, id string) (*core.Task, error) {
	t, ok := f.tasks[id]
	if !ok {
		return nil, core.ErrNotFound
	}
	clone := *t
	return &clone, nil
}

func (f *fakeProvider) Create(_ context.Context, d core.Draft) (*core.Task, error) {
	if f.failNext != nil {
		return nil, f.failNext
	}
	f.drafts = append(f.drafts, d)
	f.nextID++
	t := &core.Task{
		ID: strconv.Itoa(f.nextID), Title: d.Title, Body: d.Body, Status: d.Status,
		Priority: d.Priority, Class: d.Class, Tags: d.Tags, Due: d.Due, Claim: d.Claim,
	}
	f.tasks[t.ID] = t
	return t, nil
}

func (f *fakeProvider) Update(_ context.Context, id string, p core.Patch) (*core.Task, error) {
	f.patches = append(f.patches, p)
	t, ok := f.tasks[id]
	if !ok {
		return nil, core.ErrNotFound
	}
	if p.Status != nil {
		t.Status = *p.Status
	}
	if p.Body != nil {
		t.Body = *p.Body
	}
	if p.Started != nil {
		t.Started = p.Started
	}
	if p.Completed != nil {
		t.Completed = p.Completed
	}
	if p.ClearCompleted {
		t.Completed = nil
	}
	if p.Claim != nil {
		t.Claim = p.Claim
	}
	if p.ReleaseClaim {
		t.Claim = nil
	}
	clone := *t
	return &clone, nil
}

func (f *fakeProvider) Delete(_ context.Context, id string) error {
	delete(f.tasks, id)
	return nil
}

func (f *fakeProvider) lastPatch(t *testing.T) core.Patch {
	t.Helper()
	if len(f.patches) == 0 {
		t.Fatal("no patch was sent to the provider")
	}
	return f.patches[len(f.patches)-1]
}

var testBoard = core.BoardInfo{
	Name: "test",
	Statuses: []core.Status{
		{Name: "Backlog", Initial: true},
		{Name: "Todo"},
		{Name: "In Progress", RequireClaim: true},
		{Name: "Done", Terminal: true},
	},
	Priorities: []string{"low", "medium", "high", "critical"},
	Classes:    []core.Class{{Name: "standard"}, {Name: "expedite"}},
}

func newService(f *fakeProvider, now time.Time) *Service {
	return New(f, testBoard, Options{
		DefaultStatus:   "Backlog",
		DefaultPriority: "medium",
		DefaultClass:    "standard",
		ClaimTimeout:    time.Hour,
		Now:             func() time.Time { return now },
	})
}

func TestCreateAppliesDefaults(t *testing.T) {
	f := newFake()
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	s := newService(f, now)

	task, err := s.Create(context.Background(), core.Draft{Title: "Ship it"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if task.Status != "Backlog" || task.Priority != "medium" || task.Class != "standard" {
		t.Fatalf("defaults were not applied: %+v", task)
	}
}

func TestCreateRejectsUnknownPriority(t *testing.T) {
	s := newService(newFake(), time.Now())
	_, err := s.Create(context.Background(), core.Draft{Title: "x", Priority: "urgent"})
	var invalid *core.InvalidValueError
	if !errors.As(err, &invalid) {
		t.Fatalf("err = %v, want an InvalidValueError", err)
	}
	if !strings.Contains(err.Error(), "critical") {
		t.Fatalf("the error should list the allowed values, got %q", err)
	}
}

func TestCreateRequiresATitle(t *testing.T) {
	s := newService(newFake(), time.Now())
	if _, err := s.Create(context.Background(), core.Draft{Title: "  "}); !errors.Is(err, core.ErrInvalidInput) {
		t.Fatalf("err = %v, want ErrInvalidInput", err)
	}
}

func TestCreateSetsClaimExpiry(t *testing.T) {
	f := newFake()
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	s := newService(f, now)

	_, err := s.Create(context.Background(), core.Draft{Title: "x", Claim: &core.Claim{Agent: "frost-maple"}})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	got := f.drafts[0].Claim.Expires
	if !got.Equal(now.Add(time.Hour)) {
		t.Fatalf("claim expires at %s, want %s", got, now.Add(time.Hour))
	}
}

func TestMoveStampsStartedAndCompleted(t *testing.T) {
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	f := newFake(core.Task{ID: "1", Status: "Backlog"})
	s := newService(f, now)

	if _, err := s.Move(context.Background(), "1", "Todo", false, false, EditOptions{}); err != nil {
		t.Fatalf("Move: %v", err)
	}
	if p := f.lastPatch(t); p.Started == nil || !p.Started.Equal(now) {
		t.Fatalf("leaving the initial column should stamp Started, got %+v", p.Started)
	}

	if _, err := s.Move(context.Background(), "1", "Done", false, false, EditOptions{}); err != nil {
		t.Fatalf("Move: %v", err)
	}
	if p := f.lastPatch(t); p.Completed == nil {
		t.Fatal("moving to a terminal column should stamp Completed")
	}
}

func TestMoveBackClearsCompleted(t *testing.T) {
	now := time.Now()
	done := now.Add(-time.Hour)
	f := newFake(core.Task{ID: "1", Status: "Done", Completed: &done})
	s := newService(f, now)

	if _, err := s.Move(context.Background(), "1", "Todo", false, false, EditOptions{}); err != nil {
		t.Fatalf("Move: %v", err)
	}
	if !f.lastPatch(t).ClearCompleted {
		t.Fatal("moving out of a terminal column should clear Completed")
	}
}

func TestMoveNextAndPrev(t *testing.T) {
	f := newFake(core.Task{ID: "1", Status: "Todo"})
	s := newService(f, time.Now())

	task, err := s.Move(context.Background(), "1", "", true, false, EditOptions{Agent: "frost-maple"})
	if err != nil {
		t.Fatalf("Move --next: %v", err)
	}
	if task.Status != "In Progress" {
		t.Fatalf("status = %q, want In Progress", task.Status)
	}

	task, err = s.Move(context.Background(), "1", "", false, true, EditOptions{})
	if err != nil {
		t.Fatalf("Move --prev: %v", err)
	}
	if task.Status != "Todo" {
		t.Fatalf("status = %q, want Todo", task.Status)
	}
}

func TestMovePrevAtFirstColumnFails(t *testing.T) {
	s := newService(newFake(core.Task{ID: "1", Status: "Backlog"}), time.Now())
	_, err := s.Move(context.Background(), "1", "", false, true, EditOptions{})
	if !errors.Is(err, core.ErrInvalidInput) {
		t.Fatalf("err = %v, want ErrInvalidInput at the board edge", err)
	}
}

func TestMoveIntoAClaimRequiredColumnNeedsAClaim(t *testing.T) {
	s := newService(newFake(core.Task{ID: "1", Status: "Todo"}), time.Now())
	if _, err := s.Move(context.Background(), "1", "In Progress", false, false, EditOptions{}); !errors.Is(err, core.ErrInvalidInput) {
		t.Fatalf("err = %v, want the missing claim to be refused", err)
	}
	if _, err := s.Move(context.Background(), "1", "In Progress", false, false, EditOptions{Agent: "frost-maple"}); err != nil {
		t.Fatalf("Move with a claim: %v", err)
	}
}

func TestStatusPrefixIsResolved(t *testing.T) {
	f := newFake(core.Task{ID: "1", Status: "Todo"})
	s := newService(f, time.Now())
	task, err := s.Move(context.Background(), "1", "in p", false, false, EditOptions{Agent: "a"})
	if err != nil {
		t.Fatalf("Move: %v", err)
	}
	if task.Status != "In Progress" {
		t.Fatalf("status = %q, want the prefix to resolve to In Progress", task.Status)
	}
}

func TestClaimConflict(t *testing.T) {
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	held := core.Task{ID: "1", Status: "Todo", Claim: &core.Claim{Agent: "frost-maple", Expires: now.Add(time.Hour)}}
	f := newFake(held)
	s := newService(f, now)
	title := "renamed"

	_, err := s.Update(context.Background(), "1", core.Patch{Title: &title}, EditOptions{Agent: "tidal-vale"})
	if !errors.Is(err, core.ErrClaimed) {
		t.Fatalf("err = %v, want ErrClaimed", err)
	}

	if _, err := s.Update(context.Background(), "1", core.Patch{Title: &title}, EditOptions{Agent: "tidal-vale", Force: true}); err != nil {
		t.Fatalf("--force should override the claim, got %v", err)
	}
	if _, err := s.Update(context.Background(), "1", core.Patch{Title: &title}, EditOptions{}); err != nil {
		t.Fatalf("a human edit without --claim should not be blocked, got %v", err)
	}
}

func TestExpiredClaimDoesNotBlock(t *testing.T) {
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	stale := core.Task{ID: "1", Status: "Todo", Claim: &core.Claim{Agent: "frost-maple", Expires: now.Add(-time.Minute)}}
	s := newService(newFake(stale), now)
	title := "renamed"
	if _, err := s.Update(context.Background(), "1", core.Patch{Title: &title}, EditOptions{Agent: "tidal-vale"}); err != nil {
		t.Fatalf("an expired claim should be free to take, got %v", err)
	}
}

func TestAppendBodyKeepsExistingText(t *testing.T) {
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	f := newFake(core.Task{ID: "1", Status: "Todo", Body: "first line"})
	s := newService(f, now)
	note := "second line"

	if _, err := s.Update(context.Background(), "1", core.Patch{AppendBody: &note}, EditOptions{Timestamp: true}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	body := *f.lastPatch(t).Body
	if !strings.HasPrefix(body, "first line") {
		t.Fatalf("append dropped the existing body: %q", body)
	}
	if !strings.Contains(body, now.Format(time.RFC3339)) {
		t.Fatalf("--timestamp did not add a timestamp: %q", body)
	}
	if !strings.HasSuffix(body, note) {
		t.Fatalf("the note was not appended: %q", body)
	}
}

func TestUnsupportedCapabilityIsRefused(t *testing.T) {
	f := newFake(core.Task{ID: "1", Status: "Todo"})
	f.caps = 0
	s := newService(f, time.Now())
	reason := "waiting on legal"

	err := errorOf(s.Update(context.Background(), "1", core.Patch{Blocked: &reason}, EditOptions{}))
	if !errors.Is(err, core.ErrUnsupported) {
		t.Fatalf("err = %v, want ErrUnsupported", err)
	}
}

func TestEmptyPatchIsANoOp(t *testing.T) {
	f := newFake(core.Task{ID: "1", Status: "Todo"})
	s := newService(f, time.Now())
	if _, err := s.Update(context.Background(), "1", core.Patch{}, EditOptions{}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if len(f.patches) != 0 {
		t.Fatalf("an empty patch should not reach the provider, got %d", len(f.patches))
	}
}

func TestListAppliesFilterAndLimit(t *testing.T) {
	now := time.Now()
	f := newFake(
		core.Task{ID: "1", Status: "Todo", Priority: "high"},
		core.Task{ID: "2", Status: "Done", Priority: "low"},
		core.Task{ID: "3", Status: "Todo", Priority: "low"},
	)
	s := newService(f, now)

	tasks, err := s.List(context.Background(), core.Filter{Statuses: []string{"Todo"}}, "id", false)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(tasks) != 2 || tasks[0].ID != "1" {
		t.Fatalf("filtered list = %+v", tasks)
	}

	tasks, err = s.List(context.Background(), core.Filter{Limit: 1}, "id", false)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("limit ignored: %d tasks", len(tasks))
	}
}

func TestResolveVocabularyRejectsAmbiguousPrefix(t *testing.T) {
	if _, err := ResolveVocabulary("re", []string{"review", "ready"}); err == nil {
		t.Fatal("an ambiguous prefix should not resolve")
	}
	if got, err := ResolveVocabulary("rev", []string{"review", "ready"}); err != nil || got != "review" {
		t.Fatalf("got %q, %v; want review", got, err)
	}
}

func errorOf(_ *core.Task, err error) error { return err }
