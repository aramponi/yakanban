package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/aramponi/yakanban/internal/app"
	"github.com/aramponi/yakanban/internal/core"
	"github.com/aramponi/yakanban/internal/gitrepo"
)

func newBranchCommand(e *env) *cobra.Command {
	var (
		o      app.BranchOptions
		from   string
		list   bool
		unlink string
		dryRun bool
	)
	cmd := &cobra.Command{
		Use:   "branch ID",
		Short: "Attach a branch to a task",
		Long: `Create a branch for a task and attach it to the ticket, so the work shows
up in the tracker's own UI.

The name comes from the branch template (or --name) and is decided here, not
read back from the remote: the branch starts at a commit you already have, so
you can create your local branch from it straight away.

    BRANCH=$(yakanban branch 42 --claim "$AGENT")
    git worktree add "$(yakanban branch 42 --dry-run --json | jq -r .worktree)" -b "$BRANCH" origin/main`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			s, err := e.open(cmd.Context())
			if err != nil {
				return err
			}
			switch {
			case list:
				return e.listBranches(cmd.Context(), s, id)
			case unlink != "":
				return e.unlinkBranch(cmd.Context(), s, id, unlink)
			}

			repo := gitrepo.Open(e.workDir())
			if o.Repo == "" {
				if name, err := repo.Name(); err == nil {
					o.Repo = name
				}
			}
			task, err := s.service.Get(cmd.Context(), id)
			if err != nil {
				return err
			}
			name, err := s.service.BranchName(*task, o)
			if err != nil {
				return err
			}
			worktree, err := s.service.WorktreePath(*task, o)
			if err != nil {
				return err
			}
			if dryRun {
				return e.printBranch(core.Branch{Name: name}, worktree, "")
			}
			oid, base, err := repo.ResolveBase(from)
			if err != nil {
				return err
			}
			o.Name = name
			o.BaseOID = oid
			branch, err := s.service.CreateBranch(cmd.Context(), id, o)
			if err != nil {
				return err
			}
			return e.printBranch(*branch, worktree, base)
		},
	}
	fl := cmd.Flags()
	fl.StringVar(&o.Name, "name", "", "branch name (default: from the branch template)")
	fl.StringVar(&from, "from", "", "base branch or commit (default: the upstream of the current branch)")
	fl.StringVar(&o.Agent, "claim", "", "claim the task while attaching the branch")
	fl.BoolVar(&o.Force, "force", false, "act even if another agent holds the claim")
	fl.BoolVar(&list, "list", false, "list the branches attached to the task")
	fl.StringVar(&unlink, "unlink", "", "detach this branch from the task (the git ref is kept)")
	fl.BoolVar(&dryRun, "dry-run", false, "print the names that would be used, and create nothing")
	return cmd
}

// printBranch writes the branch name alone on stdout so it can be captured in
// a shell variable; everything else goes to --json or to stderr.
func (e *env) printBranch(branch core.Branch, worktree, base string) error {
	p := e.Printer()
	if e.format() == "json" {
		return p.JSON(map[string]any{
			"name":     branch.Name,
			"ref":      branch.Ref,
			"oid":      branch.OID,
			"id":       branch.ID,
			"base":     base,
			"worktree": worktree,
		})
	}
	p.Printf("%s\n", branch.Name)
	return nil
}

func (e *env) listBranches(ctx context.Context, s *session, id string) error {
	branches, err := s.service.Branches(ctx, id)
	if err != nil {
		return err
	}
	p := e.Printer()
	if e.format() == "json" {
		if branches == nil {
			branches = []core.Branch{}
		}
		return p.JSON(branches)
	}
	for _, b := range branches {
		p.Printf("%s\n", b.Name)
	}
	return nil
}

func (e *env) unlinkBranch(ctx context.Context, s *session, id, name string) error {
	branches, err := s.service.Branches(ctx, id)
	if err != nil {
		return err
	}
	for _, b := range branches {
		if strings.EqualFold(b.Name, name) || b.ID == name {
			if err := s.service.UnlinkBranch(ctx, b.ID); err != nil {
				return err
			}
			e.Printer().Printf("detached %s from %s (the git ref still exists; delete it with `git push --delete origin %s`)\n",
				b.Name, id, b.Name)
			return nil
		}
	}
	return fmt.Errorf("%w: %s has no branch called %q", core.ErrNotFound, id, name)
}
