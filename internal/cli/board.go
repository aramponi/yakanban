package cli

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/aramponi/yakanban/internal/core"
	"github.com/aramponi/yakanban/internal/registry"
	"github.com/aramponi/yakanban/internal/version"
)

func newBoardCommand(e *env) *cobra.Command {
	return &cobra.Command{
		Use:   "board",
		Short: "Show the board overview",
		Long:  "Counts per column, WIP pressure, blocked and overdue work.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := e.open(cmd.Context())
			if err != nil {
				return err
			}
			summary, err := s.service.Summary(cmd.Context(), core.Filter{})
			if err != nil {
				return err
			}
			return e.Printer().Summary(summary)
		},
	}
}

func newSyncCommand(e *env) *cobra.Command {
	return &cobra.Command{
		Use:   "sync",
		Short: "Refresh the local read cache from the backend",
		Long: `Drop the cached reads and fetch the board again.

Writes never go through the cache, so this only matters after someone else
has changed something in the web UI and you want to see it immediately.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			e.refresh = true
			s, err := e.open(cmd.Context())
			if err != nil {
				return err
			}
			tasks, err := s.service.List(cmd.Context(), core.Filter{}, "id", false)
			if err != nil {
				return err
			}
			e.Printer().Printf("synced %d tasks from %s\n", len(tasks), s.cfg.Provider)
			return nil
		},
	}
}

func newConfigCommand(e *env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Show the resolved board configuration",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := e.loadConfig()
			if err != nil {
				return err
			}
			p := e.Printer()
			type view struct {
				Path         string         `json:"path"`
				Provider     string         `json:"provider"`
				Board        string         `json:"board"`
				Statuses     []string       `json:"statuses"`
				Priorities   []string       `json:"priorities"`
				Classes      []string       `json:"classes"`
				ClaimTimeout string         `json:"claim_timeout"`
				Branching    any            `json:"branching"`
				CacheTTL     string         `json:"cache_ttl"`
				CacheDir     string         `json:"cache_dir"`
				Settings     map[string]any `json:"settings"`
				Version      string         `json:"yakanban_version"`
			}
			policy, err := cfg.Branching.Policy()
			if err != nil {
				return err
			}
			templates := cfg.Branching.EffectiveTemplates()
			board := cfg.BoardInfo()
			v := view{
				Path:         cfg.Path(),
				Provider:     cfg.Provider,
				Board:        cfg.Board.Name,
				Statuses:     board.StatusNames(),
				Priorities:   cfg.Priorities,
				Classes:      board.ClassNames(),
				ClaimTimeout: cfg.ClaimTimeout.Duration().String(),
				Branching:    policy,
				CacheTTL:     cfg.Cache.TTL.Duration().String(),
				CacheDir:     cfg.CacheDir(),
				Settings:     cfg.Settings(cfg.Provider),
				Version:      version.String(),
			}
			if e.format() == "json" {
				return p.JSON(v)
			}
			p.Printf("%s %s\n", p.Bold(v.Board), p.Dim("("+v.Provider+")"))
			p.Printf("%s %s\n", p.Dim("config    "), v.Path)
			p.Printf("%s %s\n", p.Dim("columns   "), strings.Join(v.Statuses, " → "))
			p.Printf("%s %s\n", p.Dim("priorities"), strings.Join(v.Priorities, ", "))
			p.Printf("%s %s\n", p.Dim("classes   "), strings.Join(v.Classes, ", "))
			p.Printf("%s %s\n", p.Dim("claims    "), "timeout "+v.ClaimTimeout)
			p.Printf("%s %s\n", p.Dim("cache     "), v.CacheTTL+" in "+v.CacheDir)
			p.Printf("%s %s (%s → %s)\n", p.Dim("branching "), orNone(policy.Model), orNone(policy.Base), orNone(policy.Integration))
			p.Printf("%s %s\n", p.Dim("branch    "), orNone(templates.Branch))
			p.Printf("%s %s\n", p.Dim("worktree  "), orNone(templates.Worktree))
			for _, rule := range policy.Rules {
				p.Printf("%s %s\n", p.Dim("rule      "), describeRule(rule))
			}
			for k, val := range v.Settings {
				p.Printf("%s %s=%v\n", p.Dim("provider  "), k, val)
			}
			p.Printf("%s %s\n", p.Dim("providers "), strings.Join(registry.Names(), ", "))
			return nil
		},
	}
	return cmd
}

// describeRule renders a branch-type rule the way it reads in the descriptor.
func describeRule(r core.BranchRule) string {
	var conditions []string
	for label, value := range map[string]string{"tag": r.Tag, "priority": r.Priority, "class": r.Class} {
		if value != "" {
			conditions = append(conditions, label+"="+value)
		}
	}
	sort.Strings(conditions)
	when := "anything"
	if len(conditions) > 0 {
		when = strings.Join(conditions, " ")
	}
	out := when + " → " + r.Type
	if r.Base != "" {
		out += " (from " + r.Base + ")"
	}
	return out
}

func orNone(v string) string {
	if strings.TrimSpace(v) == "" {
		return "—"
	}
	return v
}

// agentAdjectives and agentNouns generate short, memorable claim names in the
// spirit of kanban-md's `agent-name`.
var agentAdjectives = []string{
	"amber", "brisk", "calm", "crimson", "dusty", "eager", "frost", "gentle",
	"hazy", "iron", "jade", "keen", "lucid", "misty", "noble", "olive",
	"prime", "quiet", "rapid", "silver", "tidal", "umber", "vivid", "warm",
}

var agentNouns = []string{
	"alder", "basalt", "cedar", "delta", "ember", "fjord", "grove", "harbor",
	"inlet", "juniper", "kelp", "larch", "maple", "nimbus", "onyx", "pine",
	"quartz", "ridge", "spruce", "thicket", "umbra", "vale", "willow", "zephyr",
}

func newAgentNameCommand(e *env) *cobra.Command {
	return &cobra.Command{
		Use:   "agent-name",
		Short: "Generate a unique agent name for claims",
		Long:  "Print a random two-word identifier to use with --claim.",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			name, err := randomAgentName()
			if err != nil {
				return err
			}
			e.Printer().Printf("%s\n", name)
			return nil
		},
	}
}

func randomAgentName() (string, error) {
	pick := func(list []string) (string, error) {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(list))))
		if err != nil {
			return "", err
		}
		return list[n.Int64()], nil
	}
	adjective, err := pick(agentAdjectives)
	if err != nil {
		return "", err
	}
	noun, err := pick(agentNouns)
	if err != nil {
		return "", err
	}
	suffix, err := rand.Int(rand.Reader, big.NewInt(100))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s-%s-%02d", adjective, noun, suffix.Int64()), nil
}
