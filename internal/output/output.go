// Package output renders domain values for the terminal.
//
// Every command supports the same three shapes: a human default, --compact
// (one token-cheap line per record, for agents and pipes) and --json.
package output

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/aramponi/yakanban/internal/core"
)

// Format selects the rendering shape.
type Format string

// Supported formats.
const (
	FormatHuman   Format = "human"
	FormatTable   Format = "table"
	FormatCompact Format = "compact"
	FormatJSON    Format = "json"
)

// Printer renders values to a writer in the selected format.
type Printer struct {
	Out    io.Writer
	Err    io.Writer
	Format Format
	Color  bool
	Now    time.Time
}

// New builds a printer, disabling colour when the output is not a terminal.
func New(out, errw io.Writer, format Format, color bool) *Printer {
	return &Printer{Out: out, Err: errw, Format: format, Color: color && isTerminal(out), Now: time.Now()}
}

func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	st, err := f.Stat()
	if err != nil {
		return false
	}
	return st.Mode()&os.ModeCharDevice != 0
}

// ANSI colour helpers. They collapse to identity when colour is off.
const (
	ansiReset  = "\033[0m"
	ansiDim    = "\033[2m"
	ansiBold   = "\033[1m"
	ansiRed    = "\033[31m"
	ansiGreen  = "\033[32m"
	ansiYellow = "\033[33m"
	ansiBlue   = "\033[34m"
	ansiCyan   = "\033[36m"
)

func (p *Printer) paint(code, s string) string {
	if !p.Color || s == "" {
		return s
	}
	return code + s + ansiReset
}

// Dim renders secondary text.
func (p *Printer) Dim(s string) string { return p.paint(ansiDim, s) }

// Bold renders emphasised text.
func (p *Printer) Bold(s string) string { return p.paint(ansiBold, s) }

// JSON writes v as indented JSON.
func (p *Printer) JSON(v any) error {
	enc := json.NewEncoder(p.Out)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// Printf writes a formatted line to stdout.
func (p *Printer) Printf(format string, args ...any) {
	fmt.Fprintf(p.Out, format, args...)
}

// Warnf writes a formatted warning to stderr, so it never pollutes piped output.
func (p *Printer) Warnf(format string, args ...any) {
	fmt.Fprintf(p.Err, p.paint(ansiYellow, "warning: ")+format+"\n", args...)
}

func (p *Printer) priorityColor(priority string) string {
	switch strings.ToLower(priority) {
	case "critical":
		return ansiRed
	case "high":
		return ansiYellow
	case "medium":
		return ansiBlue
	default:
		return ansiDim
	}
}

// Tasks renders a task list.
func (p *Printer) Tasks(tasks []core.Task) error {
	switch p.Format {
	case FormatJSON:
		if tasks == nil {
			tasks = []core.Task{}
		}
		return p.JSON(tasks)
	case FormatCompact:
		for _, t := range tasks {
			fmt.Fprintln(p.Out, p.compactLine(t))
		}
		return nil
	default:
		return p.taskTable(tasks)
	}
}

// compactLine is the one-line form: cheap to read for an agent, still legible.
func (p *Printer) compactLine(t core.Task) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\t%s\t%s\t%s", t.ID, dash(t.Status), dash(t.Priority), t.Title)
	var extra []string
	if len(t.Tags) > 0 {
		extra = append(extra, "#"+strings.Join(t.Tags, ",#"))
	}
	if len(t.Assignees) > 0 {
		extra = append(extra, "@"+strings.Join(t.Assignees, ",@"))
	}
	if t.Claim.Active(p.Now) {
		extra = append(extra, "claim:"+t.Claim.Agent)
	}
	if t.IsBlocked() {
		extra = append(extra, "blocked:"+oneLine(t.Blocked))
	}
	if t.Due != nil {
		extra = append(extra, "due:"+t.Due.Format("2006-01-02"))
	}
	if len(t.DependsOn) > 0 {
		extra = append(extra, "deps:"+strings.Join(t.DependsOn, ","))
	}
	if len(extra) > 0 {
		b.WriteString("\t" + strings.Join(extra, " "))
	}
	return b.String()
}

func (p *Printer) taskTable(tasks []core.Task) error {
	if len(tasks) == 0 {
		fmt.Fprintln(p.Out, p.Dim("no tasks"))
		return nil
	}
	tw := tabwriter.NewWriter(p.Out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, p.Dim("ID\tSTATUS\tPRIORITY\tTITLE\tTAGS\tASSIGNEES\tFLAGS"))
	for _, t := range tasks {
		flags := p.flags(t)
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			t.ID,
			dash(t.Status),
			p.paint(p.priorityColor(t.Priority), dash(t.Priority)),
			truncate(t.Title, 60),
			strings.Join(t.Tags, ","),
			strings.Join(t.Assignees, ","),
			flags,
		)
	}
	return tw.Flush()
}

func (p *Printer) flags(t core.Task) string {
	var out []string
	if t.IsBlocked() {
		out = append(out, p.paint(ansiRed, "blocked"))
	}
	if t.Claim.Active(p.Now) {
		out = append(out, p.paint(ansiCyan, "claimed:"+t.Claim.Agent))
	}
	if t.Due != nil {
		label := "due " + t.Due.Format("2006-01-02")
		if t.Due.Before(p.Now) {
			label = p.paint(ansiRed, label)
		}
		out = append(out, label)
	}
	return strings.Join(out, " ")
}

// Task renders a single task in full.
func (p *Printer) Task(t core.Task) error {
	switch p.Format {
	case FormatJSON:
		return p.JSON(t)
	case FormatCompact:
		fmt.Fprintln(p.Out, p.compactLine(t))
		return nil
	}
	tw := tabwriter.NewWriter(p.Out, 0, 4, 2, ' ', 0)
	row := func(k, v string) {
		if strings.TrimSpace(v) == "" {
			return
		}
		fmt.Fprintf(tw, "%s\t%s\n", p.Dim(k), v)
	}
	fmt.Fprintf(p.Out, "%s %s\n\n", p.Bold("#"+t.ID), p.Bold(t.Title))
	row("Status", t.Status)
	row("Priority", p.paint(p.priorityColor(t.Priority), t.Priority))
	row("Class", t.Class)
	row("Tags", strings.Join(t.Tags, ", "))
	row("Assignees", strings.Join(t.Assignees, ", "))
	row("Estimate", t.Estimate)
	row("Parent", t.Parent)
	row("Depends on", strings.Join(t.DependsOn, ", "))
	if t.IsBlocked() {
		row("Blocked", p.paint(ansiRed, t.Blocked))
	}
	if t.Claim != nil {
		state := "expired"
		if t.Claim.Active(p.Now) {
			state = "until " + t.Claim.Expires.Local().Format("2006-01-02 15:04")
		}
		row("Claim", t.Claim.Agent+" ("+state+")")
	}
	row("Due", formatDate(t.Due))
	row("Started", formatDate(t.Started))
	row("Completed", formatDate(t.Completed))
	row("Created", t.Created.Local().Format("2006-01-02 15:04"))
	row("Updated", t.Updated.Local().Format("2006-01-02 15:04"))
	row("URL", t.URL)
	if err := tw.Flush(); err != nil {
		return err
	}
	if strings.TrimSpace(t.Body) != "" {
		fmt.Fprintf(p.Out, "\n%s\n%s\n", p.Dim("─── body ───"), strings.TrimRight(t.Body, "\n"))
	}
	return nil
}

// Summary renders the board overview.
func (p *Printer) Summary(s core.Summary) error {
	switch p.Format {
	case FormatJSON:
		return p.JSON(s)
	case FormatCompact:
		parts := make([]string, 0, len(s.Columns))
		for _, c := range s.Columns {
			parts = append(parts, fmt.Sprintf("%s:%d", c.Status.Name, c.Count))
		}
		fmt.Fprintf(p.Out, "%s\t%s\ttotal:%d\tblocked:%d\toverdue:%d\n",
			s.Board.Name, strings.Join(parts, " "), s.Total, s.Blocked, s.Overdue)
		return nil
	}
	fmt.Fprintf(p.Out, "%s %s\n", p.Bold(s.Board.Name), p.Dim("("+s.Board.Provider+")"))
	if s.Board.URL != "" {
		fmt.Fprintf(p.Out, "%s\n", p.Dim(s.Board.URL))
	}
	fmt.Fprintln(p.Out)
	tw := tabwriter.NewWriter(p.Out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, p.Dim("COLUMN\tTASKS\tWIP\tBLOCKED\tCLAIMED"))
	for _, c := range s.Columns {
		wip := p.Dim("—")
		if c.Status.WIPLimit > 0 {
			wip = fmt.Sprintf("%d/%d", c.Count, c.Status.WIPLimit)
			if c.OverWIP {
				wip = p.paint(ansiRed, wip+" over")
			}
		}
		fmt.Fprintf(tw, "%s\t%d\t%s\t%s\t%s\n", c.Status.Name, c.Count, wip,
			zeroDash(c.Blocked), zeroDash(c.Claimed))
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	fmt.Fprintf(p.Out, "\n%s %d   %s %d   %s %d\n",
		p.Dim("total"), s.Total,
		p.Dim("blocked"), s.Blocked,
		p.Dim("overdue"), s.Overdue)
	if len(s.Priorities) > 0 {
		var parts []string
		for _, name := range s.Board.Priorities {
			if n := s.Priorities[name]; n > 0 {
				parts = append(parts, fmt.Sprintf("%s %d", name, n))
			}
		}
		if len(parts) > 0 {
			fmt.Fprintf(p.Out, "%s %s\n", p.Dim("priorities"), strings.Join(parts, "  "))
		}
	}
	return nil
}

func formatDate(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format("2006-01-02")
}

func dash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}

func zeroDash(n int) string {
	if n == 0 {
		return "—"
	}
	return fmt.Sprint(n)
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func truncate(s string, n int) string {
	if len([]rune(s)) <= n {
		return s
	}
	return string([]rune(s)[:n-1]) + "…"
}
