// Package releasecheck guards the security settings skipped by snapshot builds.
package releasecheck

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type step struct {
	Uses string            `yaml:"uses"`
	With map[string]string `yaml:"with"`
	Run  string            `yaml:"run"`
}

func readYAML(t *testing.T, name string, target any) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", name))
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(data, target); err != nil {
		t.Fatal(err)
	}
}

func TestReleaseSecurity(t *testing.T) {
	var workflow struct {
		Jobs map[string]struct {
			Permissions map[string]string `yaml:"permissions"`
			Steps       []step            `yaml:"steps"`
		} `yaml:"jobs"`
	}
	readYAML(t, ".github/workflows/release.yml", &workflow)
	job := workflow.Jobs["goreleaser"]
	for _, permission := range []string{"contents", "id-token", "attestations"} {
		if job.Permissions[permission] != "write" {
			t.Errorf("release job needs %s: write", permission)
		}
	}
	installer, release, attest := -1, -1, -1
	var runs string
	for i, s := range job.Steps {
		switch {
		case strings.HasPrefix(s.Uses, "sigstore/cosign-installer@"):
			installer = i
			if s.With["cosign-release"] == "" {
				t.Error("pin the cosign version for detached signature compatibility")
			}
		case strings.HasPrefix(s.Uses, "goreleaser/goreleaser-action@"):
			release = i
		case strings.HasPrefix(s.Uses, "actions/attest@"):
			attest = i
			if s.With["subject-checksums"] != "dist/checksums.txt" {
				t.Error("attest the released archive manifest")
			}
		}
		runs += s.Run
	}
	if installer < 0 || release <= installer || attest <= release {
		t.Error("install cosign before releasing, then attest the archives")
	}
	for _, check := range []string{"gh release download", "cosign verify-blob", "sha256sum --check", "gh attestation verify", "--source-ref", "--source-digest"} {
		if !strings.Contains(runs, check) {
			t.Errorf("missing published-release check: %s", check)
		}
	}
	// Snapshot builds must remain usable on fork PRs without signing credentials.
	workflow.Jobs = nil
	readYAML(t, ".github/workflows/ci.yml", &workflow)
	snapshot := false
	for _, s := range workflow.Jobs["release-snapshot"].Steps {
		if strings.HasPrefix(s.Uses, "goreleaser/goreleaser-action@") && strings.Contains(s.With["args"], "--snapshot") {
			snapshot = true
			if !strings.Contains(s.With["args"], "--skip=sign") {
				t.Error("snapshot CI must explicitly skip signing without OIDC")
			}
		}
	}
	if !snapshot {
		t.Error("missing release snapshot build")
	}
	var config struct {
		Checksum struct {
			Name string `yaml:"name_template"`
		} `yaml:"checksum"`
		Signs []struct {
			Cmd         string   `yaml:"cmd"`
			Artifacts   string   `yaml:"artifacts"`
			Signature   string   `yaml:"signature"`
			Certificate string   `yaml:"certificate"`
			Args        []string `yaml:"args"`
		} `yaml:"signs"`
	}
	readYAML(t, ".goreleaser.yaml", &config)
	if config.Checksum.Name != "checksums.txt" {
		t.Error("checksum filename must match the attestation input")
	}
	if len(config.Signs) != 1 {
		t.Fatalf("expected one checksum signer, got %d", len(config.Signs))
	}
	sign := config.Signs[0]
	if sign.Cmd != "cosign" || sign.Artifacts != "checksum" || sign.Signature != "${artifact}.sig" || sign.Certificate != "${artifact}.pem" {
		t.Error("sign checksums with cosign and publish the documented detached files")
	}
	args := strings.Join(sign.Args, " ")
	for _, arg := range []string{"sign-blob", "--yes", "--use-signing-config=false", "--new-bundle-format=false", "--output-signature=${signature}", "--output-certificate=${certificate}"} {
		if !strings.Contains(args, arg) {
			t.Errorf("missing cosign argument: %s", arg)
		}
	}
	if len(sign.Args) == 0 || sign.Args[len(sign.Args)-1] != "${artifact}" {
		t.Error("cosign must sign the artifact")
	}
}
