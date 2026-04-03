package ruby

import (
	"context"
	"strings"

	"github.com/matzehuels/stacktower/pkg/cache"
	"github.com/matzehuels/stacktower/pkg/core/deps"
	"github.com/matzehuels/stacktower/pkg/core/deps/constraints"
	"github.com/matzehuels/stacktower/pkg/integrations/rubygems"
)

// Language provides Ruby dependency resolution via RubyGems.
// Supports Gemfile and Gemfile.lock manifest files.
var Language = &deps.Language{
	Name:                  "ruby",
	DefaultRegistry:       "rubygems",
	DefaultRuntimeVersion: "3.2",
	RegistryAliases:       map[string]string{"gems": "rubygems"},
	ManifestTypes:         []string{"gemfile", "gemfile-lock"},
	ManifestAliases: map[string]string{
		"Gemfile":      "gemfile",
		"Gemfile.lock": "gemfile-lock",
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
	case "gemfile":
		return &Gemfile{resolver: res}
	case "gemfile-lock":
		return &GemfileLock{} // Lock file doesn't need resolver
	default:
		return nil
	}
}

func manifestParsers(res deps.Resolver) []deps.ManifestParser {
	// Lock file first (more complete), then manifest
	return []deps.ManifestParser{
		&GemfileLock{},
		&Gemfile{resolver: res},
	}
}

func newResolver(backend cache.Cache, opts deps.Options) (deps.Resolver, error) {
	c := rubygems.NewClient(backend, opts.CacheTTL)
	f := fetcher{client: c, rubyVersion: opts.RuntimeVersion}

	// Use PubGrub for proper SAT-solver-based dependency resolution
	return deps.NewPubGrubResolver("rubygems", f, GemMatcher{})
}

type fetcher struct {
	client      *rubygems.Client
	rubyVersion string
}

func (f fetcher) Fetch(ctx context.Context, name string, refresh bool) (*deps.Package, error) {
	g, err := f.client.FetchGem(ctx, name, refresh)
	if err != nil {
		return nil, err
	}
	if err := f.checkCompatibility(g, name); err != nil {
		return nil, err
	}
	return gemInfoToDepsPkg(g), nil
}

func (f fetcher) FetchVersion(ctx context.Context, name, version string, refresh bool) (*deps.Package, error) {
	g, err := f.client.FetchGemVersion(ctx, name, version, refresh)
	if err != nil {
		return nil, err
	}
	if err := f.checkCompatibility(g, name); err != nil {
		return nil, err
	}
	return gemInfoToDepsPkg(g), nil
}

func (f fetcher) checkCompatibility(g *rubygems.GemInfo, name string) error {
	if f.rubyVersion == "" || g.RequiredRubyVersion == "" {
		return nil
	}
	if !constraints.CheckVersionConstraint(f.rubyVersion, g.RequiredRubyVersion) {
		return &deps.IncompatibleRuntimeError{
			Package:           name,
			Version:           g.Version,
			RuntimeConstraint: g.RequiredRubyVersion,
			TargetRuntime:     f.rubyVersion,
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

func gemInfoToDepsPkg(g *rubygems.GemInfo) *deps.Package {
	pkg := &deps.Package{
		Name:              g.Name,
		Version:           g.Version,
		Description:       g.Description,
		License:           g.License,
		Author:            g.Authors,
		Downloads:         g.Downloads,
		Repository:        g.SourceCodeURI,
		HomePage:          g.HomepageURI,
		ManifestFile:      "Gemfile",
		RuntimeConstraint: g.RequiredRubyVersion,
	}
	// Convert rubygems.Dependency to deps.Dependency with constraints
	if len(g.Dependencies) > 0 {
		pkg.Dependencies = make([]deps.Dependency, len(g.Dependencies))
		for i, d := range g.Dependencies {
			pkg.Dependencies[i] = deps.Dependency{
				Name:       d.Name,
				Constraint: d.Constraint,
			}
		}
	}
	return pkg
}
