package site

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode"
)

// These tests are the mechanism that stops the published page from drifting
// away from the repository. Generation is assertive — a marker that moves or a
// section that asks for something nobody produces fails the build — and what
// follows runs that same generation against the real README.md and
// site/content.md, so CI catches the drift before Pages publishes it.

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func readFile(t *testing.T, parts ...string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(append([]string{repoRoot(t)}, parts...)...))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// stubSamples stands in for the captures, which need a network and a token.
// What is under test here is that every section asks for a capture that
// exists, not what the capture said.
func stubSamples() map[string]Transcript {
	out := map[string]Transcript{}
	for _, s := range Samples {
		out[s.Name] = Transcript{Command: "yakanban " + strings.Join(s.Args, " "), Output: "OUTPUT"}
	}
	return out
}

func buildFromRepo(t *testing.T) (*Page, *Readme) {
	t.Helper()
	readme, err := ParseReadme(readFile(t, "README.md"))
	if err != nil {
		t.Fatalf("README.md: %v", err)
	}
	content, err := ParseContent(readFile(t, "site", "content.md"))
	if err != nil {
		t.Fatalf("site/content.md: %v", err)
	}
	page, err := Build(Input{
		Readme:  readme,
		Content: content,
		Samples: stubSamples(),
		Repo:    "https://github.com/aramponi/yakanban",
		SiteURL: "https://aramponi.github.io/yakanban",
		Version: "v0.0.0-test",
		Built:   time.Unix(0, 0).UTC(),
	})
	if err != nil {
		t.Fatalf("building the page from this repository: %v", err)
	}
	return page, readme
}

// TestThePageStillBuildsFromThisRepository fails when a README heading, table
// or install block moves out from under its marker, or when a section of
// content.md asks for a capture or a table that no longer exists.
func TestThePageStillBuildsFromThisRepository(t *testing.T) {
	page, readme := buildFromRepo(t)

	if len(readme.Installs) < 3 {
		t.Errorf("the hero promises Homebrew, go install and a binary: got %d install methods", len(readme.Installs))
	}
	for _, id := range []string{"commands", "exit-codes"} {
		if len(readme.Tables[id].Rows) == 0 {
			t.Errorf("README table %q is empty or missing", id)
		}
	}
	if len(page.Sections) < 5 {
		t.Errorf("got %d sections, want the full narrative", len(page.Sections))
	}
}

// TestTheInstallCommandsOnThePageAreTheReadmeOnes is the check that matters
// most: the commands a reader copies must be the ones the repository
// documents, not a copy somebody updated once.
func TestTheInstallCommandsOnThePageAreTheReadmeOnes(t *testing.T) {
	page, readme := buildFromRepo(t)
	html, err := page.RenderHTML()
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range readme.Installs {
		// The template escapes what it renders, so compare on the first
		// line, which carries the command name and no metacharacters.
		first, _, _ := strings.Cut(m.Command, "\n")
		first, _, _ = strings.Cut(first, "$")
		if !strings.Contains(html, strings.TrimSpace(first)) {
			t.Errorf("install method %q is in README.md but not on the page", m.Name)
		}
	}
}

// TestWhatWeDidntBuildGivesReasons guards the section the ticket asked for: a
// bare list of omissions reads as an apology, and the reasons are the point.
func TestWhatWeDidntBuildGivesReasons(t *testing.T) {
	page, _ := buildFromRepo(t)
	i := slices.IndexFunc(page.Sections, func(s Section) bool { return s.ID == "what-we-didnt-build" })
	if i < 0 {
		t.Fatal("no \"what we didn't build\" section")
	}
	for _, item := range page.Sections[i].Items {
		if len(strings.TrimSpace(string(item.Body))) < 80 {
			t.Errorf("%q is listed without a reason", item.Title)
		}
	}
	if len(page.Sections[i].Items) < 4 {
		t.Errorf("got %d omissions, want the list the README's decisions justify", len(page.Sections[i].Items))
	}
}

// TestLLMsTxtFollowsTheConvention checks the shape llmstxt.org specifies: an
// H1, a blockquote, then H2 sections whose bodies are Markdown link lists.
func TestLLMsTxtFollowsTheConvention(t *testing.T) {
	page, _ := buildFromRepo(t)
	out, err := page.RenderLLMs()
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(out, "\n")

	if !strings.HasPrefix(lines[0], "# ") {
		t.Errorf("first line is %q, want an H1", lines[0])
	}
	blockquote := slices.IndexFunc(lines, func(l string) bool { return strings.HasPrefix(l, "> ") })
	if blockquote < 0 || blockquote > 3 {
		t.Error("no blockquote summary near the top")
	}

	link := regexp.MustCompile(`^- \[[^\]]+\]\(https?://[^)]+\)(: .+)?$`)
	var headings []string
	inSection := false
	for _, l := range lines {
		switch {
		case strings.HasPrefix(l, "## "):
			headings = append(headings, strings.TrimPrefix(l, "## "))
			inSection = true
		case inSection && strings.HasPrefix(l, "-"):
			if !link.MatchString(l) {
				t.Errorf("not a well-formed link-list entry: %q", l)
			}
		}
	}
	if !slices.Contains(headings, "Optional") {
		t.Errorf("no ## Optional section; headings were %v", headings)
	}
	if strings.Contains(out, "&#") || strings.Contains(out, "&amp;") {
		t.Error("llms.txt is plain text and must not carry HTML entities")
	}
}

// TestLLMsTxtFragmentsResolve: an agent following a link into the page must
// land on a section that exists, not at the top of it.
func TestLLMsTxtFragmentsResolve(t *testing.T) {
	page, _ := buildFromRepo(t)
	out, err := page.RenderLLMs()
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, s := range page.Sections {
		ids[s.ID] = true
	}
	for _, m := range regexp.MustCompile(`\(`+regexp.QuoteMeta(page.SiteURL)+`[^)#]*#([^)]+)\)`).FindAllStringSubmatch(out, -1) {
		if !ids[m[1]] {
			t.Errorf("llms.txt links to #%s, which is not a section of the page", m[1])
		}
	}
}

// TestCapturesAreReadOnly: generating a web page must never move a ticket.
func TestCapturesAreReadOnly(t *testing.T) {
	for _, s := range Samples {
		if len(s.Args) == 0 {
			t.Fatalf("sample %q runs nothing", s.Name)
		}
		if !slices.Contains(readOnly, s.Args[0]) {
			t.Errorf("sample %q runs %q, which is not on the read-only allowlist", s.Name, s.Args[0])
		}
	}
}

// TestEveryCaptureIsUsed keeps the generator from spending a board round trip
// on output no section shows.
func TestEveryCaptureIsUsed(t *testing.T) {
	page, _ := buildFromRepo(t)
	used := map[string]bool{}
	for _, s := range append(slices.Clone(page.Sections), page.Hero) {
		if s.Sample != "" {
			used[s.Sample] = true
		}
	}
	for _, s := range Samples {
		if !used[s.Name] {
			t.Errorf("sample %q is captured but never shown", s.Name)
		}
	}
}

// TestNoEmojiInTheCopy is an acceptance criterion of the ticket, and the kind
// that only stays true if something checks it.
func TestNoEmojiInTheCopy(t *testing.T) {
	for _, path := range [][]string{{"site", "content.md"}} {
		for i, line := range strings.Split(readFile(t, path...), "\n") {
			for _, r := range line {
				if r > 0x2100 && !unicode.IsLetter(r) && !unicode.IsMark(r) {
					t.Errorf("%s:%d contains %q", filepath.Join(path...), i+1, r)
				}
			}
		}
	}
}

func TestInlineMarkdownEscapes(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"plain", "plain"},
		{"a <script> tag", "a &lt;script&gt; tag"},
		{"`--json` flag", "<code>--json</code> flag"},
		{"`<b>` stays code", "<code>&lt;b&gt;</code> stays code"},
		{"**bold**", "<strong>bold</strong>"},
		{"[docs](https://example.com)", `<a href="https://example.com">docs</a>`},
	} {
		if got := inlineHTML(tc.in); got != tc.want {
			t.Errorf("inlineHTML(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestSourcesParseWithEitherLineEnding: a Windows checkout converts the
// sources to CRLF, and several patterns here anchor at the end of a line. The
// generator has to read the same repository on every runner.
func TestSourcesParseWithEitherLineEnding(t *testing.T) {
	readme, content := readFile(t, "README.md"), readFile(t, "site", "content.md")
	crlf := func(s string) string {
		return strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\n", "\r\n")
	}

	lf, err := ParseReadme(readme)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ParseReadme(crlf(readme))
	if err != nil {
		t.Fatalf("README.md with CRLF line endings: %v", err)
	}
	if !reflect.DeepEqual(lf, got) {
		t.Error("README.md parses differently with CRLF line endings")
	}

	lfContent, err := ParseContent(content)
	if err != nil {
		t.Fatal(err)
	}
	gotContent, err := ParseContent(crlf(content))
	if err != nil {
		t.Fatalf("site/content.md with CRLF line endings: %v", err)
	}
	if !reflect.DeepEqual(lfContent, gotContent) {
		t.Error("site/content.md parses differently with CRLF line endings")
	}
}

// TestGeneratedFilesAreIndependentOfTheCheckout: the templates are embedded
// verbatim, so their line endings would otherwise reach the published files
// and make the site differ by the machine that built it.
func TestGeneratedFilesAreIndependentOfTheCheckout(t *testing.T) {
	page, _ := buildFromRepo(t)

	html, err := page.RenderHTML()
	if err != nil {
		t.Fatal(err)
	}
	llms, err := page.RenderLLMs()
	if err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{"index.html": html, "llms.txt": llms} {
		if strings.Contains(body, "\r") {
			t.Errorf("%s carries a carriage return from the checkout", name)
		}
	}
}

func TestParseReadmeRejectsAStrandedMarker(t *testing.T) {
	if _, err := ParseReadme("<!-- site:install name=\"Homebrew\" -->\n\njust prose\n"); err == nil {
		t.Error("a marker with no block under it must fail generation, not publish a hole")
	}
	if _, err := ParseReadme("<!-- site:table id=\"exit-codes\" -->\n\nnot a table\n"); err == nil {
		t.Error("a table marker with no table under it must fail generation")
	}
}
