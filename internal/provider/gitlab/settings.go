// Package gitlab implements project issue boards over the GitLab REST API.
package gitlab

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/aramponi/yakanban/internal/core"
)

const ProviderName = "gitlab"

type Settings struct {
	Host    string
	Project string
	BoardID int
}

func ParseSettings(raw map[string]any) (Settings, error) {
	s := Settings{Host: "gitlab.com"}
	if host, _ := raw["host"].(string); host != "" {
		s.Host = strings.TrimPrefix(strings.TrimSpace(host), "https://")
	}
	s.Project = strings.TrimSpace(fmt.Sprint(raw["project"]))
	if raw["project"] == nil {
		s.Project = ""
	}
	if s.Project == "" {
		owner, _ := raw["owner"].(string)
		repo, _ := raw["repo"].(string)
		if owner != "" && repo != "" {
			s.Project = owner + "/" + repo
		}
	}
	if value := raw["board_id"]; value != nil {
		n, err := strconv.Atoi(fmt.Sprint(value))
		if err != nil || n <= 0 {
			return s, fmt.Errorf("%w: providers.gitlab.board_id must be a positive integer", core.ErrInvalidInput)
		}
		s.BoardID = n
	}
	for _, secret := range []string{"token", "access_token", "private_token"} {
		if raw[secret] != nil {
			return s, fmt.Errorf("%w: do not store tokens in the descriptor; use GITLAB_TOKEN or glab auth login", core.ErrInvalidInput)
		}
	}
	return s, s.Validate()
}

func (s Settings) Validate() error {
	if s.Project == "" || strings.HasPrefix(s.Project, "/") || strings.HasSuffix(s.Project, "/") || strings.ContainsAny(s.Project, "?#\r\n") {
		return fmt.Errorf("%w: set providers.gitlab.project to a project ID or namespace/project path", core.ErrNotConfigured)
	}
	u, err := url.Parse("https://" + s.Host)
	if err != nil || u.Hostname() == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("%w: GitLab host must be a hostname, optionally with a port", core.ErrInvalidInput)
	}
	return nil
}

func (s Settings) ToMap() map[string]any {
	return map[string]any{"host": s.Host, "project": s.Project, "board_id": s.BoardID}
}
