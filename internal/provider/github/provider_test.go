package github

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/aramponi/yakanban/internal/core"
)

// fakeGitHub records the calls it receives and replies with canned payloads,
// so the adapter can be exercised end to end without a network.
type fakeGitHub struct {
	t          *testing.T
	server     *httptest.Server
	graphCalls []graphCall
	restCalls  []restCall
}

type graphCall struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables"`
}

type restCall struct {
	Method string
	Path   string
	Body   map[string]any
}

func newFakeGitHub(t *testing.T) *fakeGitHub {
	t.Helper()
	f := &fakeGitHub{t: t}
	mux := http.NewServeMux()
	mux.HandleFunc("/graphql", f.handleGraphQL)
	mux.HandleFunc("/", f.handleREST)
	f.server = httptest.NewServer(mux)
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeGitHub) provider() *Provider {
	return &Provider{
		settings: Settings{Owner: "acme", Repo: "app", ProjectNumber: 3},
		client: &client{
			http:      f.server.Client(),
			token:     "test-token",
			restBase:  f.server.URL,
			graphURL:  f.server.URL + "/graphql",
			userAgent: "yakanban/test",
		},
	}
}

func (f *fakeGitHub) handleGraphQL(w http.ResponseWriter, r *http.Request) {
	var call graphCall
	body, _ := io.ReadAll(r.Body)
	if err := json.Unmarshal(body, &call); err != nil {
		f.t.Fatalf("malformed GraphQL request: %v", err)
	}
	f.graphCalls = append(f.graphCalls, call)
	if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
		f.t.Fatalf("Authorization header = %q", got)
	}

	var payload string
	switch {
	case strings.Contains(call.Query, "fragment P on ProjectV2"):
		payload = projectFixture
	case strings.Contains(call.Query, "items(first:100"):
		payload = listFixture
	case strings.Contains(call.Query, "issue(number:$number)"):
		payload = issueFixture
	default:
		payload = `{"data":{}}`
	}
	w.Header().Set("Content-Type", "application/json")
	io.WriteString(w, payload)
}

func (f *fakeGitHub) handleREST(w http.ResponseWriter, r *http.Request) {
	body := map[string]any{}
	raw, _ := io.ReadAll(r.Body)
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &body)
	}
	f.restCalls = append(f.restCalls, restCall{Method: r.Method, Path: r.URL.Path, Body: body})
	w.Header().Set("Content-Type", "application/json")
	io.WriteString(w, `{"number":42,"node_id":"I_issue42","html_url":"https://github.com/acme/app/issues/42"}`)
}

const projectFixture = `{"data":{"organization":null,"user":{"projectV2":{
  "id":"PVT_project","number":3,"title":"App board","url":"https://github.com/users/acme/projects/3",
  "fields":{"nodes":[
    {"__typename":"ProjectV2SingleSelectField","id":"F_status","name":"Status","dataType":"SINGLE_SELECT",
     "options":[{"id":"o_backlog","name":"Backlog"},{"id":"o_todo","name":"Todo"},{"id":"o_wip","name":"In Progress"},{"id":"o_done","name":"Done"}]},
    {"__typename":"ProjectV2SingleSelectField","id":"F_priority","name":"Priority","dataType":"SINGLE_SELECT",
     "options":[{"id":"o_low","name":"low"},{"id":"o_high","name":"high"}]},
    {"__typename":"ProjectV2Field","id":"F_claim","name":"Claim","dataType":"TEXT"},
    {"__typename":"ProjectV2Field","id":"F_claim_exp","name":"Claim Expires","dataType":"TEXT"},
    {"__typename":"ProjectV2Field","id":"F_blocked","name":"Blocked","dataType":"TEXT"},
    {"__typename":"ProjectV2Field","id":"F_due","name":"Due","dataType":"DATE"}
  ]}}}}}`

const listFixture = `{"data":{"node":{"items":{
  "pageInfo":{"hasNextPage":false,"endCursor":null},
  "nodes":[
    {"id":"PVTI_1","isArchived":false,
     "content":{"__typename":"Issue","number":42,"title":"Fix login","body":"","url":"u","state":"OPEN",
       "createdAt":"2026-09-01T08:00:00Z","updatedAt":"2026-09-03T09:30:00Z",
       "labels":{"nodes":[{"name":"bug"}]},"assignees":{"nodes":[]}},
     "fieldValues":{"nodes":[
       {"__typename":"ProjectV2ItemFieldSingleSelectValue","name":"Todo","field":{"name":"Status"}}]}},
    {"id":"PVTI_2","isArchived":true,
     "content":{"__typename":"Issue","number":43,"title":"Archived","body":"","url":"u","state":"CLOSED",
       "createdAt":"2026-09-01T08:00:00Z","updatedAt":"2026-09-01T08:00:00Z",
       "labels":{"nodes":[]},"assignees":{"nodes":[]}},
     "fieldValues":{"nodes":[]}},
    {"id":"PVTI_3","isArchived":false,
     "content":{},"fieldValues":{"nodes":[]}}
  ]}}}}`

const issueFixture = `{"data":{"repository":{"issue":{
  "id":"I_issue42","__typename":"Issue","number":42,"title":"Fix login","body":"body","url":"u","state":"OPEN",
  "createdAt":"2026-09-01T08:00:00Z","updatedAt":"2026-09-03T09:30:00Z",
  "labels":{"nodes":[{"name":"bug"}]},"assignees":{"nodes":[{"login":"alice"}]},
  "projectItems":{"nodes":[
    {"id":"PVTI_other","isArchived":false,"project":{"id":"PVT_other"},"fieldValues":{"nodes":[
      {"__typename":"ProjectV2ItemFieldSingleSelectValue","name":"Done","field":{"name":"Status"}}]}},
    {"id":"PVTI_1","isArchived":false,"project":{"id":"PVT_project"},"fieldValues":{"nodes":[
      {"__typename":"ProjectV2ItemFieldSingleSelectValue","name":"Todo","field":{"name":"Status"}}]}}
  ]}}}}}`

func TestBoardReadsTheProjectVocabulary(t *testing.T) {
	f := newFakeGitHub(t)
	board, err := f.provider().Board(context.Background())
	if err != nil {
		t.Fatalf("Board: %v", err)
	}
	if got := board.StatusNames(); strings.Join(got, ",") != "Backlog,Todo,In Progress,Done" {
		t.Fatalf("columns = %v", got)
	}
	if strings.Join(board.Priorities, ",") != "low,high" {
		t.Fatalf("priorities = %v", board.Priorities)
	}
	if board.URL == "" {
		t.Fatal("the board should carry the project URL")
	}
}

func TestListSkipsArchivedAndNonIssues(t *testing.T) {
	f := newFakeGitHub(t)
	tasks, err := f.provider().List(context.Background(), core.Filter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(tasks) != 1 || tasks[0].ID != "42" {
		t.Fatalf("List = %+v, want only the open issue", tasks)
	}
	if tasks[0].Status != "Todo" {
		t.Fatalf("status = %q", tasks[0].Status)
	}
}

func TestGetPicksTheItemOfThisProject(t *testing.T) {
	f := newFakeGitHub(t)
	task, err := f.provider().Get(context.Background(), "42")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if itemID(task) != "PVTI_1" {
		t.Fatalf("item id = %q, want the item belonging to PVT_project", itemID(task))
	}
	if task.Status != "Todo" {
		t.Fatalf("status = %q, want the value from this project, not another board", task.Status)
	}
	if task.Metadata["node_id"] != "I_issue42" {
		t.Fatalf("metadata = %v, want the issue node id for later mutations", task.Metadata)
	}
}

func TestUpdateSplitsIssueAndProjectWrites(t *testing.T) {
	f := newFakeGitHub(t)
	title := "Fix login redirect"
	status := "In Progress"
	expires := time.Date(2026, 9, 4, 13, 0, 0, 0, time.UTC)

	_, err := f.provider().Update(context.Background(), "42", core.Patch{
		Title:   &title,
		Status:  &status,
		AddTags: []string{"auth"},
		Claim:   &core.Claim{Agent: "frost-maple", Expires: expires},
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	if len(f.restCalls) != 1 {
		t.Fatalf("expected one REST call, got %+v", f.restCalls)
	}
	patch := f.restCalls[0]
	if patch.Method != "PATCH" || patch.Path != "/repos/acme/app/issues/42" {
		t.Fatalf("REST call = %s %s", patch.Method, patch.Path)
	}
	if patch.Body["title"] != title {
		t.Fatalf("title was not sent to the issue API: %v", patch.Body)
	}
	labels, _ := patch.Body["labels"].([]any)
	if len(labels) != 2 || labels[0] != "bug" || labels[1] != "auth" {
		t.Fatalf("labels = %v, want the existing ones plus auth", patch.Body["labels"])
	}

	fields := map[string]any{}
	for _, call := range f.graphCalls {
		if !strings.Contains(call.Query, "updateProjectV2ItemFieldValue") {
			continue
		}
		if call.Variables["item"] != "PVTI_1" {
			t.Fatalf("field write targeted item %v", call.Variables["item"])
		}
		fields[call.Variables["field"].(string)] = call.Variables["value"]
	}
	statusValue, ok := fields["F_status"].(map[string]any)
	if !ok || statusValue["singleSelectOptionId"] != "o_wip" {
		t.Fatalf("status write = %v, want the In Progress option id", fields["F_status"])
	}
	claimValue, ok := fields["F_claim"].(map[string]any)
	if !ok || claimValue["text"] != "frost-maple" {
		t.Fatalf("claim write = %v", fields["F_claim"])
	}
	expiryValue, _ := fields["F_claim_exp"].(map[string]any)
	if expiryValue["text"] != "2026-09-04T13:00:00Z" {
		t.Fatalf("claim expiry write = %v", fields["F_claim_exp"])
	}
}

func TestUpdateWithOnlyIssueFieldsSkipsTheProject(t *testing.T) {
	f := newFakeGitHub(t)
	body := "new body"
	if _, err := f.provider().Update(context.Background(), "42", core.Patch{Body: &body}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	for _, call := range f.graphCalls {
		if strings.Contains(call.Query, "updateProjectV2ItemFieldValue") {
			t.Fatal("a body-only change should not touch the project fields")
		}
	}
}

func TestUnknownStatusOptionIsRefusedBeforeWriting(t *testing.T) {
	f := newFakeGitHub(t)
	status := "Shipped"
	_, err := f.provider().Update(context.Background(), "42", core.Patch{Status: &status})
	var invalid *core.InvalidValueError
	if err == nil || !asInvalid(err, &invalid) {
		t.Fatalf("err = %v, want an InvalidValueError listing the project columns", err)
	}
}

func TestGraphQLErrorsSurface(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, `{"errors":[{"type":"FORBIDDEN","message":"Resource not accessible by personal access token (scope)"}]}`)
	}))
	defer srv.Close()
	c := &client{http: srv.Client(), token: "t", graphURL: srv.URL, restBase: srv.URL}

	err := c.graphql(context.Background(), "query{}", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "gh auth refresh") {
		t.Fatalf("err = %v, want a message pointing at the missing project scope", err)
	}
}

func TestRateLimitIsReportedClearly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", "1757000000")
		w.WriteHeader(http.StatusForbidden)
		io.WriteString(w, `{"message":"API rate limit exceeded"}`)
	}))
	defer srv.Close()
	c := &client{http: srv.Client(), token: "t", graphURL: srv.URL, restBase: srv.URL}

	err := c.graphql(context.Background(), "query{}", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "rate limit") {
		t.Fatalf("err = %v, want a rate-limit message", err)
	}
}

func asInvalid(err error, target **core.InvalidValueError) bool {
	for err != nil {
		if v, ok := err.(*core.InvalidValueError); ok {
			*target = v
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}
