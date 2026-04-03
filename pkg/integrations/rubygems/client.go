package rubygems

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/matzehuels/stacktower/pkg/cache"
	"github.com/matzehuels/stacktower/pkg/integrations"
)

// Dependency represents a gem dependency with version requirement.
type Dependency struct {
	Name       string // Gem name
	Constraint string // Version requirement (e.g., "~> 6.0", ">= 1.0, < 2.0")
}

// GemInfo holds metadata for a Ruby gem from RubyGems.
//
// Gem names are normalized to lowercase.
// Dependencies include only runtime dependencies; development dependencies are excluded.
//
// Zero values: All string fields are empty, Dependencies is nil, Downloads is 0.
// A Downloads value of 0 is valid for newly published gems.
// This struct is safe for concurrent reads after construction.
type GemInfo struct {
	Name                string       // Gem name, normalized lowercase (e.g., "rails", never empty in valid info)
	Version             string       // Current version (e.g., "7.1.2", never empty in valid info)
	Dependencies        []Dependency // Runtime dependencies with version requirements (nil or empty if none)
	SourceCodeURI       string       // Source code repository URL (may be empty)
	HomepageURI         string       // Homepage URL (may be empty)
	Description         string       // Gem description/info (may be empty)
	License             string       // License(s), comma-separated if multiple (may be empty)
	Downloads           int          // Total download count (0 for new gems)
	Authors             string       // Author name(s) (may be empty)
	RequiredRubyVersion string       // Required Ruby version constraint (e.g., ">= 3.0", may be empty)
}

// Client provides access to the RubyGems package registry API.
// It handles HTTP requests with caching and automatic retries.
//
// All methods are safe for concurrent use by multiple goroutines.
type Client struct {
	*integrations.Client
	baseURL string
}

// NewClient creates a RubyGems client with the given cache backend.
//
// Parameters:
//   - backend: Cache backend for HTTP response caching (use storage.NullBackend{} for no caching)
//   - cacheTTL: How long responses are cached (typical: 1-24 hours)
//
// The returned Client is safe for concurrent use.
func NewClient(backend cache.Cache, cacheTTL time.Duration) *Client {
	rl := integrations.DefaultRateLimits["rubygems"]
	return &Client{
		Client:  integrations.NewClientWithRateLimit(backend, "rubygems:", cacheTTL, nil, rl.RequestsPerSecond, rl.Burst),
		baseURL: "https://rubygems.org/api/v1",
	}
}

// FetchGem retrieves metadata for a Ruby gem from RubyGems (latest version).
//
// The gem parameter is normalized to lowercase with whitespace trimmed.
// Gem name cannot be empty; an empty string will result in an API error.
//
// If refresh is true, the cache is bypassed and a fresh API call is made.
// If refresh is false, cached data is returned if available and not expired.
//
// Returns:
//   - GemInfo populated with metadata on success
//   - [integrations.ErrNotFound] if the gem doesn't exist
//   - [integrations.ErrNetwork] for HTTP failures (timeout, 5xx, etc.)
//   - Other errors for JSON decoding failures
//
// The returned GemInfo pointer is never nil if err is nil.
// This method is safe for concurrent use.
func (c *Client) FetchGem(ctx context.Context, gem string, refresh bool) (*GemInfo, error) {
	gem = strings.ToLower(strings.TrimSpace(gem))
	key := gem

	var info GemInfo
	err := c.Cached(ctx, key, refresh, &info, func() error {
		return c.fetch(ctx, gem, "", &info)
	})
	if err != nil {
		return nil, err
	}
	return &info, nil
}

// FetchGemVersion retrieves metadata for a specific version of a Ruby gem.
//
// The gem parameter is normalized to lowercase. The version must be an exact version
// string (e.g., "7.1.2").
//
// If refresh is true, the cache is bypassed and a fresh API call is made.
//
// Returns:
//   - GemInfo populated with metadata on success
//   - [integrations.ErrNotFound] if the gem or version doesn't exist
//   - [integrations.ErrNetwork] for HTTP failures (timeout, 5xx, etc.)
//
// This method is safe for concurrent use.
func (c *Client) FetchGemVersion(ctx context.Context, gem, version string, refresh bool) (*GemInfo, error) {
	gem = strings.ToLower(strings.TrimSpace(gem))
	key := gem + "@" + version

	var info GemInfo
	err := c.Cached(ctx, key, refresh, &info, func() error {
		return c.fetch(ctx, gem, version, &info)
	})
	if err != nil {
		return nil, err
	}
	return &info, nil
}

func (c *Client) fetch(ctx context.Context, gem, version string, info *GemInfo) error {
	if version != "" {
		// Use V2 API for version-specific fetches (includes dependencies AND full metadata)
		return c.fetchVersionFromV2API(ctx, gem, version, info)
	}

	// For latest version, use the gems API (includes dependencies)
	url := fmt.Sprintf("%s/gems/%s.json", c.baseURL, gem)
	var data gemResponse
	if err := c.Get(ctx, url, &data); err != nil {
		if errors.Is(err, integrations.ErrNotFound) {
			return fmt.Errorf("%w: gem %s", err, gem)
		}
		return err
	}

	*info = GemInfo{
		Name:                data.Name,
		Version:             data.Version,
		Description:         data.Info,
		License:             strings.Join(data.Licenses, ", "),
		SourceCodeURI:       data.SourceCodeURI,
		HomepageURI:         data.HomepageURI,
		Downloads:           data.Downloads,
		Authors:             data.Authors,
		Dependencies:        runtimeDeps(data.Dependencies.Runtime),
		RequiredRubyVersion: data.RequiredRubyVersion,
	}
	return nil
}

// fetchVersionFromV2API uses the RubyGems V2 API to get version-specific info with dependencies and metadata.
// This API provides full gem metadata including source_code_uri, homepage_uri, dependencies, and ruby_version.
func (c *Client) fetchVersionFromV2API(ctx context.Context, gem, version string, info *GemInfo) error {
	url := fmt.Sprintf("https://rubygems.org/api/v2/rubygems/%s/versions/%s.json", gem, version)

	var data gemV2Response
	if err := c.Get(ctx, url, &data); err != nil {
		if errors.Is(err, integrations.ErrNotFound) {
			return fmt.Errorf("%w: gem %s version %s", err, gem, version)
		}
		return err
	}

	*info = GemInfo{
		Name:                data.Name,
		Version:             data.Version,
		Description:         data.Info,
		License:             strings.Join(data.Licenses, ", "),
		SourceCodeURI:       data.SourceCodeURI,
		HomepageURI:         data.HomepageURI,
		Downloads:           data.Downloads,
		Authors:             data.Authors,
		Dependencies:        runtimeDeps(data.Dependencies.Runtime),
		RequiredRubyVersion: data.RubyVersion,
	}
	return nil
}

// gemV2Response represents the JSON response from RubyGems V2 API for a specific version.
type gemV2Response struct {
	Name          string   `json:"name"`
	Version       string   `json:"version"`
	Info          string   `json:"info"`
	Authors       string   `json:"authors"`
	Licenses      []string `json:"licenses"`
	SourceCodeURI string   `json:"source_code_uri"`
	HomepageURI   string   `json:"homepage_uri"`
	Downloads     int      `json:"downloads"`
	RubyVersion   string   `json:"ruby_version"`
	Dependencies  struct {
		Runtime     []dependency `json:"runtime"`
		Development []dependency `json:"development"`
	} `json:"dependencies"`
}

func runtimeDeps(deps []dependency) []Dependency {
	seen := make(map[string]bool)
	var result []Dependency
	for _, d := range deps {
		name := strings.ToLower(strings.TrimSpace(d.Name))
		if !seen[name] {
			seen[name] = true
			result = append(result, Dependency{
				Name:       name,
				Constraint: d.Requirements,
			})
		}
	}
	return result
}

// ListVersions returns all available versions for a gem, sorted from oldest to newest.
func (c *Client) ListVersions(ctx context.Context, gem string, refresh bool) ([]string, error) {
	gem = strings.ToLower(strings.TrimSpace(gem))
	key := gem + ":versions"

	var versions []string
	err := c.Cached(ctx, key, refresh, &versions, func() error {
		url := fmt.Sprintf("%s/versions/%s.json", c.baseURL, gem)

		var versionsResp []gemVersionResponse
		if err := c.Get(ctx, url, &versionsResp); err != nil {
			if errors.Is(err, integrations.ErrNotFound) {
				return fmt.Errorf("%w: gem %s", err, gem)
			}
			return err
		}

		versions = make([]string, 0, len(versionsResp))
		for _, v := range versionsResp {
			versions = append(versions, v.Number)
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

// ListVersionsWithConstraints returns all versions and their Ruby runtime constraints.
// Returns a map of version -> required_ruby_version (empty string if not specified).
func (c *Client) ListVersionsWithConstraints(ctx context.Context, gem string, refresh bool) (map[string]string, error) {
	gem = strings.ToLower(strings.TrimSpace(gem))
	key := gem + ":version_constraints"

	var result map[string]string
	err := c.Cached(ctx, key, refresh, &result, func() error {
		url := fmt.Sprintf("%s/versions/%s.json", c.baseURL, gem)

		var versionsResp []gemVersionResponse
		if err := c.Get(ctx, url, &versionsResp); err != nil {
			if errors.Is(err, integrations.ErrNotFound) {
				return fmt.Errorf("%w: gem %s", err, gem)
			}
			return err
		}

		result = make(map[string]string, len(versionsResp))
		for _, v := range versionsResp {
			result[v.Number] = strings.TrimSpace(v.RequiredRubyVersion)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

type gemResponse struct {
	Name                string   `json:"name"`
	Version             string   `json:"version"`
	Info                string   `json:"info"`
	Licenses            []string `json:"licenses"`
	SourceCodeURI       string   `json:"source_code_uri"`
	HomepageURI         string   `json:"homepage_uri"`
	Downloads           int      `json:"downloads"`
	Authors             string   `json:"authors"`
	RequiredRubyVersion string   `json:"required_ruby_version"`
	Dependencies        struct {
		Runtime []dependency `json:"runtime"`
	} `json:"dependencies"`
}

type gemVersionResponse struct {
	Number              string   `json:"number"`
	Description         string   `json:"description"`
	Licenses            []string `json:"licenses"`
	Downloads           int      `json:"downloads_count"`
	Authors             string   `json:"authors"`
	RequiredRubyVersion string   `json:"ruby_version"`
	Dependencies        struct {
		Runtime []dependency `json:"runtime"`
	} `json:"dependencies"`
	Metadata struct {
		SourceCodeURI string `json:"source_code_uri"`
		HomepageURI   string `json:"homepage_uri"`
	} `json:"metadata"`
}

type dependency struct {
	Name         string `json:"name"`
	Requirements string `json:"requirements"` // Version requirement (e.g., "~> 6.0")
}

func rubyVersionsEquivalent(a, b string) bool {
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
