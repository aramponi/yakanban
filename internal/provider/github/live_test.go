package github

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

// The fixtures in testdata were captured from the real GitHub API against a
// throwaway project, with the exact queries in queries.go. They guard the
// decoding path against the shapes GitHub actually returns — including the
// built-in field values (Title, Assignees, Repository, Labels) that come back
// interleaved with yakanban's own fields.

func loadFixture(t *testing.T, name string, out any) {
	t.Helper()
	raw, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("reading the fixture: %v", err)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		t.Fatalf("decoding %s: %v", name, err)
	}
}

func TestDecodeLiveListResponse(t *testing.T) {
	var res struct {
		Node struct {
			Items struct {
				PageInfo struct {
					HasNextPage bool   `json:"hasNextPage"`
					EndCursor   string `json:"endCursor"`
				} `json:"pageInfo"`
				Nodes []itemNode `json:"nodes"`
			} `json:"items"`
		} `json:"node"`
	}
	loadFixture(t, "live_list.json", &res)

	nodes := res.Node.Items.Nodes
	if len(nodes) != 2 {
		t.Fatalf("got %d items, want the 2 issues of the fixture", len(nodes))
	}

	task := nodes[0].toTask()
	if task.ID != "1" || task.Title != "Fix the login redirect (session cookie)" {
		t.Fatalf("identity = %s %q", task.ID, task.Title)
	}
	if task.Status != "In Progress" || task.Priority != "high" || task.Class != "expedite" {
		t.Fatalf("single-select fields = %q/%q/%q", task.Status, task.Priority, task.Class)
	}
	if task.Estimate != "4h" {
		t.Fatalf("estimate = %q", task.Estimate)
	}
	if task.Blocked != "waiting on a product call" {
		t.Fatalf("blocked = %q", task.Blocked)
	}
	if strings.Join(task.DependsOn, ",") != "2,3" || task.Parent != "2" {
		t.Fatalf("dependencies = %v, parent = %q", task.DependsOn, task.Parent)
	}
	if task.Due == nil || task.Due.Format(dateLayout) != "2026-09-30" {
		t.Fatalf("due = %v", task.Due)
	}
	if task.Started == nil || task.Completed != nil {
		t.Fatalf("started = %v, completed = %v", task.Started, task.Completed)
	}
	if strings.Join(task.Tags, ",") != "bug,auth" {
		t.Fatalf("tags = %v, want the merged label list", task.Tags)
	}
	if len(task.Assignees) != 1 || task.Assignees[0] != "aramponi" {
		t.Fatalf("assignees = %v", task.Assignees)
	}
	if task.Claim == nil || task.Claim.Agent != "frost-maple-07" {
		t.Fatalf("claim = %+v", task.Claim)
	}
	if want := time.Date(2026, 9, 4, 13, 0, 0, 0, time.UTC); !task.Claim.Expires.Equal(want) {
		t.Fatalf("claim expiry = %s, want %s", task.Claim.Expires, want)
	}
	if itemID(&task) == "" {
		t.Fatal("the project item id should reach Metadata")
	}

	// The second issue carries only Status and Priority: everything else must
	// come back as a zero value rather than a stray empty string.
	plain := nodes[1].toTask()
	if plain.Estimate != "" || plain.Blocked != "" || plain.Claim != nil || plain.Due != nil {
		t.Fatalf("unset fields leaked a value: %+v", plain)
	}
}

func TestDecodeLiveIssueResponse(t *testing.T) {
	var res issueResult
	loadFixture(t, "live_issue.json", &res)

	issue := res.Repository.Issue
	if issue == nil {
		t.Fatal("the fixture should contain an issue")
	}
	if issue.NodeID == "" {
		t.Fatal("the issue node id is needed to add the issue to a project")
	}
	if len(issue.ProjectItems.Nodes) == 0 {
		t.Fatal("the issue should report its project items")
	}
	item := issue.ProjectItems.Nodes[0]
	if item.Project.ID == "" {
		t.Fatal("each project item must name its project, so Get can pick the right board")
	}

	node := itemNode{ID: item.ID, Content: issue.issueContent, FieldValues: item.FieldValues}
	task := node.toTask()
	if task.Status != "In Progress" || task.Claim == nil {
		t.Fatalf("the issue query returns the same field values as the list query, got %+v", task)
	}
}

// TestLiveFixturesCoverBuiltInFieldValues pins the reason byName() skips
// entries without a field name: GitHub interleaves value types yakanban does
// not model, and they must be ignored rather than crash the decoder.
func TestLiveFixturesCoverBuiltInFieldValues(t *testing.T) {
	raw, err := os.ReadFile("testdata/live_list.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, typeName := range []string{"ProjectV2ItemFieldUserValue", "ProjectV2ItemFieldRepositoryValue", "ProjectV2ItemFieldLabelValue"} {
		if !strings.Contains(string(raw), typeName) {
			t.Fatalf("the fixture no longer covers %s; re-capture it against a live project", typeName)
		}
	}
}
