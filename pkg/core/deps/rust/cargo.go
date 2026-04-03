package rust

import (
	"os"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/matzehuels/stacktower/pkg/core/dag"
	"github.com/matzehuels/stacktower/pkg/core/deps"
	"github.com/matzehuels/stacktower/pkg/core/deps/constraints"
	"github.com/matzehuels/stacktower/pkg/observability"
)

// CargoToml parses Cargo.toml files. It extracts direct, dev, and build
// dependencies.
type CargoToml struct {
	resolver deps.Resolver
}

func (c *CargoToml) Type() string              { return "Cargo.toml" }
func (c *CargoToml) IncludesTransitive() bool  { return c.resolver != nil }
func (c *CargoToml) Supports(name string) bool { return strings.EqualFold(name, "cargo.toml") }

func (c *CargoToml) Parse(path string, opts deps.Options) (*deps.ManifestResult, error) {
	opts = opts.WithDefaults()

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cargo cargoFile
	if err := toml.Unmarshal(data, &cargo); err != nil {
		return nil, err
	}

	directDeps := extractCargoDepsWithVersions(cargo, opts.DependencyScope)

	// Emit observability hooks for extracted dependencies
	hooks := observability.ResolverFromContext(opts.Ctx)
	for _, dep := range directDeps {
		hooks.OnFetchStart(opts.Ctx, dep.Name, 0)
		hooks.OnFetchComplete(opts.Ctx, dep.Name, 0, 0, nil)
	}

	var g *dag.DAG
	if c.resolver != nil {
		g, err = deps.ResolveAndMerge(opts.Ctx, c.resolver, directDeps, opts)
		if err != nil {
			return nil, err
		}
	} else {
		g = deps.ShallowGraphFromDeps(directDeps)
	}

	rootPackage := cargo.Package.Name
	if rootPackage != "" {
		if root, ok := g.Node(deps.ProjectRootNodeID); ok {
			root.Meta["version"] = cargo.Package.Version
		}
	}

	return &deps.ManifestResult{
		Graph:              g,
		Type:               c.Type(),
		IncludesTransitive: c.resolver != nil,
		RootPackage:        rootPackage,
		RuntimeVersion:     cargo.Package.RustVersion,
		RuntimeConstraint:  constraints.NormalizeRuntimeConstraint(cargo.Package.RustVersion),
	}, nil
}

// extractCargoDepsWithVersions extracts dependencies with version constraints from Cargo.toml
func extractCargoDepsWithVersions(cargo cargoFile, scope string) []deps.Dependency {
	var result []deps.Dependency
	for name, spec := range cargo.Dependencies {
		result = append(result, parseCargoDependency(name, spec))
	}
	if scope == deps.DependencyScopeAll {
		for name, spec := range cargo.DevDependencies {
			result = append(result, parseCargoDependency(name, spec))
		}
	}
	for name, spec := range cargo.BuildDependencies {
		result = append(result, parseCargoDependency(name, spec))
	}
	return result
}

// parseCargoDependency extracts version constraint from a Cargo dependency spec.
// The spec can be a string (version) or a table with "version" key.
func parseCargoDependency(name string, spec any) deps.Dependency {
	dep := deps.Dependency{Name: name}
	switch v := spec.(type) {
	case string:
		dep.Constraint = v
	case map[string]any:
		if version, ok := v["version"].(string); ok {
			dep.Constraint = version
		}
		// Could also handle git/path deps here if needed
		if git, ok := v["git"].(string); ok {
			// For git dependencies, store the git URL as a hint
			if rev, ok := v["rev"].(string); ok {
				dep.Commit = rev
			} else if tag, ok := v["tag"].(string); ok {
				dep.Pinned = tag
			} else if branch, ok := v["branch"].(string); ok {
				dep.Constraint = "branch:" + branch
			}
			_ = git // suppress unused warning
		}
	}
	return dep
}

type cargoFile struct {
	Package struct {
		Name        string `toml:"name"`
		Version     string `toml:"version"`
		RustVersion string `toml:"rust-version"` // MSRV - Minimum Supported Rust Version
	} `toml:"package"`
	Dependencies      map[string]any `toml:"dependencies"`
	DevDependencies   map[string]any `toml:"dev-dependencies"`
	BuildDependencies map[string]any `toml:"build-dependencies"`
}
