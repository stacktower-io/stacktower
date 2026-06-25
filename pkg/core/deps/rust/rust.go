package rust

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/contriboss/pubgrub-go"
	"golang.org/x/sync/singleflight"

	"github.com/stacktower-io/stacktower/pkg/cache"
	"github.com/stacktower-io/stacktower/pkg/core/dag"
	"github.com/stacktower-io/stacktower/pkg/core/deps"
	"github.com/stacktower-io/stacktower/pkg/core/deps/constraints"
	"github.com/stacktower-io/stacktower/pkg/integrations/crates"
	"github.com/stacktower-io/stacktower/pkg/observability"
)

// Language provides Rust dependency resolution via crates.io.
// Supports Cargo.toml and Cargo.lock manifest files.
var Language = &deps.Language{
	Name:                  "rust",
	DefaultRegistry:       "crates",
	DefaultRuntimeVersion: "1.75",
	RegistryAliases:       map[string]string{"crates.io": "crates"},
	ManifestTypes:         []string{"cargo", "cargo-lock"},
	ManifestAliases: map[string]string{
		"Cargo.toml": "cargo",
		"cargo.toml": "cargo",
		"Cargo.lock": "cargo-lock",
		"cargo.lock": "cargo-lock",
	},
	NewResolver:     newResolver,
	NewManifest:     newManifest,
	ManifestParsers: manifestParsers,
	NormalizeName: func(name string) string {
		return strings.ToLower(strings.TrimSpace(name))
	},
}

func newManifest(name string, res deps.Resolver) deps.ManifestParser {
	switch name {
	case "cargo":
		return &CargoToml{resolver: res}
	case "cargo-lock":
		return &CargoLock{} // Lock file doesn't need resolver
	default:
		return nil
	}
}

func manifestParsers(res deps.Resolver) []deps.ManifestParser {
	// Lock file first (more complete), then manifest
	return []deps.ManifestParser{
		&CargoLock{},
		&CargoToml{resolver: res},
	}
}

func newResolver(backend cache.Cache, opts deps.Options) (deps.Resolver, error) {
	c := crates.NewClient(backend, opts.CacheTTL)
	f := fetcher{client: c, rustVersion: opts.RuntimeVersion}
	return &cargoResolver{fetcher: f, matcher: CargoMatcher{}}, nil
}

// ---------------------------------------------------------------------------
// cargoResolver: PubGrub-based resolver with major-version namespacing
// ---------------------------------------------------------------------------

// cargoResolver uses PubGrub with major-version-namespaced package names to
// support Cargo's model of allowing multiple semver-incompatible versions of
// the same crate in one dependency graph (e.g. thiserror 1.x and 2.x).
//
// Each crate is split into buckets by its semver compatibility key:
//
//	major >= 1  →  "crate/MAJOR"       (serde/1, thiserror/2)
//	0.minor > 0 →  "crate/0.MINOR"     (rand/0.8)
//	0.0.patch   →  "crate/0.0.PATCH"   (tiny/0.0.3)
//
// PubGrub then treats each bucket as an independent package, keeping its
// single-version-per-name invariant while correctly modelling Cargo semantics.
type cargoResolver struct {
	fetcher fetcher
	matcher CargoMatcher
}

func (r *cargoResolver) Name() string { return "crates.io" }

func (r *cargoResolver) Fetch(ctx context.Context, name string, refresh bool) (*deps.Package, error) {
	return r.fetcher.Fetch(ctx, name, refresh)
}

func (r *cargoResolver) FetchVersion(ctx context.Context, name, version string, refresh bool) (*deps.Package, error) {
	return r.fetcher.FetchVersion(ctx, name, version, refresh)
}

func (r *cargoResolver) ListVersions(ctx context.Context, name string, refresh bool) ([]string, error) {
	return r.fetcher.ListVersions(ctx, name, refresh)
}

func (r *cargoResolver) ListVersionsWithConstraints(ctx context.Context, name string, refresh bool) (map[string]string, error) {
	return r.fetcher.ListVersionsWithConstraints(ctx, name, refresh)
}

func (r *cargoResolver) Resolve(ctx context.Context, pkg string, opts deps.Options) (*dag.DAG, error) {
	opts = opts.WithDefaults()
	hooks := observability.ResolverFromContext(ctx)

	// Determine root version and PubGrub condition.
	var rootVersion string
	var rootCondition pubgrub.Condition

	switch {
	case opts.Version != "":
		rootVersion = opts.Version
		rootCondition = pubgrub.EqualsCondition{Version: r.matcher.ParseVersion(opts.Version)}

	case opts.Constraint != "":
		rootCondition = r.matcher.ParseConstraint(opts.Constraint)
		if rootCondition == nil {
			return nil, fmt.Errorf("invalid root constraint: %q", opts.Constraint)
		}

	default:
		hooks.OnFetchStart(ctx, pkg, 0)
		latest, err := r.fetcher.Fetch(ctx, pkg, opts.Refresh)
		depCount := 0
		if latest != nil {
			depCount = len(latest.Dependencies)
		}
		hooks.OnFetchComplete(ctx, pkg, 0, depCount, err)
		if err != nil {
			return nil, fmt.Errorf("fetch root package: %w", err)
		}
		rootVersion = latest.Version
		rootCondition = pubgrub.EqualsCondition{Version: r.matcher.ParseVersion(latest.Version)}
	}

	// Determine root bucket.
	var rootBucket string
	if rootVersion != "" {
		if cv := parseCargoVersion(rootVersion); cv.valid {
			rootBucket = cargoMajorBucket(cv.major, cv.minor, cv.patch)
		}
	} else if opts.Constraint != "" {
		rootBucket = cargoCompatBucket(opts.Constraint)
	}
	rootNsName := cargoNamespacedName(pkg, rootBucket)

	// Build PubGrub source.
	source := &cargoSource{
		ctx:     ctx,
		fetcher: r.fetcher,
		matcher: r.matcher,
		opts:    opts,
		cache:   make(map[string]*deps.Package),
		seen:    make(map[string]bool),
		depth:   make(map[string]int),
		buckets: make(map[string]string),
	}
	defer source.clearCache()
	source.allowPackage(rootNsName, 0)

	// Solve.
	root := pubgrub.NewRootSource()
	root.AddPackage(pubgrub.MakeName(rootNsName), rootCondition)

	solver := pubgrub.NewSolver(root, source).EnableIncompatibilityTracking()
	solution, err := solver.Solve(root.Term())
	if err != nil {
		if nsErr, ok := err.(*pubgrub.NoSolutionError); ok {
			return nil, fmt.Errorf("dependency resolution failed:\n%s", nsErr.Error())
		}
		return nil, fmt.Errorf("solve: %w", err)
	}

	return r.solutionToDAG(ctx, solution, pkg, rootNsName, source, opts)
}

func (r *cargoResolver) ProbeRuntimeConstraint(ctx context.Context, name, version string, refresh bool) (deps.RuntimeConstraintProbe, error) {
	var (
		pkg *deps.Package
		err error
	)
	if version != "" {
		pkg, err = r.fetcher.FetchVersion(ctx, name, version, refresh)
	} else {
		pkg, err = r.fetcher.Fetch(ctx, name, refresh)
	}
	if err != nil || pkg == nil {
		return deps.RuntimeConstraintProbe{}, err
	}
	constraint := constraints.NormalizeRuntimeConstraint(pkg.RuntimeConstraint)
	if constraint == "" {
		return deps.RuntimeConstraintProbe{}, nil
	}
	return deps.RuntimeConstraintProbe{
		Constraint: constraint,
		MinVersion: constraints.ExtractMinVersion(constraint),
	}, nil
}

// solutionToDAG converts a PubGrub solution into a DAG with real crate names.
func (r *cargoResolver) solutionToDAG(
	ctx context.Context,
	solution pubgrub.Solution,
	rootPkg string,
	rootNsName string,
	source *cargoSource,
	opts deps.Options,
) (*dag.DAG, error) {
	g := dag.New(nil)

	type resolved struct {
		nsName, realName, version, nodeID string
	}

	// Count how many versions of each crate were resolved so single-version
	// crates can use bare names as node IDs.
	realNameCount := make(map[string]int)
	for _, nv := range solution {
		ns := nv.Name.Value()
		if ns == "$$root" {
			continue
		}
		real, _ := cargoSplitNamespacedName(ns)
		realNameCount[real]++
	}

	var entries []resolved
	nsToNodeID := make(map[string]string)
	realToNsNames := make(map[string][]string)

	for _, nv := range solution {
		ns := nv.Name.Value()
		if ns == "$$root" {
			continue
		}
		real, _ := cargoSplitNamespacedName(ns)
		ver := nv.Version.String()

		var id string
		if ns == rootNsName {
			id = rootPkg
		} else if realNameCount[real] > 1 {
			id = deps.BuildPackageID(real, ver, "")
		} else {
			id = real
		}

		entries = append(entries, resolved{ns, real, ver, id})
		nsToNodeID[ns] = id
		realToNsNames[real] = append(realToNsNames[real], ns)
	}

	// Fetch package metadata in parallel. Use the sparse index (same source
	// the solver used) for dependency edges, since the REST API includes all
	// optional deps regardless of features. REST API metadata (description,
	// downloads) is overlaid where available.
	type enrichedPackage struct {
		index *deps.Package // deps from sparse index (for edges)
		rest  *deps.Package // full metadata from REST API (for display)
	}
	packages, _ := deps.ParallelMapOrdered(ctx, 10, entries, func(ctx context.Context, e resolved) enrichedPackage {
		indexPkg, _ := source.getPackage(e.realName, e.version)
		restPkg, err := r.fetcher.FetchVersion(ctx, e.realName, e.version, opts.Refresh)
		if err != nil {
			opts.Logger("enrich %s@%s: %v", e.realName, e.version, err)
		}
		return enrichedPackage{index: indexPkg, rest: restPkg}
	})

	// First pass: add all nodes
	for i, e := range entries {
		ep := packages[i]
		displayPkg := ep.rest
		if displayPkg == nil {
			displayPkg = ep.index
		}
		if displayPkg == nil {
			_ = g.AddNode(dag.Node{ID: e.nodeID})
			continue
		}

		meta := displayPkg.Metadata()
		if e.nodeID != e.realName {
			meta["name"] = e.realName
		}
		_ = g.AddNode(dag.Node{ID: e.nodeID, Meta: meta})
	}

	// Second pass: add edges (all nodes exist now)
	for i, e := range entries {
		ep := packages[i]
		edgePkg := ep.index
		if edgePkg == nil {
			if ep.rest != nil {
				edgePkg = ep.rest
			} else {
				continue
			}
		}
		for _, dep := range edgePkg.Dependencies {
			bucket := cargoCompatBucket(dep.Constraint)
			if bucket == "" {
				bucket = source.cachedBucket(dep.Name)
			}
			nsName := cargoNamespacedName(dep.Name, bucket)
			childID, ok := nsToNodeID[nsName]
			if !ok {
				for _, ns := range realToNsNames[dep.Name] {
					if id, found := nsToNodeID[ns]; found {
						childID = id
						ok = true
						break
					}
				}
			}
			if !ok {
				continue
			}
			edgeMeta := dag.Metadata{}
			if dep.Constraint != "" {
				edgeMeta["constraint"] = dep.Constraint
			}
			_ = g.AddEdge(dag.Edge{From: e.nodeID, To: childID, Meta: edgeMeta})
		}
	}

	// Enrich nodes with external metadata (GitHub stars, repo URLs, etc.)
	if len(opts.MetadataProviders) > 0 {
		refs := make([]*deps.PackageRef, 0, len(entries))
		nodeIDByName := make(map[string]string, len(entries))
		for i, e := range entries {
			ep := packages[i]
			if ep.rest != nil {
				ref := ep.rest.Ref()
				refs = append(refs, ref)
				nodeIDByName[ref.Name] = e.nodeID
			} else if ep.index != nil {
				ref := ep.index.Ref()
				refs = append(refs, ref)
				nodeIDByName[ref.Name] = e.nodeID
			}
		}
		for _, p := range opts.MetadataProviders {
			bp, ok := p.(deps.BatchMetadataProvider)
			if !ok {
				continue
			}
			batch, err := bp.EnrichBatch(ctx, refs, opts.Refresh)
			if err != nil {
				opts.Logger("batch enrich (%s): %v", p.Name(), err)
				continue
			}
			for name, extra := range batch {
				nodeID, ok := nodeIDByName[name]
				if !ok {
					continue
				}
				if n, found := g.Node(nodeID); found {
					for k, v := range extra {
						n.Meta[k] = v
					}
				}
			}
			break
		}
	}

	observability.ResolverFromContext(ctx).OnProgress(ctx, len(entries), 0, opts.MaxNodes)
	return g, nil
}

// ---------------------------------------------------------------------------
// cargoSource implements pubgrub.Source with major-version namespacing
// ---------------------------------------------------------------------------

type cargoSource struct {
	ctx     context.Context
	fetcher fetcher
	matcher CargoMatcher
	opts    deps.Options

	mu          sync.Mutex
	cache       map[string]*deps.Package     // "name@version" → package
	seen        map[string]bool              // namespaced names admitted into solving
	depth       map[string]int               // best-known depth by namespaced name
	buckets     map[string]string            // bare crate name → bucket (for wildcard deps)
	runtimeCons map[string]map[string]string // bare crate name → version → runtime constraint
	fetchGroup  singleflight.Group
}

// listRuntimeConstraints returns the version → runtime-constraint map for a
// crate, memoized per source. GetVersions is called repeatedly for the same
// crate during solving (each namespaced bucket triggers a lookup), so without
// memoization the constraint listing would be re-fetched on every call.
func (s *cargoSource) listRuntimeConstraints(realName string) map[string]string {
	s.mu.Lock()
	if s.runtimeCons != nil {
		if cons, ok := s.runtimeCons[realName]; ok {
			s.mu.Unlock()
			return cons
		}
	}
	s.mu.Unlock()

	result, _, _ := s.fetchGroup.Do("runtime-constraints:"+realName, func() (any, error) {
		// Cache failures as nil too: re-fetching on every GetVersions call
		// would re-trigger the same error path.
		cons, _ := s.fetcher.ListVersionsWithConstraints(s.ctx, realName, s.opts.Refresh)

		s.mu.Lock()
		if s.runtimeCons == nil {
			s.runtimeCons = make(map[string]map[string]string)
		}
		s.runtimeCons[realName] = cons
		s.mu.Unlock()
		return cons, nil
	})
	cons, _ := result.(map[string]string)
	return cons
}

func (s *cargoSource) GetVersions(name pubgrub.Name) ([]pubgrub.Version, error) {
	if s.ctx.Err() != nil {
		return nil, s.ctx.Err()
	}

	realName, bucket := cargoSplitNamespacedName(name.Value())

	hooks := observability.ResolverFromContext(s.ctx)
	hooks.OnFetchStart(s.ctx, realName, 0)
	versions, err := s.fetcher.ListVersions(s.ctx, realName, s.opts.Refresh)
	hooks.OnFetchComplete(s.ctx, realName, 0, 0, err)
	if err != nil {
		return nil, err
	}

	var runtimeConstraints map[string]string
	if s.opts.RuntimeVersion != "" {
		runtimeConstraints = s.listRuntimeConstraints(realName)
	}

	result := make([]pubgrub.Version, 0, len(versions))
	for _, v := range versions {
		if !s.opts.IncludePrerelease && deps.IsPrereleaseVersion(v) {
			continue
		}
		if bucket != "" {
			cv := parseCargoVersion(v)
			if !cv.valid || cargoMajorBucket(cv.major, cv.minor, cv.patch) != bucket {
				continue
			}
		}
		if s.opts.RuntimeVersion != "" && runtimeConstraints != nil {
			if rc, ok := runtimeConstraints[v]; ok && rc != "" &&
				!constraints.CheckVersionConstraint(s.opts.RuntimeVersion, rc) {
				continue
			}
		}
		if pv := s.matcher.ParseVersion(v); pv != nil {
			result = append(result, pv)
		}
	}
	return result, nil
}

func (s *cargoSource) GetDependencies(name pubgrub.Name, version pubgrub.Version) ([]pubgrub.Term, error) {
	if s.ctx.Err() != nil {
		return nil, s.ctx.Err()
	}

	nsName := name.Value()
	realName, _ := cargoSplitNamespacedName(nsName)

	currDepth, ok := s.packageDepth(nsName)
	if !ok {
		currDepth = 0
		s.allowPackage(nsName, currDepth)
	}
	if s.opts.MaxDepth > 0 && currDepth >= s.opts.MaxDepth {
		return nil, nil
	}

	pkg, err := s.getPackage(realName, version.String())
	if err != nil {
		s.opts.Logger("fetch %s@%s: %v", realName, version.String(), err)
		return nil, err
	}

	anyVer := cargoAnyVersionCondition()
	terms := make([]pubgrub.Term, 0, len(pkg.Dependencies))

	for _, dep := range pkg.Dependencies {
		bucket := cargoCompatBucket(dep.Constraint)
		if bucket == "" {
			bucket = s.resolveBucket(dep.Name)
		}

		nsDepName := cargoNamespacedName(dep.Name, bucket)
		childDepth := currDepth + 1
		if !s.allowPackage(nsDepName, childDepth) {
			continue
		}

		var cond pubgrub.Condition
		if dep.Constraint != "" {
			cond = s.matcher.ParseConstraint(dep.Constraint)
		}
		if cond == nil {
			cond = anyVer
		}

		terms = append(terms, pubgrub.NewTerm(pubgrub.MakeName(nsDepName), cond))
	}
	return terms, nil
}

// resolveBucket determines the major-version bucket for a dep with no parseable
// constraint by fetching the latest version. The result is cached.
func (s *cargoSource) resolveBucket(name string) string {
	s.mu.Lock()
	if b, ok := s.buckets[name]; ok {
		s.mu.Unlock()
		return b
	}
	s.mu.Unlock()

	latest, err := s.getLatestPackage(name)
	if err != nil || latest == nil {
		return ""
	}
	cv := parseCargoVersion(latest.Version)
	if !cv.valid {
		return ""
	}
	bucket := cargoMajorBucket(cv.major, cv.minor, cv.patch)

	s.mu.Lock()
	s.buckets[name] = bucket
	s.mu.Unlock()
	return bucket
}

func (s *cargoSource) cachedBucket(name string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buckets[name]
}

func (s *cargoSource) packageDepth(name string) (int, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.depth[name]
	return d, ok
}

func (s *cargoSource) allowPackage(name string, depth int) bool {
	if name == "" {
		return false
	}
	if s.opts.MaxDepth > 0 && depth > s.opts.MaxDepth {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if prev, ok := s.depth[name]; ok {
		if depth < prev {
			s.depth[name] = depth
		}
		s.seen[name] = true
		return true
	}
	if s.opts.MaxNodes > 0 && len(s.seen) >= s.opts.MaxNodes {
		return false
	}
	s.seen[name] = true
	s.depth[name] = depth
	return true
}

// getPackage fetches deps from the sparse index only (no REST API metadata).
// This is the hot path during PubGrub solving.
func (s *cargoSource) getPackage(name, version string) (*deps.Package, error) {
	key := name + "@" + version

	s.mu.Lock()
	if pkg, ok := s.cache[key]; ok {
		s.mu.Unlock()
		return pkg, nil
	}
	s.mu.Unlock()

	result, err, _ := s.fetchGroup.Do(key, func() (any, error) {
		s.mu.Lock()
		if pkg, ok := s.cache[key]; ok {
			s.mu.Unlock()
			return pkg, nil
		}
		s.mu.Unlock()

		hooks := observability.ResolverFromContext(s.ctx)
		hooks.OnFetchStart(s.ctx, name, 0)
		pkg, err := s.fetcher.FetchVersionFromIndex(s.ctx, name, version, s.opts.Refresh)
		depCount := 0
		if pkg != nil {
			depCount = len(pkg.Dependencies)
		}
		hooks.OnFetchComplete(s.ctx, name, 0, depCount, err)
		if err != nil {
			return nil, err
		}

		s.mu.Lock()
		s.cache[key] = pkg
		s.mu.Unlock()
		return pkg, nil
	})
	if err != nil {
		return nil, err
	}
	return result.(*deps.Package), nil
}

func (s *cargoSource) getLatestPackage(name string) (*deps.Package, error) {
	key := name + "@latest"

	s.mu.Lock()
	if pkg, ok := s.cache[key]; ok {
		s.mu.Unlock()
		return pkg, nil
	}
	s.mu.Unlock()

	result, err, _ := s.fetchGroup.Do(key, func() (any, error) {
		s.mu.Lock()
		if pkg, ok := s.cache[key]; ok {
			s.mu.Unlock()
			return pkg, nil
		}
		s.mu.Unlock()

		pkg, err := s.fetcher.Fetch(s.ctx, name, s.opts.Refresh)
		if err != nil {
			return nil, err
		}

		s.mu.Lock()
		s.cache[key] = pkg
		s.mu.Unlock()
		return pkg, nil
	})
	if err != nil {
		return nil, err
	}
	return result.(*deps.Package), nil
}

func (s *cargoSource) clearCache() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cache = nil
	s.seen = nil
	s.depth = nil
	s.buckets = nil
	s.runtimeCons = nil
}

// ---------------------------------------------------------------------------
// Major-version bucketing helpers
// ---------------------------------------------------------------------------

// cargoMajorBucket returns the Cargo semver compatibility key for a version.
// Follows Cargo's caret rules:
//
//	major >= 1  →  "1", "2", …
//	0.minor > 0 →  "0.2", "0.9", …
//	0.0.patch   →  "0.0.3", …
func cargoMajorBucket(major, minor, patch int) string {
	if major > 0 {
		return fmt.Sprintf("%d", major)
	}
	if minor > 0 {
		return fmt.Sprintf("0.%d", minor)
	}
	return fmt.Sprintf("0.0.%d", patch)
}

// cargoCompatBucket extracts the compatibility bucket from a Cargo constraint
// string by parsing the lower-bound version.
func cargoCompatBucket(constraint string) string {
	constraint = strings.TrimSpace(constraint)
	if constraint == "" || constraint == "*" {
		return ""
	}
	// For compound constraints, derive bucket from the lower bound (first part).
	part := strings.TrimSpace(strings.SplitN(constraint, ",", 2)[0])

	m := cargoOperatorRE.FindStringSubmatch(part)
	if m == nil {
		return ""
	}
	cv := parseCargoVersion(m[2])
	if !cv.valid {
		return ""
	}
	return cargoMajorBucket(cv.major, cv.minor, cv.patch)
}

// cargoNamespacedName appends the bucket suffix to produce a PubGrub-safe name
// (e.g. "thiserror" + "2" → "thiserror/2").
func cargoNamespacedName(name, bucket string) string {
	if bucket == "" {
		return name
	}
	return name + "/" + bucket
}

// cargoSplitNamespacedName splits "thiserror/2" → ("thiserror", "2").
func cargoSplitNamespacedName(name string) (realName, bucket string) {
	idx := strings.LastIndex(name, "/")
	if idx < 0 {
		return name, ""
	}
	suffix := name[idx+1:]
	if len(suffix) > 0 && suffix[0] >= '0' && suffix[0] <= '9' {
		return name[:idx], suffix
	}
	return name, ""
}

func cargoAnyVersionCondition() pubgrub.Condition {
	vs, _ := pubgrub.ParseVersionRange("*")
	return pubgrub.NewVersionSetCondition(vs)
}

// ---------------------------------------------------------------------------
// fetcher wraps the crates.io client for deps.Fetcher / deps.VersionLister
// ---------------------------------------------------------------------------

type fetcher struct {
	client      *crates.Client
	rustVersion string
}

func (f fetcher) Fetch(ctx context.Context, name string, refresh bool) (*deps.Package, error) {
	cr, err := f.client.FetchCrate(ctx, name, refresh)
	if err != nil {
		return nil, err
	}
	if err := f.checkCompatibility(cr, name); err != nil {
		return nil, err
	}
	return crateInfoToDepsPkg(cr), nil
}

func (f fetcher) FetchVersion(ctx context.Context, name, version string, refresh bool) (*deps.Package, error) {
	cr, err := f.client.FetchCrateVersion(ctx, name, version, refresh)
	if err != nil {
		return nil, err
	}
	if err := f.checkCompatibility(cr, name); err != nil {
		return nil, err
	}
	return crateInfoToDepsPkg(cr), nil
}

// FetchVersionFromIndex returns package info using only the sparse index.
// Faster than FetchVersion because it skips the REST API metadata call.
func (f fetcher) FetchVersionFromIndex(ctx context.Context, name, version string, refresh bool) (*deps.Package, error) {
	cr, err := f.client.FetchCrateVersionFromIndex(ctx, name, version, refresh)
	if err != nil {
		return nil, err
	}
	if err := f.checkCompatibility(cr, name); err != nil {
		return nil, err
	}
	return crateInfoToDepsPkg(cr), nil
}

func (f fetcher) checkCompatibility(cr *crates.CrateInfo, name string) error {
	if f.rustVersion == "" || cr.MSRV == "" {
		return nil
	}
	if !constraints.CheckVersionConstraint(f.rustVersion, ">="+cr.MSRV) {
		return &deps.IncompatibleRuntimeError{
			Package:           name,
			Version:           cr.Version,
			RuntimeConstraint: ">=" + cr.MSRV,
			TargetRuntime:     f.rustVersion,
		}
	}
	return nil
}

// ListVersions implements deps.VersionLister for constraint-based resolution.
func (f fetcher) ListVersions(ctx context.Context, name string, refresh bool) ([]string, error) {
	return f.client.ListVersions(ctx, name, refresh)
}

// ListVersionsWithConstraints implements deps.RuntimeConstraintLister.
func (f fetcher) ListVersionsWithConstraints(ctx context.Context, name string, refresh bool) (map[string]string, error) {
	raw, err := f.client.ListVersionsWithConstraints(ctx, name, refresh)
	if err != nil {
		return nil, err
	}
	for version, constraint := range raw {
		raw[version] = constraints.NormalizeRuntimeConstraint(constraint)
	}
	return raw, nil
}

func crateInfoToDepsPkg(cr *crates.CrateInfo) *deps.Package {
	runtimeConstraint := constraints.NormalizeRuntimeConstraint(cr.MSRV)

	pkg := &deps.Package{
		Name:              cr.Name,
		Version:           cr.Version,
		Description:       cr.Description,
		License:           cr.License,
		Downloads:         cr.Downloads,
		Repository:        cr.Repository,
		HomePage:          cr.HomePage,
		ManifestFile:      "Cargo.toml",
		RuntimeConstraint: runtimeConstraint,
	}
	if len(cr.Dependencies) > 0 {
		pkg.Dependencies = make([]deps.Dependency, len(cr.Dependencies))
		for i, d := range cr.Dependencies {
			pkg.Dependencies[i] = deps.Dependency{
				Name:       d.Name,
				Constraint: d.Constraint,
			}
		}
	}
	return pkg
}
