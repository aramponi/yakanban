package cli

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/aramponi/yakanban/internal/core"
	"github.com/aramponi/yakanban/internal/skill"
	"github.com/aramponi/yakanban/internal/version"
	bundled "github.com/aramponi/yakanban/skills"
)

// newSkillCommand builds the `skill` group.
//
// None of these commands opens a board: skills are how an agent learns to use
// yakanban in the first place, so they have to work before `yakanban init`
// and outside a repository.
func newSkillCommand(e *env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skill",
		Short: "Install the agent skills bundled with this binary",
		Long: `Install, refresh and inspect the agent skills shipped inside yakanban.

The skills teach an AI coding agent how to drive the board. They are embedded
in the binary, so a downloaded release can install them with no checkout.`,
	}
	cmd.AddCommand(
		newSkillInstallCommand(e),
		newSkillUpdateCommand(e),
		newSkillCheckCommand(e),
		newSkillShowCommand(e),
	)
	return cmd
}

// skillFlags are the target-selection flags shared by install, update and check.
type skillFlags struct {
	agents []string
	skills []string
	path   string
	global bool
	force  bool
	yes    bool
}

func (f *skillFlags) bind(cmd *cobra.Command, withForce bool) {
	fl := cmd.Flags()
	fl.StringSliceVar(&f.agents, "agent", nil, "agents to install for: claude, codex, cursor, openclaw (default: the ones detected)")
	fl.StringSliceVar(&f.skills, "skill", nil, "skills to act on (default: all bundled ones)")
	fl.StringVar(&f.path, "path", "", "write to this directory instead, skipping agent detection")
	fl.BoolVar(&f.global, "global", false, "install into the user-level skill directory instead of this project")
	if withForce {
		fl.BoolVar(&f.force, "force", false, "overwrite a skill file that was edited locally")
		fl.BoolVarP(&f.yes, "yes", "y", false, "do not ask for confirmation")
	}
}

func (f *skillFlags) options(e *env) (skill.Options, error) {
	o := skill.Options{
		Root:   e.workDir(),
		Global: f.global,
		Path:   f.path,
		Skills: f.skills,
	}
	home, err := os.UserHomeDir()
	if err == nil {
		o.Home = home
	} else if f.global {
		return o, fmt.Errorf("%w: cannot locate your home directory: %v", core.ErrInvalidInput, err)
	}
	for _, name := range f.agents {
		agent, err := skill.ParseAgent(name)
		if err != nil {
			return o, err
		}
		o.Agents = append(o.Agents, agent)
	}
	return o, nil
}

func newSkillInstallCommand(e *env) *cobra.Command {
	var f skillFlags
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install the bundled skills",
		Long: `Write the bundled skills into an agent's skill directory.

The default target is this project, so the skills are versioned with the code
that they describe. --global installs them once for every project on the
machine.

A file you have edited is never overwritten without --force.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			o, err := f.options(e)
			if err != nil {
				return err
			}
			targets, err := o.Targets()
			if err != nil {
				return err
			}
			// Detection proposes; the person decides. --agent is the same
			// answer given up front, so it skips the question entirely.
			if len(f.agents) == 0 && !f.yes && interactive() {
				chosen, ok, err := e.chooseAgents(cmd, o)
				if err != nil {
					return err
				}
				if !ok {
					cmd.Println("nothing was written")
					return nil
				}
				o.Agents = chosen
				if targets, err = o.Targets(); err != nil {
					return err
				}
			}
			results, err := skill.Install(targets, version.String(), f.force)
			if err != nil {
				return err
			}
			return e.reportSkillResults(results)
		},
	}
	f.bind(cmd, true)
	return cmd
}

func newSkillUpdateCommand(e *env) *cobra.Command {
	var f skillFlags
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Refresh installed skills to this binary's version",
		Long: `Refresh the skills that are already installed.

Nothing is created: a skill nobody installed should not appear as a side
effect of an update. Use install for that.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			o, err := f.options(e)
			if err != nil {
				return err
			}
			targets, err := o.Targets()
			if err != nil {
				return err
			}
			results, err := skill.Update(targets, version.String(), f.force)
			if err != nil {
				return err
			}
			return e.reportSkillResults(results)
		},
	}
	f.bind(cmd, true)
	return cmd
}

func newSkillCheckCommand(e *env) *cobra.Command {
	var f skillFlags
	cmd := &cobra.Command{
		Use:   "check",
		Short: "Report which installed skills are out of date",
		Long: `Compare the installed skills with the ones this binary carries.

Exits non-zero when an installed skill is stale or has been edited, so it can
gate CI. A skill that is not installed at all is reported but does not fail:
otherwise every fresh clone would.`,
		Args:          cobra.NoArgs,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			o, err := f.options(e)
			if err != nil {
				return err
			}
			targets, err := o.Targets()
			if err != nil {
				return err
			}
			statuses, err := skill.Check(targets, version.String())
			if err != nil {
				return err
			}
			p := e.Printer()
			stale := 0
			for _, s := range statuses {
				if s.Stale() {
					stale++
				}
			}
			if e.format() == "json" {
				if err := p.JSON(statuses); err != nil {
					return err
				}
			} else {
				var notes []string
				seen := map[string]bool{}
				for _, s := range statuses {
					p.Printf("%-9s %s\n", s.State, s.Path)
					// A skill that is installed but not yet trusted is not
					// active, whatever its version says.
					if s.Note != "" && s.State != skill.StateMissing && !seen[s.Note] {
						seen[s.Note] = true
						notes = append(notes, s.Note)
					}
				}
				printNotes(p, notes)
			}
			if stale > 0 {
				return fmt.Errorf("%d installed skill(s) out of date; run `yakanban skill update`", stale)
			}
			return nil
		},
	}
	f.bind(cmd, false)
	return cmd
}

func newSkillShowCommand(e *env) *cobra.Command {
	return &cobra.Command{
		Use:   "show NAME",
		Short: "Print a bundled skill to stdout",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			p := e.Printer()
			if len(args) == 0 {
				for _, name := range bundled.Names() {
					p.Printf("%s\n", name)
				}
				return nil
			}
			sk, err := bundled.Get(args[0])
			if err != nil {
				return fmt.Errorf("%w: %v", core.ErrNotFound, err)
			}
			p.Printf("%s", sk.Content)
			return nil
		},
	}
}

// chooseAgents shows every known agent with the detected ones ticked, and
// lets the selection be edited before anything is written.
//
// Every agent is listed, detected or not: an agent yakanban cannot see and an
// agent that is genuinely absent must not look the same. The reason beside
// each tick is what tells them apart.
//
// It is line-based on purpose. A full-screen selector would mean a TUI
// dependency in a binary that has two, for one menu.
func (e *env) chooseAgents(cmd *cobra.Command, o skill.Options) ([]skill.Agent, bool, error) {
	detected := skill.Detect(o.Home, o.LookPath)
	selected := make([]bool, len(detected))
	for i, d := range detected {
		selected[i] = d.Found
	}
	out := cmd.OutOrStdout()
	reader := bufio.NewReader(cmd.InOrStdin())

	for {
		_, _ = fmt.Fprintln(out, "\nInstall the yakanban skills for:")
		for i, d := range detected {
			box := " "
			if selected[i] {
				box = "x"
			}
			reason := d.Reason
			if reason == "" {
				reason = "not detected"
			} else {
				reason = "found: " + reason
			}
			_, _ = fmt.Fprintf(out, "  [%s] %d) %-9s %s\n", box, i+1, d.Agent, reason)
		}
		_, _ = fmt.Fprint(out, "\nToggle with a number, Enter to install, q to cancel: ")

		line, err := reader.ReadString('\n')
		answer := strings.TrimSpace(line)
		if err != nil && answer == "" {
			// The input ended mid-question: treat it as a cancel rather than
			// installing something nobody confirmed.
			return nil, false, nil
		}
		switch strings.ToLower(answer) {
		case "":
			var chosen []skill.Agent
			for i, ok := range selected {
				if ok {
					chosen = append(chosen, detected[i].Agent)
				}
			}
			if len(chosen) == 0 {
				_, _ = fmt.Fprintln(out, "nothing selected")
				continue
			}
			return chosen, true, nil
		case "q", "quit", "n", "no":
			return nil, false, nil
		}
		n, err := strconv.Atoi(answer)
		if err != nil || n < 1 || n > len(detected) {
			_, _ = fmt.Fprintf(out, "%q is not one of 1-%d\n", answer, len(detected))
			continue
		}
		selected[n-1] = !selected[n-1]
	}
}

func (e *env) reportSkillResults(results []skill.Result) error {
	p := e.Printer()
	if e.format() == "json" {
		return p.JSON(results)
	}
	for _, r := range results {
		p.Printf("%-32s %s\n", r.Action, r.Path)
	}
	printNotes(p, notesFor(results))
	return nil
}

// notesFor collects what still has to be done, once per agent rather than
// once per file.
func notesFor(results []skill.Result) []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range results {
		if r.Note == "" || r.Action == skill.ActionNotInstalled || seen[r.Note] {
			continue
		}
		seen[r.Note] = true
		out = append(out, r.Note)
	}
	return out
}

func printNotes(p interface{ Printf(string, ...any) }, notes []string) {
	for _, note := range notes {
		p.Printf("\nnote: %s\n", note)
	}
}
