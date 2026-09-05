package site

import (
	"fmt"
	"os/exec"
	"regexp"
	"slices"
	"strings"
)

// Every terminal block on the page is captured by running the binary, against
// the board this project actually runs on. Nothing here is typed by hand, so
// there is no such thing as a sample that used to be true.

// readOnly lists the commands a capture may run. Generating a web page must
// never move a ticket, and an allowlist says so in a way a reviewer can check
// at a glance.
var readOnly = []string{"board", "list", "show", "config", "agent-name", "help"}

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// Sample is one command whose real output appears on the page.
type Sample struct {
	Name string
	Args []string
	// NeedsBoard marks the captures that talk to the tracker, and so need a
	// token with the project scope.
	NeedsBoard bool
	// Env replaces the whole environment when set, which is how the
	// authentication capture is run without a token and without `gh` on the
	// PATH.
	Env []string
	// WantExit is the exit code the capture expects. Some of what the page
	// has to show is what the tool says when it cannot proceed, and that is
	// only worth showing if it is the real message.
	WantExit int
}

// Samples are the captures the page asks for, by name.
var Samples = []Sample{
	{Name: "board", Args: []string{"board", "--refresh"}, NeedsBoard: true},
	{Name: "ready", Args: []string{"list", "--unclaimed", "--not-blocked", "--status", "todo"}, NeedsBoard: true},
	// No status filter: this capture shows the --compact format, not todo
	// work, and `list --compact` prints nothing when the filter matches no
	// rows. A status the backlog can legitimately empty out would make the
	// site build depend on transient board state.
	{Name: "compact", Args: []string{"list", "--compact", "-n", "8"}, NeedsBoard: true},
	{
		Name: "auth",
		Args: []string{"board", "--refresh"},
		// Deliberately no token and no `gh`: this capture is the error a
		// first run produces, which is the only honest way to show that the
		// message tells you what to do next.
		Env:      []string{"PATH=/usr/bin:/bin", "NO_COLOR=1"},
		WantExit: 4,
	},
}

// Transcript is a captured command and what it printed.
type Transcript struct {
	Command string
	Output  string
}

// Console renders the transcript as a Markdown console block, which is what
// the templates and llms.txt both want.
func (t Transcript) Console() string {
	return "```console\n$ " + t.Command + "\n" + t.Output + "\n```"
}

// Capture runs one sample in dir and returns what a reader would have seen.
func Capture(bin, dir string, s Sample) (Transcript, error) {
	if len(s.Args) == 0 || !slices.Contains(readOnly, s.Args[0]) &&
		!strings.HasPrefix(s.Args[0], "-") {
		return Transcript{}, fmt.Errorf("sample %q runs %q, which is not a read-only command", s.Name, s.Args[0])
	}

	args := s.Args
	if !strings.HasPrefix(args[0], "-") {
		args = append(slices.Clone(args), "--no-color")
	}

	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	cmd.Env = append(cmd.Environ(), "NO_COLOR=1")
	if s.Env != nil {
		cmd.Env = s.Env
	}

	out, err := cmd.CombinedOutput()
	text := trimRightPerLine(ansiRe.ReplaceAllString(string(out), ""))

	code := cmd.ProcessState.ExitCode()
	if err != nil && code < 0 {
		return Transcript{}, fmt.Errorf("sample %q: %s did not run: %w\n%s", s.Name, strings.Join(args, " "), err, text)
	}
	if code != s.WantExit {
		return Transcript{}, fmt.Errorf("sample %q: %s exited %d, want %d\n%s", s.Name, strings.Join(args, " "), code, s.WantExit, text)
	}
	if strings.TrimSpace(text) == "" {
		return Transcript{}, fmt.Errorf("sample %q printed nothing", s.Name)
	}

	// When the point of the capture is the failure, the exit code is half of
	// what it has to show: a script branches on it.
	if s.WantExit != 0 {
		text += fmt.Sprintf("\n$ echo $?\n%d", code)
	}

	return Transcript{Command: "yakanban " + strings.Join(s.Args, " "), Output: text}, nil
}

// CaptureAll runs every sample, keyed by name.
func CaptureAll(bin, dir string) (map[string]Transcript, error) {
	out := make(map[string]Transcript, len(Samples))
	for _, s := range Samples {
		t, err := Capture(bin, dir, s)
		if err != nil {
			return nil, err
		}
		out[s.Name] = t
	}
	return out, nil
}

// trimRightPerLine drops the padding a table renderer leaves at the end of a
// row, which is invisible in a terminal and selectable on a web page.
func trimRightPerLine(s string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t\r")
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n")
}
