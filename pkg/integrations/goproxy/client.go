package goproxy

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/matzehuels/stacktower/pkg/cache"
	"github.com/matzehuels/stacktower/pkg/integrations"
)

// Dependency represents a Go module dependency with version requirement.
type Dependency struct {
	Name       string // Module path
	Constraint string // Version requirement from go.mod (e.g., "v1.2.3")
}

// ModuleInfo holds metadata for a Go module from the Go module proxy.
//
// Dependencies include only direct dependencies; indirect dependencies are stored separately
// in IndirectDependencies. Some modules (pre-modules or minimal modules) may not have a
// go.mod file; Dependencies will be nil/empty.
//
// For Go 1.17+ modules, the IndirectDependencies list represents the pruned module graph
// (only modules actually needed to build). For older modules, indirect deps may be incomplete.
//
// Zero values: All string fields are empty, Dependencies is nil.
// This struct is safe for concurrent reads after construction.
type ModuleInfo struct {
	Path                 string       // Module path (e.g., "github.com/spf13/cobra", never empty in valid info)
	Version              string       // Latest version from @latest endpoint (e.g., "v1.8.0", never empty in valid info)
	Dependencies         []Dependency // Direct dependencies with versions (nil or empty if none or no go.mod)
	IndirectDependencies []Dependency // Indirect dependencies (marked with "// indirect" in go.mod)
	GoVersion            string       // Go version from go.mod (e.g., "1.21", may be empty for old modules)
	License              string       // License identifier if detectable (may be empty)
	Repository           string       // Canonical source repository URL if discoverable (may be empty)
}

// Client provides access to the Go module proxy API.
// It handles HTTP requests with caching and automatic retries.
//
// All methods are safe for concurrent use by multiple goroutines.
type Client struct {
	*integrations.Client
	baseURL string
}

// NewClient creates a Go module proxy client with the given cache backend.
//
// Parameters:
//   - backend: Cache backend for HTTP response caching (use storage.NullBackend{} for no caching)
//   - cacheTTL: How long responses are cached (typical: 1-24 hours)
//
// The returned Client is safe for concurrent use.
func NewClient(backend cache.Cache, cacheTTL time.Duration) *Client {
	rl := integrations.DefaultRateLimits["goproxy"]
	return &Client{
		Client:  integrations.NewClientWithRateLimit(backend, "goproxy:", cacheTTL, nil, rl.RequestsPerSecond, rl.Burst),
		baseURL: "https://proxy.golang.org",
	}
}

// FetchModule retrieves metadata for a Go module from the module proxy (latest version).
//
// The mod parameter should be a full module path (e.g., "github.com/user/repo").
// Module paths with uppercase letters are escaped per the Go module proxy protocol.
// Module path cannot be empty; an empty string will result in an API error.
//
// If refresh is true, the cache is bypassed and a fresh API call is made.
// If refresh is false, cached data is returned if available and not expired.
//
// This method performs two API calls:
//  1. @latest endpoint to get the latest version
//  2. .mod endpoint to fetch and parse go.mod for dependencies
//
// go.mod fetch failures are silently ignored; Dependencies will be nil/empty if it fails.
// This is normal for pre-module packages or minimal modules without dependencies.
//
// Returns:
//   - ModuleInfo populated with metadata on success
//   - [integrations.ErrNotFound] if the module doesn't exist
//   - [integrations.ErrNetwork] for HTTP failures (timeout, 5xx, etc.)
//   - Other errors for JSON decoding failures
//
// The returned ModuleInfo pointer is never nil if err is nil.
// This method is safe for concurrent use.
func (c *Client) FetchModule(ctx context.Context, mod string, refresh bool) (*ModuleInfo, error) {
	mod = normalizePath(mod)
	key := mod

	var info ModuleInfo
	err := c.Cached(ctx, key, refresh, &info, func() error {
		return c.fetch(ctx, mod, "", &info)
	})
	if err != nil {
		return nil, err
	}
	return &info, nil
}

// FetchModuleVersion retrieves metadata for a specific version of a Go module.
//
// The mod parameter should be a full module path. The version must be an exact version
// string (e.g., "v1.8.0").
//
// If refresh is true, the cache is bypassed and a fresh API call is made.
//
// Returns:
//   - ModuleInfo populated with metadata on success
//   - [integrations.ErrNotFound] if the module or version doesn't exist
//   - [integrations.ErrNetwork] for HTTP failures (timeout, 5xx, etc.)
//
// This method is safe for concurrent use.
func (c *Client) FetchModuleVersion(ctx context.Context, mod, version string, refresh bool) (*ModuleInfo, error) {
	mod = normalizePath(mod)
	key := mod + "@" + version

	var info ModuleInfo
	err := c.Cached(ctx, key, refresh, &info, func() error {
		return c.fetch(ctx, mod, version, &info)
	})
	if err != nil {
		return nil, err
	}
	return &info, nil
}

func (c *Client) fetch(ctx context.Context, mod, version string, info *ModuleInfo) error {
	var err error
	targetVersion := version

	// Get latest version if not specified
	if targetVersion == "" {
		targetVersion, err = c.fetchLatest(ctx, mod)
		if err != nil {
			return err
		}
	} else {
		// Ensure version has 'v' prefix (Go module proxy requires it)
		targetVersion = normalizeVersion(targetVersion)
	}

	// Get go.mod for this version
	goModResult, err := c.fetchGoMod(ctx, mod, targetVersion)
	if err != nil {
		slog.Debug("goproxy: failed to fetch go.mod (no transitive deps will be resolved)",
			"module", mod, "version", targetVersion, "error", err)
		goModResult = nil
	}

	// License is intentionally NOT fetched here. pkg.go.dev scraping is expensive
	// and competes with resolution HTTP calls for rate-limiter tokens. License is
	// fetched post-resolution via FetchLicense (enrichment phase only).
	//
	// Repository is only fetched for vanity module paths (e.g. gopkg.in/*) where
	// the hosting platform cannot be inferred from the module path alone. For
	// github.com/*, gitlab.com/*, and bitbucket.org/* the URL is derived cheaply
	// in normalizeRepositoryURL without any HTTP call.
	repository := ""
	if !isKnownHostingPlatform(mod) {
		repository = c.fetchRepository(ctx, mod)
	}

	*info = ModuleInfo{
		Path:       mod,
		Version:    targetVersion,
		Repository: repository,
	}

	if goModResult != nil {
		info.Dependencies = goModResult.directDeps
		info.IndirectDependencies = goModResult.indirectDeps
		info.GoVersion = goModResult.goVersion
	}

	return nil
}

// isKnownHostingPlatform returns true for module paths whose repository URL
// can be derived from the module path itself without an HTTP call.
func isKnownHostingPlatform(mod string) bool {
	return strings.HasPrefix(mod, "github.com/") ||
		strings.HasPrefix(mod, "gitlab.com/") ||
		strings.HasPrefix(mod, "bitbucket.org/") ||
		strings.HasPrefix(mod, "golang.org/x/")
}

// FetchLicense retrieves the license identifier for a module from pkg.go.dev.
// This is intentionally separated from FetchModule/FetchModuleVersion so that
// the expensive HTML scrape only runs during the post-resolution enrichment
// phase, not during PubGrub solving where many versions are evaluated.
//
// Results are cached at the module level (not per-version) since the license
// is the same for all versions of a module.
func (c *Client) FetchLicense(ctx context.Context, mod string, refresh bool) string {
	mod = normalizePath(mod)
	key := mod + ":license"

	var lic string
	_ = c.Cached(ctx, key, refresh, &lic, func() error {
		lic = c.fetchLicense(ctx, mod)
		return nil
	})
	return lic
}

// licensePattern matches license identifiers in pkg.go.dev HTML.
// pkg.go.dev displays licenses in a "License:" section with links to SPDX.
var licensePattern = regexp.MustCompile(`<a[^>]*href="https?://pkg\.go\.dev/license\?lic=[^"]*"[^>]*>([^<]+)</a>`)

// pkgGoDevURL is the base URL for pkg.go.dev
const pkgGoDevURL = "https://pkg.go.dev"

// fetchLicense attempts to extract license info from pkg.go.dev.
// This scrapes the HTML page for the license section.
// Returns empty string on failure (best-effort).
func (c *Client) fetchLicense(ctx context.Context, mod string) string {
	// Fetch the pkg.go.dev page for this module
	url := fmt.Sprintf("%s/%s", pkgGoDevURL, mod)

	body, err := c.GetText(ctx, url)
	if err != nil {
		slog.Debug("goproxy: failed to fetch pkg.go.dev page for license",
			"module", mod, "error", err)
		return ""
	}

	// Look for license information in the HTML
	// pkg.go.dev formats it as: <a href="https://pkg.go.dev/license?lic=MIT">MIT</a>
	matches := licensePattern.FindStringSubmatch(body)
	if len(matches) >= 2 {
		license := strings.TrimSpace(matches[1])
		// Handle multiple licenses (e.g., "MIT, Apache-2.0")
		// Just return the first one for simplicity
		if idx := strings.Index(license, ","); idx > 0 {
			license = strings.TrimSpace(license[:idx])
		}
		return license
	}

	// Fallback: look for common license patterns in a simpler way
	// pkg.go.dev also shows "License: MIT" in plain text sometimes
	return extractSimpleLicense(body)
}

// extractSimpleLicense looks for common license patterns in HTML.
func extractSimpleLicense(html string) string {
	// Common SPDX identifiers to look for
	licenses := []string{
		"MIT", "Apache-2.0", "BSD-3-Clause", "BSD-2-Clause",
		"GPL-3.0", "GPL-2.0", "LGPL-3.0", "LGPL-2.1",
		"MPL-2.0", "ISC", "Unlicense", "CC0-1.0",
	}

	// Look for "License: X" pattern
	for _, lic := range licenses {
		// Case-insensitive search for the license name
		if strings.Contains(html, ">"+lic+"<") ||
			strings.Contains(html, "License: "+lic) ||
			strings.Contains(html, "license: "+lic) {
			return lic
		}
	}

	return ""
}

var (
	goImportPattern = regexp.MustCompile(`(?i)<meta[^>]+name=["']go-import["'][^>]+content=["']([^"']+)["']`)
	goSourcePattern = regexp.MustCompile(`(?i)<meta[^>]+name=["']go-source["'][^>]+content=["']([^"']+)["']`)
)

// fetchRepository attempts to discover the canonical VCS repository for module
// paths that don't directly reveal their host (for example gopkg.in vanity
// paths). It inspects go-import / go-source meta tags served at ?go-get=1.
func (c *Client) fetchRepository(ctx context.Context, mod string) string {
	url := fmt.Sprintf("https://%s?go-get=1", mod)
	body, err := c.GetText(ctx, url)
	if err != nil {
		return ""
	}
	return extractRepoFromGoGetMeta(body)
}

func extractRepoFromGoGetMeta(html string) string {
	if m := goSourcePattern.FindStringSubmatch(html); len(m) >= 2 {
		parts := strings.Fields(strings.TrimSpace(m[1]))
		// go-source format: "<prefix> <vcs|_> <dir-template> <file-template> ..."
		if len(parts) >= 3 {
			repo := normalizeGoSourceTemplate(parts[2])
			if repo != "" {
				return integrations.NormalizeRepoURL(repo)
			}
		}
	}
	if m := goImportPattern.FindStringSubmatch(html); len(m) >= 2 {
		parts := strings.Fields(strings.TrimSpace(m[1]))
		// go-import format: "<prefix> <vcs> <repo-root>"
		if len(parts) >= 3 {
			return integrations.NormalizeRepoURL(parts[2])
		}
	}
	return ""
}

func normalizeGoSourceTemplate(template string) string {
	s := strings.TrimSpace(template)
	if s == "" {
		return ""
	}
	// Drop template placeholders first.
	s = strings.ReplaceAll(s, "{/dir}", "")
	s = strings.ReplaceAll(s, "{file}", "")
	s = strings.ReplaceAll(s, "{line}", "")

	cutMarkers := []string{"/tree/", "/blob/", "/src/"}
	for _, marker := range cutMarkers {
		if idx := strings.Index(s, marker); idx > 0 {
			s = s[:idx]
			break
		}
	}
	return strings.TrimSuffix(s, "/")
}

func (c *Client) fetchLatest(ctx context.Context, mod string) (string, error) {
	url := fmt.Sprintf("%s/%s/@latest", c.baseURL, escapePath(mod))

	var data latestResponse
	if err := c.Get(ctx, url, &data); err != nil {
		if errors.Is(err, integrations.ErrNotFound) {
			return "", fmt.Errorf("%w: go module %s", err, mod)
		}
		return "", err
	}
	return data.Version, nil
}

// goModParseResult holds the complete result of parsing a go.mod file.
type goModParseResult struct {
	goVersion    string
	directDeps   []Dependency
	indirectDeps []Dependency
}

func (c *Client) fetchGoMod(ctx context.Context, mod, version string) (*goModParseResult, error) {
	url := fmt.Sprintf("%s/%s/@v/%s.mod", c.baseURL, escapePath(mod), version)

	body, err := c.GetText(ctx, url)
	if err != nil {
		return nil, err
	}
	return parseGoModComplete(strings.NewReader(body))
}

// parseGoModComplete parses a go.mod file and returns both direct and indirect
// dependencies along with the go version directive.
func parseGoModComplete(r io.Reader) (*goModParseResult, error) {
	result := &goModParseResult{}
	seenDirect := make(map[string]bool)
	seenIndirect := make(map[string]bool)
	inRequire := false

	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip comments and empty lines
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}

		// Extract go version directive
		if strings.HasPrefix(line, "go ") {
			result.goVersion = strings.TrimSpace(strings.TrimPrefix(line, "go "))
			continue
		}

		// Handle require block
		if strings.HasPrefix(line, "require (") || line == "require(" {
			inRequire = true
			continue
		}
		if inRequire && line == ")" {
			inRequire = false
			continue
		}

		// Single-line require
		if strings.HasPrefix(line, "require ") && !strings.Contains(line, "(") {
			line = strings.TrimPrefix(line, "require ")
		} else if !inRequire {
			continue
		}

		// Parse module path and version from require line
		// Format: module/path v1.2.3 [// indirect]
		dep, isIndirect := parseRequireLineComplete(line)
		if dep.Name == "" {
			continue
		}

		if isIndirect {
			if !seenIndirect[dep.Name] {
				seenIndirect[dep.Name] = true
				result.indirectDeps = append(result.indirectDeps, dep)
			}
		} else {
			if !seenDirect[dep.Name] {
				seenDirect[dep.Name] = true
				result.directDeps = append(result.directDeps, dep)
			}
		}
	}

	return result, scanner.Err()
}

// parseRequireLineComplete parses a require line and returns a Dependency
// along with a flag indicating if it's an indirect dependency.
func parseRequireLineComplete(line string) (Dependency, bool) {
	isIndirect := strings.Contains(line, "// indirect")

	// Remove inline comments
	if idx := strings.Index(line, "//"); idx != -1 {
		line = line[:idx]
	}

	line = strings.TrimSpace(line)
	fields := strings.Fields(line)
	if len(fields) >= 1 {
		// Strip quotes from old-style go.mod files
		name := strings.Trim(fields[0], `"`)
		constraint := ""
		if len(fields) >= 2 {
			version := strings.Trim(fields[1], `"`)
			// Go modules use exact version pins, prefix with = for clarity
			if version != "" {
				constraint = "=" + version
			}
		}
		return Dependency{Name: name, Constraint: constraint}, isIndirect
	}
	return Dependency{}, false
}

// ListVersions returns all available versions for a module, sorted from oldest to newest.
func (c *Client) ListVersions(ctx context.Context, mod string, refresh bool) ([]string, error) {
	mod = normalizePath(mod)
	key := mod + ":versions"

	var versions []string
	err := c.Cached(ctx, key, refresh, &versions, func() error {
		url := fmt.Sprintf("%s/%s/@v/list", c.baseURL, escapePath(mod))

		body, err := c.GetText(ctx, url)
		if err != nil {
			if errors.Is(err, integrations.ErrNotFound) {
				return fmt.Errorf("%w: go module %s", err, mod)
			}
			return err
		}

		// The list endpoint returns one version per line
		lines := strings.Split(strings.TrimSpace(body), "\n")
		versions = make([]string, 0, len(lines))
		for _, line := range lines {
			v := strings.TrimSpace(line)
			if v != "" {
				versions = append(versions, v)
			}
		}
		// Sort versions semantically (oldest to newest)
		integrations.SortVersions(versions)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return versions, nil
}

func normalizePath(path string) string {
	return strings.TrimSpace(path)
}

// normalizeVersion ensures the version has a 'v' prefix.
// Go module proxy requires versions to be prefixed with 'v' (e.g., v1.2.3).
// PubGrub's semantic versioning may strip the prefix, so we add it back.
func normalizeVersion(version string) string {
	version = strings.TrimSpace(version)
	if version == "" {
		return version
	}
	if !strings.HasPrefix(version, "v") {
		return "v" + version
	}
	return version
}

func escapePath(path string) string {
	var b strings.Builder
	for _, r := range path {
		if r >= 'A' && r <= 'Z' {
			b.WriteByte('!')
			b.WriteRune(r + ('a' - 'A'))
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

type latestResponse struct {
	Version string `json:"Version"`
	Time    string `json:"Time"`
}
