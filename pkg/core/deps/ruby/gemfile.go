package ruby

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/matzehuels/stacktower/pkg/core/dag"
	"github.com/matzehuels/stacktower/pkg/core/deps"
	"github.com/matzehuels/stacktower/pkg/observability"
)

// Gemfile parses Ruby Gemfiles. It extracts gems and optionally resolves
// them via RubyGems.
type Gemfile struct {
	resolver deps.Resolver
}

func (g *Gemfile) Type() string              { return "Gemfile" }
func (g *Gemfile) IncludesTransitive() bool  { return g.resolver != nil }
func (g *Gemfile) Supports(name string) bool { return name == "Gemfile" }

func (gf *Gemfile) Parse(path string, opts deps.Options) (*deps.ManifestResult, error) {
	opts = opts.WithDefaults()

	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	directDeps, rubyVersion := parseGemfileWithVersions(f, opts.DependencyScope)

	// Emit observability hooks for parsed dependencies
	hooks := observability.ResolverFromContext(opts.Ctx)
	for _, dep := range directDeps {
		hooks.OnFetchStart(opts.Ctx, dep.Name, 0)
		hooks.OnFetchComplete(opts.Ctx, dep.Name, 0, 0, nil)
	}

	var g *dag.DAG
	if gf.resolver != nil {
		g, err = deps.ResolveAndMerge(opts.Ctx, gf.resolver, directDeps, opts)
		if err != nil {
			return nil, err
		}
	} else {
		g = deps.ShallowGraphFromDeps(directDeps)
	}

	return &deps.ManifestResult{
		Graph:              g,
		Type:               gf.Type(),
		IncludesTransitive: gf.resolver != nil,
		RootPackage:        extractGemspecName(filepath.Dir(path)),
		RuntimeVersion:     extractRubyVersion(rubyVersion),
		RuntimeConstraint:  rubyVersion,
	}, nil
}

// extractRubyVersion extracts the minimum Ruby version from a constraint.
// Examples: "3.2.0" → "3.2.0", "~> 3.0" → "3.0", ">= 2.7" → "2.7"
func extractRubyVersion(constraint string) string {
	if constraint == "" {
		return ""
	}
	if m := rubyVersionExtractRE.FindStringSubmatch(constraint); len(m) > 1 {
		return m[1]
	}
	return ""
}

var gemPattern = regexp.MustCompile(`^\s*gem\s+['"]([^'"]+)['"]`)
var groupStartPattern = regexp.MustCompile(`^\s*group\s+(.+?)\s+do\s*$`)

// gemWithVersionPattern captures gem name and version constraints
// Examples: gem 'rails', '~> 5.0.0'
//
//	gem 'rack', '>= 1.0', '< 2.0'
var gemWithVersionPattern = regexp.MustCompile(`^\s*gem\s+['"]([^'"]+)['"](?:\s*,\s*['"]([^'"]+)['"])?(?:\s*,\s*['"]([^'"]+)['"])?`)
var gemspecNamePattern = regexp.MustCompile(`\.name\s*=\s*['"]([^'"]+)['"]`)

// rubyVersionPattern captures ruby version from Gemfile
// Examples: ruby '3.2.0'
//
//	ruby "~> 3.0"
//	ruby '>= 2.7'
var rubyVersionPattern = regexp.MustCompile(`^\s*ruby\s+['"]([^'"]+)['"]`)

// rubyVersionExtractRE extracts the minimum version from a ruby constraint
var rubyVersionExtractRE = regexp.MustCompile(`[>=~^]*\s*(\d+(?:\.\d+)*)`)

func extractGemspecName(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".gemspec") {
			data, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				continue
			}
			if m := gemspecNamePattern.FindSubmatch(data); len(m) > 1 {
				return string(m[1])
			}
		}
	}
	return ""
}

// parseGemfileWithVersions parses a Gemfile and extracts gems with version constraints.
// Also returns the ruby version constraint if specified.
func parseGemfileWithVersions(f *os.File, scope string) ([]deps.Dependency, string) {
	var result []deps.Dependency
	var rubyVersion string
	seen := make(map[string]bool)
	excludedGroupDepth := 0

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}

		// Check for ruby version directive
		if m := rubyVersionPattern.FindStringSubmatch(line); len(m) > 1 {
			rubyVersion = m[1]
			continue
		}

		if m := groupStartPattern.FindStringSubmatch(line); len(m) > 1 {
			if scope == deps.DependencyScopeProdOnly && groupContainsDevOrTest(m[1]) {
				excludedGroupDepth++
			}
			continue
		}

		trimmed := strings.TrimSpace(line)
		if trimmed == "end" && excludedGroupDepth > 0 {
			excludedGroupDepth--
			continue
		}

		if match := gemWithVersionPattern.FindStringSubmatch(line); len(match) > 1 {
			if scope == deps.DependencyScopeProdOnly && excludedGroupDepth > 0 {
				continue
			}
			name := match[1]
			if !seen[name] {
				seen[name] = true
				dep := deps.Dependency{Name: name}
				// Combine version constraints if multiple are present
				var constraints []string
				if len(match) > 2 && match[2] != "" {
					constraints = append(constraints, match[2])
				}
				if len(match) > 3 && match[3] != "" {
					constraints = append(constraints, match[3])
				}
				if len(constraints) > 0 {
					dep.Constraint = strings.Join(constraints, ", ")
				}
				result = append(result, dep)
			}
		}
	}

	return result, rubyVersion
}

func groupContainsDevOrTest(raw string) bool {
	for _, part := range strings.Split(raw, ",") {
		g := strings.TrimSpace(strings.TrimPrefix(strings.Trim(part, `'"`), ":"))
		if g == "development" || g == "test" {
			return true
		}
	}
	return false
}

// parseGemfile is kept for backward compatibility
func parseGemfile(f *os.File) []string {
	var gems []string
	seen := make(map[string]bool)

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}

		if match := gemPattern.FindStringSubmatch(line); len(match) > 1 {
			name := match[1]
			if !seen[name] {
				seen[name] = true
				gems = append(gems, name)
			}
		}
	}

	return gems
}
