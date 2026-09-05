package gitlab

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aramponi/yakanban/internal/cache"
	"github.com/aramponi/yakanban/internal/core"
)

type capture struct {
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers"`
	Body    json.RawMessage   `json:"body"`
}

func fixture(t *testing.T, name string) capture {
	t.Helper()
	raw, err := os.ReadFile("testdata/" + name + ".json")
	if err != nil {
		t.Fatal(err)
	}
	var c capture
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	return c
}
func replay(t *testing.T, w http.ResponseWriter, name string) {
	t.Helper()
	c := fixture(t, name)
	for k, v := range c.Headers {
		w.Header().Set(k, v)
	}
	w.WriteHeader(c.Status)
	if c.Status != 204 {
		_, _ = w.Write(c.Body)
	}
}

type testServer struct {
	p        *Provider
	requests []string
	route    func(http.ResponseWriter, *http.Request) bool
}

func newServer(t *testing.T) *testServer {
	t.Helper()
	s := &testServer{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.requests = append(s.requests, r.Method+" "+r.URL.RequestURI())
		if r.Header.Get("Authorization") != "Bearer fixture-token" {
			t.Error("missing authentication header")
		}
		if r.Method != "GET" && r.Body != http.NoBody && r.ContentLength != 0 && r.Header.Get("Content-Type") != "application/json" {
			t.Error("missing JSON Content-Type")
		}
		if s.route != nil && s.route(w, r) {
			return
		}
		path := r.URL.Path
		switch {
		case path == "/api/v4/projects/aramponi/mysandbox":
			replay(t, w, "project")
		case path == "/api/v4/namespaces/53636805":
			replay(t, w, "namespace")
		case strings.HasSuffix(path, "/boards/11585676"):
			replay(t, w, "board")
		case strings.HasSuffix(path, "/boards"):
			replay(t, w, "boards")
		case strings.HasSuffix(path, "/labels"):
			replay(t, w, "labels")
		case path == "/api/v4/users":
			replay(t, w, "users")
		case strings.HasSuffix(path, "/issues/1"):
			replay(t, w, "issue")
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
			http.Error(w, "unexpected request", 500)
		}
	}))
	t.Cleanup(server.Close)
	c := newClient("gitlab.com", "fixture-token", "test")
	c.base = server.URL + "/api/v4"
	c.http = server.Client()
	s.p = &Provider{settings: Settings{Host: "gitlab.com", Project: "aramponi/mysandbox", BoardID: 11585676}, client: c, store: cache.New(t.TempDir(), time.Minute, true)}
	return s
}

func TestCapturedBoardAndIssue(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()
	b, err := s.p.Board(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(b.StatusNames(), ",") != "Open,Doing,Review,Closed" {
		t.Fatalf("statuses: %v", b.StatusNames())
	}
	if strings.Join(b.Priorities, ",") != "low,medium,high,critical" {
		t.Fatalf("priorities: %v", b.Priorities)
	}
	if b.Capabilities.Has(core.CapClaims) || b.Capabilities.Has(core.CapDependencies) || !b.Capabilities.Has(core.CapDelete) {
		t.Fatalf("Free capabilities: %+v", b.Capabilities)
	}
	task, err := s.p.Get(ctx, "1")
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != "Doing" || task.Priority != "medium" || task.Class != "standard" || task.Estimate != "4h" || task.Due == nil || task.Due.Format(time.DateOnly) != "2026-10-01" || task.Assignees[0] != "aramponi" || strings.Join(task.Tags, ",") != "unrelated" {
		t.Fatalf("mapping: %+v", task)
	}
	if len(s.requests) != 5 {
		t.Fatalf("schema not reused: %v", s.requests)
	}
}

func TestCapturedConflictIsNotGuessed(t *testing.T) {
	s := newServer(t)
	s.route = func(w http.ResponseWriter, r *http.Request) bool {
		if strings.HasSuffix(r.URL.Path, "/issues/1") {
			replay(t, w, "conflicting_labels")
			return true
		}
		return false
	}
	_, err := s.p.Get(context.Background(), "1")
	if !errors.Is(err, core.ErrInvalidInput) || !strings.Contains(err.Error(), "conflicting status labels") {
		t.Fatalf("error: %v", err)
	}
}

func TestCapturedNullFieldsAndClosedTimestamp(t *testing.T) {
	for _, name := range []string{"issue_closed", "issue_reopened", "clear_due_assignees"} {
		t.Run(name, func(t *testing.T) {
			s := newServer(t)
			s.route = func(w http.ResponseWriter, r *http.Request) bool {
				if strings.HasSuffix(r.URL.Path, "/issues/1") {
					replay(t, w, name)
					return true
				}
				return false
			}
			got, err := s.p.Get(context.Background(), "1")
			if err != nil {
				t.Fatal(err)
			}
			if name == "issue_closed" && (got.Status != "Closed" || got.Completed == nil) {
				t.Fatalf("closed: %+v", got)
			}
			if name == "issue_reopened" && (got.Status != "Doing" || got.Completed != nil) {
				t.Fatalf("reopened: %+v", got)
			}
			if name == "clear_due_assignees" && (got.Due != nil || len(got.Assignees) != 0) {
				t.Fatalf("cleared: %+v", got)
			}
		})
	}
}

func TestCapturedPagination(t *testing.T) {
	s := newServer(t)
	s.route = func(w http.ResponseWriter, r *http.Request) bool {
		if strings.HasSuffix(r.URL.Path, "/issues") {
			name := "issues_page" + r.URL.Query().Get("page")
			replay(t, w, name)
			return true
		}
		return false
	}
	got, err := s.p.List(context.Background(), core.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID == got[1].ID {
		t.Fatalf("pagination: %+v", got)
	}
}

func TestUnsupportedDraftNeverCreatesAnIssue(t *testing.T) {
	for name, draft := range map[string]core.Draft{
		"claim":        {Title: "x", Status: "Open", Claim: &core.Claim{Agent: "a"}},
		"dependency":   {Title: "x", Status: "Open", DependsOn: []string{"2"}},
		"parent":       {Title: "x", Status: "Open", Parent: "2"},
		"reserved tag": {Title: "x", Status: "Open", Tags: []string{"Doing"}},
		"bad duration": {Title: "x", Status: "Open", Estimate: "not-a-duration"},
	} {
		t.Run(name, func(t *testing.T) {
			s := newServer(t)
			_, err := s.p.Create(context.Background(), draft)
			if err == nil {
				t.Fatal("unsupported draft accepted")
			}
			for _, r := range s.requests {
				if !strings.HasPrefix(r, "GET ") {
					t.Fatalf("write before refusal: %s", r)
				}
			}
		})
	}
}

func TestUnknownPlanIsNotReportedAsFree(t *testing.T) {
	s := newServer(t)
	s.route = func(w http.ResponseWriter, r *http.Request) bool {
		if strings.Contains(r.URL.Path, "/namespaces/") {
			http.Error(w, `{"message":"403 Forbidden"}`, http.StatusForbidden)
			return true
		}
		return false
	}
	b, err := s.p.Board(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	reason := b.Capabilities.Reasons[core.CapDependencies]
	if !strings.Contains(reason, "unknown") || strings.Contains(reason, "is Free") {
		t.Fatalf("invented tier: %s", reason)
	}
}

func TestCapturedAPIRefusalsAndNullSuccess(t *testing.T) {
	for name, sentinel := range map[string]error{"dependency_refused": core.ErrAuth, "get_deleted": core.ErrNotFound, "invalid_estimate": core.ErrInvalidInput} {
		t.Run(name, func(t *testing.T) {
			s := newServer(t)
			s.route = func(w http.ResponseWriter, _ *http.Request) bool { replay(t, w, name); return true }
			_, err := s.p.client.request(context.Background(), "POST", "/test", map[string]any{}, nil)
			if !errors.Is(err, sentinel) {
				t.Fatalf("error: %v", err)
			}
		})
	}
	s := newServer(t)
	s.route = func(w http.ResponseWriter, _ *http.Request) bool { _, _ = w.Write([]byte("null")); return true }
	var result issue
	if _, err := s.p.client.request(context.Background(), "POST", "/test", map[string]any{}, &result); err == nil {
		t.Fatal("null success accepted")
	}
}

func TestCapturedDelete(t *testing.T) {
	s := newServer(t)
	s.route = func(w http.ResponseWriter, r *http.Request) bool {
		if r.Method == "DELETE" {
			replay(t, w, "delete_target")
			return true
		}
		return false
	}
	if err := s.p.Delete(context.Background(), "2"); err != nil {
		t.Fatal(err)
	}
}

func TestSettingsAndAuthentication(t *testing.T) {
	settings, err := ParseSettings(map[string]any{"host": "https://gitlab.example.com:8443", "project": "team/subgroup/repo", "board_id": "12"})
	if err != nil {
		t.Fatal(err)
	}
	if settings.Host != "gitlab.example.com:8443" || settings.BoardID != 12 {
		t.Fatalf("settings: %+v", settings)
	}
	for _, host := range []string{"evil.example/path", "user:password@gitlab.com", "gitlab.com?redirect=evil", "http://gitlab.com"} {
		if _, err := ParseSettings(map[string]any{"host": host, "project": "a/b"}); err == nil {
			t.Errorf("accepted host %q", host)
		}
	}
	t.Setenv("GITLAB_TOKEN", "explicit-test-token")
	token, err := ResolveToken("gitlab.example.com")
	if err != nil || token != "explicit-test-token" {
		t.Fatalf("environment credential not preferred: %v", err)
	}
}

func TestSyntheticScopedBoardRefused(t *testing.T) {
	s := newServer(t)
	s.route = func(w http.ResponseWriter, r *http.Request) bool {
		if strings.Contains(r.URL.Path, "/boards/") {
			c := fixture(t, "board")
			var b map[string]any
			if err := json.Unmarshal(c.Body, &b); err != nil {
				t.Fatal(err)
			}
			b["milestone"] = map[string]any{"id": 1}
			_ = json.NewEncoder(w).Encode(b)
			return true
		}
		return false
	}
	if _, err := s.p.Board(context.Background()); !errors.Is(err, core.ErrUnsupported) {
		t.Fatalf("scoped board: %v", err)
	}
}

func TestCreatePreservesContentAndVerifiesReadback(t *testing.T) {
	s := newServer(t)
	created := false
	s.route = func(w http.ResponseWriter, r *http.Request) bool {
		if r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/issues") {
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body["description"] != "User text stays intact." || body["labels"] != "unrelated,Doing,priority::medium,class::standard" {
				t.Errorf("payload: %v", body)
			}
			if _, ok := body["state_event"]; ok {
				t.Error("state_event is not a create field")
			}
			created = true
			replay(t, w, "issue_created")
			return true
		}
		if strings.HasSuffix(r.URL.Path, "/time_estimate") {
			replay(t, w, "estimate_set")
			return true
		}
		return false
	}
	due, _ := time.Parse(time.DateOnly, "2026-10-01")
	task, err := s.p.Create(context.Background(), core.Draft{Title: "yakanban live mapping fixture", Body: "User text stays intact.", Status: "Doing", Priority: "medium", Class: "standard", Estimate: "4h", Tags: []string{"unrelated"}, Assignees: []string{"aramponi"}, Due: &due})
	if err != nil || !created || task.ID != "1" {
		t.Fatalf("create: %+v, %v", task, err)
	}
}

func TestPartialCreateReportsExistingIssue(t *testing.T) {
	s := newServer(t)
	s.route = func(w http.ResponseWriter, r *http.Request) bool {
		if r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/issues") {
			replay(t, w, "issue_created")
			return true
		}
		if strings.HasSuffix(r.URL.Path, "/time_estimate") {
			replay(t, w, "invalid_estimate")
			return true
		}
		return false
	}
	_, err := s.p.Create(context.Background(), core.Draft{Title: "x", Status: "Open", Estimate: "1h"})
	if err == nil || !strings.Contains(err.Error(), "#1 was created") {
		t.Fatalf("partial creation error: %v", err)
	}
}

func TestUpdateRetainsUnrelatedLabelsAndChecksStoredState(t *testing.T) {
	s := newServer(t)
	updated := false
	review := "Review"
	s.route = func(w http.ResponseWriter, r *http.Request) bool {
		if r.Method == "PUT" && strings.HasSuffix(r.URL.Path, "/issues/1") {
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["add_labels"] != "Review" || body["remove_labels"] != "Doing" {
				t.Errorf("move payload: %+v", body)
			}
			if _, ok := body["labels"]; ok {
				t.Error("move replaces unrelated labels")
			}
			updated = true
			replay(t, w, "issue")
			return true
		}
		return false
	}
	// Fault injection: server accepts the PUT but returns the old state on GET.
	_, err := s.p.Update(context.Background(), "1", core.Patch{Status: &review})
	if !updated || err == nil || !strings.Contains(err.Error(), "did not retain") {
		t.Fatalf("ignored update accepted: %v", err)
	}
}

func TestSyntheticPaidPlanEnablesDependencies(t *testing.T) {
	s := newServer(t)
	s.route = func(w http.ResponseWriter, r *http.Request) bool {
		if strings.Contains(r.URL.Path, "/namespaces/") {
			c := fixture(t, "namespace")
			var namespace map[string]any
			_ = json.Unmarshal(c.Body, &namespace)
			namespace["plan"] = "premium"
			_ = json.NewEncoder(w).Encode(namespace)
			return true
		}
		if strings.HasSuffix(r.URL.Path, "/links") {
			c := fixture(t, "links")
			var links []map[string]any
			_ = json.Unmarshal(c.Body, &links)
			links[0]["link_type"] = "is_blocked_by"
			_ = json.NewEncoder(w).Encode(links)
			return true
		}
		return false
	}
	b, err := s.p.Board(context.Background())
	if err != nil || !b.Capabilities.Has(core.CapDependencies) {
		t.Fatalf("paid capabilities: %+v, %v", b, err)
	}
	task, err := s.p.Get(context.Background(), "1")
	if err != nil || len(task.DependsOn) != 1 || task.DependsOn[0] != "2" {
		t.Fatalf("paid mapping: %+v, %v", task, err)
	}
}

func TestPaginationRejectsRepeatedNextPage(t *testing.T) {
	s := newServer(t)
	s.route = func(w http.ResponseWriter, _ *http.Request) bool {
		w.Header().Set("X-Next-Page", "1")
		_, _ = fmt.Fprint(w, "[]")
		return true
	}
	if _, err := pages[issue](context.Background(), s.p.client, "/test"); err == nil {
		t.Fatal("pagination loop accepted")
	}
}

func TestIssueIDsCannotEscapeEndpoint(t *testing.T) {
	for _, bad := range []string{"../boards", "0", "-1", "1?state=closed"} {
		if _, err := issueID(bad); !errors.Is(err, core.ErrInvalidInput) {
			t.Fatalf("bad IID %q: %v", bad, err)
		}
	}
	id, err := issueID("#12")
	if err != nil || id != strconv.Itoa(12) {
		t.Fatalf("IID: %q, %v", id, err)
	}
}

func TestSyntheticPaidDependencyWritesAndRemoval(t *testing.T) {
	s := newServer(t)
	linked := false
	s.route = func(w http.ResponseWriter, r *http.Request) bool {
		if strings.Contains(r.URL.Path, "/namespaces/") {
			c := fixture(t, "namespace")
			var data map[string]any
			_ = json.Unmarshal(c.Body, &data)
			data["plan"] = "premium"
			_ = json.NewEncoder(w).Encode(data)
			return true
		}
		if strings.HasSuffix(r.URL.Path, "/links") && r.Method == "GET" {
			c := fixture(t, "links")
			var links []map[string]any
			_ = json.Unmarshal(c.Body, &links)
			if linked {
				links[0]["link_type"] = "is_blocked_by"
			} else {
				links = []map[string]any{}
			}
			_ = json.NewEncoder(w).Encode(links)
			return true
		}
		if strings.HasSuffix(r.URL.Path, "/links") && r.Method == "POST" {
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["link_type"] != "is_blocked_by" || body["target_issue_iid"] != "2" {
				t.Errorf("dependency payload: %+v", body)
			}
			linked = true
			c := fixture(t, "related_created")
			var data map[string]any
			_ = json.Unmarshal(c.Body, &data)
			data["link_type"] = "is_blocked_by"
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(data)
			return true
		}
		if strings.Contains(r.URL.Path, "/links/") && r.Method == "DELETE" {
			linked = false
			replay(t, w, "related_created")
			return true
		}
		return false
	}
	got, err := s.p.Update(context.Background(), "1", core.Patch{AddDeps: []string{"2"}})
	if err != nil || len(got.DependsOn) != 1 {
		t.Fatalf("add dependency: %+v, %v", got, err)
	}
	got, err = s.p.Update(context.Background(), "1", core.Patch{RemoveDeps: []string{"2"}})
	if err != nil || len(got.DependsOn) != 0 {
		t.Fatalf("remove dependency: %+v, %v", got, err)
	}
}

func TestCreateCanStartInClosedColumn(t *testing.T) {
	s := newServer(t)
	closed := false
	s.route = func(w http.ResponseWriter, r *http.Request) bool {
		if strings.HasSuffix(r.URL.Path, "/issues") && r.Method == "POST" {
			replay(t, w, "issue_created")
			return true
		}
		if strings.HasSuffix(r.URL.Path, "/issues/1") && r.Method == "PUT" {
			closed = true
			replay(t, w, "issue_closed")
			return true
		}
		if strings.HasSuffix(r.URL.Path, "/issues/1") && r.Method == "GET" && closed {
			replay(t, w, "issue_closed")
			return true
		}
		return false
	}
	got, err := s.p.Create(context.Background(), core.Draft{Title: "yakanban live mapping fixture", Body: "User text stays intact.", Status: "Closed", Priority: "medium", Class: "standard", Assignees: []string{"aramponi"}})
	if err != nil || got.Status != "Closed" || got.Completed == nil {
		t.Fatalf("create closed: %+v, %v", got, err)
	}
}

func TestClientNeverFollowsTokenRedirects(t *testing.T) {
	reached := make(chan bool, 1)
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached <- true; w.WriteHeader(http.StatusOK) }))
	defer destination.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, destination.URL, http.StatusFound) }))
	defer source.Close()
	c := newClient("gitlab.example.com", "test-token", "test")
	c.base = source.URL
	if _, err := c.request(context.Background(), "GET", "/redirect", nil, nil); err == nil {
		t.Fatal("redirect accepted")
	}
	select {
	case <-reached:
		t.Fatal("credential-bearing redirect followed")
	default:
	}
}

func TestEstimateWriteVerifiesFinalStoredTime(t *testing.T) {
	s := newServer(t)
	s.route = func(w http.ResponseWriter, r *http.Request) bool {
		if strings.HasSuffix(r.URL.Path, "/time_estimate") {
			replay(t, w, "estimate_set")
			return true
		}
		if strings.HasSuffix(r.URL.Path, "/issues/1") {
			replay(t, w, "issue_created")
			return true
		}
		return false
	}
	estimate := "4h"
	_, err := s.p.Update(context.Background(), "1", core.Patch{Estimate: &estimate})
	if err == nil || !strings.Contains(err.Error(), "did not retain its estimate") {
		t.Fatalf("ignored estimate: %v", err)
	}
}
