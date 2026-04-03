package rust

import (
	"context"
	"strings"

	"github.com/matzehuels/stacktower/pkg/cache"
	"github.com/matzehuels/stacktower/pkg/core/deps"
	"github.com/matzehuels/stacktower/pkg/core/deps/constraints"
	"github.com/matzehuels/stacktower/pkg/integrations/crates"
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

	// Use PubGrub for proper SAT-solver-based dependency resolution
	return deps.NewPubGrubResolver("crates.io", f, CargoMatcher{})
}

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

func (f fetcher) checkCompatibility(cr *crates.CrateInfo, name string) error {
	if f.rustVersion == "" || cr.MSRV == "" {
		return nil
	}
	// MSRV is a minimum version requirement, so check if rustVersion >= MSRV
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
	// Convert crates.Dependency to deps.Dependency with constraints
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
