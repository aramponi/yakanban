package skill

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/aramponi/yakanban/internal/core"
	bundled "github.com/aramponi/yakanban/skills"
)

// Options describes where a skill command should write, and what.
//
// Nothing here comes from the board: these commands must work before
// `yakanban init` and outside a repository.
type Options struct {
	// Root is the project directory, used for a project-local install.
	Root string
	// Home is the user's home directory, used for --global.
	Home string
	// Global installs into the user-level directory instead of the project.
	Global bool
	// Path writes to this directory and skips agent detection entirely.
	Path string
	// Agents restricts which agents to install for. Empty means the ones
	// detected on this machine.
	Agents []Agent
	// Skills restricts which bundled skills to install. Empty means all.
	Skills []string
	// LookPath is injected by tests; nil means exec.LookPath.
	LookPath LookPathFunc
}

// Targets resolves the options into the files a command would touch.
func (o Options) Targets() ([]Target, error) {
	names, err := o.skillNames()
	if err != nil {
		return nil, err
	}
	if o.Path != "" {
		targets := make([]Target, 0, len(names))
		for _, name := range names {
			targets = append(targets, Target{Skill: name, Path: filepath.Join(o.Path, name, "SKILL.md")})
		}
		return targets, nil
	}
	agents, err := o.resolveAgents()
	if err != nil {
		return nil, err
	}
	targets := make([]Target, 0, len(agents)*len(names))
	// Two agents can share a discovery directory — Codex and Antigravity both
	// read .agents/skills — and writing the same file twice would report one
	// install as two.
	claimed := make(map[string]bool, len(agents)*len(names))
	for _, agent := range agents {
		dir := agent.ProjectDir(o.Root)
		note := agent.ProjectNote()
		if o.Global {
			if o.Home == "" {
				return nil, fmt.Errorf("%w: --global needs a home directory", core.ErrInvalidInput)
			}
			dir = agent.GlobalDir(o.Home)
			note = "" // the note is about project-level discovery only
		}
		for _, name := range names {
			path := filepath.Join(dir, name, "SKILL.md")
			if claimed[path] {
				continue
			}
			claimed[path] = true
			targets = append(targets, Target{
				Skill: name,
				Agent: agent,
				Path:  path,
				Note:  note,
			})
		}
	}
	return targets, nil
}

func (o Options) skillNames() ([]string, error) {
	if len(o.Skills) == 0 {
		return bundled.Names(), nil
	}
	out := make([]string, 0, len(o.Skills))
	for _, name := range o.Skills {
		name = strings.TrimSpace(name)
		if _, err := bundled.Get(name); err != nil {
			return nil, fmt.Errorf("%w: %v", core.ErrInvalidInput, err)
		}
		out = append(out, name)
	}
	return out, nil
}

// resolveAgents returns the agents to install for: the ones asked for, or the
// ones this machine appears to have.
func (o Options) resolveAgents() ([]Agent, error) {
	if len(o.Agents) > 0 {
		return o.Agents, nil
	}
	var detected []Agent
	for _, d := range Detect(o.Home, o.LookPath) {
		if d.Found {
			detected = append(detected, d.Agent)
		}
	}
	if len(detected) == 0 {
		names := make([]string, 0, len(Agents))
		for _, a := range Agents {
			names = append(names, string(a))
		}
		return nil, fmt.Errorf("%w: no AI coding agent detected on this machine; name one with --agent (known: %s) or write to a directory with --path",
			core.ErrNotFound, strings.Join(names, ", "))
	}
	return detected, nil
}

// Install writes every target, creating what is missing.
func Install(targets []Target, version string, force bool) ([]Result, error) {
	return write(targets, version, force, true)
}

// Update refreshes what is already installed, and creates nothing: a skill
// nobody installed should not appear as a side effect of an update.
func Update(targets []Target, version string, force bool) ([]Result, error) {
	return write(targets, version, force, false)
}

func write(targets []Target, version string, force, create bool) ([]Result, error) {
	results := make([]Result, 0, len(targets))
	for _, t := range targets {
		r, err := Write(t, version, force, create)
		if err != nil {
			return results, err
		}
		results = append(results, r)
	}
	return results, nil
}
