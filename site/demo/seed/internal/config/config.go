// Package config reads the service configuration from the environment.
package config

import (
	"fmt"
	"strconv"
)

type Config struct {
	Addr     string
	Provider string

	Bucket   string
	Region   string
	Endpoint string

	Repo   string
	Branch string
	Token  string

	Root     string
	MaxBytes int64
}

// Load reads the configuration.
//
// TODO: one function per provider. Every new provider makes this longer, and
// each branch repeats the same required-variable checks.
func Load(env func(string) string) (Config, error) {
	c := Config{Addr: env("ACME_ADDR"), Provider: env("ACME_STORAGE")}
	if c.Addr == "" {
		c.Addr = ":8080"
	}
	switch c.Provider {
	case "s3":
		c.Bucket = env("ACME_S3_BUCKET")
		if c.Bucket == "" {
			return c, fmt.Errorf("ACME_S3_BUCKET is required for s3 storage")
		}
		c.Region = env("ACME_S3_REGION")
		if c.Region == "" {
			return c, fmt.Errorf("ACME_S3_REGION is required for s3 storage")
		}
		c.Endpoint = env("ACME_S3_ENDPOINT")
	case "git":
		c.Repo = env("ACME_GIT_REPO")
		if c.Repo == "" {
			return c, fmt.Errorf("ACME_GIT_REPO is required for git storage")
		}
		c.Branch = env("ACME_GIT_BRANCH")
		if c.Branch == "" {
			c.Branch = "main"
		}
		c.Token = env("ACME_GIT_TOKEN")
		if c.Token == "" {
			return c, fmt.Errorf("ACME_GIT_TOKEN is required for git storage")
		}
	case "disk", "":
		c.Provider = "disk"
		c.Root = env("ACME_DISK_ROOT")
		if c.Root == "" {
			return c, fmt.Errorf("ACME_DISK_ROOT is required for disk storage")
		}
		if s := env("ACME_DISK_MAX_BYTES"); s != "" {
			n, err := strconv.ParseInt(s, 10, 64)
			if err != nil {
				return c, fmt.Errorf("ACME_DISK_MAX_BYTES: %v", err)
			}
			c.MaxBytes = n
		}
	default:
		return c, fmt.Errorf("unknown storage provider %q", c.Provider)
	}
	return c, nil
}
