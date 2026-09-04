package github

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aramponi/yakanban/internal/core"
)

// branchServer replies to the branch mutations, letting a test decide what
// createLinkedBranch answers.
type branchServer struct {
	t         *testing.T
	server    *httptest.Server
	linkReply string
	calls     []graphCall
}

func newBranchServer(t *testing.T, linkReply string) *branchServer {
	t.Helper()
	b := &branchServer{t: t, linkReply: linkReply}
	mux := http.NewServeMux()
	mux.HandleFunc("/graphql", func(w http.ResponseWriter, r *http.Request) {
		var call graphCall
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &call); err != nil {
			t.Fatalf("malformed request: %v", err)
		}
		b.calls = append(b.calls, call)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(call.Query, "fragment P on ProjectV2"):
			_, _ = io.WriteString(w, projectFixture)
		case strings.Contains(call.Query, "createLinkedBranch"):
			_, _ = io.WriteString(w, b.linkReply)
		case strings.Contains(call.Query, "deleteLinkedBranch"):
			_, _ = io.WriteString(w, `{"data":{"deleteLinkedBranch":{"clientMutationId":null}}}`)
		// Both issue queries ask for linkedBranches, so the full one has to be
		// matched first: only it reaches into the project items.
		case strings.Contains(call.Query, "projectItems"):
			_, _ = io.WriteString(w, issueWithBranchFixture)
		case strings.Contains(call.Query, "linkedBranches"):
			_, _ = io.WriteString(w, linkedBranchesFixture)
		default:
			_, _ = io.WriteString(w, `{"data":{}}`)
		}
	})
	b.server = httptest.NewServer(mux)
	t.Cleanup(b.server.Close)
	return b
}

func (b *branchServer) provider() *Provider {
	return &Provider{
		settings: Settings{Owner: "acme", Repo: "app", ProjectNumber: 3},
		client: &client{http: b.server.Client(), token: "t",
			restBase: b.server.URL, graphURL: b.server.URL + "/graphql", userAgent: "yakanban/test"},
	}
}

const issueWithBranchFixture = `{"data":{"repository":{"issue":{
  "id":"I_issue42","__typename":"Issue","number":42,"title":"Fix login","body":"","url":"u","state":"OPEN",
  "createdAt":"2026-09-01T08:00:00Z","updatedAt":"2026-09-03T09:30:00Z",
  "labels":{"nodes":[]},"assignees":{"nodes":[]},
  "linkedBranches":{"nodes":[
    {"id":"LB_1","ref":{"name":"42-fix-login","prefix":"refs/heads/","target":{"oid":"deadbeef"}}}]},
  "projectItems":{"nodes":[]}}}}}`

const linkedBranchesFixture = `{"data":{"repository":{"issue":{
  "id":"I_issue42",
  "linkedBranches":{"nodes":[
    {"id":"LB_1","ref":{"name":"42-fix-login","prefix":"refs/heads/","target":{"oid":"deadbeef"}}},
    {"id":"LB_2","ref":{"name":"feature/42-retry","prefix":"refs/heads/","target":{"oid":"cafe"}}}]}}}}}`

func TestCreateBranchSendsTheCallersName(t *testing.T) {
	reply := `{"data":{"createLinkedBranch":{"linkedBranch":{
      "id":"LB_9","ref":{"name":"42-fix-login","prefix":"refs/heads/","target":{"oid":"abc123"}}}}}}`
	s := newBranchServer(t, reply)

	branch, err := s.provider().CreateBranch(context.Background(), "42",
		core.BranchRequest{Name: "42-fix-login", BaseOID: "abc123"})
	if err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}
	if branch.Name != "42-fix-login" || branch.Ref != "refs/heads/42-fix-login" {
		t.Fatalf("branch = %+v", branch)
	}

	var mutation *graphCall
	for i := range s.calls {
		if strings.Contains(s.calls[i].Query, "createLinkedBranch") {
			mutation = &s.calls[i]
		}
	}
	if mutation == nil {
		t.Fatal("no createLinkedBranch call was made")
	}
	if mutation.Variables["name"] != "42-fix-login" {
		t.Fatalf("name = %v; GitHub must be told the name, never asked for one", mutation.Variables["name"])
	}
	if mutation.Variables["oid"] != "abc123" {
		t.Fatalf("oid = %v", mutation.Variables["oid"])
	}
	if mutation.Variables["issue"] != "I_issue42" {
		t.Fatalf("issue node id = %v", mutation.Variables["issue"])
	}
}

// GitHub answers 200 with a null linkedBranch and no errors array when it
// declines to link — notably for a branch that already exists on the remote.
// Verified against the live API on 2026-09-04.
func TestCreateBranchTreatsANullLinkedBranchAsFailure(t *testing.T) {
	s := newBranchServer(t, `{"data":{"createLinkedBranch":{"linkedBranch":null}}}`)

	_, err := s.provider().CreateBranch(context.Background(), "42",
		core.BranchRequest{Name: "99-already-pushed", BaseOID: "abc123"})
	if err == nil {
		t.Fatal("a null linkedBranch must not be reported as success")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("err = %v, want it to explain the likely cause", err)
	}
}

func TestCreateBranchRequiresABaseCommit(t *testing.T) {
	s := newBranchServer(t, `{"data":{}}`)
	_, err := s.provider().CreateBranch(context.Background(), "42", core.BranchRequest{Name: "x"})
	if err == nil || !strings.Contains(err.Error(), "commit") {
		t.Fatalf("err = %v, want a missing base commit to be refused before the call", err)
	}
}

func TestBranchesListsWhatIsAttached(t *testing.T) {
	s := newBranchServer(t, `{"data":{}}`)
	branches, err := s.provider().Branches(context.Background(), "42")
	if err != nil {
		t.Fatalf("Branches: %v", err)
	}
	if len(branches) != 2 {
		t.Fatalf("got %d branches, want 2: %+v", len(branches), branches)
	}
	if branches[1].Name != "feature/42-retry" {
		t.Fatalf("a slashed branch name did not survive: %+v", branches[1])
	}
	if branches[0].ID != "LB_1" {
		t.Fatalf("the link id is needed to detach it later, got %+v", branches[0])
	}
}

func TestGetCarriesLinkedBranchesInMetadata(t *testing.T) {
	s := newBranchServer(t, `{"data":{}}`)
	task, err := s.provider().Get(context.Background(), "42")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	names, ok := task.Metadata[core.MetaLinkedBranches].([]string)
	if !ok || len(names) != 1 || names[0] != "42-fix-login" {
		t.Fatalf("metadata = %v, want the linked branches to ride along with the task", task.Metadata)
	}
}

func TestUnlinkBranchRequiresAnID(t *testing.T) {
	s := newBranchServer(t, `{"data":{}}`)
	if err := s.provider().UnlinkBranch(context.Background(), "  "); err == nil {
		t.Fatal("an empty link id should be refused")
	}
}

func TestProviderAdvertisesTheBranchCapability(t *testing.T) {
	s := newBranchServer(t, `{"data":{}}`)
	if !s.provider().Capabilities().Has(core.CapLinkedBranch) {
		t.Fatal("the GitHub provider should advertise CapLinkedBranch")
	}
}
