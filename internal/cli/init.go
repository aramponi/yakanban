package cli

import (
	"bufio"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
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
		settings  []string
		branching string
	)
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Set up a board and write .yakanban.yml",
		Long: `Provision the backend board and write the local descriptor.

With --project, an existing GitHub Project v2 is adopted as-is: its columns
stay untouched and yakanban only adds the custom fields it needs (Priority,
Class, Claim, Blocked, Depends On...). Without it, a new project is created,
linked to the repository and given the default columns.

Other providers accept their settings through --set key=value, for example:
  yakanban init --provider gitlab --set project=group/repo --branching trunk-based
  yakanban init --provider gitlab --set project=group/repo --set board_id=12 --set host=gitlab.example.com --branching trunk-based

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

			autoRepository := owner == "" && repo == "" && len(settings) == 0
			detected := detectRepo(e.workDir())
			if owner == "" {
				owner = detected.owner
			}
			if repo == "" {
				repo = detected.repo
			}
			if (owner == "" || repo == "") && len(settings) == 0 {
				return fmt.Errorf("%w: could not detect the repository; pass --owner and --repo or provider --set options", core.ErrInvalidInput)
			}
			if name == "" {
				name = repo
				if name == "" {
					name = filepath.Base(e.workDir())
				}
			}

			cfg := config.Default(name, provider)
			model, err := resolveBranchingModel(cmd, branching)
			if err != nil {
				return err
			}
			cfg.Branching = config.Branching{}
			if preset, ok := config.FindPreset(model); ok {
				preset.Apply(&cfg.Branching)
			} else {
				cfg.Branching.Model = model
			}
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

			if autoRepository && detected.host != "" {
				cfg.Providers[provider]["host"] = detected.host
			}
			providerOptions := map[string]string{}
			for _, setting := range settings {
				key, value, ok := strings.Cut(setting, "=")
				if !ok || strings.TrimSpace(key) == "" {
					return fmt.Errorf("%w: --set expects key=value", core.ErrInvalidInput)
				}
				key = strings.TrimSpace(key)
				cfg.Providers[provider][key] = value
				providerOptions[key] = value
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
				Options:    providerOptions,
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
			if board.Capabilities != nil && !board.Capabilities.Has(core.CapClaims) {
				for i := range cfg.Statuses {
					cfg.Statuses[i].RequireClaim = false
				}
			}
			if len(board.Priorities) > 0 {
				cfg.Priorities = board.Priorities
			}
			if len(board.Classes) > 0 {
				cfg.Classes = adoptClasses(cfg.Classes, board.Classes)
			}
			cfg.Defaults.Status = cfg.Statuses[0].Name
			cfg.Defaults.Review = adoptReview(cfg.Defaults.Review, cfg.Statuses)
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
			p.Printf("%s %s (%s → %s)\n", p.Dim("branching"), cfg.Branching.Model,
				cfg.Branching.Base, cfg.Branching.Integration)
			p.Printf("%s %s\n", p.Dim("config "), target)
			p.Printf("\nNext: %s\n", p.Bold("yakanban create \"My first task\""))
			return nil
		},
	}
	fl := cmd.Flags()
	fl.StringArrayVar(&settings, "set", nil, "provider setting as key=value (repeatable; never store credentials)")
	fl.StringVar(&provider, "provider", "github", "backend provider ("+strings.Join(registry.Names(), ", ")+")")
	fl.StringVar(&owner, "owner", "", "repository owner or namespace (default: from the git remote)")
	fl.StringVar(&repo, "repo", "", "repository name (default: from the git remote)")
	fl.IntVar(&project, "project", 0, "adopt this existing Projects v2 number instead of creating one")
	fl.StringVar(&name, "name", "", "board name (default: the repository name)")
	fl.StringSliceVar(&statuses, "statuses", nil, "comma-separated column names (only applied to a new project)")
	fl.StringArrayVar(&wipLimits, "wip-limit", nil, "WIP limit per column, as status:N (repeatable)")
	fl.StringVar(&branching, "branching", "", "branching model ("+strings.Join(config.ModelNames(), ", ")+"); asked interactively when omitted")
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
	for _, s := range live {
		if l, ok := byName[strings.ToLower(s.Name)]; ok {
			l.Name = s.Name
			out = append(out, l)
			continue
		}
		out = append(out, s)
	}
	// A board must have endpoints: without an initial column nothing ever
	// stamps Started, and without a terminal one nothing stamps Completed.
	// Adoption can drop them — a project with no Backlog column loses the
	// column the descriptor had marked as intake.
	if !anyStatus(out, func(s core.Status) bool { return s.Initial }) {
		out[0].Initial = true
	}
	if !anyStatus(out, func(s core.Status) bool { return s.Terminal }) {
		out[len(out)-1].Terminal = true
	}
	return out
}

func anyStatus(statuses []core.Status, pred func(core.Status) bool) bool {
	for _, s := range statuses {
		if pred(s) {
			return true
		}
	}
	return false
}

// adoptReview keeps the handoff column only if the adopted board actually has
// it. Leaving it pointing at a column the project does not have would make
// `handoff` fail with a confusing message rather than a clear one.
func adoptReview(configured string, statuses []core.Status) string {
	for _, s := range statuses {
		if strings.EqualFold(s.Name, configured) {
			return s.Name
		}
	}
	return ""
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

// resolveBranchingModel takes the model from the flag, asks for it when there
// is somebody to ask, and otherwise picks the default. A piped or CI run must
// never block on a prompt.
func resolveBranchingModel(cmd *cobra.Command, flag string) (string, error) {
	if flag != "" {
		if _, ok := config.FindPreset(flag); !ok && !strings.EqualFold(flag, config.ModelCustom) {
			return "", &core.InvalidValueError{Field: "branching model", Value: flag, Allowed: config.ModelNames()}
		}
		return flag, nil
	}
	if !interactive() {
		return config.ModelTrunkBased, nil
	}
	out := cmd.OutOrStdout()
	_, _ = fmt.Fprintln(out, "\nHow does this repository branch?")
	presets := config.Presets()
	for i, preset := range presets {
		marker := " "
		if preset.Name == config.ModelTrunkBased {
			marker = "*"
		}
		_, _ = fmt.Fprintf(out, "  %s %d) %-12s %s\n", marker, i+1, preset.Name, preset.Description)
	}
	_, _ = fmt.Fprintf(out, "\nChoice [1]: ")

	reader := bufio.NewReader(cmd.InOrStdin())
	line, err := reader.ReadString('\n')
	if err != nil && strings.TrimSpace(line) == "" {
		return config.ModelTrunkBased, nil
	}
	answer := strings.TrimSpace(line)
	if answer == "" {
		return config.ModelTrunkBased, nil
	}
	if n, err := strconv.Atoi(answer); err == nil {
		if n < 1 || n > len(presets) {
			return "", fmt.Errorf("%w: %d is not one of the %d models offered", core.ErrInvalidInput, n, len(presets))
		}
		return presets[n-1].Name, nil
	}
	if _, ok := config.FindPreset(answer); !ok && !strings.EqualFold(answer, config.ModelCustom) {
		return "", &core.InvalidValueError{Field: "branching model", Value: answer, Allowed: config.ModelNames()}
	}
	return answer, nil
}

// interactive reports whether there is a human on both ends of the pipe.
func interactive() bool {
	for _, f := range []*os.File{os.Stdin, os.Stdout} {
		st, err := f.Stat()
		if err != nil || st.Mode()&os.ModeCharDevice == 0 {
			return false
		}
	}
	return true
}

type repoRef struct{ host, owner, repo string }

var scpRemotePattern = regexp.MustCompile(`^(?:[^@/]+@)?([^/:]+):(.+)$`)

func parseRemote(raw string) repoRef {
	var host, path string
	if strings.Contains(raw, "://") {
		u, err := url.Parse(raw)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http" && u.Scheme != "ssh") || u.Host == "" {
			return repoRef{}
		}
		host, path = u.Host, strings.TrimPrefix(u.Path, "/")
	} else {
		m := scpRemotePattern.FindStringSubmatch(raw)
		if len(m) != 3 {
			return repoRef{}
		}
		host, path = m[1], m[2]
	}
	path = strings.TrimSuffix(path, ".git")
	index := strings.LastIndex(path, "/")
	if index <= 0 || index == len(path)-1 {
		return repoRef{}
	}
	return repoRef{host: host, owner: path[:index], repo: path[index+1:]}
}

// detectRepo accepts HTTPS, SSH URLs and scp-style remotes on any forge,
// including nested namespaces. Provider settings still decide the API host.
func detectRepo(dir string) repoRef {
	cmd := exec.Command("git", "remote", "get-url", "origin")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return repoRef{}
	}
	return parseRemote(strings.TrimSpace(string(out)))
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

// adoptClasses refreshes vocabulary without discarding local WIP policy.
func adoptClasses(local, live []core.Class) []core.Class {
	byName := make(map[string]core.Class, len(local))
	for _, class := range local {
		byName[strings.ToLower(class.Name)] = class
	}
	out := make([]core.Class, 0, len(live))
	for _, class := range live {
		if existing, ok := byName[strings.ToLower(class.Name)]; ok {
			existing.Name = class.Name
			out = append(out, existing)
		} else {
			out = append(out, class)
		}
	}
	return out
}
