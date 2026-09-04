package github

import (
	"context"
	"fmt"
	"strings"

	"github.com/aramponi/yakanban/internal/core"
)

var _ core.Brancher = (*Provider)(nil)

// linkedBranchNode is one entry of Issue.linkedBranches.
type linkedBranchNode struct {
	ID  string `json:"id"`
	Ref struct {
		Name   string `json:"name"`
		Prefix string `json:"prefix"`
		Target struct {
			OID string `json:"oid"`
		} `json:"target"`
	} `json:"ref"`
}

func (n linkedBranchNode) toBranch() core.Branch {
	return core.Branch{
		ID:   n.ID,
		Name: n.Ref.Name,
		Ref:  n.Ref.Prefix + n.Ref.Name,
		OID:  n.Ref.Target.OID,
	}
}

// CreateBranch creates the branch on GitHub and attaches it to the issue.
//
// The caller's name is passed through: GitHub would otherwise invent
// "<number>-<slug>" itself, and an agent needs to know the name before the
// branch exists so it can create its local branch without fetching.
func (p *Provider) CreateBranch(ctx context.Context, id string, req core.BranchRequest) (*core.Branch, error) {
	number, err := issueNumber(id)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.BaseOID) == "" {
		return nil, fmt.Errorf("%w: createLinkedBranch needs the commit to branch from", core.ErrInvalidInput)
	}
	task, err := p.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	issueNodeID, _ := task.Metadata["node_id"].(string)
	if issueNodeID == "" {
		return nil, fmt.Errorf("%w: could not resolve the node id of issue #%d", core.ErrNotFound, number)
	}
	var res struct {
		CreateLinkedBranch struct {
			LinkedBranch *linkedBranchNode `json:"linkedBranch"`
		} `json:"createLinkedBranch"`
	}
	vars := map[string]any{"issue": issueNodeID, "oid": req.BaseOID, "name": req.Name}
	if err := p.client.graphql(ctx, createLinkedBranchMutation, vars, &res); err != nil {
		return nil, err
	}
	// GitHub answers 200 with a null linkedBranch and no errors array when it
	// declines — notably when the branch already exists on the remote, which
	// it will not adopt. Reporting success here would be a lie.
	if res.CreateLinkedBranch.LinkedBranch == nil {
		return nil, fmt.Errorf("github did not link %q to issue #%d: the branch most likely already exists on the remote, and GitHub only links branches it creates itself (delete it, or pick another name)",
			req.Name, number)
	}
	branch := res.CreateLinkedBranch.LinkedBranch.toBranch()
	return &branch, nil
}

// Branches lists the branches attached to an issue.
func (p *Provider) Branches(ctx context.Context, id string) ([]core.Branch, error) {
	number, err := issueNumber(id)
	if err != nil {
		return nil, err
	}
	var res struct {
		Repository struct {
			Issue *struct {
				LinkedBranches struct {
					Nodes []linkedBranchNode `json:"nodes"`
				} `json:"linkedBranches"`
			} `json:"issue"`
		} `json:"repository"`
	}
	vars := map[string]any{"owner": p.settings.Owner, "repo": p.settings.Repo, "number": number}
	if err := p.client.graphql(ctx, linkedBranchesQuery, vars, &res); err != nil {
		return nil, err
	}
	if res.Repository.Issue == nil {
		return nil, fmt.Errorf("%w: issue #%d in %s/%s", core.ErrNotFound, number, p.settings.Owner, p.settings.Repo)
	}
	out := make([]core.Branch, 0, len(res.Repository.Issue.LinkedBranches.Nodes))
	for _, n := range res.Repository.Issue.LinkedBranches.Nodes {
		out = append(out, n.toBranch())
	}
	return out, nil
}

// UnlinkBranch detaches a branch from its issue. The git ref survives: GitHub
// has no mutation that does both, and deleting someone's work as a side effect
// of tidying a board would be indefensible.
func (p *Provider) UnlinkBranch(ctx context.Context, branchID string) error {
	if strings.TrimSpace(branchID) == "" {
		return fmt.Errorf("%w: a linked-branch id is required", core.ErrInvalidInput)
	}
	return p.client.graphql(ctx, deleteLinkedBranchMutation, map[string]any{"branch": branchID}, nil)
}
