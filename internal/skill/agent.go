// Package skill installs the agent skills embedded in the skills package
// (yakanban's own docs live there too — see skills/embed.go) into an AI
// coding agent's skill directory: Claude Code, Codex CLI, Cursor or
// OpenClaw. It touches no board and reads no .yakanban.yml, so it works
// before `yakanban init` and outside a repository.
package skill

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/aramponi/yakanban/internal/core"
)

// Agent identifies an AI coding agent yakanban can install skills for.
type Agent string

// Supported agents.
const (
	Claude   Agent = "claude"
	Codex    Agent = "codex"
	Cursor   Agent = "cursor"
	OpenClaw Agent = "openclaw"
)

// Agents lists every agent this package understands, in a stable order.
var Agents = []Agent{Claude, Codex, Cursor, OpenClaw}

// layout is one agent's skill directory convention, plus the executable used
// to detect it on the machine. Every path below was read from that agent's
// own documentation, checked on 2026-09-04: writing a skill file nothing will
// ever load is worse than refusing to write one.
//
//   - Claude Code — project .claude/skills, user ~/.claude/skills.
//   - Codex CLI — .agents/skills, scanned from the working directory up to
//     the repository root, and the same path under $HOME for personal skills
//     (developers.openai.com/codex/concepts/customization).
//   - Cursor — .cursor/skills and ~/.cursor/skills. Cursor also reads
//     .agents/skills, but its own directory is the one it documents first
//     (cursor.com/docs/skills).
//   - OpenClaw — the workspace's own skills/ directory, and ~/.openclaw/skills
//     for --global (docs.openclaw.ai/tools/skills). The project path is the
//     documented one even though it collides with this repository's own
//     skills/ source directory; picking a different path for everyone to
//     spare one repository would install skills where OpenClaw may not look.
type layout struct {
	binary  string
	project string // "/"-separated, relative to the project root
	global  string // "/"-separated, relative to $HOME
}

var layouts = map[Agent]layout{
	Claude:   {binary: "claude", project: ".claude/skills", global: ".claude/skills"},
	Codex:    {binary: "codex", project: ".agents/skills", global: ".agents/skills"},
	Cursor:   {binary: "cursor", project: ".cursor/skills", global: ".cursor/skills"},
	OpenClaw: {binary: "openclaw", project: "skills", global: ".openclaw/skills"},
}

// ParseAgent validates a user-supplied agent name.
func ParseAgent(s string) (Agent, error) {
	a := Agent(strings.ToLower(strings.TrimSpace(s)))
	if _, ok := layouts[a]; !ok {
		names := make([]string, 0, len(Agents))
		for _, x := range Agents {
			names = append(names, string(x))
		}
		return "", fmt.Errorf("%w: unknown agent %q (known: %s)", core.ErrInvalidInput, s, strings.Join(names, ", "))
	}
	return a, nil
}

// ProjectDir returns the project-local skills root for an agent.
func (a Agent) ProjectDir(root string) string {
	return filepath.Join(root, filepath.FromSlash(layouts[a].project))
}

// GlobalDir returns the user-level skills root for an agent.
func (a Agent) GlobalDir(home string) string {
	return filepath.Join(home, filepath.FromSlash(layouts[a].global))
}

// LookPathFunc abstracts exec.LookPath so detection is testable without a
// real PATH.
type LookPathFunc func(file string) (string, error)

// Present reports whether an agent looks installed on this machine: its
// executable is on PATH, or its global config directory already exists.
// This only ever decides a *default* — the set offered by the interactive
// prompt, or installed to when stdout is not a terminal — an explicit
// --agent always overrides it.
func Present(a Agent, home string, lookPath LookPathFunc) bool {
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	if _, err := lookPath(layouts[a].binary); err == nil {
		return true
	}
	if home == "" {
		return false
	}
	root, _, _ := strings.Cut(layouts[a].global, "/")
	st, err := os.Stat(filepath.Join(home, root))
	return err == nil && st.IsDir()
}
