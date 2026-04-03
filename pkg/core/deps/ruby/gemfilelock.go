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

// GemfileLock parses Gemfile.lock files. It provides a full transitive
// closure of the dependency graph without needing to contact a registry.
type GemfileLock struct{}

func (g *GemfileLock) Type() string              { return "Gemfile.lock" }
func (g *GemfileLock) IncludesTransitive() bool  { return true }
func (g *GemfileLock) Supports(name string) bool { return name == "Gemfile.lock" }

func (gl *GemfileLock) Parse(path string, opts deps.Options) (*deps.ManifestResult, error) {
	opts = opts.WithDefaults()

	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	lock := parseGemfileLock(f)
	g := buildGemfileLockGraph(lock, opts)
	deps.EnrichGraph(opts.Ctx, g, "Gemfile", opts)

	return &deps.ManifestResult{
		Graph:              g,
		Type:               gl.Type(),
		IncludesTransitive: true,
		RootPackage:        extractGemspecName(filepath.Dir(path)),
		RuntimeVersion:     lock.rubyVersion,
		RuntimeConstraint:  lock.rubyVersion, // Lock files have exact versions
	}, nil
}

// gemfileLockData holds parsed Gemfile.lock data
type gemfileLockData struct {
	gems         map[string]*gemLockEntry
	dependencies []string // Direct dependencies from DEPENDENCIES section
	rubyVersion  string   // Ruby version from RUBY VERSION section
}

// gemLockEntry represents a gem entry in the specs section
type gemLockEntry struct {
	name         string
	version      string
	dependencies []gemLockDep
}

// gemLockDep represents a dependency within a gem entry
type gemLockDep struct {
	name       string
	constraint string
}

// Regex patterns for parsing Gemfile.lock
var (
	// Matches gem spec line: "    gem-name (1.2.3)"
	gemSpecPattern = regexp.MustCompile(`^    ([a-zA-Z0-9_-]+(?:-[a-zA-Z0-9_-]+)*)\s+\(([^)]+)\)`)
	// Matches dependency line: "      dep-name (~> 1.0)"
	gemDepPattern = regexp.MustCompile(`^      ([a-zA-Z0-9_-]+(?:-[a-zA-Z0-9_-]+)*)\s*(?:\(([^)]+)\))?`)
	// Matches direct dependency in DEPENDENCIES section: "  gem-name (~> 1.0)" or "  gem-name"
	directDepPattern = regexp.MustCompile(`^  ([a-zA-Z0-9_-]+(?:-[a-zA-Z0-9_-]+)*)`)
	// Matches Ruby version line: "   ruby 3.2.0p0" or "   ruby 3.2.0"
	rubyVersionLockPattern = regexp.MustCompile(`^\s+ruby\s+(\d+\.\d+(?:\.\d+)?(?:p\d+)?)`)
)

func parseGemfileLock(f *os.File) *gemfileLockData {
	lock := &gemfileLockData{
		gems: make(map[string]*gemLockEntry),
	}

	scanner := bufio.NewScanner(f)
	var currentSection string
	var currentGem *gemLockEntry
	var inSpecs bool

	for scanner.Scan() {
		line := scanner.Text()

		// Detect section headers
		if !strings.HasPrefix(line, " ") {
			trimmed := strings.TrimSpace(line)
			switch trimmed {
			case "GEM", "PATH", "GIT":
				currentSection = "SOURCE"
				inSpecs = false
			case "PLATFORMS", "RUBY VERSION", "BUNDLED WITH", "CHECKSUMS":
				currentSection = trimmed
				inSpecs = false
			case "DEPENDENCIES":
				currentSection = "DEPENDENCIES"
				inSpecs = false
			default:
				// Could be empty line or other section
				currentSection = ""
				inSpecs = false
			}
			continue
		}

		switch currentSection {
		case "SOURCE":
			// Check for specs: subsection
			if strings.TrimSpace(line) == "specs:" {
				inSpecs = true
				continue
			}

			if !inSpecs {
				continue
			}

			// Try to match a gem spec line (4 spaces indent)
			if match := gemSpecPattern.FindStringSubmatch(line); len(match) > 2 {
				currentGem = &gemLockEntry{
					name:    match[1],
					version: match[2],
				}
				lock.gems[currentGem.name] = currentGem
				continue
			}

			// Try to match a dependency line (6 spaces indent)
			if currentGem != nil {
				if match := gemDepPattern.FindStringSubmatch(line); len(match) > 1 {
					dep := gemLockDep{name: match[1]}
					if len(match) > 2 && match[2] != "" {
						dep.constraint = match[2]
					}
					currentGem.dependencies = append(currentGem.dependencies, dep)
				}
			}

		case "DEPENDENCIES":
			if match := directDepPattern.FindStringSubmatch(line); len(match) > 1 {
				// Capture direct dependency name (without version constraint for simplicity)
				lock.dependencies = append(lock.dependencies, match[1])
			}
		case "RUBY VERSION":
			if match := rubyVersionLockPattern.FindStringSubmatch(line); len(match) > 1 {
				lock.rubyVersion = match[1]
			}
		}
	}

	return lock
}

func buildGemfileLockGraph(lock *gemfileLockData, opts deps.Options) *dag.DAG {
	g := dag.New(nil)
	hooks := observability.ResolverFromContext(opts.Ctx)

	// First pass: add all gem nodes
	for _, gem := range lock.gems {
		hooks.OnFetchStart(opts.Ctx, gem.name, 0)
		meta := dag.Metadata{"version": gem.version}
		_ = g.AddNode(dag.Node{ID: gem.name, Meta: meta})
		hooks.OnFetchComplete(opts.Ctx, gem.name, 0, len(gem.dependencies), nil)
	}

	// Second pass: add dependency edges
	incoming := make(map[string]bool)
	for _, gem := range lock.gems {
		for _, dep := range gem.dependencies {
			// Only add edge if the dependency exists in the lockfile
			if _, ok := lock.gems[dep.name]; ok {
				edgeMeta := dag.Metadata{}
				if dep.constraint != "" {
					edgeMeta["constraint"] = dep.constraint
				}
				_ = g.AddEdge(dag.Edge{From: gem.name, To: dep.name, Meta: edgeMeta})
				incoming[dep.name] = true
			}
		}
	}

	// Add virtual root
	_ = g.AddNode(dag.Node{ID: deps.ProjectRootNodeID, Meta: dag.Metadata{"virtual": true}})

	// Connect direct dependencies to root
	if len(lock.dependencies) > 0 {
		for _, dep := range lock.dependencies {
			if gem, ok := lock.gems[dep]; ok {
				edgeMeta := dag.Metadata{}
				if gem.version != "" {
					edgeMeta["constraint"] = "==" + gem.version
				}
				_ = g.AddEdge(dag.Edge{From: deps.ProjectRootNodeID, To: dep, Meta: edgeMeta})
			}
		}
	} else {
		// Fall back to gems with no incoming edges
		for name, gem := range lock.gems {
			if !incoming[name] {
				edgeMeta := dag.Metadata{}
				if gem.version != "" {
					edgeMeta["constraint"] = "==" + gem.version
				}
				_ = g.AddEdge(dag.Edge{From: deps.ProjectRootNodeID, To: name, Meta: edgeMeta})
			}
		}
	}

	return g
}
