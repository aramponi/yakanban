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
		{Name: "Review", RequireClaim: true},
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

func TestPickTakesTheHighestPriorityFirst(t *testing.T) {
	now := time.Now()
	f := newFake(
		core.Task{ID: "1", Status: "Todo", Priority: "low"},
		core.Task{ID: "2", Status: "Todo", Priority: "critical"},
		core.Task{ID: "3", Status: "Todo", Priority: "high"},
	)
	s := newService(f, now)

	task, err := s.Pick(context.Background(), PickOptions{Agent: "frost-maple", Status: "Todo"})
	if err != nil {
		t.Fatalf("Pick: %v", err)
	}
	if task.ID != "2" {
		t.Fatalf("picked %s, want the critical task", task.ID)
	}
	if task.Claim == nil || task.Claim.Agent != "frost-maple" {
		t.Fatalf("the picked task should come back claimed, got %+v", task.Claim)
	}
}

func TestPickPrefersTheOldestWithinAPriority(t *testing.T) {
	f := newFake(
		core.Task{ID: "9", Status: "Todo", Priority: "high"},
		core.Task{ID: "2", Status: "Todo", Priority: "high"},
	)
	s := newService(f, time.Now())

	task, err := s.Pick(context.Background(), PickOptions{Agent: "a", Status: "Todo"})
	if err != nil {
		t.Fatalf("Pick: %v", err)
	}
	if task.ID != "2" {
		t.Fatalf("picked %s, want the older task", task.ID)
	}
}

func TestPickSkipsClaimedBlockedAndDependentTasks(t *testing.T) {
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	f := newFake(
		core.Task{ID: "1", Status: "Todo", Priority: "critical",
			Claim: &core.Claim{Agent: "someone-else", Expires: now.Add(time.Hour)}},
		core.Task{ID: "2", Status: "Todo", Priority: "critical", Blocked: "waiting on legal"},
		core.Task{ID: "3", Status: "Todo", Priority: "critical", DependsOn: []string{"5"}},
		core.Task{ID: "4", Status: "Todo", Priority: "low"},
		core.Task{ID: "5", Status: "Todo", Priority: "low"},
	)
	s := newService(f, now)

	task, err := s.Pick(context.Background(), PickOptions{Agent: "frost-maple", Status: "Todo"})
	if err != nil {
		t.Fatalf("Pick: %v", err)
	}
	if task.ID == "1" || task.ID == "2" || task.ID == "3" {
		t.Fatalf("picked %s: claimed, blocked and dependent tasks must be skipped", task.ID)
	}
}

func TestPickMovesWhenAsked(t *testing.T) {
	f := newFake(core.Task{ID: "1", Status: "Todo", Priority: "high"})
	s := newService(f, time.Now())

	task, err := s.Pick(context.Background(), PickOptions{Agent: "a", Status: "Todo", Move: "In Progress"})
	if err != nil {
		t.Fatalf("Pick: %v", err)
	}
	if task.Status != "In Progress" {
		t.Fatalf("status = %q, want the pick to have moved it", task.Status)
	}
}

func TestPickSkipsTerminalColumnsByDefault(t *testing.T) {
	f := newFake(
		core.Task{ID: "1", Status: "Done", Priority: "critical"},
		core.Task{ID: "2", Status: "Todo", Priority: "low"},
	)
	s := newService(f, time.Now())

	task, err := s.Pick(context.Background(), PickOptions{Agent: "a"})
	if err != nil {
		t.Fatalf("Pick: %v", err)
	}
	if task.ID != "2" {
		t.Fatalf("picked %s, want finished work to be left alone", task.ID)
	}
}

func TestPickReportsAnEmptyBoard(t *testing.T) {
	s := newService(newFake(), time.Now())
	_, err := s.Pick(context.Background(), PickOptions{Agent: "a"})
	if !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestPickRequiresAnAgent(t *testing.T) {
	s := newService(newFake(core.Task{ID: "1", Status: "Todo"}), time.Now())
	if _, err := s.Pick(context.Background(), PickOptions{}); !errors.Is(err, core.ErrInvalidInput) {
		t.Fatalf("err = %v, want the missing --claim to be refused", err)
	}
}

// racingProvider hands out a task that another agent claims between the
// listing and the write — the exact race `pick` exists to survive.
type racingProvider struct {
	*fakeProvider
	stolen map[string]bool
}

func (r *racingProvider) Get(ctx context.Context, id string) (*core.Task, error) {
	task, err := r.fakeProvider.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if r.stolen[id] {
		task.Claim = &core.Claim{Agent: "rival-agent", Expires: time.Now().Add(time.Hour)}
	}
	return task, nil
}

func TestPickSurvivesAStolenCandidate(t *testing.T) {
	inner := newFake(
		core.Task{ID: "1", Status: "Todo", Priority: "critical"},
		core.Task{ID: "2", Status: "Todo", Priority: "high"},
	)
	racing := &racingProvider{fakeProvider: inner, stolen: map[string]bool{"1": true}}
	s := New(racing, testBoard, Options{ClaimTimeout: time.Hour, Now: time.Now})

	task, err := s.Pick(context.Background(), PickOptions{Agent: "frost-maple", Status: "Todo"})
	if err != nil {
		t.Fatalf("Pick should fall through to the next candidate, got %v", err)
	}
	if task.ID != "2" {
		t.Fatalf("picked %s, want the task the rival did not take", task.ID)
	}
}

func TestHandoffParksBlocksAndReleases(t *testing.T) {
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	f := newFake(core.Task{ID: "1", Status: "In Progress", Body: "context so far"})
	s := newService(f, now)

	task, err := s.Handoff(context.Background(), "1", HandoffOptions{
		Agent:     "frost-maple",
		To:        "Review",
		Note:      "Ready to merge: task/1-login",
		Block:     "Waiting on a product call",
		Release:   true,
		Timestamp: true,
	})
	if err != nil {
		t.Fatalf("Handoff: %v", err)
	}
	if task.Claim != nil {
		t.Fatalf("--release should have dropped the claim, got %+v", task.Claim)
	}

	parked := f.patches[0]
	if parked.Status == nil || *parked.Status != "Review" {
		t.Fatalf("status = %v, want Review", parked.Status)
	}
	if parked.Blocked == nil || *parked.Blocked != "Waiting on a product call" {
		t.Fatalf("blocked = %v", parked.Blocked)
	}
	body := *parked.Body
	if !strings.HasPrefix(body, "context so far") || !strings.Contains(body, "Ready to merge") {
		t.Fatalf("the note should be appended to the existing body, got %q", body)
	}
	if !strings.Contains(body, now.Format(time.RFC3339)) {
		t.Fatalf("--timestamp did not stamp the note: %q", body)
	}
}

func TestHandoffKeepsTheClaimWithoutRelease(t *testing.T) {
	f := newFake(core.Task{ID: "1", Status: "In Progress"})
	s := newService(f, time.Now())

	task, err := s.Handoff(context.Background(), "1", HandoffOptions{Agent: "frost-maple", To: "Review"})
	if err != nil {
		t.Fatalf("Handoff: %v", err)
	}
	if task.Claim == nil || task.Claim.Agent != "frost-maple" {
		t.Fatalf("claim = %+v, want it kept", task.Claim)
	}
}

func TestHandoffRefusesAnUnknownColumn(t *testing.T) {
	s := newService(newFake(core.Task{ID: "1", Status: "Todo"}), time.Now())
	_, err := s.Handoff(context.Background(), "1", HandoffOptions{Agent: "a", To: "Nowhere"})
	var invalid *core.InvalidValueError
	if !errors.As(err, &invalid) {
		t.Fatalf("err = %v, want the column to be rejected", err)
	}
}

func TestResolveVocabularyIgnoresSeparators(t *testing.T) {
	columns := []string{"Backlog", "In Progress", "Done"}
	for _, spelling := range []string{"In Progress", "in progress", "in-progress", "in_progress", "inprogress", "INPROGRESS"} {
		got, err := ResolveVocabulary(spelling, columns)
		if err != nil || got != "In Progress" {
			t.Fatalf("ResolveVocabulary(%q) = %q, %v; want In Progress", spelling, got, err)
		}
	}
	if _, err := ResolveVocabulary("", columns); err == nil {
		t.Fatal("an empty term should not resolve")
	}
}

func TestMoveAcceptsAHyphenatedColumn(t *testing.T) {
	f := newFake(core.Task{ID: "1", Status: "Todo"})
	s := newService(f, time.Now())
	task, err := s.Move(context.Background(), "1", "in-progress", false, false, EditOptions{Agent: "a"})
	if err != nil {
		t.Fatalf("Move: %v", err)
	}
	if task.Status != "In Progress" {
		t.Fatalf("status = %q, want In Progress", task.Status)
	}
}

func TestListResolvesFilterSpelling(t *testing.T) {
	f := newFake(
		core.Task{ID: "1", Status: "In Progress"},
		core.Task{ID: "2", Status: "Todo"},
	)
	s := newService(f, time.Now())

	tasks, err := s.List(context.Background(), core.Filter{Statuses: []string{"in-progress"}}, "id", false)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(tasks) != 1 || tasks[0].ID != "1" {
		t.Fatalf("filtering on a hyphenated column returned %+v", tasks)
	}
}

func TestListReportsAnUnknownFilterValue(t *testing.T) {
	s := newService(newFake(core.Task{ID: "1", Status: "Todo"}), time.Now())
	_, err := s.List(context.Background(), core.Filter{Statuses: []string{"shipped"}}, "id", false)
	var invalid *core.InvalidValueError
	if !errors.As(err, &invalid) {
		t.Fatalf("err = %v, want an unknown column to be reported, not an empty list", err)
	}
}

// losingProvider accepts the write and then reports somebody else's claim, the
// shape of a race where two agents claim the same task in the same instant and
// the other one's write lands last.
type losingProvider struct {
	*fakeProvider
	winner string
}

func (l *losingProvider) Update(ctx context.Context, id string, p core.Patch) (*core.Task, error) {
	task, err := l.fakeProvider.Update(ctx, id, p)
	if err != nil {
		return nil, err
	}
	task.Claim = &core.Claim{Agent: l.winner, Expires: time.Now().Add(time.Hour)}
	return task, nil
}

// `move ID --claim` is the targeted equivalent of pick, and must be just as
// safe: taking a named task cannot be best-effort while taking the next one is
// verified.
func TestATargetedMoveRefusesAClaimThatDidNotSurvive(t *testing.T) {
	provider := &losingProvider{fakeProvider: newFake(core.Task{ID: "5", Status: "Todo"}), winner: "rival-agent"}
	s := New(provider, testBoard, Options{ClaimTimeout: time.Hour, Now: time.Now})

	_, err := s.Move(context.Background(), "5", "In Progress", false, false, EditOptions{Agent: "frost-maple"})
	if !errors.Is(err, core.ErrClaimed) {
		t.Fatalf("err = %v, want ErrClaimed: the write succeeded but the claim is not ours", err)
	}
	if !strings.Contains(err.Error(), "rival-agent") {
		t.Fatalf("err = %q, want it to name who actually holds the task", err)
	}
}

func TestAPlainEditIsNotSubjectToClaimVerification(t *testing.T) {
	provider := &losingProvider{fakeProvider: newFake(core.Task{ID: "5", Status: "Todo"}), winner: "rival-agent"}
	s := New(provider, testBoard, Options{ClaimTimeout: time.Hour, Now: time.Now})
	title := "renamed"

	if _, err := s.Update(context.Background(), "5", core.Patch{Title: &title}, EditOptions{}); err != nil {
		t.Fatalf("an edit that claims nothing has no claim to verify, got %v", err)
	}
}

func TestReleasingIsNotSubjectToClaimVerification(t *testing.T) {
	now := time.Now()
	held := core.Task{ID: "5", Status: "Todo", Claim: &core.Claim{Agent: "frost-maple", Expires: now.Add(time.Hour)}}
	s := newService(newFake(held), now)

	if _, err := s.Update(context.Background(), "5", core.Patch{}, EditOptions{Agent: "frost-maple", Release: true}); err != nil {
		t.Fatalf("releasing must not be refused for lacking a claim afterwards, got %v", err)
	}
}
