// Package cli assembles the command tree.
//
// Commands stay thin: they parse flags, call the application service and hand
// the result to a printer. All the rules live in internal/app, all the API
// traffic in internal/provider.
package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/aramponi/yakanban/internal/app"
	"github.com/aramponi/yakanban/internal/cache"
	"github.com/aramponi/yakanban/internal/config"
	"github.com/aramponi/yakanban/internal/core"
	"github.com/aramponi/yakanban/internal/output"
	"github.com/aramponi/yakanban/internal/provider/cached"
	"github.com/aramponi/yakanban/internal/registry"
	"github.com/aramponi/yakanban/internal/version"
)

// env holds the global flags and lazily builds everything a command needs.
type env struct {
	dir        string
	configPath string

	asJSON    bool
	asTable   bool
	asCompact bool
	noColor   bool
	noCache   bool
	refresh   bool

	printer *output.Printer
}

// Printer returns the configured printer.
func (e *env) Printer() *output.Printer {
	if e.printer == nil {
		e.printer = output.New(os.Stdout, os.Stderr, e.format(), !e.noColor && os.Getenv("NO_COLOR") == "")
	}
	return e.printer
}

func (e *env) format() output.Format {
	switch {
	case e.asJSON:
		return output.FormatJSON
	case e.asCompact:
		return output.FormatCompact
	case e.asTable:
		return output.FormatTable
	default:
		return output.FormatHuman
	}
}

func (e *env) workDir() string {
	if e.dir != "" {
		return e.dir
	}
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return wd
}

// loadConfig reads the board descriptor.
func (e *env) loadConfig() (*config.Config, error) {
	if e.configPath != "" {
		return config.LoadFile(e.configPath)
	}
	return config.Load(e.workDir())
}

// session bundles everything a board command operates on.
type session struct {
	cfg      *config.Config
	provider *cached.Provider
	service  *app.Service
	store    *cache.Store
}

// open loads the config, builds the provider (wrapped in the read cache) and
// resolves the board vocabulary from the backend.
func (e *env) open(ctx context.Context) (*session, error) {
	cfg, err := e.loadConfig()
	if err != nil {
		return nil, err
	}
	store := cache.New(cfg.CacheDir(), cfg.Cache.TTL.Duration(), cfg.Cache.Enabled && !e.noCache)
	if e.refresh {
		if err := store.Invalidate(); err != nil {
			return nil, err
		}
	}
	raw, err := registry.Open(cfg, store, version.UserAgent())
	if err != nil {
		return nil, err
	}
	provider := cached.New(raw, store)
	live, err := provider.Board(ctx)
	if err != nil {
		return nil, err
	}
	board := mergeBoard(cfg, live)
	service := app.New(provider, board, app.Options{
		DefaultStatus:   cfg.Defaults.Status,
		DefaultPriority: cfg.Defaults.Priority,
		DefaultClass:    cfg.Defaults.Class,
		ClaimTimeout:    cfg.ClaimTimeout.Duration(),
	})
	return &session{cfg: cfg, provider: provider, service: service, store: store}, nil
}

// mergeBoard reconciles the committed vocabulary with the live one.
//
// The backend is the source of truth for which columns exist, so a column
// added in the web UI shows up immediately; the descriptor only adds the
// local semantics GitHub has no place for (which column is terminal, which
// requires a claim, WIP limits).
func mergeBoard(cfg *config.Config, live *core.BoardInfo) core.BoardInfo {
	board := cfg.BoardInfo()
	if live == nil {
		return board
	}
	board.URL = live.URL
	board.Metadata = live.Metadata
	if len(live.Priorities) > 0 {
		board.Priorities = live.Priorities
	}
	if len(live.Statuses) == 0 {
		return board
	}
	byName := make(map[string]core.Status, len(board.Statuses))
	for _, s := range board.Statuses {
		byName[strings.ToLower(s.Name)] = s
	}
	merged := make([]core.Status, 0, len(live.Statuses))
	for _, s := range live.Statuses {
		if local, ok := byName[strings.ToLower(s.Name)]; ok {
			local.Name = s.Name // the backend owns the spelling
			merged = append(merged, local)
			continue
		}
		merged = append(merged, s)
	}
	board.Statuses = merged
	return board
}

// Execute builds the command tree and runs it, mapping domain errors onto
// exit codes so scripts and agents can branch on them.
func Execute(ctx context.Context, args []string) int {
	e := &env{}
	root := newRootCommand(e)
	root.SetArgs(args)
	if err := root.ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "yakanban: "+err.Error())
		return exitCode(err)
	}
	return 0
}

// Exit codes, stable across releases.
const (
	exitOK          = 0
	exitError       = 1
	exitUsage       = 2
	exitNotFound    = 3
	exitAuth        = 4
	exitClaimed     = 5
	exitUnsupported = 6
)

func exitCode(err error) int {
	switch {
	case errors.Is(err, core.ErrNotFound):
		return exitNotFound
	case errors.Is(err, core.ErrAuth):
		return exitAuth
	case errors.Is(err, core.ErrClaimed):
		return exitClaimed
	case errors.Is(err, core.ErrUnsupported):
		return exitUnsupported
	case errors.Is(err, core.ErrInvalidInput), errors.Is(err, core.ErrNotConfigured), errors.Is(err, core.ErrUnknownStatus):
		return exitUsage
	default:
		return exitError
	}
}

func newRootCommand(e *env) *cobra.Command {
	root := &cobra.Command{
		Use:   invokedAs(),
		Short: "Kanban over your real ticket tracker",
		Long: `yakanban drives GitHub Issues and GitHub Projects v2 (and, later, Jira,
Plane or Linear) from the command line.

Developers and AI agents work here; everyone else keeps using the native web
UI of the tracker. The backend stays the single source of truth — yakanban
only caches reads locally and always writes straight through.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       version.String(),
	}
	flags := root.PersistentFlags()
	flags.StringVar(&e.dir, "dir", "", "directory to look for .yakanban.yml in (default: current directory and its parents)")
	flags.StringVar(&e.configPath, "config", "", "path to an explicit .yakanban.yml")
	flags.BoolVar(&e.asJSON, "json", false, "output as JSON")
	flags.BoolVar(&e.asTable, "table", false, "output as a table")
	flags.BoolVar(&e.asCompact, "compact", false, "compact one-line-per-record output")
	flags.BoolVar(&e.asCompact, "oneline", false, "alias for --compact")
	flags.BoolVar(&e.noColor, "no-color", false, "disable colour output")
	flags.BoolVar(&e.noCache, "no-cache", false, "bypass the local read cache for this command")
	flags.BoolVar(&e.refresh, "refresh", false, "drop the local read cache before running")

	root.AddCommand(
		newInitCommand(e),
		newListCommand(e),
		newShowCommand(e),
		newCreateCommand(e),
		newEditCommand(e),
		newMoveCommand(e),
		newDeleteCommand(e),
		newBoardCommand(e),
		newSyncCommand(e),
		newConfigCommand(e),
		newAgentNameCommand(e),
	)
	return root
}

// invokedAs makes the help text match how the binary was called, so the
// GitHub CLI extension shows `gh yakanban` rather than `gh-yakanban`.
func invokedAs() string {
	base := filepath.Base(os.Args[0])
	if strings.HasPrefix(base, "gh-") {
		return "gh " + strings.TrimPrefix(base, "gh-")
	}
	return "yakanban"
}
