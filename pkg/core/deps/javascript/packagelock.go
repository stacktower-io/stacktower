package javascript

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/stacktower-io/stacktower/pkg/core/dag"
	"github.com/stacktower-io/stacktower/pkg/core/deps"
	"github.com/stacktower-io/stacktower/pkg/observability"
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

// buildFromPackages builds the graph from v2/v3 "packages" format.
// When the same package appears at multiple versions (nested node_modules),
// each distinct version gets its own node with an ID of "name@version" and
// the display name stored in metadata. Single-version packages use just the
// name as the node ID for backward compatibility.
func buildFromPackages(lock packageLockFile, opts deps.Options) *dag.DAG {
	g := dag.New(nil)
	hooks := observability.ResolverFromContext(opts.Ctx)

	type pkgEntry struct {
		name    string
		version string
		path    string
		entry   packageLockEntry
	}

	// First pass: collect all entries keyed by name to detect duplicates.
	byName := make(map[string][]pkgEntry)
	for path, entry := range lock.Packages {
		if path == "" {
			continue
		}
		name := extractPackageName(path)
		if name == "" {
			continue
		}
		// Note: entries with `optional: true` are intentionally kept in
		// prod-only scope. npm installs optional dependencies by default
		// (they are only excluded with --omit=optional), so they are part
		// of a production install. Only dev entries are excluded.
		if opts.DependencyScope == deps.DependencyScopeProdOnly && entry.Dev {
			continue
		}
		byName[name] = append(byName[name], pkgEntry{name: name, version: entry.Version, path: path, entry: entry})
	}

	// Deduplicate: keep one entry per distinct name+version pair.
	// Build a node ID for each: use "name" when there's a single version,
	// "name@version" when multiple versions coexist.
	type nodeInfo struct {
		id      string
		name    string
		version string
		entry   packageLockEntry
	}
	nodes := make(map[string]*nodeInfo)    // nodeID -> info
	nameToIDs := make(map[string][]string) // package name -> list of node IDs

	for _, entries := range byName {
		// Deduplicate by version
		versionSeen := make(map[string]bool)
		var unique []pkgEntry
		for _, e := range entries {
			if !versionSeen[e.version] {
				versionSeen[e.version] = true
				unique = append(unique, e)
			}
		}

		multiVersion := len(unique) > 1
		for _, e := range unique {
			id := e.name
			if multiVersion {
				id = deps.BuildPackageID(e.name, e.version, "")
			}
			if nodes[id] != nil {
				continue
			}
			nodes[id] = &nodeInfo{id: id, name: e.name, version: e.version, entry: e.entry}
			nameToIDs[e.name] = append(nameToIDs[e.name], id)
		}
	}

	// Add all nodes to the graph
	for _, ni := range nodes {
		hooks.OnFetchStart(opts.Ctx, ni.name, 0)
		meta := dag.Metadata{"version": ni.version}
		if ni.id != ni.name {
			meta["name"] = ni.name
		}
		if ni.entry.Dev {
			meta["dev"] = true
		}
		if ni.entry.License != "" {
			meta["license"] = ni.entry.License
		}
		_ = g.AddNode(dag.Node{ID: ni.id, Meta: meta})
		hooks.OnFetchComplete(opts.Ctx, ni.name, 0, len(ni.entry.Dependencies), nil)
	}

	// Add edges. For dependencies, find the matching node ID.
	// When multiple versions exist, use semver constraint matching to pick
	// the correct target instead of comparing the constraint string literally.
	matcher := SemverMatcher{}
	incoming := make(map[string]bool)
	for _, ni := range nodes {
		for depName, constraint := range ni.entry.Dependencies {
			targetIDs := nameToIDs[depName]
			if len(targetIDs) == 0 {
				continue
			}
			targetID := targetIDs[0]
			if len(targetIDs) > 1 && constraint != "" {
				cond := matcher.ParseConstraint(constraint)
				if cond != nil {
					for _, tid := range targetIDs {
						if tn := nodes[tid]; tn != nil {
							pv := matcher.ParseVersion(tn.version)
							if pv != nil && cond.Satisfies(pv) {
								targetID = tid
								break
							}
						}
					}
				}
			}
			edgeMeta := dag.Metadata{}
			if constraint != "" {
				edgeMeta["constraint"] = constraint
			}
			_ = g.AddEdge(dag.Edge{From: ni.id, To: targetID, Meta: edgeMeta})
			incoming[targetID] = true
		}
	}

	// Add virtual root
	_ = g.AddNode(dag.Node{ID: deps.ProjectRootNodeID, Meta: dag.Metadata{"virtual": true}})

	if rootEntry, ok := lock.Packages[""]; ok {
		for depName := range rootEntry.Dependencies {
			targetIDs := nameToIDs[depName]
			if len(targetIDs) == 0 {
				continue
			}
			targetID := targetIDs[0]
			edgeMeta := dag.Metadata{}
			if ni := nodes[targetID]; ni != nil && ni.version != "" {
				edgeMeta["constraint"] = "==" + ni.version
			}
			_ = g.AddEdge(dag.Edge{From: deps.ProjectRootNodeID, To: targetID, Meta: edgeMeta})
		}
	} else {
		for id := range nodes {
			if !incoming[id] {
				edgeMeta := dag.Metadata{}
				if ni := nodes[id]; ni != nil && ni.version != "" {
					edgeMeta["constraint"] = "==" + ni.version
				}
				_ = g.AddEdge(dag.Edge{From: deps.ProjectRootNodeID, To: id, Meta: edgeMeta})
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
// e.g., "node_modules/@scope/pkg/extra" -> "@scope/pkg"
func extractPackageName(path string) string {
	// Find the last "node_modules/" segment
	const nm = "node_modules/"
	idx := strings.LastIndex(path, nm)
	if idx == -1 {
		return ""
	}
	name := path[idx+len(nm):]

	// Handle scoped packages (@org/pkg): keep exactly two path segments,
	// truncating any nested remainder after the package name.
	if strings.HasPrefix(name, "@") {
		if slashIdx := strings.Index(name, "/"); slashIdx != -1 {
			if nextIdx := strings.Index(name[slashIdx+1:], "/"); nextIdx != -1 {
				return name[:slashIdx+1+nextIdx]
			}
		}
		return name
	}

	// For non-scoped, take just the first path segment
	if slashIdx := strings.Index(name, "/"); slashIdx != -1 {
		return name[:slashIdx]
	}

	return name
}
