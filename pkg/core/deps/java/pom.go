package java

import (
	"encoding/xml"
	"os"
	"regexp"
	"strings"

	"github.com/stacktower-io/stacktower/pkg/core/dag"
	"github.com/stacktower-io/stacktower/pkg/core/deps"
	"github.com/stacktower-io/stacktower/pkg/core/deps/constraints"
	"github.com/stacktower-io/stacktower/pkg/observability"
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

// resolvePomProperty resolves simple ${property} references against the POM's
// own <properties> section. Returns the input unchanged when it is not a
// property reference; returns "" when the property is unknown.
func resolvePomProperty(value string, props *pomProperties) string {
	if !strings.HasPrefix(value, "${") || !strings.HasSuffix(value, "}") {
		return value
	}
	key := value[2 : len(value)-1]
	if props != nil {
		if v, ok := props.All[key]; ok {
			return v
		}
	}
	return ""
}

// buildManagedVersions builds a coordinate -> version map from the POM's own
// <dependencyManagement> section, resolving ${property} references.
// TODO: fetch and merge parent/imported BOMs (scope=import) for versions
// managed outside this POM. Out of scope for now.
func buildManagedVersions(pom *pomProject) map[string]string {
	managed := make(map[string]string, len(pom.DependencyManagement))
	for _, dep := range pom.DependencyManagement {
		if strings.HasPrefix(dep.GroupID, "${") || strings.HasPrefix(dep.ArtifactID, "${") {
			continue
		}
		version := resolvePomProperty(dep.Version, &pom.Properties)
		if version == "" {
			continue
		}
		managed[dep.GroupID+":"+dep.ArtifactID] = version
	}
	return managed
}

// extractDependenciesWithVersions extracts dependencies with version information.
// Dependencies without an inline <version> fall back to the version declared in
// the POM's own <dependencyManagement> section, and simple ${property}
// references are resolved from <properties> in the same POM.
func extractDependenciesWithVersions(pom *pomProject) []deps.Dependency {
	var result []deps.Dependency
	seen := make(map[string]bool)
	managed := buildManagedVersions(pom)

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
			version := resolvePomProperty(dep.Version, &pom.Properties)
			if version == "" {
				// Version managed by <dependencyManagement> in the same POM.
				version = managed[coord]
			}
			// In Maven, versions are typically pinned (exact)
			// unless they use version ranges like [1.0,2.0)
			if version != "" {
				d.Pinned = version
				d.Constraint = version
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
	GroupID              string          `xml:"groupId"`
	ArtifactID           string          `xml:"artifactId"`
	Version              string          `xml:"version"`
	Name                 string          `xml:"name"`
	Description          string          `xml:"description"`
	URL                  string          `xml:"url"`
	Dependencies         []pomDependency `xml:"dependencies>dependency"`
	DependencyManagement []pomDependency `xml:"dependencyManagement>dependencies>dependency"`
	Parent               *pomParent      `xml:"parent"`
	Properties           pomProperties   `xml:"properties"`
}

// pomProperties holds the POM <properties> section. Well-known Java version
// properties are exposed as fields; all properties (including custom ones like
// <guava.version>) are collected in All for ${property} resolution.
type pomProperties struct {
	MavenCompilerSource string
	MavenCompilerTarget string
	JavaVersion         string
	All                 map[string]string
}

// UnmarshalXML collects every child element of <properties> into a generic
// map so that arbitrary ${property} references in versions can be resolved.
func (p *pomProperties) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	p.All = make(map[string]string)
	for {
		tok, err := d.Token()
		if err != nil {
			return err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			var value string
			if err := d.DecodeElement(&value, &t); err != nil {
				return err
			}
			p.All[t.Name.Local] = strings.TrimSpace(value)
		case xml.EndElement:
			if t.Name == start.Name {
				p.MavenCompilerSource = p.All["maven.compiler.source"]
				p.MavenCompilerTarget = p.All["maven.compiler.target"]
				p.JavaVersion = p.All["java.version"]
				return nil
			}
		}
	}
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
