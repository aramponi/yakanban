package gitlab

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/aramponi/yakanban/internal/core"
)

type client struct {
	base, token, agent string
	http               *http.Client
}

func newClient(host, token, agent string) *client {
	return &client{base: "https://" + host + "/api/v4", token: token, agent: agent, http: &http.Client{
		Timeout: 30 * time.Second,
		// A configured host's private token must never follow a redirect.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

type apiError struct {
	status                int
	method, path, message string
}

func (e *apiError) Error() string {
	return fmt.Sprintf("GitLab %s %s: HTTP %d: %s", e.method, e.path, e.status, e.message)
}
func (e *apiError) Unwrap() error {
	switch e.status {
	case 401, 403:
		return core.ErrAuth
	case 404:
		return core.ErrNotFound
	case 400, 409, 422:
		return core.ErrInvalidInput
	}
	return nil
}

func (c *client) request(ctx context.Context, method, path string, body, result any) (http.Header, error) {
	var input io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		input = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, input)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("User-Agent", c.agent)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 16<<20))
	if err != nil {
		return nil, err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, &apiError{res.StatusCode, method, path, string(raw)}
	}
	if result != nil {
		if len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return nil, fmt.Errorf("GitLab %s %s returned an empty success payload", method, path)
		}
		if err := json.Unmarshal(raw, result); err != nil {
			return nil, fmt.Errorf("decode GitLab %s %s: %w", method, path, err)
		}
	}
	return res.Header, nil
}

func pages[T any](ctx context.Context, c *client, path string) ([]T, error) {
	var all []T
	page := 1
	for {
		u, err := url.Parse(path)
		if err != nil {
			return nil, err
		}
		q := u.Query()
		q.Set("per_page", "100")
		q.Set("page", strconv.Itoa(page))
		u.RawQuery = q.Encode()
		var batch []T
		headers, err := c.request(ctx, "GET", u.String(), nil, &batch)
		if err != nil {
			return nil, err
		}
		all = append(all, batch...)
		next := headers.Get("X-Next-Page")
		if next == "" {
			return all, nil
		}
		n, err := strconv.Atoi(next)
		if err != nil || n <= page {
			return nil, fmt.Errorf("GitLab returned invalid next page %q", next)
		}
		page = n
	}
}
