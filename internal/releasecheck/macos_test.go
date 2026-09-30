package releasecheck

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestMacOSSigningHook(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("release hook runs on macOS; Unix stubs")
	}
	for _, tc := range []struct {
		name, target, snapshot, result            string
		failSign, wantError, wantSign, wantSubmit bool
	}{
		{name: "linux", target: "linux", snapshot: "false"},
		{name: "snapshot", target: "darwin", snapshot: "true"},
		{name: "accepted", target: "darwin", snapshot: "false", result: "Accepted", wantSign: true, wantSubmit: true},
		{name: "rejected", target: "darwin", snapshot: "false", result: "Invalid", wantError: true, wantSign: true, wantSubmit: true},
		{name: "timeout", target: "darwin", snapshot: "false", result: "timeout", wantError: true, wantSign: true, wantSubmit: true},
		{name: "bad certificate", target: "darwin", snapshot: "false", failSign: true, wantError: true, wantSign: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			logPath := filepath.Join(dir, "calls")
			stubs := map[string]string{
				"codesign": `echo "codesign $*" >> "$CALL_LOG"
if [ "$FAIL_SIGN" = true ]; then exit 1; fi`,
				"ditto": `echo "ditto $*" >> "$CALL_LOG"`,
				"xcrun": `echo "xcrun $*" >> "$CALL_LOG"
if [ "$RESULT" = timeout ]; then exit 1; fi
printf '{"status":"%s"}\n' "$RESULT"`,
				"jq": `# Check the exact Accepted predicate used by the production script.
[ "$1" = -e ] && [ "$2" = '.status == "Accepted"' ] || exit 2
[ "$RESULT" = Accepted ]`,
			}
			for name, body := range stubs {
				if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/usr/bin/env bash\nset -eu\n"+body+"\n"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			cmd := exec.Command("bash", "../../scripts/sign-macos.sh", filepath.Join(dir, "binary with spaces"))
			failSign := "false"
			if tc.failSign {
				failSign = "true"
			}
			cmd.Env = append(os.Environ(), "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"),
				"CALL_LOG="+logPath, "FAIL_SIGN="+failSign, "RESULT="+tc.result,
				"RELEASE_GOOS="+tc.target, "RELEASE_SNAPSHOT="+tc.snapshot,
				"MACOS_SIGN_IDENTITY=test", "MACOS_KEYCHAIN=test", "MACOS_NOTARY_PROFILE=test")
			out, err := cmd.CombinedOutput()
			if (err != nil) != tc.wantError {
				t.Fatalf("err=%v, output=%s", err, out)
			}
			calls, err := os.ReadFile(logPath)
			if err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			if strings.Contains(string(calls), "codesign --force --timestamp --options runtime") != tc.wantSign {
				t.Errorf("unexpected signing: %s", calls)
			}
			if strings.Contains(string(calls), "xcrun notarytool submit") != tc.wantSubmit {
				t.Errorf("unexpected notarization: %s", calls)
			}
		})
	}
}

func TestAppleReleaseConfiguration(t *testing.T) {
	var workflow struct {
		Jobs map[string]struct {
			Runner string `yaml:"runs-on"`
			Steps  []step `yaml:"steps"`
		} `yaml:"jobs"`
	}
	readYAML(t, ".github/workflows/release.yml", &workflow)
	job := workflow.Jobs["goreleaser"]
	if !strings.HasPrefix(job.Runner, "macos-") {
		t.Error("native Apple tools require a macOS release runner")
	}
	setup, publish, cleanup := -1, -1, -1
	for i, s := range job.Steps {
		if strings.Contains(s.Run, "security create-keychain") {
			setup = i
			for _, key := range []string{"MACOS_SIGN_P12", "MACOS_SIGN_PASSWORD", "MACOS_NOTARY_KEY", "MACOS_NOTARY_KEY_ID", "MACOS_NOTARY_ISSUER_ID"} {
				if s.Env[key] != "${{ secrets."+key+" }}" {
					t.Errorf("missing secret mapping: %s", key)
				}
			}
		}
		if strings.HasPrefix(s.Uses, "goreleaser/goreleaser-action@") {
			publish = i
		}
		if strings.Contains(s.Run, "security delete-keychain") {
			cleanup = i
			if s.If != "always()" {
				t.Error("keychain cleanup must run on failures too")
			}
		}
	}
	if setup < 0 || publish <= setup || cleanup <= publish {
		t.Error("Apple setup must precede publishing and cleanup must follow it")
	}
	var config struct {
		Builds []struct {
			Hooks struct {
				Post []struct {
					Cmd string   `yaml:"cmd"`
					Env []string `yaml:"env"`
				} `yaml:"post"`
			} `yaml:"hooks"`
		} `yaml:"builds"`
	}
	readYAML(t, ".goreleaser.yaml", &config)
	if len(config.Builds) != 1 || len(config.Builds[0].Hooks.Post) != 1 {
		t.Fatal("expected the macOS signing hook on the CLI build")
	}
	hook := config.Builds[0].Hooks.Post[0]
	if hook.Cmd != `bash scripts/sign-macos.sh "{{ .Path }}"` {
		t.Error("sign the built binary before archives and checksums")
	}
	env := strings.Join(hook.Env, "\n")
	for _, v := range []string{"RELEASE_GOOS={{ .Os }}", "RELEASE_SNAPSHOT={{ .IsSnapshot }}"} {
		if !strings.Contains(env, v) {
			t.Errorf("missing hook context: %s", v)
		}
	}
	data, err := os.ReadFile("../../.goreleaser.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "com.apple.quarantine") {
		t.Error("signed releases must not remove download quarantine")
	}
}
