package deps

import (
	"context"

	"github.com/matzehuels/stacktower/pkg/core/dag"
)

// DefaultWorkers is the default number of concurrent goroutines for fetching packages.
// This limits parallelism to prevent overwhelming registries and to bound
// memory usage. Set to 20 to match the burst limit of most registry rate limiters.
const DefaultWorkers = 20

// Fetcher retrieves package metadata from a registry.
//
// Implementations wrap HTTP clients for specific registries (PyPI, npm, crates.io).
// The Fetcher is responsible for HTTP caching, rate limiting, and error handling.
//
// Fetchers are found in the integrations subpackages (e.g., integrations/pypi).
type Fetcher interface {
	// Fetch retrieves package information by name (latest version).
	//
	// The name is the package identifier in the registry (e.g., "requests", "serde").
	// If refresh is true, cached HTTP responses are bypassed and fresh data is fetched.
	//
	// Returns an error if:
	//   - The package does not exist in the registry
	//   - The registry API is unreachable or returns an error
	//   - The response cannot be parsed
	//
	// Implementations should respect context cancellation and return ctx.Err()
	// when the context is canceled.
	//
	// Fetch must be safe for concurrent use by multiple goroutines.
	Fetch(ctx context.Context, name string, refresh bool) (*Package, error)

	// FetchVersion retrieves package information for a specific version.
	//
	// The name is the package identifier, and version is the exact version to fetch.
	// If refresh is true, cached HTTP responses are bypassed.
	//
	// Returns an error if:
	//   - The package or version does not exist in the registry
	//   - The registry API is unreachable or returns an error
	//   - The response cannot be parsed
	//
	// FetchVersion must be safe for concurrent use by multiple goroutines.
	FetchVersion(ctx context.Context, name, version string, refresh bool) (*Package, error)
}

// VersionLister extends Fetcher with the ability to list available versions.
// This is optional - fetchers that don't implement it will fall back to latest
// version resolution for transitive dependencies.
type VersionLister interface {
	// ListVersions returns all available versions for a package, sorted from
	// oldest to newest. Pre-release versions (alpha, beta, rc, dev) may be
	// included but are typically filtered out during constraint resolution.
	//
	// Returns an error if:
	//   - The package does not exist in the registry
	//   - The registry API is unreachable or returns an error
	ListVersions(ctx context.Context, name string, refresh bool) ([]string, error)
}

// RuntimeConstraintLister extends Fetcher with the ability to list runtime
// constraints for all versions in a single API call. This is more efficient
// than calling FetchVersion for each version individually.
//
// This is optional - fetchers that don't implement it will fall back to
// individual FetchVersion calls for runtime constraint information.
type RuntimeConstraintLister interface {
	// ListVersionsWithConstraints returns a map of version -> runtime constraint
	// (e.g., ">=3.8" for Python) for all versions of a package.
	// Empty string values indicate no runtime constraint for that version.
	//
	// Returns an error if:
	//   - The package does not exist in the registry
	//   - The registry API is unreachable or returns an error
	ListVersionsWithConstraints(ctx context.Context, name string, refresh bool) (map[string]string, error)
}

// RuntimeConstraintProbe contains runtime requirement information for a package.
type RuntimeConstraintProbe struct {
	// Constraint is the normalized runtime constraint string (e.g., ">=3.8").
	Constraint string
	// MinVersion is the extracted minimum runtime version when derivable.
	MinVersion string
}

// RuntimeConstraintProber extends Resolver with package-level runtime probing.
// This lets orchestration layers infer runtime defaults without constructing
// registry-specific clients directly.
type RuntimeConstraintProber interface {
	// ProbeRuntimeConstraint returns runtime requirements for a package.
	// If version is empty, the latest package version is probed.
	ProbeRuntimeConstraint(ctx context.Context, name, version string, refresh bool) (RuntimeConstraintProbe, error)
}

// Resolver builds a dependency graph starting from a root package.
//
// The sole production implementation is [PubGrubResolver], which uses
// a SAT-solver for proper constraint resolution with backtracking.
type Resolver interface {
	// Resolve fetches the package and its transitive dependencies.
	//
	// Starting from pkg, the resolver recursively fetches dependencies up to
	// Options.MaxDepth levels deep and Options.MaxNodes total packages.
	//
	// Returns a [dag.DAG] where:
	//   - Nodes represent packages (ID = package name)
	//   - Edges represent dependencies (From depends on To)
	//   - Node.Meta contains package metadata from [Package.Metadata] and
	//     enrichment from Options.MetadataProviders
	//
	// The DAG is fully connected from the root package. Isolated nodes may
	// appear if dependency fetching fails for non-root packages.
	//
	// Returns an error if:
	//   - The root package cannot be fetched (registry error or does not exist)
	//   - The context is canceled
	//   - Internal errors occur
	//
	// Partial failures (missing transitive dependencies) are logged via
	// Options.Logger but do not fail the entire resolution.
	//
	// Resolve is safe for concurrent use if the underlying Fetcher is safe.
	Resolve(ctx context.Context, pkg string, opts Options) (*dag.DAG, error)

	// Name returns the resolver's identifier (e.g., "pypi", "npm", "crates").
	//
	// This is used for logging and error messages.
	Name() string
}
