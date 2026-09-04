package site

import (
	"fmt"
	"html/template"
	"regexp"
	"strings"
)

// site/content.md holds the prose that exists only on the landing page — the
// narrative between the facts. It states nothing that README.md also states:
// commands, tables and terminal output are pulled from the repository instead,
// so there is no second copy of anything to keep true.

var (
	slugDrop = regexp.MustCompile(`['\x{2019}]`)
	slugRe   = regexp.MustCompile(`[^a-z0-9]+`)
)

// Item is a titled paragraph inside a section: one entry of "what we didn't
// build", one point of the pitch.
type Item struct {
	Title string
	Body  template.HTML
}

// Section is one band of the single-column page.
type Section struct {
	ID     string
	Title  string
	Sample string // name of a terminal capture to show beside the prose
	Table  string // id of a README table to render in the section
	Body   template.HTML
	Items  []Item

	// Filled in by Render, from the captures and the README.
	SampleHTML template.HTML
	TableHTML  template.HTML
}

// Content is site/content.md, parsed.
type Content struct {
	Title    string
	Sections []Section
}

// ParseContent reads the landing page's own prose.
func ParseContent(md string) (*Content, error) {
	c := &Content{}
	var (
		section *Section
		item    *Item
		buf     []string
	)

	flush := func() {
		switch {
		case item != nil:
			item.Body = template.HTML(blocksHTML(strings.Join(buf, "\n")))
		case section != nil:
			section.Body = template.HTML(blocksHTML(strings.Join(section.directives(buf), "\n")))
		}
		buf = nil
	}
	closeItem := func() {
		if item != nil {
			flush()
			section.Items = append(section.Items, *item)
			item = nil
		}
	}
	closeSection := func() {
		closeItem()
		if section != nil {
			if section.Body == "" && len(section.Items) == 0 {
				flush()
			}
			c.Sections = append(c.Sections, *section)
			section = nil
		}
	}

	inFence := false
	for _, line := range strings.Split(md, "\n") {
		if strings.HasPrefix(line, "```") {
			inFence = !inFence
		}
		m := headingLevel.FindStringSubmatch(line)
		if inFence || m == nil {
			if section == nil && c.Title != "" && strings.TrimSpace(line) != "" {
				return nil, fmt.Errorf("content.md: prose before the first section: %q", line)
			}
			if section != nil {
				buf = append(buf, line)
			}
			continue
		}

		title := strings.TrimSpace(m[2])
		switch len(m[1]) {
		case 1:
			closeSection()
			c.Title = title
		case 2:
			closeSection()
			section = &Section{ID: slug(title), Title: title}
		case 3:
			if section == nil {
				return nil, fmt.Errorf("content.md: %q is not inside a section", title)
			}
			closeItem()
			flush()
			item = &Item{Title: title}
		default:
			return nil, fmt.Errorf("content.md: heading %q is deeper than the page has levels", title)
		}
	}
	closeSection()

	if c.Title == "" {
		return nil, fmt.Errorf("content.md has no title")
	}
	if len(c.Sections) == 0 {
		return nil, fmt.Errorf("content.md has no sections")
	}
	return c, nil
}

// directives pulls the "sample:" and "table:" lines out of a section body.
// They say what the section shows; the rest of it is prose.
func (s *Section) directives(lines []string) []string {
	var kept []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if name, ok := strings.CutPrefix(trimmed, "sample:"); ok {
			s.Sample = strings.TrimSpace(name)
			continue
		}
		if id, ok := strings.CutPrefix(trimmed, "table:"); ok {
			s.Table = strings.TrimSpace(id)
			continue
		}
		kept = append(kept, line)
	}
	return kept
}

// slug builds the fragment a section is linked by. Apostrophes are dropped
// rather than turned into separators, so "what we didn't build" anchors at
// #what-we-didnt-build — the id llms.txt points at.
func slug(s string) string {
	s = slugDrop.ReplaceAllString(strings.ToLower(s), "")
	return strings.Trim(slugRe.ReplaceAllString(s, "-"), "-")
}
