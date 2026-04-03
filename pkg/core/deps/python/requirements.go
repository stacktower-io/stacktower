package python

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

// parseRequirementsFile parses a requirements.txt file and returns dependencies
// with their version constraints.
func parseRequirementsFile(path string) ([]deps.Dependency, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	seen := make(map[string]bool)
	var result []deps.Dependency

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || line[0] == '#' || line[0] == '-' {
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
				result = append(result, dep)
			}
		}
	}

	return result, scanner.Err()
}
