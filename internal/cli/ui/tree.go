package ui

import (
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/matzehuels/stacktower/pkg/core/dag"
	"github.com/matzehuels/stacktower/pkg/core/deps"
	"github.com/matzehuels/stacktower/pkg/core/deps/metadata"
)

// =============================================================================
// Tree Styles
// =============================================================================

var (
	styleTreeRoot    = lipgloss.NewStyle().Bold(true).Foreground(ColorPurple)
	styleTreePkg     = lipgloss.NewStyle().Foreground(ColorWhite)
	styleTreeVersion = lipgloss.NewStyle().Foreground(ColorGreen) // green to match resolve table
	styleTreeBranch  = lipgloss.NewStyle().Foreground(ColorDim)
	styleTreeElided  = lipgloss.NewStyle().Foreground(ColorDim).Italic(true)
	styleTreeMeta    = lipgloss.NewStyle().Foreground(ColorGray).Italic(true)
	styleTreeStat    = lipgloss.NewStyle().Foreground(ColorGray)
	styleTreeNum     = lipgloss.NewStyle().Foreground(ColorPurple).Bold(true)
)

// =============================================================================
// Tree Options and Stats
// =============================================================================

// TreeOpts controls tree rendering behavior.
type TreeOpts struct {
	Color    bool
	ShowMeta bool // display description/license/stars beneath each node
}

// TreeStats holds statistics collected during tree rendering.
type TreeStats struct {
	MaxDepth   int
	DirectDeps int
}

// =============================================================================
// Tree Rendering
// =============================================================================

// WriteTree renders a DAG as an indented dependency tree.
func WriteTree(w io.Writer, g *dag.DAG, roots []string, opts TreeOpts) TreeStats {
	visited := make(map[string]bool)
	stats := TreeStats{}

	for _, root := range roots {
		stats.DirectDeps = len(g.Children(root))
		writeTreeNode(w, g, root, "", 0, true, true, visited, &stats, opts)
	}

	return stats
}

// PrintTreeSummary writes a styled summary line for resolved dependencies.
func PrintTreeSummary(w io.Writer, nodeCount int, stats TreeStats) {
	fmt.Fprintf(w, "%s %s %s %s %s %s %s %s\n",
		styleTreeStat.Render("Resolved"),
		styleTreeNum.Render(fmt.Sprintf("%d", nodeCount)),
		styleTreeStat.Render("packages"),
		StyleDim.Render("·"),
		styleTreeStat.Render("depth"),
		styleTreeNum.Render(fmt.Sprintf("%d", stats.MaxDepth)),
		StyleDim.Render("·"),
		styleTreeStat.Render(fmt.Sprintf("%d direct", stats.DirectDeps)))
}

func writeTreeNode(w io.Writer, g *dag.DAG, id, prefix string, depth int, isRoot, isLast bool, visited map[string]bool, stats *TreeStats, opts TreeOpts) {
	if depth > stats.MaxDepth {
		stats.MaxDepth = depth
	}

	version := nodeVersion(g, id)
	inline := ""
	if opts.ShowMeta {
		inline = nodeInlineMeta(g, id, opts.Color)
	}

	if isRoot {
		if opts.Color {
			fmt.Fprintf(w, "%s %s%s\n", styleTreeRoot.Render(id), styleTreeVersion.Render(version), inline)
		} else {
			fmt.Fprintf(w, "%s %s%s\n", id, version, inline)
		}
	} else {
		connector := "├── "
		if isLast {
			connector = "└── "
		}
		if opts.Color {
			versionStr := ""
			if version != "" {
				versionStr = " " + styleTreeVersion.Render(version)
			}
			fmt.Fprintf(w, "%s%s%s%s%s\n",
				styleTreeBranch.Render(prefix),
				styleTreeBranch.Render(connector),
				styleTreePkg.Render(id),
				versionStr,
				inline)
		} else {
			versionStr := ""
			if version != "" {
				versionStr = " " + version
			}
			fmt.Fprintf(w, "%s%s%s%s%s\n", prefix, connector, id, versionStr, inline)
		}
	}

	if opts.ShowMeta && !visited[id] {
		writeDescLine(w, g, id, prefix, isRoot, isLast, opts.Color)
	}

	if visited[id] {
		if len(g.Children(id)) > 0 {
			childPrefix := nextPrefix(prefix, isRoot, isLast)
			if opts.Color {
				fmt.Fprintf(w, "%s%s\n",
					styleTreeBranch.Render(childPrefix+"└── "),
					styleTreeElided.Render("(...)"))
			} else {
				fmt.Fprintf(w, "%s└── (...)\n", childPrefix)
			}
		}
		return
	}
	visited[id] = true

	children := g.Children(id)
	slices.Sort(children)

	childPrefix := nextPrefix(prefix, isRoot, isLast)
	for i, child := range children {
		last := i == len(children)-1
		writeTreeNode(w, g, child, childPrefix, depth+1, false, last, visited, stats, opts)
	}
}

// nodeInlineMeta returns a suffix string with license and stars for the node line.
func nodeInlineMeta(g *dag.DAG, id string, color bool) string {
	n, ok := g.Node(id)
	if !ok || n.Meta == nil {
		return ""
	}

	var parts []string
	if lic, _ := n.Meta["license"].(string); lic != "" {
		parts = append(parts, lic)
	}
	if stars, ok := n.Meta[metadata.RepoStars].(int); ok && stars > 0 {
		parts = append(parts, formatStars(stars))
	}
	if len(parts) == 0 {
		return ""
	}

	text := strings.Join(parts, " · ")
	if color {
		return "  " + styleTreeMeta.Render(text)
	}
	return "  " + text
}

// writeDescLine renders the package description beneath a tree node.
func writeDescLine(w io.Writer, g *dag.DAG, id, prefix string, isRoot, isLast, color bool) {
	n, ok := g.Node(id)
	if !ok || n.Meta == nil {
		return
	}

	desc, _ := n.Meta["description"].(string)
	if desc == "" {
		return
	}
	if len(desc) > 60 {
		desc = desc[:57] + "..."
	}

	metaPrefix := prefix
	if !isRoot {
		if isLast {
			metaPrefix += "    "
		} else {
			metaPrefix += "│   "
		}
	}

	if color {
		fmt.Fprintf(w, "%s  %s\n", styleTreeBranch.Render(metaPrefix), styleTreeMeta.Render(desc))
	} else {
		fmt.Fprintf(w, "%s  %s\n", metaPrefix, desc)
	}
}

func formatStars(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM stars", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.0fk stars", float64(n)/1_000)
	default:
		return fmt.Sprintf("%d stars", n)
	}
}

func nextPrefix(prefix string, isRoot, isLast bool) string {
	if isRoot {
		return prefix
	}
	if isLast {
		return prefix + "    "
	}
	return prefix + "│   "
}

// nodeVersion extracts the version string from a DAG node's metadata.
func nodeVersion(g *dag.DAG, id string) string {
	n, ok := g.Node(id)
	if !ok {
		return ""
	}
	if v, ok := n.Meta["version"]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// =============================================================================
// Graph Utilities
// =============================================================================

// FindRoots returns sorted IDs of all root nodes (nodes with no parents).
func FindRoots(g *dag.DAG) []string {
	var roots []string
	for _, n := range g.Nodes() {
		if len(g.Parents(n.ID)) == 0 {
			roots = append(roots, n.ID)
		}
	}
	slices.Sort(roots)
	return roots
}

// GraphDepth returns the maximum depth of the DAG from its root(s).
func GraphDepth(g *dag.DAG) int {
	if g == nil || g.NodeCount() == 0 {
		return 0
	}

	start := deps.ProjectRootNodeID
	if _, ok := g.Node(start); !ok {
		foundVirtualRoot := false
		for _, n := range g.Nodes() {
			if v, ok := n.Meta["virtual"].(bool); ok && v {
				start = n.ID
				foundVirtualRoot = true
				break
			}
		}
		if !foundVirtualRoot {
			roots := FindRoots(g)
			if len(roots) == 0 {
				return 0
			}
			start = roots[0]
		}
	}

	// Longest-path style BFS from the primary root. We update depth when a
	// longer path is discovered (important for DAGs with merge/rejoin patterns).
	depth := map[string]int{start: 0}
	queue := []string{start}
	best := 0
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		d := depth[id]
		if d > best {
			best = d
		}
		for _, child := range g.Children(id) {
			nd := d + 1
			if prev, ok := depth[child]; !ok || nd > prev {
				depth[child] = nd
				queue = append(queue, child)
			}
		}
	}
	return best
}
