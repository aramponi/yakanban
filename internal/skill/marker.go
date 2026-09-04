package skill

import (
	"crypto/sha256"
	"fmt"
	"regexp"
)

// markerPattern matches the trailing HTML comment install/update stamp onto
// every skill file, modelled on kanban-md's own
// `<!-- kanban-md-skill-version: 0.34.1 -->` convention.
//
// Alongside the version it carries a hash of the pristine body, so check and
// update can tell "stale but never touched" (safe to refresh even without
// --force) from "the user edited this" (needs --force) without keeping a
// copy of every past release around: the hash was computed over exactly the
// bytes written at install time, so recomputing it over what is on disk now
// only still matches if nothing has changed since — including edits to the
// prose the marker sits below, which is why the check does not depend on the
// marker staying in one place in the file.
var markerPattern = regexp.MustCompile(`<!--\s*yakanban-skill-version:\s*(.+?)\s+sha256:([0-9a-f]{64})\s*-->\n?`)

type marker struct {
	Version string
	Hash    string
	Found   bool
}

func parseMarker(content []byte) marker {
	m := markerPattern.FindSubmatch(content)
	if m == nil {
		return marker{}
	}
	return marker{Version: string(m[1]), Hash: string(m[2]), Found: true}
}

// stamp appends a version marker to body, ready to write to disk.
func stamp(version string, body []byte) []byte {
	b := body
	if len(b) == 0 || b[len(b)-1] != '\n' {
		b = append(append([]byte(nil), b...), '\n')
	}
	sum := sha256.Sum256(b)
	line := fmt.Sprintf("<!-- yakanban-skill-version: %s sha256:%x -->\n", version, sum)
	return append(b, []byte(line)...)
}

// unmodified reports whether content is exactly what stamp produced: its
// stored hash still matches a fresh hash of the body around it. A missing or
// unparseable marker is never "unmodified" — it is unknown provenance, and
// must not be silently overwritten.
func unmodified(content []byte) bool {
	m := parseMarker(content)
	if !m.Found {
		return false
	}
	stripped := markerPattern.ReplaceAll(content, nil)
	sum := sha256.Sum256(stripped)
	return fmt.Sprintf("%x", sum) == m.Hash
}

// installedVersion extracts the version marker from an installed file, or
// reports ok=false when it is missing or malformed — treated as unknown and
// therefore stale by check and update, never a crash.
func installedVersion(content []byte) (version string, ok bool) {
	m := parseMarker(content)
	if !m.Found {
		return "", false
	}
	return m.Version, true
}
