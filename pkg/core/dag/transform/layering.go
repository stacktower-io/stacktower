package transform

import "github.com/stacktower-io/stacktower/pkg/core/dag"

// AssignLayers assigns nodes to horizontal rows (layers) based on their depth
// in the graph.
//
// AssignLayers uses a longest-path algorithm via topological sort (Kahn's
// algorithm) to compute row assignments. Each node is placed at one plus the
// maximum row of any of its parents, ensuring that:
//   - Source nodes (no incoming edges) are at row 0
//   - All parents are strictly above their children
//   - Each node is pushed as deep as necessary to avoid parent conflicts
//
// Existing row assignments in the DAG are overwritten.
//
// # Algorithm
//
// AssignLayers performs a topological traversal:
//  1. Initialize all source nodes (in-degree 0) at row 0 and add to queue
//  2. Process queue: for each node, assign children to max(current_row + 1)
//  3. Decrement in-degree counters; add newly zero-degree nodes to queue
//  4. Repeat until queue is empty
//
// # Cycles
//
// AssignLayers assumes the graph is acyclic. If cycles exist, nodes in the
// cycle will never reach zero in-degree and will remain at row 0 (their
// default). Run [BreakCycles] first to ensure correct layering.
//
// # Nil Handling
//
// AssignLayers panics if g is nil. If g is empty (zero nodes), the function
// returns immediately.
//
// # Performance
//
// Time complexity is O(V + E), where V is nodes and E is edges. Space
// complexity is O(V) for the queue and row/degree maps.
func AssignLayers(g *dag.DAG) {
	nodes := g.Nodes()
	inDegree := make(map[string]int, len(nodes))
	rows := make(map[string]int, len(nodes))
	queue := make([]string, 0, len(nodes))

	for _, n := range nodes {
		// Seed every node at row 0 so stale Row values (e.g. from a graph
		// file with bad metadata) are always overwritten: sources and
		// unreachable nodes would otherwise keep their old rows because
		// SetRows preserves rows for nodes missing from the map.
		rows[n.ID] = 0
		degree := g.InDegree(n.ID)
		inDegree[n.ID] = degree
		if degree == 0 {
			queue = append(queue, n.ID)
		}
	}

	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]

		for _, child := range g.Children(curr) {
			if row := rows[curr] + 1; row > rows[child] {
				rows[child] = row
			}
			inDegree[child]--
			if inDegree[child] == 0 {
				queue = append(queue, child)
			}
		}
	}

	g.SetRows(rows)
}

// IsLayered reports whether the graph's row assignments are consistent with
// a valid layering:
//   - at least one node sits on row 0 (rows start at the top), and
//   - every edge points from a lower row to a strictly higher row.
//
// Edges may span more than one row (subdivision happens separately), but an
// edge within a row or pointing upward means the rows were never assigned or
// are stale, and AssignLayers should be (re-)run.
//
// An empty graph is considered layered.
func IsLayered(g *dag.DAG) bool {
	if g.NodeCount() == 0 {
		return true
	}
	if len(g.NodesInRow(0)) == 0 {
		return false
	}
	for _, e := range g.Edges() {
		from, okF := g.Node(e.From)
		to, okT := g.Node(e.To)
		if !okF || !okT {
			return false
		}
		if to.Row <= from.Row {
			return false
		}
	}
	return true
}
