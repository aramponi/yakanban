package site

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"regexp"

	"github.com/aramponi/yakanban/internal/site/demo"
)

// The hero clip replays a Claude Code session recorded on a throwaway
// repository. It cannot be captured at build time like the terminal blocks:
// the session writes to a board, and generating a page must never write
// anything. So it is recorded once (site/demo/record.sh), condensed into a
// timeline (cmd/gendemo) and committed; the page says when it was recorded.

var demoNameRe = regexp.MustCompile(`^[a-z0-9-]+$`)

// LoadDemos reads the timelines the content asks for, from site/demo.
func LoadDemos(root string, c *Content) (map[string][]byte, error) {
	out := map[string][]byte{}
	for _, s := range c.Sections {
		if s.Demo == "" {
			continue
		}
		if !demoNameRe.MatchString(s.Demo) {
			return nil, fmt.Errorf("section %q: demo name %q is not a plain name", s.Title, s.Demo)
		}
		b, err := os.ReadFile(filepath.Join(root, "site", "demo", s.Demo+".json"))
		if err != nil {
			return nil, err
		}
		out[s.Demo] = b
	}
	return out, nil
}

// demoHTML is the clip's markup: where it mounts, its timeline and the
// player, in that order, so the player finds both when it runs.
func demoHTML(name string, raw []byte) (template.HTML, error) {
	if raw == nil {
		return "", fmt.Errorf("demo %q has no timeline", name)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var tl demo.Timeline
	if err := dec.Decode(&tl); err != nil {
		return "", fmt.Errorf("demo %q: %w", name, err)
	}
	// A hand-written timeline has no recording date; the page shows a
	// recorded session or nothing.
	if tl.Recorded == "" || tl.Real == 0 || len(tl.Events) == 0 {
		return "", fmt.Errorf("demo %q is not a recorded session", name)
	}
	if err := demo.Scrub(&tl); err != nil {
		return "", fmt.Errorf("demo %q: %w", name, err)
	}
	data, err := json.Marshal(tl) // escapes <, > and &, so it cannot close its script
	if err != nil {
		return "", err
	}
	js, err := assets.ReadFile("assets/demo.js")
	if err != nil {
		return "", err
	}
	if bytes.Contains(bytes.ToLower(js), []byte("</script")) {
		return "", fmt.Errorf("demo.js contains </script and cannot be inlined")
	}
	id := "demo-" + name
	return template.HTML(`<div data-demo="` + id + `"></div>
<script type="application/json" id="` + id + `">` + string(data) + `</script>
<script>` + string(js) + `</script>`), nil
}
