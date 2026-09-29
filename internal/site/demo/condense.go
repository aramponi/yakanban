package demo

import (
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Timeline is what the player replays; demo.js documents the format.
type Timeline struct {
	Recorded string  `json:"recorded"`
	Version  string  `json:"version"`
	Real     int64   `json:"real"` // ms the session actually took
	Duration int64   `json:"duration"`
	Title    string  `json:"title"`
	Events   []Event `json:"events"`
}

// Event is one thing the clip shows. Which fields are set depends on Type.
type Event struct {
	T        int64    `json:"t"`
	Type     string   `json:"type"`
	Text     string   `json:"text,omitempty"`
	Cmd      string   `json:"cmd,omitempty"`
	Out      []string `json:"out,omitempty"`
	Name     string   `json:"name,omitempty"`
	Columns  []string `json:"columns,omitempty"`
	ID       string   `json:"id,omitempty"`
	Title    string   `json:"title,omitempty"`
	Column   string   `json:"column,omitempty"`
	Priority string   `json:"priority,omitempty"`
	Agent    string   `json:"agent,omitempty"`
	Task     string   `json:"task,omitempty"`
	Label    string   `json:"label,omitempty"`
	Model    string   `json:"model,omitempty"`
	Effort   string   `json:"effort,omitempty"`
	Status   string   `json:"status,omitempty"`
	Activity string   `json:"activity,omitempty"`

	real int64 // ms into the session, before compression
}

// ---------- pacing ----------
//
// The clip keeps the order of everything it shows and nothing else of the
// session's timing: a sub-agent thinking for a minute is not worth a minute
// of anybody's attention. Each event instead gets the time it takes to read,
// plus a little of the real gap after it, so that a long wait still feels
// longer than a short one.

// TypeMS is how fast the player types a prompt; keep it in step with
// TYPE_MS in demo.js.
const TypeMS = 32

var readMS = map[string]int64{
	"prompt":   800, // after typing it
	"tool":     650,
	"say":      2200,
	"board":    800,
	"card":     450,
	"agent":    700,
	"activity": 380,
	"status":   450,
	"move":     650,
	"archive":  550,
}

const (
	outputMS   = 350 // extra time to read a command's output
	gapShare   = 60  // one clip ms per this many real ms of waiting…
	gapCeiling = 900 // …up to this much
	firstMS    = 300
	holdMS     = 1500
)

func pace(ev []Event) int64 {
	clip := int64(firstMS)
	for i := range ev {
		ev[i].T = clip
		if i == len(ev)-1 {
			break
		}
		kind := ev[i].Type
		switch {
		case kind == "agent" && ev[i].Activity != "":
			kind = "activity"
		case kind == "agent" && ev[i].Status != "":
			kind = "status"
		}
		read := readMS[kind]
		if kind == "prompt" {
			read += int64(utf8.RuneCountInString(ev[i].Text)) * TypeMS
		}
		if len(ev[i].Out) > 0 {
			read += outputMS
		}
		clip += read + min((ev[i+1].real-ev[i].real)/gapShare, gapCeiling)
	}
	return clip + holdMS
}

// ---------- the stream ----------

type streamEvent struct {
	Type            string `json:"type"`
	Subtype         string `json:"subtype"`
	Text            string `json:"text"`
	ParentToolUseID string `json:"parent_tool_use_id"`
	// An object for assistant and user turns, a plain string on some system
	// events: decoded only where it is an object.
	Message      json.RawMessage `json:"message"`
	TaskID       string          `json:"task_id"`
	ToolUseID    string          `json:"tool_use_id"`
	TaskType     string          `json:"task_type"`
	SubagentType string          `json:"subagent_type"`
	Description  string          `json:"description"`
	Status       string          `json:"status"`
}

type block struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
	IsError   bool            `json:"is_error"`
}

func blocks(message json.RawMessage) []block {
	var m struct {
		Content json.RawMessage `json:"content"`
	}
	if json.Unmarshal(message, &m) != nil {
		return nil
	}
	raw := m.Content
	var bs []block
	if json.Unmarshal(raw, &bs) != nil {
		return nil // a plain string: nothing the clip shows
	}
	return bs
}

func resultText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var parts []struct {
		Text string `json:"text"`
	}
	_ = json.Unmarshal(raw, &parts)
	var b strings.Builder
	for _, p := range parts {
		b.WriteString(p.Text)
	}
	return b.String()
}

// ---------- condensing ----------

type pendingTool struct {
	at    int64 // when it was run: its effects are shown right after it
	cmd   string
	agent string // the sub-agent that ran it, or "" for the main session
	shown int    // index of its event, or -1
}

type agentState struct {
	task     string
	done     bool
	activity string // the last one shown, so a repeat is not shown twice
}

type condenser struct {
	take   *Take
	t0     int64
	events []Event

	boardShown bool
	columns    []string
	cards      map[string]string // id → column

	tools   map[string]*pendingTool
	agentOf map[string]string // Agent tool-use id or task id → agent key
	agents  map[string]*agentState
	nAgents int

	prompt   int
	lastText map[int]Event
}

// Condense builds the timeline from a take.
func Condense(t *Take) (*Timeline, error) {
	c := &condenser{
		take:     t,
		t0:       t.Session[0].At,
		cards:    map[string]string{},
		tools:    map[string]*pendingTool{},
		agentOf:  map[string]string{},
		agents:   map[string]*agentState{},
		prompt:   -1,
		lastText: map[int]Event{},
	}
	for _, l := range t.Session {
		var e streamEvent
		if err := json.Unmarshal(l.Raw, &e); err != nil {
			return nil, err
		}
		if err := c.handle(l.At-c.t0, e); err != nil {
			return nil, err
		}
	}
	for _, e := range c.lastText {
		c.events = append(c.events, e)
	}
	if !c.boardShown {
		return nil, fmt.Errorf("the session never ran yakanban init")
	}
	if err := c.checkFinalBoard(); err != nil {
		return nil, err
	}

	ev := make([]Event, 0, len(c.events))
	for _, e := range c.events {
		if e.Type != "" {
			ev = append(ev, e)
		}
	}
	sort.SliceStable(ev, func(i, j int) bool { return ev[i].real < ev[j].real })

	tl := &Timeline{
		Recorded: strings.SplitN(t.Meta.Recorded, "T", 2)[0],
		Version:  version(t.Meta.Yakanban),
		Real:     t.Session[len(t.Session)-1].At - c.t0,
		Title:    "claude — " + path.Base(t.Meta.Repository),
		Events:   ev,
	}
	tl.Duration = pace(tl.Events)
	if err := Scrub(tl); err != nil {
		return nil, err
	}
	return tl, nil
}

func version(s string) string {
	// "yakanban version 1.0.0 (7e644ed)"
	f := strings.Fields(s)
	if len(f) >= 3 {
		return "v" + strings.TrimPrefix(f[2], "v")
	}
	return ""
}

func (c *condenser) emit(at int64, e Event) int {
	e.real = at
	c.events = append(c.events, e)
	return len(c.events) - 1
}

func (c *condenser) handle(at int64, e streamEvent) error {
	sub := e.ParentToolUseID != ""
	switch e.Type {
	case "demo_prompt":
		c.prompt++
		c.emit(at, Event{Type: "prompt", Text: e.Text})

	case "assistant":
		for _, b := range blocks(e.Message) {
			switch {
			case b.Type == "text" && !sub && strings.TrimSpace(b.Text) != "":
				c.lastText[c.prompt] = Event{Type: "say", Text: firstSentence(b.Text), real: at}
			case b.Type == "tool_use" && b.Name == "Bash":
				var in struct {
					Command string `json:"command"`
				}
				_ = json.Unmarshal(b.Input, &in)
				p := &pendingTool{at: at, cmd: strings.TrimSpace(in.Command), shown: -1}
				if sub {
					p.agent = c.agentOf[e.ParentToolUseID]
				} else if showCommand(p.cmd) {
					p.shown = c.emit(at, Event{Type: "tool", Cmd: cleanCommand(p.cmd)})
				}
				c.tools[b.ID] = p
			case b.Type == "tool_use" && b.Name == "Agent" && !sub:
				c.startAgent(at, b)
			}
		}

	case "user":
		for _, b := range blocks(e.Message) {
			p := c.tools[b.ToolUseID]
			if b.Type != "tool_result" || p == nil {
				continue
			}
			delete(c.tools, b.ToolUseID)
			if b.IsError {
				if p.shown >= 0 {
					c.events[p.shown].Type = "" // a refused command is not the story
				}
				continue
			}
			out := resultText(b.Content)
			if p.shown >= 0 {
				c.events[p.shown].Out = outputLines(p.cmd, out)
			}
			if err := c.applyYakanban(p.at, p, out); err != nil {
				return err
			}
		}

	case "system":
		c.handleTask(at, e)
	}
	return nil
}

func (c *condenser) startAgent(at int64, b block) {
	var in struct {
		Description  string `json:"description"`
		Prompt       string `json:"prompt"`
		Model        string `json:"model"`
		SubagentType string `json:"subagent_type"`
	}
	_ = json.Unmarshal(b.Input, &in)
	c.nAgents++
	key := fmt.Sprintf("a%d", c.nAgents)
	c.agentOf[b.ID] = key

	task := ""
	if m := ticketRe.FindStringSubmatch(in.Description + " " + in.Prompt); m != nil {
		task = m[1]
	}
	c.agents[key] = &agentState{task: task}
	model := in.Model
	if model == "" {
		model = "inherit"
	}
	effort := ""
	if i := strings.LastIndex(in.SubagentType, "-"); i >= 0 {
		switch s := in.SubagentType[i+1:]; s {
		case "low", "medium", "high", "xhigh", "max":
			effort = s
		}
	}
	c.emit(at, Event{
		Type: "agent", ID: key, Task: task,
		Label: strings.Join(strings.Fields(ticketRe.ReplaceAllString(in.Description, " ")), " "),
		Model: model, Effort: effort,
	})
}

// handleTask follows the sub-agents through the session's task events: when
// one starts (or is resumed after stopping), what it is doing, when it ends.
func (c *condenser) handleTask(at int64, e streamEvent) {
	switch e.Subtype {
	case "task_started":
		if e.TaskType != "local_agent" {
			return
		}
		if key, resumed := c.agentOf[e.TaskID]; resumed {
			c.agentOf[e.ToolUseID] = key
			if a := c.agents[key]; a.done {
				a.done = false
				c.emit(at, Event{Type: "agent", ID: key, Status: "running"})
			}
			return
		}
		if key, ok := c.agentOf[e.ToolUseID]; ok {
			c.agentOf[e.TaskID] = key
		}
	case "task_progress":
		key, ok := c.agentOf[e.TaskID]
		if !ok {
			return
		}
		if act := activity(e.Description); act != "" && act != c.agents[key].activity {
			c.agents[key].activity = act
			c.emit(at, Event{Type: "agent", ID: key, Activity: act})
		}
	case "task_notification":
		key, ok := c.agentOf[e.TaskID]
		if !ok || e.Status != "completed" {
			return
		}
		c.agents[key].done = true
		c.agents[key].activity = ""
		c.emit(at, Event{Type: "agent", ID: key, Status: "done"})
	}
}

// ---------- what yakanban did ----------

var (
	ticketRe  = regexp.MustCompile(`#(\d+)`)
	readyRe   = regexp.MustCompile(`(?m)^board ready:\s*(.+)$`)
	columnsRe = regexp.MustCompile(`(?m)^columns\s+(.+)$`)
	createdRe = regexp.MustCompile(`^#?(\d+)\b`)
)

// applyYakanban replays the board writes a successful command made. Taking
// them from the commands, not from the sampled board, gives each one its
// exact moment; the samples are then used to check the replay ended where
// the board did.
func (c *condenser) applyYakanban(at int64, p *pendingTool, out string) error {
	args := shellWords(firstCommand(p.cmd))
	if len(args) < 2 || args[0] != "yakanban" || hasFlag(args, "--help") || hasFlag(args, "-h") {
		return nil
	}
	switch args[1] {
	case "init":
		name := "board"
		if m := readyRe.FindStringSubmatch(out); m != nil {
			name = strings.TrimSpace(m[1])
		}
		m := columnsRe.FindStringSubmatch(out)
		if m == nil {
			return fmt.Errorf("yakanban init printed no columns: %q", out)
		}
		c.columns = nil
		for _, col := range strings.Split(m[1], "→") {
			c.columns = append(c.columns, strings.TrimSpace(col))
		}
		c.boardShown = true
		c.emit(at, Event{Type: "board", Name: name, Columns: c.columns})

	case "create":
		m := createdRe.FindStringSubmatch(strings.TrimSpace(out))
		if m == nil {
			return fmt.Errorf("cannot read the new ticket's id from %q", out)
		}
		task, ok := c.firstSeen(m[1])
		if !ok {
			return fmt.Errorf("ticket #%s was created but never appears on the sampled board", m[1])
		}
		c.cards[task.ID] = task.Status
		c.emit(at, Event{Type: "card", ID: task.ID, Title: task.Title, Column: task.Status, Priority: task.Priority})

	case "move":
		pos := positional(args[2:])
		if len(pos) == 0 {
			return nil
		}
		id := strings.TrimPrefix(pos[0], "#")
		from, ok := c.cards[id]
		if !ok {
			return nil // a ticket the clip never showed
		}
		to := ""
		switch {
		case hasFlag(args, "--next"):
			to = c.step(from, 1)
		case hasFlag(args, "--prev"):
			to = c.step(from, -1)
		case len(pos) > 1:
			to = c.column(pos[1])
		}
		if to == "" {
			return fmt.Errorf("cannot tell where %q moved ticket #%s", p.cmd, id)
		}
		if to == from {
			return nil
		}
		c.cards[id] = to
		mv := Event{Type: "move", ID: id, Column: to}
		if p.agent != "" && hasFlag(args, "--claim") {
			mv.Agent = p.agent
		}
		c.emit(at, mv)

	case "delete":
		// Closes and archives: the ticket leaves the board.
		for _, a := range positional(args[2:]) {
			id := strings.TrimPrefix(a, "#")
			if _, ok := c.cards[id]; ok {
				delete(c.cards, id)
				c.emit(at, Event{Type: "archive", ID: id})
			}
		}
	}
	return nil
}

func (c *condenser) firstSeen(id string) (Task, bool) {
	for _, s := range c.take.Board {
		for _, t := range s.Tasks {
			if t.ID == id {
				return t, true
			}
		}
	}
	return Task{}, false
}

func norm(s string) string {
	return strings.NewReplacer(" ", "", "-", "", "_", "").Replace(strings.ToLower(s))
}

func (c *condenser) column(name string) string {
	for _, col := range c.columns {
		if norm(col) == norm(name) {
			return col
		}
	}
	return ""
}

func (c *condenser) step(from string, d int) string {
	for i, col := range c.columns {
		if col == from && i+d >= 0 && i+d < len(c.columns) {
			return c.columns[i+d]
		}
	}
	return ""
}

func (c *condenser) checkFinalBoard() error {
	last := c.take.Board[len(c.take.Board)-1]
	var bad []string
	onBoard := map[string]bool{}
	for _, t := range last.Tasks {
		onBoard[t.ID] = true
		if got, ok := c.cards[t.ID]; ok && got != t.Status {
			bad = append(bad, fmt.Sprintf("#%s replays to %s, the board says %s", t.ID, got, t.Status))
		}
	}
	for id, col := range c.cards {
		if !onBoard[id] {
			bad = append(bad, fmt.Sprintf("#%s replays to %s, but it is no longer on the board", id, col))
		}
	}
	sort.Strings(bad)
	if len(bad) > 0 {
		return fmt.Errorf("the replay does not end where the board did: %s", strings.Join(bad, "; "))
	}
	return nil
}

// ---------- what the terminal shows ----------

var shownRe = regexp.MustCompile(`^(yakanban (init|create|move|delete|board|pick)\b|make test\b|go test\b|git (merge|push)\b)`)

func showCommand(cmd string) bool {
	return shownRe.MatchString(cmd) && !strings.Contains(cmd, "--help") && !compound(cmd)
}

// compound reports a command line that runs more than one command: its
// output is theirs together, which the clip cannot attribute to one command.
// A pipe is fine; it filters one command's output.
func compound(cmd string) bool {
	var quote rune
	prev := rune(0)
	for _, r := range cmd {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			}
		case r == '"' || r == '\'':
			quote = r
		case r == ';' || r == '\n' || (r == '&' && prev == '&'):
			return true
		}
		prev = r
	}
	return false
}

// firstCommand keeps the first command of a line: what comes after a pipe or
// a separator is housekeeping, not something the clip needs to show.
func firstCommand(cmd string) string {
	for _, cut := range []string{" | ", " 2>&1", " && ", "; "} {
		if i := strings.Index(cmd, cut); i >= 0 {
			cmd = cmd[:i]
		}
	}
	return strings.TrimSpace(cmd)
}

// hiddenFlags are dropped from what the terminal shows: output switches, and
// ticket bodies, which run to paragraphs.
var hiddenFlags = map[string]bool{
	"--compact": false, "--refresh": false, "--no-cache": false, "-q": false,
	"--body": true, "-a": true, "--append-body": true, "--status": true,
}

// cleanCommand is the command as the clip shows it.
func cleanCommand(cmd string) string {
	words := shellWords(firstCommand(cmd))
	var out []string
	for i := 0; i < len(words); i++ {
		w := words[i]
		if takesValue, hidden := hiddenFlags[w]; hidden {
			if takesValue {
				i++
			}
			continue
		}
		if w == "" || strings.ContainsAny(w, " \"'") {
			w = strconv.Quote(w)
		}
		out = append(out, w)
	}
	return strings.Join(out, " ")
}

var (
	testLineRe = regexp.MustCompile(`^(ok|FAIL|--- FAIL|WARNING: DATA RACE)`)
	modPathRe  = regexp.MustCompile(`\S+?/((internal|cmd|pkg)/\S*)`)
	durationRe = regexp.MustCompile(`\s+(\(cached\)|[\d.]+s)$`)
)

func outputLines(cmd, out string) []string {
	var lines []string
	test := strings.HasPrefix(cmd, "make test") || strings.HasPrefix(cmd, "go test")
	limit := 2
	if test {
		limit = 4
	}
	for _, l := range strings.Split(out, "\n") {
		l = strings.TrimRight(l, " \r")
		if strings.TrimSpace(l) == "" || strings.Contains(l, "completed with no output") {
			continue
		}
		if test {
			if !testLineRe.MatchString(l) {
				continue
			}
			l = durationRe.ReplaceAllString(modPathRe.ReplaceAllString(l, "$1"), "")
		}
		lines = append(lines, truncate(strings.ReplaceAll(l, "\t", "  "), 90))
		if len(lines) == limit {
			break
		}
	}
	return lines
}

var activityRe = regexp.MustCompile(`^(Editing|Writing) (\S+)`)

// activity keeps the part of a sub-agent's progress a reader can follow: the
// file it is changing, and when it runs the tests.
func activity(desc string) string {
	if m := activityRe.FindStringSubmatch(desc); m != nil {
		f := m[2]
		if strings.HasPrefix(f, "/") {
			f = path.Base(f)
		}
		return strings.ToLower(m[1]) + " " + f
	}
	if strings.HasPrefix(desc, "Running make test") || strings.HasPrefix(desc, "Running go test") ||
		strings.HasPrefix(desc, "Running Run make test") {
		return "running tests"
	}
	return ""
}

func firstSentence(s string) string {
	s = strings.NewReplacer("`", "", "**", "").Replace(strings.TrimSpace(s))
	s = strings.SplitN(s, "\n", 2)[0]
	for i := 0; i+1 < len(s); i++ {
		if (s[i] == '.' || s[i] == ':') && s[i+1] == ' ' {
			s = s[:i+1]
			break
		}
	}
	if strings.HasSuffix(s, ":") {
		s = strings.TrimSuffix(s, ":") + "."
	}
	return truncate(s, 110)
}

func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}

// ---------- arguments ----------

// shellWords splits a command line the way a shell would for the simple
// commands the clip shows: words, single and double quotes.
func shellWords(s string) []string {
	var (
		words []string
		cur   strings.Builder
		quote rune
		in    bool
	)
	for _, r := range s {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '"' || r == '\'':
			quote, in = r, true
		case r == ' ' || r == '\t' || r == '\n':
			if in {
				words = append(words, cur.String())
				cur.Reset()
				in = false
			}
		default:
			cur.WriteRune(r)
			in = true
		}
	}
	if in {
		words = append(words, cur.String())
	}
	return words
}

// valueFlags are the yakanban flags that take a value, so that the value is
// not mistaken for a positional argument.
var valueFlags = map[string]bool{
	"--claim": true, "--status": true, "--priority": true, "--tags": true, "--note": true,
	"--block": true, "--title": true, "--body": true, "-a": true, "--append-body": true,
	"--config": true, "--dir": true,
}

func positional(args []string) []string {
	var out []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case valueFlags[a]:
			i++
		case strings.HasPrefix(a, "-"):
		default:
			out = append(out, a)
		}
	}
	return out
}

func hasFlag(args []string, flag string) bool {
	for _, a := range args {
		if a == flag || strings.HasPrefix(a, flag+"=") {
			return true
		}
	}
	return false
}
