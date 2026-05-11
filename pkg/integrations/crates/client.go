package crates

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/stacktower-io/stacktower/pkg/cache"
	"github.com/stacktower-io/stacktower/pkg/integrations"
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
	baseURL  string
	indexURL string // sparse index base URL (defaults to indexBaseURL)
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
		"User-Agent": "stacktower/1.0 (https://github.com/stacktower-io/stacktower)",
	}
	rl := integrations.DefaultRateLimits["crates"]
	return &Client{
		Client:   integrations.NewClientWithRateLimit(backend, "crates:", cacheTTL, headers, rl.RequestsPerSecond, rl.Burst),
		baseURL:  "https://crates.io/api/v1",
		indexURL: indexBaseURL,
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
		return c.fetch(ctx, crate, "", refresh, &info)
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
		return c.fetch(ctx, crate, version, refresh, &info)
	})
	if err != nil {
		return nil, err
	}
	return &info, nil
}

// FetchCrateVersionFromIndex returns package info using only the sparse index.
// This is much faster than FetchCrateVersion because it avoids the REST API
// call for metadata (description, license, downloads, etc.). Use this during
// dependency resolution where only deps and MSRV are needed.
func (c *Client) FetchCrateVersionFromIndex(ctx context.Context, crate, version string, refresh bool) (*CrateInfo, error) {
	entries, err := c.fetchIndex(ctx, crate, refresh)
	if err != nil {
		return nil, err
	}

	for i := range entries {
		if entries[i].Version == version {
			return &CrateInfo{
				Name:         entries[i].Name,
				Version:      version,
				Dependencies: indexEntryToDeps(entries[i]),
				MSRV:         entries[i].RustVersion,
			}, nil
		}
	}
	return nil, fmt.Errorf("%w: crate %s version %s", integrations.ErrNotFound, crate, version)
}

func (c *Client) fetch(ctx context.Context, crate, version string, refresh bool, info *CrateInfo) error {
	entries, err := c.fetchIndex(ctx, crate, refresh)
	if err != nil {
		if errors.Is(err, integrations.ErrNotFound) {
			return fmt.Errorf("%w: crate %s", err, crate)
		}
		return err
	}

	targetVersion := version
	if targetVersion == "" {
		for i := len(entries) - 1; i >= 0; i-- {
			if !entries[i].Yanked {
				targetVersion = entries[i].Version
				break
			}
		}
		if targetVersion == "" {
			return fmt.Errorf("no non-yanked versions found for %s", crate)
		}
	}

	var entry *indexEntry
	for i := range entries {
		if entries[i].Version == targetVersion {
			entry = &entries[i]
			break
		}
	}
	if entry == nil {
		return fmt.Errorf("%w: crate %s version %s", integrations.ErrNotFound, crate, targetVersion)
	}

	deps := indexEntryToDeps(*entry)

	data, err := c.fetchCrateResponse(ctx, crate, refresh)
	if err != nil {
		slog.Debug("crates: failed to fetch crate metadata from REST API", "crate", crate, "error", err)
		*info = CrateInfo{
			Name:         crate,
			Version:      targetVersion,
			Dependencies: deps,
			MSRV:         entry.RustVersion,
		}
		return nil
	}

	license := data.Crate.License
	if license == "" {
		license = entry.License
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
		MSRV:         entry.RustVersion,
	}
	return nil
}

func (c *Client) fetchCrateResponse(ctx context.Context, crate string, refresh bool) (crateResponse, error) {
	key := crate + ":metadata"

	var data crateResponse
	err := c.Cached(ctx, key, refresh, &data, func() error {
		return c.Get(ctx, fmt.Sprintf("%s/crates/%s", c.baseURL, crate), &data)
	})
	return data, err
}

// ---------------------------------------------------------------------------
// Sparse index: https://index.crates.io/
// One GET per crate returns ALL versions + ALL deps in NDJSON format.
// ---------------------------------------------------------------------------

const indexBaseURL = "https://index.crates.io"

func sparseIndexPath(crate string) string {
	name := strings.ToLower(crate)
	n := len(name)
	switch {
	case n <= 0:
		return ""
	case n == 1:
		return "/1/" + name
	case n == 2:
		return "/2/" + name
	case n == 3:
		return "/3/" + name[:1] + "/" + name
	default:
		return "/" + name[:2] + "/" + name[2:4] + "/" + name
	}
}

func sparseIndexURL(crate string) string {
	return indexBaseURL + sparseIndexPath(crate)
}

type indexEntry struct {
	Name        string     `json:"name"`
	Version     string     `json:"vers"`
	Deps        []indexDep `json:"deps"`
	Yanked      bool       `json:"yanked"`
	RustVersion string     `json:"rust_version"`
	License     string     `json:"license"`
}

type indexDep struct {
	Name     string  `json:"name"`
	Req      string  `json:"req"`
	Kind     string  `json:"kind"`
	Optional bool    `json:"optional"`
	Package  *string `json:"package"` // actual crate name if renamed
}

func (c *Client) fetchIndex(ctx context.Context, crate string, refresh bool) ([]indexEntry, error) {
	key := crate + ":index"

	var entries []indexEntry
	err := c.Cached(ctx, key, refresh, &entries, func() error {
		path := sparseIndexPath(crate)
		if path == "" {
			return fmt.Errorf("invalid crate name: %q", crate)
		}
		url := c.indexURL + path
		text, err := c.GetText(ctx, url)
		if err != nil {
			return err
		}

		lines := strings.Split(strings.TrimSpace(text), "\n")
		entries = make([]indexEntry, 0, len(lines))
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			var e indexEntry
			if err := json.Unmarshal([]byte(line), &e); err != nil {
				continue
			}
			entries = append(entries, e)
		}
		return nil
	})
	return entries, err
}

func indexEntryToDeps(entry indexEntry) []Dependency {
	var deps []Dependency
	for _, d := range entry.Deps {
		if d.Kind != "normal" || d.Optional {
			continue
		}
		name := d.Name
		if d.Package != nil && *d.Package != "" {
			name = *d.Package
		}
		deps = append(deps, Dependency{
			Name:       name,
			Constraint: d.Req,
		})
	}
	return deps
}

// ListVersions returns all non-yanked versions for a crate, sorted semantically
// from oldest to newest.
func (c *Client) ListVersions(ctx context.Context, crate string, refresh bool) ([]string, error) {
	key := crate + ":versions"

	var versions []string
	err := c.Cached(ctx, key, refresh, &versions, func() error {
		entries, err := c.fetchIndex(ctx, crate, refresh)
		if err != nil {
			if errors.Is(err, integrations.ErrNotFound) {
				return fmt.Errorf("%w: crate %s", err, crate)
			}
			return err
		}

		versions = make([]string, 0, len(entries))
		for _, e := range entries {
			if !e.Yanked {
				versions = append(versions, e.Version)
			}
		}
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
		entries, err := c.fetchIndex(ctx, crate, refresh)
		if err != nil {
			if errors.Is(err, integrations.ErrNotFound) {
				return fmt.Errorf("%w: crate %s", err, crate)
			}
			return err
		}

		result = make(map[string]string, len(entries))
		for _, e := range entries {
			if !e.Yanked {
				result[e.Version] = e.RustVersion
			}
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
