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
	Claude      Agent = "claude"
	Codex       Agent = "codex"
	Cursor      Agent = "cursor"
	Gemini      Agent = "gemini"
	Antigravity Agent = "antigravity"
	Hermes      Agent = "hermes"
	Pi          Agent = "pi"
	OpenClaw    Agent = "openclaw"
)

// Agents lists every agent this package understands, in a stable order.
var Agents = []Agent{Claude, Codex, Cursor, Gemini, Antigravity, Hermes, Pi, OpenClaw}

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
//   - Gemini CLI — .gemini/skills in the workspace, ~/.gemini/skills for the
//     user (google-gemini/gemini-cli, docs/cli/skills.md). It also reads the
//     shared .agents/skills, but its own directory is unambiguous.
//   - Antigravity — .agents/skills in the workspace, ~/.gemini/config/skills
//     for the user (antigravity.google/docs/skills). It is an IDE, so there is
//     no executable to look for, and its user directory sits inside Gemini
//     CLI's — hence the explicit detect path below.
//   - Hermes — .hermes/skills in a git repository, ~/.hermes/skills for the
//     user (NousResearch/hermes-agent, docs/user-guide/features/skills.md).
//     Project skills stay inert until `hermes skills trust` runs, which is why
//     Note exists.
//   - Pi — .pi/skills in the project, ~/.pi/agent/skills for the user
//     (pi.dev/docs/latest/skills). Note the extra agent/ segment: pi is the
//     first agent here whose user-level path is not its project path under
//     $HOME.
//   - OpenClaw — the workspace's own skills/ directory, and ~/.openclaw/skills
//     for --global (docs.openclaw.ai/tools/skills). The project path is the
//     documented one even though it collides with this repository's own
//     skills/ source directory; picking a different path for everyone to
//     spare one repository would install skills where OpenClaw may not look.
type layout struct {
	binary  string
	project string // "/"-separated, relative to the project root
	global  string // "/"-separated, relative to $HOME
	// projectNote is printed after writing a project-level skill, when the
	// file alone is not enough for the agent to load it.
	projectNote string
	// detect is the directory under $HOME whose existence is evidence the
	// agent is in use. It defaults to the first segment of global, which is
	// wrong when two agents share a root: ~/.gemini belongs to Gemini CLI,
	// while Antigravity lives under ~/.gemini/config.
	detect string
}

var layouts = map[Agent]layout{
	Claude: {binary: "claude", project: ".claude/skills", global: ".claude/skills"},
	Codex:  {binary: "codex", project: ".agents/skills", global: ".agents/skills"},
	Cursor: {binary: "cursor", project: ".cursor/skills", global: ".cursor/skills"},
	Gemini: {binary: "gemini", project: ".gemini/skills", global: ".gemini/skills", detect: ".gemini"},
	Antigravity: {
		// An IDE, so no binary: only its own skills directory is evidence,
		// and ~/.gemini alone would be Gemini CLI's, not Antigravity's.
		project: ".agents/skills", global: ".gemini/config/skills", detect: ".gemini/config/skills",
	},
	Hermes: {
		binary: "hermes", project: ".hermes/skills", global: ".hermes/skills",
		projectNote: "hermes does not load project skills until they are trusted: run `hermes skills trust`",
	},
	Pi:       {binary: "pi", project: ".pi/skills", global: ".pi/agent/skills"},
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

// detectDir returns the directory whose existence is evidence of the agent.
func (l layout) detectDir() string {
	if l.detect != "" {
		return l.detect
	}
	root, _, _ := strings.Cut(l.global, "/")
	return root
}

// ProjectNote returns what still has to happen after a project-level install
// for the agent to actually load the skill, or "" when writing the file is
// enough. Reporting "wrote" and stopping would be telling somebody a thing
// works when nothing will load it.
func (a Agent) ProjectNote() string { return layouts[a].projectNote }

// Detection is what the machine says about one agent, and why.
type Detection struct {
	Agent Agent `json:"agent"`
	// Found reports whether the agent looks installed.
	Found bool `json:"found"`
	// Reason is the evidence, e.g. "codex on PATH" or "~/.claude". It is
	// shown in the selection menu: without it, an agent yakanban cannot see
	// and an agent that is genuinely absent look identical.
	Reason string `json:"reason,omitempty"`
}

// Detect reports on every known agent, in the order of Agents.
//
// This only decides a *default*: the boxes ticked in the selection menu, or
// the set installed to when nobody can be asked. An explicit --agent always
// wins over it.
func Detect(home string, lookPath LookPathFunc) []Detection {
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	out := make([]Detection, 0, len(Agents))
	for _, a := range Agents {
		l := layouts[a]
		d := Detection{Agent: a}
		if l.binary != "" {
			if _, err := lookPath(l.binary); err == nil {
				d.Found, d.Reason = true, l.binary+" on PATH"
			}
		}
		if !d.Found && home != "" {
			if st, err := os.Stat(filepath.Join(home, filepath.FromSlash(l.detectDir()))); err == nil && st.IsDir() {
				d.Found, d.Reason = true, "~/"+l.detectDir()
			}
		}
		out = append(out, d)
	}
	return out
}

// Present reports whether an agent looks installed on this machine.
func Present(a Agent, home string, lookPath LookPathFunc) bool {
	for _, d := range Detect(home, lookPath) {
		if d.Agent == a {
			return d.Found
		}
	}
	return false
}
