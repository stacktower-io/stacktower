package python

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/matzehuels/stacktower/pkg/core/dag"
	"github.com/matzehuels/stacktower/pkg/core/deps"
	"github.com/matzehuels/stacktower/pkg/observability"
)

// pythonVersionRE extracts version from constraints like ">=3.9", ">=3.9,<4", "~=3.8"
var pythonVersionRE = regexp.MustCompile(`[>=~^]*\s*(\d+(?:\.\d+)*)`)

// extractPythonVersion extracts the minimum Python version from a requires-python constraint.
// Examples: ">=3.9" → "3.9", ">=3.9,<4.0" → "3.9", "~=3.8" → "3.8"
func extractPythonVersion(constraint string) string {
	if constraint == "" {
		return ""
	}
	// Take the first constraint in case of comma-separated (e.g., ">=3.9,<4.0")
	parts := strings.Split(constraint, ",")
	if len(parts) > 0 {
		if m := pythonVersionRE.FindStringSubmatch(parts[0]); len(m) > 1 {
			return m[1]
		}
	}
	return ""
}

// UVLock parses uv.lock files. It provides a full transitive closure
// of the dependency graph without needing to contact a registry.
// uv.lock is the lockfile format used by uv (https://github.com/astral-sh/uv).
type UVLock struct{}

func (u *UVLock) Type() string              { return "uv.lock" }
func (u *UVLock) IncludesTransitive() bool  { return true }
func (u *UVLock) Supports(name string) bool { return name == "uv.lock" }

func (u *UVLock) Parse(path string, opts deps.Options) (*deps.ManifestResult, error) {
	opts = opts.WithDefaults()

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var lock uvLockFile
	if err := toml.Unmarshal(data, &lock); err != nil {
		return nil, err
	}

	g := buildUVGraph(opts.Ctx, lock.Packages, opts.DependencyScope)
	deps.EnrichGraph(opts.Ctx, g, "pyproject.toml", opts)

	return &deps.ManifestResult{
		Graph:              g,
		Type:               u.Type(),
		IncludesTransitive: true,
		RootPackage:        extractPyprojectName(filepath.Dir(path)),
		RuntimeVersion:     extractPythonVersion(lock.RequiresPython),
		RuntimeConstraint:  lock.RequiresPython,
	}, nil
}

// uvLockFile represents the top-level structure of a uv.lock file.
type uvLockFile struct {
	Version        int         `toml:"version"`
	RequiresPython string      `toml:"requires-python"`
	Packages       []uvLockPkg `toml:"package"`
}

// uvLockPkg represents a single package in the uv.lock file.
type uvLockPkg struct {
	Name         string           `toml:"name"`
	Version      string           `toml:"version"`
	Source       uvSource         `toml:"source"`
	Dependencies []uvDependency   `toml:"dependencies"`
	OptionalDeps uvOptionalDeps   `toml:"optional-dependencies"`
	DevDeps      uvDevDeps        `toml:"dev-dependencies"`
	Sdist        *uvDistribution  `toml:"sdist"`
	Wheels       []uvDistribution `toml:"wheels"`
}

// uvOptionalDeps handles uv.lock optional-dependencies (extras like [all], [standard]).
// Format: [package.optional-dependencies]\n all = [{ name = "uvicorn" }]
type uvOptionalDeps struct {
	Groups map[string][]uvDependency
}

// UnmarshalTOML implements toml.Unmarshaler for optional-dependencies.
func (d *uvOptionalDeps) UnmarshalTOML(data any) error {
	d.Groups = make(map[string][]uvDependency)
	if m, ok := data.(map[string]any); ok {
		for groupName, rawDeps := range m {
			switch items := rawDeps.(type) {
			case []map[string]any:
				d.Groups[groupName] = decodeDepMaps(items)
			case []interface{}:
				d.Groups[groupName] = decodeDepIfaces(items)
			}
		}
	}
	return nil
}

// uvDevDeps handles uv.lock dev-dependencies in both formats:
//   - v1 (flat list): dev-dependencies = [{ name = "pytest" }]
//   - v2 (group map): [package.dev-dependencies]\n dev = [{ name = "pytest" }]
type uvDevDeps struct {
	Groups map[string][]uvDependency
}

// UnmarshalTOML implements toml.Unmarshaler, accepting either format.
func (d *uvDevDeps) UnmarshalTOML(data any) error {
	d.Groups = make(map[string][]uvDependency)
	switch v := data.(type) {
	case []map[string]any:
		// v1: flat array of inline tables (BurntSushi typed slice)
		d.Groups["dev"] = decodeDepMaps(v)
	case []interface{}:
		// v1: flat array of inline tables (BurntSushi interface slice)
		d.Groups["dev"] = decodeDepIfaces(v)
	case map[string]any:
		// v2: group name → array of inline tables
		for groupName, rawDeps := range v {
			switch items := rawDeps.(type) {
			case []map[string]any:
				d.Groups[groupName] = decodeDepMaps(items)
			case []interface{}:
				d.Groups[groupName] = decodeDepIfaces(items)
			}
		}
	}
	return nil
}

func decodeDepMaps(items []map[string]any) []uvDependency {
	deps := make([]uvDependency, 0, len(items))
	for _, item := range items {
		deps = append(deps, depFromMap(item))
	}
	return deps
}

func decodeDepIfaces(items []interface{}) []uvDependency {
	deps := make([]uvDependency, 0, len(items))
	for _, raw := range items {
		if item, ok := raw.(map[string]any); ok {
			deps = append(deps, depFromMap(item))
		}
	}
	return deps
}

func depFromMap(item map[string]any) uvDependency {
	var dep uvDependency
	if n, ok := item["name"].(string); ok {
		dep.Name = n
	}
	if s, ok := item["specifier"].(string); ok {
		dep.Specifier = s
	}
	if m, ok := item["marker"].(string); ok {
		dep.Marker = m
	}
	return dep
}

// uvSource represents where a package comes from.
type uvSource struct {
	Registry string `toml:"registry"`
	Editable string `toml:"editable"`
	Virtual  string `toml:"virtual"`
	Git      string `toml:"git"`
}

// uvDependency represents a dependency in uv.lock format.
type uvDependency struct {
	Name      string `toml:"name"`
	Specifier string `toml:"specifier"`
	Marker    string `toml:"marker"`
}

// uvDistribution represents download information for a package.
type uvDistribution struct {
	URL  string `toml:"url"`
	Hash string `toml:"hash"`
	Size int64  `toml:"size"`
}

func buildUVGraph(ctx context.Context, packages []uvLockPkg, scope string) *dag.DAG {
	g := dag.New(nil)
	pkgs := make(map[string]bool, len(packages))

	// Virtual/editable packages are the project root(s); they get the
	// projectRoot ID so the caller can rename them to the repo name.
	isProjectRoot := func(pkg uvLockPkg) bool {
		return pkg.Source.Virtual != "" || pkg.Source.Editable != ""
	}
	// nodeID returns the graph ID for a package: project roots use the
	// reserved "__project__" sentinel so RenameNode works correctly.
	nodeID := func(pkg uvLockPkg) string {
		if isProjectRoot(pkg) {
			return deps.ProjectRootNodeID
		}
		return normalize(pkg.Name)
	}

	// Build lookup map for transitive walk
	pkgByName := make(map[string]uvLockPkg, len(packages))
	for _, pkg := range packages {
		pkgByName[normalize(pkg.Name)] = pkg
	}

	// Find production packages by walking transitively from root's direct deps.
	// A package is "prod" if it's reachable via production dependency edges only.
	prodPkgs := make(map[string]bool)
	var walkProd func(name string)
	walkProd = func(name string) {
		if prodPkgs[name] {
			return
		}
		prodPkgs[name] = true
		if pkg, ok := pkgByName[name]; ok {
			for _, dep := range pkg.Dependencies {
				walkProd(normalize(dep.Name))
			}
		}
	}

	// Start walk from root package's production dependencies
	hasProjectRoot := false
	for _, pkg := range packages {
		if isProjectRoot(pkg) {
			hasProjectRoot = true
			for _, dep := range pkg.Dependencies {
				walkProd(normalize(dep.Name))
			}
		}
	}

	// If no project root found, find entry points (packages with no incoming
	// production edges) and walk from those. This handles lock files without
	// an explicit virtual/editable root.
	if !hasProjectRoot {
		// Track packages referenced only via dev/optional dependencies
		prodIncoming := make(map[string]bool)
		nonProdIncoming := make(map[string]bool)
		for _, pkg := range packages {
			for _, dep := range pkg.Dependencies {
				prodIncoming[normalize(dep.Name)] = true
			}
			// Optional dependencies (extras) are not required for base package
			for _, groupDeps := range pkg.OptionalDeps.Groups {
				for _, dep := range groupDeps {
					nonProdIncoming[normalize(dep.Name)] = true
				}
			}
			for _, groupDeps := range pkg.DevDeps.Groups {
				for _, dep := range groupDeps {
					nonProdIncoming[normalize(dep.Name)] = true
				}
			}
		}
		// Remove from nonProd anything that's also a prod dep
		for name := range prodIncoming {
			delete(nonProdIncoming, name)
		}

		// Walk from packages that have no incoming prod edges AND are not dev/optional-only
		for _, pkg := range packages {
			name := normalize(pkg.Name)
			if !prodIncoming[name] && !nonProdIncoming[name] {
				walkProd(name)
			}
		}
	}

	// First pass: add all package nodes.
	// Project-root packages use projectRoot as their ID; their metadata
	// (version, description, etc.) is preserved on that node.
	hooks := observability.ResolverFromContext(ctx)
	for _, pkg := range packages {
		name := normalize(pkg.Name)
		// Skip dev-only packages when scope is prod-only.
		// A package is dev-only if it's not reachable via production deps.
		if scope == deps.DependencyScopeProdOnly && !isProjectRoot(pkg) && !prodPkgs[name] {
			continue
		}
		id := nodeID(pkg)
		pkgs[name] = true
		meta := dag.Metadata{}
		if pkg.Version != "" {
			meta["version"] = pkg.Version
		}
		if pkg.Source.Editable != "" {
			meta["editable"] = true
			meta["path"] = pkg.Source.Editable
		}
		if pkg.Source.Virtual != "" {
			meta["virtual"] = true
			meta["path"] = pkg.Source.Virtual
		}
		if pkg.Source.Git != "" {
			meta["git"] = pkg.Source.Git
		}
		hooks.OnFetchStart(ctx, name, 0)
		_ = g.AddNode(dag.Node{ID: id, Meta: meta})
		hooks.OnFetchComplete(ctx, name, 0, len(pkg.Dependencies), nil)
	}

	// Second pass: add dependency edges, translating project-root source IDs.
	// For lock files, set constraint to pinned version if not explicitly specified.
	incoming := make(map[string]bool)
	for _, pkg := range packages {
		from := nodeID(pkg)

		for _, dep := range pkg.Dependencies {
			to := normalize(dep.Name)
			if pkgs[to] {
				edgeMeta := dag.Metadata{}
				if dep.Specifier != "" {
					edgeMeta["constraint"] = dep.Specifier
				} else if targetPkg, ok := pkgByName[to]; ok && targetPkg.Version != "" {
					// Lock file: use pinned version as constraint
					edgeMeta["constraint"] = "==" + targetPkg.Version
				}
				if dep.Marker != "" {
					edgeMeta["marker"] = dep.Marker
				}
				_ = g.AddEdge(dag.Edge{From: from, To: to, Meta: edgeMeta})
				incoming[to] = true
			}
		}
		if scope == deps.DependencyScopeAll {
			for _, groupDeps := range pkg.DevDeps.Groups {
				for _, dep := range groupDeps {
					to := normalize(dep.Name)
					if pkgs[to] {
						edgeMeta := dag.Metadata{"dev": true}
						if dep.Specifier != "" {
							edgeMeta["constraint"] = dep.Specifier
						} else if targetPkg, ok := pkgByName[to]; ok && targetPkg.Version != "" {
							// Lock file: use pinned version as constraint
							edgeMeta["constraint"] = "==" + targetPkg.Version
						}
						if dep.Marker != "" {
							edgeMeta["marker"] = dep.Marker
						}
						_ = g.AddEdge(dag.Edge{From: from, To: to, Meta: edgeMeta})
						incoming[to] = true
					}
				}
			}
		}
	}

	// If no project-root package was found (unusual), ensure __project__ exists
	// and wire up packages that have no other parent.
	if _, ok := g.Node(deps.ProjectRootNodeID); !ok {
		_ = g.AddNode(dag.Node{ID: deps.ProjectRootNodeID, Meta: dag.Metadata{"virtual": true}})
		for _, pkg := range packages {
			name := normalize(pkg.Name)
			if pkgs[name] && !incoming[name] {
				edgeMeta := dag.Metadata{}
				if pkg.Version != "" {
					edgeMeta["constraint"] = "==" + pkg.Version
				}
				_ = g.AddEdge(dag.Edge{From: deps.ProjectRootNodeID, To: name, Meta: edgeMeta})
			}
		}
	}

	return g
}
