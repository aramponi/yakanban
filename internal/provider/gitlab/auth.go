package gitlab

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/aramponi/yakanban/internal/core"
)

type credentialCommand func(args []string, input string) ([]byte, error)

// ResolveToken uses GitLab's credential helper so keyring credentials and OAuth
// expiry are respected. Only glab renews/persists its own login; yakanban never
// stores access or refresh tokens. Explicit environment tokens stay explicit.
func ResolveToken(host string) (string, error) {
	return resolveToken(host, func(args []string, input string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "glab", args...)
		cmd.Stdin = strings.NewReader(input)
		return cmd.Output()
	}, time.Now())
}

func resolveToken(host string, run credentialCommand, now time.Time) (string, error) {
	if token := strings.TrimSpace(os.Getenv("GITLAB_TOKEN")); token != "" {
		return token, nil
	}
	if strings.ContainsAny(host, "\r\n") {
		return "", fmt.Errorf("%w: invalid GitLab hostname", core.ErrInvalidInput)
	}
	failure := func() error {
		return fmt.Errorf("%w: set GITLAB_TOKEN or run glab auth login --hostname %s", core.ErrAuth, host)
	}
	read := func() (string, bool, error) {
		raw, err := run([]string{"auth", "git-credential", "get"}, "protocol=https\nhost="+host+"\n\n")
		if err != nil {
			return "", false, failure()
		}
		fields := map[string]string{}
		for _, line := range strings.Split(string(raw), "\n") {
			key, value, ok := strings.Cut(line, "=")
			if ok {
				fields[key] = strings.TrimSuffix(value, "\r")
			}
		}
		token := fields["password"]
		if token == "" {
			return "", false, failure()
		}
		if expiry := fields["password_expiry_utc"]; expiry != "" {
			seconds, err := strconv.ParseInt(expiry, 10, 64)
			if err != nil {
				return "", false, failure()
			}
			return token, !time.Unix(seconds, 0).After(now.Add(30 * time.Second)), nil
		}
		return token, false, nil
	}
	token, expired, err := read()
	if err != nil {
		return "", err
	}
	if !expired {
		return token, nil
	}
	// auth status builds glab's authenticated client, which refreshes OAuth.
	// Its output is discarded; never use --show-token or log helper output.
	if _, err := run([]string{"auth", "status", "--hostname", host}, ""); err != nil {
		return "", failure()
	}
	token, expired, err = read()
	if err != nil {
		return "", err
	}
	if expired {
		return "", failure()
	}
	return token, nil
}
