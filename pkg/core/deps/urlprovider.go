package deps

import (
	"context"
	"strings"
	"time"

	"github.com/matzehuels/stacktower/pkg/cache"
	"github.com/matzehuels/stacktower/pkg/integrations/crates"
	"github.com/matzehuels/stacktower/pkg/integrations/goproxy"
	"github.com/matzehuels/stacktower/pkg/integrations/maven"
	"github.com/matzehuels/stacktower/pkg/integrations/npm"
	"github.com/matzehuels/stacktower/pkg/integrations/packagist"
	"github.com/matzehuels/stacktower/pkg/integrations/pypi"
	"github.com/matzehuels/stacktower/pkg/integrations/rubygems"
	"github.com/matzehuels/stacktower/pkg/observability"
)

// defaultChunkSize is the number of packages to process per chunk.
// Smaller chunks give better progress feedback; larger chunks have less overhead.
const defaultChunkSize = 50

// urlFetchResult holds the result of a single URL fetch operation.
type urlFetchResult struct {
	name string
	urls PackageURLs
	ok   bool
}

// fetchURLsChunked fetches URLs in chunks for better progress feedback.
// It processes packages in groups of chunkSize, completing each chunk before starting the next.
// This ensures the in-flight count regularly drops to zero between chunks, giving
// users clear feedback that progress is being made.
func fetchURLsChunked(
	ctx context.Context,
	names []string,
	workers int,
	chunkSize int,
	fetchFn func(ctx context.Context, name string) urlFetchResult,
) map[string]PackageURLs {
	if len(names) == 0 {
		return nil
	}

	urlMap := make(map[string]PackageURLs, len(names))
	hooks := observability.ResolverFromContext(ctx)

	for i := 0; i < len(names); i += chunkSize {
		end := i + chunkSize
		if end > len(names) {
			end = len(names)
		}
		chunk := names[i:end]

		results := ParallelMapOrdered(ctx, workers, chunk, func(ctx context.Context, name string) urlFetchResult {
			hooks.OnFetchStart(ctx, name, 0)
			result := fetchFn(ctx, name)
			hooks.OnFetchComplete(ctx, name, 0, 0, nil)
			return result
		})

		for _, r := range results {
			if r.ok {
				urlMap[r.name] = r.urls
			}
		}

		if ctx.Err() != nil {
			break
		}
	}

	return urlMap
}

// PyPIURLProvider fetches package URLs from PyPI in parallel.
// The underlying pypi.Client handles rate limiting (50 req/s, burst 30).
type PyPIURLProvider struct {
	client  *pypi.Client
	workers int
}

// NewPyPIURLProvider creates a new PyPI URL provider.
func NewPyPIURLProvider(c cache.Cache, cacheTTL time.Duration) *PyPIURLProvider {
	return &PyPIURLProvider{
		client:  pypi.NewClient(c, cacheTTL, ""),
		workers: DefaultWorkers,
	}
}

// FetchURLs fetches repository URLs for the given package names from PyPI.
// Packages are processed in chunks of 50 for better progress feedback.
func (p *PyPIURLProvider) FetchURLs(ctx context.Context, names []string, refresh bool) (map[string]PackageURLs, error) {
	return fetchURLsChunked(ctx, names, p.workers, defaultChunkSize, func(ctx context.Context, name string) urlFetchResult {
		pkg, err := p.client.FetchPackage(ctx, name, refresh)
		if err != nil {
			return urlFetchResult{name: name}
		}
		return urlFetchResult{
			name: name,
			urls: PackageURLs{
				ProjectURLs: pkg.ProjectURLs,
				HomePage:    pkg.HomePage,
			},
			ok: true,
		}
	}), nil
}

// NpmURLProvider fetches package URLs from npm in parallel.
// The underlying npm.Client handles rate limiting (50 req/s, burst 30).
type NpmURLProvider struct {
	client  *npm.Client
	workers int
}

// NewNpmURLProvider creates a new npm URL provider.
func NewNpmURLProvider(c cache.Cache, cacheTTL time.Duration) *NpmURLProvider {
	return &NpmURLProvider{
		client:  npm.NewClient(c, cacheTTL),
		workers: DefaultWorkers,
	}
}

// FetchURLs fetches repository URLs for the given package names from npm.
// Packages are processed in chunks of 50 for better progress feedback.
func (p *NpmURLProvider) FetchURLs(ctx context.Context, names []string, refresh bool) (map[string]PackageURLs, error) {
	return fetchURLsChunked(ctx, names, p.workers, defaultChunkSize, func(ctx context.Context, name string) urlFetchResult {
		pkg, err := p.client.FetchPackage(ctx, name, refresh)
		if err != nil {
			return urlFetchResult{name: name}
		}
		projectURLs := make(map[string]string)
		if pkg.Repository != "" {
			projectURLs["repository"] = pkg.Repository
		}
		if pkg.HomePage != "" {
			projectURLs["homepage"] = pkg.HomePage
		}
		return urlFetchResult{
			name: name,
			urls: PackageURLs{
				ProjectURLs: projectURLs,
				HomePage:    pkg.HomePage,
			},
			ok: true,
		}
	}), nil
}

// CratesURLProvider fetches package URLs from crates.io in parallel.
// The underlying crates.Client handles rate limiting (5 req/s, burst 10).
type CratesURLProvider struct {
	client  *crates.Client
	workers int
}

// NewCratesURLProvider creates a new crates.io URL provider.
func NewCratesURLProvider(c cache.Cache, cacheTTL time.Duration) *CratesURLProvider {
	return &CratesURLProvider{
		client:  crates.NewClient(c, cacheTTL),
		workers: DefaultWorkers,
	}
}

// FetchURLs fetches repository URLs for the given crate names from crates.io.
// Packages are processed in chunks of 50 for better progress feedback.
func (p *CratesURLProvider) FetchURLs(ctx context.Context, names []string, refresh bool) (map[string]PackageURLs, error) {
	return fetchURLsChunked(ctx, names, p.workers, defaultChunkSize, func(ctx context.Context, name string) urlFetchResult {
		crate, err := p.client.FetchCrate(ctx, name, refresh)
		if err != nil {
			return urlFetchResult{name: name}
		}
		projectURLs := make(map[string]string)
		if crate.Repository != "" {
			projectURLs["repository"] = crate.Repository
		}
		if crate.HomePage != "" {
			projectURLs["homepage"] = crate.HomePage
		}
		return urlFetchResult{
			name: name,
			urls: PackageURLs{
				ProjectURLs: projectURLs,
				HomePage:    crate.HomePage,
			},
			ok: true,
		}
	}), nil
}

// RubyGemsURLProvider fetches package URLs from RubyGems in parallel.
// The underlying rubygems.Client handles rate limiting (30 req/s, burst 20).
type RubyGemsURLProvider struct {
	client  *rubygems.Client
	workers int
}

// NewRubyGemsURLProvider creates a new RubyGems URL provider.
func NewRubyGemsURLProvider(c cache.Cache, cacheTTL time.Duration) *RubyGemsURLProvider {
	return &RubyGemsURLProvider{
		client:  rubygems.NewClient(c, cacheTTL),
		workers: DefaultWorkers,
	}
}

// FetchURLs fetches repository URLs for the given gem names from RubyGems.
// Packages are processed in chunks of 50 for better progress feedback.
func (p *RubyGemsURLProvider) FetchURLs(ctx context.Context, names []string, refresh bool) (map[string]PackageURLs, error) {
	return fetchURLsChunked(ctx, names, p.workers, defaultChunkSize, func(ctx context.Context, name string) urlFetchResult {
		gem, err := p.client.FetchGem(ctx, name, refresh)
		if err != nil {
			return urlFetchResult{name: name}
		}
		projectURLs := make(map[string]string)
		if gem.SourceCodeURI != "" {
			projectURLs["repository"] = gem.SourceCodeURI
		}
		if gem.HomepageURI != "" {
			projectURLs["homepage"] = gem.HomepageURI
		}
		return urlFetchResult{
			name: name,
			urls: PackageURLs{
				ProjectURLs: projectURLs,
				HomePage:    gem.HomepageURI,
			},
			ok: true,
		}
	}), nil
}

// GoProxyURLProvider fetches module URLs from Go module proxy in parallel.
// The underlying goproxy.Client handles rate limiting.
type GoProxyURLProvider struct {
	client  *goproxy.Client
	workers int
}

// NewGoProxyURLProvider creates a new Go module proxy URL provider.
func NewGoProxyURLProvider(c cache.Cache, cacheTTL time.Duration) *GoProxyURLProvider {
	return &GoProxyURLProvider{
		client:  goproxy.NewClient(c, cacheTTL),
		workers: DefaultWorkers,
	}
}

// FetchURLs fetches repository URLs for the given module paths from Go proxy.
// Packages are processed in chunks of 50 for better progress feedback.
func (p *GoProxyURLProvider) FetchURLs(ctx context.Context, names []string, refresh bool) (map[string]PackageURLs, error) {
	return fetchURLsChunked(ctx, names, p.workers, defaultChunkSize, func(ctx context.Context, name string) urlFetchResult {
		mod, err := p.client.FetchModule(ctx, name, refresh)
		if err != nil {
			return urlFetchResult{name: name}
		}
		// FetchModule leaves Repository empty for known hosting platforms
		// (github.com, gitlab.com, etc.) since the URL can be derived cheaply.
		// Derive it here for GitHub enrichment to work.
		repoURL := mod.Repository
		if repoURL == "" {
			repoURL = inferGoRepoURL(name)
		}
		projectURLs := make(map[string]string)
		if repoURL != "" {
			projectURLs["repository"] = repoURL
		}
		return urlFetchResult{
			name: name,
			urls: PackageURLs{
				ProjectURLs: projectURLs,
				HomePage:    repoURL,
			},
			ok: true,
		}
	}), nil
}

// inferGoRepoURL extracts the repository URL from a Go module path.
// For github.com, gitlab.com, bitbucket.org, and golang.org/x modules,
// it converts the module path to an HTTPS URL.
func inferGoRepoURL(modulePath string) string {
	// golang.org/x modules mirror to github.com/golang/<repo>
	if strings.HasPrefix(modulePath, "golang.org/x/") {
		rest := strings.TrimPrefix(modulePath, "golang.org/x/")
		if rest != "" {
			parts := strings.Split(rest, "/")
			return "https://github.com/golang/" + parts[0]
		}
		return ""
	}
	// Common hosting platforms that use path-based module names
	for _, prefix := range []string{"github.com/", "gitlab.com/", "bitbucket.org/"} {
		if strings.HasPrefix(modulePath, prefix) {
			parts := strings.Split(strings.TrimPrefix(modulePath, prefix), "/")
			if len(parts) >= 2 {
				return "https://" + prefix + parts[0] + "/" + parts[1]
			}
		}
	}
	return ""
}

// PackagistURLProvider fetches package URLs from Packagist (PHP) in parallel.
// The underlying packagist.Client handles rate limiting.
type PackagistURLProvider struct {
	client  *packagist.Client
	workers int
}

// NewPackagistURLProvider creates a new Packagist URL provider.
func NewPackagistURLProvider(c cache.Cache, cacheTTL time.Duration) *PackagistURLProvider {
	return &PackagistURLProvider{
		client:  packagist.NewClient(c, cacheTTL),
		workers: DefaultWorkers,
	}
}

// FetchURLs fetches repository URLs for the given package names from Packagist.
// Packages are processed in chunks of 50 for better progress feedback.
func (p *PackagistURLProvider) FetchURLs(ctx context.Context, names []string, refresh bool) (map[string]PackageURLs, error) {
	return fetchURLsChunked(ctx, names, p.workers, defaultChunkSize, func(ctx context.Context, name string) urlFetchResult {
		pkg, err := p.client.FetchPackage(ctx, name, refresh)
		if err != nil {
			return urlFetchResult{name: name}
		}
		projectURLs := make(map[string]string)
		if pkg.Repository != "" {
			projectURLs["repository"] = pkg.Repository
		}
		if pkg.HomePage != "" {
			projectURLs["homepage"] = pkg.HomePage
		}
		return urlFetchResult{
			name: name,
			urls: PackageURLs{
				ProjectURLs: projectURLs,
				HomePage:    pkg.HomePage,
			},
			ok: true,
		}
	}), nil
}

// MavenURLProvider fetches artifact URLs from Maven Central in parallel.
// The underlying maven.Client handles rate limiting.
type MavenURLProvider struct {
	client  *maven.Client
	workers int
}

// NewMavenURLProvider creates a new Maven URL provider.
func NewMavenURLProvider(c cache.Cache, cacheTTL time.Duration) *MavenURLProvider {
	return &MavenURLProvider{
		client:  maven.NewClient(c, cacheTTL),
		workers: DefaultWorkers,
	}
}

// FetchURLs fetches repository URLs for the given artifact coordinates from Maven.
// Coordinates should be in the format "groupId:artifactId".
// Packages are processed in chunks of 50 for better progress feedback.
func (p *MavenURLProvider) FetchURLs(ctx context.Context, names []string, refresh bool) (map[string]PackageURLs, error) {
	return fetchURLsChunked(ctx, names, p.workers, defaultChunkSize, func(ctx context.Context, name string) urlFetchResult {
		artifact, err := p.client.FetchArtifact(ctx, name, refresh)
		if err != nil {
			return urlFetchResult{name: name}
		}
		projectURLs := make(map[string]string)
		if artifact.Repository != "" {
			projectURLs["repository"] = artifact.Repository
		}
		if artifact.HomePage != "" {
			projectURLs["homepage"] = artifact.HomePage
		}
		return urlFetchResult{
			name: name,
			urls: PackageURLs{
				ProjectURLs: projectURLs,
				HomePage:    artifact.HomePage,
			},
			ok: true,
		}
	}), nil
}
