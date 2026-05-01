// Package buildinfo provides build-time version information.
//
// Variables are set via ldflags during build:
//
//	go build -ldflags "-X github.com/stacktower-io/stacktower/pkg/buildinfo.Version=v1.0.0 \
//	    -X github.com/stacktower-io/stacktower/pkg/buildinfo.Commit=$(git rev-parse HEAD) \
//	    -X github.com/stacktower-io/stacktower/pkg/buildinfo.Date=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
package buildinfo

import (
	"fmt"
	"os"
)

var (
	// Version is the semantic version (e.g., "v1.2.3").
	// Set via ldflags: -X github.com/stacktower-io/stacktower/pkg/buildinfo.Version=...
	Version = "dev"

	// Commit is the git commit SHA.
	// Set via ldflags: -X github.com/stacktower-io/stacktower/pkg/buildinfo.Commit=...
	Commit = "none"

	// Date is the build timestamp.
	// Set via ldflags: -X github.com/stacktower-io/stacktower/pkg/buildinfo.Date=...
	Date = "unknown"

	// GitHubAppClientID is the OAuth client ID for GitHub device flow authentication.
	// Set via ldflags or override at runtime with STACKTOWER_GITHUB_APP_CLIENT_ID.
	GitHubAppClientID = ""

	// GitHubAppSlug is the GitHub App slug for installation URLs.
	// Set via ldflags or override at runtime with STACKTOWER_GITHUB_APP_SLUG.
	GitHubAppSlug = ""

	// CompiledGitHubAppClientID is the OAuth client ID embedded at build time.
	// This value is captured before runtime env var overrides are applied.
	CompiledGitHubAppClientID = ""

	// CompiledGitHubAppSlug is the app slug embedded at build time.
	// This value is captured before runtime env var overrides are applied.
	CompiledGitHubAppSlug = ""
)

func init() {
	// Preserve the build-time values so CLI diagnostics can show exactly what was compiled.
	CompiledGitHubAppClientID = GitHubAppClientID
	CompiledGitHubAppSlug = GitHubAppSlug

	if v := os.Getenv("STACKTOWER_GITHUB_APP_CLIENT_ID"); v != "" {
		GitHubAppClientID = v
	}
	if v := os.Getenv("STACKTOWER_GITHUB_APP_SLUG"); v != "" {
		GitHubAppSlug = v
	}
}

// String returns the formatted build information.
func String() string {
	return fmt.Sprintf("version: %s\ncommit: %s\nbuilt: %s", Version, Commit, Date)
}

// Template returns the version template string for cobra.
func Template() string {
	return fmt.Sprintf("{{.Name}} version %s\ncommit: %s\nbuilt: %s\n", Version, Commit, Date)
}
