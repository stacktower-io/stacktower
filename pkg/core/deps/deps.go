package deps

import (
	"context"
	"fmt"
	"maps"
	"strings"
	"time"
)

const (
	// DefaultMaxDepth is the default maximum dependency depth (50 levels).
	// This prevents infinite recursion in circular or very deep dependency trees.
	// Note: pipeline.DefaultMaxDepth (10) is more conservative for CLI/API UX,
	// but this higher limit is appropriate for the low-level resolver.
	DefaultMaxDepth = 50

	// DefaultMaxNodes is the default maximum number of packages to fetch (5000 nodes).
	// This caps memory usage and prevents unbounded crawling of large ecosystems.
	// This value is shared with pipeline.DefaultMaxNodes.
	DefaultMaxNodes = 5000

	// DefaultCacheTTL is the default HTTP cache duration (24 hours).
	// Cached registry responses are reused within this window unless Refresh is true.
	DefaultCacheTTL = 24 * time.Hour

	// versionSeparator separates package name from version in versioned IDs.
	versionSeparator = "@"

	// commitPrefix identifies commit-based versions in versioned IDs.
	commitPrefix = "commit:"

	// DependencyScopeProdOnly keeps only production/runtime dependencies.
	DependencyScopeProdOnly = "prod_only"

	// DependencyScopeAll keeps all dependencies including dev/test.
	DependencyScopeAll = "all"
)

// Dependency represents a package dependency with version information.
//
// This struct captures both the declared constraint (from manifest files) and
// the resolved version (from lock files or registry lookups). For private repos
// without semantic versions, the Commit field tracks the git commit SHA.
type Dependency struct {
	// Name is the normalized package name (e.g., "requests", "lodash").
	Name string

	// Constraint is the version constraint from the manifest file (e.g., "^4.17.0",
	// ">=2.0", "~1.0"). Empty if no constraint was specified (meaning "latest").
	Constraint string

	// Pinned is the resolved/pinned version (e.g., "4.17.21"). This is set when
	// the exact version is known, either from a lock file or after resolution.
	// Empty if the version hasn't been resolved yet.
	Pinned string

	// Commit is the git commit SHA for private repos or packages without semantic
	// versions. Takes precedence over Pinned when building the versioned ID.
	// Format: full SHA or abbreviated (e.g., "abc1234").
	Commit string
}

// ID returns the unique identifier for this dependency.
//
// The format depends on what version information is available:
//   - With commit: "name@commit:sha" (e.g., "my-lib@commit:abc1234")
//   - With pinned version: "name@version" (e.g., "requests@2.31.0")
//   - Unresolved: "name" (e.g., "requests")
//
// This ID is suitable for use as a dag.Node.ID and uniquely identifies
// the specific version of the package in the dependency graph.
func (d Dependency) ID() string {
	return BuildPackageID(d.Name, d.Pinned, d.Commit)
}

// NameOnly returns just the package name without version information.
// This is useful for backward compatibility with name-only lookups.
func (d Dependency) NameOnly() string {
	return d.Name
}

// IsResolved reports whether this dependency has a resolved version or commit.
func (d Dependency) IsResolved() bool {
	return d.Pinned != "" || d.Commit != ""
}

// VersionConstraint returns the version constraint string for edge metadata.
// Returns Constraint if set, otherwise Pinned.
func (d Dependency) VersionConstraint() string {
	if d.Constraint != "" {
		return d.Constraint
	}
	return d.Pinned
}

// BuildPackageID constructs a versioned package identifier.
//
// The format depends on the inputs:
//   - With commit: "name@commit:sha"
//   - With version: "name@version"
//   - Neither: "name"
//
// Commit takes precedence over version if both are provided.
// Empty name returns an empty string.
func BuildPackageID(name, version, commit string) string {
	if name == "" {
		return ""
	}
	if commit != "" {
		return name + versionSeparator + commitPrefix + commit
	}
	if version != "" {
		return name + versionSeparator + version
	}
	return name
}

// ParsePackageID parses a versioned package identifier into its components.
//
// Examples:
//   - "requests@2.31.0" → ("requests", "2.31.0", "")
//   - "lib@commit:abc1234" → ("lib", "", "abc1234")
//   - "requests" → ("requests", "", "")
//
// Returns the package name, version, and commit. At most one of version or
// commit will be non-empty.
func ParsePackageID(id string) (name, version, commit string) {
	if id == "" {
		return "", "", ""
	}

	idx := strings.Index(id, versionSeparator)
	if idx == -1 {
		return id, "", ""
	}

	name = id[:idx]
	versionPart := id[idx+1:]

	if strings.HasPrefix(versionPart, commitPrefix) {
		return name, "", strings.TrimPrefix(versionPart, commitPrefix)
	}
	return name, versionPart, ""
}

// DependencyFromName creates a Dependency with only the name set.
// This is a convenience function for backward compatibility when only
// the package name is known.
func DependencyFromName(name string) Dependency {
	return Dependency{Name: name}
}

// DependencyFromNameVersion creates a Dependency with name and pinned version.
func DependencyFromNameVersion(name, version string) Dependency {
	return Dependency{Name: name, Pinned: version}
}

// DependencyFromNameConstraint creates a Dependency with name and constraint.
func DependencyFromNameConstraint(name, constraint string) Dependency {
	return Dependency{Name: name, Constraint: constraint}
}

// Options configures dependency resolution behavior.
//
// All fields are optional. Zero values are replaced by defaults when passed
// to WithDefaults. Options is safe to copy and does not modify any inputs.
type Options struct {
	// Ctx is the context for cancellation and timeouts. If nil, WithDefaults
	// replaces it with context.Background(). Manifest parsers and resolvers
	// use this context for network operations and enrichment so that callers
	// can cancel long-running parses.
	Ctx context.Context

	// Version constrains the root package to a specific version. If empty,
	// the latest version is fetched. This only applies to the root package;
	// transitive dependencies are resolved normally.
	// Example: "2.31.0" for requests@2.31.0
	Version string

	// Constraint constrains the root package to a version range (ecosystem
	// syntax), such as "^4.18.0" (npm) or ">=1.0, <2.0". If both Version and
	// Constraint are set, Version takes precedence.
	Constraint string

	// DependencyScope controls which dependency groups parsers include.
	// Supported values:
	//   - "prod_only" (default): runtime/production dependencies only
	//   - "all": include dev/test dependencies too
	DependencyScope string

	// MaxDepth limits how many levels deep to traverse. A value of 1 fetches
	// only direct dependencies. Zero or negative values use DefaultMaxDepth (50).
	MaxDepth int

	// MaxNodes limits the total number of packages to fetch. When this limit
	// is reached, deeper dependencies are ignored but already-queued packages
	// may still be fetched. Zero or negative values use DefaultMaxNodes (5000).
	MaxNodes int

	// Workers is the number of concurrent goroutines for fetching packages.
	// Higher values increase parallelism but may trigger rate limits.
	// Zero or negative values use DefaultWorkers (20).
	Workers int

	// CacheTTL controls how long HTTP responses are cached. Registry clients
	// will reuse cached data within this duration. Zero or negative values use
	// DefaultCacheTTL (24 hours).
	CacheTTL time.Duration

	// Refresh bypasses the HTTP cache when true, forcing fresh registry fetches.
	// This is useful for getting the latest package versions but increases latency.
	Refresh bool

	// MetadataProviders is an optional list of enrichment sources (e.g., GitHub)
	// that add extra metadata to package nodes. Providers are called concurrently
	// after fetching each package. Nil or empty is safe.
	MetadataProviders []MetadataProvider

	// Logger is an optional callback for progress and error messages. If nil,
	// WithDefaults replaces it with a no-op logger. The format string follows
	// fmt.Printf conventions. Logger may be called concurrently from multiple
	// goroutines and must be safe for concurrent use.
	Logger func(string, ...any)

	// IncludePrerelease controls whether prerelease versions (alpha, beta, rc,
	// dev, etc.) are considered during resolution. When false, the resolver
	// filters out prerelease versions before version selection. Default is true.
	IncludePrerelease bool

	// RuntimeVersion is the target runtime version for environment marker evaluation.
	// For Python, this filters deps with markers like `python_version < "3.11"`.
	// For Node.js, this would filter based on `engines.node` constraints.
	// Empty means use the language-specific default (e.g., "3.11" for Python).
	RuntimeVersion string

	// URLProvider fetches repository URLs from the package registry.
	// Used by EnrichGraph to populate PackageRef.ProjectURLs for manifest parsing.
	// If nil, packages without URLs in node metadata won't be enriched with
	// external data like GitHub stars.
	URLProvider URLProvider
}

// WithDefaults returns a copy of Options with zero values replaced by defaults.
//
// This method is safe to call on a zero Options value. It fills in:
//   - MaxDepth: DefaultMaxDepth (50)
//   - MaxNodes: DefaultMaxNodes (5000)
//   - CacheTTL: DefaultCacheTTL (24h)
//   - Logger: no-op function if nil
//
// All other fields (Refresh, MetadataProviders) are preserved as-is, including
// nil slices. The original Options value is not modified.
func (o Options) WithDefaults() Options {
	opts := o
	if opts.Ctx == nil {
		opts.Ctx = context.Background()
	}
	if opts.MaxDepth <= 0 {
		opts.MaxDepth = DefaultMaxDepth
	}
	if opts.MaxNodes <= 0 {
		opts.MaxNodes = DefaultMaxNodes
	}
	if opts.Workers <= 0 {
		opts.Workers = DefaultWorkers
	}
	if opts.CacheTTL <= 0 {
		opts.CacheTTL = DefaultCacheTTL
	}
	if opts.Logger == nil {
		opts.Logger = func(string, ...any) {}
	}
	if opts.DependencyScope == "" {
		opts.DependencyScope = DependencyScopeProdOnly
	}
	return opts
}

// MetadataProvider enriches package nodes with external data (e.g., GitHub stars).
//
// Implementations fetch supplementary information that is not available in package
// registries, such as repository activity, maintainer counts, or security metrics.
// Providers are called concurrently after fetching each package during resolution.
type MetadataProvider interface {
	// Name returns the provider identifier (e.g., "github", "gitlab").
	// This is used for logging and error messages.
	Name() string

	// Enrich fetches additional metadata for the package.
	//
	// The pkg parameter contains registry information and URLs for lookup.
	// If refresh is true, the provider should bypass its cache.
	//
	// Returns a map of metadata keys to values, which are merged into the
	// package node's metadata. Keys should be provider-specific (e.g.,
	// "github_stars") to avoid conflicts with other providers.
	//
	// Returns an error if enrichment fails. The resolver logs the error
	// but continues without failing the entire resolution.
	Enrich(ctx context.Context, pkg *PackageRef, refresh bool) (map[string]any, error)
}

// BatchMetadataProvider extends MetadataProvider with bulk enrichment.
// Instead of N individual Enrich calls (each hitting rate limits), a single
// EnrichBatch call fetches metadata for all packages at once -- typically
// via a batched API like GitHub GraphQL.
//
// The resolver checks for this interface and uses it when available,
// falling back to per-package Enrich calls otherwise.
type BatchMetadataProvider interface {
	MetadataProvider

	// EnrichBatch fetches metadata for multiple packages in one operation.
	// Returns a map keyed by package name to its metadata map.
	// Packages that fail enrichment are silently omitted from the result.
	EnrichBatch(ctx context.Context, pkgs []*PackageRef, refresh bool) (map[string]map[string]any, error)
}

// URLProvider fetches repository URLs for packages from a registry.
// Used by EnrichGraph to populate PackageRef.ProjectURLs when parsing manifest files.
// This enables GitHub enrichment for lock files and other manifests that don't
// include repository URLs directly.
type URLProvider interface {
	// FetchURLs returns repository URLs for the given package names.
	// Implementations should fetch in parallel and respect rate limits.
	// Returns a map from package name to URLs (ProjectURLs map and HomePage).
	// Packages not found or without URLs are omitted from the result.
	FetchURLs(ctx context.Context, names []string, refresh bool) (map[string]PackageURLs, error)
}

// PackageURLs contains repository URL information for a package.
// Used by URLProvider to return URL data for manifest enrichment.
type PackageURLs struct {
	// ProjectURLs contains URL mappings from the package registry, typically
	// including "repository", "homepage", "documentation", etc.
	ProjectURLs map[string]string

	// HomePage is the project's home page URL, if available.
	HomePage string
}

// PackageRef identifies a package for metadata enrichment lookups.
//
// It contains the information metadata providers need to look up external data
// like GitHub repository statistics. Created by [Package.Ref].
type PackageRef struct {
	// Name is the package name as it appears in the registry.
	Name string

	// Version is the specific version being referenced.
	Version string

	// ProjectURLs contains URL mappings from the package registry, typically
	// including "repository", "homepage", "documentation", etc. The keys
	// depend on the registry. May be nil or empty.
	ProjectURLs map[string]string

	// HomePage is the project's home page URL, if available. May be empty.
	HomePage string

	// ManifestFile is the associated manifest type (e.g., "poetry", "cargo")
	// when the package comes from manifest parsing. Empty for registry-only packages.
	ManifestFile string
}

// IncompatibleRuntimeError is returned when a package requires a runtime version
// that is incompatible with the target runtime version.
type IncompatibleRuntimeError struct {
	Package           string // Package name
	Version           string // Package version
	RuntimeConstraint string // Package's runtime requirement (e.g., ">=3.8")
	TargetRuntime     string // Target runtime version (e.g., "2.7")
}

func (e *IncompatibleRuntimeError) Error() string {
	return fmt.Sprintf("%s %s requires runtime %s, but target is %s",
		e.Package, e.Version, e.RuntimeConstraint, e.TargetRuntime)
}

// DiamondDependencyError is returned when dependency resolution fails due to
// conflicting version requirements for the same package from different dependents.
// This is common in npm where packages allow nested/duplicate dependencies, but
// our SAT solver requires a single version per package.
type DiamondDependencyError struct {
	Package     string   // The package with conflicting requirements
	Dependents  []string // Packages that depend on conflicting versions
	Language    string   // Ecosystem (e.g., "javascript")
	OriginalErr string   // Original resolver error message
}

func (e *DiamondDependencyError) Error() string {
	return e.OriginalErr
}

// Package holds metadata fetched from a package registry.
//
// This is the core data structure returned by [Fetcher.Fetch] and used throughout
// the resolution process. Not all fields are populated by every registry—consult
// the specific integration documentation for field availability.
type Package struct {
	// Name is the package identifier in the registry (e.g., "requests", "serde").
	Name string

	// Version is the package version (e.g., "2.31.0"). For registry lookups
	// without a version constraint, this is typically the latest stable version.
	Version string

	// Commit is the git commit SHA for private repos or packages without semantic
	// versions. When set, this takes precedence over Version for building the
	// package's versioned ID.
	Commit string

	// Dependencies lists direct dependencies with their version constraints.
	// The resolver recursively fetches these to build the dependency tree.
	// Nil and empty slices are equivalent.
	Dependencies []Dependency

	// Description is a short summary of the package purpose. May be empty.
	Description string

	// License is the package license identifier (e.g., "MIT", "Apache-2.0").
	// May be empty or unknown.
	License string

	// LicenseText is the full raw license text for custom/proprietary licenses.
	// This is populated when the license field contains a long custom license
	// (e.g., custom commercial terms) rather than a standard SPDX identifier.
	// Intended for downstream LLM analysis of non-standard licenses.
	// May be empty for standard licenses where the text is well-known.
	LicenseText string

	// Author is the primary package author or maintainer. May be empty.
	Author string

	// Downloads is the total download count or recent download rate, depending
	// on the registry. Zero if unavailable. Not all registries provide this.
	Downloads int

	// Repository is the source code repository URL (e.g., GitHub, GitLab).
	// May be empty if not specified in registry metadata.
	Repository string

	// HomePage is the project home page URL. May be empty or identical to Repository.
	HomePage string

	// ProjectURLs contains additional URLs from the registry (docs, issues, etc.).
	// Keys and availability vary by registry. May be nil.
	ProjectURLs map[string]string

	// ManifestFile identifies the manifest type when this Package comes from
	// manifest parsing (e.g., "poetry", "cargo"). Empty for registry packages.
	ManifestFile string

	// RuntimeConstraint is the runtime version constraint (e.g., ">=3.8" for Python).
	// Used to check compatibility with target runtime versions. May be empty.
	RuntimeConstraint string
}

// ID returns the versioned identifier for this package.
//
// The format depends on what version information is available:
//   - With commit: "name@commit:sha"
//   - With version: "name@version"
//   - Neither: "name"
func (p *Package) ID() string {
	return BuildPackageID(p.Name, p.Version, p.Commit)
}

// DependencyNames returns the dependency names as a slice of strings.
// This is a backward compatibility helper for code that only needs names.
func (p *Package) DependencyNames() []string {
	if len(p.Dependencies) == 0 {
		return nil
	}
	names := make([]string, len(p.Dependencies))
	for i, d := range p.Dependencies {
		names[i] = d.Name
	}
	return names
}

// SetDependenciesFromNames sets dependencies from a slice of package names.
// This is a backward compatibility helper for code that only has names.
func (p *Package) SetDependenciesFromNames(names []string) {
	if len(names) == 0 {
		p.Dependencies = nil
		return
	}
	p.Dependencies = make([]Dependency, len(names))
	for i, name := range names {
		p.Dependencies[i] = Dependency{Name: name}
	}
}

// Metadata converts Package fields to a map for node metadata.
//
// The returned map always contains "version". Optional fields (description,
// license, author, downloads, commit) are included only if non-empty/non-zero.
//
// This map is suitable for use as dag.Node.Meta and can be further enriched
// by [MetadataProvider] implementations. The map is newly allocated and safe
// to modify. Returns a non-nil map even if the Package has no optional fields.
func (p *Package) Metadata() map[string]any {
	m := map[string]any{"version": p.Version}
	if p.Commit != "" {
		m["commit"] = p.Commit
	}
	if p.Description != "" {
		m["description"] = p.Description
	}
	if p.License != "" {
		m["license"] = p.License
	}
	if p.LicenseText != "" {
		m["license_text"] = p.LicenseText
	}
	if p.Author != "" {
		m["author"] = p.Author
	}
	if p.Downloads > 0 {
		m["downloads"] = p.Downloads
	}
	if p.HomePage != "" {
		m["homepage"] = p.HomePage
	}
	return m
}

// Ref creates a PackageRef for metadata provider lookups.
//
// The returned PackageRef consolidates URL information from multiple Package
// fields (ProjectURLs, Repository, HomePage) into a single ProjectURLs map
// for convenient provider lookups.
//
// The ProjectURLs map is a clone of the original, so modifying it does not
// affect the Package. If the Package has nil ProjectURLs, an empty map is
// allocated. Repository and HomePage are added to the map under "repository"
// and "homepage" keys if non-empty.
//
// This method never returns nil. Safe to call on a zero Package value.
func (p *Package) Ref() *PackageRef {
	urls := maps.Clone(p.ProjectURLs)
	if urls == nil {
		urls = make(map[string]string)
	}
	if p.Repository != "" {
		urls["repository"] = p.Repository
	}
	if p.HomePage != "" {
		urls["homepage"] = p.HomePage
	}
	return &PackageRef{
		Name:         p.Name,
		Version:      p.Version,
		ProjectURLs:  urls,
		HomePage:     p.HomePage,
		ManifestFile: p.ManifestFile,
	}
}
