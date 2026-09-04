package cli

import (
	"strings"
	"testing"

	"github.com/aramponi/yakanban/internal/config"
	"github.com/aramponi/yakanban/internal/core"
)

// descriptor is a board file with the usual five columns.
func descriptor() *config.Config {
	cfg := config.Default("demo", "github")
	return cfg
}

func names(b core.BoardInfo) string { return strings.Join(b.StatusNames(), ",") }

// The point of the design: somebody adds a column in the tracker's own web UI
// and yakanban follows on the next command, with no edit and no re-init.
func TestAColumnAddedInTheTrackerAppears(t *testing.T) {
	live := &core.BoardInfo{Statuses: []core.Status{
		{Name: "Backlog"}, {Name: "Todo"}, {Name: "In Progress"},
		{Name: "Blocked by legal"}, {Name: "Review"}, {Name: "Done"},
	}}

	board := mergeBoard(descriptor(), live)

	if got := names(board); got != "Backlog,Todo,In Progress,Blocked by legal,Review,Done" {
		t.Fatalf("columns = %s, want the new one in the position the tracker gave it", got)
	}
}

// The converse: a column removed in the web UI stops existing here, even
// though the committed descriptor still lists it.
func TestAColumnRemovedInTheTrackerDisappears(t *testing.T) {
	live := &core.BoardInfo{Statuses: []core.Status{
		{Name: "Todo"}, {Name: "In Progress"}, {Name: "Done"},
	}}

	board := mergeBoard(descriptor(), live)

	if got := names(board); got != "Todo,In Progress,Done" {
		t.Fatalf("columns = %s, want only the ones the tracker has", got)
	}
}

// A column the descriptor knows keeps the semantics GitHub has nowhere to
// store: which one is terminal, which requires a claim, what its WIP limit is.
func TestLocalSemanticsSurviveTheMerge(t *testing.T) {
	cfg := descriptor()
	for i := range cfg.Statuses {
		if cfg.Statuses[i].Name == "In Progress" {
			cfg.Statuses[i].WIPLimit = 3
		}
	}
	live := &core.BoardInfo{Statuses: []core.Status{
		{Name: "Backlog"}, {Name: "Todo"}, {Name: "In Progress"}, {Name: "Review"}, {Name: "Done"},
	}}

	board := mergeBoard(cfg, live)

	inProgress, ok := board.Status("In Progress")
	if !ok {
		t.Fatal("In Progress went missing")
	}
	if !inProgress.RequireClaim || inProgress.WIPLimit != 3 {
		t.Fatalf("In Progress = %+v, want its claim rule and WIP limit kept", inProgress)
	}
	if !board.IsTerminal("Done") || !board.IsInitial("Backlog") {
		t.Fatal("the endpoints were lost in the merge")
	}
}

// A brand new column has no semantics until somebody writes them down, and
// yakanban must not invent any.
func TestANewColumnCarriesNoSemantics(t *testing.T) {
	live := &core.BoardInfo{Statuses: []core.Status{
		{Name: "Backlog"}, {Name: "Todo"}, {Name: "Waiting on legal"}, {Name: "Done"},
	}}

	board := mergeBoard(descriptor(), live)

	waiting, ok := board.Status("Waiting on legal")
	if !ok {
		t.Fatal("the new column is missing")
	}
	if waiting.RequireClaim || waiting.Terminal || waiting.Initial || waiting.WIPLimit != 0 {
		t.Fatalf("a new column should start plain, got %+v", waiting)
	}
}

// Renaming a column in the web UI must not leave yakanban writing the old
// spelling back into the project.
func TestTheTrackerOwnsTheSpelling(t *testing.T) {
	live := &core.BoardInfo{Statuses: []core.Status{
		{Name: "Backlog"}, {Name: "Todo"}, {Name: "in progress"}, {Name: "Review"}, {Name: "Done"},
	}}

	board := mergeBoard(descriptor(), live)

	status, ok := board.Status("In Progress")
	if !ok {
		t.Fatal("the renamed column should still be found case-insensitively")
	}
	if status.Name != "in progress" {
		t.Fatalf("name = %q, want the tracker's own spelling", status.Name)
	}
	if !status.RequireClaim {
		t.Fatal("a rename should not drop the column's semantics")
	}
}

func TestPrioritiesComeFromTheTrackerWhenItHasThem(t *testing.T) {
	live := &core.BoardInfo{
		Statuses:   []core.Status{{Name: "Todo"}},
		Priorities: []string{"p0", "p1"},
		URL:        "https://github.com/users/acme/projects/2",
	}

	board := mergeBoard(descriptor(), live)

	if strings.Join(board.Priorities, ",") != "p0,p1" {
		t.Fatalf("priorities = %v, want the project's own field options", board.Priorities)
	}
	if board.URL != live.URL {
		t.Fatalf("URL = %q, want the project's", board.URL)
	}
}

func TestAnUnreachableBoardFallsBackToTheDescriptor(t *testing.T) {
	board := mergeBoard(descriptor(), nil)
	if got := names(board); got == "" {
		t.Fatal("with no live board, the committed columns should still be usable")
	}
}
