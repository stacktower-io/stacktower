package npm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/matzehuels/stacktower/pkg/cache"
	"github.com/matzehuels/stacktower/pkg/integrations"
)

// Dependency represents a package dependency with version constraint.
type Dependency struct {
	Name       string // Package name
	Constraint string // Version constraint (e.g., "^4.17.0", ">=2.0.0"), empty for no constraint
}

// PackageInfo holds metadata for a JavaScript/TypeScript package from npm.
//
// The Version field always contains the "latest" dist-tag version.
// Dependencies include only runtime "dependencies", not devDependencies or peerDependencies.
//
// Zero values: All string fields are empty, Dependencies is nil.
// This struct is safe for concurrent reads after construction.
type PackageInfo struct {
	Name         string       // Package name as published (e.g., "@scope/package", never empty in valid info)
	Version      string       // Latest version tag (e.g., "4.18.2", never empty in valid info)
	Dependencies []Dependency // Runtime dependencies with version constraints (nil or empty if none)
	Repository   string       // Normalized repository URL (empty if not provided)
	HomePage     string       // Homepage URL (may be empty)
	Description  string       // Package description (may be empty)
	License      string       // License identifier (e.g., "MIT", may be empty)
	LicenseText  string       // Full license text for custom licenses (may be empty)
	Author       string       // Author name (may be empty)
	RequiredNode string       // Node.js version constraint from engines.node (e.g., ">=18", may be empty)
}

// Client provides access to the npm package registry API.
// It handles HTTP requests with caching and automatic retries.
//
// All methods are safe for concurrent use by multiple goroutines.
type Client struct {
	*integrations.Client
	baseURL string
}

// NewClient creates an npm client with the given cache backend.
//
// Parameters:
//   - backend: Cache backend for HTTP response caching (use storage.NullBackend{} for no caching)
//   - cacheTTL: How long responses are cached (typical: 1-24 hours)
//
// The returned Client is safe for concurrent use.
func NewClient(backend cache.Cache, cacheTTL time.Duration) *Client {
	rl := integrations.DefaultRateLimits["npm"]
	return &Client{
		Client:  integrations.NewClientWithRateLimit(backend, "npm:", cacheTTL, nil, rl.RequestsPerSecond, rl.Burst),
		baseURL: "https://registry.npmjs.org",
	}
}

// FetchPackage retrieves metadata for a JavaScript/TypeScript package from npm (latest version).
//
// The pkg parameter is normalized to lowercase with whitespace trimmed.
// Supports scoped packages (e.g., "@types/node").
// Package name cannot be empty; an empty string will result in an API error.
//
// If refresh is true, the cache is bypassed and a fresh API call is made.
// If refresh is false, cached data is returned if available and not expired.
//
// Returns:
//   - PackageInfo populated with metadata for the "latest" dist-tag version
//   - [integrations.ErrNotFound] if the package doesn't exist
//   - [integrations.ErrNetwork] for HTTP failures (timeout, 5xx, etc.)
//   - Other errors for JSON decoding failures or missing "latest" version
//
// The returned PackageInfo pointer is never nil if err is nil.
// This method is safe for concurrent use.
func (c *Client) FetchPackage(ctx context.Context, pkg string, refresh bool) (*PackageInfo, error) {
	pkg = strings.ToLower(strings.TrimSpace(pkg))
	key := pkg

	var info PackageInfo
	err := c.Cached(ctx, key, refresh, &info, func() error {
		return c.fetch(ctx, pkg, "", &info)
	})
	if err != nil {
		return nil, err
	}
	return &info, nil
}

// FetchPackageVersion retrieves metadata for a specific version of an npm package.
//
// The pkg parameter is normalized to lowercase. The version must be an exact version
// string (e.g., "4.17.21").
//
// If refresh is true, the cache is bypassed and a fresh API call is made.
//
// Returns:
//   - PackageInfo populated with metadata on success
//   - [integrations.ErrNotFound] if the package or version doesn't exist
//   - [integrations.ErrNetwork] for HTTP failures (timeout, 5xx, etc.)
//
// This method is safe for concurrent use.
func (c *Client) FetchPackageVersion(ctx context.Context, pkg, version string, refresh bool) (*PackageInfo, error) {
	pkg = strings.ToLower(strings.TrimSpace(pkg))
	key := pkg + "@" + version

	var info PackageInfo
	err := c.Cached(ctx, key, refresh, &info, func() error {
		return c.fetch(ctx, pkg, version, &info)
	})
	if err != nil {
		return nil, err
	}
	return &info, nil
}

func (c *Client) fetch(ctx context.Context, pkg, version string, info *PackageInfo) error {
	var url string
	if version != "" {
		// Use abbreviated endpoint for specific version
		url = c.baseURL + "/" + pkg + "/" + version
	} else {
		url = c.baseURL + "/" + pkg
	}

	if version != "" {
		// Fetch specific version directly
		var v versionDetails
		if err := c.Get(ctx, url, &v); err != nil {
			if errors.Is(err, integrations.ErrNotFound) {
				return fmt.Errorf("%w: npm package %s version %s", err, pkg, version)
			}
			return err
		}
		license, licenseText := extractLicense(v.License)
		*info = PackageInfo{
			Name:         pkg,
			Version:      version,
			Description:  v.Description,
			License:      license,
			LicenseText:  licenseText,
			Author:       extractField(v.Author, "name"),
			Repository:   integrations.NormalizeRepoURL(extractField(v.Repository, "url")),
			HomePage:     v.HomePage,
			Dependencies: extractDeps(v.Dependencies),
			RequiredNode: v.Engines.Node,
		}
		return nil
	}

	// Fetch latest version from full package data
	var data registryResponse
	if err := c.Get(ctx, url, &data); err != nil {
		if errors.Is(err, integrations.ErrNotFound) {
			return fmt.Errorf("%w: npm package %s", err, pkg)
		}
		return err
	}

	latest := data.DistTags.Latest
	v, ok := data.Versions[latest]
	if !ok {
		return fmt.Errorf("version %s not found", latest)
	}

	license, licenseText := extractLicense(v.License)
	*info = PackageInfo{
		Name:         data.Name,
		Version:      latest,
		Description:  v.Description,
		License:      license,
		LicenseText:  licenseText,
		Author:       extractField(v.Author, "name"),
		Repository:   integrations.NormalizeRepoURL(extractField(v.Repository, "url")),
		HomePage:     v.HomePage,
		Dependencies: extractDeps(v.Dependencies),
		RequiredNode: v.Engines.Node,
	}
	return nil
}

// extractDeps converts a dependencies map to a slice of Dependency structs.
func extractDeps(deps map[string]string) []Dependency {
	if len(deps) == 0 {
		return nil
	}
	result := make([]Dependency, 0, len(deps))
	for name, constraint := range deps {
		result = append(result, Dependency{Name: name, Constraint: constraint})
	}
	// Sort for consistent ordering
	slices.SortFunc(result, func(a, b Dependency) int {
		return strings.Compare(a.Name, b.Name)
	})
	return result
}

// extractLicense extracts license identifier and full text from the license field.
// npm packages can have license as a string or an object with type/url fields.
func extractLicense(v any) (license, licenseText string) {
	switch val := v.(type) {
	case string:
		// If it's a long string (>100 chars), it might be full license text
		if len(val) > 100 {
			licenseText = val
			// Try to extract identifier from first line
			if idx := strings.Index(val, "\n"); idx > 0 && idx < 50 {
				license = strings.TrimSpace(val[:idx])
			}
		} else {
			license = val
		}
	case map[string]any:
		if t, ok := val["type"].(string); ok {
			license = t
		}
		if text, ok := val["text"].(string); ok && len(text) > 100 {
			licenseText = text
		}
	}
	return license, licenseText
}

// ListVersions returns all available versions for a package.
// Versions are sorted lexicographically (not by semver), so "1.10.0" sorts before "1.2.0".
// Callers that need semver ordering should sort the result with a semver-aware comparator.
func (c *Client) ListVersions(ctx context.Context, pkg string, refresh bool) ([]string, error) {
	pkg = strings.ToLower(strings.TrimSpace(pkg))
	key := pkg + ":versions"

	var versions []string
	err := c.Cached(ctx, key, refresh, &versions, func() error {
		url := c.baseURL + "/" + pkg

		var data registryResponse
		if err := c.Get(ctx, url, &data); err != nil {
			if errors.Is(err, integrations.ErrNotFound) {
				return fmt.Errorf("%w: npm package %s", err, pkg)
			}
			return err
		}

		// Extract version strings from versions map
		versions = make([]string, 0, len(data.Versions))
		for v := range data.Versions {
			versions = append(versions, v)
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

// ListVersionsWithConstraints returns all versions and their Node.js runtime constraints.
// Returns a map of version -> engines.node (empty string if not specified).
func (c *Client) ListVersionsWithConstraints(ctx context.Context, pkg string, refresh bool) (map[string]string, error) {
	pkg = strings.ToLower(strings.TrimSpace(pkg))
	key := pkg + ":version_constraints"

	var result map[string]string
	err := c.Cached(ctx, key, refresh, &result, func() error {
		url := c.baseURL + "/" + pkg

		var data registryResponse
		if err := c.Get(ctx, url, &data); err != nil {
			if errors.Is(err, integrations.ErrNotFound) {
				return fmt.Errorf("%w: npm package %s", err, pkg)
			}
			return err
		}

		result = make(map[string]string, len(data.Versions))
		for version, v := range data.Versions {
			result[version] = strings.TrimSpace(v.Engines.Node)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func extractField(v any, field string) string {
	switch val := v.(type) {
	case string:
		return val
	case map[string]any:
		if s, ok := val[field].(string); ok {
			return s
		}
	}
	return ""
}

type registryResponse struct {
	Name     string                    `json:"name"`
	DistTags distTags                  `json:"dist-tags"`
	Versions map[string]versionDetails `json:"versions"`
}

type distTags struct {
	Latest string `json:"latest"`
}

type versionDetails struct {
	Description  string            `json:"description"`
	License      any               `json:"license"`
	Author       any               `json:"author"`
	Repository   any               `json:"repository"`
	HomePage     string            `json:"homepage"`
	Dependencies map[string]string `json:"dependencies"`
	Engines      packageEngines    `json:"engines"`
}

type packageEngines struct {
	Node string `json:"node"`
	NPM  string `json:"npm"`
}

// UnmarshalJSON tolerates non-object "engines" values found in some npm package versions.
// npm metadata in the wild occasionally contains malformed engines payloads (e.g. arrays).
// For these cases we keep engines empty instead of failing the entire package decode.
func (p *packageEngines) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || trimmed == "null" {
		*p = packageEngines{}
		return nil
	}

	// Expected shape: {"node":"...", "npm":"..."}
	if strings.HasPrefix(trimmed, "{") {
		type alias packageEngines
		var decoded alias
		if err := json.Unmarshal(data, &decoded); err != nil {
			return err
		}
		*p = packageEngines(decoded)
		return nil
	}

	// Unexpected shape (array/string/number/etc.) -> ignore safely.
	*p = packageEngines{}
	return nil
}
