package pipeline

import (
	"context"
	"fmt"

	"github.com/stacktower-io/stacktower/pkg/cache"
	"github.com/stacktower-io/stacktower/pkg/core/deps"
	"github.com/stacktower-io/stacktower/pkg/core/deps/constraints"
	"github.com/stacktower-io/stacktower/pkg/core/deps/languages"
	"github.com/stacktower-io/stacktower/pkg/integrations"
)

// ListOptions configures version listing behavior.
type ListOptions struct {
	// Language is the target ecosystem (e.g., "python", "rust").
	Language string

	// Package is the package name to list versions for.
	Package string

	// RuntimeVersion filters versions compatible with this runtime (e.g., "3.8" for Python).
	// Empty string means no filtering.
	RuntimeVersion string

	// IncludeConstraints includes runtime constraints in the result.
	// This may require additional API calls for some registries.
	IncludeConstraints bool

	// Refresh bypasses cache for fresh data.
	Refresh bool
}

// ListResult contains the result of a version listing operation.
type ListResult struct {
	// Package is the normalized package name.
	Package string

	// Language is the ecosystem name.
	Language string

	// Versions is the list of available versions, sorted newest first.
	Versions []string

	// RuntimeConstraints maps version -> runtime constraint (e.g., ">=3.8").
	// Only populated when ListOptions.IncludeConstraints is true.
	RuntimeConstraints map[string]string

	// LatestStable is the highest non-prerelease version.
	LatestStable string
}

// ValidateListOptions validates options for the List operation.
func ValidateListOptions(opts ListOptions) error {
	if opts.Language == "" {
		return fmt.Errorf("language is required")
	}
	if opts.Package == "" {
		return fmt.Errorf("package is required")
	}
	if languages.Find(opts.Language) == nil {
		return fmt.Errorf("unsupported language: %q", opts.Language)
	}
	return nil
}

// ListVersions returns available versions for a package.
// This is a stateless function that can be called without a Runner.
func ListVersions(ctx context.Context, c cache.Cache, opts ListOptions) (*ListResult, error) {
	if err := ValidateListOptions(opts); err != nil {
		return nil, err
	}

	lang := languages.Find(opts.Language)
	if lang == nil {
		return nil, fmt.Errorf("unsupported language: %q", opts.Language)
	}

	pkg := opts.Package
	if lang.NormalizeName != nil {
		pkg = lang.NormalizeName(pkg)
	}

	if c == nil {
		c = cache.NewNullCache()
	}

	resolver, err := lang.Resolver(c, deps.Options{})
	if err != nil {
		return nil, fmt.Errorf("initialize resolver: %w", err)
	}

	lister, ok := resolver.(deps.VersionLister)
	if !ok {
		return nil, fmt.Errorf("%s does not support version listing", opts.Language)
	}

	versions, err := lister.ListVersions(ctx, pkg, opts.Refresh)
	if err != nil {
		return nil, fmt.Errorf("list versions: %w", err)
	}

	integrations.SortVersionsDescending(versions)

	result := &ListResult{
		Package:      pkg,
		Language:     opts.Language,
		Versions:     versions,
		LatestStable: latestStable(versions),
	}

	if opts.IncludeConstraints || opts.RuntimeVersion != "" {
		constraints, err := fetchRuntimeConstraints(ctx, resolver, pkg, versions, opts.Refresh)
		if err != nil {
			return nil, fmt.Errorf("fetch runtime constraints: %w", err)
		}
		result.RuntimeConstraints = constraints

		if opts.RuntimeVersion != "" {
			result.Versions = filterByRuntime(result.Versions, constraints, opts.RuntimeVersion)
			result.LatestStable = latestStable(result.Versions)
		}
	}

	return result, nil
}

// ListVersions is a convenience method on Runner that uses its cache.
func (r *Runner) ListVersions(ctx context.Context, opts ListOptions) (*ListResult, error) {
	return ListVersions(ctx, r.Cache, opts)
}

// latestStable returns the highest non-prerelease version from a descending-sorted slice.
func latestStable(descending []string) string {
	if len(descending) == 0 {
		return ""
	}
	for _, v := range descending {
		sv := integrations.ParseSemver(v)
		if sv.Valid && sv.Prerelease == "" {
			return v
		}
	}
	return descending[0]
}

// fetchRuntimeConstraints fetches runtime constraints for all versions.
// Uses batch API if available, otherwise falls back to individual fetches.
func fetchRuntimeConstraints(ctx context.Context, resolver deps.Resolver, pkg string, versions []string, refresh bool) (map[string]string, error) {
	if lister, ok := resolver.(deps.RuntimeConstraintLister); ok {
		constraints, err := lister.ListVersionsWithConstraints(ctx, pkg, refresh)
		if err == nil {
			return constraints, nil
		}
	}

	fetcher, ok := resolver.(deps.Fetcher)
	if !ok {
		return nil, nil
	}

	return fetchRuntimeConstraintsIndividual(ctx, fetcher, pkg, versions, refresh), nil
}

// fetchRuntimeConstraintsIndividual fetches constraints per version with
// bounded concurrency; cancellation stops scheduling further fetches.
func fetchRuntimeConstraintsIndividual(ctx context.Context, fetcher deps.Fetcher, pkg string, versions []string, refresh bool) map[string]string {
	results, err := deps.ParallelMapOrdered(ctx, 10, versions, func(ctx context.Context, version string) string {
		info, err := fetcher.FetchVersion(ctx, pkg, version, refresh)
		if err != nil {
			return ""
		}
		return info.RuntimeConstraint
	})
	if err != nil {
		return map[string]string{}
	}

	constraints := make(map[string]string)
	for i, c := range results {
		if c != "" {
			constraints[versions[i]] = c
		}
	}
	return constraints
}

// filterByRuntime filters versions to those compatible with the given runtime.
func filterByRuntime(versions []string, constraints map[string]string, runtimeVersion string) []string {
	if constraints == nil {
		return versions
	}

	var filtered []string
	for _, v := range versions {
		constraint := constraints[v]
		if constraint == "" || checkVersionConstraint(runtimeVersion, constraint) {
			filtered = append(filtered, v)
		}
	}
	return filtered
}

// checkVersionConstraint is a simple wrapper that checks if a version satisfies a constraint.
// Uses the constraints package for the actual check.
func checkVersionConstraint(version, constraint string) bool {
	return constraints.CheckVersionConstraint(version, constraint)
}
