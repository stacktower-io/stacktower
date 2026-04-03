package transform

import (
	"fmt"

	"github.com/matzehuels/stacktower/pkg/core/dag"
)

// Normalize prepares a DAG for tower rendering by applying a sequence of
// transformations that satisfy the layout's structural constraints.
//
// Normalize modifies g in place and returns transformation metrics. All
// transformations are applied in this specific order:
//
//  1. [BreakCycles]: Remove back-edges to ensure it is a true DAG.
//  2. [TransitiveReduction]: Remove redundant edges to simplify the visual.
//  3. [AssignLayers]: Assign horizontal rows (layers) based on node depth.
//  4. [Subdivide]: Break edges crossing multiple rows into single-row segments.
//  5. [ResolveSpanOverlaps]: Insert separator beams to resolve layout conflicts.
//
// This order is critical: cycles must be broken before transitive reduction,
// layers must be assigned before subdivision, and span overlaps can only be
// detected after edges are subdivided into single-row segments.
//
// To skip specific transformations, use [NormalizeWithOptions].
//
// # Return Value
//
// Normalize returns a [TransformResult] containing metrics about the
// transformations applied (cycles removed, edges reduced, nodes added, etc.).
// This is useful for logging and understanding graph complexity.
//
// # Nil Handling
//
// Normalize returns an error if g is nil. An empty DAG is returned unchanged
// with zero metrics.
//
// # Performance
//
// Complexity is O(V²·E) in the worst case due to transitive reduction, where
// V is the number of nodes and E is the number of edges. For typical
// dependency graphs with limited fan-out, performance is near-linear.
func Normalize(g *dag.DAG) (*TransformResult, error) {
	return NormalizeWithOptions(g, NormalizeOptions{})
}

// NormalizeWithOptions prepares a DAG for tower rendering with configurable
// transformation steps.
//
// NormalizeWithOptions is like [Normalize] but allows skipping specific
// transformations via opts. This is useful when:
//   - The input is known to be acyclic (skip cycle breaking)
//   - Transitive edges should be preserved (skip reduction)
//   - Edge crossings are acceptable (skip separators)
//
// The transformations are applied in this order (unless skipped):
//
//  1. [BreakCycles]: Remove back-edges (unless opts.SkipCycleBreaking)
//  2. [TransitiveReduction]: Remove redundant edges (unless opts.SkipTransitiveReduction)
//  3. [AssignLayers]: Assign rows (always applied)
//  4. [Subdivide]: Break long edges (always applied)
//  5. [ResolveSpanOverlaps]: Insert separators (unless opts.SkipSeparators)
//
// Layer assignment and edge subdivision are always applied because they are
// required for valid tower layouts.
//
// # Nil Handling
//
// NormalizeWithOptions returns an error if g is nil. An empty DAG returns
// zero metrics.
//
// # Performance
//
// See [Normalize]. Skipping transitive reduction reduces worst-case complexity
// from O(V²·E) to O(V·E).
func NormalizeWithOptions(g *dag.DAG, opts NormalizeOptions) (result *TransformResult, err error) {
	if g == nil {
		return nil, fmt.Errorf("normalize: DAG must not be nil")
	}

	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("normalize: internal error during graph transformation: %v", r)
		}
	}()

	result = &TransformResult{}

	if !opts.SkipCycleBreaking {
		result.CyclesRemoved = BreakCycles(g)
	}

	if !opts.SkipTransitiveReduction {
		edgesBefore := g.EdgeCount()
		TransitiveReduction(g)
		result.TransitiveEdgesRemoved = edgesBefore - g.EdgeCount()
	}

	AssignLayers(g)

	nodesBefore := g.NodeCount()
	Subdivide(g)
	result.SubdividersAdded = g.NodeCount() - nodesBefore

	if !opts.SkipSeparators {
		nodesBefore := g.NodeCount()
		ResolveSpanOverlaps(g)
		result.SeparatorsAdded = g.NodeCount() - nodesBefore
	}

	result.MaxRow = g.MaxRow()

	return result, nil
}
