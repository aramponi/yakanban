package demo

import (
	"encoding/json"
	"strings"
	"testing"
)

// session builds a take the way record.sh would have written it, one
// stream-json event at a time, without anyone having to read a real one.
type session struct {
	at    int64
	lines []Line
	board []Snapshot
}

func (s *session) add(v any) {
	s.at += 1000
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	s.lines = append(s.lines, Line{At: s.at, Raw: b})
}

func (s *session) prompt(text string) { s.add(map[string]any{"type": "demo_prompt", "text": text}) }

func (s *session) say(text string) {
	s.add(map[string]any{"type": "assistant", "message": map[string]any{
		"content": []any{map[string]any{"type": "text", "text": text}}}})
}

// run is a Bash call and its result, from the main session or, with parent
// set, from inside a sub-agent.
func (s *session) run(id, parent, cmd, out string, isErr bool) {
	s.add(map[string]any{"type": "assistant", "parent_tool_use_id": nilIfEmpty(parent), "message": map[string]any{
		"content": []any{map[string]any{"type": "tool_use", "id": id, "name": "Bash", "input": map[string]any{"command": cmd}}}}})
	s.add(map[string]any{"type": "user", "parent_tool_use_id": nilIfEmpty(parent), "message": map[string]any{
		"content": []any{map[string]any{"type": "tool_result", "tool_use_id": id, "content": out, "is_error": isErr}}}})
}

func (s *session) agent(id, task, desc, model, typ string) {
	s.add(map[string]any{"type": "assistant", "message": map[string]any{"content": []any{map[string]any{
		"type": "tool_use", "id": id, "name": "Agent",
		"input": map[string]any{"description": desc, "prompt": "Work ticket #" + task, "model": model, "subagent_type": typ}}}}})
	s.add(map[string]any{"type": "system", "subtype": "task_started", "task_type": "local_agent", "task_id": "task-" + id, "tool_use_id": id})
}

func (s *session) task(subtype, taskID string, fields map[string]any) {
	v := map[string]any{"type": "system", "subtype": subtype, "task_id": taskID}
	for k, f := range fields {
		v[k] = f
	}
	s.add(v)
}

func (s *session) snapshot(tasks ...Task) {
	s.board = append(s.board, Snapshot{At: s.at, Tasks: tasks})
}

func (s *session) take() *Take {
	return &Take{
		Meta:    Meta{Recorded: "2026-09-29T16:35:46Z", Repository: "aramponi/acme-api", Yakanban: "yakanban version 1.0.0 (7e644ed)"},
		Session: s.lines,
		Board:   s.board,
	}
}

// words joins the fields that are set.
func words(parts ...string) string {
	return strings.Join(strings.Fields(strings.Join(parts, " ")), " ")
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

const initOut = "board ready: acme-api\nproject https://github.com/users/aramponi/projects/9\ncolumns Backlog → Todo → In Progress → Review → Done"

// happyPath is one ticket from creation to Done, worked by one sub-agent
// that stops on a refused command, is resumed, and finishes.
func happyPath(final string) *session {
	s := &session{}
	s.prompt("Set up a board.")
	s.run("t1", "", "yakanban init --help; git remote -v", "Usage: ...", false)
	s.run("t2", "", "yakanban init", initOut, false)
	s.say("Your board is set up.")
	s.prompt("File tickets.")
	s.run("t3", "", "yakanban create \"Fix the typo\" --priority low --body \"line one\nline two\" --compact", "7\tTodo\tlow\tFix the typo", false)
	s.snapshot(Task{ID: "7", Title: "Fix the typo", Status: "Todo", Priority: "low"})
	s.say("Filed one ticket.")
	s.prompt("Work the board.")
	s.say("Launching one agent.")
	s.agent("ag1", "7", "Fix README typo #7", "haiku", "worker-low")
	s.run("t4", "ag1", "AGENT=$(yakanban agent-name) && yakanban move 7 \"In Progress\" --claim x", "denied", true)
	s.task("task_notification", "task-ag1", map[string]any{"status": "completed"})
	s.task("task_started", "task-ag1", map[string]any{"task_type": "local_agent", "tool_use_id": "ag1-resume"})
	s.run("t5", "ag1-resume", "yakanban move 7 \"In Progress\" --claim lucid-onyx-11", "#7 Fix the typo", false)
	s.task("task_progress", "task-ag1", map[string]any{"description": "Editing /private/var/folders/x/README.md"})
	s.task("task_progress", "task-ag1", map[string]any{"description": "Reading README.md"})
	s.run("t6", "ag1-resume", "yakanban move 7 review --claim lucid-onyx-11", "#7 Fix the typo", false)
	s.task("task_notification", "task-ag1", map[string]any{"status": "completed"})
	s.run("t7", "", "ls -a", "", true)
	s.run("t8", "", "make test 2>&1 | tail -20", "ok  \tgithub.com/aramponi/acme-api/internal/auth\t1.3s\n", false)
	s.run("t9", "", "yakanban move 7 Done", "#7 Fix the typo\nStatus Done", false)
	s.snapshot(Task{ID: "7", Title: "Fix the typo", Status: final, Priority: "low"})
	s.say("All done.\n\nDetails follow.")
	return s
}

func TestCondenseReplaysTheSession(t *testing.T) {
	tl, err := Condense(happyPath("Done").take())
	if err != nil {
		t.Fatal(err)
	}

	var got []string
	for _, e := range tl.Events {
		switch e.Type {
		case "prompt", "say":
			got = append(got, e.Type+" "+e.Text)
		case "tool":
			got = append(got, "tool "+e.Cmd+" => "+strings.Join(e.Out, " | "))
		case "board":
			got = append(got, "board "+e.Name+" "+strings.Join(e.Columns, ","))
		case "card":
			got = append(got, "card #"+e.ID+" "+e.Column+" "+e.Priority)
		case "move":
			got = append(got, words("move #"+e.ID, e.Column, e.Agent))
		case "agent":
			got = append(got, words("agent", e.ID, e.Task, e.Model, e.Effort, e.Status, e.Activity))
		}
	}
	want := []string{
		"prompt Set up a board.",
		"tool yakanban init => board ready: acme-api | project https://github.com/users/aramponi/projects/9",
		"board acme-api Backlog,Todo,In Progress,Review,Done",
		"say Your board is set up.",
		"prompt File tickets.",
		`tool yakanban create "Fix the typo" --priority low => 7  Todo  low  Fix the typo`,
		"card #7 Todo low",
		"say Filed one ticket.",
		"prompt Work the board.",
		"agent a1 7 haiku low",
		"agent a1 done",
		"agent a1 running",
		"move #7 In Progress a1",
		"agent a1 editing README.md",
		"move #7 Review a1",
		"agent a1 done",
		"tool make test => ok    internal/auth",
		"tool yakanban move 7 Done => #7 Fix the typo | Status Done",
		"move #7 Done",
		"say All done.",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("timeline:\n%s\n\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	if tl.Title != "claude — acme-api" || tl.Version != "v1.0.0" || tl.Recorded != "2026-09-29" {
		t.Errorf("header = %q %q %q", tl.Title, tl.Version, tl.Recorded)
	}
	if tl.Real != happyPath("Done").at-1000 {
		t.Errorf("real = %d", tl.Real)
	}
}

func TestCondenseRefusesAReplayTheBoardContradicts(t *testing.T) {
	_, err := Condense(happyPath("Review").take())
	if err == nil || !strings.Contains(err.Error(), "#7 replays to Done, the board says Review") {
		t.Fatalf("err = %v", err)
	}
}

func TestPaceKeepsOrderAndLeavesTimeToRead(t *testing.T) {
	tl, err := Condense(happyPath("Done").take())
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i < len(tl.Events); i++ {
		if tl.Events[i].T <= tl.Events[i-1].T {
			t.Fatalf("event %d at %d does not follow event %d at %d", i, tl.Events[i].T, i-1, tl.Events[i-1].T)
		}
	}
	first := tl.Events[0]
	if typing := int64(len(first.Text)) * TypeMS; tl.Events[1].T-first.T < typing {
		t.Errorf("the prompt gets %dms, typing it takes %dms", tl.Events[1].T-first.T, typing)
	}
	if last := tl.Events[len(tl.Events)-1].T; tl.Duration <= last {
		t.Errorf("duration %d leaves no hold after the last event at %d", tl.Duration, last)
	}
}

func TestScrubRefusesWhatATakeMustNotPublish(t *testing.T) {
	for _, leak := range []string{
		"/Users/antoine/devel/x",
		"cwd /private/var/folders/0q/T/tmp.x",
		"wrote /tmp/cfg/go.mod",
		"toolu_01KwbyZZbw2FMKpykXJnwNs7",
		"Co-Authored-By: someone@example.com",
	} {
		tl := &Timeline{Events: []Event{{Type: "tool", Out: []string{leak}}}}
		if err := Scrub(tl); err == nil {
			t.Errorf("Scrub let %q through", leak)
		}
	}
	ok := &Timeline{Title: "claude — acme-api", Events: []Event{{Type: "tool", Cmd: "git push origin main", Out: []string{"To https://github.com/aramponi/acme-api.git"}}}}
	if err := Scrub(ok); err != nil {
		t.Errorf("Scrub refused a clean timeline: %v", err)
	}
}

func TestCleanCommand(t *testing.T) {
	for in, want := range map[string]string{
		"yakanban board --compact --refresh":                    "yakanban board",
		"make test 2>&1 | tail -60":                             "make test",
		`yakanban create "A title" --status Todo --body "x\ny"`: `yakanban create "A title"`,
		"yakanban edit 3 -a \"note\" --claim x":                 "yakanban edit 3 --claim x",
		"git merge --no-edit a b && git push":                   "git merge --no-edit a b",
	} {
		if got := cleanCommand(in); got != want {
			t.Errorf("cleanCommand(%q) = %q, want %q", in, got, want)
		}
	}
}
