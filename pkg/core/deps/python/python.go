package python

import (
	"context"

	"github.com/matzehuels/stacktower/pkg/cache"
	"github.com/matzehuels/stacktower/pkg/core/deps"
	"github.com/matzehuels/stacktower/pkg/integrations"
	"github.com/matzehuels/stacktower/pkg/integrations/pypi"
)

// Language provides Python dependency resolution via PyPI.
// Supports uv.lock, poetry.lock, pyproject.toml, and requirements.txt manifest files.
var Language = &deps.Language{
	Name:                  "python",
	DefaultRegistry:       "pypi",
	DefaultRuntimeVersion: pypi.DefaultPythonVersion,
	ManifestTypes:         []string{"uv", "poetry", "pyproject", "requirements"},
	ManifestAliases: map[string]string{
		"uv.lock":          "uv",
		"poetry.lock":      "poetry",
		"pyproject.toml":   "pyproject",
		"requirements.txt": "requirements",
	},
	NewResolver:     newResolver,
	NewManifest:     newManifest,
	ManifestParsers: manifestParsers,
	NormalizeName:   normalize,
}

func newResolver(backend cache.Cache, opts deps.Options) (deps.Resolver, error) {
	c := pypi.NewClient(backend, opts.CacheTTL, opts.RuntimeVersion)
	f := fetcher{client: c, pythonVersion: opts.RuntimeVersion}

	// Use PubGrub for proper SAT-solver-based dependency resolution
	return deps.NewPubGrubResolver("pypi", f, PEP440Matcher{})
}

type fetcher struct {
	client        *pypi.Client
	pythonVersion string
}

func (f fetcher) Fetch(ctx context.Context, name string, refresh bool) (*deps.Package, error) {
	p, err := f.client.FetchPackage(ctx, name, refresh)
	if err != nil {
		return nil, err
	}
	if err := f.checkCompatibility(p, name); err != nil {
		return nil, err
	}
	return pypiPkgToDepsPkg(p), nil
}

func (f fetcher) FetchVersion(ctx context.Context, name, version string, refresh bool) (*deps.Package, error) {
	p, err := f.client.FetchPackageVersion(ctx, name, version, refresh)
	if err != nil {
		return nil, err
	}
	if err := f.checkCompatibility(p, name); err != nil {
		return nil, err
	}
	return pypiPkgToDepsPkg(p), nil
}

func (f fetcher) checkCompatibility(p *pypi.PackageInfo, name string) error {
	if f.pythonVersion == "" || p.RequiresPython == "" {
		return nil
	}
	if !p.IsCompatibleWith(f.pythonVersion) {
		return &deps.IncompatibleRuntimeError{
			Package:           name,
			Version:           p.Version,
			RuntimeConstraint: p.RequiresPython,
			TargetRuntime:     f.pythonVersion,
		}
	}
	return nil
}

// ListVersions implements deps.VersionLister for constraint-based resolution.
func (f fetcher) ListVersions(ctx context.Context, name string, refresh bool) ([]string, error) {
	return f.client.ListVersions(ctx, name, refresh)
}

// ListVersionsWithConstraints implements deps.RuntimeConstraintLister.
// Returns all versions with their requires_python constraints in a single API call.
func (f fetcher) ListVersionsWithConstraints(ctx context.Context, name string, refresh bool) (map[string]string, error) {
	return f.client.ListVersionsWithConstraints(ctx, name, refresh)
}

func pypiPkgToDepsPkg(p *pypi.PackageInfo) *deps.Package {
	pkg := &deps.Package{
		Name:              p.Name,
		Version:           p.Version,
		Description:       p.Summary,
		License:           p.License,
		LicenseText:       p.LicenseText,
		Author:            p.Author,
		HomePage:          p.HomePage,
		ProjectURLs:       p.ProjectURLs,
		ManifestFile:      "pyproject.toml",
		RuntimeConstraint: p.RequiresPython,
	}
	// Convert pypi.Dependency to deps.Dependency with constraints
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

func newManifest(name string, res deps.Resolver) deps.ManifestParser {
	switch name {
	case "uv":
		return &UVLock{}
	case "poetry":
		return &PoetryLock{}
	case "pyproject":
		return &PyProject{resolver: res}
	case "requirements":
		return &Requirements{resolver: res}
	default:
		return nil
	}
}

func manifestParsers(res deps.Resolver) []deps.ManifestParser {
	return []deps.ManifestParser{
		&UVLock{}, // Lockfiles first (most complete)
		&PoetryLock{},
		&PyProject{resolver: res},
		&Requirements{resolver: res},
	}
}

func normalize(name string) string {
	return integrations.NormalizePkgName(name)
}
