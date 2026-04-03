package php

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/matzehuels/stacktower/pkg/core/dag"
	"github.com/matzehuels/stacktower/pkg/core/deps"
	"github.com/matzehuels/stacktower/pkg/observability"
)

// ComposerLock parses composer.lock files. It provides a full transitive
// closure of the dependency graph without needing to contact a registry.
type ComposerLock struct{}

func (c *ComposerLock) Type() string              { return "composer.lock" }
func (c *ComposerLock) IncludesTransitive() bool  { return true }
func (c *ComposerLock) Supports(name string) bool { return strings.EqualFold(name, "composer.lock") }

func (c *ComposerLock) Parse(path string, opts deps.Options) (*deps.ManifestResult, error) {
	opts = opts.WithDefaults()

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var lock composerLockFile
	if err := json.Unmarshal(data, &lock); err != nil {
		return nil, err
	}

	g := buildComposerLockGraph(lock, opts)
	deps.EnrichGraph(opts.Ctx, g, "composer.json", opts)

	// Extract PHP version from lock's platform field or fallback to composer.json
	phpConstraint := lock.Platform["php"]
	if phpConstraint == "" {
		phpConstraint = extractComposerJSONPHP(filepath.Dir(path))
	}

	return &deps.ManifestResult{
		Graph:              g,
		Type:               c.Type(),
		IncludesTransitive: true,
		RootPackage:        extractComposerJSONName(filepath.Dir(path)),
		RuntimeVersion:     extractPHPVersion(phpConstraint),
		RuntimeConstraint:  phpConstraint,
	}, nil
}

// extractComposerJSONName reads the package name from composer.json in the same directory.
func extractComposerJSONName(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, "composer.json"))
	if err != nil {
		return ""
	}
	var comp struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(data, &comp); err != nil {
		return ""
	}
	return comp.Name
}

// extractComposerJSONPHP reads the PHP version constraint from composer.json in the same directory.
func extractComposerJSONPHP(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, "composer.json"))
	if err != nil {
		return ""
	}
	var comp struct {
		Require map[string]string `json:"require"`
	}
	if err := json.Unmarshal(data, &comp); err != nil {
		return ""
	}
	return comp.Require["php"]
}

// composerLockFile represents the composer.lock file structure
type composerLockFile struct {
	Packages    []composerLockPackage `json:"packages"`
	PackagesDev []composerLockPackage `json:"packages-dev"`
	Platform    map[string]string     `json:"platform"`
}

// composerLockPackage represents a package entry in composer.lock
type composerLockPackage struct {
	Name        string            `json:"name"`
	Version     string            `json:"version"`
	Description string            `json:"description"`
	License     []string          `json:"license"`
	Source      map[string]string `json:"source"`
	Dist        map[string]string `json:"dist"`
	Require     map[string]string `json:"require"`
	RequireDev  map[string]string `json:"require-dev"`
	Type        string            `json:"type"`
}

func buildComposerLockGraph(lock composerLockFile, opts deps.Options) *dag.DAG {
	g := dag.New(nil)
	hooks := observability.ResolverFromContext(opts.Ctx)

	// Collect all packages (production + dev)
	allPackages := make(map[string]*composerLockPackage)
	devPackages := make(map[string]bool)

	for i := range lock.Packages {
		pkg := &lock.Packages[i]
		allPackages[pkg.Name] = pkg
	}
	if opts.DependencyScope == deps.DependencyScopeAll {
		for i := range lock.PackagesDev {
			pkg := &lock.PackagesDev[i]
			if _, exists := allPackages[pkg.Name]; !exists {
				allPackages[pkg.Name] = pkg
				devPackages[pkg.Name] = true
			}
		}
	}

	// First pass: add all package nodes
	for _, pkg := range allPackages {
		hooks.OnFetchStart(opts.Ctx, pkg.Name, 0)
		meta := dag.Metadata{"version": normalizeComposerVersion(pkg.Version)}
		if pkg.Description != "" {
			meta["description"] = pkg.Description
		}
		if len(pkg.License) > 0 {
			meta["license"] = strings.Join(pkg.License, ", ")
		}
		if devPackages[pkg.Name] {
			meta["dev"] = true
		}
		_ = g.AddNode(dag.Node{ID: pkg.Name, Meta: meta})
		hooks.OnFetchComplete(opts.Ctx, pkg.Name, 0, len(pkg.Require), nil)
	}

	// Second pass: add dependency edges
	incoming := make(map[string]bool)
	for _, pkg := range allPackages {
		for depName, constraint := range pkg.Require {
			// Skip PHP and extension requirements
			if isPHPRequirement(depName) {
				continue
			}
			// Only add edge if the dependency exists in the lockfile
			if _, ok := allPackages[depName]; ok {
				edgeMeta := dag.Metadata{}
				if constraint != "" {
					edgeMeta["constraint"] = constraint
				}
				_ = g.AddEdge(dag.Edge{From: pkg.Name, To: depName, Meta: edgeMeta})
				incoming[depName] = true
			}
		}
	}

	// Add virtual root
	_ = g.AddNode(dag.Node{ID: deps.ProjectRootNodeID, Meta: dag.Metadata{"virtual": true}})

	// Connect packages with no incoming edges to root (these are direct dependencies)
	for name, pkg := range allPackages {
		if !incoming[name] {
			edgeMeta := dag.Metadata{}
			if pkg.Version != "" {
				edgeMeta["constraint"] = "==" + normalizeComposerVersion(pkg.Version)
			}
			_ = g.AddEdge(dag.Edge{From: deps.ProjectRootNodeID, To: name, Meta: edgeMeta})
		}
	}

	return g
}

// normalizeComposerVersion removes the 'v' prefix if present.
// composer.lock often has versions like "v1.2.3" but we want "1.2.3" for consistency.
func normalizeComposerVersion(version string) string {
	if strings.HasPrefix(version, "v") && len(version) > 1 {
		next := version[1]
		// Only strip 'v' if followed by a digit
		if next >= '0' && next <= '9' {
			return version[1:]
		}
	}
	return version
}
