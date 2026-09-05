package gitlab

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/aramponi/yakanban/internal/core"
)

func TestBootstrapAdoptsCapturedBoardWithoutChangingColumns(t *testing.T) {
	s := newServer(t)
	b, err := s.p.Bootstrap(context.Background(), core.BootstrapOptions{Statuses: []core.Status{{Name: "Different"}}, Priorities: []string{"medium"}, Classes: []core.Class{{Name: "standard"}}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(b.StatusNames(), ",") != "Open,Doing,Review,Closed" {
		t.Fatalf("adopted board: %+v", b)
	}
	for _, r := range s.requests {
		if !strings.HasPrefix(r, "GET ") {
			t.Fatalf("adoption changed existing data: %s", r)
		}
	}
	if s.p.ConfigSettings()["board_id"] != 11585676 {
		t.Fatal("selected board not persisted")
	}
}

func TestBootstrapCreatesCapturedBoard(t *testing.T) {
	s := newServer(t)
	s.p.settings.BoardID = 0
	s.route = func(w http.ResponseWriter, r *http.Request) bool {
		switch {
		case strings.HasSuffix(r.URL.Path, "/boards") && r.Method == "GET":
			_, _ = w.Write([]byte("[]"))
			return true
		case strings.HasSuffix(r.URL.Path, "/boards") && r.Method == "POST":
			replay(t, w, "board_created")
			return true
		case strings.HasSuffix(r.URL.Path, "/lists") && r.Method == "POST":
			var body map[string]int
			_ = json.NewDecoder(r.Body).Decode(&body)
			switch body["label_id"] {
			case 53388806:
				replay(t, w, "list_Doing")
			case 53388807:
				replay(t, w, "list_Review")
			default:
				t.Errorf("unexpected list: %+v", body)
			}
			return true
		}
		return false
	}
	b, err := s.p.Bootstrap(context.Background(), core.BootstrapOptions{Name: "new", Statuses: []core.Status{{Name: "Backlog", Initial: true}, {Name: "Doing"}, {Name: "Review"}, {Name: "Done", Terminal: true}}})
	if err != nil {
		t.Fatal(err)
	}
	if b.Name != "yakanban live validation" || s.p.settings.BoardID != 11585676 {
		t.Fatalf("bootstrap: %+v", b)
	}
}

func TestBootstrapRequiresExplicitSelectionWhenAmbiguous(t *testing.T) {
	s := newServer(t)
	s.p.settings.BoardID = 0
	s.route = func(w http.ResponseWriter, r *http.Request) bool {
		if strings.HasSuffix(r.URL.Path, "/boards") {
			c := fixture(t, "boards")
			var boards []board
			_ = json.Unmarshal(c.Body, &boards)
			boards = append(boards, boards[0])
			_ = json.NewEncoder(w).Encode(boards)
			return true
		}
		return false
	}
	_, err := s.p.Bootstrap(context.Background(), core.BootstrapOptions{})
	if !errors.Is(err, core.ErrInvalidInput) || !strings.Contains(err.Error(), "board_id") {
		t.Fatalf("ambiguous adoption: %v", err)
	}
}
