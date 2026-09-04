package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/cobra"

	"github.com/aramponi/yakanban/internal/cache"
	"github.com/aramponi/yakanban/internal/config"
	"github.com/aramponi/yakanban/internal/core"
	"github.com/aramponi/yakanban/internal/registry"
	"github.com/aramponi/yakanban/internal/version"
)

func newInitCommand(e *env) *cobra.Command {
	var (
		provider  string
		owner     string
		repo      string
		project   int
		name      string
		statuses  []string
		force     bool
		wipLimits []string
	)
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Set up a board and write .yakanban.yml",
		Long: `Provision the backend board and write the local descriptor.

With --project, an existing GitHub Project v2 is adopted as-is: its columns
stay untouched and yakanban only adds the custom fields it needs (Priority,
Class, Claim, Blocked, Depends On...). Without it, a new project is created,
linked to the repository and given the default columns.

Run it again with --force after changing the descriptor to re-apply it.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			p := e.Printer()
			target := e.configPath
			if target == "" {
				target = filepath.Join(e.workDir(), config.FileName)
			}
			if _, err := os.Stat(target); err == nil && !force {
				return fmt.Errorf("%w: %s already exists (pass --force to re-apply it)", core.ErrInvalidInput, target)
			}

			detected := detectRepo(e.workDir())
			if owner == "" {
				owner = detected.owner
			}
			if repo == "" {
				repo = detected.repo
			}
			if owner == "" || repo == "" {
				return fmt.Errorf("%w: could not detect the GitHub repository; pass --owner and --repo", core.ErrInvalidInput)
			}
			if name == "" {
				name = repo
			}

			cfg := config.Default(name, provider)
			if len(statuses) > 0 {
				cfg.Statuses = statusesFromNames(statuses)
				cfg.Defaults.Status = cfg.Statuses[0].Name
			}
			if err := applyWIPLimits(cfg, wipLimits); err != nil {
				return err
			}
			cfg.Providers[provider] = map[string]any{
				"owner":          owner,
				"repo":           repo,
				"project_number": project,
			}

			// The cache is deliberately disabled here: init must see the
			// project exactly as it is right now.
			raw, err := registry.Open(cfg, cache.New("", 0, false), version.UserAgent())
			if err != nil {
				return err
			}
			bootstrapper, ok := raw.(core.Bootstrapper)
			if !ok {
				return fmt.Errorf("%w: provider %s cannot provision a board", core.ErrUnsupported, provider)
			}
			board, err := bootstrapper.Bootstrap(cmd.Context(), core.BootstrapOptions{
				Name:       name,
				Statuses:   cfg.Statuses,
				Priorities: cfg.Priorities,
				Classes:    cfg.Classes,
			})
			if err != nil {
				return err
			}
			if w, ok := raw.(core.SettingsWriter); ok {
				if settings := w.ConfigSettings(); settings != nil {
					cfg.Providers[provider] = settings
				}
			}
			cfg.Statuses = adoptStatuses(cfg.Statuses, board.Statuses)
			cfg.Defaults.Status = cfg.Statuses[0].Name
			if err := cfg.Save(target); err != nil {
				return err
			}
			if err := ignoreCacheDir(filepath.Dir(target)); err != nil {
				p.Warnf("could not update .gitignore: %v", err)
			}

			if e.format() == "json" {
				return p.JSON(map[string]any{
					"config":   target,
					"board":    board.Name,
					"provider": provider,
					"url":      board.URL,
					"statuses": board.StatusNames(),
					"settings": cfg.Providers[provider],
				})
			}
			p.Printf("%s %s\n", p.Bold("board ready:"), board.Name)
			if board.URL != "" {
				p.Printf("%s %s\n", p.Dim("project"), board.URL)
			}
			p.Printf("%s %s\n", p.Dim("columns"), strings.Join(board.StatusNames(), " → "))
			p.Printf("%s %s\n", p.Dim("config "), target)
			p.Printf("\nNext: %s\n", p.Bold("yakanban create \"My first task\""))
			return nil
		},
	}
	fl := cmd.Flags()
	fl.StringVar(&provider, "provider", "github", "backend provider ("+strings.Join(registry.Names(), ", ")+")")
	fl.StringVar(&owner, "owner", "", "GitHub user or organization (default: from the git remote)")
	fl.StringVar(&repo, "repo", "", "GitHub repository (default: from the git remote)")
	fl.IntVar(&project, "project", 0, "adopt this existing Projects v2 number instead of creating one")
	fl.StringVar(&name, "name", "", "board name (default: the repository name)")
	fl.StringSliceVar(&statuses, "statuses", nil, "comma-separated column names (only applied to a new project)")
	fl.StringArrayVar(&wipLimits, "wip-limit", nil, "WIP limit per column, as status:N (repeatable)")
	fl.BoolVar(&force, "force", false, "overwrite an existing .yakanban.yml")
	return cmd
}

// statusesFromNames builds a column list, marking the first as the intake
// column and the last as terminal, which is what every board does anyway.
func statusesFromNames(names []string) []core.Status {
	out := make([]core.Status, 0, len(names))
	for i, n := range names {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		s := core.Status{Name: n}
		if i == 0 {
			s.Initial = true
		}
		if i == len(names)-1 {
			s.Terminal = true
		}
		out = append(out, s)
	}
	return out
}

// adoptStatuses keeps the local semantics (terminal, WIP limits, claims) but
// takes the column names and their order from the backend, which owns them.
func adoptStatuses(local, live []core.Status) []core.Status {
	if len(live) == 0 {
		return local
	}
	byName := make(map[string]core.Status, len(local))
	for _, s := range local {
		byName[strings.ToLower(s.Name)] = s
	}
	out := make([]core.Status, 0, len(live))
	matched := false
	for _, s := range live {
		if l, ok := byName[strings.ToLower(s.Name)]; ok {
			l.Name = s.Name
			out = append(out, l)
			matched = true
			continue
		}
		out = append(out, s)
	}
	if !matched {
		// A pre-existing project with entirely different columns: assume the
		// usual shape rather than leaving the board without endpoints.
		out[0].Initial = true
		out[len(out)-1].Terminal = true
	}
	return out
}

func applyWIPLimits(cfg *config.Config, specs []string) error {
	for _, spec := range specs {
		name, value, ok := strings.Cut(spec, ":")
		if !ok {
			return fmt.Errorf("%w: --wip-limit expects status:N, got %q", core.ErrInvalidInput, spec)
		}
		limit := 0
		if _, err := fmt.Sscanf(value, "%d", &limit); err != nil || limit < 0 {
			return fmt.Errorf("%w: %q is not a WIP limit", core.ErrInvalidInput, value)
		}
		found := false
		for i := range cfg.Statuses {
			if strings.EqualFold(cfg.Statuses[i].Name, name) {
				cfg.Statuses[i].WIPLimit = limit
				found = true
			}
		}
		if !found {
			return fmt.Errorf("%w: no column named %q", core.ErrInvalidInput, name)
		}
	}
	return nil
}

type repoRef struct{ owner, repo string }

var remotePattern = regexp.MustCompile(`(?:github\.com[:/])([^/]+)/([^/]+?)(?:\.git)?$`)

// detectRepo reads the origin remote, so `yakanban init` needs no flags in a
// checked-out repository.
func detectRepo(dir string) repoRef {
	cmd := exec.Command("git", "remote", "get-url", "origin")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return repoRef{}
	}
	m := remotePattern.FindStringSubmatch(strings.TrimSpace(string(out)))
	if len(m) != 3 {
		return repoRef{}
	}
	return repoRef{owner: m[1], repo: m[2]}
}

// ignoreCacheDir keeps the local cache out of version control.
func ignoreCacheDir(root string) error {
	path := filepath.Join(root, ".gitignore")
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	entry := config.DirName + "/"
	for _, line := range strings.Split(string(existing), "\n") {
		if strings.TrimSpace(line) == entry {
			return nil
		}
	}
	content := string(existing)
	if content != "" && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	content += entry + "\n"
	return os.WriteFile(path, []byte(content), 0o644)
}
