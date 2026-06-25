package dag

import (
	"math/bits"
	"sort"
)

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

	// BFS for max depth (shortest path from root, cycle-safe)
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
			if _, ok := depth[child]; !ok {
				depth[child] = d + 1
				queue = append(queue, child)
			}
		}
	}

	// Count non-synthetic, non-root nodes as total deps
	totalDeps := 0
	for _, n := range g.Nodes() {
		if n.IsSynthetic() || n.ID == root || n.ID == ProjectRootNodeID {
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
		if !ok || n.IsSynthetic() || id == root || id == ProjectRootNodeID {
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
//
// Instead of one DFS per source node (O(V·(V+E))), a single pass in
// topological order propagates ancestor bitsets along edges: a node's
// ancestor set is the union of each parent's set plus the parent itself.
// Time is O(E·V/64), memory O(V²/64) — for a 2000-node graph that's a
// ~500KB flat buffer instead of thousands of map-based DFS visits.
func computeReverseDeps(g *DAG, root string) map[string]int {
	nodes := g.Nodes()
	n := len(nodes)
	if n == 0 {
		return map[string]int{}
	}
	idx := NodePosMap(nodes)

	// countable[i] reports whether node i is counted as an ancestor,
	// mirroring the source filter of the per-node DFS version.
	countable := make([]bool, n)
	for i, node := range nodes {
		countable[i] = !node.IsSynthetic() && node.ID != root && node.ID != ProjectRootNodeID
	}

	children := make([][]int, n)
	indeg := make([]int, n)
	for _, e := range g.EdgesIter() {
		u, ok := idx[e.From]
		if !ok {
			continue
		}
		v, ok := idx[e.To]
		if !ok {
			continue
		}
		children[u] = append(children[u], v)
		indeg[v]++
	}

	words := (n + 63) / 64
	buf := make([]uint64, n*words)
	ancestors := make([][]uint64, n)
	for i := range ancestors {
		ancestors[i] = buf[i*words : (i+1)*words]
	}

	// Kahn's algorithm with a head-index queue.
	queue := make([]int, 0, n)
	for i, d := range indeg {
		if d == 0 {
			queue = append(queue, i)
		}
	}
	for head := 0; head < len(queue); head++ {
		u := queue[head]
		au := ancestors[u]
		for _, v := range children[u] {
			av := ancestors[v]
			for w := range au {
				av[w] |= au[w]
			}
			if countable[u] {
				av[u/64] |= 1 << (u % 64)
			}
			indeg[v]--
			if indeg[v] == 0 {
				queue = append(queue, v)
			}
		}
	}

	counts := make(map[string]int, n)
	for i, node := range nodes {
		c := 0
		for _, w := range ancestors[i] {
			c += bits.OnesCount64(w)
		}
		if c > 0 {
			counts[node.ID] = c
		}
	}
	return counts
}

// FindRoot returns the ID of the primary root node (non-synthetic, in-degree 0).
func FindRoot(g *DAG) string {
	var candidates []string
	for _, n := range g.Nodes() {
		if n.IsSynthetic() || n.ID == ProjectRootNodeID {
			continue
		}
		if g.InDegree(n.ID) == 0 {
			candidates = append(candidates, n.ID)
		}
	}
	if len(candidates) == 0 {
		return ""
	}
	sort.Strings(candidates)
	return candidates[0]
}
