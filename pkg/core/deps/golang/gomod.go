package golang

import (
	"bufio"
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/matzehuels/stacktower/pkg/core/dag"
	"github.com/matzehuels/stacktower/pkg/core/deps"
	"github.com/matzehuels/stacktower/pkg/core/deps/constraints"
	"github.com/matzehuels/stacktower/pkg/observability"
)

var runtimeGoModulesFn = runtimeGoModules

// GoModParser parses go.mod files. It extracts direct dependencies and
// optionally resolves them via the Go Module Proxy if a [deps.Resolver]
// is provided.
type GoModParser struct {
	resolver deps.Resolver
}

func (p *GoModParser) Type() string              { return "go.mod" }
func (p *GoModParser) IncludesTransitive() bool  { return p.resolver != nil }
func (p *GoModParser) Supports(name string) bool { return name == "go.mod" }

func (p *GoModParser) Parse(path string, opts deps.Options) (*deps.ManifestResult, error) {
	opts = opts.WithDefaults()
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	modResult := parseGoModFileComplete(f)
	directDeps := modResult.directDeps
	indirectDeps := modResult.indirectDeps
	if opts.DependencyScope == deps.DependencyScopeProdOnly {
		directDeps, indirectDeps = filterGoModRuntimeDeps(path, directDeps, indirectDeps, opts)
	}

	// Emit observability hooks for parsed dependencies
	hooks := observability.ResolverFromContext(opts.Ctx)
	for _, dep := range directDeps {
		hooks.OnFetchStart(opts.Ctx, dep.Name, 0)
		hooks.OnFetchComplete(opts.Ctx, dep.Name, 0, 0, nil)
	}
	for _, dep := range indirectDeps {
		hooks.OnFetchStart(opts.Ctx, dep.Name, 0)
		hooks.OnFetchComplete(opts.Ctx, dep.Name, 0, 0, nil)
	}

	var g *dag.DAG
	includesTransitive := false
	if len(indirectDeps) > 0 || len(directDeps) > 0 {
		// go.mod already contains all dependencies with pinned versions.
		// We use buildGoModGraphWithEdges to fetch edge information in parallel
		// without running the full PubGrub solver (which would be redundant since
		// versions are already resolved by Go's MVS).
		if p.resolver != nil {
			g = buildGoModGraphWithEdges(opts.Ctx, p.resolver, directDeps, indirectDeps, opts)
		} else {
			g = buildGoModGraph(directDeps, indirectDeps)
		}
		includesTransitive = len(indirectDeps) > 0
	} else {
		// Only direct deps available - use Constraint field which is set to the pinned version
		g = deps.ShallowGraphFromDeps(directDeps)
	}

	// Enrich graph with metadata (licenses, GitHub stars, etc.) from configured providers.
	// This is the standard pattern for all lock file parsers.
	deps.EnrichGraph(opts.Ctx, g, "go.mod", opts)

	return &deps.ManifestResult{
		Graph:              g,
		Type:               p.Type(),
		IncludesTransitive: includesTransitive,
		RootPackage:        modResult.moduleName,
		RuntimeVersion:     modResult.goVersion,
		RuntimeConstraint:  constraints.NormalizeRuntimeConstraint(modResult.goVersion),
	}, nil
}

func filterGoModRuntimeDeps(path string, directDeps, indirectDeps []deps.Dependency, opts deps.Options) ([]deps.Dependency, []deps.Dependency) {
	opts = opts.WithDefaults()
	modules, err := runtimeGoModulesFn(opts.Ctx, filepath.Dir(path))
	if err != nil {
		opts.Logger("go runtime dependency filter skipped: %v", err)
		return directDeps, indirectDeps
	}
	if len(modules) == 0 {
		return directDeps, indirectDeps
	}
	return filterDepsByModuleSet(directDeps, modules), filterDepsByModuleSet(indirectDeps, modules)
}

func runtimeGoModules(ctx context.Context, dir string) (map[string]bool, error) {
	cmd := exec.CommandContext(ctx, "go", "list", "-deps", "-test=false", "-f", "{{if .Module}}{{.Module.Path}}{{end}}", "./...")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	modules := make(map[string]bool)
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		name := strings.TrimSpace(scanner.Text())
		if name != "" {
			modules[name] = true
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return modules, nil
}

func filterDepsByModuleSet(in []deps.Dependency, modules map[string]bool) []deps.Dependency {
	out := make([]deps.Dependency, 0, len(in))
	for _, dep := range in {
		if modules[dep.Name] {
			out = append(out, dep)
		}
	}
	return out
}

// parseGoModFileComplete parses a go.mod file and returns both direct and indirect dependencies.
// goModParseResult holds the complete result of parsing a go.mod file
type goModParseResult struct {
	moduleName   string
	goVersion    string
	directDeps   []deps.Dependency
	indirectDeps []deps.Dependency
}

func parseGoModFileComplete(f *os.File) goModParseResult {
	result := goModParseResult{}
	seenDirect := make(map[string]bool)
	seenIndirect := make(map[string]bool)
	inRequire := false

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip comments and empty lines
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}

		// Extract module name
		if strings.HasPrefix(line, "module ") {
			result.moduleName = strings.TrimPrefix(line, "module ")
			result.moduleName = strings.TrimSpace(result.moduleName)
			continue
		}

		// Extract go version directive
		if strings.HasPrefix(line, "go ") {
			result.goVersion = strings.TrimPrefix(line, "go ")
			result.goVersion = strings.TrimSpace(result.goVersion)
			continue
		}

		// Handle require block
		if strings.HasPrefix(line, "require (") || line == "require(" {
			inRequire = true
			continue
		}
		if inRequire && line == ")" {
			inRequire = false
			continue
		}

		// Single-line require
		if strings.HasPrefix(line, "require ") && !strings.Contains(line, "(") {
			line = strings.TrimPrefix(line, "require ")
		} else if !inRequire {
			continue
		}

		// Parse module path and version, separating direct from indirect
		dep, isIndirect := parseRequireLineComplete(line)
		if dep.Name == "" {
			continue
		}

		if isIndirect {
			if !seenIndirect[dep.Name] {
				seenIndirect[dep.Name] = true
				result.indirectDeps = append(result.indirectDeps, dep)
			}
		} else {
			if !seenDirect[dep.Name] {
				seenDirect[dep.Name] = true
				result.directDeps = append(result.directDeps, dep)
			}
		}
	}

	return result
}

// parseRequireLineComplete parses a require line and returns a Dependency with version
// and a flag indicating if it's an indirect dependency.
func parseRequireLineComplete(line string) (dep deps.Dependency, isIndirect bool) {
	isIndirect = strings.Contains(line, "// indirect")

	// Remove inline comments
	if idx := strings.Index(line, "//"); idx != -1 {
		line = line[:idx]
	}

	line = strings.TrimSpace(line)
	fields := strings.Fields(line)
	if len(fields) >= 1 {
		dep.Name = fields[0]
		if len(fields) >= 2 {
			version := fields[1]
			// Pinned: tells resolver which version to fetch (bare version)
			dep.Pinned = version
			// Constraint: displayed in UI as the requirement (prefix with = for exact pin)
			if version != "" {
				dep.Constraint = "=" + version
			}
		}
	}
	return dep, isIndirect
}

// buildGoModGraphWithEdges fetches dependency edges by querying the Go proxy for each
// package's go.mod file. Unlike PubGrub resolution, this doesn't do constraint solving
// (versions are already pinned in go.mod), it just discovers the edges in parallel.
//
// This is much faster than full PubGrub resolution because:
// - No backtracking or SAT solving needed (MVS already resolved versions)
// - All fetches run in parallel with no dependencies between them
// - We only fetch packages we already know about (no transitive discovery)
func buildGoModGraphWithEdges(ctx context.Context, resolver deps.Resolver, directDeps, indirectDeps []deps.Dependency, opts deps.Options) *dag.DAG {
	opts = opts.WithDefaults()
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: deps.ProjectRootNodeID, Meta: dag.Metadata{"virtual": true}})

	// The resolver must implement Fetcher to fetch individual package versions
	fetcher, ok := resolver.(deps.Fetcher)
	if !ok {
		// Fallback to flat graph if resolver doesn't support fetching
		return buildGoModGraph(directDeps, indirectDeps)
	}

	// Build lookup of all known packages and their versions
	allDeps := make(map[string]deps.Dependency, len(directDeps)+len(indirectDeps))
	directSet := make(map[string]bool, len(directDeps))
	for _, dep := range directDeps {
		allDeps[dep.Name] = dep
		directSet[dep.Name] = true
	}
	for _, dep := range indirectDeps {
		allDeps[dep.Name] = dep
	}

	// Add all nodes first
	for name, dep := range allDeps {
		meta := dag.Metadata{}
		if dep.Pinned != "" {
			meta["version"] = dep.Pinned
		}
		if !directSet[name] {
			meta["indirect"] = true
		}
		_ = g.AddNode(dag.Node{ID: name, Meta: meta})
	}

	// Connect direct deps to root
	for _, dep := range directDeps {
		edgeMeta := dag.Metadata{}
		if dep.Constraint != "" {
			edgeMeta["constraint"] = dep.Constraint
		}
		_ = g.AddEdge(dag.Edge{From: deps.ProjectRootNodeID, To: dep.Name, Meta: edgeMeta})
	}

	// Fetch dependencies for each package in parallel to discover edges
	type fetchResult struct {
		name         string
		dependencies []deps.Dependency
		err          error
	}

	// Convert to slice for parallel processing
	depSlice := make([]deps.Dependency, 0, len(allDeps))
	for _, dep := range allDeps {
		depSlice = append(depSlice, dep)
	}

	results := deps.ParallelMapOrdered(ctx, opts.Workers, depSlice, func(ctx context.Context, dep deps.Dependency) fetchResult {
		if ctx.Err() != nil {
			return fetchResult{name: dep.Name, err: ctx.Err()}
		}

		version := dep.Pinned
		if version == "" {
			return fetchResult{name: dep.Name}
		}

		// Fetch the package to get its dependencies
		pkg, err := fetcher.FetchVersion(ctx, dep.Name, version, opts.Refresh)
		if err != nil {
			opts.Logger("fetch %s@%s for edges: %v", dep.Name, version, err)
			return fetchResult{name: dep.Name, err: err}
		}
		return fetchResult{name: dep.Name, dependencies: pkg.Dependencies}
	})

	// Add edges based on fetched dependencies
	for _, res := range results {
		if res.err != nil {
			continue
		}
		for _, childDep := range res.dependencies {
			// Only add edge if the target exists in our known set
			if _, exists := allDeps[childDep.Name]; exists {
				edgeMeta := dag.Metadata{}
				if childDep.Constraint != "" {
					edgeMeta["constraint"] = childDep.Constraint
				}
				_ = g.AddEdge(dag.Edge{From: res.name, To: childDep.Name, Meta: edgeMeta})
			}
		}
	}

	return g
}

// buildGoModGraph builds a complete dependency graph from go.mod's direct and indirect deps.
// Since go.mod doesn't specify which direct dep requires which indirect dep,
// we connect all deps to the root but mark indirect ones with metadata.
// This gives us a flat graph showing all dependencies with their pinned versions.
func buildGoModGraph(directDeps, indirectDeps []deps.Dependency) *dag.DAG {
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: deps.ProjectRootNodeID, Meta: dag.Metadata{"virtual": true}})

	// Add direct dependencies connected to root
	for _, dep := range directDeps {
		meta := dag.Metadata{}
		if dep.Pinned != "" {
			meta["version"] = dep.Pinned
		}
		_ = g.AddNode(dag.Node{ID: dep.Name, Meta: meta})
		edgeMeta := dag.Metadata{}
		if dep.Constraint != "" {
			edgeMeta["constraint"] = dep.Constraint
		}
		_ = g.AddEdge(dag.Edge{From: deps.ProjectRootNodeID, To: dep.Name, Meta: edgeMeta})
	}

	// Add indirect dependencies connected to root as well, but marked as indirect.
	// go.mod doesn't tell us the actual dependency edges between packages,
	// so we show them as direct children of root but visually distinguish them.
	// This ensures they appear in the visualization with their pinned versions.
	for _, dep := range indirectDeps {
		meta := dag.Metadata{"indirect": true}
		if dep.Pinned != "" {
			meta["version"] = dep.Pinned
		}
		_ = g.AddNode(dag.Node{ID: dep.Name, Meta: meta})
		edgeMeta := dag.Metadata{"indirect": true}
		if dep.Constraint != "" {
			edgeMeta["constraint"] = dep.Constraint
		}
		_ = g.AddEdge(dag.Edge{From: deps.ProjectRootNodeID, To: dep.Name, Meta: edgeMeta})
	}

	return g
}
