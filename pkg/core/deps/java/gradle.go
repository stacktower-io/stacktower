package java

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/matzehuels/stacktower/pkg/core/dag"
	"github.com/matzehuels/stacktower/pkg/core/deps"
	"github.com/matzehuels/stacktower/pkg/core/deps/constraints"
	"github.com/matzehuels/stacktower/pkg/observability"
)

// GradleParser parses Gradle build files (build.gradle and build.gradle.kts).
// It extracts dependencies from both Groovy DSL and Kotlin DSL formats.
type GradleParser struct {
	resolver deps.Resolver
}

func (p *GradleParser) Type() string             { return "build.gradle" }
func (p *GradleParser) IncludesTransitive() bool { return p.resolver != nil }

func (p *GradleParser) Supports(name string) bool {
	return name == "build.gradle" || name == "build.gradle.kts"
}

func (p *GradleParser) Parse(path string, opts deps.Options) (*deps.ManifestResult, error) {
	opts = opts.WithDefaults()

	// Read file content for both dependency and Java version extraction
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	directDeps := parseGradleDependencies(f)

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

	// Try to extract project name from settings.gradle or use filename
	projectName := extractGradleProjectName(path)

	// Extract Java version from build file
	javaVersion := extractGradleJavaVersion(string(data))

	return &deps.ManifestResult{
		Graph:              g,
		Type:               p.Type(),
		IncludesTransitive: p.resolver != nil,
		RootPackage:        projectName,
		RuntimeVersion:     javaVersion,
		RuntimeConstraint:  constraints.NormalizeRuntimeConstraint(javaVersion),
	}, nil
}

// gradleConfigurations lists the Gradle dependency configurations we care about.
// We skip test configurations to focus on production dependencies.
var gradleConfigurations = map[string]bool{
	// Production configurations
	"implementation":      true,
	"api":                 true,
	"compile":             true, // deprecated but still used
	"runtimeOnly":         true,
	"compileOnly":         true,
	"runtimeClasspath":    true,
	"compileClasspath":    true,
	"annotationProcessor": true,
	// Android-specific
	"kapt": true, // Kotlin annotation processing
	"ksp":  true, // Kotlin Symbol Processing
}

// gradleTestConfigurations lists test-related configurations to skip.
var gradleTestConfigurations = map[string]bool{
	"testImplementation":        true,
	"testCompile":               true,
	"testRuntimeOnly":           true,
	"testCompileOnly":           true,
	"androidTestImplementation": true,
	"debugImplementation":       true,
}

// depStringPattern matches dependency declarations like:
// implementation 'group:artifact:version'
// implementation "group:artifact:version"
// implementation("group:artifact:version")
// api 'group:artifact:version:classifier@extension'
var depStringPattern = regexp.MustCompile(`['"]([^'"]+:[^'"]+:[^'"]+)['"]`)

// javaCompatibilityPattern matches sourceCompatibility or targetCompatibility settings
// Examples: sourceCompatibility = '17', sourceCompatibility = JavaVersion.VERSION_11
var javaCompatibilityPattern = regexp.MustCompile(`(?:source|target)Compatibility\s*[=:]\s*(?:JavaVersion\.VERSION_)?['"]?(\d+(?:\.\d+)?)['"]?`)

// javaToolchainPattern matches java toolchain languageVersion settings
// Examples: languageVersion.set(JavaLanguageVersion.of(17))
var javaToolchainPattern = regexp.MustCompile(`languageVersion(?:\s*=\s*|\s*\.set\s*\(?\s*)(?:JavaLanguageVersion\.of\s*\(?\s*)?['"]?(\d+)['"]?`)

// depMapPattern matches map-style declarations (deprecated but still found):
// implementation group: 'com.google.guava', name: 'guava', version: '31.0-jre'
var depMapPattern = regexp.MustCompile(`group:\s*['"]([^'"]+)['"],?\s*name:\s*['"]([^'"]+)['"](?:,?\s*version:\s*['"]([^'"]+)['"])?`)

// parseGradleDependencies extracts dependencies from a Gradle build file.
func parseGradleDependencies(f *os.File) []deps.Dependency {
	var result []deps.Dependency
	seen := make(map[string]bool)
	inDependencies := false
	braceCount := 0

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "//") || strings.HasPrefix(line, "/*") || strings.HasPrefix(line, "*") {
			continue
		}

		// Detect dependencies block
		if strings.HasPrefix(line, "dependencies") && (strings.Contains(line, "{") || strings.HasSuffix(line, "{")) {
			inDependencies = true
			braceCount = 1
			// Check if there's content after the opening brace on same line
			if idx := strings.Index(line, "{"); idx != -1 {
				line = line[idx+1:]
			} else {
				continue
			}
		}

		if !inDependencies {
			continue
		}

		// Track brace depth
		braceCount += strings.Count(line, "{") - strings.Count(line, "}")
		if braceCount <= 0 {
			inDependencies = false
			continue
		}

		// Skip test configurations
		if isTestConfiguration(line) {
			continue
		}

		// Try to extract dependencies from this line
		deps := extractGradleDeps(line, seen)
		result = append(result, deps...)
	}

	return result
}

// isTestConfiguration checks if a line starts with a test-related configuration.
func isTestConfiguration(line string) bool {
	for testConfig := range gradleTestConfigurations {
		if strings.HasPrefix(line, testConfig) {
			return true
		}
		// Also check for Kotlin DSL style: testImplementation(...)
		if strings.HasPrefix(line, testConfig+"(") {
			return true
		}
	}
	return false
}

// extractGradleDeps extracts dependencies from a single line.
func extractGradleDeps(line string, seen map[string]bool) []deps.Dependency {
	var result []deps.Dependency

	// Skip lines that don't look like dependency declarations
	hasConfig := false
	for config := range gradleConfigurations {
		if strings.Contains(line, config) {
			hasConfig = true
			break
		}
	}

	// Also check for generic patterns
	if !hasConfig && !strings.Contains(line, "'") && !strings.Contains(line, "\"") {
		return nil
	}

	// Skip project dependencies (internal modules)
	if strings.Contains(line, "project(") || strings.Contains(line, "project '") || strings.Contains(line, "project \"") {
		return nil
	}

	// Skip platform/BOM dependencies
	if strings.Contains(line, "platform(") || strings.Contains(line, "enforcedPlatform(") {
		return nil
	}

	// Try string notation first (most common)
	// Matches: 'group:artifact:version' or "group:artifact:version"
	matches := depStringPattern.FindAllStringSubmatch(line, -1)
	for _, match := range matches {
		if len(match) >= 2 {
			dep := parseGradleCoordinate(match[1])
			if dep.Name != "" && !seen[dep.Name] {
				seen[dep.Name] = true
				result = append(result, dep)
			}
		}
	}

	// Try map notation (deprecated but still used)
	if mapMatch := depMapPattern.FindStringSubmatch(line); len(mapMatch) >= 3 {
		group := mapMatch[1]
		name := mapMatch[2]
		version := ""
		if len(mapMatch) >= 4 {
			version = mapMatch[3]
		}
		coord := group + ":" + name
		if !seen[coord] {
			seen[coord] = true
			dep := deps.Dependency{Name: coord}
			if version != "" {
				dep.Pinned = version
				dep.Constraint = version
			}
			result = append(result, dep)
		}
	}

	return result
}

// parseGradleCoordinate parses a Gradle dependency coordinate string.
// Format: group:artifact:version[:classifier][@extension]
func parseGradleCoordinate(coord string) deps.Dependency {
	// Remove @extension if present (e.g., @aar, @zip)
	if atIdx := strings.Index(coord, "@"); atIdx != -1 {
		coord = coord[:atIdx]
	}

	parts := strings.Split(coord, ":")
	if len(parts) < 2 {
		return deps.Dependency{}
	}

	group := parts[0]
	artifact := parts[1]
	var version string
	if len(parts) >= 3 {
		version = parts[2]
	}

	// Skip dependencies with Gradle property references
	if strings.Contains(group, "$") || strings.Contains(artifact, "$") {
		return deps.Dependency{}
	}

	dep := deps.Dependency{
		Name: group + ":" + artifact,
	}
	if version != "" && !strings.Contains(version, "$") {
		dep.Pinned = version
		dep.Constraint = version
	}

	return dep
}

// rootProjectNamePattern matches rootProject.name settings in Groovy and Kotlin DSL.
// Examples:
//   - rootProject.name = 'my-project'
//   - rootProject.name = "my-project"
//   - rootProject.name("my-project")
var rootProjectNamePattern = regexp.MustCompile(`rootProject\.name\s*[=(]\s*['"]([^'"]+)['"]`)

// extractGradleProjectName tries to determine the project name.
// It looks for settings.gradle or settings.gradle.kts in the same directory.
func extractGradleProjectName(buildFilePath string) string {
	dir := filepath.Dir(buildFilePath)

	// Try settings.gradle (Groovy DSL)
	if name := readSettingsGradleName(filepath.Join(dir, "settings.gradle")); name != "" {
		return name
	}

	// Try settings.gradle.kts (Kotlin DSL)
	if name := readSettingsGradleName(filepath.Join(dir, "settings.gradle.kts")); name != "" {
		return name
	}

	// Fall back to directory name
	dirName := filepath.Base(dir)
	if dirName != "" && dirName != "." && dirName != "/" {
		return dirName
	}

	return "gradle-project"
}

// readSettingsGradleName extracts the rootProject.name from a settings file.
func readSettingsGradleName(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}

	if m := rootProjectNamePattern.FindSubmatch(data); len(m) > 1 {
		return string(m[1])
	}

	return ""
}

// extractGradleJavaVersion extracts the Java version from build.gradle content.
// Looks for sourceCompatibility, targetCompatibility, or toolchain languageVersion.
func extractGradleJavaVersion(content string) string {
	// Try toolchain first (modern approach)
	if m := javaToolchainPattern.FindStringSubmatch(content); len(m) > 1 {
		return m[1]
	}

	// Try sourceCompatibility/targetCompatibility
	if m := javaCompatibilityPattern.FindStringSubmatch(content); len(m) > 1 {
		version := m[1]
		// Normalize "1.8" to "8"
		if strings.HasPrefix(version, "1.") && len(version) >= 3 {
			return version[2:]
		}
		return version
	}

	return ""
}
