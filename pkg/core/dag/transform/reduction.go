package transform

import "github.com/stacktower-io/stacktower/pkg/core/dag"

// TransitiveReduction removes redundant edges from the graph.
//
// TransitiveReduction removes any edge (u, v) where there exists an alternate
// path from u to v through at least one intermediate node. For example, if
// edges A→B, B→C, and A→C all exist, then A→C is redundant and is removed
// because A reaches C via B.
//
// This simplifies visualization by showing only direct dependencies, which is
// critical for tower layouts where transitive edges create impossible geometry
// (a block cannot rest on both adjacent and distant floors simultaneously).
//
// # Algorithm
//
// For each node u with at least two children, a single DFS is run from the
// children of u (paths of length ≥ 2 from u). Any child of u reached this way
// is redundant. Checking against the original edge set is sound: in a DAG an
// edge belongs to the transitive reduction iff no path of length ≥ 2 connects
// its endpoints, and such paths never depend on other redundant edges being
// kept.
//
// # Nil Handling
//
// TransitiveReduction panics if g is nil. If g is empty (zero nodes), the
// function returns immediately without error.
//
// # Performance
//
// Time complexity is O(V·(V+E)) in the worst case, but each per-node DFS only
// touches the descendants of that node, so on sparse layered dependency
// graphs the typical cost is far lower. Space complexity is O(V): scratch
// arrays are epoch-marked and reused across nodes instead of materializing a
// V×V reachability matrix.
//
// # Edge Metadata
//
// TransitiveReduction preserves edge metadata for all non-redundant edges.
// Metadata on removed edges is discarded.
func TransitiveReduction(g *dag.DAG) {
	nodes := g.Nodes()
	if len(nodes) == 0 {
		return
	}

	nodeIndex := dag.NodePosMap(nodes)
	adjacency := make([][]int, len(nodes))
	for _, e := range g.Edges() {
		if src, ok := nodeIndex[e.From]; ok {
			if dst, ok := nodeIndex[e.To]; ok {
				adjacency[src] = append(adjacency[src], dst)
			}
		}
	}

	n := len(nodes)
	// Epoch-marked scratch state, reused for every source node: a slot
	// belongs to the current source iff it stores the current epoch.
	visited := make([]int, n)   // node already expanded in this epoch
	target := make([]int, n)    // node is a direct child of the source
	redundant := make([]int, n) // child reached via an intermediate path
	for i := range visited {
		visited[i] = -1
		target[i] = -1
		redundant[i] = -1
	}
	stack := make([]int, 0, n)

	for u, children := range adjacency {
		if len(children) < 2 {
			// Redundancy requires an alternate path through another child.
			continue
		}

		epoch := u
		for _, c := range children {
			target[c] = epoch
		}

		// DFS from every child's children: each node reached lies on a
		// path of length ≥ 2 from u. The visited set is shared across
		// children — once a subtree is expanded, all targets inside it
		// have been recorded.
		stack = stack[:0]
		for _, c := range children {
			stack = append(stack, adjacency[c]...)
		}
		for len(stack) > 0 {
			x := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if visited[x] == epoch {
				continue
			}
			visited[x] = epoch
			if target[x] == epoch {
				redundant[x] = epoch
			}
			stack = append(stack, adjacency[x]...)
		}

		for _, c := range children {
			if redundant[c] == epoch {
				g.RemoveEdge(nodes[u].ID, nodes[c].ID)
			}
		}
	}
}
