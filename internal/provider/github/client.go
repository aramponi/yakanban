package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/aramponi/yakanban/internal/core"
)

const defaultHost = "github.com"

var errAuth = core.ErrAuth

// client speaks both GitHub APIs: REST for issue content (it takes label and
// assignee names directly, where GraphQL would need node IDs) and GraphQL for
// everything Projects v2, which has no REST surface.
type client struct {
	http      *http.Client
	token     string
	restBase  string
	graphURL  string
	userAgent string
}

func newClient(host, token, userAgent string) *client {
	rest, graph := endpoints(host)
	return &client{
		http:      &http.Client{Timeout: 30 * time.Second},
		token:     token,
		restBase:  rest,
		graphURL:  graph,
		userAgent: userAgent,
	}
}

// endpoints derives the API URLs, supporting GitHub Enterprise Server hosts.
func endpoints(host string) (rest, graph string) {
	host = strings.TrimSpace(host)
	if host == "" || host == defaultHost {
		return "https://api.github.com", "https://api.github.com/graphql"
	}
	host = strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(host, "https://"), "http://"), "/")
	return "https://" + host + "/api/v3", "https://" + host + "/api/graphql"
}

// graphErr is one entry of a GraphQL errors array.
type graphErr struct {
	Type    string `json:"type"`
	Message string `json:"message"`
	Path    []any  `json:"path"`
}

type graphResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []graphErr      `json:"errors"`
}

// graphql runs a query and decodes data into out.
func (c *client) graphql(ctx context.Context, query string, vars map[string]any, out any) error {
	payload, err := json.Marshal(map[string]any{"query": query, "variables": vars})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.graphURL, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	c.setHeaders(req)
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("github graphql: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return c.authError(resp, body)
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("github graphql: %s: %s", resp.Status, truncate(string(body), 400))
	}
	var gr graphResponse
	if err := json.Unmarshal(body, &gr); err != nil {
		return fmt.Errorf("github graphql: cannot decode response: %w", err)
	}
	// GraphQL answers partially: a query that asks for a project under both an
	// organization and a user gets data for the one that exists and a
	// NOT_FOUND error for the other. Decoding before reporting the error is
	// what lets a caller keep the half that worked.
	if out != nil && len(gr.Data) > 0 && string(gr.Data) != "null" {
		if err := json.Unmarshal(gr.Data, out); err != nil {
			return fmt.Errorf("github graphql: cannot decode data: %w", err)
		}
	}
	if len(gr.Errors) > 0 {
		msgs := make([]string, 0, len(gr.Errors))
		notFound := true
		for _, e := range gr.Errors {
			msgs = append(msgs, e.Message)
			if !strings.EqualFold(e.Type, "NOT_FOUND") {
				notFound = false
			}
		}
		joined := strings.Join(msgs, "; ")
		if notFound {
			return fmt.Errorf("%w: %s", core.ErrNotFound, joined)
		}
		if strings.Contains(strings.ToLower(joined), "scope") {
			return fmt.Errorf("%w: %s (Projects v2 needs the `project` scope: run `gh auth refresh -s project`)", errAuth, joined)
		}
		return fmt.Errorf("github graphql: %s", joined)
	}
	return nil
}

// rest runs a REST call. body may be nil; out may be nil.
func (c *client) rest(ctx context.Context, method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.restBase+path, reader)
	if err != nil {
		return err
	}
	c.setHeaders(req)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("github rest: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return fmt.Errorf("%w: %s %s", core.ErrNotFound, method, path)
	case resp.StatusCode == http.StatusUnauthorized, resp.StatusCode == http.StatusForbidden:
		return c.authError(resp, raw)
	case resp.StatusCode >= 400:
		return fmt.Errorf("github rest %s %s: %s: %s", method, path, resp.Status, truncate(string(raw), 400))
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(raw, out)
}

func (c *client) setHeaders(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", c.userAgent)
}

// authError turns a 401/403 into an actionable message, telling rate limiting
// apart from a genuinely missing permission.
func (c *client) authError(resp *http.Response, body []byte) error {
	if resp.Header.Get("X-RateLimit-Remaining") == "0" {
		reset := resp.Header.Get("X-RateLimit-Reset")
		return fmt.Errorf("github: rate limit exhausted (resets at %s); yakanban caches reads, try again shortly", reset)
	}
	var parsed struct {
		Message string `json:"message"`
	}
	_ = json.Unmarshal(body, &parsed)
	msg := parsed.Message
	if msg == "" {
		msg = resp.Status
	}
	return fmt.Errorf("%w: %s (check `gh auth status`; Projects v2 needs the `project` scope: `gh auth refresh -s project`)", errAuth, msg)
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// isNotFound reports whether err is a GitHub not-found in any of its shapes.
func isNotFound(err error) bool { return errors.Is(err, core.ErrNotFound) }
