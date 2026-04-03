package java

import (
	"encoding/xml"
	"os"
	"regexp"
	"strings"

	"github.com/matzehuels/stacktower/pkg/core/dag"
	"github.com/matzehuels/stacktower/pkg/core/deps"
	"github.com/matzehuels/stacktower/pkg/core/deps/constraints"
	"github.com/matzehuels/stacktower/pkg/observability"
)

// javaVersionRE extracts numeric Java version (e.g., "17", "11", "1.8")
var javaVersionRE = regexp.MustCompile(`(\d+(?:\.\d+)?)`)

// POMParser parses Maven pom.xml files. It extracts dependencies and
// optionally resolves them via Maven Central.
type POMParser struct {
	resolver deps.Resolver
}

func (p *POMParser) Type() string              { return "pom.xml" }
func (p *POMParser) IncludesTransitive() bool  { return p.resolver != nil }
func (p *POMParser) Supports(name string) bool { return name == "pom.xml" }

func (p *POMParser) Parse(path string, opts deps.Options) (*deps.ManifestResult, error) {
	opts = opts.WithDefaults()

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var pom pomProject
	if err := xml.Unmarshal(data, &pom); err != nil {
		return nil, err
	}

	directDeps := extractDependenciesWithVersions(&pom)

	// Emit observability hooks for extracted dependencies
	hooks := observability.ResolverFromContext(opts.Ctx)
	for _, dep := range directDeps {
		hooks.OnFetchStart(opts.Ctx, dep.Name, 0)
		hooks.OnFetchComplete(opts.Ctx, dep.Name, 0, 0, nil)
	}

	var g *dag.DAG
	if p.resolver != nil {
		g, err = deps.ResolveAndMerge(opts.Ctx, p.resolver, directDeps, opts)
		if err != nil {
			return nil, err
		}
	} else {
		g = deps.ShallowGraphFromDeps(directDeps)
	}

	// Extract Java version from properties
	javaVersion := extractJavaVersion(&pom.Properties)

	return &deps.ManifestResult{
		Graph:              g,
		Type:               p.Type(),
		IncludesTransitive: p.resolver != nil,
		RootPackage:        pom.GroupID + ":" + pom.ArtifactID,
		RuntimeVersion:     normalizeJavaVersion(javaVersion),
		RuntimeConstraint:  constraints.NormalizeRuntimeConstraint(normalizeJavaVersion(javaVersion)),
	}, nil
}

// extractJavaVersion extracts the Java version from pom.xml properties.
// Priority: maven.compiler.source > maven.compiler.target > java.version
func extractJavaVersion(props *pomProperties) string {
	if props.MavenCompilerSource != "" {
		return props.MavenCompilerSource
	}
	if props.MavenCompilerTarget != "" {
		return props.MavenCompilerTarget
	}
	if props.JavaVersion != "" {
		return props.JavaVersion
	}
	return ""
}

// normalizeJavaVersion normalizes Java version strings.
// Converts "1.8" to "8", keeps "11", "17" etc. as-is.
func normalizeJavaVersion(version string) string {
	if version == "" {
		return ""
	}
	// Handle "1.8" style versions
	if strings.HasPrefix(version, "1.") && len(version) >= 3 {
		return version[2:]
	}
	// Extract just the major version number
	if m := javaVersionRE.FindStringSubmatch(version); len(m) > 1 {
		v := m[1]
		// If it's like "1.8", convert to "8"
		if strings.HasPrefix(v, "1.") {
			return v[2:]
		}
		return v
	}
	return version
}

// extractDependenciesWithVersions extracts dependencies with version information
func extractDependenciesWithVersions(pom *pomProject) []deps.Dependency {
	var result []deps.Dependency
	seen := make(map[string]bool)

	for _, dep := range pom.Dependencies {
		// Skip test and provided scope dependencies
		if dep.Scope == "test" || dep.Scope == "provided" || dep.Optional == "true" {
			continue
		}
		// Skip dependencies with unresolved Maven properties
		if strings.HasPrefix(dep.GroupID, "${") || strings.HasPrefix(dep.ArtifactID, "${") {
			continue
		}
		coord := dep.GroupID + ":" + dep.ArtifactID
		if !seen[coord] {
			seen[coord] = true
			d := deps.Dependency{Name: coord}
			// In Maven, versions are typically pinned (exact)
			// unless they use version ranges like [1.0,2.0)
			if dep.Version != "" && !strings.HasPrefix(dep.Version, "${") {
				d.Pinned = dep.Version
				d.Constraint = dep.Version
			}
			result = append(result, d)
		}
	}
	return result
}

// extractDependencies is kept for backward compatibility
func extractDependencies(pom *pomProject) []string {
	var names []string
	seen := make(map[string]bool)

	for _, dep := range pom.Dependencies {
		// Skip test and provided scope dependencies
		if dep.Scope == "test" || dep.Scope == "provided" || dep.Optional == "true" {
			continue
		}
		// Skip dependencies with unresolved Maven properties
		if strings.HasPrefix(dep.GroupID, "${") || strings.HasPrefix(dep.ArtifactID, "${") {
			continue
		}
		coord := dep.GroupID + ":" + dep.ArtifactID
		if !seen[coord] {
			seen[coord] = true
			names = append(names, coord)
		}
	}
	return names
}

type pomProject struct {
	GroupID      string          `xml:"groupId"`
	ArtifactID   string          `xml:"artifactId"`
	Version      string          `xml:"version"`
	Name         string          `xml:"name"`
	Description  string          `xml:"description"`
	URL          string          `xml:"url"`
	Dependencies []pomDependency `xml:"dependencies>dependency"`
	Parent       *pomParent      `xml:"parent"`
	Properties   pomProperties   `xml:"properties"`
}

type pomProperties struct {
	MavenCompilerSource string `xml:"maven.compiler.source"`
	MavenCompilerTarget string `xml:"maven.compiler.target"`
	JavaVersion         string `xml:"java.version"`
}

type pomParent struct {
	GroupID    string `xml:"groupId"`
	ArtifactID string `xml:"artifactId"`
	Version    string `xml:"version"`
}

type pomDependency struct {
	GroupID    string `xml:"groupId"`
	ArtifactID string `xml:"artifactId"`
	Version    string `xml:"version"`
	Scope      string `xml:"scope"`
	Optional   string `xml:"optional"`
}
