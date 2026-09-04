package cli

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/aramponi/yakanban/internal/config"
	"github.com/aramponi/yakanban/internal/core"
)

func TestAdoptStatusesKeepsLocalSemantics(t *testing.T) {
	local := []core.Status{
		{Name: "Backlog", Initial: true},
		{Name: "In Progress", RequireClaim: true, WIPLimit: 3},
		{Name: "Done", Terminal: true},
	}
	live := []core.Status{{Name: "Todo"}, {Name: "In progress"}, {Name: "Done"}}

	got := adoptStatuses(local, live)

	if len(got) != 3 || got[0].Name != "Todo" {
		t.Fatalf("the backend owns the column list, got %+v", got)
	}
	if got[1].Name != "In progress" {
		t.Fatalf("the backend owns the spelling too, got %q", got[1].Name)
	}
	if !got[1].RequireClaim || got[1].WIPLimit != 3 {
		t.Fatalf("local semantics were lost on a matched column: %+v", got[1])
	}
	if !got[2].Terminal {
		t.Fatalf("Done should stay terminal, got %+v", got[2])
	}
}

func TestAdoptStatusesGivesEndpointsToAForeignBoard(t *testing.T) {
	local := []core.Status{{Name: "Backlog", Initial: true}, {Name: "Done", Terminal: true}}
	live := []core.Status{{Name: "Inbox"}, {Name: "Doing"}, {Name: "Shipped"}}

	got := adoptStatuses(local, live)

	if !got[0].Initial || !got[2].Terminal {
		t.Fatalf("a board with entirely different columns should still get endpoints: %+v", got)
	}
}

// A GitHub project created from the default template has Todo/In Progress/Done
// and no Backlog: adoption drops the column the descriptor marked as intake,
// and the board would silently stop stamping Started.
func TestAdoptStatusesRestoresAMissingIntakeColumn(t *testing.T) {
	local := []core.Status{
		{Name: "Backlog", Initial: true},
		{Name: "Todo"},
		{Name: "In Progress", RequireClaim: true},
		{Name: "Done", Terminal: true},
	}
	live := []core.Status{{Name: "Todo"}, {Name: "In Progress"}, {Name: "Done"}}

	got := adoptStatuses(local, live)

	if !got[0].Initial {
		t.Fatalf("no column is initial after adoption: %+v", got)
	}
	if !got[2].Terminal {
		t.Fatalf("no column is terminal after adoption: %+v", got)
	}
	if !got[1].RequireClaim {
		t.Fatalf("matched columns should keep their semantics: %+v", got[1])
	}
}

func TestAdoptReviewDropsAMissingColumn(t *testing.T) {
	statuses := []core.Status{{Name: "Todo"}, {Name: "In Progress"}, {Name: "Done"}}
	if got := adoptReview("Review", statuses); got != "" {
		t.Fatalf("adoptReview = %q, want it cleared when the board has no such column", got)
	}
	if got := adoptReview("review", append(statuses, core.Status{Name: "Review"})); got != "Review" {
		t.Fatalf("adoptReview = %q, want the board's spelling", got)
	}
}

func TestBranchingModelFromTheFlag(t *testing.T) {
	cmd := &cobra.Command{}
	got, err := resolveBranchingModel(cmd, "git-flow")
	if err != nil || got != "git-flow" {
		t.Fatalf("resolveBranchingModel = %q, %v", got, err)
	}
}

func TestBranchingModelRejectsAnUnknownName(t *testing.T) {
	_, err := resolveBranchingModel(&cobra.Command{}, "gitflow-ish")
	var invalid *core.InvalidValueError
	if !errors.As(err, &invalid) {
		t.Fatalf("err = %v, want the list of models", err)
	}
}

// A piped or CI run must pick the default rather than block on a prompt.
func TestBranchingModelDefaultsWhenNobodyCanAnswer(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.SetIn(strings.NewReader(""))
	cmd.SetOut(io.Discard)
	got, err := resolveBranchingModel(cmd, "")
	if err != nil {
		t.Fatalf("resolveBranchingModel: %v", err)
	}
	if got != config.ModelTrunkBased {
		t.Fatalf("model = %q, want the trunk-based default", got)
	}
}
