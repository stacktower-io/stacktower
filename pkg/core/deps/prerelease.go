package deps

import (
	"maps"
	"strings"

	"github.com/matzehuels/stacktower/pkg/core/dag"
)

// FilterPrereleaseNodes removes prerelease versions from the graph unless
// includePrerelease is true.
func FilterPrereleaseNodes(g *dag.DAG, includePrerelease bool) *dag.DAG {
	if includePrerelease {
		return g
	}

	// Keep roots and walk reachability from roots while skipping prerelease nodes.
	visited := make(map[string]bool, g.NodeCount())
	queue := make([]string, 0, g.NodeCount())
	for _, n := range g.Nodes() {
		if len(g.Parents(n.ID)) == 0 {
			visited[n.ID] = true
			queue = append(queue, n.ID)
		}
	}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, child := range g.Children(cur) {
			if visited[child] {
				continue
			}
			if childNode, ok := g.Node(child); ok && isPrereleaseNode(childNode) {
				continue
			}
			visited[child] = true
			queue = append(queue, child)
		}
	}

	filtered := dag.New(nil)
	maps.Copy(filtered.Meta(), g.Meta())

	for _, n := range g.Nodes() {
		if !visited[n.ID] {
			continue
		}
		_ = filtered.AddNode(dag.Node{
			ID:       n.ID,
			Row:      n.Row,
			Meta:     maps.Clone(n.Meta),
			Kind:     n.Kind,
			MasterID: n.MasterID,
		})
	}
	for _, e := range g.Edges() {
		if !visited[e.From] || !visited[e.To] {
			continue
		}
		_ = filtered.AddEdge(dag.Edge{
			From: e.From,
			To:   e.To,
			Meta: maps.Clone(e.Meta),
		})
	}
	return filtered
}

func isPrereleaseNode(n *dag.Node) bool {
	if n == nil {
		return false
	}
	rawVersion, ok := n.Meta["version"]
	if !ok {
		return false
	}
	version, ok := rawVersion.(string)
	if !ok {
		return false
	}
	return IsPrereleaseVersion(version)
}

// IsPrereleaseVersion checks if a version string represents a prerelease.
// It detects common prerelease markers like alpha, beta, rc, dev, canary,
// nightly, next, etc., as well as PEP 440 style markers (e.g., 1.0.0a1).
func IsPrereleaseVersion(version string) bool {
	v := strings.ToLower(strings.TrimSpace(version))
	if v == "" {
		return false
	}
	// Go pseudo versions include two hyphens and a timestamp/hash and should not
	// be treated as prerelease channels.
	if strings.HasPrefix(v, "v0.0.0-") && strings.Count(v, "-") >= 2 {
		return false
	}

	// Full-word markers (e.g., "1.0.0-alpha.1", "2.0.0-beta.2")
	markers := []string{
		"alpha", "beta", "rc", "dev", "snapshot", "preview", "pre", "canary", "nightly", "next",
		"milestone", // Maven milestone releases
	}
	for _, m := range markers {
		if strings.Contains(v, m) {
			return true
		}
	}

	// Maven milestone pattern: -M followed by digit (e.g., "7.0.0-M6", "3.0.0-M1")
	for i := 0; i < len(v)-2; i++ {
		if v[i] == '-' && v[i+1] == 'm' && v[i+2] >= '0' && v[i+2] <= '9' {
			return true
		}
	}

	// PEP 440 abbreviated markers (e.g., "2.13.0b1", "1.0.0a1")
	// Match patterns like: 1.0.0a1, 1.0.0b2, 1.0.0.post1 (post is stable)
	// The pattern is: digit followed by 'a' or 'b' followed by digit
	for i := 0; i < len(v)-1; i++ {
		if v[i] >= '0' && v[i] <= '9' {
			next := v[i+1]
			// Check for 'a' or 'b' followed by a digit (PEP 440 alpha/beta)
			if (next == 'a' || next == 'b') && i+2 < len(v) && v[i+2] >= '0' && v[i+2] <= '9' {
				return true
			}
		}
	}
	return false
}
