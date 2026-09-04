package site

import (
	"fmt"
	"regexp"
	"strings"
)

// The landing page states no fact of its own. Install commands, the command
// list and the exit codes are lifted out of README.md, which is marked up with
// invisible HTML comments naming what the page takes:
//
//	<!-- site:install name="Homebrew" -->
//	<!-- site:table id="exit-codes" -->
//	<!-- site:blurb -->
//
// The extraction is assertive: a marker that disappears, or one whose block is
// no longer where it was, fails generation instead of quietly publishing a
// page with a hole in it. That is the mechanism that keeps the two in step —
// nobody has to remember anything.

var (
	markerRe   = regexp.MustCompile(`^<!--\s*site:(\w+)([^>]*)-->\s*$`)
	attrRe     = regexp.MustCompile(`(\w+)="([^"]*)"`)
	tableSepRe = regexp.MustCompile(`^\|[\s:|-]+\|$`)
)

// Install is one way of getting the binary, as the README documents it.
type Install struct {
	Name    string
	Command string
}

// Table is a Markdown table lifted whole out of the README.
type Table struct {
	Header []string
	Rows   [][]string
}

// Readme is everything the page borrows from README.md.
type Readme struct {
	Blurb    string
	Installs []Install
	Tables   map[string]Table
}

// ParseReadme reads the marked-up regions of README.md.
func ParseReadme(md string) (*Readme, error) {
	r := &Readme{Tables: map[string]Table{}}
	lines := strings.Split(md, "\n")

	for i := 0; i < len(lines); i++ {
		m := markerRe.FindStringSubmatch(lines[i])
		if m == nil {
			continue
		}
		kind, attrs := m[1], parseAttrs(m[2])

		switch kind {
		case "blurb":
			para, next, err := paragraphAt(lines, i+1)
			if err != nil {
				return nil, fmt.Errorf("site:blurb: %w", err)
			}
			r.Blurb, i = para, next

		case "install":
			name := attrs["name"]
			if name == "" {
				return nil, fmt.Errorf("site:install at README line %d has no name", i+1)
			}
			code, next, err := fenceAt(lines, i+1)
			if err != nil {
				return nil, fmt.Errorf("site:install %q: %w", name, err)
			}
			r.Installs = append(r.Installs, Install{Name: name, Command: code})
			i = next

		case "table":
			id := attrs["id"]
			if id == "" {
				return nil, fmt.Errorf("site:table at README line %d has no id", i+1)
			}
			t, next, err := tableAt(lines, i+1)
			if err != nil {
				return nil, fmt.Errorf("site:table %q: %w", id, err)
			}
			r.Tables[id], i = t, next

		default:
			return nil, fmt.Errorf("unknown marker site:%s at README line %d", kind, i+1)
		}
	}
	return r, nil
}

func parseAttrs(s string) map[string]string {
	attrs := map[string]string{}
	for _, m := range attrRe.FindAllStringSubmatch(s, -1) {
		attrs[m[1]] = m[2]
	}
	return attrs
}

// skipBlank advances past empty lines, so a marker may sit one line above what
// it names.
func skipBlank(lines []string, i int) int {
	for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
		i++
	}
	return i
}

func paragraphAt(lines []string, i int) (string, int, error) {
	i = skipBlank(lines, i)
	if i >= len(lines) {
		return "", 0, fmt.Errorf("no paragraph follows the marker")
	}
	var para []string
	for ; i < len(lines) && strings.TrimSpace(lines[i]) != ""; i++ {
		para = append(para, strings.TrimSpace(lines[i]))
	}
	return strings.Join(para, " "), i - 1, nil
}

func fenceAt(lines []string, i int) (string, int, error) {
	i = skipBlank(lines, i)
	if i >= len(lines) || !strings.HasPrefix(lines[i], "```") {
		return "", 0, fmt.Errorf("no fenced block follows the marker")
	}
	var code []string
	for i++; i < len(lines) && !strings.HasPrefix(lines[i], "```"); i++ {
		code = append(code, lines[i])
	}
	if i >= len(lines) {
		return "", 0, fmt.Errorf("unterminated fenced block")
	}
	return strings.Join(code, "\n"), i, nil
}

func tableAt(lines []string, i int) (Table, int, error) {
	i = skipBlank(lines, i)
	if i+1 >= len(lines) || !strings.HasPrefix(lines[i], "|") || !tableSepRe.MatchString(lines[i+1]) {
		return Table{}, 0, fmt.Errorf("no table follows the marker")
	}
	t := Table{Header: splitRow(lines[i])}
	for i += 2; i < len(lines) && strings.HasPrefix(lines[i], "|"); i++ {
		t.Rows = append(t.Rows, splitRow(lines[i]))
	}
	if len(t.Rows) == 0 {
		return Table{}, 0, fmt.Errorf("table has no rows")
	}
	return t, i - 1, nil
}

func splitRow(line string) []string {
	cells := strings.Split(strings.Trim(strings.TrimSpace(line), "|"), "|")
	for i, c := range cells {
		cells[i] = strings.TrimSpace(c)
	}
	return cells
}
