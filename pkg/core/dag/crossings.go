package dag

import (
	"maps"
	"slices"
)

// CrossingWorkspace provides reusable buffers for crossing calculations to avoid
// repeated allocations. Create with [NewCrossingWorkspace] and reuse across multiple
// calls to [CountCrossingsIdx]. This optimization matters when evaluating millions
// of candidate orderings during branch-and-bound search.
//
// The workspace is not safe for concurrent use - each goroutine should have its own.
type CrossingWorkspace struct {
	ft  []int // Fenwick tree for counting inversions
	pos []int // Position lookup buffer
}

// NewCrossingWorkspace creates a workspace for counting crossings efficiently.
// The maxWidth parameter should be the maximum number of nodes in any single row
// across all calls that will use this workspace. Using a workspace smaller than
// needed will cause CountCrossingsIdx to produce incorrect results.
//
// For typical use, set maxWidth to the size of the largest row in your graph:
//
//	maxWidth := 0
//	for _, row := range g.RowIDs() {
//	    if n := len(g.NodesInRow(row)); n > maxWidth {
//	        maxWidth = n
//	    }
//	}
//	ws := dag.NewCrossingWorkspace(maxWidth)
func NewCrossingWorkspace(maxWidth int) *CrossingWorkspace {
	return &CrossingWorkspace{
		ft:  make([]int, maxWidth+2),
		pos: make([]int, maxWidth+2),
	}
}

// CountCrossings returns the total number of edge crossings for the given row orderings.
// It sums the crossings between each pair of consecutive rows. The orders map should
// contain node IDs in left-to-right order for each row. Rows without entries in the
// map are treated as empty.
//
// Example:
//
//	orders := map[int][]string{
//	    0: {"app", "cli"},           // row 0: app on left, cli on right
//	    1: {"lib1", "lib2", "lib3"}, // row 1: three nodes
//	}
//	crossings := dag.CountCrossings(g, orders)
//
// This function is typically used during optimization to evaluate candidate orderings.
// It runs in O(R × E log V) time where R is the number of rows, E is edges per layer,
// and V is nodes per layer.
func CountCrossings(g *DAG, orders map[int][]string) int {
	rows := slices.Sorted(maps.Keys(orders))
	crossings := 0
	for i := 0; i < len(rows)-1; i++ {
		// Pair each row with the next row that actually exists in the map,
		// not r+1: gapped row IDs would otherwise silently count zero
		// crossings for the affected pairs.
		crossings += CountLayerCrossings(g, orders[rows[i]], orders[rows[i+1]])
	}
	return crossings
}

// CountLayerCrossings counts edge crossings between two adjacent rows using a
// Fenwick tree (binary indexed tree) for O(E log V) performance where E is the
// number of edges between the rows and V is the number of nodes in the lower row.
//
// Two edges (u1,v1) and (u2,v2) cross if and only if:
//
//	pos(u1) < pos(u2) AND pos(v1) > pos(v2)
//
// This is equivalent to counting inversions in the sequence of target positions
// when edges are sorted by source position. The Fenwick tree enables efficient
// inversion counting compared to the naive O(E²) algorithm.
//
// Returns 0 if either row is empty or nil, as no crossings can exist without edges.
func CountLayerCrossings(g *DAG, upper, lower []string) int {
	if len(upper) == 0 || len(lower) == 0 {
		return 0
	}

	lowerPos := PosMap(lower)

	type edge struct{ upper, lower int }
	edges := make([]edge, 0, len(upper)*2)
	for i, nodeID := range upper {
		for _, child := range g.Children(nodeID) {
			if pos, ok := lowerPos[child]; ok {
				edges = append(edges, edge{i, pos})
			}
		}
	}
	if len(edges) < 2 {
		return 0
	}

	// Sort edges by source position, then by target position
	slices.SortFunc(edges, func(a, b edge) int {
		if a.upper != b.upper {
			return a.upper - b.upper
		}
		return a.lower - b.lower
	})

	// Count inversions using Fenwick tree
	fenwick := make([]int, len(lower)+1)
	crossings, total := 0, 0
	for _, e := range edges {
		// Query: count edges seen so far with target <= e.lower
		lessOrEqual := 0
		for q := e.lower + 1; q > 0; q -= q & (-q) {
			lessOrEqual += fenwick[q]
		}
		// Crossings = edges seen so far with target > e.lower
		crossings += total - lessOrEqual

		// Update: increment count at target position
		total++
		for idx := e.lower + 1; idx < len(fenwick); idx += idx & (-idx) {
			fenwick[idx]++
		}
	}
	return crossings
}

// CountCrossingsIdx counts crossings using index-based edges and permutations.
// This is an optimized version for the branch-and-bound search that avoids
// string lookups by using integer indices throughout.
//
// The edges parameter should be a slice where edges[i] contains the indices
// (into the lower row) of all children of upper row node i. The upperPerm
// and lowerPerm parameters are permutations (orderings) of node indices.
// The ws parameter must be a workspace created with [NewCrossingWorkspace]
// with maxWidth >= len(lowerPerm).
//
// This function is typically only used internally by optimization code that
// needs to evaluate thousands of orderings per second. Most callers should
// use [CountCrossings] or [CountLayerCrossings] instead.
//
// Performance: O(E log V) where E is the total number of edges and V is len(lowerPerm).
func CountCrossingsIdx(edges [][]int, upperPerm, lowerPerm []int, ws *CrossingWorkspace) int {
	if len(upperPerm) == 0 || len(lowerPerm) == 0 {
		return 0
	}

	// Grow the workspace if it is undersized: indexing past the buffers
	// would silently produce wrong crossing counts (or panic).
	if need := len(lowerPerm) + 2; len(ws.ft) < need {
		ws.ft = make([]int, need)
		ws.pos = make([]int, need)
	}

	// Build position lookup: where is each original index in the permutation?
	for pos, origIdx := range lowerPerm {
		ws.pos[origIdx] = pos
	}

	// Clear Fenwick tree
	limit := len(lowerPerm) + 1
	for i := 0; i < limit; i++ {
		ws.ft[i] = 0
	}

	// Count inversions using Fenwick tree
	crossings, total := 0, 0
	for _, upperIdx := range upperPerm {
		targets := edges[upperIdx]
		// Query phase: count crossings for all edges from this source
		for _, targetIdx := range targets {
			targetPos := ws.pos[targetIdx]
			lessOrEqual := 0
			for q := targetPos + 1; q > 0; q -= q & (-q) {
				lessOrEqual += ws.ft[q]
			}
			crossings += total - lessOrEqual
		}

		// Update phase: mark all these edges as processed
		for _, targetIdx := range targets {
			targetPos := ws.pos[targetIdx]
			total++
			for idx := targetPos + 1; idx < limit; idx += idx & (-idx) {
				ws.ft[idx]++
			}
		}
	}
	return crossings
}

// CountPairCrossings counts how many crossings would result from swapping two
// adjacent nodes (left and right) in their row. If useParents is true, considers
// edges to the row above; otherwise, considers edges to the row below.
//
// This is used by local search heuristics (e.g., adjacent node swapping) to
// decide whether a swap would reduce crossings. The adjOrder slice should
// contain the node IDs of the adjacent row in left-to-right order.
//
// Returns 0 if either node has no edges to the adjacent row, or if no crossings
// would occur. This function does not modify the graph.
func CountPairCrossings(g *DAG, left, right string, adjOrder []string, useParents bool) int {
	return CountPairCrossingsWithPos(g, left, right, PosMap(adjOrder), useParents)
}

// CountPairCrossingsWithPos is like [CountPairCrossings] but takes a precomputed
// position map for the adjacent row. This avoids repeated calls to [PosMap] when
// checking multiple swaps against the same adjacent row.
//
// The adjPos map should map node IDs to their positions (0-indexed) in the
// adjacent row. Nodes not in the map are ignored.
func CountPairCrossingsWithPos(g *DAG, left, right string, adjPos map[string]int, useParents bool) int {
	var lnbr, rnbr []string
	if useParents {
		lnbr = g.Parents(left)
		rnbr = g.Parents(right)
	} else {
		lnbr = g.Children(left)
		rnbr = g.Children(right)
	}

	crossings := 0
	for _, ln := range lnbr {
		lp, ok := adjPos[ln]
		if !ok {
			continue
		}
		for _, rn := range rnbr {
			// If left's neighbor is to the right of right's neighbor, they cross
			if rp, ok := adjPos[rn]; ok && lp > rp {
				crossings++
			}
		}
	}
	return crossings
}
