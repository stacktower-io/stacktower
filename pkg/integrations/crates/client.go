package crates

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/matzehuels/stacktower/pkg/cache"
	"github.com/matzehuels/stacktower/pkg/integrations"
)

// Dependency represents a crate dependency with version requirement.
type Dependency struct {
	Name       string // Crate name
	Constraint string // Version requirement (e.g., "^1.0", ">=0.5,<1.0"), empty for no constraint
}

// CrateInfo holds metadata for a Rust crate from crates.io.
//
// The Version field contains the max_version (latest stable or highest version).
// Dependencies include only "normal" (non-dev, non-optional) dependencies.
//
// Zero values: All string fields are empty, Dependencies is nil, Downloads is 0.
// A Downloads value of 0 is valid for newly published crates.
// This struct is safe for concurrent reads after construction.
type CrateInfo struct {
	Name         string       // Crate name (e.g., "serde", never empty in valid info)
	Version      string       // Latest version (e.g., "1.0.193", never empty in valid info)
	Dependencies []Dependency // Normal dependencies with version requirements (nil or empty if none)
	Repository   string       // Repository URL (may be empty)
	HomePage     string       // Homepage URL (may be empty)
	Description  string       // Crate description (may be empty)
	License      string       // License identifier(s) (may be empty or "MIT OR Apache-2.0")
	Downloads    int          // Total download count across all versions (0 for new crates)
	MSRV         string       // Minimum Supported Rust Version (e.g., "1.70.0", may be empty)
}

// Client provides access to the crates.io package registry API.
// It handles HTTP requests with caching and automatic retries.
//
// All methods are safe for concurrent use by multiple goroutines.
//
// Note: crates.io requires a User-Agent header; this client sets one automatically.
type Client struct {
	*integrations.Client
	baseURL string
}

// NewClient creates a crates.io client with the given cache backend.
//
// Parameters:
//   - backend: Cache backend for HTTP response caching (use storage.NullBackend{} for no caching)
//   - cacheTTL: How long responses are cached (typical: 1-24 hours)
//
// The client includes a User-Agent header as required by crates.io API policy.
// The returned Client is safe for concurrent use.
func NewClient(backend cache.Cache, cacheTTL time.Duration) *Client {
	headers := map[string]string{
		"User-Agent": "stacktower/1.0 (https://github.com/matzehuels/stacktower)",
	}
	rl := integrations.DefaultRateLimits["crates"]
	return &Client{
		Client:  integrations.NewClientWithRateLimit(backend, "crates:", cacheTTL, headers, rl.RequestsPerSecond, rl.Burst),
		baseURL: "https://crates.io/api/v1",
	}
}

// FetchCrate retrieves metadata for a Rust crate from crates.io (latest version).
//
// The crate parameter is case-sensitive and must match the published crate name exactly.
// Crate name cannot be empty; an empty string will result in an API error.
//
// If refresh is true, the cache is bypassed and a fresh API call is made.
// If refresh is false, cached data is returned if available and not expired.
//
// Dependency fetching failures are silently ignored; Dependencies will be empty/nil
// if the secondary API call fails. This is not considered an error.
//
// Returns:
//   - CrateInfo populated with metadata on success
//   - [integrations.ErrNotFound] if the crate doesn't exist
//   - [integrations.ErrNetwork] for HTTP failures (timeout, 5xx, etc.)
//   - Other errors for JSON decoding failures
//
// The returned CrateInfo pointer is never nil if err is nil.
// This method is safe for concurrent use.
func (c *Client) FetchCrate(ctx context.Context, crate string, refresh bool) (*CrateInfo, error) {
	key := crate

	var info CrateInfo
	err := c.Cached(ctx, key, refresh, &info, func() error {
		return c.fetch(ctx, crate, "", &info)
	})
	if err != nil {
		return nil, err
	}
	return &info, nil
}

// FetchCrateVersion retrieves metadata for a specific version of a Rust crate.
//
// The crate parameter is case-sensitive. The version must be an exact version
// string (e.g., "1.0.193").
//
// If refresh is true, the cache is bypassed and a fresh API call is made.
//
// Returns:
//   - CrateInfo populated with metadata on success
//   - [integrations.ErrNotFound] if the crate or version doesn't exist
//   - [integrations.ErrNetwork] for HTTP failures (timeout, 5xx, etc.)
//
// This method is safe for concurrent use.
func (c *Client) FetchCrateVersion(ctx context.Context, crate, version string, refresh bool) (*CrateInfo, error) {
	key := crate + "@" + version

	var info CrateInfo
	err := c.Cached(ctx, key, refresh, &info, func() error {
		return c.fetch(ctx, crate, version, &info)
	})
	if err != nil {
		return nil, err
	}
	return &info, nil
}

func (c *Client) fetch(ctx context.Context, crate, version string, info *CrateInfo) error {
	var data crateResponse
	if err := c.Get(ctx, fmt.Sprintf("%s/crates/%s", c.baseURL, crate), &data); err != nil {
		if errors.Is(err, integrations.ErrNotFound) {
			return fmt.Errorf("%w: crate %s", err, crate)
		}
		return err
	}

	targetVersion := version
	if targetVersion == "" {
		targetVersion = data.Crate.MaxVersion
	}

	deps, err := c.fetchDeps(ctx, crate, targetVersion)
	if err != nil {
		slog.Debug("crates: failed to fetch dependencies", "crate", crate, "version", targetVersion, "error", err)
	}

	// Fetch version-specific metadata (MSRV, license)
	// Some crates have null license at the crate level but valid license at the version level
	verMeta := c.fetchVersionMeta(ctx, crate, targetVersion)

	// Use crate-level license if available, otherwise fall back to version-specific license
	license := data.Crate.License
	if license == "" {
		license = verMeta.License
	}

	*info = CrateInfo{
		Name:         data.Crate.Name,
		Version:      targetVersion,
		Description:  data.Crate.Description,
		License:      license,
		Repository:   data.Crate.Repository,
		HomePage:     data.Crate.HomePage,
		Downloads:    data.Crate.Downloads,
		Dependencies: deps,
		MSRV:         verMeta.MSRV,
	}
	return nil
}

// versionMeta holds version-specific metadata that may not be available at the crate level.
type versionMeta struct {
	MSRV    string // Minimum Supported Rust Version
	License string // License for this specific version
}

// fetchVersionMeta retrieves version-specific metadata (MSRV, license) for a crate version.
// Some crates have null license at the crate level but valid license at the version level.
func (c *Client) fetchVersionMeta(ctx context.Context, crate, version string) versionMeta {
	url := fmt.Sprintf("%s/crates/%s/versions", c.baseURL, crate)
	var versionsResp versionsResponse
	if err := c.Get(ctx, url, &versionsResp); err != nil {
		return versionMeta{}
	}

	for _, v := range versionsResp.Versions {
		if v.Num == version {
			return versionMeta{MSRV: v.RustVersion, License: v.License}
		}
	}
	return versionMeta{}
}

func (c *Client) fetchDeps(ctx context.Context, crate, version string) ([]Dependency, error) {
	url := fmt.Sprintf("%s/crates/%s/%s/dependencies", c.baseURL, crate, version)

	var data depsResponse
	if err := c.Get(ctx, url, &data); err != nil {
		return nil, err
	}

	var deps []Dependency
	for _, d := range data.Dependencies {
		if d.Kind == "normal" && !d.Optional {
			deps = append(deps, Dependency{
				Name:       d.CrateID,
				Constraint: d.Req,
			})
		}
	}
	return deps, nil
}

// ListVersions returns all non-yanked versions for a crate, sorted semantically
// from oldest to newest.
func (c *Client) ListVersions(ctx context.Context, crate string, refresh bool) ([]string, error) {
	key := crate + ":versions"

	var versions []string
	err := c.Cached(ctx, key, refresh, &versions, func() error {
		var data crateResponse
		if err := c.Get(ctx, fmt.Sprintf("%s/crates/%s", c.baseURL, crate), &data); err != nil {
			if errors.Is(err, integrations.ErrNotFound) {
				return fmt.Errorf("%w: crate %s", err, crate)
			}
			return err
		}

		// Fetch all versions
		url := fmt.Sprintf("%s/crates/%s/versions", c.baseURL, crate)
		var versionsResp versionsResponse
		if err := c.Get(ctx, url, &versionsResp); err != nil {
			// Fallback: just return the max_version if versions endpoint fails
			versions = []string{data.Crate.MaxVersion}
			return nil
		}

		versions = make([]string, 0, len(versionsResp.Versions))
		for _, v := range versionsResp.Versions {
			if !v.Yanked {
				versions = append(versions, v.Num)
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

// ListVersionsWithConstraints returns all versions and their rust_version (MSRV).
// Returns a map of version -> rust_version (empty string if not specified).
func (c *Client) ListVersionsWithConstraints(ctx context.Context, crate string, refresh bool) (map[string]string, error) {
	key := crate + ":version_constraints"

	var result map[string]string
	err := c.Cached(ctx, key, refresh, &result, func() error {
		url := fmt.Sprintf("%s/crates/%s/versions", c.baseURL, crate)
		var versionsResp versionsResponse
		if err := c.Get(ctx, url, &versionsResp); err != nil {
			if errors.Is(err, integrations.ErrNotFound) {
				return fmt.Errorf("%w: crate %s", err, crate)
			}
			return err
		}

		result = make(map[string]string, len(versionsResp.Versions))
		for _, v := range versionsResp.Versions {
			if v.Yanked {
				continue
			}
			result[v.Num] = v.RustVersion
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

type crateResponse struct {
	Crate struct {
		Name        string `json:"name"`
		MaxVersion  string `json:"max_version"`
		Description string `json:"description"`
		License     string `json:"license"`
		Repository  string `json:"repository"`
		HomePage    string `json:"homepage"`
		Downloads   int    `json:"downloads"`
	} `json:"crate"`
}

type depsResponse struct {
	Dependencies []struct {
		CrateID  string `json:"crate_id"`
		Kind     string `json:"kind"`
		Optional bool   `json:"optional"`
		Req      string `json:"req"` // Version requirement (e.g., "^1.0")
	} `json:"dependencies"`
}

type versionsResponse struct {
	Versions []versionInfo `json:"versions"`
}

type versionInfo struct {
	Num         string `json:"num"`
	Yanked      bool   `json:"yanked"`
	RustVersion string `json:"rust_version"` // MSRV for this version
	License     string `json:"license"`      // License for this version (may differ from crate-level)
}
