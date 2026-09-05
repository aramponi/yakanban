package gitlab

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/aramponi/yakanban/internal/core"
)

func TestCredentialHelperReadsSelectedHostWithoutNetwork(t *testing.T) {
	t.Setenv("GITLAB_TOKEN", "")
	calls := 0
	token, err := resolveToken("gitlab.example.com", func(args []string, input string) ([]byte, error) {
		calls++
		if strings.Join(args, " ") != "auth git-credential get" || input != "protocol=https\nhost=gitlab.example.com\n\n" {
			t.Fatalf("credential request: %v %q", args, input)
		}
		return []byte("username=oauth2\npassword=test-access\n"), nil
	}, time.Now())
	if err != nil || token != "test-access" || calls != 1 {
		t.Fatalf("credential lookup: calls=%d, %v", calls, err)
	}
}

func TestExpiredOAuthIsRefreshedByGlab(t *testing.T) {
	t.Setenv("GITLAB_TOKEN", "")
	now := time.Unix(2000, 0)
	calls := 0
	token, err := resolveToken("gitlab.example.com", func(args []string, input string) ([]byte, error) {
		calls++
		switch calls {
		case 1:
			return []byte("password=expired-access\npassword_expiry_utc=1900\n"), nil
		case 2:
			if strings.Join(args, " ") != "auth status --hostname gitlab.example.com" || input != "" {
				t.Fatalf("refresh request: %v", args)
			}
			return nil, nil
		case 3:
			return []byte("password=renewed-access\npassword_expiry_utc=9000\n"), nil
		default:
			t.Fatal("unbounded refresh")
			return nil, nil
		}
	}, now)
	if err != nil || token != "renewed-access" || calls != 3 {
		t.Fatalf("refresh calls=%d, %v", calls, err)
	}
}

func TestFailedOAuthRefreshDoesNotLeakCredentialOutput(t *testing.T) {
	t.Setenv("GITLAB_TOKEN", "")
	calls := 0
	_, err := resolveToken("gitlab.example.com", func(args []string, input string) ([]byte, error) {
		calls++
		if calls == 1 {
			return []byte("password=secret-access\npassword_expiry_utc=1\n"), nil
		}
		return []byte("secret-output"), fmt.Errorf("secret-error")
	}, time.Now())
	if !errors.Is(err, core.ErrAuth) || strings.Contains(err.Error(), "secret-") {
		t.Fatalf("unsafe error: %v", err)
	}
}

func TestEnvironmentTokenDoesNotRefreshAnotherLogin(t *testing.T) {
	t.Setenv("GITLAB_TOKEN", "explicit-access")
	token, err := resolveToken("gitlab.example.com", func([]string, string) ([]byte, error) { t.Fatal("explicit token caused a glab call"); return nil, nil }, time.Now())
	if err != nil || token != "explicit-access" {
		t.Fatalf("environment token: %v", err)
	}
}
