package packagist

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/matzehuels/stacktower/pkg/cache"
	"github.com/matzehuels/stacktower/pkg/integrations"
)

// Dependency represents a Composer package dependency with version constraint.
type Dependency struct {
	Name       string // Package name (vendor/package)
	Constraint string // Version constraint (e.g., "^6.0", ">=2.0,<3.0")
}

// PackageInfo holds metadata for a PHP package from Packagist.
//
// Package names follow Composer conventions (vendor/package format).
// Version is the latest stable version; dev versions are skipped.
// Dependencies exclude PHP, extensions (ext-*), libraries (lib-*), and Composer platform packages.
//
// Zero values: All string fields are empty, Dependencies is nil.
// This struct is safe for concurrent reads after construction.
type PackageInfo struct {
	Name         string       // Package name (e.g., "symfony/console", never empty in valid info)
	Version      string       // Latest stable version (e.g., "6.3.0", never empty in valid info)
	Dependencies []Dependency // Composer require dependencies with constraints (nil or empty if none)
	Repository   string       // Normalized repository URL (empty if not provided)
	HomePage     string       // Homepage URL (may be empty)
	Description  string       // Package description (may be empty)
	License      string       // License identifier (may be empty, only first license if multiple)
	Author       string       // First author name (may be empty)
	RequiredPHP  string       // PHP version constraint from require.php (e.g., ">=8.1", may be empty)
}

// Client provides access to the Packagist package registry API.
// It handles HTTP requests with caching and automatic retries.
//
// All methods are safe for concurrent use by multiple goroutines.
type Client struct {
	*integrations.Client
	baseURL     string // P2 metadata API (repo.packagist.org)
	registryURL string // Full package API (packagist.org) — includes require for all versions
}

// NewClient creates a Packagist client with the given cache backend.
//
// Parameters:
//   - backend: Cache backend for HTTP response caching (use storage.NullBackend{} for no caching)
//   - cacheTTL: How long responses are cached (typical: 1-24 hours)
//
// The returned Client is safe for concurrent use.
func NewClient(backend cache.Cache, cacheTTL time.Duration) *Client {
	rl := integrations.DefaultRateLimits["packagist"]
	return &Client{
		Client:      integrations.NewClientWithRateLimit(backend, "packagist:", cacheTTL, nil, rl.RequestsPerSecond, rl.Burst),
		baseURL:     "https://repo.packagist.org",
		registryURL: "https://packagist.org",
	}
}

// FetchPackage retrieves metadata for a PHP package from Packagist (latest version).
//
// The pkg parameter must be in "vendor/package" format (e.g., "symfony/console").
// Package name is normalized to lowercase with whitespace trimmed.
// Package name cannot be empty; an empty string will result in an API error.
//
// If refresh is true, the cache is bypassed and a fresh API call is made.
// If refresh is false, cached data is returned if available and not expired.
//
// Version selection: The latest stable version is selected, skipping dev versions.
// If no stable version exists, the first version in the list is used.
//
// Returns:
//   - PackageInfo populated with metadata on success
//   - [integrations.ErrNotFound] if the package doesn't exist
//   - [integrations.ErrNetwork] for HTTP failures (timeout, 5xx, etc.)
//   - Other errors for JSON decoding failures or missing version data
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

// FetchPackageVersion retrieves metadata for a specific version of a PHP package.
//
// The pkg parameter must be in "vendor/package" format. The version must be an exact
// version string (e.g., "6.3.0").
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
	var data p2Response
	if err := c.Get(ctx, fmt.Sprintf("%s/p2/%s.json", c.baseURL, pkg), &data); err != nil {
		if errors.Is(err, integrations.ErrNotFound) {
			return fmt.Errorf("%w: packagist package %s", err, pkg)
		}
		return err
	}

	versions, ok := data.Packages[pkg]
	if !ok || len(versions) == 0 {
		return fmt.Errorf("no versions found for %s", pkg)
	}

	var v p2Version
	if version != "" {
		// Find specific version
		found := false
		for _, ver := range versions {
			if composerVersionsEquivalent(ver.Version, version) {
				v = ver
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("%w: packagist package %s version %s", integrations.ErrNotFound, pkg, version)
		}
	} else {
		v = latestStable(versions)
	}

	var license, author string
	if len(v.License) > 0 {
		license = v.License[0]
	}
	if len(v.Authors) > 0 {
		author = strings.TrimSpace(v.Authors[0].Name)
	}

	// Extract PHP version constraint before filtering
	requiredPHP := v.Require["php"]

	*info = PackageInfo{
		Name:         v.Name,
		Version:      v.Version,
		Description:  v.Description,
		License:      license,
		Author:       author,
		Repository:   integrations.NormalizeRepoURL(v.Source.URL),
		HomePage:     v.Homepage,
		Dependencies: filterDeps(v.Require),
		RequiredPHP:  requiredPHP,
	}
	return nil
}

func filterDeps(require map[string]string) []Dependency {
	var deps []Dependency
	for name, constraint := range require {
		ln := strings.ToLower(name)
		switch {
		case ln == "php" || ln == "composer-plugin-api" || ln == "composer-runtime-api":
			continue
		case strings.HasPrefix(ln, "ext-") || strings.HasPrefix(ln, "lib-"):
			continue
		case !strings.Contains(ln, "/"):
			continue
		}
		deps = append(deps, Dependency{Name: ln, Constraint: constraint})
	}
	// Sort for consistent ordering
	slices.SortFunc(deps, func(a, b Dependency) int {
		return strings.Compare(a.Name, b.Name)
	})
	return deps
}

// ListVersions returns all available versions for a package, sorted from oldest to newest.
func (c *Client) ListVersions(ctx context.Context, pkg string, refresh bool) ([]string, error) {
	pkg = strings.ToLower(strings.TrimSpace(pkg))
	key := pkg + ":versions"

	var versions []string
	err := c.Cached(ctx, key, refresh, &versions, func() error {
		var data p2Response
		if err := c.Get(ctx, fmt.Sprintf("%s/p2/%s.json", c.baseURL, pkg), &data); err != nil {
			if errors.Is(err, integrations.ErrNotFound) {
				return fmt.Errorf("%w: packagist package %s", err, pkg)
			}
			return err
		}

		pkgVersions, ok := data.Packages[pkg]
		if !ok || len(pkgVersions) == 0 {
			return fmt.Errorf("no versions found for %s", pkg)
		}

		versions = make([]string, 0, len(pkgVersions))
		for _, v := range pkgVersions {
			versions = append(versions, v.Version)
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

// ListVersionsWithConstraints returns all versions and their PHP runtime constraints.
// Returns a map of version -> require.php (empty string if not specified).
//
// Uses the full Packagist API (packagist.org/packages/) instead of the P2 API
// because P2 returns minimal stubs (no require field) for most versions.
func (c *Client) ListVersionsWithConstraints(ctx context.Context, pkg string, refresh bool) (map[string]string, error) {
	pkg = strings.ToLower(strings.TrimSpace(pkg))
	key := pkg + ":version_constraints"

	var result map[string]string
	err := c.Cached(ctx, key, refresh, &result, func() error {
		var data fullPackageResponse
		if err := c.Get(ctx, fmt.Sprintf("%s/packages/%s.json", c.registryURL, pkg), &data); err != nil {
			if errors.Is(err, integrations.ErrNotFound) {
				return fmt.Errorf("%w: packagist package %s", err, pkg)
			}
			return err
		}

		if len(data.Package.Versions) == 0 {
			return fmt.Errorf("no versions found for %s", pkg)
		}

		result = make(map[string]string, len(data.Package.Versions))
		for ver, meta := range data.Package.Versions {
			result[ver] = strings.TrimSpace(meta.Require["php"])
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func latestStable(versions []p2Version) p2Version {
	for _, v := range versions {
		lv := strings.ToLower(v.Version)
		if strings.Contains(lv, "dev") {
			continue
		}
		if strings.Contains(strings.TrimPrefix(lv, "v"), ".") {
			return v
		}
	}
	return versions[0]
}

type p2Response struct {
	Packages map[string][]p2Version `json:"packages"`
}

// fullPackageResponse is the response from packagist.org/packages/{name}.json.
// Unlike the P2 API, this endpoint returns complete metadata (including require)
// for every version.
type fullPackageResponse struct {
	Package struct {
		Versions map[string]fullVersion `json:"versions"`
	} `json:"package"`
}

type fullVersion struct {
	Require map[string]string `json:"require"`
}

type p2Version struct {
	Name        string            `json:"name"`
	Version     string            `json:"version"`
	Description string            `json:"description"`
	Homepage    string            `json:"homepage"`
	License     []string          `json:"license"`
	Require     map[string]string `json:"require"`
	Source      struct {
		URL string `json:"url"`
	} `json:"source"`
	Authors []struct {
		Name string `json:"name"`
	} `json:"authors"`
}

func (v *p2Version) UnmarshalJSON(b []byte) error {
	type raw struct {
		Name        string          `json:"name"`
		Version     string          `json:"version"`
		Description string          `json:"description"`
		Homepage    string          `json:"homepage"`
		License     json.RawMessage `json:"license"`
		Require     json.RawMessage `json:"require"`
		Source      struct {
			URL string `json:"url"`
		} `json:"source"`
		Authors []struct {
			Name string `json:"name"`
		} `json:"authors"`
	}

	var r raw
	if err := json.Unmarshal(b, &r); err != nil {
		return err
	}

	v.Name = r.Name
	v.Version = r.Version
	v.Description = r.Description
	v.Homepage = r.Homepage
	v.Source = r.Source
	v.Authors = r.Authors

	if len(r.License) > 0 && string(r.License) != "null" {
		if err := json.Unmarshal(r.License, &v.License); err != nil {
			var single string
			if json.Unmarshal(r.License, &single) == nil && single != "" {
				v.License = []string{single}
			}
		}
	}

	if len(r.Require) > 0 && string(r.Require) != "null" {
		v.Require = make(map[string]string)
		if err := json.Unmarshal(r.Require, &v.Require); err != nil {
			var anyObj map[string]any
			if json.Unmarshal(r.Require, &anyObj) == nil {
				for k, val := range anyObj {
					if s, ok := val.(string); ok {
						v.Require[k] = s
					}
				}
			}
		}
	}
	return nil
}

func composerVersionsEquivalent(a, b string) bool {
	normalize := func(v string) string {
		v = strings.TrimSpace(strings.TrimPrefix(v, "v"))
		parts := strings.Split(v, ".")
		clean := make([]string, 0, len(parts))
		for _, p := range parts {
			if p == "" {
				return v
			}
			if _, err := strconv.Atoi(p); err != nil {
				return v
			}
			clean = append(clean, p)
		}
		for len(clean) > 1 && clean[len(clean)-1] == "0" {
			clean = clean[:len(clean)-1]
		}
		return strings.Join(clean, ".")
	}
	return normalize(a) == normalize(b)
}
