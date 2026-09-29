package demo

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aramponi/yakanban/internal/cli"
)

// The committed timeline was recorded with some version of yakanban. These
// tests fail when the binary has since moved on in a way the clip would show:
// a command or a flag it no longer takes.

func committed(t *testing.T) *Timeline {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "site", "demo", "session.json"))
	if err != nil {
		t.Fatal(err)
	}
	var tl Timeline
	if err := json.Unmarshal(b, &tl); err != nil {
		t.Fatal(err)
	}
	return &tl
}

func TestTheCommittedClipOnlyShowsCommandsTheBinaryTakes(t *testing.T) {
	tl := committed(t)
	n := 0
	for _, e := range tl.Events {
		if e.Type != "tool" || !strings.HasPrefix(e.Cmd, "yakanban ") {
			continue
		}
		n++
		if err := cli.Check(shellWords(e.Cmd)[1:]); err != nil {
			t.Errorf("the clip shows %q, which yakanban no longer takes: %v", e.Cmd, err)
		}
	}
	if n == 0 {
		t.Fatal("the clip shows no yakanban command at all")
	}
}

func TestTheCommittedClipIsClean(t *testing.T) {
	tl := committed(t)
	if err := Scrub(tl); err != nil {
		t.Error(err)
	}
	for i := 1; i < len(tl.Events); i++ {
		if tl.Events[i].T < tl.Events[i-1].T {
			t.Fatalf("event %d goes back in time", i)
		}
	}
}
