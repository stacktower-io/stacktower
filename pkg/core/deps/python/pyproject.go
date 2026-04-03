package python

import (
	"os"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/matzehuels/stacktower/pkg/core/dag"
	"github.com/matzehuels/stacktower/pkg/core/deps"
	"github.com/matzehuels/stacktower/pkg/observability"
)

// PyProject parses pyproject.toml files. By default, it only provides
// direct dependencies. If a [deps.Resolver] is provided, it can resolve
// the full transitive closure.
//
// Extracts production dependencies only from:
// - PEP 621 [project.dependencies]
// - Poetry [tool.poetry.dependencies]
// - Flit [tool.flit.metadata.requires]
//
// Skips dev/test/optional dependencies from:
// - [project.optional-dependencies]
// - [tool.poetry.dev-dependencies]
// - [tool.poetry.group.*]
// - [dependency-groups] (PEP 735)
// - [tool.uv.dev-dependencies]
// - [tool.flit.metadata.requires-extra]
type PyProject struct {
	resolver deps.Resolver
}

func (p *PyProject) Type() string             { return "pyproject.toml" }
func (p *PyProject) IncludesTransitive() bool { return p.resolver != nil }

func (p *PyProject) Supports(name string) bool {
	return name == "pyproject.toml"
}

func (p *PyProject) Parse(path string, opts deps.Options) (*deps.ManifestResult, error) {
	opts = opts.WithDefaults()

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var pyproject pyprojectFile
	if err := toml.Unmarshal(data, &pyproject); err != nil {
		return nil, err
	}

	dependencies := extractPyprojectDependencies(&pyproject)

	// Emit observability hooks for extracted dependencies
	hooks := observability.ResolverFromContext(opts.Ctx)
	for _, dep := range dependencies {
		hooks.OnFetchStart(opts.Ctx, dep.Name, 0)
		hooks.OnFetchComplete(opts.Ctx, dep.Name, 0, 0, nil)
	}

	var g *dag.DAG
	if p.resolver != nil {
		g, err = deps.ResolveAndMerge(opts.Ctx, p.resolver, dependencies, opts)
		if err != nil {
			return nil, err
		}
	} else {
		g = deps.ShallowGraphFromDeps(dependencies)
	}

	rootPackage := pyproject.Project.Name
	if rootPackage == "" {
		rootPackage = pyproject.Tool.Poetry.Name
	}
	if rootPackage == "" {
		rootPackage = pyproject.Tool.Flit.Metadata.Module
	}

	// Extract Python version requirement
	runtimeConstraint := pyproject.Project.RequiresPython
	if runtimeConstraint == "" {
		// Try Poetry's python dependency
		if pythonDep, ok := pyproject.Tool.Poetry.Dependencies["python"]; ok {
			if s, ok := pythonDep.(string); ok {
				runtimeConstraint = s
			}
		}
	}

	return &deps.ManifestResult{
		Graph:              g,
		Type:               p.Type(),
		IncludesTransitive: p.resolver != nil,
		RootPackage:        rootPackage,
		RuntimeVersion:     extractPythonVersion(runtimeConstraint),
		RuntimeConstraint:  runtimeConstraint,
	}, nil
}

// pyprojectFile represents the structure of a pyproject.toml file.
type pyprojectFile struct {
	Project          pyprojectProject `toml:"project"`
	Tool             pyprojectTool    `toml:"tool"`
	DependencyGroups map[string]any   `toml:"dependency-groups"`
}

// pyprojectProject represents the [project] section (PEP 621).
type pyprojectProject struct {
	Name                 string              `toml:"name"`
	Version              string              `toml:"version"`
	RequiresPython       string              `toml:"requires-python"`
	Dependencies         []string            `toml:"dependencies"`
	OptionalDependencies map[string][]string `toml:"optional-dependencies"`
}

// pyprojectTool represents the [tool] section.
type pyprojectTool struct {
	Poetry pyprojectPoetry `toml:"poetry"`
	UV     pyprojectUV     `toml:"uv"`
	Flit   pyprojectFlit   `toml:"flit"`
}

// pyprojectPoetry represents the [tool.poetry] section.
type pyprojectPoetry struct {
	Name         string         `toml:"name"`
	Version      string         `toml:"version"`
	Dependencies map[string]any `toml:"dependencies"`
	DevDeps      map[string]any `toml:"dev-dependencies"`
	Group        map[string]any `toml:"group"`
}

// pyprojectUV represents the [tool.uv] section.
type pyprojectUV struct {
	DevDeps []string `toml:"dev-dependencies"`
}

// pyprojectFlit represents the [tool.flit] section.
type pyprojectFlit struct {
	Metadata pyprojectFlitMetadata `toml:"metadata"`
}

// pyprojectFlitMetadata represents the [tool.flit.metadata] section.
type pyprojectFlitMetadata struct {
	Module        string              `toml:"module"`
	Requires      []string            `toml:"requires"`
	RequiresExtra map[string][]string `toml:"requires-extra"`
}

// pep508RE captures package name and optional version constraint from PEP 508 format.
// Examples: "requests>=2.28.0", "numpy==1.24.0", "flask~=2.0", "pytest; extra == 'test'"
var pep508RE = regexp.MustCompile(`^([a-zA-Z0-9][-a-zA-Z0-9._]*)\s*(.*)`)

// extractPyprojectDependencies extracts production dependencies from a pyproject.toml file.
// Only extracts runtime dependencies, skipping dev, test, and optional dependencies.
func extractPyprojectDependencies(p *pyprojectFile) []deps.Dependency {
	seen := make(map[string]bool)
	var result []deps.Dependency

	// PEP 621 [project.dependencies] - production deps
	for _, dep := range p.Project.Dependencies {
		if d := parsePEP508(dep); d.Name != "" && !seen[d.Name] {
			seen[d.Name] = true
			result = append(result, d)
		}
	}
	// NOTE: Skipping [project.optional-dependencies] - these are extras (dev, test, docs, etc.)

	// Poetry [tool.poetry.dependencies] - production deps
	for name, constraint := range p.Tool.Poetry.Dependencies {
		normalizedName := normalize(name)
		if normalizedName == "python" || seen[normalizedName] {
			continue
		}
		seen[normalizedName] = true
		result = append(result, deps.Dependency{
			Name:       normalizedName,
			Constraint: extractConstraint(constraint),
		})
	}
	// NOTE: Skipping [tool.poetry.dev-dependencies] - dev deps
	// NOTE: Skipping [tool.poetry.group.*] - groups are typically dev, test, docs, etc.

	// NOTE: Skipping [dependency-groups] (PEP 735) - groups are typically dev, test, docs, etc.
	// NOTE: Skipping [tool.uv.dev-dependencies] - dev deps

	// Flit [tool.flit.metadata.requires] - production deps
	for _, dep := range p.Tool.Flit.Metadata.Requires {
		if d := parsePEP508(dep); d.Name != "" && !seen[d.Name] {
			seen[d.Name] = true
			result = append(result, d)
		}
	}
	// NOTE: Skipping [tool.flit.metadata.requires-extra] - extras (dev, test, etc.)

	return result
}

// parsePEP508 parses a PEP 508 dependency string into name and constraint.
func parsePEP508(dep string) deps.Dependency {
	dep = strings.TrimSpace(dep)
	if dep == "" {
		return deps.Dependency{}
	}

	m := pep508RE.FindStringSubmatch(dep)
	if len(m) < 2 {
		return deps.Dependency{}
	}

	name := normalize(m[1])
	constraint := ""
	if len(m) > 2 && m[2] != "" {
		constraint = strings.TrimSpace(m[2])
		// Remove environment markers (everything after ;)
		if idx := strings.Index(constraint, ";"); idx != -1 {
			constraint = strings.TrimSpace(constraint[:idx])
		}
		// Remove extras (everything in [])
		if idx := strings.Index(constraint, "["); idx == 0 {
			// Extract what comes after the closing ]
			if endIdx := strings.Index(constraint, "]"); endIdx != -1 {
				constraint = strings.TrimSpace(constraint[endIdx+1:])
			}
		}
	}

	return deps.Dependency{
		Name:       name,
		Constraint: constraint,
	}
}
