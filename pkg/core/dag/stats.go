package dag

import "sort"

// GraphStats contains computed metrics about a dependency graph's topology.
type GraphStats struct {
	NodeCount      int
	EdgeCount      int
	MaxDepth       int
	DirectDeps     int
	TransitiveDeps int
	LoadBearing    []LoadBearingNode
}

// LoadBearingNode records how many packages transitively depend on a given node.
type LoadBearingNode struct {
	ID          string
	ReverseDeps int
}

// ComputeStats analyzes the DAG and returns structural metrics.
// It computes depth, direct/transitive dependency counts, and load-bearing
// ranking (which nodes are depended on by the most other packages).
func ComputeStats(g *DAG) *GraphStats {
	stats := &GraphStats{
		NodeCount: g.NodeCount(),
		EdgeCount: g.EdgeCount(),
	}

	root := FindRoot(g)
	if root == "" {
		return stats
	}

	stats.DirectDeps = len(g.Children(root))

	// BFS for max depth and total reachable non-synthetic nodes
	depth := map[string]int{root: 0}
	queue := []string{root}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		d := depth[id]
		if d > stats.MaxDepth {
			stats.MaxDepth = d
		}
		for _, child := range g.Children(id) {
			nd := d + 1
			if prev, ok := depth[child]; !ok || nd > prev {
				depth[child] = nd
				queue = append(queue, child)
			}
		}
	}

	// Count non-synthetic, non-root nodes as total deps
	totalDeps := 0
	for _, n := range g.Nodes() {
		if n.IsSynthetic() || n.ID == root || n.ID == "__project__" {
			continue
		}
		totalDeps++
	}
	stats.TransitiveDeps = totalDeps - stats.DirectDeps

	// Compute reverse dependency counts (how many non-synthetic nodes can reach each node)
	revDeps := computeReverseDeps(g, root)

	var loadBearing []LoadBearingNode
	for id, count := range revDeps {
		n, ok := g.Node(id)
		if !ok || n.IsSynthetic() || id == root || id == "__project__" {
			continue
		}
		if count > 0 {
			loadBearing = append(loadBearing, LoadBearingNode{ID: id, ReverseDeps: count})
		}
	}
	sort.Slice(loadBearing, func(i, j int) bool {
		if loadBearing[i].ReverseDeps != loadBearing[j].ReverseDeps {
			return loadBearing[i].ReverseDeps > loadBearing[j].ReverseDeps
		}
		return loadBearing[i].ID < loadBearing[j].ID
	})
	stats.LoadBearing = loadBearing

	return stats
}

// computeReverseDeps counts, for each node, how many distinct non-synthetic
// nodes transitively depend on it (i.e. can reach it via forward edges).
func computeReverseDeps(g *DAG, root string) map[string]int {
	counts := make(map[string]int)

	// For each non-synthetic, non-root node, walk its children and increment
	// their reverse-dep count. We use DFS with per-source visited sets.
	for _, n := range g.Nodes() {
		if n.IsSynthetic() || n.ID == root || n.ID == "__project__" {
			continue
		}

		visited := map[string]bool{n.ID: true}
		stack := []string{}
		for _, child := range g.Children(n.ID) {
			stack = append(stack, child)
		}
		for len(stack) > 0 {
			id := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if visited[id] {
				continue
			}
			visited[id] = true
			counts[id]++
			for _, child := range g.Children(id) {
				if !visited[child] {
					stack = append(stack, child)
				}
			}
		}
	}

	return counts
}

// FindRoot returns the ID of the primary root node (non-synthetic, in-degree 0).
func FindRoot(g *DAG) string {
	for _, n := range g.Nodes() {
		if n.IsSynthetic() || n.ID == "__project__" {
			continue
		}
		if g.InDegree(n.ID) == 0 {
			return n.ID
		}
	}
	return ""
}
