// Package pkg provides the core libraries for Stacktower dependency visualization.
//
// # Overview
//
// Stacktower transforms dependency trees into visual tower diagrams where packages
// rest on what they depend on—inspired by XKCD #2347 ("Dependency"). The pkg
// directory is organized into these areas:
//
//  1. [core] - Domain logic (dependency resolution, graph structures, rendering)
//  2. [integrations] - External API clients (PyPI, npm, GitHub, etc.)
//  3. [pipeline] - Orchestration (parse → layout → render)
//  4. [graph] - Serialization types for graphs and layouts
//  5. [cache] - Caching interfaces and implementations
//  6. [security] - Vulnerability scanning and license analysis
//  7. [sbom] - SBOM generation (CycloneDX, SPDX)
//  8. [errors] - Structured error types and codes
//  9. [observability] - Pipeline and security event hooks
//  10. [buildinfo] - Build-time version metadata
//  11. [fonts] - Embedded font data for SVG rendering
//  12. [session] - GitHub session and credential storage
//
// # Architecture
//
// The typical data flow through Stacktower:
//
//	Package Registry/Manifest
//	         ↓
//	    [core/deps] package (resolve dependencies)
//	         ↓
//	    [core/dag] package (graph structure + transformations)
//	         ↓
//	    [core/render] package (layout + visualization)
//	         ↓
//	    SVG/PDF/PNG/JSON output
//
// # Quick Start
//
// Resolve dependencies and render a tower visualization:
//
//	import (
//	    "context"
//	    "github.com/stacktower-io/stacktower/pkg/core/deps/python"
//	    "github.com/stacktower-io/stacktower/pkg/core/dag/transform"
//	    "github.com/stacktower-io/stacktower/pkg/core/render/tower/layout"
//	    "github.com/stacktower-io/stacktower/pkg/core/render/tower/sink"
//	)
//
//	// 1. Resolve dependencies
//	resolver, _ := python.Language.Resolver()
//	g, _ := resolver.Resolve(context.Background(), "fastapi", deps.Options{
//	    MaxDepth: 10,
//	    MaxNodes: 1000,
//	})
//
//	// 2. Transform the graph
//	_, _ = transform.Normalize(g)
//
//	// 3. Compute layout
//	l := layout.Build(g, 1200, 800)
//
//	// 4. Render to SVG
//	svg := sink.RenderSVG(l, sink.WithGraph(g))
//
// # Main Packages
//
// ## Core Domain Logic
//
// [core/deps] - Dependency resolution supporting 7 languages (Python, Rust,
// JavaScript, Go, Ruby, PHP, Java). Each language has its own subpackage with
// manifest parsers and registry resolvers.
//
// [core/dag] - Directed acyclic graph optimized for row-based layered layouts.
// Nodes are organized into horizontal rows with edges connecting consecutive
// rows only. Supports regular, subdivider, and auxiliary node types.
//
// [core/render] - Visualization rendering with two output formats: tower
// (stacked blocks) and nodelink (traditional directed graphs).
//
// ## External Integrations
//
// [integrations] - HTTP clients for package registries (PyPI, npm, crates.io,
// RubyGems, Packagist, Maven, Go proxy) and code hosts (GitHub, GitLab).
//
// ## Graph Transformations
//
// [dag/transform] - Graph transformations: transitive reduction, layering,
// edge subdivision, and span overlap resolution. [transform.Normalize] runs
// the complete pipeline.
//
// [dag/perm] - Permutation algorithms including PQ-trees for efficiently
// generating valid orderings with partial ordering constraints.
//
// ## Visualization
//
// [render/tower] - Stacktower's signature tower visualization. The rendering
// pipeline: ordering → layout → transform → sink.
//
//   - [render/tower/ordering]: Minimize edge crossings (barycentric, optimal)
//   - [render/tower/layout]: Compute block positions and dimensions
//   - [render/tower/transform]: Post-layout (merge subdividers, randomize widths)
//   - [render/tower/sink]: Output formats (SVG, PDF, PNG, JSON)
//   - [render/tower/styles]: Visual styles (simple, hand-drawn)
//   - [render/tower/feature]: Analysis (Nebraska ranking, brittle detection)
//
// [render/nodelink] - Traditional directed graph diagrams using Graphviz.
//
// [render] - Top-level utilities for format conversion (SVG to PDF/PNG).
//
// ## Serialization
//
// [graph] - Serialization types for graphs and layouts (JSON node-link format).
//
// ## Orchestration
//
// [pipeline] - Complete visualization pipeline (parse → layout → render) used
// by CLI and API. Ensures consistent behavior across all entry points.
//
// ## Compliance & Security
//
// [sbom] - SBOM generation in CycloneDX and SPDX formats, including package
// identifiers (purls), license data, and vulnerability findings.
//
// ## Infrastructure
//
// [errors] - Structured error types with machine-readable codes for CLI and API.
//
// [observability] - Hook interfaces for pipeline and security lifecycle events
// (resolution progress, ordering, vulnerability scanning).
//
// [buildinfo] - Build-time version, commit, and date metadata populated via ldflags.
//
// [fonts] - Embedded font data (Gaegu) used by the hand-drawn SVG renderer.
//
// [session] - GitHub OAuth session storage and credential management.
//
// # Common Workflows
//
// Parse a manifest file:
//
//	parser := python.PoetryLock{}
//	result, _ := parser.Parse("poetry.lock", deps.Options{})
//	g := result.Graph
//
// Enrich with GitHub metadata:
//
//	provider, _ := metadata.NewGitHub(token, 24*time.Hour)
//	opts := deps.Options{MetadataProviders: []deps.MetadataProvider{provider}}
//	g, _ := resolver.Resolve(ctx, "fastapi", opts)
//
// Render with custom style:
//
//	l := layout.Build(g, 1200, 800)
//	style := handdrawn.New(42)
//	svg := sink.RenderSVG(l, sink.WithGraph(g), sink.WithStyle(style))
//
// Analyze maintainer risk:
//
//	rankings := feature.RankNebraska(g, 10)
//	for _, n := range g.Nodes() {
//	    if feature.IsBrittle(n) {
//	        fmt.Printf("Warning: %s may be unmaintained\n", n.ID)
//	    }
//	}
//
// # Testing
//
// Run tests:
//
//	go test ./pkg/...                    # All tests
//	go test ./pkg/core/dag/...           # Specific package
//	go test -run Example                 # Examples only
//	go test -tags integration ./pkg/...  # Include integration tests
//
// [core]: https://pkg.go.dev/github.com/stacktower-io/stacktower/pkg/core
// [core/deps]: https://pkg.go.dev/github.com/stacktower-io/stacktower/pkg/core/deps
// [core/dag]: https://pkg.go.dev/github.com/stacktower-io/stacktower/pkg/core/dag
// [core/render]: https://pkg.go.dev/github.com/stacktower-io/stacktower/pkg/core/render
// [integrations]: https://pkg.go.dev/github.com/stacktower-io/stacktower/pkg/integrations
// [dag/transform]: https://pkg.go.dev/github.com/stacktower-io/stacktower/pkg/core/dag/transform
// [dag/perm]: https://pkg.go.dev/github.com/stacktower-io/stacktower/pkg/core/dag/perm
// [render]: https://pkg.go.dev/github.com/stacktower-io/stacktower/pkg/core/render
// [render/tower]: https://pkg.go.dev/github.com/stacktower-io/stacktower/pkg/core/render/tower
// [render/tower/ordering]: https://pkg.go.dev/github.com/stacktower-io/stacktower/pkg/core/render/tower/ordering
// [render/tower/layout]: https://pkg.go.dev/github.com/stacktower-io/stacktower/pkg/core/render/tower/layout
// [render/tower/transform]: https://pkg.go.dev/github.com/stacktower-io/stacktower/pkg/core/render/tower/transform
// [render/tower/sink]: https://pkg.go.dev/github.com/stacktower-io/stacktower/pkg/core/render/tower/sink
// [render/tower/styles]: https://pkg.go.dev/github.com/stacktower-io/stacktower/pkg/core/render/tower/styles
// [render/tower/feature]: https://pkg.go.dev/github.com/stacktower-io/stacktower/pkg/core/render/tower/feature
// [render/nodelink]: https://pkg.go.dev/github.com/stacktower-io/stacktower/pkg/core/render/nodelink
// [graph]: https://pkg.go.dev/github.com/stacktower-io/stacktower/pkg/graph
// [pipeline]: https://pkg.go.dev/github.com/stacktower-io/stacktower/pkg/pipeline
// [cache]: https://pkg.go.dev/github.com/stacktower-io/stacktower/pkg/cache
// [security]: https://pkg.go.dev/github.com/stacktower-io/stacktower/pkg/security
// [sbom]: https://pkg.go.dev/github.com/stacktower-io/stacktower/pkg/sbom
// [errors]: https://pkg.go.dev/github.com/stacktower-io/stacktower/pkg/errors
// [observability]: https://pkg.go.dev/github.com/stacktower-io/stacktower/pkg/observability
// [buildinfo]: https://pkg.go.dev/github.com/stacktower-io/stacktower/pkg/buildinfo
// [fonts]: https://pkg.go.dev/github.com/stacktower-io/stacktower/pkg/fonts
// [session]: https://pkg.go.dev/github.com/stacktower-io/stacktower/pkg/session
//
// [deps]: https://pkg.go.dev/github.com/stacktower-io/stacktower/pkg/core/deps
// [dag]: https://pkg.go.dev/github.com/stacktower-io/stacktower/pkg/core/dag
// [render/tower/styles/handdrawn]: https://pkg.go.dev/github.com/stacktower-io/stacktower/pkg/core/render/tower/styles/handdrawn
package pkg
