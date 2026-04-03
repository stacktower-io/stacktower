package maven

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/matzehuels/stacktower/pkg/cache"
	"github.com/matzehuels/stacktower/pkg/integrations"
)

// Dependency represents a Maven dependency with version information.
type Dependency struct {
	Name       string // Maven coordinate (groupId:artifactId)
	Constraint string // Version from POM (may contain Maven properties or ranges)
}

// ArtifactInfo holds metadata for a Java artifact from Maven Central.
//
// Artifacts are identified by "groupId:artifactId" coordinates.
// Dependencies include only compile-scope dependencies; test, provided, and optional deps are excluded.
// Dependencies with unresolved Maven properties (${...}) are skipped.
//
// Zero values: All string fields are empty, Dependencies is nil.
// This struct is safe for concurrent reads after construction.
type ArtifactInfo struct {
	GroupID      string       // Maven groupId (e.g., "com.google.guava", never empty in valid info)
	ArtifactID   string       // Maven artifactId (e.g., "guava", never empty in valid info)
	Version      string       // Latest version (e.g., "32.1.3-jre", never empty in valid info)
	Dependencies []Dependency // Compile-scope dependencies with versions (nil or empty if none or POM fetch failed)
	Description  string       // Artifact description from POM (may be empty)
	Repository   string       // Source code repository URL from POM SCM section (may be empty)
	HomePage     string       // Project homepage URL from POM (may be empty, often same as Repository)
	License      string       // License name from POM (may be empty)
	LicenseURL   string       // License URL from POM (may be empty)
}

// Coordinate returns the Maven coordinate string "groupId:artifactId".
// Example: "com.google.guava:guava"
func (a *ArtifactInfo) Coordinate() string {
	return a.GroupID + ":" + a.ArtifactID
}

// Client provides access to the Maven Central repository API.
// It handles HTTP requests with caching and automatic retries.
//
// All methods are safe for concurrent use by multiple goroutines.
type Client struct {
	*integrations.Client
	baseURL string
}

// NewClient creates a Maven Central client with the given cache backend.
//
// Parameters:
//   - backend: Cache backend for HTTP response caching (use storage.NullBackend{} for no caching)
//   - cacheTTL: How long responses are cached (typical: 1-24 hours)
//
// The returned Client is safe for concurrent use.
func NewClient(backend cache.Cache, cacheTTL time.Duration) *Client {
	rl := integrations.DefaultRateLimits["maven"]
	return &Client{
		Client:  integrations.NewClientWithRateLimit(backend, "maven:", cacheTTL, nil, rl.RequestsPerSecond, rl.Burst),
		baseURL: "https://search.maven.org/solrsearch/select",
	}
}

// FetchArtifact retrieves metadata for a Java artifact from Maven Central (latest version).
//
// The coordinate parameter must be in the format "groupId:artifactId".
// Examples: "com.google.guava:guava", "org.apache.commons:commons-lang3"
// Coordinate cannot be empty or missing the colon separator.
//
// If refresh is true, the cache is bypassed and a fresh API call is made.
// If refresh is false, cached data is returned if available and not expired.
//
// This method performs two API calls:
//  1. Maven Central Search API to find the latest version
//  2. Direct POM fetch to extract dependencies
//
// POM fetch failures are silently ignored; Dependencies will be empty/nil if it fails.
//
// Returns:
//   - ArtifactInfo populated with metadata on success
//   - [integrations.ErrNotFound] if the artifact doesn't exist
//   - [integrations.ErrNetwork] for HTTP failures (timeout, 5xx, etc.)
//   - Error if coordinate format is invalid
//   - Other errors for JSON decoding failures
//
// The returned ArtifactInfo pointer is never nil if err is nil.
// This method is safe for concurrent use.
func (c *Client) FetchArtifact(ctx context.Context, coordinate string, refresh bool) (*ArtifactInfo, error) {
	groupID, artifactID, err := parseCoordinate(coordinate)
	if err != nil {
		return nil, err
	}

	key := coordinate

	var info ArtifactInfo
	err = c.Cached(ctx, key, refresh, &info, func() error {
		return c.fetch(ctx, groupID, artifactID, "", &info)
	})
	if err != nil {
		return nil, err
	}
	return &info, nil
}

// FetchArtifactVersion retrieves metadata for a specific version of a Java artifact.
//
// The coordinate parameter must be in the format "groupId:artifactId".
// The version must be an exact version string (e.g., "32.1.3-jre").
//
// If refresh is true, the cache is bypassed and a fresh API call is made.
//
// Returns:
//   - ArtifactInfo populated with metadata on success
//   - [integrations.ErrNotFound] if the artifact or version doesn't exist
//   - [integrations.ErrNetwork] for HTTP failures (timeout, 5xx, etc.)
//
// This method is safe for concurrent use.
func (c *Client) FetchArtifactVersion(ctx context.Context, coordinate, version string, refresh bool) (*ArtifactInfo, error) {
	groupID, artifactID, err := parseCoordinate(coordinate)
	if err != nil {
		return nil, err
	}

	key := coordinate + "@" + version

	var info ArtifactInfo
	err = c.Cached(ctx, key, refresh, &info, func() error {
		return c.fetch(ctx, groupID, artifactID, version, &info)
	})
	if err != nil {
		return nil, err
	}
	return &info, nil
}

func (c *Client) fetch(ctx context.Context, groupID, artifactID, version string, info *ArtifactInfo) error {
	targetVersion := version

	if targetVersion == "" {
		// Get the latest version from search API
		query := fmt.Sprintf("g:%q AND a:%q", groupID, artifactID)
		url := fmt.Sprintf("%s?q=%s&rows=1&wt=json", c.baseURL, integrations.URLEncode(query))

		var searchResp searchResponse
		if err := c.Get(ctx, url, &searchResp); err != nil {
			if errors.Is(err, integrations.ErrNotFound) {
				return fmt.Errorf("%w: maven artifact %s:%s", err, groupID, artifactID)
			}
			return err
		}

		if searchResp.Response.NumFound == 0 {
			return fmt.Errorf("%w: maven artifact %s:%s", integrations.ErrNotFound, groupID, artifactID)
		}

		doc := searchResp.Response.Docs[0]
		targetVersion = doc.LatestVersion
		if targetVersion == "" {
			targetVersion = doc.Version
		}
	}

	// Fetch POM to get dependencies, URLs, and license
	pomData := c.fetchPOMDeps(ctx, groupID, artifactID, targetVersion)

	*info = ArtifactInfo{
		GroupID:      groupID,
		ArtifactID:   artifactID,
		Version:      targetVersion,
		Dependencies: pomData.Dependencies,
		Repository:   pomData.Repository,
		HomePage:     pomData.HomePage,
		License:      pomData.License,
		LicenseURL:   pomData.LicenseURL,
	}
	return nil
}

// ListVersions returns all available versions for an artifact, sorted from oldest to newest.
func (c *Client) ListVersions(ctx context.Context, coordinate string, refresh bool) ([]string, error) {
	groupID, artifactID, err := parseCoordinate(coordinate)
	if err != nil {
		return nil, err
	}

	key := coordinate + ":versions"

	var versions []string
	err = c.Cached(ctx, key, refresh, &versions, func() error {
		// Fetch from maven-metadata.xml which contains all versions
		groupPath := strings.ReplaceAll(groupID, ".", "/")
		metadataURL := fmt.Sprintf("https://repo1.maven.org/maven2/%s/%s/maven-metadata.xml",
			groupPath, artifactID)

		body, err := c.GetText(ctx, metadataURL)
		if err != nil {
			// Fallback to search API if metadata not available
			return c.listVersionsFromSearch(ctx, groupID, artifactID, &versions)
		}

		// Parse maven-metadata.xml
		var metadata struct {
			Versioning struct {
				Versions []string `xml:"versions>version"`
			} `xml:"versioning"`
		}
		if err := xml.Unmarshal([]byte(body), &metadata); err != nil {
			return c.listVersionsFromSearch(ctx, groupID, artifactID, &versions)
		}

		versions = metadata.Versioning.Versions
		// Sort versions semantically (oldest to newest)
		integrations.SortVersions(versions)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return versions, nil
}

func (c *Client) listVersionsFromSearch(ctx context.Context, groupID, artifactID string, versions *[]string) error {
	query := fmt.Sprintf("g:%q AND a:%q", groupID, artifactID)
	url := fmt.Sprintf("%s?q=%s&rows=100&core=gav&wt=json", c.baseURL, integrations.URLEncode(query))

	var searchResp searchResponse
	if err := c.Get(ctx, url, &searchResp); err != nil {
		return err
	}

	*versions = make([]string, 0, len(searchResp.Response.Docs))
	for _, doc := range searchResp.Response.Docs {
		if doc.Version != "" {
			*versions = append(*versions, doc.Version)
		}
	}
	return nil
}

// pomInfo holds extracted information from a POM file.
type pomInfo struct {
	Dependencies []Dependency
	Repository   string // SCM repository URL (GitHub, GitLab, etc.)
	HomePage     string // Project homepage URL
	License      string
	LicenseURL   string
}

func (c *Client) fetchPOMDeps(ctx context.Context, groupID, artifactID, version string) *pomInfo {
	groupPath := strings.ReplaceAll(groupID, ".", "/")
	pomURL := fmt.Sprintf("https://repo1.maven.org/maven2/%s/%s/%s/%s-%s.pom",
		groupPath, artifactID, version, artifactID, version)

	pom, err := c.fetchPOM(ctx, pomURL)
	if err != nil {
		slog.Debug("maven: failed to fetch POM", "group", groupID, "artifact", artifactID, "version", version, "error", err)
		return &pomInfo{}
	}

	repo, home := extractURLs(pom)
	info := &pomInfo{
		Dependencies: extractDeps(pom),
		Repository:   repo,
		HomePage:     home,
	}

	// Extract license info
	if len(pom.Licenses) > 0 {
		info.License = pom.Licenses[0].Name
		info.LicenseURL = pom.Licenses[0].URL
	}

	return info
}

// extractURLs extracts repository and homepage URLs from a POM.
// Returns (repository, homepage) where repository is from SCM section
// and homepage is from the project URL field.
func extractURLs(pom *pomProject) (repository, homepage string) {
	homepage = pom.URL

	// Extract SCM URL (source code repository)
	if pom.SCM != nil {
		// Prefer scm.url (browser-viewable)
		if pom.SCM.URL != "" {
			repository = integrations.NormalizeRepoURL(pom.SCM.URL)
		} else if pom.SCM.DeveloperConnection != "" {
			// Fall back to developer connection (git@... or https://...)
			repository = integrations.NormalizeRepoURL(pom.SCM.DeveloperConnection)
		} else if pom.SCM.Connection != "" {
			// Last resort: connection string
			repository = integrations.NormalizeRepoURL(pom.SCM.Connection)
		}
	}

	// If no SCM but homepage looks like a repo URL, use it as repository
	if repository == "" && homepage != "" {
		normalized := integrations.NormalizeRepoURL(homepage)
		if isRepoURL(normalized) {
			repository = normalized
		}
	}

	return repository, homepage
}

// isRepoURL checks if a URL looks like a source code repository.
func isRepoURL(url string) bool {
	for _, host := range []string{"github.com/", "gitlab.com/", "bitbucket.org/", "codeberg.org/"} {
		if strings.Contains(url, host) {
			return true
		}
	}
	return false
}

func (c *Client) fetchPOM(ctx context.Context, url string) (*pomProject, error) {
	text, err := c.GetText(ctx, url)
	if err != nil {
		return nil, err
	}

	var pom pomProject
	if err := xml.Unmarshal([]byte(text), &pom); err != nil {
		return nil, err
	}
	return &pom, nil
}

func extractDeps(pom *pomProject) []Dependency {
	var deps []Dependency
	seen := make(map[string]bool)

	for _, dep := range pom.Dependencies {
		if dep.Scope == "test" || dep.Scope == "provided" || dep.Optional == "true" {
			continue
		}
		// Skip dependencies with unresolved properties in groupId or artifactId
		if strings.HasPrefix(dep.GroupID, "${") || strings.HasPrefix(dep.ArtifactID, "${") {
			continue
		}
		coord := dep.GroupID + ":" + dep.ArtifactID
		if !seen[coord] {
			seen[coord] = true
			// Include version as constraint (may contain properties like ${version})
			deps = append(deps, Dependency{
				Name:       coord,
				Constraint: dep.Version,
			})
		}
	}
	return deps
}

func parseCoordinate(coord string) (groupID, artifactID string, err error) {
	parts := strings.Split(coord, ":")
	if len(parts) < 2 {
		return "", "", fmt.Errorf("invalid maven coordinate %q (expected groupId:artifactId)", coord)
	}
	return parts[0], parts[1], nil
}

type searchResponse struct {
	Response struct {
		NumFound int         `json:"numFound"`
		Docs     []searchDoc `json:"docs"`
	} `json:"response"`
}

type searchDoc struct {
	GroupID       string `json:"g"`
	ArtifactID    string `json:"a"`
	Version       string `json:"v"`
	LatestVersion string `json:"latestVersion"`
}

type pomProject struct {
	GroupID      string          `xml:"groupId"`
	ArtifactID   string          `xml:"artifactId"`
	Version      string          `xml:"version"`
	Name         string          `xml:"name"`
	Description  string          `xml:"description"`
	URL          string          `xml:"url"`
	SCM          *pomSCM         `xml:"scm"`
	Licenses     []pomLicense    `xml:"licenses>license"`
	Dependencies []pomDependency `xml:"dependencies>dependency"`
}

type pomSCM struct {
	Connection          string `xml:"connection"`
	DeveloperConnection string `xml:"developerConnection"`
	URL                 string `xml:"url"`
}

type pomLicense struct {
	Name string `xml:"name"`
	URL  string `xml:"url"`
}

type pomDependency struct {
	GroupID    string `xml:"groupId"`
	ArtifactID string `xml:"artifactId"`
	Version    string `xml:"version"`
	Scope      string `xml:"scope"`
	Optional   string `xml:"optional"`
}
