package javascript

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"golang.org/x/sync/singleflight"

	"github.com/stacktower-io/stacktower/pkg/core/dag"
	"github.com/stacktower-io/stacktower/pkg/core/deps"
	"github.com/stacktower-io/stacktower/pkg/core/deps/constraints"
	"github.com/stacktower-io/stacktower/pkg/observability"
)

// npmFetcher combines the fetch interfaces the greedy resolver needs.
type npmFetcher interface {
	deps.Fetcher
	deps.VersionLister
	deps.RuntimeConstraintLister
}

// npmResolver implements deps.Resolver using a greedy BFS approach that
// mirrors npm's actual install algorithm. Unlike PubGrub (single-version SAT
// solver), this allows multiple versions of the same package — exactly how
// node_modules works. This avoids the combinatorial explosion that PubGrub
// adapters cause for npm's dependency model.
type npmResolver struct {
	fetcher npmFetcher
	matcher SemverMatcher
	sf      singleflight.Group
}

func (r *npmResolver) Name() string { return "npm" }

// Fetch delegates to the underlying fetcher.
func (r *npmResolver) Fetch(ctx context.Context, name string, refresh bool) (*deps.Package, error) {
	return r.fetcher.Fetch(ctx, name, refresh)
}

// FetchVersion delegates to the underlying fetcher.
func (r *npmResolver) FetchVersion(ctx context.Context, name, version string, refresh bool) (*deps.Package, error) {
	return r.fetcher.FetchVersion(ctx, name, version, refresh)
}

// ListVersionsWithConstraints delegates to the underlying fetcher.
func (r *npmResolver) ListVersionsWithConstraints(ctx context.Context, name string, refresh bool) (map[string]string, error) {
	return r.fetcher.ListVersionsWithConstraints(ctx, name, refresh)
}

// ProbeRuntimeConstraint probes runtime requirements for a package.
func (r *npmResolver) ProbeRuntimeConstraint(ctx context.Context, name, version string, refresh bool) (deps.RuntimeConstraintProbe, error) {
	var (
		pkg *deps.Package
		err error
	)
	if version != "" {
		pkg, err = r.fetcher.FetchVersion(ctx, name, version, refresh)
	} else {
		pkg, err = r.fetcher.Fetch(ctx, name, refresh)
	}
	if err != nil {
		return deps.RuntimeConstraintProbe{}, err
	}
	return deps.RuntimeConstraintProbe{
		Constraint: constraints.NormalizeRuntimeConstraint(pkg.RuntimeConstraint),
		MinVersion: constraints.ExtractMinVersion(pkg.RuntimeConstraint),
	}, nil
}

type resolveItem struct {
	name       string
	constraint string
	depth      int
}

// versionEntry caches versions + parsed semver for a package.
type versionEntry struct {
	versions []string
}

func (r *npmResolver) Resolve(ctx context.Context, pkg string, opts deps.Options) (*dag.DAG, error) {
	opts = opts.WithDefaults()

	g := dag.New(nil)

	// nameVersions tracks which versions have been resolved for each package
	// name. Most packages resolve to a single version (bare name as ID);
	// only packages with multiple resolved versions get "name@version" IDs.
	nameVersions := make(map[string]map[string]bool) // name -> set of versions
	// nodePackages tracks resolved key -> *Package for metadata and edges
	nodePackages := make(map[string]*deps.Package) // "name@version" -> pkg

	var mu sync.Mutex
	nodeCount := 0

	// versionCache avoids re-listing versions for the same package
	versionCache := make(map[string]*versionEntry)
	var vcMu sync.Mutex

	listVersions := func(name string) ([]string, error) {
		vcMu.Lock()
		if entry, ok := versionCache[name]; ok {
			vcMu.Unlock()
			return entry.versions, nil
		}
		vcMu.Unlock()

		result, err, _ := r.sf.Do("versions:"+name, func() (any, error) {
			versions, err := r.fetcher.ListVersions(ctx, name, opts.Refresh)
			if err != nil {
				return nil, err
			}
			// Sort newest-first for greedy "pick latest matching" strategy
			sortVersionsDescending(versions)
			return versions, nil
		})
		if err != nil {
			return nil, err
		}
		versions := result.([]string)
		vcMu.Lock()
		versionCache[name] = &versionEntry{versions: versions}
		vcMu.Unlock()
		return versions, nil
	}

	// pickVersion finds the latest version satisfying a constraint.
	pickVersion := func(name, constraint string) (string, error) {
		if constraint == "" || constraint == "*" || constraint == "latest" {
			pkg, err := r.fetcher.Fetch(ctx, name, opts.Refresh)
			if err != nil {
				return "", err
			}
			return pkg.Version, nil
		}
		versions, err := listVersions(name)
		if err != nil {
			return "", err
		}
		cond := r.matcher.ParseConstraint(constraint)
		if cond == nil {
			if len(versions) > 0 {
				return versions[0], nil
			}
			return "", fmt.Errorf("no versions found for %s", name)
		}
		for _, v := range versions {
			pv := r.matcher.ParseVersion(v)
			if pv != nil && cond.Satisfies(pv) {
				if !opts.IncludePrerelease && deps.IsPrereleaseVersion(v) {
					if !deps.IsPrereleaseVersion(constraint) {
						continue
					}
				}
				return v, nil
			}
		}
		return "", fmt.Errorf("no version of %s satisfies %s", name, constraint)
	}

	// BFS with bounded parallelism
	sem := make(chan struct{}, opts.Workers)
	var wg sync.WaitGroup

	var processItem func(item resolveItem)
	processItem = func(item resolveItem) {
		defer wg.Done()

		if ctx.Err() != nil {
			return
		}

		depth := item.depth
		if opts.MaxDepth > 0 && depth > opts.MaxDepth {
			return
		}

		mu.Lock()
		if nodeCount >= opts.MaxNodes {
			mu.Unlock()
			return
		}
		mu.Unlock()

		observability.ResolverFromContext(ctx).OnFetchStart(ctx, item.name, depth)

		version, err := pickVersion(item.name, item.constraint)
		if err != nil {
			opts.Logger("resolve %s: %v", item.name, err)
			observability.ResolverFromContext(ctx).OnFetchComplete(ctx, item.name, depth, 0, err)
			return
		}

		// Deduplicate: if we already resolved this exact name@version, skip.
		key := item.name + "@" + version
		mu.Lock()
		if _, exists := nodePackages[key]; exists {
			mu.Unlock()
			observability.ResolverFromContext(ctx).OnFetchComplete(ctx, item.name, depth, 0, nil)
			return
		}
		if nodeCount >= opts.MaxNodes {
			mu.Unlock()
			return
		}
		// Reserve the slot
		nodePackages[key] = nil
		if nameVersions[item.name] == nil {
			nameVersions[item.name] = make(map[string]bool)
		}
		nameVersions[item.name][version] = true
		nodeCount++
		mu.Unlock()

		fetchedPkg, err := r.fetcher.FetchVersion(ctx, item.name, version, opts.Refresh)
		if err != nil || fetchedPkg == nil {
			if err != nil {
				opts.Logger("fetch %s@%s: %v", item.name, version, err)
			}
			observability.ResolverFromContext(ctx).OnFetchComplete(ctx, item.name, depth, 0, err)
			// Release the reserved slot so failed fetches don't consume MaxNodes budget.
			mu.Lock()
			nodeCount--
			delete(nodePackages, key)
			if nameVersions[item.name] != nil {
				delete(nameVersions[item.name], version)
				if len(nameVersions[item.name]) == 0 {
					delete(nameVersions, item.name)
				}
			}
			mu.Unlock()
			return
		}

		mu.Lock()
		nodePackages[key] = fetchedPkg
		mu.Unlock()

		observability.ResolverFromContext(ctx).OnFetchComplete(ctx, item.name, depth, len(fetchedPkg.Dependencies), nil)

		for _, dep := range fetchedPkg.Dependencies {
			if ctx.Err() != nil {
				return
			}
			wg.Add(1)
			go func(d deps.Dependency) {
				sem <- struct{}{}
				defer func() { <-sem }()
				processItem(resolveItem{
					name:       d.Name,
					constraint: d.Constraint,
					depth:      depth + 1,
				})
			}(dep)
		}
	}

	// Resolve root package
	observability.ResolverFromContext(ctx).OnFetchStart(ctx, pkg, 0)
	var rootPkg *deps.Package
	var rootErr error
	if opts.Version != "" {
		rootPkg, rootErr = r.fetcher.FetchVersion(ctx, pkg, opts.Version, opts.Refresh)
	} else {
		rootPkg, rootErr = r.fetcher.Fetch(ctx, pkg, opts.Refresh)
	}
	if rootErr != nil {
		return nil, fmt.Errorf("fetch root package: %w", rootErr)
	}
	observability.ResolverFromContext(ctx).OnFetchComplete(ctx, pkg, 0, len(rootPkg.Dependencies), nil)

	rootKey := pkg + "@" + rootPkg.Version
	nodePackages[rootKey] = rootPkg
	nameVersions[pkg] = map[string]bool{rootPkg.Version: true}
	nodeCount++

	// Enqueue root's deps
	for _, dep := range rootPkg.Dependencies {
		wg.Add(1)
		go func(d deps.Dependency) {
			sem <- struct{}{}
			defer func() { <-sem }()
			processItem(resolveItem{
				name:       d.Name,
				constraint: d.Constraint,
				depth:      1,
			})
		}(dep)
	}

	wg.Wait()

	// Build node ID mapping: use bare "name" when only one version was resolved,
	// "name@version" when multiple versions coexist.
	keyToNodeID := make(map[string]string, len(nodePackages))
	for key := range nodePackages {
		name, _ := splitLastAt(key)
		if len(nameVersions[name]) > 1 {
			keyToNodeID[key] = key
		} else {
			keyToNodeID[key] = name
		}
	}

	// Add nodes
	for key, nodeID := range keyToNodeID {
		pkg := nodePackages[key]
		if pkg == nil {
			continue
		}
		meta := pkg.Metadata()
		name, _ := splitLastAt(key)
		if nodeID != name {
			meta["name"] = name
		}
		_ = g.AddNode(dag.Node{ID: nodeID, Meta: meta})
	}

	// Add edges. Build a name -> node IDs index once so each dependency is a
	// map lookup instead of a scan over every resolved package (O(n²)).
	nameToNodeIDs := make(map[string][]string, len(keyToNodeID))
	for resolvedKey, nodeID := range keyToNodeID {
		name, _ := splitLastAt(resolvedKey)
		nameToNodeIDs[name] = append(nameToNodeIDs[name], nodeID)
	}
	for key, pkg := range nodePackages {
		if pkg == nil {
			continue
		}
		fromID := keyToNodeID[key]
		for _, dep := range pkg.Dependencies {
			targetIDs := nameToNodeIDs[dep.Name]
			if len(targetIDs) == 0 {
				continue
			}
			// When multiple versions of the same package coexist, pick the
			// one whose version satisfies the dependency constraint instead
			// of wiring edges to every resolved version.
			toID := targetIDs[0]
			if len(targetIDs) > 1 && dep.Constraint != "" {
				cond := r.matcher.ParseConstraint(dep.Constraint)
				if cond != nil {
					for _, candidate := range targetIDs {
						_, ver := splitLastAt(candidate)
						pv := r.matcher.ParseVersion(ver)
						if pv != nil && cond.Satisfies(pv) {
							toID = candidate
							break
						}
					}
				}
			}
			edgeMeta := dag.Metadata{}
			if dep.Constraint != "" {
				edgeMeta["constraint"] = dep.Constraint
			}
			_ = g.AddEdge(dag.Edge{From: fromID, To: toID, Meta: edgeMeta})
		}
	}

	// Enrich metadata (GitHub stars, repo URLs, etc.)
	if len(opts.MetadataProviders) > 0 {
		refs := make([]*deps.PackageRef, 0, len(nodePackages))
		for _, pkg := range nodePackages {
			if pkg != nil {
				refs = append(refs, pkg.Ref())
			}
		}
		for _, provider := range opts.MetadataProviders {
			bp, ok := provider.(deps.BatchMetadataProvider)
			if !ok {
				continue
			}
			enriched, err := bp.EnrichBatch(ctx, refs, opts.Refresh)
			if err != nil {
				opts.Logger("batch enrich: %v", err)
				break
			}
			for key, nodeID := range keyToNodeID {
				pkg := nodePackages[key]
				if pkg == nil {
					continue
				}
				if n, ok := g.Node(nodeID); ok {
					if extra, ok := enriched[pkg.Name]; ok {
						for k, v := range extra {
							n.Meta[k] = v
						}
					}
				}
			}
			break
		}
	}

	observability.ResolverFromContext(ctx).OnProgress(ctx, len(nodePackages), 0, opts.MaxNodes)
	return g, nil
}

// splitLastAt splits "name@version" on the last '@', correctly handling
// scoped npm names like "@scope/pkg@1.0.0" → ("@scope/pkg", "1.0.0").
func splitLastAt(s string) (name, version string) {
	idx := strings.LastIndex(s, "@")
	if idx <= 0 {
		return s, ""
	}
	return s[:idx], s[idx+1:]
}

// sortVersionsDescending sorts version strings newest-first using semver
// comparison rather than lexicographic ordering (which would put "9.0.0"
// above "10.0.0"). Stable versions sort above prereleases of the same
// release; unparseable versions sort last (by string, descending).
func sortVersionsDescending(versions []string) {
	parsed := make(map[string]semverVersion, len(versions))
	for _, v := range versions {
		parsed[v] = parseSemver(v)
	}
	sort.SliceStable(versions, func(i, j int) bool {
		return compareSemver(parsed[versions[i]], parsed[versions[j]]) > 0
	})
}

// compareSemver compares two parsed semver versions.
// Returns >0 if a > b, <0 if a < b, 0 if equal.
func compareSemver(a, b semverVersion) int {
	switch {
	case !a.valid && !b.valid:
		return strings.Compare(a.original, b.original)
	case !a.valid:
		return -1
	case !b.valid:
		return 1
	}
	if a.major != b.major {
		return a.major - b.major
	}
	if a.minor != b.minor {
		return a.minor - b.minor
	}
	if a.patch != b.patch {
		return a.patch - b.patch
	}
	// Per semver, a version with a prerelease tag sorts below the release.
	switch {
	case a.prerelease == b.prerelease:
		return strings.Compare(a.prerelease, b.prerelease)
	case a.prerelease == "":
		return 1
	case b.prerelease == "":
		return -1
	}
	return strings.Compare(a.prerelease, b.prerelease)
}
