package javascript

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/matzehuels/stacktower/pkg/core/dag"
	"github.com/matzehuels/stacktower/pkg/core/deps"
	"github.com/matzehuels/stacktower/pkg/observability"
)

// PackageLock parses package-lock.json files. It provides a full transitive
// closure of the dependency graph without needing to contact a registry.
// Supports lockfileVersion 2 and 3 formats.
type PackageLock struct{}

func (p *PackageLock) Type() string              { return "package-lock.json" }
func (p *PackageLock) IncludesTransitive() bool  { return true }
func (p *PackageLock) Supports(name string) bool { return strings.EqualFold(name, "package-lock.json") }

func (p *PackageLock) Parse(path string, opts deps.Options) (*deps.ManifestResult, error) {
	opts = opts.WithDefaults()

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var lock packageLockFile
	if err := json.Unmarshal(data, &lock); err != nil {
		return nil, err
	}

	g := buildPackageLockGraph(lock, opts)
	deps.EnrichGraph(opts.Ctx, g, "package.json", opts)

	// Extract runtime info from companion package.json
	pkgInfo := extractPackageJSONInfo(filepath.Dir(path))

	return &deps.ManifestResult{
		Graph:              g,
		Type:               p.Type(),
		IncludesTransitive: true,
		RootPackage:        pkgInfo.Name,
		RuntimeVersion:     extractNodeVersion(pkgInfo.NodeEngine),
		RuntimeConstraint:  pkgInfo.NodeEngine,
	}, nil
}

// packageJSONInfo holds extracted info from package.json
type packageJSONInfo struct {
	Name       string
	NodeEngine string
}

// extractPackageJSONInfo reads the package name and engines.node from package.json in the same directory.
func extractPackageJSONInfo(dir string) packageJSONInfo {
	data, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		return packageJSONInfo{}
	}
	var pkg struct {
		Name    string `json:"name"`
		Engines struct {
			Node string `json:"node"`
		} `json:"engines"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return packageJSONInfo{}
	}
	return packageJSONInfo{Name: pkg.Name, NodeEngine: pkg.Engines.Node}
}

// packageLockFile represents package-lock.json structure (v2/v3)
type packageLockFile struct {
	Name            string                      `json:"name"`
	Version         string                      `json:"version"`
	LockfileVersion int                         `json:"lockfileVersion"`
	Packages        map[string]packageLockEntry `json:"packages"`     // v2/v3 format
	Dependencies    map[string]packageLockDepV1 `json:"dependencies"` // v1 format (backwards compat)
}

// packageLockEntry represents a package entry in the "packages" object (v2/v3)
type packageLockEntry struct {
	Version      string            `json:"version"`
	Resolved     string            `json:"resolved"`
	Dev          bool              `json:"dev"`
	Optional     bool              `json:"optional"`
	Dependencies map[string]string `json:"dependencies"`
	License      string            `json:"license"`
}

// packageLockDepV1 represents a dependency entry in v1 format
type packageLockDepV1 struct {
	Version      string                      `json:"version"`
	Resolved     string                      `json:"resolved"`
	Dev          bool                        `json:"dev"`
	Optional     bool                        `json:"optional"`
	Requires     map[string]string           `json:"requires"`
	Dependencies map[string]packageLockDepV1 `json:"dependencies"` // nested deps
}

func buildPackageLockGraph(lock packageLockFile, opts deps.Options) *dag.DAG {
	g := dag.New(nil)

	// Use v2/v3 packages format if available, otherwise fall back to v1
	if len(lock.Packages) > 0 {
		return buildFromPackages(lock, opts)
	}
	if len(lock.Dependencies) > 0 {
		return buildFromDependenciesV1(lock, opts)
	}

	// Empty lockfile
	_ = g.AddNode(dag.Node{ID: deps.ProjectRootNodeID, Meta: dag.Metadata{"virtual": true}})
	return g
}

// buildFromPackages builds the graph from v2/v3 "packages" format
func buildFromPackages(lock packageLockFile, opts deps.Options) *dag.DAG {
	g := dag.New(nil)
	pkgs := make(map[string]bool)
	hooks := observability.ResolverFromContext(opts.Ctx)

	// First pass: add all package nodes
	for path, entry := range lock.Packages {
		// Skip the root entry (empty path)
		if path == "" {
			continue
		}

		// Extract package name from path (e.g., "node_modules/lodash" -> "lodash")
		// Handle nested deps: "node_modules/foo/node_modules/bar" -> "bar"
		name := extractPackageName(path)
		if name == "" {
			continue
		}

		// Only add if not already seen (handle duplicate nested paths)
		if pkgs[name] {
			continue
		}
		if opts.DependencyScope == deps.DependencyScopeProdOnly && entry.Dev {
			continue
		}
		pkgs[name] = true

		hooks.OnFetchStart(opts.Ctx, name, 0)
		meta := dag.Metadata{"version": entry.Version}
		if entry.Dev {
			meta["dev"] = true
		}
		if entry.License != "" {
			meta["license"] = entry.License
		}
		_ = g.AddNode(dag.Node{ID: name, Meta: meta})
		hooks.OnFetchComplete(opts.Ctx, name, 0, len(entry.Dependencies), nil)
	}

	// Second pass: add dependency edges
	incoming := make(map[string]bool)
	for path, entry := range lock.Packages {
		if path == "" {
			continue
		}

		from := extractPackageName(path)
		if from == "" || !pkgs[from] {
			continue
		}

		for depName, constraint := range entry.Dependencies {
			if pkgs[depName] {
				edgeMeta := dag.Metadata{}
				if constraint != "" {
					edgeMeta["constraint"] = constraint
				}
				_ = g.AddEdge(dag.Edge{From: from, To: depName, Meta: edgeMeta})
				incoming[depName] = true
			}
		}
	}

	// Add virtual root and connect packages with no incoming edges
	_ = g.AddNode(dag.Node{ID: deps.ProjectRootNodeID, Meta: dag.Metadata{"virtual": true}})

	// Get direct dependencies from the root entry
	if rootEntry, ok := lock.Packages[""]; ok {
		for depName := range rootEntry.Dependencies {
			if pkgs[depName] {
				edgeMeta := dag.Metadata{}
				// Look up the pinned version from the lock file
				if entry, ok := lock.Packages["node_modules/"+depName]; ok && entry.Version != "" {
					edgeMeta["constraint"] = "==" + entry.Version
				}
				_ = g.AddEdge(dag.Edge{From: deps.ProjectRootNodeID, To: depName, Meta: edgeMeta})
			}
		}
	} else {
		// Fall back to packages with no incoming edges
		for name := range pkgs {
			if !incoming[name] {
				edgeMeta := dag.Metadata{}
				// Look up the pinned version from the lock file
				if entry, ok := lock.Packages["node_modules/"+name]; ok && entry.Version != "" {
					edgeMeta["constraint"] = "==" + entry.Version
				}
				_ = g.AddEdge(dag.Edge{From: deps.ProjectRootNodeID, To: name, Meta: edgeMeta})
			}
		}
	}

	return g
}

// buildFromDependenciesV1 builds the graph from v1 "dependencies" format
func buildFromDependenciesV1(lock packageLockFile, opts deps.Options) *dag.DAG {
	g := dag.New(nil)
	pkgs := make(map[string]bool)
	hooks := observability.ResolverFromContext(opts.Ctx)

	// Recursively collect all packages
	var collectPackages func(depMap map[string]packageLockDepV1)
	collectPackages = func(depMap map[string]packageLockDepV1) {
		for name, entry := range depMap {
			if opts.DependencyScope == deps.DependencyScopeProdOnly && entry.Dev {
				continue
			}
			if !pkgs[name] {
				pkgs[name] = true
				hooks.OnFetchStart(opts.Ctx, name, 0)
				meta := dag.Metadata{"version": entry.Version}
				if entry.Dev {
					meta["dev"] = true
				}
				_ = g.AddNode(dag.Node{ID: name, Meta: meta})
				hooks.OnFetchComplete(opts.Ctx, name, 0, len(entry.Requires), nil)
			}
			// Recurse into nested dependencies
			if len(entry.Dependencies) > 0 {
				collectPackages(entry.Dependencies)
			}
		}
	}
	collectPackages(lock.Dependencies)

	// Add edges based on "requires"
	incoming := make(map[string]bool)
	var addEdges func(deps map[string]packageLockDepV1)
	addEdges = func(deps map[string]packageLockDepV1) {
		for name, entry := range deps {
			for reqName, constraint := range entry.Requires {
				if pkgs[reqName] {
					edgeMeta := dag.Metadata{}
					if constraint != "" {
						edgeMeta["constraint"] = constraint
					}
					_ = g.AddEdge(dag.Edge{From: name, To: reqName, Meta: edgeMeta})
					incoming[reqName] = true
				}
			}
			if len(entry.Dependencies) > 0 {
				addEdges(entry.Dependencies)
			}
		}
	}
	addEdges(lock.Dependencies)

	// Add virtual root
	_ = g.AddNode(dag.Node{ID: deps.ProjectRootNodeID, Meta: dag.Metadata{"virtual": true}})

	// Direct dependencies are top-level entries
	for name, entry := range lock.Dependencies {
		if pkgs[name] {
			edgeMeta := dag.Metadata{}
			if entry.Version != "" {
				edgeMeta["constraint"] = "==" + entry.Version
			}
			_ = g.AddEdge(dag.Edge{From: deps.ProjectRootNodeID, To: name, Meta: edgeMeta})
		}
	}

	return g
}

// extractPackageName extracts the package name from a node_modules path.
// e.g., "node_modules/lodash" -> "lodash"
// e.g., "node_modules/@types/node" -> "@types/node"
// e.g., "node_modules/foo/node_modules/bar" -> "bar"
func extractPackageName(path string) string {
	// Find the last "node_modules/" segment
	const nm = "node_modules/"
	idx := strings.LastIndex(path, nm)
	if idx == -1 {
		return ""
	}
	name := path[idx+len(nm):]

	// Handle scoped packages (@org/pkg)
	if strings.HasPrefix(name, "@") {
		// The full scoped name is @org/pkg
		return name
	}

	// For non-scoped, take just the first path segment
	if slashIdx := strings.Index(name, "/"); slashIdx != -1 {
		// Check if this is a scoped package that somehow got through
		if !strings.HasPrefix(name, "@") {
			return name[:slashIdx]
		}
	}

	return name
}
