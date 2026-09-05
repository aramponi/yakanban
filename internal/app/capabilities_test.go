package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aramponi/yakanban/internal/core"
)

func TestCreateRefusesUnsupportedFieldsBeforeWriting(t *testing.T) {
	for name, draft := range map[string]core.Draft{
		"dependencies": {Title: "x", DependsOn: []string{"2"}},
		"parent":       {Title: "x", Parent: "2"},
		"estimate":     {Title: "x", Estimate: "1h"},
		"class":        {Title: "x", Class: "standard"},
		"due date":     {Title: "x", Due: new(time.Time)},
		"claims":       {Title: "x", Claim: &core.Claim{Agent: "agent"}},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFake()
			f.caps = 0
			s := New(f, testBoard, Options{DefaultStatus: "Todo"})
			if _, err := s.Create(context.Background(), draft); !errors.Is(err, core.ErrUnsupported) {
				t.Fatalf("error = %v, want unsupported before creating issue", err)
			}
			if len(f.drafts) != 0 {
				t.Fatal("unsupported draft reached the backend")
			}
		})
	}
}

func TestUpdateRefusesUnsupportedClassAndDueDate(t *testing.T) {
	value := "standard"
	for name, patch := range map[string]core.Patch{
		"class": {Class: &value}, "due date": {Due: new(time.Time)}, "clear due date": {ClearDue: true},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFake(core.Task{ID: "1", Status: "Todo"})
			f.caps = 0
			s := New(f, testBoard, Options{})
			if _, err := s.Update(context.Background(), "1", patch, EditOptions{}); !errors.Is(err, core.ErrUnsupported) {
				t.Fatalf("error = %v, want unsupported", err)
			}
			if len(f.patches) != 0 {
				t.Fatal("unsupported patch reached the backend")
			}
		})
	}
}

type runtimeProvider struct {
	*fakeProvider
	resolved   core.CapabilitySet
	boardError error
}

func (p *runtimeProvider) Board(context.Context) (*core.BoardInfo, error) {
	if p.boardError != nil {
		return nil, p.boardError
	}
	return &core.BoardInfo{Capabilities: &p.resolved}, nil
}

func TestRuntimeCapabilitiesOverrideStaticDefaults(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		p := &runtimeProvider{fakeProvider: newFake(core.Task{ID: "1", Status: "Todo"}), resolved: core.CapabilitySet{
			Reasons: map[core.Capability]string{core.CapDependencies: "dependencies need Premium; this instance is Free"},
		}}
		if enabled {
			p.resolved.Supported = core.CapDependencies
		}
		s := New(p, testBoard, Options{})
		_, err := s.Update(context.Background(), "1", core.Patch{AddDeps: []string{"2"}}, EditOptions{})
		if enabled && err != nil {
			t.Fatal(err)
		}
		if !enabled && (!errors.Is(err, core.ErrUnsupported) || !strings.Contains(err.Error(), "this instance is Free")) {
			t.Fatalf("missing backend reason: %v", err)
		}
	}
}

func TestCapabilityResolutionErrorPreventsWrite(t *testing.T) {
	problem := errors.New("schema access denied")
	p := &runtimeProvider{fakeProvider: newFake(), boardError: problem}
	s := New(p, testBoard, Options{DefaultStatus: "Todo"})
	if _, err := s.Create(context.Background(), core.Draft{Title: "x"}); !errors.Is(err, problem) {
		t.Fatalf("error = %v", err)
	}
	if len(p.drafts) != 0 {
		t.Fatal("write followed failed capability resolution")
	}
}

func TestAutomaticWorkflowDatesRespectRuntimeCapabilities(t *testing.T) {
	p := &runtimeProvider{fakeProvider: newFake(core.Task{ID: "1", Status: "Todo"})}
	s := New(p, testBoard, Options{})
	if _, err := s.Move(context.Background(), "1", "Done", false, false, EditOptions{}); err != nil {
		t.Fatal(err)
	}
	patch := p.lastPatch(t)
	if patch.Started != nil || patch.Completed != nil {
		t.Fatal("read-only backend timestamps must not be written")
	}
	_, err := s.Update(context.Background(), "1", core.Patch{Started: new(time.Time)}, EditOptions{})
	if !errors.Is(err, core.ErrUnsupported) {
		t.Fatalf("explicit date edit must be refused: %v", err)
	}
}
