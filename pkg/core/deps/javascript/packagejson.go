package javascript

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"

	"github.com/matzehuels/stacktower/pkg/core/dag"
	"github.com/matzehuels/stacktower/pkg/core/deps"
	"github.com/matzehuels/stacktower/pkg/observability"
)

// nodeVersionRE extracts version from constraints like ">=18", ">=18.0.0", "^20"
var nodeVersionRE = regexp.MustCompile(`[>=^~]*\s*v?(\d+(?:\.\d+)*)`)

// PackageJSON parses package.json files. It extracts dependencies,
// devDependencies, and peerDependencies.
type PackageJSON struct {
	resolver deps.Resolver
}

func (p *PackageJSON) Type() string              { return "package.json" }
func (p *PackageJSON) IncludesTransitive() bool  { return p.resolver != nil }
func (p *PackageJSON) Supports(name string) bool { return strings.EqualFold(name, "package.json") }

func (p *PackageJSON) Parse(path string, opts deps.Options) (*deps.ManifestResult, error) {
	opts = opts.WithDefaults()

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var pkg packageFile
	if err := json.Unmarshal(data, &pkg); err != nil {
		return nil, err
	}

	directDeps := extractPackageDepsWithConstraints(pkg, opts.DependencyScope)

	// Emit observability hooks for extracted dependencies
	hooks := observability.ResolverFromContext(opts.Ctx)
	for _, dep := range directDeps {
		hooks.OnFetchStart(opts.Ctx, dep.Name, 0)
		hooks.OnFetchComplete(opts.Ctx, dep.Name, 0, 0, nil)
	}

	var g *dag.DAG
	if p.resolver != nil {
		g, err = deps.ResolveAndMerge(opts.Ctx, p.resolver, directDeps, opts)
		if err != nil {
			return nil, err
		}
	} else {
		g = deps.ShallowGraphFromDeps(directDeps)
	}

	if pkg.Name != "" {
		if root, ok := g.Node(deps.ProjectRootNodeID); ok {
			root.Meta["version"] = pkg.Version
		}
	}

	return &deps.ManifestResult{
		Graph:              g,
		Type:               p.Type(),
		IncludesTransitive: p.resolver != nil,
		RootPackage:        pkg.Name,
		RuntimeVersion:     extractNodeVersion(pkg.Engines.Node),
		RuntimeConstraint:  pkg.Engines.Node,
	}, nil
}

// extractPackageDepsWithConstraints extracts dependencies with version constraints
func extractPackageDepsWithConstraints(pkg packageFile, scope string) []deps.Dependency {
	var result []deps.Dependency
	for name, constraint := range pkg.Dependencies {
		result = append(result, deps.Dependency{Name: name, Constraint: constraint})
	}
	if scope == deps.DependencyScopeAll {
		for name, constraint := range pkg.DevDependencies {
			result = append(result, deps.Dependency{Name: name, Constraint: constraint})
		}
	}
	for name, constraint := range pkg.PeerDependencies {
		result = append(result, deps.Dependency{Name: name, Constraint: constraint})
	}
	return result
}

type packageFile struct {
	Name             string            `json:"name"`
	Version          string            `json:"version"`
	Dependencies     map[string]string `json:"dependencies"`
	DevDependencies  map[string]string `json:"devDependencies"`
	PeerDependencies map[string]string `json:"peerDependencies"`
	Engines          packageEngines    `json:"engines"`
}

type packageEngines struct {
	Node string `json:"node"`
	NPM  string `json:"npm"`
}

// UnmarshalJSON tolerates malformed package.json engines shapes.
// Some ecosystems/tools may emit non-object values (e.g. arrays). We ignore
// those instead of failing manifest parsing.
func (p *packageEngines) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || trimmed == "null" {
		*p = packageEngines{}
		return nil
	}
	if strings.HasPrefix(trimmed, "{") {
		type alias packageEngines
		var decoded alias
		if err := json.Unmarshal(data, &decoded); err != nil {
			return err
		}
		*p = packageEngines(decoded)
		return nil
	}
	*p = packageEngines{}
	return nil
}

// extractNodeVersion extracts the minimum Node.js version from an engines.node constraint.
// Examples: ">=18" → "18", ">=18.0.0" → "18.0.0", "^20" → "20"
func extractNodeVersion(constraint string) string {
	if constraint == "" {
		return ""
	}
	// Take the first constraint in case of combined (e.g., ">=18 <21")
	parts := strings.Fields(constraint)
	if len(parts) > 0 {
		if m := nodeVersionRE.FindStringSubmatch(parts[0]); len(m) > 1 {
			return m[1]
		}
	}
	return ""
}
