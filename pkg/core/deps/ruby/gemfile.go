package ruby

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/stacktower-io/stacktower/pkg/core/dag"
	"github.com/stacktower-io/stacktower/pkg/core/deps"
	"github.com/stacktower-io/stacktower/pkg/observability"
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

// gemArgPattern matches one comma-separated argument following the gem name:
// either a quoted string ('~> 5.0') or anything else (hash options like
// github:, git:, path:, require:). Used to collect ALL version constraints,
// not just the first two.
var gemArgPattern = regexp.MustCompile(`^\s*,\s*(?:['"]([^'"]*)['"]|([^,]+))`)

// gemVersionConstraintPattern recognizes quoted args that look like version
// constraints: an optional operator (~>, >=, <=, >, <, =, !=) followed by a
// version number.
var gemVersionConstraintPattern = regexp.MustCompile(`^\s*(?:~>|>=|<=|!=|=|>|<)?\s*\d[\w.]*\s*$`)

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

		if match := gemPattern.FindStringSubmatch(line); len(match) > 1 {
			if scope == deps.DependencyScopeProdOnly && excludedGroupDepth > 0 {
				continue
			}
			name := match[1]
			if !seen[name] {
				seen[name] = true
				dep := deps.Dependency{Name: name}
				// Collect ALL quoted version constraint args after the gem
				// name (e.g. '>= 1.0', '< 2.0', '< 3.0'), stopping at the
				// first non-constraint argument such as hash options
				// (github:, git:, path:, require:). Gems declared only with
				// git:/github:/path: sources still produce a dependency node
				// with no constraint.
				constraints := extractGemConstraints(line[len(match[0]):])
				if len(constraints) > 0 {
					dep.Constraint = strings.Join(constraints, ", ")
				}
				result = append(result, dep)
			}
		}
	}

	return result, rubyVersion
}

// extractGemConstraints parses the argument list that follows a gem name and
// returns the leading run of quoted version constraints. Parsing stops at the
// first argument that is not a quoted version constraint (hash options like
// github:/git:/path:/require:, or quoted values of such options).
func extractGemConstraints(rest string) []string {
	var constraints []string
	for {
		m := gemArgPattern.FindStringSubmatch(rest)
		if m == nil {
			break
		}
		quoted := m[1]
		if quoted == "" || !gemVersionConstraintPattern.MatchString(quoted) {
			break
		}
		constraints = append(constraints, strings.TrimSpace(quoted))
		rest = rest[len(m[0]):]
	}
	return constraints
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
