package php

import (
	"context"
	"strings"

	"github.com/matzehuels/stacktower/pkg/cache"
	"github.com/matzehuels/stacktower/pkg/core/deps"
	"github.com/matzehuels/stacktower/pkg/core/deps/constraints"
	"github.com/matzehuels/stacktower/pkg/integrations/packagist"
)

// Language provides PHP dependency resolution via Packagist.
// Supports composer.json and composer.lock manifest files.
var Language = &deps.Language{
	Name:                  "php",
	DefaultRegistry:       "packagist",
	DefaultRuntimeVersion: "8.2",
	ManifestTypes:         []string{"composer", "composer-lock"},
	ManifestAliases: map[string]string{
		"composer.json": "composer",
		"composer.lock": "composer-lock",
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
	case "composer":
		return &ComposerJSON{resolver: res}
	case "composer-lock":
		return &ComposerLock{} // Lock file doesn't need resolver
	default:
		return nil
	}
}

func manifestParsers(res deps.Resolver) []deps.ManifestParser {
	// Lock file first (more complete), then manifest
	return []deps.ManifestParser{
		&ComposerLock{},
		&ComposerJSON{resolver: res},
	}
}

func newResolver(backend cache.Cache, opts deps.Options) (deps.Resolver, error) {
	c := packagist.NewClient(backend, opts.CacheTTL)
	f := fetcher{client: c, phpVersion: opts.RuntimeVersion}

	// Use PubGrub for proper SAT-solver-based dependency resolution
	return deps.NewPubGrubResolver("packagist", f, ComposerMatcher{})
}

type fetcher struct {
	client     *packagist.Client
	phpVersion string
}

func (f fetcher) Fetch(ctx context.Context, name string, refresh bool) (*deps.Package, error) {
	p, err := f.client.FetchPackage(ctx, name, refresh)
	if err != nil {
		return nil, err
	}
	if err := f.checkCompatibility(p, name); err != nil {
		return nil, err
	}
	return packagistPkgToDepsPkg(p), nil
}

func (f fetcher) FetchVersion(ctx context.Context, name, version string, refresh bool) (*deps.Package, error) {
	p, err := f.client.FetchPackageVersion(ctx, name, version, refresh)
	if err != nil {
		return nil, err
	}
	if err := f.checkCompatibility(p, name); err != nil {
		return nil, err
	}
	return packagistPkgToDepsPkg(p), nil
}

func (f fetcher) checkCompatibility(p *packagist.PackageInfo, name string) error {
	if f.phpVersion == "" || p.RequiredPHP == "" {
		return nil
	}
	if !constraints.CheckVersionConstraint(f.phpVersion, p.RequiredPHP) {
		return &deps.IncompatibleRuntimeError{
			Package:           name,
			Version:           p.Version,
			RuntimeConstraint: p.RequiredPHP,
			TargetRuntime:     f.phpVersion,
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

func packagistPkgToDepsPkg(p *packagist.PackageInfo) *deps.Package {
	pkg := &deps.Package{
		Name:              p.Name,
		Version:           p.Version,
		Description:       p.Description,
		License:           p.License,
		Author:            p.Author,
		Repository:        p.Repository,
		HomePage:          p.HomePage,
		ManifestFile:      "composer.json",
		RuntimeConstraint: p.RequiredPHP,
	}
	// Convert packagist.Dependency to deps.Dependency with constraints
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
