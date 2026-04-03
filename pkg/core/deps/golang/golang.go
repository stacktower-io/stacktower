package golang

import (
	"context"
	"net/url"
	"strconv"
	"strings"

	"github.com/matzehuels/stacktower/pkg/cache"
	"github.com/matzehuels/stacktower/pkg/core/dag"
	"github.com/matzehuels/stacktower/pkg/core/deps"
	"github.com/matzehuels/stacktower/pkg/integrations"
	"github.com/matzehuels/stacktower/pkg/integrations/goproxy"
)

// Ensure fetcher implements VersionLister (required by PubGrub resolver).
var _ deps.VersionLister = fetcher{}

// Language provides Go dependency resolution via the Go module proxy.
// Supports go.mod manifest files.
var Language = &deps.Language{
	Name:                  "go",
	DefaultRegistry:       "goproxy",
	DefaultRuntimeVersion: "1.21",
	RegistryAliases:       map[string]string{"proxy": "goproxy", "go": "goproxy"},
	ManifestTypes:         []string{"gomod"},
	ManifestAliases:       map[string]string{"go.mod": "gomod"},
	NewResolver:           newResolver,
	NewManifest:           newManifest,
	ManifestParsers:       manifestParsers,
}

func newResolver(backend cache.Cache, opts deps.Options) (deps.Resolver, error) {
	c := goproxy.NewClient(backend, opts.CacheTTL)
	f := fetcher{client: c, goVersion: opts.RuntimeVersion}
	r, err := deps.NewPubGrubResolver("goproxy", f, GoModMatcher{})
	if err != nil {
		return nil, err
	}
	return &goResolver{PubGrubResolver: r, provider: f, client: c}, nil
}

// goResolver wraps PubGrubResolver and injects the goproxy fetcher as a
// MetadataProvider so that license information is fetched post-resolution
// (enrichment phase) rather than during the PubGrub solving phase.
//
// For Go 1.17+ modules, the resolver uses the module's go.mod indirect deps
// directly (lockfile-style resolution) instead of recursive PubGrub resolution.
// This respects Go's module graph pruning and produces results consistent with
// what `go mod tidy` would generate.
type goResolver struct {
	*deps.PubGrubResolver
	provider fetcher
	client   *goproxy.Client
}

func (r *goResolver) Resolve(ctx context.Context, pkg string, opts deps.Options) (*dag.DAG, error) {
	opts.MetadataProviders = append([]deps.MetadataProvider{r.provider}, opts.MetadataProviders...)

	// First, fetch the root module to check its go version
	version := ""
	if idx := strings.Index(pkg, "@"); idx != -1 {
		version = pkg[idx+1:]
		pkg = pkg[:idx]
	}

	var rootModule *goproxy.ModuleInfo
	var err error
	if version != "" {
		rootModule, err = r.client.FetchModuleVersion(ctx, pkg, version, opts.Refresh)
	} else {
		rootModule, err = r.client.FetchModule(ctx, pkg, opts.Refresh)
	}
	if err != nil {
		return nil, err
	}

	// For Go 1.17+ modules, use lockfile-style resolution.
	// The go.mod's indirect deps already represent the pruned module graph.
	if isGo117OrLater(rootModule.GoVersion) {
		return r.resolveLockfileStyle(ctx, rootModule, opts)
	}

	// For older modules, fall back to recursive PubGrub resolution
	pkgWithVersion := pkg
	if version != "" {
		pkgWithVersion = pkg + "@" + version
	}
	return r.PubGrubResolver.Resolve(ctx, pkgWithVersion, opts)
}

// resolveLockfileStyle builds a dependency graph directly from the module's
// go.mod file, using both direct and indirect dependencies. This is used for
// Go 1.17+ modules where the indirect deps represent the pruned module graph.
func (r *goResolver) resolveLockfileStyle(ctx context.Context, root *goproxy.ModuleInfo, opts deps.Options) (*dag.DAG, error) {
	g := dag.New(nil)

	// Add root node
	rootMeta := dag.Metadata{"version": root.Version}
	if root.GoVersion != "" {
		rootMeta["go_version"] = root.GoVersion
	}
	_ = g.AddNode(dag.Node{ID: root.Path, Meta: rootMeta})

	// Build lookup of all dependencies
	allDeps := make(map[string]goproxy.Dependency, len(root.Dependencies)+len(root.IndirectDependencies))
	directSet := make(map[string]bool, len(root.Dependencies))

	for _, dep := range root.Dependencies {
		allDeps[dep.Name] = dep
		directSet[dep.Name] = true
	}
	for _, dep := range root.IndirectDependencies {
		allDeps[dep.Name] = dep
	}

	// Add all dependency nodes and build package refs for enrichment
	refs := make([]*deps.PackageRef, 0, len(allDeps)+1)
	refs = append(refs, makeGoPackageRef(root.Path, root.Version))

	for name, dep := range allDeps {
		version := strings.TrimPrefix(dep.Constraint, "=")
		meta := dag.Metadata{}
		if version != "" {
			meta["version"] = version
		}
		if !directSet[name] {
			meta["indirect"] = true
		}
		_ = g.AddNode(dag.Node{ID: name, Meta: meta})

		refs = append(refs, makeGoPackageRef(name, version))
	}

	// Connect direct deps to root
	for _, dep := range root.Dependencies {
		edgeMeta := dag.Metadata{}
		if dep.Constraint != "" {
			edgeMeta["constraint"] = dep.Constraint
		}
		_ = g.AddEdge(dag.Edge{From: root.Path, To: dep.Name, Meta: edgeMeta})
	}

	// Fetch dependency edges in parallel (like buildGoModGraphWithEdges in gomod.go)
	type fetchResult struct {
		name         string
		dependencies []goproxy.Dependency
	}

	// Convert to slice for parallel processing
	depSlice := make([]goproxy.Dependency, 0, len(allDeps))
	for _, dep := range allDeps {
		depSlice = append(depSlice, dep)
	}

	results := deps.ParallelMapOrdered(ctx, opts.Workers, depSlice, func(ctx context.Context, dep goproxy.Dependency) fetchResult {
		if ctx.Err() != nil {
			return fetchResult{name: dep.Name}
		}

		version := strings.TrimPrefix(dep.Constraint, "=")
		if version == "" {
			return fetchResult{name: dep.Name}
		}

		// Fetch the package to get its dependencies
		pkg, err := r.client.FetchModuleVersion(ctx, dep.Name, version, opts.Refresh)
		if err != nil {
			return fetchResult{name: dep.Name}
		}
		return fetchResult{name: dep.Name, dependencies: pkg.Dependencies}
	})

	// Add edges based on fetched dependencies
	for _, res := range results {
		for _, childDep := range res.dependencies {
			// Only add edge if the target exists in our known set
			if _, exists := allDeps[childDep.Name]; exists {
				edgeMeta := dag.Metadata{}
				if childDep.Constraint != "" {
					edgeMeta["constraint"] = childDep.Constraint
				}
				_ = g.AddEdge(dag.Edge{From: res.name, To: childDep.Name, Meta: edgeMeta})
			}
		}
	}

	// Enrich metadata (licenses, etc.) from metadata providers
	if len(opts.MetadataProviders) > 0 {
		enriched := r.enrichPackages(ctx, refs, opts)
		for name, meta := range enriched {
			if n, ok := g.Node(name); ok {
				for k, v := range meta {
					n.Meta[k] = v
				}
			}
		}
	}

	return g, nil
}

// enrichPackages calls metadata providers for each package to fetch licenses and other metadata.
func (r *goResolver) enrichPackages(ctx context.Context, refs []*deps.PackageRef, opts deps.Options) map[string]map[string]any {
	// Try batch enrichment first (e.g., GitHub GraphQL)
	for _, p := range opts.MetadataProviders {
		if bp, ok := p.(deps.BatchMetadataProvider); ok {
			batch, err := bp.EnrichBatch(ctx, refs, opts.Refresh)
			if err == nil && batch != nil {
				return batch
			}
		}
	}

	// Fall back to per-package enrichment
	type enrichResult struct {
		name string
		meta map[string]any
	}

	workers := min(deps.DefaultWorkers, len(refs))
	results := deps.ParallelMapOrdered(ctx, workers, refs, func(ctx context.Context, ref *deps.PackageRef) enrichResult {
		meta := make(map[string]any)
		for _, p := range opts.MetadataProviders {
			if m, err := p.Enrich(ctx, ref, opts.Refresh); err == nil {
				for k, v := range m {
					meta[k] = v
				}
			}
		}
		return enrichResult{name: ref.Name, meta: meta}
	})

	enriched := make(map[string]map[string]any, len(refs))
	for _, r := range results {
		enriched[r.name] = r.meta
	}
	return enriched
}

// isGo117OrLater returns true if the given go version string represents
// Go 1.17 or later, which introduced module graph pruning.
func isGo117OrLater(goVersion string) bool {
	if goVersion == "" {
		return false
	}

	// Parse version like "1.21", "1.17", "1.21.0", etc.
	parts := strings.Split(goVersion, ".")
	if len(parts) < 2 {
		return false
	}

	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return false
	}

	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return false
	}

	// Go 1.17+ has module graph pruning
	return major > 1 || (major == 1 && minor >= 17)
}

// ListVersions satisfies the deps.VersionLister interface so that the CLI's
// list command can still type-assert the resolver to VersionLister.
func (r *goResolver) ListVersions(ctx context.Context, name string, refresh bool) ([]string, error) {
	return r.PubGrubResolver.ListVersions(ctx, name, refresh)
}

type fetcher struct {
	client    *goproxy.Client
	goVersion string
}

func (f fetcher) Fetch(ctx context.Context, name string, refresh bool) (*deps.Package, error) {
	m, err := f.client.FetchModule(ctx, name, refresh)
	if err != nil {
		return nil, err
	}
	return goproxyModuleToDepsPkg(m), nil
}

func (f fetcher) FetchVersion(ctx context.Context, name, version string, refresh bool) (*deps.Package, error) {
	m, err := f.client.FetchModuleVersion(ctx, name, version, refresh)
	if err != nil {
		return nil, err
	}
	return goproxyModuleToDepsPkg(m), nil
}

// ListVersions implements deps.VersionLister for constraint-based resolution.
func (f fetcher) ListVersions(ctx context.Context, name string, refresh bool) ([]string, error) {
	return f.client.ListVersions(ctx, name, refresh)
}

// Name implements deps.MetadataProvider.
func (f fetcher) Name() string { return "goproxy" }

// Enrich implements deps.MetadataProvider, fetching license information for
// the post-resolution enrichment phase. This is the only place where the
// expensive pkg.go.dev scrape runs — never during PubGrub solving.
//
// Note: Description is intentionally not fetched from pkg.go.dev. For most
// packages, GitHub's repo_description (from the GitHub enricher) is sufficient
// and more reliable than scraping pkg.go.dev HTML.
func (f fetcher) Enrich(ctx context.Context, ref *deps.PackageRef, refresh bool) (map[string]any, error) {
	meta := make(map[string]any)

	if license := f.client.FetchLicense(ctx, ref.Name, refresh); license != "" {
		meta["license"] = license
	}

	return meta, nil
}

func goproxyModuleToDepsPkg(m *goproxy.ModuleInfo) *deps.Package {
	pkg := &deps.Package{
		Name:         m.Path,
		Version:      m.Version,
		ManifestFile: "go.mod",
		// License is not set here; it is populated during the post-resolution
		// enrichment phase via fetcher.Enrich → goproxy.Client.FetchLicense.
	}
	// Convert goproxy.Dependency to deps.Dependency with constraints
	if len(m.Dependencies) > 0 {
		pkg.Dependencies = make([]deps.Dependency, len(m.Dependencies))
		for i, d := range m.Dependencies {
			pkg.Dependencies[i] = deps.Dependency{
				Name:       d.Name,
				Constraint: d.Constraint,
			}
		}
	}

	// Prefer discovered repository, but normalize googlesource URLs to their
	// GitHub mirrors for downstream metadata enrichment.
	pkg.Repository = normalizeRepositoryURL(m.Path, m.Repository)

	return pkg
}

func newManifest(name string, res deps.Resolver) deps.ManifestParser {
	switch name {
	case "gomod":
		return &GoModParser{resolver: res}
	default:
		return nil
	}
}

func manifestParsers(res deps.Resolver) []deps.ManifestParser {
	return []deps.ManifestParser{
		&GoModParser{resolver: res},
	}
}

// inferRepoURL extracts the repository URL from a Go module path.
// For github.com, gitlab.com, and bitbucket.org modules, it converts
// the module path to an HTTPS URL by taking the first two path segments.
//
// Examples:
//   - github.com/spf13/cobra → https://github.com/spf13/cobra
//   - github.com/gofiber/fiber/v2 → https://github.com/gofiber/fiber
//   - gitlab.com/user/repo → https://gitlab.com/user/repo
//   - gopkg.in/yaml.v3 → (returns empty string)
//
// Returns an empty string for non-repository-based modules or modules
// from unsupported hosting platforms.
func inferRepoURL(modulePath string) string {
	// Common hosting platforms that use path-based module names
	for _, prefix := range []string{"github.com/", "gitlab.com/", "bitbucket.org/"} {
		if strings.HasPrefix(modulePath, prefix) {
			// Extract owner/repo (first two path segments after the domain)
			// e.g., "github.com/spf13/cobra/doc" → owner="spf13", repo="cobra"
			parts := strings.Split(strings.TrimPrefix(modulePath, prefix), "/")
			if len(parts) >= 2 {
				return "https://" + prefix + parts[0] + "/" + parts[1]
			}
		}
	}
	return ""
}

func normalizeRepositoryURL(modulePath, discoveredRepo string) string {
	// Keep known repo hosts as-is.
	if discoveredRepo != "" {
		if mirrored := mirrorGoogleSourceToGitHub(discoveredRepo); mirrored != "" {
			return mirrored
		}
		return discoveredRepo
	}

	// Vanity golang.org/x modules mirror to github.com/golang/<repo>.
	if strings.HasPrefix(modulePath, "golang.org/x/") {
		rest := strings.TrimPrefix(modulePath, "golang.org/x/")
		if rest != "" {
			parts := strings.Split(rest, "/")
			return "https://github.com/golang/" + parts[0]
		}
	}
	return inferRepoURL(modulePath)
}

func mirrorGoogleSourceToGitHub(raw string) string {
	u, err := url.Parse(integrations.NormalizeRepoURL(raw))
	if err != nil || u.Host == "" || !strings.HasSuffix(strings.ToLower(u.Host), "googlesource.com") {
		return ""
	}
	path := strings.Trim(strings.TrimSpace(u.Path), "/")
	if path == "" {
		return ""
	}
	if idx := strings.Index(path, "/+"); idx > 0 {
		path = path[:idx]
	}
	parts := strings.Split(path, "/")
	if u.Host == "go.googlesource.com" {
		return "https://github.com/golang/" + strings.TrimSuffix(parts[0], ".git")
	}
	if len(parts) >= 2 {
		owner := strings.TrimSuffix(parts[0], ".git")
		repo := strings.TrimSuffix(parts[len(parts)-1], ".git")
		return "https://github.com/" + owner + "/" + repo
	}
	return ""
}

// makeGoPackageRef creates a PackageRef with the repository URL inferred from
// the Go module path. This enables GitHub enrichment for github.com/* modules.
func makeGoPackageRef(modulePath, version string) *deps.PackageRef {
	ref := &deps.PackageRef{
		Name:    modulePath,
		Version: version,
	}

	// Infer GitHub repository URL from the module path.
	// This enables the GitHub metadata enricher to fetch stars, description, etc.
	if repoURL := inferRepoURL(modulePath); repoURL != "" {
		ref.ProjectURLs = map[string]string{"repository": repoURL}
	}

	return ref
}
