package github

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/aramponi/yakanban/internal/core"
)

// itemJSON is the shape GitHub returns for one project item, trimmed to what
// yakanban reads.
const itemJSON = `{
  "id": "PVTI_item1",
  "isArchived": false,
  "content": {
    "__typename": "Issue",
    "number": 42,
    "title": "Fix the login redirect",
    "body": "Steps to reproduce",
    "url": "https://github.com/acme/app/issues/42",
    "state": "OPEN",
    "createdAt": "2026-09-01T08:00:00Z",
    "updatedAt": "2026-09-03T09:30:00Z",
    "labels": { "nodes": [ { "name": "bug" }, { "name": "auth" } ] },
    "assignees": { "nodes": [ { "login": "alice" } ] }
  },
  "fieldValues": { "nodes": [
    { "__typename": "ProjectV2ItemFieldSingleSelectValue", "name": "In Progress", "field": { "name": "Status" } },
    { "__typename": "ProjectV2ItemFieldSingleSelectValue", "name": "high", "field": { "name": "Priority" } },
    { "__typename": "ProjectV2ItemFieldSingleSelectValue", "name": "expedite", "field": { "name": "Class" } },
    { "__typename": "ProjectV2ItemFieldTextValue", "text": "4h", "field": { "name": "Estimate" } },
    { "__typename": "ProjectV2ItemFieldTextValue", "text": "waiting on review", "field": { "name": "Blocked" } },
    { "__typename": "ProjectV2ItemFieldTextValue", "text": "12, #13", "field": { "name": "Depends On" } },
    { "__typename": "ProjectV2ItemFieldTextValue", "text": "7", "field": { "name": "Parent" } },
    { "__typename": "ProjectV2ItemFieldTextValue", "text": "frost-maple", "field": { "name": "Claim" } },
    { "__typename": "ProjectV2ItemFieldTextValue", "text": "2026-09-03T10:30:00Z", "field": { "name": "Claim Expires" } },
    { "__typename": "ProjectV2ItemFieldDateValue", "date": "2026-09-10", "field": { "name": "Due" } },
    { "__typename": "ProjectV2ItemFieldDateValue", "date": "2026-09-02", "field": { "name": "Started" } }
  ] }
}`

func TestItemToTask(t *testing.T) {
	var item itemNode
	if err := json.Unmarshal([]byte(itemJSON), &item); err != nil {
		t.Fatalf("decoding the fixture: %v", err)
	}
	task := item.toTask()

	if task.ID != "42" {
		t.Fatalf("ID = %q, want the issue number", task.ID)
	}
	if task.Status != "In Progress" || task.Priority != "high" || task.Class != "expedite" {
		t.Fatalf("single-select fields were not mapped: %+v", task)
	}
	if task.Estimate != "4h" || task.Blocked != "waiting on review" || task.Parent != "7" {
		t.Fatalf("text fields were not mapped: %+v", task)
	}
	if len(task.DependsOn) != 2 || task.DependsOn[0] != "12" || task.DependsOn[1] != "13" {
		t.Fatalf("dependencies = %v, want [12 13] with the # stripped", task.DependsOn)
	}
	if len(task.Tags) != 2 || task.Tags[0] != "bug" {
		t.Fatalf("labels did not become tags: %v", task.Tags)
	}
	if len(task.Assignees) != 1 || task.Assignees[0] != "alice" {
		t.Fatalf("assignees = %v", task.Assignees)
	}
	if task.Claim == nil || task.Claim.Agent != "frost-maple" {
		t.Fatalf("claim = %+v", task.Claim)
	}
	if want := time.Date(2026, 9, 3, 10, 30, 0, 0, time.UTC); !task.Claim.Expires.Equal(want) {
		t.Fatalf("claim expiry = %s, want %s", task.Claim.Expires, want)
	}
	if task.Due == nil || task.Due.Format(dateLayout) != "2026-09-10" {
		t.Fatalf("due = %v", task.Due)
	}
	if task.Completed != nil {
		t.Fatalf("completed should be nil when the project has no value, got %v", task.Completed)
	}
	if itemID(&task) != "PVTI_item1" {
		t.Fatalf("the project item id should be kept in metadata, got %v", task.Metadata)
	}
	if task.Metadata["state"] != "open" {
		t.Fatalf("issue state = %v, want open", task.Metadata["state"])
	}
}

func TestProjectPatchClearsAndSets(t *testing.T) {
	cur := &core.Task{ID: "1", DependsOn: []string{"2", "3"}}
	empty := ""
	reason := "waiting on legal"
	due := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)

	updates := projectPatch(cur, core.Patch{
		Blocked:    &reason,
		Due:        &due,
		RemoveDeps: []string{"2"},
		AddDeps:    []string{"9"},
	})
	byField := indexUpdates(updates)

	if u := byField[fieldBlocked]; u.Kind != updateText || u.Text != reason {
		t.Fatalf("Blocked update = %+v", u)
	}
	if u := byField[fieldDue]; u.Kind != updateDate || !u.Date.Equal(due) {
		t.Fatalf("Due update = %+v", u)
	}
	if u := byField[fieldDependsOn]; u.Text != "3,9" {
		t.Fatalf("dependencies = %q, want 3,9", u.Text)
	}

	updates = projectPatch(cur, core.Patch{Blocked: &empty, ClearDue: true, ReleaseClaim: true})
	byField = indexUpdates(updates)
	for _, name := range []string{fieldBlocked, fieldDue, fieldClaim, fieldClaimExpires} {
		if byField[name].Kind != updateClear {
			t.Fatalf("%s should be cleared, got %+v", name, byField[name])
		}
	}
}

func TestProjectPatchWritesClaimExpiry(t *testing.T) {
	expires := time.Date(2026, 9, 4, 13, 0, 0, 0, time.UTC)
	updates := projectPatch(&core.Task{}, core.Patch{Claim: &core.Claim{Agent: "tidal-vale", Expires: expires}})
	byField := indexUpdates(updates)

	if byField[fieldClaim].Text != "tidal-vale" {
		t.Fatalf("claim agent = %q", byField[fieldClaim].Text)
	}
	if got := byField[fieldClaimExpires].Text; got != "2026-09-04T13:00:00Z" {
		t.Fatalf("claim expiry = %q, want RFC3339 UTC", got)
	}
}

func TestFieldValueForRejectsAnUnknownOption(t *testing.T) {
	f := field{
		Name:     fieldPriority,
		DataType: typeSingleSelect,
		Options:  []option{{ID: "opt1", Name: "low"}, {ID: "opt2", Name: "high"}},
	}
	if _, err := fieldValueFor(f, fieldUpdate{Field: fieldPriority, Kind: updateSelect, Text: "urgent"}); err == nil {
		t.Fatal("an option outside the project vocabulary should be refused")
	}
	value, err := fieldValueFor(f, fieldUpdate{Field: fieldPriority, Kind: updateSelect, Text: "HIGH"})
	if err != nil {
		t.Fatalf("case-insensitive match failed: %v", err)
	}
	if value["singleSelectOptionId"] != "opt2" {
		t.Fatalf("value = %v, want the option id", value)
	}
}

func TestIssueNumber(t *testing.T) {
	for _, in := range []string{"42", "#42", " 42 "} {
		n, err := issueNumber(in)
		if err != nil || n != 42 {
			t.Fatalf("issueNumber(%q) = %d, %v", in, n, err)
		}
	}
	for _, in := range []string{"", "abc", "0", "-1", "PROJ-12"} {
		if _, err := issueNumber(in); err == nil {
			t.Fatalf("issueNumber(%q) should fail", in)
		}
	}
}

func TestMergeListIsCaseInsensitiveAndOrdered(t *testing.T) {
	got := mergeList([]string{"bug", "auth"}, []string{"ui", "BUG"}, []string{"AUTH"})
	if len(got) != 2 || got[0] != "bug" || got[1] != "ui" {
		t.Fatalf("mergeList = %v, want [bug ui]", got)
	}
}

func TestEndpointsSupportEnterprise(t *testing.T) {
	rest, graph := endpoints("")
	if rest != "https://api.github.com" || graph != "https://api.github.com/graphql" {
		t.Fatalf("github.com endpoints = %s / %s", rest, graph)
	}
	rest, graph = endpoints("https://github.acme.corp/")
	if rest != "https://github.acme.corp/api/v3" || graph != "https://github.acme.corp/api/graphql" {
		t.Fatalf("GHES endpoints = %s / %s", rest, graph)
	}
}

func indexUpdates(updates fieldUpdates) map[string]fieldUpdate {
	out := make(map[string]fieldUpdate, len(updates))
	for _, u := range updates {
		out[u.Field] = u
	}
	return out
}
