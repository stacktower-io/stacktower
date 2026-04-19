package dag

// FindPaths returns all dependency paths from root to target in the graph,
// up to maxPaths results. It uses reverse BFS from the target via Parents(),
// which is efficient because targets typically have fewer parents than roots
// have children.
//
// Returns nil if target is not in the graph. Returns a single empty-inner-slice
// path if root == target. The maxPaths parameter caps the result size to prevent
// explosion in wide graphs; use 0 for unlimited.
func FindPaths(g *DAG, root, target string, maxPaths int) [][]string {
	if _, ok := g.Node(target); !ok {
		return nil
	}
	if root == target {
		return [][]string{{root}}
	}
	if _, ok := g.Node(root); !ok {
		return nil
	}

	type partial struct {
		path []string
	}

	var results [][]string
	queue := []partial{{path: []string{target}}}

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]

		tip := cur.path[len(cur.path)-1]
		for _, parent := range g.Parents(tip) {
			next := make([]string, len(cur.path)+1)
			copy(next, cur.path)
			next[len(cur.path)] = parent

			if parent == root {
				reversed := make([]string, len(next))
				for i, id := range next {
					reversed[len(next)-1-i] = id
				}
				results = append(results, reversed)
				if maxPaths > 0 && len(results) >= maxPaths {
					return results
				}
				continue
			}
			queue = append(queue, partial{path: next})
		}
	}
	return results
}

// ShortestPaths returns only the shortest dependency paths from root to target.
// If there are multiple paths of the same minimum length, all are returned.
func ShortestPaths(g *DAG, root, target string) [][]string {
	all := FindPaths(g, root, target, 0)
	if len(all) == 0 {
		return nil
	}

	minLen := len(all[0])
	for _, p := range all[1:] {
		if len(p) < minLen {
			minLen = len(p)
		}
	}

	var shortest [][]string
	for _, p := range all {
		if len(p) == minLen {
			shortest = append(shortest, p)
		}
	}
	return shortest
}

// ShortestDepth returns the depth of the shortest path (number of edges, not nodes).
// Returns -1 if no paths exist.
func ShortestDepth(paths [][]string) int {
	if len(paths) == 0 {
		return -1
	}
	min := len(paths[0])
	for _, p := range paths[1:] {
		if len(p) < min {
			min = len(p)
		}
	}
	return min - 1 // edges = nodes - 1
}
