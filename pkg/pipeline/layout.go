package pipeline

import (
	"github.com/stacktower-io/stacktower/pkg/core/dag"
	"github.com/stacktower-io/stacktower/pkg/core/render/nodelink"
	"github.com/stacktower-io/stacktower/pkg/core/render/tower/feature"
	"github.com/stacktower-io/stacktower/pkg/core/render/tower/layout"
	"github.com/stacktower-io/stacktower/pkg/core/render/tower/transform"
	"github.com/stacktower-io/stacktower/pkg/graph"
)

// =============================================================================
// Layout Generation
// =============================================================================

// GenerateLayout generates a complete layout for any visualization type.
// This is the unified entry point for generating serializable layout data.
//
// Both tower and nodelink layouts include:
//   - Graph structure (nodes, edges, rows)
//   - Nebraska rankings (maintainer data)
//   - Visualization-specific data (blocks for tower, DOT for nodelink)
func GenerateLayout(g *dag.DAG, opts Options) (graph.Layout, error) {
	if opts.IsNodelink() {
		return generateNodelinkLayout(g, opts)
	}
	return generateTowerLayout(g, opts)
}

// =============================================================================
// Tower
// =============================================================================

// generateTowerLayout generates a complete tower layout.
// Computes positions, applies transforms, and includes Nebraska rankings.
//
// Note: Nebraska rankings are ALWAYS computed and stored, regardless of opts.Nebraska.
// The opts.Nebraska flag only controls whether the ranking panel is rendered in the SVG.
func generateTowerLayout(g *dag.DAG, opts Options) (graph.Layout, error) {
	// Ensure graph has row assignments
	workGraph := g
	if g.MaxRow() == 0 && g.EdgeCount() > 0 {
		workGraph = g.Clone()
		layout.EnsureLayered(workGraph)
	}

	// Build layout options
	var layoutOpts []layout.Option
	if opts.Orderer != nil {
		layoutOpts = append(layoutOpts, layout.WithOrderer(opts.Orderer))
	}

	// Compute base layout
	l := layout.Build(workGraph, opts.Width, opts.Height, layoutOpts...)

	// Apply transforms
	if opts.Merge {
		l = transform.MergeSubdividers(l, workGraph)
	}
	if opts.Randomize {
		l = transform.Randomize(l, workGraph, opts.Seed, nil)
	}

	// Set metadata
	l.Style = opts.Style
	l.Seed = opts.Seed
	l.Randomize = opts.Randomize
	l.Merged = opts.Merge

	// Compute Nebraska rankings
	l.Nebraska = feature.RankNebraska(workGraph, 10)

	// Export to serialization format
	exported, err := l.Export(workGraph)
	if err != nil {
		return exported, err
	}

	// Record the crossing count on the serialized layout so cache consumers
	// and diff tooling can read it without re-running the ordering step.
	if len(exported.Rows) > 0 && len(exported.Edges) > 0 {
		exported.Crossings = dag.CountCrossings(workGraph, exported.Rows)
	}

	return exported, nil
}

// =============================================================================
// Nodelink
// =============================================================================

// generateNodelinkLayout generates a complete nodelink layout.
// Includes DOT string for Graphviz and Nebraska rankings.
//
// Note: Nebraska rankings are ALWAYS computed and stored, regardless of opts.Nebraska.
// The opts.Nebraska flag only controls whether the ranking panel is rendered in the SVG.
func generateNodelinkLayout(g *dag.DAG, opts Options) (graph.Layout, error) {
	// Generate DOT representation
	dot := nodelink.ToDOT(g, nodelink.Options{Detailed: false})

	// Build base layout
	result, err := nodelink.Export(dot, g, nodelink.Options{Detailed: false}, opts.Width, opts.Height, opts.Style)
	if err != nil {
		return result, err
	}

	// Add Nebraska rankings
	result.Nebraska = exportNebraska(feature.RankNebraska(g, 10))

	return result, nil
}

// =============================================================================
// Helpers
// =============================================================================

// exportNebraska converts internal Nebraska rankings to the serialization format.
func exportNebraska(rankings []feature.NebraskaRanking) []graph.NebraskaRanking {
	result := make([]graph.NebraskaRanking, len(rankings))
	for i, r := range rankings {
		pkgs := make([]graph.NebraskaPackage, len(r.Packages))
		for j, p := range r.Packages {
			pkgs[j] = graph.NebraskaPackage{
				Package: p.Package,
				Role:    string(p.Role),
				URL:     p.URL,
			}
		}
		result[i] = graph.NebraskaRanking{
			Maintainer: r.Maintainer,
			Score:      r.Score,
			Packages:   pkgs,
		}
	}
	return result
}
