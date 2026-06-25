package python

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

// depRE captures package name and optional version constraint
// Examples: "requests", "requests>=2.28.0", "numpy==1.24.0", "flask~=2.0"
var depRE = regexp.MustCompile(`^([a-zA-Z0-9][-a-zA-Z0-9._]*)\s*(.*)`)

// Requirements parses requirements.txt files. By default, it only provides
// direct dependencies. If a [deps.Resolver] is provided, it can resolve
// the full transitive closure.
type Requirements struct {
	resolver deps.Resolver
}

func (r *Requirements) Type() string             { return "requirements.txt" }
func (r *Requirements) IncludesTransitive() bool { return r.resolver != nil }

func (r *Requirements) Supports(name string) bool {
	return name == "requirements.txt" ||
		(strings.HasPrefix(name, "requirements") && strings.HasSuffix(name, ".txt"))
}

func (r *Requirements) Parse(path string, opts deps.Options) (*deps.ManifestResult, error) {
	opts = opts.WithDefaults()

	dependencies, err := parseRequirementsFile(path)
	if err != nil {
		return nil, err
	}

	// Emit observability hooks for parsed dependencies
	hooks := observability.ResolverFromContext(opts.Ctx)
	for _, dep := range dependencies {
		hooks.OnFetchStart(opts.Ctx, dep.Name, 0)
		hooks.OnFetchComplete(opts.Ctx, dep.Name, 0, 0, nil)
	}

	var g *dag.DAG
	if r.resolver != nil {
		g, err = deps.ResolveAndMerge(opts.Ctx, r.resolver, dependencies, opts)
		if err != nil {
			return nil, err
		}
	} else {
		g = deps.ShallowGraphFromDeps(dependencies)
	}

	rootPackage := extractPyprojectName(filepath.Dir(path))

	return &deps.ManifestResult{
		Graph:              g,
		Type:               r.Type(),
		IncludesTransitive: r.resolver != nil,
		RootPackage:        rootPackage,
	}, nil
}

// maxRequirementsIncludeDepth caps how deeply nested "-r other.txt" includes
// are followed, guarding against pathological include chains.
const maxRequirementsIncludeDepth = 10

// parseRequirementsFile parses a requirements.txt file and returns dependencies
// with their version constraints. Backslash line continuations are joined
// before parsing, and "-r"/"--requirement" includes are followed relative to
// the including file's directory (with cycle detection and a depth cap).
func parseRequirementsFile(path string) ([]deps.Dependency, error) {
	seen := make(map[string]bool)
	visited := make(map[string]bool)
	var result []deps.Dependency
	if err := parseRequirementsInto(path, seen, visited, 0, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// parseRequirementsInto appends dependencies from path into result.
// seen deduplicates package names across all included files; visited guards
// against include cycles (keyed by absolute path).
func parseRequirementsInto(path string, seen, visited map[string]bool, depth int, result *[]deps.Dependency) error {
	if depth > maxRequirementsIncludeDepth {
		return nil
	}
	abs, err := filepath.Abs(path)
	if err == nil {
		if visited[abs] {
			return nil
		}
		visited[abs] = true
	}

	f, err := os.Open(path)
	if err != nil {
		// Included files (depth > 0) may be missing; don't fail the whole
		// parse for a broken include, only for the top-level file.
		if depth > 0 {
			return nil
		}
		return err
	}
	defer f.Close()

	dir := filepath.Dir(path)

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Join backslash line continuations into a single logical line.
		for strings.HasSuffix(line, `\`) && scanner.Scan() {
			line = strings.TrimSpace(strings.TrimSuffix(line, `\`)) + " " + strings.TrimSpace(scanner.Text())
		}

		if line == "" || line[0] == '#' {
			continue
		}
		if line[0] == '-' {
			// Follow "-r other.txt" / "--requirement other.txt" includes.
			if include := requirementsIncludePath(line); include != "" {
				includePath := include
				if !filepath.IsAbs(includePath) {
					includePath = filepath.Join(dir, includePath)
				}
				if err := parseRequirementsInto(includePath, seen, visited, depth+1, result); err != nil {
					return err
				}
			}
			// Editable installs ("-e ./path", "-e git+...") reference local
			// or VCS sources we can't resolve against a registry; they are
			// skipped, as are all other "-"-prefixed pip flags.
			continue
		}
		if strings.Contains(line, "://") || strings.HasPrefix(line, "git+") {
			continue
		}
		if m := depRE.FindStringSubmatch(line); len(m) > 1 {
			name := normalize(m[1])
			if !seen[name] {
				seen[name] = true
				dep := deps.Dependency{Name: name}
				// Capture version constraint if present
				if len(m) > 2 && m[2] != "" {
					constraint := strings.TrimSpace(m[2])
					// Remove environment markers (everything after ;)
					if idx := strings.Index(constraint, ";"); idx != -1 {
						constraint = strings.TrimSpace(constraint[:idx])
					}
					dep.Constraint = constraint
				}
				*result = append(*result, dep)
			}
		}
	}

	return scanner.Err()
}

// requirementsIncludePath extracts the referenced file from a
// "-r file" / "--requirement file" (or "=file") line. Returns "" when the
// line is not an include directive.
func requirementsIncludePath(line string) string {
	for _, prefix := range []string{"-r", "--requirement"} {
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		rest := line[len(prefix):]
		if rest == "" {
			return ""
		}
		// Require a separator so "-rfoo" or "--requirements" don't match.
		if rest[0] != ' ' && rest[0] != '\t' && rest[0] != '=' {
			continue
		}
		return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(rest), "="))
	}
	return ""
}
