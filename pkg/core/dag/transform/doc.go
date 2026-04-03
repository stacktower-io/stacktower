// Package transform provides graph transformations that prepare a DAG for
// tower rendering.
//
// # Overview
//
// Real-world dependency graphs rarely arrive in a form suitable for direct
// tower visualization. This package provides a normalization pipeline that
// transforms arbitrary DAGs into a canonical form where:
//
//   - Edges connect only consecutive rows (no long-spanning edges)
//   - Redundant transitive edges are removed
//   - Impossible crossing patterns are resolved with separator beams
//   - Nodes are assigned to rows based on their depth from roots
//
// The [Normalize] function applies the complete pipeline in the correct order.
//
// # Transitive Reduction
//
// [TransitiveReduction] removes redundant edges that can be inferred through
// other paths. If A→B and B→C exist, then A→C is redundant and removed.
//
// This is critical for tower layouts because transitive edges create
// impossible geometry—a block cannot simultaneously rest on something two
// floors down while also having direct contact.
//
// # Edge Subdivision
//
// [Subdivide] breaks long edges (spanning multiple rows) into chains of
// single-row hops by inserting subdivider nodes. For example:
//
//	Before: app (row 0) → core (row 3)
//	After:  app → app_sub_1 → app_sub_2 → core
//
// Subdivider nodes maintain a MasterID linking back to their origin, allowing
// them to be visually merged into continuous vertical blocks during rendering.
//
// This also extends all sink nodes (leaves) to the bottom row, ensuring the
// tower has a flat foundation.
//
// # Span Overlap Resolution
//
// [ResolveSpanOverlaps] handles "tangle motifs"—graph patterns that guarantee
// edge crossings regardless of ordering. The classic example is a complete
// bipartite subgraph where multiple parents share multiple children.
//
// Rather than accepting unavoidable crossings, this function inserts auxiliary
// "separator beam" nodes that group the edges through a shared intermediate:
//
//	Before: auth→logging, auth→metrics, api→logging, api→metrics (guaranteed crossing)
//	After:  auth→sep, api→sep, sep→logging, sep→metrics (no crossing possible)
//
// # Layer Assignment
//
// [AssignLayers] computes the row (layer) for each node based on its depth
// from source nodes (those with no incoming edges). This uses a topological
// traversal to ensure parents are always in rows above their children.
//
// # Cycle Breaking
//
// [BreakCycles] detects and removes edges that create cycles. While dependency
// graphs should be acyclic, real-world data sometimes contains circular
// dependencies. This function removes the minimum edges needed to restore
// acyclicity using a DFS-based approach.
//
// # Goroutine Safety
//
// All functions in this package modify the input DAG in place and are NOT safe
// for concurrent use. Callers must ensure exclusive access to the DAG during
// transformation. The DAG itself is not internally synchronized.
//
// # Usage
//
// For most use cases, call [Normalize] which applies all transformations:
//
//	g := dag.New(nil)
//	// ... populate graph ...
//	result, err := transform.Normalize(g) // Modifies g in place, returns metrics
//	if err != nil {
//	    log.Fatal(err)
//	}
//	fmt.Printf("Removed %d cycles, %d transitive edges\n",
//	    result.CyclesRemoved, result.TransitiveEdgesRemoved)
//
// To skip specific transformations, use [NormalizeWithOptions]:
//
//	result, err := transform.NormalizeWithOptions(g, transform.NormalizeOptions{
//	    SkipTransitiveReduction: true, // Keep all edges
//	    SkipSeparators:          true, // Accept crossings
//	})
//
// For fine-grained control, apply transformations individually in this order:
//
//	transform.BreakCycles(g)
//	transform.TransitiveReduction(g)
//	transform.AssignLayers(g)
//	transform.Subdivide(g)
//	transform.ResolveSpanOverlaps(g)
package transform
