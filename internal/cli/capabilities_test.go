package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/aramponi/yakanban/internal/cache"
	"github.com/aramponi/yakanban/internal/config"
	"github.com/aramponi/yakanban/internal/core"
	"github.com/aramponi/yakanban/internal/output"
)

func TestConfigShowsResolvedCapabilities(t *testing.T) {
	for _, format := range []output.Format{output.FormatHuman, output.FormatJSON} {
		t.Run(string(format), func(t *testing.T) {
			t.Setenv("GH_TOKEN", "test-token-never-sent")
			cfg := config.Default("capabilities", "github")
			cfg.Providers["github"] = map[string]any{"owner": "test", "repo": "test", "project_number": 1}
			path := t.TempDir() + "/.yakanban.yml"
			if err := cfg.Save(path); err != nil {
				t.Fatal(err)
			}
			loaded, err := config.LoadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			store := cache.New(loaded.CacheDir(), loaded.Cache.TTL.Duration(), true)
			board := core.BoardInfo{Name: "live", Statuses: []core.Status{{Name: "Inbox"}}, Capabilities: &core.CapabilitySet{
				Supported: core.CapClaims, Reasons: map[core.Capability]string{core.CapDependencies: "Premium required on this instance"},
			}}
			if err := store.Put("github:board:v2", board); err != nil {
				t.Fatal(err)
			}
			var stdout, stderr bytes.Buffer
			e := &env{configPath: path, asJSON: format == output.FormatJSON, printer: output.New(&stdout, &stderr, format, false)}
			cmd := newConfigCommand(e)
			cmd.SetContext(context.Background())
			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(stdout.String(), "Premium required on this instance") {
				t.Fatal(stdout.String())
			}
			if format == output.FormatJSON {
				var got struct {
					Capabilities map[string]core.CapabilityStatus
					Statuses     []string
				}
				if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
					t.Fatal(err)
				}
				if !got.Capabilities["claims"].Supported || got.Capabilities["dependencies"].Supported || got.Statuses[0] != "Inbox" {
					t.Fatalf("got %+v", got)
				}
			}
		})
	}
}
