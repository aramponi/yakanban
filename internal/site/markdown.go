// Package site generates the landing page and llms.txt from the repository's
// own sources, so the public copy cannot drift from the README.
package site

import (
	"fmt"
	"html"
	"regexp"
	"strconv"
	"strings"
)

// The site renders a deliberately small subset of Markdown: paragraphs, lists,
// fenced code and the inline forms used in site/content.md. A full parser
// would mean a third dependency in a repository that has two, and nothing on
// the page needs more than this.

var (
	inlineCode   = regexp.MustCompile("`([^`]+)`")
	inlineBold   = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	inlineLink   = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)
	placeholder  = regexp.MustCompile(`\x00(\d+)\x00`)
	headingLevel = regexp.MustCompile(`^(#{1,6})\s+(.*)$`)
)

// splitLines cuts text into lines with the line ending thrown away. Every
// parser here works a line at a time and several of them anchor a pattern at
// the end of one, so a CRLF checkout — which is what a Windows runner gets by
// default — would otherwise leave a stray carriage return outside the match.
func splitLines(s string) []string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimSuffix(line, "\r")
	}
	return lines
}

// inlineHTML renders one line of inline Markdown. Code spans are extracted
// first and put back last, so their contents are never treated as markup.
func inlineHTML(s string) string {
	var spans []string
	s = inlineCode.ReplaceAllStringFunc(s, func(m string) string {
		spans = append(spans, inlineCode.FindStringSubmatch(m)[1])
		return fmt.Sprintf("\x00%d\x00", len(spans)-1)
	})

	s = html.EscapeString(s)
	s = inlineLink.ReplaceAllString(s, `<a href="$2">$1</a>`)
	s = inlineBold.ReplaceAllString(s, `<strong>$1</strong>`)

	return placeholder.ReplaceAllStringFunc(s, func(m string) string {
		// The index was written by the loop above, so it always parses and
		// is always in range.
		i, _ := strconv.Atoi(strings.Trim(m, "\x00"))
		return "<code>" + html.EscapeString(spans[i]) + "</code>"
	})
}

// blocksHTML renders the block-level Markdown used in site/content.md:
// paragraphs, unordered lists and fenced code. Headings are consumed by the
// section splitter before they reach here.
func blocksHTML(md string) string {
	var out strings.Builder
	lines := splitLines(strings.TrimSpace(md))

	for i := 0; i < len(lines); i++ {
		line := strings.TrimRight(lines[i], " ")

		switch {
		case strings.TrimSpace(line) == "":
			continue

		case strings.HasPrefix(line, "```"):
			lang := strings.TrimSpace(strings.TrimPrefix(line, "```"))
			var code []string
			for i++; i < len(lines) && !strings.HasPrefix(lines[i], "```"); i++ {
				code = append(code, lines[i])
			}
			out.WriteString(codeHTML(lang, strings.Join(code, "\n")))

		case strings.HasPrefix(line, "- "):
			out.WriteString("<ul>\n")
			for ; i < len(lines) && strings.HasPrefix(lines[i], "- "); i++ {
				item := strings.TrimPrefix(lines[i], "- ")
				// Continuation lines are indented under their bullet.
				for i+1 < len(lines) && strings.HasPrefix(lines[i+1], "  ") {
					i++
					item += " " + strings.TrimSpace(lines[i])
				}
				out.WriteString("<li>" + inlineHTML(item) + "</li>\n")
			}
			i--
			out.WriteString("</ul>\n")

		default:
			var para []string
			for ; i < len(lines) && strings.TrimSpace(lines[i]) != "" &&
				!strings.HasPrefix(lines[i], "- ") && !strings.HasPrefix(lines[i], "```"); i++ {
				para = append(para, strings.TrimSpace(lines[i]))
			}
			i--
			out.WriteString("<p>" + inlineHTML(strings.Join(para, " ")) + "</p>\n")
		}
	}
	return out.String()
}

// codeHTML renders a fenced block. Console transcripts get their prompt lines
// marked up so the command reads differently from the output it produced.
func codeHTML(lang, code string) string {
	if lang != "console" {
		return "<pre><code>" + html.EscapeString(code) + "</code></pre>\n"
	}
	var out strings.Builder
	out.WriteString("<pre class=\"console\"><code>")
	for i, line := range splitLines(code) {
		if i > 0 {
			out.WriteString("\n")
		}
		if rest, ok := strings.CutPrefix(line, "$ "); ok {
			out.WriteString("<span class=\"prompt\">$ </span><b>" + html.EscapeString(rest) + "</b>")
			continue
		}
		out.WriteString(html.EscapeString(line))
	}
	out.WriteString("</code></pre>\n")
	return out.String()
}
