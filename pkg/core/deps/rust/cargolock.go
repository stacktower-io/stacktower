package rust

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/stacktower-io/stacktower/pkg/core/dag"
	"github.com/stacktower-io/stacktower/pkg/core/deps"
	"github.com/stacktower-io/stacktower/pkg/core/deps/constraints"
	"github.com/stacktower-io/stacktower/pkg/observability"
)

// CargoLock parses Cargo.lock files. It provides a full transitive
// closure of the dependency graph without needing to contact a registry.
type CargoLock struct{}

func (c *CargoLock) Type() string              { return "Cargo.lock" }
func (c *CargoLock) IncludesTransitive() bool  { return true }
func (c *CargoLock) Supports(name string) bool { return strings.EqualFold(name, "cargo.lock") }

func (c *CargoLock) Parse(path string, opts deps.Options) (*deps.ManifestResult, error) {
	opts = opts.WithDefaults()

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var lock cargoLockFile
	if err := toml.Unmarshal(data, &lock); err != nil {
		return nil, err
	}

	g := buildCargoLockGraph(lock, opts)
	deps.EnrichGraph(opts.Ctx, g, "Cargo.toml", opts)

	// Extract runtime info from companion Cargo.toml
	cargoInfo := extractCargoTomlInfo(filepath.Dir(path))

	return &deps.ManifestResult{
		Graph:              g,
		Type:               c.Type(),
		IncludesTransitive: true,
		RootPackage:        cargoInfo.Name,
		RuntimeVersion:     cargoInfo.RustVersion,
		RuntimeConstraint:  constraints.NormalizeRuntimeConstraint(cargoInfo.RustVersion),
	}, nil
}

// cargoTomlInfo holds extracted info from Cargo.toml
type cargoTomlInfo struct {
	Name        string
	RustVersion string // MSRV from rust-version field
}

// extractCargoTomlInfo reads the package name and rust-version from Cargo.toml in the same directory.
func extractCargoTomlInfo(dir string) cargoTomlInfo {
	data, err := os.ReadFile(filepath.Join(dir, "Cargo.toml"))
	if err != nil {
		return cargoTomlInfo{}
	}
	var cargo struct {
		Package struct {
			Name        string `toml:"name"`
			RustVersion string `toml:"rust-version"`
		} `toml:"package"`
	}
	if err := toml.Unmarshal(data, &cargo); err != nil {
		return cargoTomlInfo{}
	}
	return cargoTomlInfo{Name: cargo.Package.Name, RustVersion: cargo.Package.RustVersion}
}

// cargoLockFile represents the Cargo.lock file structure
type cargoLockFile struct {
	Version  int                `toml:"version"` // Lock file version (1, 2, or 3)
	Packages []cargoLockPackage `toml:"package"`
}

// cargoLockPackage represents a package entry in Cargo.lock
type cargoLockPackage struct {
	Name         string   `toml:"name"`
	Version      string   `toml:"version"`
	Source       string   `toml:"source"`
	Checksum     string   `toml:"checksum"`
	Dependencies []string `toml:"dependencies"` // Format: "name version" or "name version (source)"
}

func buildCargoLockGraph(lock cargoLockFile, opts deps.Options) *dag.DAG {
	g := dag.New(nil)
	hooks := observability.ResolverFromContext(opts.Ctx)

	// Detect crate names that appear at multiple versions.
	nameCount := make(map[string]int)
	for _, pkg := range lock.Packages {
		nameCount[pkg.Name]++
	}

	// Build a map of package keys (name + version) to their node IDs.
	// When a crate name appears more than once, use "name@version" as the
	// node ID so both versions are preserved. Single-version crates keep
	// just the name for backward compatibility.
	type nodeEntry struct {
		id      string
		name    string
		version string
	}
	pkgKeys := make(map[string]bool)         // "name@version" dedup
	nodeByKey := make(map[string]*nodeEntry) // "name@version" -> entry
	nameToKeys := make(map[string][]string)  // bare name -> list of "name@version" keys

	for _, pkg := range lock.Packages {
		key := pkg.Name + "@" + pkg.Version
		if pkgKeys[key] {
			continue
		}
		pkgKeys[key] = true

		id := pkg.Name
		if nameCount[pkg.Name] > 1 {
			id = deps.BuildPackageID(pkg.Name, pkg.Version, "")
		}

		ne := &nodeEntry{id: id, name: pkg.Name, version: pkg.Version}
		nodeByKey[key] = ne
		nameToKeys[pkg.Name] = append(nameToKeys[pkg.Name], key)

		hooks.OnFetchStart(opts.Ctx, pkg.Name, 0)
		meta := dag.Metadata{"version": pkg.Version}
		if id != pkg.Name {
			meta["name"] = pkg.Name
		}
		if pkg.Source != "" {
			meta["source"] = pkg.Source
		}
		_ = g.AddNode(dag.Node{ID: id, Meta: meta})
		hooks.OnFetchComplete(opts.Ctx, pkg.Name, 0, len(pkg.Dependencies), nil)
	}

	// resolveNodeID finds the node ID for a dependency given name and optional version.
	resolveNodeID := func(name, version string) (string, bool) {
		if version != "" {
			key := name + "@" + version
			if ne, ok := nodeByKey[key]; ok {
				return ne.id, true
			}
		}
		keys := nameToKeys[name]
		if len(keys) == 0 {
			return "", false
		}
		return nodeByKey[keys[0]].id, true
	}

	// Add dependency edges
	incoming := make(map[string]bool)
	for _, pkg := range lock.Packages {
		fromKey := pkg.Name + "@" + pkg.Version
		fromEntry := nodeByKey[fromKey]
		if fromEntry == nil {
			continue
		}

		for _, depSpec := range pkg.Dependencies {
			depName, depVersion := parseCargoLockDep(depSpec)
			if depName == "" {
				continue
			}

			targetID, ok := resolveNodeID(depName, depVersion)
			if !ok {
				continue
			}

			edgeMeta := dag.Metadata{}
			if depVersion != "" {
				edgeMeta["constraint"] = "=" + depVersion
			}
			_ = g.AddEdge(dag.Edge{From: fromEntry.id, To: targetID, Meta: edgeMeta})
			incoming[targetID] = true
		}
	}

	// Add virtual root and connect packages with no incoming edges
	_ = g.AddNode(dag.Node{ID: deps.ProjectRootNodeID, Meta: dag.Metadata{"virtual": true}})

	for _, pkg := range lock.Packages {
		key := pkg.Name + "@" + pkg.Version
		ne := nodeByKey[key]
		if ne == nil {
			continue
		}
		if !incoming[ne.id] {
			edgeMeta := dag.Metadata{}
			if pkg.Version != "" {
				edgeMeta["constraint"] = "=" + pkg.Version
			}
			_ = g.AddEdge(dag.Edge{From: deps.ProjectRootNodeID, To: ne.id, Meta: edgeMeta})
		}
	}

	return g
}

// parseCargoLockDep parses a Cargo.lock dependency string.
// Formats:
//   - "name version" (e.g., "libc 0.2.149")
//   - "name version (source)" (e.g., "libc 0.2.149 (registry+https://github.com/rust-lang/crates.io-index)")
//   - "name" (e.g., "libc" - version-less format in some older lockfiles)
func parseCargoLockDep(spec string) (name, version string) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return "", ""
	}

	// Remove source suffix if present
	if idx := strings.Index(spec, " ("); idx != -1 {
		spec = spec[:idx]
	}

	// Split into name and version
	parts := strings.SplitN(spec, " ", 2)
	name = parts[0]
	if len(parts) > 1 {
		version = parts[1]
	}

	return name, version
}
