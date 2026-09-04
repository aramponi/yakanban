// Command gensite generates the landing page and llms.txt.
//
// It is deliberately the whole build step. The page is static HTML and one
// stylesheet; the repository is a Go binary with two dependencies, and a
// landing page has no business bringing a second toolchain with it.
//
//	make site        # ./bin/yakanban, then site/public/
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/aramponi/yakanban/internal/site"
	"github.com/aramponi/yakanban/internal/version"
)

func main() {
	var (
		root    = flag.String("root", ".", "repository root, holding README.md and site/content.md")
		out     = flag.String("out", "site/public", "directory to write the generated site into")
		bin     = flag.String("bin", "bin/yakanban", "yakanban binary to capture terminal output from")
		repo    = flag.String("repo", "https://github.com/aramponi/yakanban", "repository URL")
		siteURL = flag.String("site", "https://aramponi.github.io/yakanban", "URL the site is published at")
		ver     = flag.String("version", version.String(), "version the page describes")
	)
	flag.Parse()

	if err := run(*root, *out, *bin, *repo, *siteURL, *ver); err != nil {
		fmt.Fprintln(os.Stderr, "gensite:", err)
		os.Exit(1)
	}
}

func run(root, out, bin, repo, siteURL, ver string) error {
	readmeSrc, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil {
		return err
	}
	contentSrc, err := os.ReadFile(filepath.Join(root, "site", "content.md"))
	if err != nil {
		return err
	}

	readme, err := site.ParseReadme(string(readmeSrc))
	if err != nil {
		return fmt.Errorf("README.md: %w", err)
	}
	content, err := site.ParseContent(string(contentSrc))
	if err != nil {
		return err
	}

	binPath, err := filepath.Abs(bin)
	if err != nil {
		return err
	}
	// A failure here fails the build. The alternative — falling back to a
	// hand-written transcript — is exactly the mockup the page promises not
	// to show.
	samples, err := site.CaptureAll(binPath, root)
	if err != nil {
		return fmt.Errorf("capturing terminal output: %w", err)
	}

	page, err := site.Build(site.Input{
		Readme:  readme,
		Content: content,
		Samples: samples,
		Repo:    repo,
		SiteURL: siteURL,
		Version: ver,
		Built:   time.Now().UTC(),
	})
	if err != nil {
		return err
	}

	indexHTML, err := page.RenderHTML()
	if err != nil {
		return err
	}
	llms, err := page.RenderLLMs()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	files := map[string]string{
		"index.html": indexHTML,
		"llms.txt":   llms,
		// GitHub Pages runs Jekyll unless told not to, and Jekyll would
		// swallow anything that looks like a template.
		".nojekyll": "",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(out, name), []byte(body), 0o644); err != nil {
			return err
		}
	}

	fmt.Printf("wrote %s: index.html (%d bytes), llms.txt (%d bytes)\n", out, len(indexHTML), len(llms))
	return nil
}
