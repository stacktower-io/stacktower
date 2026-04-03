package pipeline

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/matzehuels/stacktower/pkg/cache"
	"github.com/matzehuels/stacktower/pkg/core/dag"
	"github.com/matzehuels/stacktower/pkg/core/deps"
	"github.com/matzehuels/stacktower/pkg/core/deps/constraints"
	"github.com/matzehuels/stacktower/pkg/core/deps/languages"
	"github.com/matzehuels/stacktower/pkg/core/deps/metadata"
)

// ParseResult contains the parsed dependency graph and metadata.
type ParseResult struct {
	Graph          *dag.DAG
	RuntimeVersion string // Target runtime version used (e.g., "3.11")
	RuntimeSource  string // Where runtime came from: "cli", "manifest", "default"
}

// Parse resolves dependencies for a package or manifest.
func Parse(ctx context.Context, c cache.Cache, opts Options) (*ParseResult, error) {
	lang := languages.Find(opts.Language)
	if lang == nil {
		return nil, fmt.Errorf("unsupported language: %s", opts.Language)
	}

	resolveOpts := buildResolveOptions(ctx, c, opts)

	var g *dag.DAG
	var runtimeVersion string
	var runtimeSource string
	var err error

	if opts.Manifest != "" {
		var manifestResult *deps.ManifestResult
		manifestResult, err = parseManifestWithResult(ctx, c, lang, opts, resolveOpts)
		if err != nil {
			return nil, err
		}
		g = manifestResult.Graph

		// Determine runtime version: CLI > manifest > default
		if opts.RuntimeVersion != "" {
			runtimeVersion = opts.RuntimeVersion
			runtimeSource = "cli"

			// Validate CLI-specified runtime against manifest's constraint
			if manifestResult.RuntimeConstraint != "" {
				if !constraints.CheckVersionConstraint(runtimeVersion, manifestResult.RuntimeConstraint) {
					return nil, &deps.IncompatibleRuntimeError{
						Package:           manifestResult.RootPackage,
						Version:           "manifest",
						RuntimeConstraint: manifestResult.RuntimeConstraint,
						TargetRuntime:     runtimeVersion,
					}
				}
			}
		} else if manifestResult.RuntimeVersion != "" {
			runtimeVersion = manifestResult.RuntimeVersion
			runtimeSource = "manifest"
		} else {
			runtimeVersion = lang.DefaultRuntimeVersion
			runtimeSource = "default"
		}
	} else {
		// For registry resolution: CLI > default (if compatible) > package minimum
		if opts.RuntimeVersion != "" {
			runtimeVersion = opts.RuntimeVersion
			runtimeSource = "cli"
		} else {
			// Get package's runtime constraint to check compatibility
			_, pkgConstraint := getPackageRuntimeConstraint(ctx, c, lang, opts.Package, opts.Version, resolveOpts)

			// Prefer the default runtime version if it's compatible with the package.
			// This ensures we use a modern runtime (e.g., 3.11) that can satisfy
			// transitive dependencies, rather than the package's minimum (e.g., 3.8)
			// which may be too old for some dependencies.
			if pkgConstraint == "" || constraints.CheckVersionConstraint(lang.DefaultRuntimeVersion, pkgConstraint) {
				runtimeVersion = lang.DefaultRuntimeVersion
				runtimeSource = "default"
			} else {
				// Default is incompatible with package constraint; use minimum from constraint
				if minVer := constraints.ExtractMinVersion(pkgConstraint); minVer != "" {
					runtimeVersion = minVer
					runtimeSource = "package"
				} else {
					runtimeVersion = lang.DefaultRuntimeVersion
					runtimeSource = "default"
				}
			}
			resolveOpts.RuntimeVersion = runtimeVersion
		}

		g, err = resolvePackage(ctx, c, lang, opts.Package, resolveOpts)
		if err != nil {
			return nil, err
		}
	}

	// NOTE: Normalization (adding subdividers) is NOT done here.
	// It's applied later during layout via PrepareGraph, which allows
	// the raw graph to be stored and reused for different viz types
	// (tower needs normalized graph, nodelink needs raw graph).

	filteredGraph := deps.FilterPrereleaseNodes(g, opts.IncludePrerelease)

	// Store parse options in graph metadata so they persist through caching
	filteredGraph.Meta()["runtime_version"] = runtimeVersion
	filteredGraph.Meta()["runtime_source"] = runtimeSource
	filteredGraph.Meta()["dependency_scope"] = opts.DependencyScope
	filteredGraph.Meta()["include_prerelease"] = opts.IncludePrerelease

	return &ParseResult{
		Graph:          filteredGraph,
		RuntimeVersion: runtimeVersion,
		RuntimeSource:  runtimeSource,
	}, nil
}

// buildResolveOptions creates deps.Options from pipeline options.
func buildResolveOptions(ctx context.Context, c cache.Cache, opts Options) deps.Options {
	resolveOpts := deps.Options{
		Ctx:               ctx,
		Version:           opts.Version,
		MaxDepth:          opts.MaxDepth,
		MaxNodes:          opts.MaxNodes,
		Workers:           opts.Workers,
		Refresh:           opts.Refresh,
		CacheTTL:          deps.DefaultCacheTTL,
		DependencyScope:   opts.DependencyScope,
		IncludePrerelease: opts.IncludePrerelease,
		RuntimeVersion:    opts.RuntimeVersion,
	}

	// Set up logger callback
	if opts.Logger != nil {
		resolveOpts.Logger = func(format string, args ...any) {
			opts.Logger.Warnf(format, args...)
		}
	}

	// Set up metadata providers and URLProvider for enrichment.
	// GitHub enrichment works both with and without a token:
	// - With token: uses authenticated rate limits (5000 req/hr)
	// - Without token: uses unauthenticated rate limits (60 req/hr per IP)
	// This ensures public/anonymous requests still get GitHub metadata.
	token := opts.GitHubToken
	if token == "" {
		token = os.Getenv("GITHUB_TOKEN")
	}
	if opts.ShouldEnrich() {
		var ghOpts []metadata.GitHubOption
		if opts.FetchContributors {
			ghOpts = append(ghOpts, metadata.WithContributors())
		}
		gh := metadata.NewGitHub(c, token, deps.DefaultCacheTTL, ghOpts...)
		resolveOpts.MetadataProviders = []deps.MetadataProvider{gh}

		// Set up URLProvider for manifest enrichment.
		// This enables GitHub enrichment for lock files and other manifests
		// that don't include repository URLs directly.
		resolveOpts.URLProvider = deps.NewURLProvider(opts.Language, c, deps.DefaultCacheTTL)
	}

	return resolveOpts
}

// resolvePackage resolves dependencies from a package registry.
func resolvePackage(ctx context.Context, c cache.Cache, lang *deps.Language, pkg string, opts deps.Options) (*dag.DAG, error) {
	resolver, err := lang.Resolver(c, opts)
	if err != nil {
		return nil, fmt.Errorf("get resolver: %w", err)
	}

	// Normalize package name if the language supports it
	if lang.NormalizeName != nil {
		pkg = lang.NormalizeName(pkg)
	}

	g, err := resolver.Resolve(ctx, pkg, opts)
	if err != nil {
		return nil, fmt.Errorf("resolve %s: %w", pkg, err)
	}

	return g, nil
}

// getPackageRuntimeConstraint fetches a package's runtime constraint from the registry.
// Returns the extracted minimum version and the raw constraint string.
// Returns empty strings if the package has no runtime constraint or fetch fails.
func getPackageRuntimeConstraint(ctx context.Context, c cache.Cache, lang *deps.Language, pkg, version string, opts deps.Options) (minVersion, constraint string) {
	// Normalize package name
	if lang.NormalizeName != nil {
		pkg = lang.NormalizeName(pkg)
	}
	resolver, err := lang.Resolver(c, opts)
	if err != nil {
		return "", ""
	}
	prober, ok := resolver.(deps.RuntimeConstraintProber)
	if !ok {
		return "", ""
	}
	probe, err := prober.ProbeRuntimeConstraint(ctx, pkg, version, opts.Refresh)
	if err != nil {
		return "", ""
	}
	return probe.MinVersion, probe.Constraint
}

// parseManifestWithResult parses dependencies and returns the full manifest result including runtime info.
func parseManifestWithResult(ctx context.Context, c cache.Cache, lang *deps.Language, opts Options, resolveOpts deps.Options) (*deps.ManifestResult, error) {
	resolver, err := lang.Resolver(c, resolveOpts)
	if err != nil {
		return nil, fmt.Errorf("get resolver: %w", err)
	}

	manifestName := filepath.Base(opts.ManifestFilename)
	parser, ok := lang.Manifest(manifestName, resolver)
	if !ok {
		return nil, fmt.Errorf("no parser for manifest: %s", opts.ManifestFilename)
	}

	// If manifest content is provided, write to temp file
	var filePath string
	if opts.ManifestPath != "" {
		filePath = opts.ManifestPath
	} else if opts.Manifest != "" {
		tmpDir, err := os.MkdirTemp("", "stacktower-*")
		if err != nil {
			return nil, fmt.Errorf("create temp dir: %w", err)
		}
		defer os.RemoveAll(tmpDir)

		filePath = filepath.Join(tmpDir, opts.ManifestFilename)
		if err := os.WriteFile(filePath, []byte(opts.Manifest), 0644); err != nil {
			return nil, fmt.Errorf("write temp file: %w", err)
		}
	} else {
		filePath = opts.ManifestFilename
	}

	result, err := parser.Parse(filePath, resolveOpts)
	if err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}

	// Rename __project__ node if RootName is specified.
	// This allows callers (CLI, API) to set a custom root name.
	// Ignore error: the node may not exist if the manifest has no virtual root.
	if opts.RootName != "" {
		result.Graph.RenameNode("__project__", opts.RootName) //nolint:errcheck // non-critical rename
	}

	return result, nil
}
