package php

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"

	"github.com/matzehuels/stacktower/pkg/core/dag"
	"github.com/matzehuels/stacktower/pkg/core/deps"
	"github.com/matzehuels/stacktower/pkg/observability"
)

// phpVersionRE extracts version from constraints like ">=8.1", "^8.0", "~8.2"
var phpVersionRE = regexp.MustCompile(`[>=^~]*\s*(\d+(?:\.\d+)*)`)

// extractPHPVersion extracts the minimum PHP version from a require.php constraint.
// Examples: ">=8.1" → "8.1", "^8.0" → "8.0", ">=8.1,<9.0" → "8.1"
func extractPHPVersion(constraint string) string {
	if constraint == "" {
		return ""
	}
	// Take the first constraint in case of comma-separated or space-separated
	constraint = strings.ReplaceAll(constraint, " ", ",")
	parts := strings.Split(constraint, ",")
	if len(parts) > 0 {
		if m := phpVersionRE.FindStringSubmatch(parts[0]); len(m) > 1 {
			return m[1]
		}
	}
	return ""
}

// ComposerJSON parses composer.json files. It extracts direct and dev
// dependencies and optionally resolves them via Packagist.
type ComposerJSON struct {
	resolver deps.Resolver
}

func (c *ComposerJSON) Type() string              { return "composer.json" }
func (c *ComposerJSON) IncludesTransitive() bool  { return c.resolver != nil }
func (c *ComposerJSON) Supports(name string) bool { return strings.EqualFold(name, "composer.json") }

func (c *ComposerJSON) Parse(path string, opts deps.Options) (*deps.ManifestResult, error) {
	opts = opts.WithDefaults()

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var comp composerFile
	if err := json.Unmarshal(data, &comp); err != nil {
		return nil, err
	}

	directDeps := extractComposerDepsWithVersions(comp, opts.DependencyScope)

	// Emit observability hooks for extracted dependencies
	hooks := observability.ResolverFromContext(opts.Ctx)
	for _, dep := range directDeps {
		hooks.OnFetchStart(opts.Ctx, dep.Name, 0)
		hooks.OnFetchComplete(opts.Ctx, dep.Name, 0, 0, nil)
	}

	var g *dag.DAG
	if c.resolver != nil {
		g, err = deps.ResolveAndMerge(opts.Ctx, c.resolver, directDeps, opts)
		if err != nil {
			return nil, err
		}
	} else {
		g = deps.ShallowGraphFromDeps(directDeps)
	}

	if comp.Name != "" {
		if root, ok := g.Node(deps.ProjectRootNodeID); ok {
			root.Meta["version"] = comp.Version
		}
	}

	// Extract PHP version constraint from require.php
	phpConstraint := comp.Require["php"]

	return &deps.ManifestResult{
		Graph:              g,
		Type:               c.Type(),
		IncludesTransitive: c.resolver != nil,
		RootPackage:        comp.Name,
		RuntimeVersion:     extractPHPVersion(phpConstraint),
		RuntimeConstraint:  phpConstraint,
	}, nil
}

// extractComposerDepsWithVersions extracts dependencies with version constraints
func extractComposerDepsWithVersions(comp composerFile, scope string) []deps.Dependency {
	var result []deps.Dependency
	for name, constraint := range comp.Require {
		if isPHPRequirement(name) {
			continue
		}
		result = append(result, deps.Dependency{Name: name, Constraint: constraint})
	}
	if scope == deps.DependencyScopeAll {
		for name, constraint := range comp.RequireDev {
			if isPHPRequirement(name) {
				continue
			}
			result = append(result, deps.Dependency{Name: name, Constraint: constraint})
		}
	}
	return result
}

func isPHPRequirement(name string) bool {
	return name == "php" || strings.HasPrefix(name, "php-") || strings.HasPrefix(name, "ext-")
}

type composerFile struct {
	Name       string            `json:"name"`
	Version    string            `json:"version"`
	Require    map[string]string `json:"require"`
	RequireDev map[string]string `json:"require-dev"`
}
