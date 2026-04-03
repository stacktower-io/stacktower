package java

import (
	"context"
	"strings"

	"github.com/matzehuels/stacktower/pkg/cache"
	"github.com/matzehuels/stacktower/pkg/core/deps"
	"github.com/matzehuels/stacktower/pkg/integrations/maven"
)

// Language provides Java dependency resolution via Maven Central.
// Supports pom.xml and build.gradle manifest files.
var Language = &deps.Language{
	Name:                  "java",
	DefaultRegistry:       "maven",
	DefaultRuntimeVersion: "17", // LTS
	RegistryAliases:       map[string]string{"maven-central": "maven", "mvn": "maven"},
	ManifestTypes:         []string{"pom", "gradle"},
	ManifestAliases: map[string]string{
		"pom.xml":          "pom",
		"build.gradle":     "gradle",
		"build.gradle.kts": "gradle",
	},
	NewResolver:     newResolver,
	NewManifest:     newManifest,
	ManifestParsers: manifestParsers,
	NormalizeName:   NormalizeCoordinate,
}

func newResolver(backend cache.Cache, opts deps.Options) (deps.Resolver, error) {
	c := maven.NewClient(backend, opts.CacheTTL)
	f := fetcher{client: c, javaVersion: opts.RuntimeVersion}

	// Use PubGrub for proper SAT-solver-based dependency resolution
	return deps.NewPubGrubResolver("maven", f, MavenMatcher{})
}

type fetcher struct {
	client      *maven.Client
	javaVersion string
}

func (f fetcher) Fetch(ctx context.Context, name string, refresh bool) (*deps.Package, error) {
	coord := NormalizeCoordinate(name)
	a, err := f.client.FetchArtifact(ctx, coord, refresh)
	if err != nil {
		return nil, err
	}
	return mavenArtifactToDepsPkg(a), nil
}

func (f fetcher) FetchVersion(ctx context.Context, name, version string, refresh bool) (*deps.Package, error) {
	coord := NormalizeCoordinate(name)
	a, err := f.client.FetchArtifactVersion(ctx, coord, version, refresh)
	if err != nil {
		return nil, err
	}
	return mavenArtifactToDepsPkg(a), nil
}

// ListVersions implements deps.VersionLister for constraint-based resolution.
func (f fetcher) ListVersions(ctx context.Context, name string, refresh bool) ([]string, error) {
	coord := NormalizeCoordinate(name)
	return f.client.ListVersions(ctx, coord, refresh)
}

func mavenArtifactToDepsPkg(a *maven.ArtifactInfo) *deps.Package {
	pkg := &deps.Package{
		Name:         a.Coordinate(),
		Version:      a.Version,
		Description:  a.Description,
		License:      a.License,
		Repository:   a.Repository,
		HomePage:     a.HomePage,
		ManifestFile: "pom.xml",
	}
	// Convert maven.Dependency to deps.Dependency with constraints
	if len(a.Dependencies) > 0 {
		pkg.Dependencies = make([]deps.Dependency, len(a.Dependencies))
		for i, d := range a.Dependencies {
			pkg.Dependencies[i] = deps.Dependency{
				Name:       d.Name,
				Constraint: d.Constraint,
			}
		}
	}
	return pkg
}

// NormalizeCoordinate converts filename-safe coordinates to Maven format.
// Since colons are not allowed in filenames (especially on Windows and in some
// build tools), underscores can be used as a substitute. This function converts
// "groupId_artifactId" to "groupId:artifactId" when no colon is present.
//
// Examples:
//   - "com.google.guava:guava" → "com.google.guava:guava" (unchanged)
//   - "com.google.guava_guava" → "com.google.guava:guava" (converted)
func NormalizeCoordinate(coord string) string {
	if strings.Contains(coord, ":") {
		return coord
	}
	// Replace the last underscore with a colon
	// GroupIds follow reverse domain notation (no underscores typically)
	// while artifactIds may contain hyphens or underscores
	if idx := strings.LastIndex(coord, "_"); idx != -1 {
		return coord[:idx] + ":" + coord[idx+1:]
	}
	return coord
}

func newManifest(name string, res deps.Resolver) deps.ManifestParser {
	switch name {
	case "pom":
		return &POMParser{resolver: res}
	case "gradle":
		return &GradleParser{resolver: res}
	default:
		return nil
	}
}

func manifestParsers(res deps.Resolver) []deps.ManifestParser {
	return []deps.ManifestParser{
		&POMParser{resolver: res},
		&GradleParser{resolver: res},
	}
}
