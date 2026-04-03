package python

import (
	"context"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"

	"github.com/matzehuels/stacktower/pkg/core/dag"
	"github.com/matzehuels/stacktower/pkg/core/deps"
	"github.com/matzehuels/stacktower/pkg/observability"
)

// PoetryLock parses poetry.lock files. It provides a full transitive closure
// of the dependency graph without needing to contact a registry.
type PoetryLock struct{}

func (p *PoetryLock) Type() string              { return "poetry.lock" }
func (p *PoetryLock) IncludesTransitive() bool  { return true }
func (p *PoetryLock) Supports(name string) bool { return name == "poetry.lock" }

func (p *PoetryLock) Parse(path string, opts deps.Options) (*deps.ManifestResult, error) {
	opts = opts.WithDefaults()

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var lock lockFile
	if err := toml.Unmarshal(data, &lock); err != nil {
		return nil, err
	}

	g := buildGraph(opts.Ctx, lock.Packages, opts.DependencyScope)
	deps.EnrichGraph(opts.Ctx, g, "pyproject.toml", opts)

	return &deps.ManifestResult{
		Graph:              g,
		Type:               p.Type(),
		IncludesTransitive: true,
		RootPackage:        extractPyprojectName(filepath.Dir(path)),
		RuntimeVersion:     extractPythonVersion(lock.Metadata.PythonVersions),
		RuntimeConstraint:  lock.Metadata.PythonVersions,
	}, nil
}

type lockFile struct {
	Packages []lockPackage `toml:"package"`
	Metadata lockMetadata  `toml:"metadata"`
}

type lockMetadata struct {
	PythonVersions string `toml:"python-versions"`
}

type lockPackage struct {
	Name         string         `toml:"name"`
	Version      string         `toml:"version"`
	Description  string         `toml:"description"`
	Category     string         `toml:"category"`
	Dependencies map[string]any `toml:"dependencies"`
}

func buildGraph(ctx context.Context, packages []lockPackage, scope string) *dag.DAG {
	g := dag.New(nil)
	pkgs := make(map[string]bool, len(packages))

	hooks := observability.ResolverFromContext(ctx)
	for _, pkg := range packages {
		if scope == deps.DependencyScopeProdOnly && pkg.Category == "dev" {
			continue
		}
		name := normalize(pkg.Name)
		pkgs[name] = true
		meta := dag.Metadata{"version": pkg.Version}
		if pkg.Description != "" {
			meta["description"] = pkg.Description
		}
		if pkg.Category != "" {
			meta["category"] = pkg.Category
		}
		hooks.OnFetchStart(ctx, name, 0)
		_ = g.AddNode(dag.Node{ID: name, Meta: meta})
		hooks.OnFetchComplete(ctx, name, 0, len(pkg.Dependencies), nil)
	}

	incoming := make(map[string]bool)
	for _, pkg := range packages {
		from := normalize(pkg.Name)
		for dep, constraint := range pkg.Dependencies {
			to := normalize(dep)
			if pkgs[to] {
				edgeMeta := dag.Metadata{}
				// Extract constraint from the dependency value
				if constraintStr := extractConstraint(constraint); constraintStr != "" {
					edgeMeta["constraint"] = constraintStr
				}
				_ = g.AddEdge(dag.Edge{From: from, To: to, Meta: edgeMeta})
				incoming[to] = true
			}
		}
	}

	_ = g.AddNode(dag.Node{ID: deps.ProjectRootNodeID, Meta: dag.Metadata{"virtual": true}})
	for _, pkg := range packages {
		name := normalize(pkg.Name)
		if !incoming[name] {
			edgeMeta := dag.Metadata{}
			if pkg.Version != "" {
				edgeMeta["constraint"] = "==" + pkg.Version
			}
			_ = g.AddEdge(dag.Edge{From: deps.ProjectRootNodeID, To: name, Meta: edgeMeta})
		}
	}

	return g
}
