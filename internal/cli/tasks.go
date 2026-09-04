package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/aramponi/yakanban/internal/app"
	"github.com/aramponi/yakanban/internal/core"
)

func newListCommand(e *env) *cobra.Command {
	var (
		f          core.Filter
		sortField  string
		reverse    bool
		tags       []string
		statuses   []string
		priorities []string
		classes    []string
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List tasks",
		Long:  "List the tasks of the board, with the same filters as the rest of the tool.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := e.open(cmd.Context())
			if err != nil {
				return err
			}
			f.Statuses, f.Priorities, f.Classes, f.Tags = statuses, priorities, classes, tags
			tasks, err := s.service.List(cmd.Context(), f, sortField, reverse)
			if err != nil {
				return err
			}
			return e.Printer().Tasks(tasks)
		},
	}
	fl := cmd.Flags()
	fl.StringSliceVar(&statuses, "status", nil, "filter by status (comma-separated)")
	fl.StringSliceVar(&priorities, "priority", nil, "filter by priority (comma-separated)")
	fl.StringSliceVar(&classes, "class", nil, "filter by class of service")
	fl.StringSliceVar(&tags, "tag", nil, "filter by tag (repeatable, all must match)")
	fl.StringVar(&f.Assignee, "assignee", "", "filter by assignee")
	fl.StringVar(&f.ClaimedBy, "claimed-by", "", "filter by claiming agent")
	fl.StringVar(&f.Parent, "parent", "", "filter by parent task ID")
	fl.StringVarP(&f.Search, "search", "s", "", "search in title, body and tags")
	fl.BoolVar(&f.Blocked, "blocked", false, "show only blocked tasks")
	fl.BoolVar(&f.NotBlocked, "not-blocked", false, "show only non-blocked tasks")
	fl.BoolVar(&f.Unclaimed, "unclaimed", false, "show only unclaimed or expired-claim tasks")
	fl.BoolVar(&f.Unblocked, "unblocked", false, "show only tasks whose dependencies are all done")
	fl.IntVarP(&f.Limit, "limit", "n", 0, "limit the number of results")
	fl.StringVar(&sortField, "sort", "id", "sort field (id, title, status, priority, created, updated, due)")
	fl.BoolVarP(&reverse, "reverse", "r", false, "reverse the sort order")
	return cmd
}

func newShowCommand(e *env) *cobra.Command {
	return &cobra.Command{
		Use:   "show ID",
		Short: "Show a task in full",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := e.open(cmd.Context())
			if err != nil {
				return err
			}
			task, err := s.service.Get(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return e.Printer().Task(*task)
		},
	}
}

func newCreateCommand(e *env) *cobra.Command {
	var (
		d         core.Draft
		title     string
		due       string
		claim     string
		dependsOn []string
	)
	cmd := &cobra.Command{
		Use:   "create [TITLE]",
		Short: "Create a task",
		Long:  "Open a new issue and put it on the board with the given fields.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
				d.Title = args[0]
			}
			if title != "" {
				d.Title = title
			}
			s, err := e.open(cmd.Context())
			if err != nil {
				return err
			}
			if due != "" {
				parsed, err := parseDate(due)
				if err != nil {
					return err
				}
				d.Due = parsed
			}
			d.DependsOn = splitIDs(dependsOn)
			if claim != "" {
				d.Claim = &core.Claim{Agent: claim}
			}
			task, err := s.service.Create(cmd.Context(), d)
			if err != nil {
				return err
			}
			return e.Printer().Task(*task)
		},
	}
	fl := cmd.Flags()
	fl.StringVar(&title, "title", "", "task title (alternative to the positional argument)")
	fl.StringVar(&d.Body, "body", "", "task body (markdown)")
	fl.StringVar(&d.Status, "status", "", "initial status (default from the board descriptor)")
	fl.StringVar(&d.Priority, "priority", "", "priority (default from the board descriptor)")
	fl.StringVar(&d.Class, "class", "", "class of service")
	fl.StringSliceVar(&d.Tags, "tags", nil, "comma-separated tags (GitHub labels)")
	fl.StringSliceVar(&d.Assignees, "assignee", nil, "assignees (repeatable)")
	fl.StringVar(&d.Estimate, "estimate", "", "time estimate, e.g. 4h or 2d")
	fl.StringVar(&due, "due", "", "due date (YYYY-MM-DD)")
	fl.StringVar(&d.Parent, "parent", "", "parent task ID")
	fl.StringSliceVar(&dependsOn, "depends-on", nil, "dependency task IDs (comma-separated)")
	fl.StringVar(&claim, "claim", "", "claim the new task for an agent")
	return cmd
}

func newEditCommand(e *env) *cobra.Command {
	var (
		title, body, appendBody           string
		status, priority, class, estimate string
		due, started, completed           string
		parent, block                     string
		assignees                         []string
		addTags, removeTags               []string
		addDeps, removeDeps               []string
		clearDue, clearStarted            bool
		clearCompleted, clearParent       bool
		unblock                           bool
		eo                                app.EditOptions
	)
	cmd := &cobra.Command{
		Use:   "edit ID[,ID...]",
		Short: "Edit task fields",
		Long: `Change one or more fields of one or more tasks.

Only the flags you pass are applied; everything else is left untouched.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := e.open(cmd.Context())
			if err != nil {
				return err
			}
			p := core.Patch{
				ClearDue:       clearDue,
				ClearStarted:   clearStarted,
				ClearCompleted: clearCompleted,
				ClearParent:    clearParent,
				AddTags:        addTags,
				RemoveTags:     removeTags,
				AddDeps:        splitIDs(addDeps),
				RemoveDeps:     splitIDs(removeDeps),
			}
			set := func(name string, dst **string, v string) {
				if cmd.Flags().Changed(name) {
					val := v
					*dst = &val
				}
			}
			set("title", &p.Title, title)
			set("body", &p.Body, body)
			set("append-body", &p.AppendBody, appendBody)
			set("status", &p.Status, status)
			set("priority", &p.Priority, priority)
			set("class", &p.Class, class)
			set("estimate", &p.Estimate, estimate)
			set("parent", &p.Parent, parent)
			set("block", &p.Blocked, block)
			if unblock {
				empty := ""
				p.Blocked = &empty
			}
			if cmd.Flags().Changed("assignee") {
				list := assignees
				p.Assignees = &list
			}
			for _, spec := range []struct {
				name string
				raw  string
				dst  **time.Time
			}{
				{"due", due, &p.Due},
				{"started", started, &p.Started},
				{"completed", completed, &p.Completed},
			} {
				if !cmd.Flags().Changed(spec.name) {
					continue
				}
				parsed, err := parseDate(spec.raw)
				if err != nil {
					return err
				}
				*spec.dst = parsed
			}
			printer := e.Printer()
			for _, id := range splitIDs(args) {
				task, err := s.service.Update(cmd.Context(), id, p, eo)
				if err != nil {
					return err
				}
				if err := printer.Task(*task); err != nil {
					return err
				}
			}
			return nil
		},
	}
	fl := cmd.Flags()
	fl.StringVar(&title, "title", "", "new title")
	fl.StringVar(&body, "body", "", "replace the body")
	fl.StringVarP(&appendBody, "append-body", "a", "", "append text to the body")
	fl.BoolVarP(&eo.Timestamp, "timestamp", "t", false, "prefix appended text with a timestamp")
	fl.StringVar(&status, "status", "", "new status")
	fl.StringVar(&priority, "priority", "", "new priority")
	fl.StringVar(&class, "class", "", "new class of service")
	fl.StringVar(&estimate, "estimate", "", "new estimate")
	fl.StringSliceVar(&assignees, "assignee", nil, "replace the assignees")
	fl.StringSliceVar(&addTags, "add-tag", nil, "add tags")
	fl.StringSliceVar(&removeTags, "remove-tag", nil, "remove tags")
	fl.StringSliceVar(&addDeps, "add-dep", nil, "add dependency task IDs")
	fl.StringSliceVar(&removeDeps, "remove-dep", nil, "remove dependency task IDs")
	fl.StringVar(&due, "due", "", "due date (YYYY-MM-DD)")
	fl.BoolVar(&clearDue, "clear-due", false, "clear the due date")
	fl.StringVar(&started, "started", "", "started date (YYYY-MM-DD)")
	fl.BoolVar(&clearStarted, "clear-started", false, "clear the started date")
	fl.StringVar(&completed, "completed", "", "completed date (YYYY-MM-DD)")
	fl.BoolVar(&clearCompleted, "clear-completed", false, "clear the completed date")
	fl.StringVar(&parent, "parent", "", "set the parent task ID")
	fl.BoolVar(&clearParent, "clear-parent", false, "clear the parent")
	fl.StringVar(&block, "block", "", "mark the task blocked with a reason")
	fl.BoolVar(&unblock, "unblock", false, "clear the blocked state")
	fl.StringVar(&eo.Agent, "claim", "", "claim (or renew the claim on) the task for an agent")
	fl.BoolVar(&eo.Release, "release", false, "release the claim")
	fl.BoolVar(&eo.Force, "force", false, "write even if another agent holds the claim")
	return cmd
}

func newMoveCommand(e *env) *cobra.Command {
	var (
		next, prev bool
		eo         app.EditOptions
	)
	cmd := &cobra.Command{
		Use:   "move ID[,ID...] [STATUS]",
		Short: "Move a task to another column",
		Long: `Move one or more tasks to a status, either by name or relative to the
current column with --next / --prev.

Moving out of the initial column stamps Started; moving into a terminal
column stamps Completed.`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			status := ""
			if len(args) == 2 {
				status = args[1]
			}
			switch {
			case next && prev:
				return fmt.Errorf("%w: --next and --prev are mutually exclusive", core.ErrInvalidInput)
			case status == "" && !next && !prev:
				return fmt.Errorf("%w: give a status, or use --next / --prev", core.ErrInvalidInput)
			case status != "" && (next || prev):
				return fmt.Errorf("%w: give a status or --next/--prev, not both", core.ErrInvalidInput)
			}
			s, err := e.open(cmd.Context())
			if err != nil {
				return err
			}
			printer := e.Printer()
			for _, id := range splitIDs(args[:1]) {
				task, err := s.service.Move(cmd.Context(), id, status, next, prev, eo)
				if err != nil {
					return err
				}
				if err := printer.Task(*task); err != nil {
					return err
				}
			}
			return nil
		},
	}
	fl := cmd.Flags()
	fl.BoolVar(&next, "next", false, "move to the next column")
	fl.BoolVar(&prev, "prev", false, "move to the previous column")
	fl.StringVar(&eo.Agent, "claim", "", "claim the task during the move")
	fl.BoolVar(&eo.Force, "force", false, "move even if another agent holds the claim")
	return cmd
}

func newDeleteCommand(e *env) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete ID[,ID...]",
		Short: "Close a task and remove it from the board",
		Long: `Close the underlying issue and archive its project item.

GitHub only lets repository admins truly delete an issue, and deleting one
would take its discussion with it — so yakanban closes and archives instead.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !yes {
				return fmt.Errorf("%w: pass --yes to confirm", core.ErrInvalidInput)
			}
			s, err := e.open(cmd.Context())
			if err != nil {
				return err
			}
			for _, id := range splitIDs(args) {
				if err := s.service.Delete(cmd.Context(), id); err != nil {
					return err
				}
				e.Printer().Printf("closed and archived %s\n", id)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm without prompting")
	return cmd
}

// splitIDs flattens comma-separated ID arguments into a clean list.
func splitIDs(args []string) []string {
	var out []string
	for _, arg := range args {
		for _, part := range strings.Split(arg, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}

// parseDate accepts the YYYY-MM-DD form used across the CLI.
func parseDate(s string) (*time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return nil, fmt.Errorf("%w: %q is not a date (expected YYYY-MM-DD)", core.ErrInvalidInput, s)
	}
	return &t, nil
}
