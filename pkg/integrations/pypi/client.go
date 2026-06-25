package pypi

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/stacktower-io/stacktower/pkg/cache"
	"github.com/stacktower-io/stacktower/pkg/core/deps/constraints"
	"github.com/stacktower-io/stacktower/pkg/integrations"
)

var (
	// depRE matches the package name at the start of a requirement
	depRE = regexp.MustCompile(`^([a-zA-Z0-9][a-zA-Z0-9._-]*)`)
	// constraintRE matches version constraints after the package name
	// Handles: ==1.0, >=1.0,<2.0, ~=1.4, etc.
	constraintRE = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*\s*(\[.*?\])?\s*((?:[<>=!~]=?|===?)[^;]+)?`)
	markerRE     = regexp.MustCompile(`;\s*(.+)`)
	skipRE       = regexp.MustCompile(`extra|dev|test`)
	// pythonVersionRE matches python_version markers like: python_version < "3.11"
	pythonVersionRE = regexp.MustCompile(`python_version\s*([<>=!]+)\s*["'](\d+(?:\.\d+)?)["']`)
	// envMarkerRE matches string-valued environment markers like:
	// sys_platform == "linux", os_name != 'nt', platform_machine == "x86_64"
	envMarkerRE = regexp.MustCompile(`(sys_platform|os_name|platform_machine)\s*(==|!=)\s*["']([^"']*)["']`)
)

// DefaultPythonVersion is the default assumed Python version for marker evaluation.
// Dependencies with markers like `python_version < "3.11"` are skipped.
const DefaultPythonVersion = "3.11"

// Dependency represents a package dependency with version constraint.
type Dependency struct {
	Name       string // Normalized package name
	Constraint string // Version constraint (e.g., ">=1.0,<2.0"), empty for no constraint
}

// PackageInfo holds metadata for a Python package from PyPI.
//
// Package names are normalized following PEP 503 (lowercase, underscores→hyphens).
// Dependencies list only runtime dependencies; extras, dev, and test deps are excluded.
//
// Zero values: All string fields are empty, Dependencies is nil.
// A nil Dependencies slice is valid and indicates no dependencies or failed dependency fetch.
// This struct is safe for concurrent reads after construction.
type PackageInfo struct {
	Name           string            // Normalized package name (e.g., "fastapi", never empty in valid info)
	Version        string            // Version string (e.g., "0.104.1", never empty in valid info)
	RequiresPython string            // Python version constraint (e.g., ">=3.8", may be empty)
	Dependencies   []Dependency      // Direct runtime dependencies with constraints (nil or empty if none)
	ProjectURLs    map[string]string // Project URLs from metadata (e.g., "Homepage", "Repository", may be nil)
	HomePage       string            // Homepage URL (may be empty)
	Summary        string            // Short package description (may be empty)
	License        string            // License name or expression (may be empty)
	LicenseText    string            // Full raw license text for custom/proprietary licenses (may be empty)
	Author         string            // Author name (may be empty)
}

// Client provides access to the PyPI package registry API.
// It handles HTTP requests with caching and automatic retries.
//
// All methods are safe for concurrent use by multiple goroutines.
type Client struct {
	*integrations.Client
	baseURL       string
	pythonVersion string // Target Python version for marker evaluation
}

// NewClient creates a PyPI client with the given cache backend.
//
// Parameters:
//   - backend: Cache backend for HTTP response caching (use storage.NullBackend{} for no caching)
//   - cacheTTL: How long responses are cached (typical: 1-24 hours)
//   - pythonVersion: Target Python version for marker evaluation (e.g., "3.11"); empty uses default
//
// The returned Client is safe for concurrent use.
func NewClient(backend cache.Cache, cacheTTL time.Duration, pythonVersion string) *Client {
	rl := integrations.DefaultRateLimits["pypi"]
	pv := pythonVersion
	if pv == "" {
		pv = DefaultPythonVersion
	}
	return &Client{
		Client:        integrations.NewClientWithRateLimit(backend, "pypi:", cacheTTL, nil, rl.RequestsPerSecond, rl.Burst),
		baseURL:       "https://pypi.org/pypi",
		pythonVersion: pv,
	}
}

// FetchPackage retrieves metadata for a Python package from PyPI (latest version).
//
// The pkg parameter is normalized automatically (case-insensitive, underscores→hyphens).
// Package name cannot be empty; an empty string will result in an API error.
//
// If refresh is true, the cache is bypassed and a fresh API call is made.
// If refresh is false, cached data is returned if available and not expired.
//
// Returns:
//   - PackageInfo populated with metadata on success
//   - [integrations.ErrNotFound] if the package doesn't exist
//   - [integrations.ErrNetwork] for HTTP failures (timeout, 5xx, etc.)
//   - Other errors for JSON decoding failures
//
// The returned PackageInfo pointer is never nil if err is nil.
// This method is safe for concurrent use.
func (c *Client) FetchPackage(ctx context.Context, pkg string, refresh bool) (*PackageInfo, error) {
	pkg = integrations.NormalizePkgName(pkg)

	var info PackageInfo
	if err := c.fetch(ctx, pkg, "", refresh, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

// FetchPackageVersion retrieves metadata for a specific version of a Python package.
//
// The pkg parameter is normalized automatically. The version must be an exact version
// string (e.g., "2.31.0").
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
	pkg = integrations.NormalizePkgName(pkg)
	key := pkg + "@" + version

	var info PackageInfo
	err := c.Cached(ctx, key, refresh, &info, func() error {
		return c.fetch(ctx, pkg, version, refresh, &info)
	})
	if err != nil {
		return nil, err
	}
	return &info, nil
}

// fetchProjectResponse fetches and caches the full project JSON for a package.
// Both ListVersions and ListVersionsWithConstraints derive from this single
// cached response, eliminating duplicate HTTP calls to the same endpoint.
func (c *Client) fetchProjectResponse(ctx context.Context, pkg string, refresh bool) (apiResponse, error) {
	key := pkg + ":project_response"

	var data apiResponse
	err := c.Cached(ctx, key, refresh, &data, func() error {
		if err := c.Get(ctx, fmt.Sprintf("%s/%s/json", c.baseURL, pkg), &data); err != nil {
			if errors.Is(err, integrations.ErrNotFound) {
				return fmt.Errorf("%w: pypi package %s", err, pkg)
			}
			return err
		}
		return nil
	})
	return data, err
}

func apiResponseToInfo(data apiResponse, c *Client) PackageInfo {
	urls := make(map[string]string, len(data.Info.ProjectURLs))
	for k, v := range data.Info.ProjectURLs {
		if s, ok := v.(string); ok {
			urls[k] = s
		}
	}

	licenseType := extractLicenseType(data.Info.License, data.Info.LicenseExpression, data.Info.Classifiers)
	licenseText := ""
	if len(data.Info.License) > 100 || strings.Contains(data.Info.License, "\n") {
		licenseText = data.Info.License
	}

	return PackageInfo{
		Name:           data.Info.Name,
		Version:        data.Info.Version,
		RequiresPython: data.Info.RequiresPython,
		Summary:        data.Info.Summary,
		License:        licenseType,
		LicenseText:    licenseText,
		Dependencies:   c.extractDeps(data.Info.RequiresDist),
		ProjectURLs:    urls,
		HomePage:       data.Info.HomePage,
		Author:         data.Info.Author,
	}
}

func (c *Client) fetch(ctx context.Context, pkg, version string, refresh bool, info *PackageInfo) error {
	if version != "" {
		// Per-version endpoint has version-specific metadata (deps, requires_python).
		// Escape the version: PEP 440 local versions contain '+' (e.g. "1.0+cu118")
		// which must not be interpolated into the path unescaped.
		endpoint := fmt.Sprintf("%s/%s/%s/json", c.baseURL, pkg, url.PathEscape(version))

		var data apiResponse
		if err := c.Get(ctx, endpoint, &data); err != nil {
			if errors.Is(err, integrations.ErrNotFound) {
				return fmt.Errorf("%w: pypi package %s version %s", err, pkg, version)
			}
			return err
		}

		*info = apiResponseToInfo(data, c)
		return nil
	}

	data, err := c.fetchProjectResponse(ctx, pkg, refresh)
	if err != nil {
		return err
	}

	*info = apiResponseToInfo(data, c)
	return nil
}

func (c *Client) extractDeps(requires []string) []Dependency {
	seen := make(map[string]bool)
	var deps []Dependency
	for _, req := range requires {
		// Check markers
		if m := markerRE.FindStringSubmatch(req); len(m) > 1 {
			marker := m[1]
			// Skip extras, dev, and test dependencies
			if skipRE.MatchString(marker) {
				continue
			}
			// Skip dependencies with python_version markers that don't match target
			if !evaluatePythonVersionMarker(marker, c.pythonVersion) {
				continue
			}
		}

		// Extract package name
		nameMatch := depRE.FindStringSubmatch(req)
		if len(nameMatch) < 2 {
			continue
		}
		name := integrations.NormalizePkgName(nameMatch[1])

		// Skip if already seen
		if seen[name] {
			continue
		}
		seen[name] = true

		// Extract version constraint
		constraint := ""
		if m := constraintRE.FindStringSubmatch(req); len(m) > 2 && m[2] != "" {
			// m[2] contains the version specifier part (e.g., ">=1.0,<2.0")
			constraint = strings.TrimSpace(m[2])
		}

		deps = append(deps, Dependency{Name: name, Constraint: constraint})
	}
	return deps
}

// defaultEnvMarkers returns the environment marker values used to evaluate
// sys_platform / os_name / platform_machine conditions, derived from the
// current OS and architecture (PEP 508 marker names).
func defaultEnvMarkers() map[string]string {
	sysPlatform := runtime.GOOS // "linux", "darwin"
	osName := "posix"
	if runtime.GOOS == "windows" {
		sysPlatform = "win32"
		osName = "nt"
	}

	machine := runtime.GOARCH
	switch runtime.GOARCH {
	case "amd64":
		machine = "x86_64"
	case "arm64":
		if runtime.GOOS == "linux" {
			machine = "aarch64"
		}
	case "386":
		machine = "i386"
	}

	return map[string]string{
		"sys_platform":     sysPlatform,
		"os_name":          osName,
		"platform_machine": machine,
	}
}

// evaluatePythonVersionMarker checks if a marker's python_version and
// platform conditions are satisfied. Returns true if the dependency should
// be included (all recognized conditions are satisfied or the marker has no
// recognized conditions).
//
// Supports "and" / "or" boolean connectives. python_version conditions are
// compared against the target Python version; sys_platform / os_name /
// platform_machine conditions are compared against the current OS defaults.
// Unrecognized markers are ignored (treated as satisfied).
func evaluatePythonVersionMarker(marker string, pythonVersion string) bool {
	return evaluateMarker(marker, pythonVersion, defaultEnvMarkers())
}

// evaluateMarker is the env-injectable core of [evaluatePythonVersionMarker].
func evaluateMarker(marker string, pythonVersion string, env map[string]string) bool {
	// Split on " or " first: any OR-group matching means include.
	orGroups := splitMarkerOr(marker)
	for _, group := range orGroups {
		if evaluateMarkerAndGroup(group, pythonVersion, env) {
			return true
		}
	}
	return len(orGroups) == 0
}

// splitMarkerOr splits a marker expression on top-level " or " connectives,
// respecting parenthesised sub-expressions.
func splitMarkerOr(marker string) []string {
	var groups []string
	depth := 0
	start := 0
	lower := strings.ToLower(marker)
	for i := 0; i < len(lower); i++ {
		switch lower[i] {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		}
		if depth == 0 && i+4 <= len(lower) && lower[i:i+4] == " or " {
			groups = append(groups, strings.TrimSpace(marker[start:i]))
			start = i + 4
		}
	}
	tail := strings.TrimSpace(marker[start:])
	if tail != "" {
		groups = append(groups, tail)
	}
	return groups
}

// evaluateMarkerAndGroup evaluates a single AND-connected group of marker
// conditions. All recognized conditions (python_version plus string-valued
// environment markers) must be satisfied.
func evaluateMarkerAndGroup(group string, pythonVersion string, env map[string]string) bool {
	for _, m := range envMarkerRE.FindAllStringSubmatch(group, -1) {
		name, op, want := m[1], m[2], m[3]
		got, known := env[name]
		if !known {
			continue
		}
		if (op == "==") != (got == want) {
			return false
		}
	}

	matches := pythonVersionRE.FindAllStringSubmatch(group, -1)
	if len(matches) == 0 {
		return true
	}

	targetParts := parseVersionParts(pythonVersion)

	for _, m := range matches {
		if len(m) < 3 {
			continue
		}
		op := m[1]
		verStr := m[2]
		verParts := parseVersionParts(verStr)

		cmp := compareVersionParts(targetParts, verParts)

		var satisfied bool
		switch op {
		case "<":
			satisfied = cmp < 0
		case "<=":
			satisfied = cmp <= 0
		case ">":
			satisfied = cmp > 0
		case ">=":
			satisfied = cmp >= 0
		case "==":
			satisfied = cmp == 0
		case "!=":
			satisfied = cmp != 0
		default:
			satisfied = false
		}

		if !satisfied {
			return false
		}
	}

	return true
}

// parseVersionParts splits a version string like "3.11" into [3, 11].
func parseVersionParts(v string) []int {
	parts := strings.Split(v, ".")
	result := make([]int, len(parts))
	for i, p := range parts {
		var n int
		fmt.Sscanf(p, "%d", &n)
		result[i] = n
	}
	return result
}

// compareVersionParts compares two version part slices.
// Returns -1 if a < b, 0 if a == b, 1 if a > b.
func compareVersionParts(a, b []int) int {
	maxLen := len(a)
	if len(b) > maxLen {
		maxLen = len(b)
	}
	for i := 0; i < maxLen; i++ {
		av, bv := 0, 0
		if i < len(a) {
			av = a[i]
		}
		if i < len(b) {
			bv = b[i]
		}
		if av < bv {
			return -1
		}
		if av > bv {
			return 1
		}
	}
	return 0
}

// CheckPythonVersion checks if a Python version satisfies a requires_python constraint.
// This is a convenience wrapper around [constraints.CheckVersionConstraint].
func CheckPythonVersion(pythonVersion, requiresPython string) bool {
	return constraints.CheckVersionConstraint(pythonVersion, requiresPython)
}

// IsCompatibleWith checks if this package is compatible with the given Python version.
// Returns true if compatible (or if requires_python is not specified), false otherwise.
func (p *PackageInfo) IsCompatibleWith(pythonVersion string) bool {
	return constraints.CheckVersionConstraint(pythonVersion, p.RequiresPython)
}

type apiResponse struct {
	Info     apiInfo                     `json:"info"`
	Releases map[string][]apiReleaseFile `json:"releases"` // Version -> release files
}

// apiReleaseFile represents a single release file (wheel, sdist) from PyPI
type apiReleaseFile struct {
	RequiresPython string `json:"requires_python"` // Python version constraint for this release
}

type apiInfo struct {
	Name              string         `json:"name"`
	Version           string         `json:"version"`
	Summary           string         `json:"summary"`
	License           string         `json:"license"`
	LicenseExpression string         `json:"license_expression"` // SPDX license expression (newer PyPI field)
	Classifiers       []string       `json:"classifiers"`
	RequiresDist      []string       `json:"requires_dist"`
	RequiresPython    string         `json:"requires_python"` // Python version constraint (e.g., ">=3.8")
	ProjectURLs       map[string]any `json:"project_urls"`
	HomePage          string         `json:"home_page"`
	Author            string         `json:"author"`
}

// ListVersions returns all available versions for a package.
// Versions are sorted lexicographically (not by PEP 440), so "1.10.0" sorts before "1.2.0".
// Pre-release versions are included in the list.
// Callers that need proper version ordering should sort the result with a PEP 440 comparator.
func (c *Client) ListVersions(ctx context.Context, pkg string, refresh bool) ([]string, error) {
	pkg = integrations.NormalizePkgName(pkg)

	data, err := c.fetchProjectResponse(ctx, pkg, refresh)
	if err != nil {
		return nil, err
	}

	versions := make([]string, 0, len(data.Releases))
	for v := range data.Releases {
		versions = append(versions, v)
	}

	integrations.SortVersions(versions)
	return versions, nil
}

// ListVersionsWithConstraints returns all versions and their runtime constraints in a single API call.
// This is more efficient than calling FetchPackageVersion for each version individually.
// Returns a map of version -> requires_python constraint (empty string if not specified).
func (c *Client) ListVersionsWithConstraints(ctx context.Context, pkg string, refresh bool) (map[string]string, error) {
	pkg = integrations.NormalizePkgName(pkg)

	data, err := c.fetchProjectResponse(ctx, pkg, refresh)
	if err != nil {
		return nil, err
	}

	result := make(map[string]string, len(data.Releases))
	for version, files := range data.Releases {
		for _, f := range files {
			if f.RequiresPython != "" {
				result[version] = f.RequiresPython
				break
			}
		}
	}
	return result, nil
}

// extractLicenseType extracts a short license identifier from PyPI data.
// Priority: 1) license_expression (SPDX), 2) classifiers, 3) license field
func extractLicenseType(license, licenseExpression string, classifiers []string) string {
	// First, prefer SPDX license_expression (newer PyPI field)
	if licenseExpression != "" {
		return strings.TrimSpace(licenseExpression)
	}

	// Second, try to extract from classifiers
	for _, c := range classifiers {
		if strings.HasPrefix(c, "License :: ") {
			parts := strings.Split(c, " :: ")
			if len(parts) >= 3 {
				// Return the last part, e.g., "MIT License", "BSD-3-Clause"
				return parts[len(parts)-1]
			}
		}
	}

	// If license field is short (likely just the type), use it
	if license != "" && len(license) < 100 && !strings.Contains(license, "\n") {
		return strings.TrimSpace(license)
	}

	// Otherwise, try to extract type from the beginning of the license text
	if license != "" {
		// Common patterns: "MIT License", "BSD 3-Clause License", "Apache License 2.0"
		firstLine := strings.Split(license, "\n")[0]
		firstLine = strings.TrimSpace(firstLine)
		if len(firstLine) < 50 {
			return firstLine
		}
	}

	return ""
}
