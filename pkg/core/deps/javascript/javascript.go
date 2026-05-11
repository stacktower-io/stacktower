package javascript

import (
	"context"
	"strings"

	"github.com/stacktower-io/stacktower/pkg/cache"
	"github.com/stacktower-io/stacktower/pkg/core/deps"
	"github.com/stacktower-io/stacktower/pkg/core/deps/constraints"
	"github.com/stacktower-io/stacktower/pkg/integrations/npm"
)

// Language provides JavaScript/TypeScript dependency resolution via npm.
// Supports package.json and package-lock.json manifest files.
var Language = &deps.Language{
	Name:                  "javascript",
	DefaultRegistry:       "npm",
	DefaultRuntimeVersion: "22.12.0", // Node.js LTS baseline for npm engines.node checks
	ManifestTypes:         []string{"package", "package-lock"},
	ManifestAliases: map[string]string{
		"package.json":      "package",
		"package-lock.json": "package-lock",
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
	case "package":
		return &PackageJSON{resolver: res}
	case "package-lock":
		return &PackageLock{} // Lock file doesn't need resolver
	default:
		return nil
	}
}

func manifestParsers(res deps.Resolver) []deps.ManifestParser {
	// Lock file first (more complete), then manifest
	return []deps.ManifestParser{
		&PackageLock{},
		&PackageJSON{resolver: res},
	}
}

func newResolver(backend cache.Cache, opts deps.Options) (deps.Resolver, error) {
	c := npm.NewClient(backend, opts.CacheTTL)
	f := fetcher{client: c, nodeVersion: opts.RuntimeVersion}

	// npm permits multiple installed copies of the same package at different
	// versions (nested node_modules). Use a greedy BFS resolver that mirrors
	// npm's actual install algorithm instead of PubGrub's single-version SAT
	// solver, which can't handle npm's multi-version model efficiently.
	return &npmResolver{fetcher: f, matcher: SemverMatcher{}}, nil
}

type fetcher struct {
	client      *npm.Client
	nodeVersion string
}

func (f fetcher) Fetch(ctx context.Context, name string, refresh bool) (*deps.Package, error) {
	p, err := f.client.FetchPackage(ctx, name, refresh)
	if err != nil {
		return nil, err
	}
	if err := f.checkCompatibility(p, name); err != nil {
		return nil, err
	}
	return npmPkgToDepsPkg(p), nil
}

func (f fetcher) FetchVersion(ctx context.Context, name, version string, refresh bool) (*deps.Package, error) {
	p, err := f.client.FetchPackageVersion(ctx, name, version, refresh)
	if err != nil {
		return nil, err
	}
	if err := f.checkCompatibility(p, name); err != nil {
		return nil, err
	}
	return npmPkgToDepsPkg(p), nil
}

func (f fetcher) checkCompatibility(p *npm.PackageInfo, name string) error {
	if f.nodeVersion == "" || p.RequiredNode == "" {
		return nil
	}
	if !constraints.CheckVersionConstraint(f.nodeVersion, p.RequiredNode) {
		return &deps.IncompatibleRuntimeError{
			Package:           name,
			Version:           p.Version,
			RuntimeConstraint: p.RequiredNode,
			TargetRuntime:     f.nodeVersion,
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

func npmPkgToDepsPkg(p *npm.PackageInfo) *deps.Package {
	pkg := &deps.Package{
		Name:              p.Name,
		Version:           p.Version,
		Description:       p.Description,
		License:           p.License,
		LicenseText:       p.LicenseText,
		Author:            p.Author,
		Repository:        p.Repository,
		HomePage:          p.HomePage,
		ManifestFile:      "package.json",
		RuntimeConstraint: p.RequiredNode,
	}
	// Convert npm.Dependency to deps.Dependency with constraints
	if len(p.Dependencies) > 0 {
		pkg.Dependencies = make([]deps.Dependency, len(p.Dependencies))
		for i, d := range p.Dependencies {
			pkg.Dependencies[i] = deps.Dependency{
				Name:       d.Name,
				Constraint: d.Constraint,
			}
		}
	}
	return pkg
}
