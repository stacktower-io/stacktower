package deps

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/matzehuels/stacktower/pkg/core/dag"
	"github.com/matzehuels/stacktower/pkg/observability"
)

// ManifestParser reads dependency information from local manifest files.
//
// Manifest files describe a project's dependencies and may be either:
//   - Requirement files (package.json, requirements.txt) with direct deps only
//   - Lock files (poetry.lock, Cargo.lock) with full transitive closures
//
// Implementations are found in language subpackages (e.g., python.PoetryParser).
type ManifestParser interface {
	// Parse reads the manifest file at path and returns the dependency graph.
	//
	// The path is typically a local file system path. Options may influence
	// parsing behavior (e.g., MaxDepth for resolvers that fetch additional data).
	//
	// Returns an error if the file cannot be read, is malformed, or if
	// dependency resolution fails. Common errors:
	//   - File not found or unreadable
	//   - Invalid JSON/TOML/YAML syntax
	//   - Missing required fields
	//   - Dependency fetching failures (if the parser resolves transitive deps)
	Parse(path string, opts Options) (*ManifestResult, error)

	// Supports reports whether this parser handles the given filename.
	//
	// The filename is typically the basename of a path (e.g., "package.json").
	// Returns true if this parser recognizes the file format.
	Supports(filename string) bool

	// Type returns the manifest type identifier (e.g., "poetry", "cargo", "npm").
	//
	// This identifier appears in ManifestResult.Type and is used for
	// logging and error messages.
	Type() string

	// IncludesTransitive reports whether this parser produces transitive deps.
	//
	// Returns true for lock files (poetry.lock, Cargo.lock) that contain the
	// full dependency closure. Returns false for requirement files (requirements.txt,
	// package.json) that only list direct dependencies.
	//
	// This is used by the CLI to decide whether additional resolution is needed.
	IncludesTransitive() bool
}

// ManifestResult holds the parsed dependency data from a manifest file.
//
// Returned by [ManifestParser.Parse] after successfully reading a manifest.
type ManifestResult struct {
	// Graph is the dependency graph with nodes for packages and edges
	// for dependencies.
	Graph *dag.DAG

	// Type is the manifest type identifier (from ManifestParser.Type).
	// Examples: "poetry", "cargo", "npm", "requirements".
	Type string

	// IncludesTransitive indicates whether Graph contains the full transitive
	// closure (true for lock files) or just direct dependencies (false).
	IncludesTransitive bool

	// RootPackage is the name of the root package, if determinable from the
	// manifest. Empty if the manifest doesn't specify a package name (e.g.,
	// requirements.txt has no root package).
	RootPackage string

	// RuntimeVersion is the target runtime version detected from the manifest.
	// For Python: extracted from requires-python (e.g., ">=3.9" → "3.9")
	// For Node.js: extracted from engines.node
	// For Ruby: extracted from ruby version directive
	// Empty if not specified; callers should use a language-specific default.
	RuntimeVersion string

	// RuntimeConstraint is the raw runtime constraint from the manifest.
	// For Python: ">=3.9,<4.0"
	// For Node.js: ">=18"
	// Empty if not specified.
	RuntimeConstraint string
}

// DetectManifest finds a parser that supports the given file path.
//
// The path is matched against each parser's Supports method using the basename.
// Parsers are checked in order, and the first match is returned.
//
// Typical usage:
//
//	lang := python.Language
//	parsers := lang.ManifestParsers(nil)
//	parser, err := deps.DetectManifest("poetry.lock", parsers...)
//	if err != nil {
//	    // No parser supports poetry.lock
//	}
//	result, err := parser.Parse("poetry.lock", opts)
//
// Returns an error if no parser in the list supports the file. An empty
// parsers list always returns an error.
func DetectManifest(path string, parsers ...ManifestParser) (ManifestParser, error) {
	name := filepath.Base(path)
	for _, p := range parsers {
		if p.Supports(name) {
			return p, nil
		}
	}
	return nil, fmt.Errorf("unsupported manifest: %s", name)
}

// ManifestInfo describes a manifest file and its support status.
type ManifestInfo struct {
	// Filename is the manifest file name (e.g., "package.json", "go.mod").
	Filename string

	// Language is the language name (e.g., "python", "javascript").
	Language string

	// ManifestType is the internal type identifier (e.g., "poetry", "cargo").
	ManifestType string

	// Supported indicates whether stacktower can parse this manifest.
	Supported bool
}

// KnownManifests lists all manifest files that stacktower knows about.
// This includes both supported manifests (from Language.ManifestAliases) and
// commonly encountered manifests that are not yet supported.
//
// The languages parameter should contain all Language definitions to aggregate.
// Additional unsupported manifests can be added via extraUnsupported.
func KnownManifests(languages []*Language, extraUnsupported map[string]string) []ManifestInfo {
	var result []ManifestInfo
	seen := make(map[string]bool)

	// First, add all supported manifests from languages
	for _, lang := range languages {
		for filename, manifestType := range lang.ManifestAliases {
			if seen[filename] {
				continue
			}
			seen[filename] = true
			result = append(result, ManifestInfo{
				Filename:     filename,
				Language:     lang.Name,
				ManifestType: manifestType,
				Supported:    true,
			})
		}
	}

	// Then add extra unsupported manifests
	for filename, language := range extraUnsupported {
		if seen[filename] {
			continue
		}
		seen[filename] = true
		result = append(result, ManifestInfo{
			Filename:     filename,
			Language:     language,
			ManifestType: "",
			Supported:    false,
		})
	}

	return result
}

// SupportedManifests returns a map of filename -> language for all supported manifests.
// This is a convenience function for quick lookups.
func SupportedManifests(languages []*Language) map[string]string {
	result := make(map[string]string)
	for _, lang := range languages {
		for filename := range lang.ManifestAliases {
			result[filename] = lang.Name
		}
	}
	return result
}

// IsManifestSupported checks if a manifest filename is supported by any of the languages.
func IsManifestSupported(filename string, languages []*Language) bool {
	for _, lang := range languages {
		if _, ok := lang.ManifestAliases[filename]; ok {
			return true
		}
	}
	return false
}

// GetManifestLanguage returns the language name for a manifest file, if supported.
// Returns empty string if the manifest is not supported.
func GetManifestLanguage(filename string, languages []*Language) string {
	for _, lang := range languages {
		if _, ok := lang.ManifestAliases[filename]; ok {
			return lang.Name
		}
	}
	return ""
}

// NormalizeLanguageName maps external language names (e.g., from GitHub API)
// to our standard internal names. Returns the original (lowercased) if no mapping exists.
func NormalizeLanguageName(name string, languages []*Language) string {
	if name == "" {
		return ""
	}

	// Build a case-insensitive lookup map
	lower := strings.ToLower(name)

	// Check against our language names
	for _, lang := range languages {
		if strings.ToLower(lang.Name) == lower {
			return lang.Name
		}
	}

	// Common aliases not covered by Language.Name
	aliases := map[string]string{
		"golang": "go",
	}
	if mapped, ok := aliases[lower]; ok {
		// Find the actual language name
		for _, lang := range languages {
			if lang.Name == mapped {
				return lang.Name
			}
		}
	}

	// Return lowercase if no match
	return lower
}

// ProjectRootNodeID is the conventional node ID for the virtual project root
// in manifest-based dependency graphs.
const ProjectRootNodeID = "__project__"

// ResolveAndMerge resolves each dependency via the resolver and merges the
// results into a single DAG with a virtual project root. Dependencies that
// fail to resolve are added as leaf nodes with a direct edge from the root.
//
// This is the shared implementation used by all manifest parsers that support
// transitive resolution (e.g., requirements.txt + resolver, Cargo.toml + resolver).
//
// When a dependency has a Pinned version, the resolver will fetch that exact
// version rather than the latest. This is important for lock files and go.mod
// where versions are explicitly specified.
func ResolveAndMerge(ctx context.Context, resolver Resolver, dependencies []Dependency, opts Options) (*dag.DAG, error) {
	opts = opts.WithDefaults()
	merged := dag.New(nil)
	_ = merged.AddNode(dag.Node{ID: ProjectRootNodeID, Meta: dag.Metadata{"virtual": true}})

	seenEdges := make(map[[2]string]bool)
	addEdge := func(e dag.Edge) {
		key := [2]string{e.From, e.To}
		if seenEdges[key] {
			return
		}
		seenEdges[key] = true
		_ = merged.AddEdge(e)
	}

	type resolveResult struct {
		index int
		dep   Dependency
		g     *dag.DAG
		err   error
	}

	if len(dependencies) == 0 {
		return merged, nil
	}

	// Check context before starting work
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	hooks := observability.ResolverFromContext(ctx)
	workerCount := min(opts.Workers, len(dependencies))
	if workerCount <= 0 {
		workerCount = 1
	}

	jobs := make(chan int)
	results := make(chan resolveResult, workerCount)

	var wg sync.WaitGroup
	for range workerCount {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range jobs {
				if ctx.Err() != nil {
					return
				}
				dep := dependencies[index]
				hooks.OnFetchStart(ctx, dep.Name, 0)

				resolveOpts := opts
				if dep.Pinned != "" {
					resolveOpts.Version = dep.Pinned
					resolveOpts.Constraint = ""
				} else if dep.Constraint != "" {
					resolveOpts.Version = ""
					resolveOpts.Constraint = dep.Constraint
				}
				g, err := resolver.Resolve(ctx, dep.Name, resolveOpts)

				depCount := 0
				if g != nil {
					depCount = len(g.Nodes())
				}
				hooks.OnFetchComplete(ctx, dep.Name, 0, depCount, err)

				results <- resolveResult{index: index, dep: dep, g: g, err: err}
			}
		}()
	}

	go func() {
		defer close(jobs)
		for index := range dependencies {
			select {
			case <-ctx.Done():
				return
			case jobs <- index:
			}
		}
	}()

	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect all results, then merge in original dependency order for
	// deterministic graph construction regardless of goroutine scheduling.
	collected := make([]resolveResult, 0, len(dependencies))
	for res := range results {
		if res.err != nil {
			if errors.Is(res.err, context.Canceled) || errors.Is(res.err, context.DeadlineExceeded) || ctx.Err() != nil {
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				return nil, res.err
			}
		}
		collected = append(collected, res)
	}
	slices.SortFunc(collected, func(a, b resolveResult) int { return a.index - b.index })

	for _, res := range collected {
		if res.err != nil {
			opts.Logger("resolve failed: %s: %v", res.dep.Name, res.err)
			_ = merged.AddNode(dag.Node{ID: res.dep.Name})
			addEdge(dag.Edge{From: ProjectRootNodeID, To: res.dep.Name, Meta: buildEdgeMeta(res.dep)})
			continue
		}

		for _, n := range res.g.Nodes() {
			_ = merged.AddNode(dag.Node{ID: n.ID, Meta: n.Meta})
		}
		for _, e := range res.g.Edges() {
			addEdge(dag.Edge{From: e.From, To: e.To, Meta: e.Meta})
		}
		addEdge(dag.Edge{From: ProjectRootNodeID, To: res.dep.Name, Meta: buildEdgeMeta(res.dep)})
	}

	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	return merged, nil
}

// buildEdgeMeta creates edge metadata from a dependency, storing version and
// constraint separately. This allows consumers to distinguish between pinned
// versions (from lock files / go.mod) and version constraints (from manifests).
func buildEdgeMeta(dep Dependency) dag.Metadata {
	meta := dag.Metadata{}
	if dep.Pinned != "" {
		meta["version"] = dep.Pinned
	}
	if dep.Constraint != "" {
		meta["constraint"] = dep.Constraint
	}
	return meta
}
