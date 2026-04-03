package deps

import (
	"context"
	"fmt"
	"maps"
	"regexp"
	"sync"

	"github.com/contriboss/pubgrub-go"
	"golang.org/x/sync/singleflight"

	"github.com/matzehuels/stacktower/pkg/core/dag"
	"github.com/matzehuels/stacktower/pkg/core/deps/constraints"
	"github.com/matzehuels/stacktower/pkg/observability"
)

// anyVersionCondition matches any version using "*" wildcard
var anyVersionCondition pubgrub.Condition

func init() {
	// Parse "*" as a version set that matches any version
	vs, _ := pubgrub.ParseVersionRange("*")
	anyVersionCondition = pubgrub.NewVersionSetCondition(vs)
}

// PubGrubResolver implements dependency resolution using the PubGrub algorithm.
// This provides proper SAT-solver-based resolution with conflict detection and
// backtracking, producing accurate dependency graphs.
type PubGrubResolver struct {
	name    string
	fetcher Fetcher
	lister  VersionLister
	parser  ConstraintParser
}

// ConstraintParser converts ecosystem-specific version constraints to PubGrub format.
// Different ecosystems use different constraint syntaxes (PEP 440, semver, etc.),
// so each provides its own parser implementation.
type ConstraintParser interface {
	// ParseConstraint converts an ecosystem-specific constraint string to a
	// PubGrub Condition. Returns nil if the constraint is empty or invalid.
	ParseConstraint(constraint string) pubgrub.Condition

	// ParseVersion converts a version string to a PubGrub Version.
	ParseVersion(version string) pubgrub.Version
}

// VersionHinter is an optional interface that ConstraintParser implementations
// can implement to provide version hints from constraints. This is needed for
// ecosystems like Go where constraints reference exact versions (pseudo-versions)
// that may not appear in the registry's version list.
type VersionHinter interface {
	// HintedVersion extracts an exact version string from a constraint that
	// should be hinted to the version lister. Returns empty string if no
	// version should be hinted. This allows PubGrub to find versions that
	// aren't listed by the registry (e.g., Go pseudo-versions).
	HintedVersion(constraint string) string
}

// NewPubGrubResolver creates a resolver using the PubGrub algorithm.
//
// The fetcher must implement VersionLister for listing available versions.
// The parser converts ecosystem-specific constraints to PubGrub format.
//
// Returns an error if the fetcher doesn't support version listing.
func NewPubGrubResolver(name string, fetcher Fetcher, parser ConstraintParser) (*PubGrubResolver, error) {
	lister, ok := fetcher.(VersionLister)
	if !ok {
		return nil, fmt.Errorf("fetcher does not implement VersionLister")
	}
	return &PubGrubResolver{
		name:    name,
		fetcher: fetcher,
		lister:  lister,
		parser:  parser,
	}, nil
}

// Name returns the resolver identifier.
func (r *PubGrubResolver) Name() string { return r.name }

// ListVersions delegates to the underlying VersionLister, satisfying the
// VersionLister interface so callers can list versions through the resolver.
func (r *PubGrubResolver) ListVersions(ctx context.Context, name string, refresh bool) ([]string, error) {
	return r.lister.ListVersions(ctx, name, refresh)
}

// Fetch delegates to the underlying Fetcher, satisfying the Fetcher interface.
func (r *PubGrubResolver) Fetch(ctx context.Context, name string, refresh bool) (*Package, error) {
	return r.fetcher.Fetch(ctx, name, refresh)
}

// FetchVersion delegates to the underlying Fetcher, satisfying the Fetcher interface.
func (r *PubGrubResolver) FetchVersion(ctx context.Context, name, version string, refresh bool) (*Package, error) {
	return r.fetcher.FetchVersion(ctx, name, version, refresh)
}

// ListVersionsWithConstraints delegates to the underlying fetcher if it implements
// RuntimeConstraintLister. Returns nil, nil if not supported.
func (r *PubGrubResolver) ListVersionsWithConstraints(ctx context.Context, name string, refresh bool) (map[string]string, error) {
	if lister, ok := r.fetcher.(RuntimeConstraintLister); ok {
		return lister.ListVersionsWithConstraints(ctx, name, refresh)
	}
	return nil, nil
}

// ProbeRuntimeConstraint probes runtime requirements for a package.
// It fetches either latest or a specific version and returns normalized runtime metadata.
func (r *PubGrubResolver) ProbeRuntimeConstraint(ctx context.Context, name, version string, refresh bool) (RuntimeConstraintProbe, error) {
	var (
		pkg *Package
		err error
	)
	if version != "" {
		pkg, err = r.fetcher.FetchVersion(ctx, name, version, refresh)
	} else {
		pkg, err = r.fetcher.Fetch(ctx, name, refresh)
	}
	if err != nil || pkg == nil {
		return RuntimeConstraintProbe{}, err
	}

	constraint := constraints.NormalizeRuntimeConstraint(pkg.RuntimeConstraint)
	if constraint == "" {
		return RuntimeConstraintProbe{}, nil
	}

	minVersion := constraints.ExtractMinVersion(constraint)
	return RuntimeConstraintProbe{
		Constraint: constraint,
		MinVersion: minVersion,
	}, nil
}

// Resolve uses PubGrub to resolve the dependency graph.
func (r *PubGrubResolver) Resolve(ctx context.Context, pkg string, opts Options) (*dag.DAG, error) {
	resolvedOpts := opts.WithDefaults()

	source := &pubgrubSource{
		ctx:            ctx,
		fetcher:        r.fetcher,
		lister:         r.lister,
		parser:         r.parser,
		opts:           resolvedOpts,
		rootPkg:        pkg,
		cache:          make(map[string]*Package),
		seen:           make(map[string]bool),
		depth:          make(map[string]int),
		hintedVersions: make(map[string]map[string]bool),
	}
	// Clear source cache after resolution to free memory from accumulated Package structs
	defer source.clearCache()

	source.allowPackage(pkg, 0)

	// Create root requirements
	root := pubgrub.NewRootSource()

	// Add root package with optional version constraint
	var rootCondition pubgrub.Condition
	if opts.Version != "" {
		rootCondition = pubgrub.EqualsCondition{Version: r.parser.ParseVersion(opts.Version)}
	} else if opts.Constraint != "" {
		rootCondition = r.parser.ParseConstraint(opts.Constraint)
		if rootCondition == nil {
			return nil, fmt.Errorf("invalid root constraint: %q", opts.Constraint)
		}
	} else {
		observability.ResolverFromContext(ctx).OnFetchStart(ctx, pkg, 0)
		latestPkg, err := r.fetcher.Fetch(ctx, pkg, resolvedOpts.Refresh)
		depCount := 0
		if latestPkg != nil {
			depCount = len(latestPkg.Dependencies)
		}
		observability.ResolverFromContext(ctx).OnFetchComplete(ctx, pkg, 0, depCount, err)
		if err != nil {
			return nil, fmt.Errorf("fetch root package: %w", err)
		}
		rootCondition = pubgrub.EqualsCondition{Version: r.parser.ParseVersion(latestPkg.Version)}
		source.cache[pkg+"@"+latestPkg.Version] = latestPkg
	}
	root.AddPackage(pubgrub.MakeName(pkg), rootCondition)

	// Create and run solver
	solver := pubgrub.NewSolver(root, source).EnableIncompatibilityTracking()
	solution, err := solver.Solve(root.Term())
	if err != nil {
		if nsErr, ok := err.(*pubgrub.NoSolutionError); ok {
			// Check if this is a diamond dependency conflict (common in npm)
			if conflict := detectDiamondConflict(nsErr.Error(), r.name); conflict != nil {
				return nil, conflict
			}
			return nil, fmt.Errorf("dependency resolution failed:\n%s", nsErr.Error())
		}
		return nil, fmt.Errorf("solve: %w", err)
	}

	// Convert solution to DAG
	return r.solutionToDAG(ctx, solution, pkg, source, resolvedOpts)
}

// enrichResult holds the output of a parallel enrichment call.
type enrichResult struct {
	name string
	meta map[string]any
}

// solutionToDAG converts a PubGrub solution to our DAG format.
func (r *PubGrubResolver) solutionToDAG(
	ctx context.Context,
	solution pubgrub.Solution,
	rootPkg string,
	source *pubgrubSource,
	opts Options,
) (*dag.DAG, error) {
	g := dag.New(nil)

	// Build a map of resolved versions
	resolved := make(map[string]string) // name -> version
	for _, nv := range solution {
		name := nv.Name.Value()
		if name == "$$root" {
			continue
		}
		version := nv.Version.String()
		resolved[name] = version
		_ = g.AddNode(dag.Node{ID: name})
	}

	// Collect packages and add edges (fast, all from cache)
	packages := make(map[string]*Package, len(resolved))
	type packageFetchJob struct {
		name    string
		version string
	}
	type packageFetchResult struct {
		name    string
		version string
		pkg     *Package
		err     error
	}

	if len(resolved) > 0 {
		jobs := make([]packageFetchJob, 0, len(resolved))
		for name, version := range resolved {
			jobs = append(jobs, packageFetchJob{name: name, version: version})
		}
		results := ParallelMapOrdered(ctx, opts.Workers, jobs, func(ctx context.Context, j packageFetchJob) packageFetchResult {
			pkg, err := source.getPackage(j.name, j.version)
			return packageFetchResult{
				name:    j.name,
				version: j.version,
				pkg:     pkg,
				err:     err,
			}
		})

		for _, res := range results {
			if res.err != nil {
				opts.Logger("failed to get package %s@%s for edges: %v", res.name, res.version, res.err)
				continue
			}
			packages[res.name] = res.pkg
		}
	}

	for name, pkg := range packages {
		for _, dep := range pkg.Dependencies {
			if _, exists := resolved[dep.Name]; exists {
				edgeMeta := dag.Metadata{}
				if dep.Constraint != "" {
					edgeMeta["constraint"] = dep.Constraint
				}
				_ = g.AddEdge(dag.Edge{From: name, To: dep.Name, Meta: edgeMeta})
			}
		}
	}

	// Compute depth of each package from the root via BFS
	depths := make(map[string]int, len(resolved))
	depths[rootPkg] = 0
	queue := []string{rootPkg}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, child := range g.Children(cur) {
			if _, seen := depths[child]; !seen {
				depths[child] = depths[cur] + 1
				queue = append(queue, child)
			}
		}
	}

	// Enrich metadata -- prefer batch providers (single GraphQL call) over
	// per-package worker pool (N sequential REST calls).
	if len(opts.MetadataProviders) > 0 {
		// Build PackageRef list and base metadata for every resolved package
		refs := make([]*PackageRef, 0, len(packages))
		for _, pkg := range packages {
			refs = append(refs, pkg.Ref())
		}

		// Try batch enrichment first (e.g., GitHub GraphQL)
		enriched := r.enrichBatch(ctx, refs, opts)
		if enriched == nil {
			// No batch provider available -- fall back to per-package worker pool
			enriched = r.enrichParallel(ctx, packages, depths, opts)
		}

		// Apply enrichment results to DAG nodes, merging with base metadata
		for name, pkg := range packages {
			if n, ok := g.Node(name); ok {
				meta := pkg.Metadata()
				if extra, ok := enriched[name]; ok {
					maps.Copy(meta, extra)
				}
				n.Meta = meta
			}
		}
	} else {
		for name, pkg := range packages {
			if n, ok := g.Node(name); ok {
				n.Meta = pkg.Metadata()
			}
		}
	}

	observability.ResolverFromContext(ctx).OnProgress(ctx, len(resolved), 0, opts.MaxNodes)
	return pruneResolvedGraph(g, rootPkg, opts.MaxDepth, opts.MaxNodes), nil
}

func pruneResolvedGraph(g *dag.DAG, rootPkg string, maxDepth, maxNodes int) *dag.DAG {
	if g == nil {
		return nil
	}
	if maxDepth <= 0 && maxNodes <= 0 {
		return g
	}
	if _, ok := g.Node(rootPkg); !ok {
		return g
	}

	keep := make(map[string]bool, g.NodeCount())
	depth := map[string]int{rootPkg: 0}
	queue := []string{rootPkg}
	enqueued := map[string]bool{rootPkg: true}
	seenCount := 0

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		enqueued[cur] = false
		if keep[cur] {
			continue
		}
		d := depth[cur]
		if maxDepth > 0 && d > maxDepth {
			continue
		}
		if maxNodes > 0 && seenCount >= maxNodes {
			break
		}

		keep[cur] = true
		seenCount++

		if maxDepth > 0 && d >= maxDepth {
			continue
		}
		for _, child := range g.Children(cur) {
			if _, seen := depth[child]; !seen {
				depth[child] = d + 1
			}
			if !keep[child] && !enqueued[child] {
				queue = append(queue, child)
				enqueued[child] = true
			}
		}
	}

	if len(keep) == g.NodeCount() {
		return g
	}

	pruned := dag.New(nil)
	for _, n := range g.Nodes() {
		if !keep[n.ID] {
			continue
		}
		meta := maps.Clone(n.Meta)
		if meta == nil {
			meta = dag.Metadata{}
		}
		_ = pruned.AddNode(dag.Node{
			ID:       n.ID,
			Row:      n.Row,
			Meta:     meta,
			Kind:     n.Kind,
			MasterID: n.MasterID,
		})
	}
	for _, e := range g.Edges() {
		if !keep[e.From] || !keep[e.To] {
			continue
		}
		_ = pruned.AddEdge(dag.Edge{From: e.From, To: e.To, Meta: maps.Clone(e.Meta)})
	}
	return pruned
}

// enrichBatch tries to use BatchMetadataProvider for all providers.
// Returns combined enrichment map, or nil if no batch provider was found.
func (r *PubGrubResolver) enrichBatch(
	ctx context.Context,
	refs []*PackageRef,
	opts Options,
) map[string]map[string]any {
	combined := make(map[string]map[string]any)
	foundBatch := false

	for _, p := range opts.MetadataProviders {
		bp, ok := p.(BatchMetadataProvider)
		if !ok {
			continue
		}
		foundBatch = true

		// Fire progress hooks for observability
		for _, ref := range refs {
			observability.ResolverFromContext(ctx).OnFetchStart(ctx, ref.Name, 0)
		}

		batch, err := bp.EnrichBatch(ctx, refs, opts.Refresh)
		if err != nil {
			opts.Logger("batch enrich (%s): %v", p.Name(), err)
			// Fall through -- will return nil so caller uses per-package fallback
			return nil
		}

		for _, ref := range refs {
			observability.ResolverFromContext(ctx).OnFetchComplete(ctx, ref.Name, 0, 0, nil)
		}

		for name, meta := range batch {
			if combined[name] == nil {
				combined[name] = make(map[string]any)
			}
			maps.Copy(combined[name], meta)
		}
	}

	if !foundBatch {
		return nil
	}
	return combined
}

// enrichParallel is the fallback: per-package worker pool for non-batch providers.
func (r *PubGrubResolver) enrichParallel(
	ctx context.Context,
	packages map[string]*Package,
	depths map[string]int,
	opts Options,
) map[string]map[string]any {
	type enrichJob struct {
		pkg   *Package
		depth int
	}

	workers := min(DefaultWorkers, len(packages))
	jobs := make([]enrichJob, 0, len(packages))
	for _, pkg := range packages {
		jobs = append(jobs, enrichJob{pkg: pkg, depth: depths[pkg.Name]})
	}
	results := ParallelMapOrdered(ctx, workers, jobs, func(ctx context.Context, j enrichJob) enrichResult {
		observability.ResolverFromContext(ctx).OnFetchStart(ctx, j.pkg.Name, j.depth)
		meta := r.enrich(ctx, j.pkg, opts)
		observability.ResolverFromContext(ctx).OnFetchComplete(ctx, j.pkg.Name, j.depth, 0, nil)
		return enrichResult{name: j.pkg.Name, meta: meta}
	})

	combined := make(map[string]map[string]any, len(packages))
	for _, er := range results {
		combined[er.name] = er.meta
	}
	return combined
}

// diamondConflictRE matches package version constraints in PubGrub error messages.
// Pattern: "Because package X.Y.Z depends on other ==A.B.C"
var diamondConflictRE = regexp.MustCompile(`Because\s+(\S+)\s+([\d.]+)\s+depends on\s+(\S+)\s*==\s*([\d.]+)`)

// detectDiamondConflict analyzes a PubGrub error message to detect diamond dependency
// conflicts where the same package is required at different versions. This is common
// in npm where nested node_modules allows multiple versions, but PubGrub requires one.
func detectDiamondConflict(errMsg, registry string) *DiamondDependencyError {
	// Only provide enhanced error for npm (JavaScript) where this is a known limitation
	if registry != "npm" {
		return nil
	}

	// Find all "Because X Y.Z depends on P ==V" patterns
	// Capture groups: 1=dependent, 2=dependent_version, 3=dependency, 4=dep_version
	matches := diamondConflictRE.FindAllStringSubmatch(errMsg, -1)
	if len(matches) < 2 {
		return nil
	}

	// Look for the same package required at different versions
	pkgVersions := make(map[string]map[string][]string) // pkg -> version -> dependents
	for _, m := range matches {
		dependent, depPkg, depVersion := m[1], m[3], m[4]
		if pkgVersions[depPkg] == nil {
			pkgVersions[depPkg] = make(map[string][]string)
		}
		pkgVersions[depPkg][depVersion] = append(pkgVersions[depPkg][depVersion], dependent)
	}

	// Find packages with multiple different version requirements
	for pkg, versions := range pkgVersions {
		if len(versions) > 1 {
			var dependents []string
			for _, deps := range versions {
				dependents = append(dependents, deps...)
			}
			return &DiamondDependencyError{
				Package:     pkg,
				Dependents:  dependents,
				Language:    "javascript",
				OriginalErr: errMsg,
			}
		}
	}

	return nil
}

// pubgrubSource implements pubgrub.Source by wrapping our Fetcher.
// It caches already-fetched package data to avoid duplicate HTTP calls during solving.
type pubgrubSource struct {
	ctx     context.Context
	fetcher Fetcher
	lister  VersionLister
	parser  ConstraintParser
	opts    Options
	rootPkg string

	mu    sync.Mutex
	cache map[string]*Package // key: "name@version"
	seen  map[string]bool     // admitted packages participating in solving
	depth map[string]int      // best-known depth by package name

	// hintedVersions stores exact version strings discovered through dependency
	// constraints (e.g., Go pseudo-versions pinned in go.mod). GetVersions
	// includes these so PubGrub can find them even if the registry doesn't list them.
	hintedVersions map[string]map[string]bool // package name -> set of version strings

	// fetchGroup deduplicates concurrent fetches for the same package@version.
	fetchGroup singleflight.Group
}

// GetVersions returns all available versions for a package.
func (s *pubgrubSource) GetVersions(name pubgrub.Name) ([]pubgrub.Version, error) {
	// Check for context cancellation to respect job timeouts
	if s.ctx.Err() != nil {
		return nil, s.ctx.Err()
	}
	observability.ResolverFromContext(s.ctx).OnFetchStart(s.ctx, name.Value(), 0)
	versions, err := s.lister.ListVersions(s.ctx, name.Value(), s.opts.Refresh)
	observability.ResolverFromContext(s.ctx).OnFetchComplete(s.ctx, name.Value(), 0, 0, err)
	if err != nil {
		return nil, err
	}

	// If the fetcher can provide per-version runtime constraints, filter out
	// versions that cannot run on the selected runtime before PubGrub explores them.
	// This prevents hard failures when a latest transitive version is incompatible.
	var runtimeConstraints map[string]string
	if s.opts.RuntimeVersion != "" {
		if rcLister, ok := s.fetcher.(RuntimeConstraintLister); ok {
			if constraints, rcErr := rcLister.ListVersionsWithConstraints(s.ctx, name.Value(), s.opts.Refresh); rcErr == nil {
				runtimeConstraints = constraints
			}
		}
	}

	seen := make(map[string]bool, len(versions))
	result := make([]pubgrub.Version, 0, len(versions))
	for _, v := range versions {
		// Filter prerelease versions if not included
		if !s.opts.IncludePrerelease && IsPrereleaseVersion(v) {
			continue
		}
		// Filter runtime-incompatible versions when constraint data is available.
		if s.opts.RuntimeVersion != "" && runtimeConstraints != nil {
			if constraint, ok := runtimeConstraints[v]; ok && constraint != "" &&
				!constraints.CheckVersionConstraint(s.opts.RuntimeVersion, constraint) {
				continue
			}
		}
		pv := s.parser.ParseVersion(v)
		if pv != nil {
			result = append(result, pv)
			seen[pv.String()] = true
		}
	}

	// When the registry returns no tagged versions (e.g., gopkg.in/* modules),
	// fall back to fetching @latest so PubGrub has at least one candidate.
	if len(result) == 0 {
		pkg, err := s.fetcher.Fetch(s.ctx, name.Value(), s.opts.Refresh)
		if err != nil {
			return nil, err
		}
		if pkg.Version != "" && (s.opts.IncludePrerelease || !IsPrereleaseVersion(pkg.Version)) {
			pv := s.parser.ParseVersion(pkg.Version)
			if pv != nil {
				result = append(result, pv)
				seen[pv.String()] = true
			}
		}
	}

	// Include hinted versions (exact pins from dependency constraints, e.g.,
	// Go pseudo-versions) that the registry didn't list.
	s.mu.Lock()
	hints := s.hintedVersions[name.Value()]
	s.mu.Unlock()
	for vStr := range hints {
		if !seen[vStr] && (s.opts.IncludePrerelease || !IsPrereleaseVersion(vStr)) {
			pv := s.parser.ParseVersion(vStr)
			if pv != nil {
				result = append(result, pv)
				seen[pv.String()] = true
			}
		}
	}

	return result, nil
}

// GetDependencies returns dependencies for a specific package version.
func (s *pubgrubSource) GetDependencies(name pubgrub.Name, version pubgrub.Version) ([]pubgrub.Term, error) {
	// Check for context cancellation to respect job timeouts
	if s.ctx.Err() != nil {
		return nil, s.ctx.Err()
	}
	nameStr := name.Value()
	currDepth, ok := s.packageDepth(nameStr)
	if !ok {
		// If this package reached the solver, treat it as admitted with unknown depth.
		// This keeps behavior safe for unusual solve orders while still enforcing
		// depth budget for newly discovered transitive dependencies.
		currDepth = 0
		s.allowPackage(nameStr, currDepth)
	}
	if s.opts.MaxDepth > 0 && currDepth >= s.opts.MaxDepth {
		return nil, nil
	}

	pkg, err := s.getPackage(name.Value(), version.String())
	if err != nil {
		s.opts.Logger("fetch %s@%s: %v", name.Value(), version.String(), err)
		return nil, err
	}

	terms := make([]pubgrub.Term, 0, len(pkg.Dependencies))
	for _, dep := range pkg.Dependencies {
		childDepth := currDepth + 1
		if !s.allowPackage(dep.Name, childDepth) {
			continue
		}

		var cond pubgrub.Condition
		if dep.Constraint != "" {
			cond = s.parser.ParseConstraint(dep.Constraint)
		}
		if cond == nil {
			cond = anyVersionCondition
		}
		if eq, ok := cond.(pubgrub.EqualsCondition); ok {
			s.hintVersion(dep.Name, eq.Version.String())
		}
		// Allow language-specific parsers to hint versions from constraints.
		// This is needed for ecosystems like Go where constraints reference
		// pseudo-versions that don't appear in the registry's version list.
		if hinter, ok := s.parser.(VersionHinter); ok {
			if v := hinter.HintedVersion(dep.Constraint); v != "" {
				s.hintVersion(dep.Name, v)
			}
		}
		terms = append(terms, pubgrub.NewTerm(pubgrub.MakeName(dep.Name), cond))
	}
	return terms, nil
}

func (s *pubgrubSource) packageDepth(name string) (int, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.depth[name]
	return d, ok
}

func (s *pubgrubSource) allowPackage(name string, depth int) bool {
	if name == "" {
		return false
	}
	if s.opts.MaxDepth > 0 && depth > s.opts.MaxDepth {
		return false
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if prevDepth, ok := s.depth[name]; ok {
		if depth < prevDepth {
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

// hintVersion registers a version string for a package so GetVersions includes it.
func (s *pubgrubSource) hintVersion(pkg, version string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.hintedVersions[pkg] == nil {
		s.hintedVersions[pkg] = make(map[string]bool)
	}
	s.hintedVersions[pkg][version] = true
}

// clearCache releases memory held by the package cache after resolution completes.
// This prevents accumulation of Package structs for long-lived resolver instances.
func (s *pubgrubSource) clearCache() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cache = nil
	s.seen = nil
	s.depth = nil
	s.hintedVersions = nil
}

// getPackage fetches and caches a package by name and version.
// Concurrent requests for the same key are deduplicated via singleflight.
func (s *pubgrubSource) getPackage(name, version string) (*Package, error) {
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

		observability.ResolverFromContext(s.ctx).OnFetchStart(s.ctx, name, 0)
		pkg, err := s.fetcher.FetchVersion(s.ctx, name, version, s.opts.Refresh)
		depCount := 0
		if pkg != nil {
			depCount = len(pkg.Dependencies)
		}
		observability.ResolverFromContext(s.ctx).OnFetchComplete(s.ctx, name, 0, depCount, err)
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
	return result.(*Package), nil
}

// enrich combines package metadata with external provider data (e.g., GitHub maintainers).
func (r *PubGrubResolver) enrich(ctx context.Context, pkg *Package, opts Options) map[string]any {
	m := pkg.Metadata()
	ref := pkg.Ref()
	for _, p := range opts.MetadataProviders {
		if enriched, err := p.Enrich(ctx, ref, opts.Refresh); err == nil {
			maps.Copy(m, enriched)
		} else {
			opts.Logger("enrich failed: %s: %v", pkg.Name, err)
		}
	}
	return m
}
