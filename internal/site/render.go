package site

import (
	"embed"
	"fmt"
	"html"
	"html/template"
	"strings"
	txttemplate "text/template"
	"time"
)

//go:embed assets
var assets embed.FS

// Page is everything the templates are allowed to know.
type Page struct {
	Title    string
	Blurb    string
	Repo     string
	SiteURL  string
	Version  string
	Built    string
	Installs []Install
	Hero     Section
	Sections []Section

	CSS template.CSS

	// ExitCodeSummary is the exit-code table folded into one line, for
	// llms.txt, where an agent wants the contract and not a page of prose.
	ExitCodeSummary string
}

// Input is what Render needs from the repository.
type Input struct {
	Readme  *Readme
	Content *Content
	Samples map[string]Transcript
	Repo    string
	SiteURL string
	Version string
	Built   time.Time
}

// Build assembles the page from the repository's sources, checking as it goes
// that every section actually got what it asked for.
func Build(in Input) (*Page, error) {
	if len(in.Readme.Installs) == 0 {
		return nil, fmt.Errorf("README.md has no site:install markers")
	}

	css, err := assets.ReadFile("assets/styles.css")
	if err != nil {
		return nil, err
	}

	p := &Page{
		Title:    in.Content.Title,
		Blurb:    in.Readme.Blurb,
		Repo:     strings.TrimSuffix(in.Repo, "/"),
		SiteURL:  strings.TrimSuffix(in.SiteURL, "/"),
		Version:  in.Version,
		Built:    in.Built.Format("2 January 2006"),
		Installs: in.Readme.Installs,
		CSS:      template.CSS(css),
	}
	if p.Blurb == "" {
		return nil, fmt.Errorf("README.md has no site:blurb marker")
	}

	for _, s := range in.Content.Sections {
		if s.Sample != "" {
			t, ok := in.Samples[s.Sample]
			if !ok {
				return nil, fmt.Errorf("section %q wants the capture %q, which no sample produces", s.Title, s.Sample)
			}
			s.SampleHTML = template.HTML(codeHTML("console", "$ "+t.Command+"\n"+t.Output))
		}
		if s.Table != "" {
			t, ok := in.Readme.Tables[s.Table]
			if !ok {
				return nil, fmt.Errorf("section %q wants the README table %q, which is not marked up", s.Title, s.Table)
			}
			s.TableHTML = template.HTML(tableHTML(t))
		}
		if strings.EqualFold(s.Title, "hero") {
			p.Hero = s
			continue
		}
		p.Sections = append(p.Sections, s)
	}

	if p.Hero.Body == "" {
		return nil, fmt.Errorf("content.md has no Hero section")
	}
	exit, ok := in.Readme.Tables["exit-codes"]
	if !ok {
		return nil, fmt.Errorf("README.md has no site:table id=\"exit-codes\"")
	}
	p.ExitCodeSummary = summarise(exit)

	return p, nil
}

// summarise folds a two-column table into one sentence.
func summarise(t Table) string {
	parts := make([]string, 0, len(t.Rows))
	for _, r := range t.Rows {
		if len(r) < 2 {
			continue
		}
		parts = append(parts, r[0]+" "+strings.TrimSuffix(r[1], "."))
	}
	return strings.Join(parts, ", ")
}

func tableHTML(t Table) string {
	var b strings.Builder
	b.WriteString("<table>\n<thead><tr>")
	for _, h := range t.Header {
		b.WriteString("<th>" + inlineHTML(h) + "</th>")
	}
	b.WriteString("</tr></thead>\n<tbody>\n")
	for _, row := range t.Rows {
		b.WriteString("<tr>")
		for _, cell := range row {
			b.WriteString("<td>" + inlineHTML(cell) + "</td>")
		}
		b.WriteString("</tr>\n")
	}
	b.WriteString("</tbody>\n</table>\n")
	return b.String()
}

// normalise makes what is generated independent of how the repository was
// checked out. The templates are embedded verbatim, so a CRLF checkout — what
// Git gives a Windows machine by default — would otherwise put carriage
// returns in the published files.
func normalise(s string) string {
	return strings.ReplaceAll(s, "\r\n", "\n")
}

// RenderHTML writes the landing page.
func (p *Page) RenderHTML() (string, error) {
	t, err := template.New("index.html.tmpl").
		Funcs(template.FuncMap{
			"inc": func(i int) int { return i + 1 },
			// Headings carry the same inline Markdown as the prose under
			// them: `delete` is a command, not three backticks.
			"md": func(s string) template.HTML { return template.HTML(inlineHTML(s)) },
		}).
		ParseFS(assets, "assets/index.html.tmpl")
	if err != nil {
		return "", err
	}
	var out strings.Builder
	if err := t.Execute(&out, p); err != nil {
		return "", err
	}
	return normalise(out.String()), nil
}

// RenderLLMs writes llms.txt, which is plain text and so must not go through
// the HTML escaper.
func (p *Page) RenderLLMs() (string, error) {
	t, err := txttemplate.ParseFS(assets, "assets/llms.txt.tmpl")
	if err != nil {
		return "", err
	}
	var out strings.Builder
	if err := t.Execute(&out, p); err != nil {
		return "", err
	}
	text := normalise(html.UnescapeString(out.String()))
	if !strings.HasPrefix(text, "# ") {
		return "", fmt.Errorf("llms.txt must open with an H1")
	}
	return text, nil
}
