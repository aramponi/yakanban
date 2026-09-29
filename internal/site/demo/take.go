// Package demo turns a recorded take of the hero clip into the timeline the
// page replays.
//
// A take (site/demo/record.sh) is a Claude Code session in stream-json and the
// board sampled beside it, both timestamped. It is raw: the session header
// carries the account, local paths and installed plugins, so a take never
// leaves the machine it was recorded on. What leaves is the timeline built
// here, which keeps only what the clip shows and refuses to be written if
// anything else slipped through.
package demo

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Take is one recording, as record.sh writes it.
type Take struct {
	Meta    Meta
	Session []Line
	Board   []Snapshot
}

// Meta is meta.json.
type Meta struct {
	Recorded   string   `json:"recorded"`
	Repository string   `json:"repository"`
	Yakanban   string   `json:"yakanban"`
	Claude     string   `json:"claude"`
	Prompts    []string `json:"prompts"`
}

// Line is one stream-json event, at the epoch millisecond it was printed.
type Line struct {
	At  int64
	Raw json.RawMessage
}

// Snapshot is the board at one instant.
type Snapshot struct {
	At    int64
	Tasks []Task
}

// Task is what a snapshot keeps of a ticket.
type Task struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Status   string `json:"status"`
	Priority string `json:"priority"`
}

// Load reads a take directory.
func Load(dir string) (*Take, error) {
	t := &Take{}
	b, err := os.ReadFile(filepath.Join(dir, "meta.json"))
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &t.Meta); err != nil {
		return nil, fmt.Errorf("meta.json: %w", err)
	}
	err = eachLine(filepath.Join(dir, "session.tsv"), func(at int64, v []byte) error {
		t.Session = append(t.Session, Line{At: at, Raw: append(json.RawMessage(nil), v...)})
		return nil
	})
	if err != nil {
		return nil, err
	}
	err = eachLine(filepath.Join(dir, "board.tsv"), func(at int64, v []byte) error {
		s := Snapshot{At: at}
		if err := json.Unmarshal(v, &s.Tasks); err != nil {
			return err
		}
		t.Board = append(t.Board, s)
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(t.Session) == 0 {
		return nil, fmt.Errorf("%s: empty session", dir)
	}
	if len(t.Board) == 0 {
		return nil, fmt.Errorf("%s: the board was never sampled", dir)
	}
	return t, nil
}

func eachLine(path string, fn func(at int64, v []byte) error) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 64<<20) // a tool result can be large
	for n := 1; sc.Scan(); n++ {
		ts, v, ok := strings.Cut(sc.Text(), "\t")
		if !ok {
			return fmt.Errorf("%s:%d: no timestamp", path, n)
		}
		at, err := strconv.ParseInt(ts, 10, 64)
		if err != nil {
			return fmt.Errorf("%s:%d: %w", path, n, err)
		}
		if err := fn(at, []byte(v)); err != nil {
			return fmt.Errorf("%s:%d: %w", path, n, err)
		}
	}
	return sc.Err()
}
