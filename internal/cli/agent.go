package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/aramponi/yakanban/internal/app"
	"github.com/aramponi/yakanban/internal/core"
)

func newPickCommand(e *env) *cobra.Command {
	var (
		o      app.PickOptions
		noBody bool
	)
	cmd := &cobra.Command{
		Use:   "pick",
		Short: "Claim the next available task",
		Long: `Claim the highest-priority task that is unclaimed, unblocked and whose
dependencies are done — in one call.

Use this rather than list → edit → move: a task another agent takes in
between is detected and skipped, which is what makes several agents safe to
run against the same board.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := e.open(cmd.Context())
			if err != nil {
				return err
			}
			task, err := s.service.Pick(cmd.Context(), o)
			if err != nil {
				return err
			}
			p := e.Printer()
			if noBody && e.format() == "human" {
				task.Body = ""
			}
			return p.Task(*task)
		},
	}
	fl := cmd.Flags()
	fl.StringVar(&o.Agent, "claim", "", "agent name to claim as (required)")
	fl.StringVar(&o.Status, "status", "", "column to pick from (default: any non-terminal column)")
	fl.StringVar(&o.Move, "move", "", "also move the picked task to this column")
	fl.StringSliceVar(&o.Tags, "tags", nil, "only consider tasks carrying one of these tags")
	fl.BoolVar(&noBody, "no-body", false, "suppress the task body in the output")
	_ = cmd.MarkFlagRequired("claim")
	return cmd
}

func newHandoffCommand(e *env) *cobra.Command {
	var (
		o  app.HandoffOptions
		to string
	)
	cmd := &cobra.Command{
		Use:   "handoff ID",
		Short: "Park a task for someone else",
		Long: `Move a task to the waiting column, append a note saying where the work
stands, and optionally block it and release the claim — in one write.

Use it when you are done for now but the task is not: ready to merge, waiting
on a decision, or blocked on something outside your control.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := e.open(cmd.Context())
			if err != nil {
				return err
			}
			o.To, err = reviewColumn(to, s.cfg.Defaults.Review, s.service.Board())
			if err != nil {
				return err
			}
			task, err := s.service.Handoff(cmd.Context(), args[0], o)
			if err != nil {
				return err
			}
			return e.Printer().Task(*task)
		},
	}
	fl := cmd.Flags()
	fl.StringVar(&o.Agent, "claim", "", "agent name handing the task over (required)")
	fl.StringVar(&to, "to", "", "column to park the task in (default: the board's review column)")
	fl.StringVar(&o.Note, "note", "", "note appended to the task body")
	fl.StringVar(&o.Block, "block", "", "also mark the task blocked with this reason")
	fl.BoolVar(&o.Release, "release", false, "release the claim after the handoff")
	fl.BoolVarP(&o.Timestamp, "timestamp", "t", false, "prefix the note with a timestamp")
	_ = cmd.MarkFlagRequired("claim")
	return cmd
}

// reviewColumn resolves where a handoff parks work: the explicit flag, then
// the descriptor, then a column that looks like a review column.
func reviewColumn(flag, configured string, board core.BoardInfo) (string, error) {
	for _, candidate := range []string{flag, configured, "review"} {
		if candidate == "" {
			continue
		}
		if resolved, err := app.ResolveVocabulary(candidate, board.StatusNames()); err == nil {
			return resolved, nil
		}
	}
	return "", fmt.Errorf("%w: this board has no review column; pass --to COLUMN or set defaults.review in .yakanban.yml (columns: %s)",
		core.ErrInvalidInput, strings.Join(board.StatusNames(), ", "))
}
