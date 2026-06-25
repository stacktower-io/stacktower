package transform

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/stacktower-io/stacktower/pkg/core/dag"
)

// ResolveSpanOverlaps identifies and resolves impossible crossing patterns by
// inserting separator beam nodes.
//
// ResolveSpanOverlaps detects "tangle motifs"—subgraph patterns where multiple
// parent nodes share multiple child nodes in a way that guarantees edge
// crossings regardless of child ordering. The canonical example is a complete
// bipartite graph K(2,2):
//
//	auth → logging    auth → metrics
//	api  → logging    api  → metrics
//
// No matter how you order {logging, metrics}, edges must cross. Rather than
// accepting crossings, ResolveSpanOverlaps inserts a [dag.NodeKindAuxiliary]
// separator node that routes edges through a shared intermediate:
//
//	auth → separator → logging
//	api  → separator → metrics
//
// This eliminates crossings by factoring shared dependencies through a beam.
//
// # Detection Algorithm
//
// ResolveSpanOverlaps processes rows bottom-up. For each row, it:
//  1. Computes the "span" of each parent (min/max child positions)
//  2. Counts how many parent spans overlap each gap between children
//  3. Where 2+ parents overlap, inserts a separator and reroutes edges
//  4. Repeats until no overlaps remain (may insert multiple separators per row)
//
// # Separator Nodes
//
// Separator nodes are inserted in a new row between parents and children,
// shifting all lower rows down. Separator IDs are generated as
// "Sep_row_firstChild_lastChild" with numeric suffixes if needed for
// uniqueness.
//
// # Eligibility Rules
//
// A parent is eligible for separator insertion only if:
//   - It has 2+ children in the target row
//   - ALL its children are in that single row (no splitting across rows)
//   - None of its children are subdividers of the same master (avoids splitting logical columns)
//
// Separators are inserted in gaps between children where canInsertBetween
// returns true (respects subdivider master boundaries).
//
// # Multiple Passes
//
// ResolveSpanOverlaps may make multiple passes over a row, inserting separators
// iteratively until no overlaps remain. Each insertion shifts rows and
// recomputes spans.
//
// # Nil Handling
//
// ResolveSpanOverlaps panics if d is nil. If d is empty (zero nodes), the
// function returns immediately.
//
// # Performance
//
// Time complexity is O(R·P·C·I) where R is the number of rows, P is the
// average number of parents per row, C is children per parent, and I is the
// number of separator insertion iterations (typically 1-3). For typical
// dependency graphs, this is effectively O(V) where V is the number of nodes.
//
// Space complexity is O(V) for tracking used node IDs.
func ResolveSpanOverlaps(d *dag.DAG) {
	_ = resolveSpanOverlaps(d)
}

func resolveSpanOverlaps(d *dag.DAG) error {
	usedIDs := nodeIDSet(d.Nodes())
	// Process row boundaries by index (not row number) since separator insertion
	// shifts row numbers but not our position in the traversal.
	for i := 1; i < d.RowCount(); i++ {
		row := d.RowIDs()[i]
		for {
			inserted, err := insertSeparatorAt(d, row, usedIDs)
			if err != nil {
				return err
			}
			if !inserted {
				break
			}
			row = d.RowIDs()[i] // re-fetch: same index, new row number
		}
	}
	return nil
}

func insertSeparatorAt(d *dag.DAG, row int, usedIDs map[string]struct{}) (bool, error) {
	children := d.NodesInRow(row)
	if len(children) < 2 {
		return false, nil
	}

	// Rows containing subdividers are still processed: eligibleForSeparation
	// excludes parents whose children include subdividers, and
	// canInsertBetween keeps separator ranges away from subdivider columns,
	// so the non-subdivider portion of the row can be untangled safely.

	sorted := slices.Clone(children)
	slices.SortFunc(sorted, func(a, b *dag.Node) int { return cmp.Compare(a.ID, b.ID) })

	if ranges := findOverlappingSpans(d, sorted); len(ranges) > 0 {
		shiftRowsDown(d, row)
		// Insert only the first range, then signal the caller to re-run the
		// overlap analysis. The insertion rewires edges and shifts rows, so
		// the remaining ranges were computed against a stale graph.
		if err := insertSeparator(d, row, sorted, ranges[0], usedIDs); err != nil {
			return false, err
		}
		return true, nil
	}
	return false, nil
}

type span struct{ lo, hi int }

func findOverlappingSpans(d *dag.DAG, children []*dag.Node) []span {
	if len(children) < 2 {
		return nil
	}

	childPos := dag.NodePosMap(children)
	overlapCounts := make([]int, len(children)-1)
	targetRow := children[0].Row

	for _, parent := range d.NodesInRow(targetRow - 1) {
		if !eligibleForSeparation(d, parent, targetRow) {
			continue
		}

		if indices := childPositions(d.Children(parent.ID), childPos); len(indices) >= 2 {
			minIdx, maxIdx := slices.Min(indices), slices.Max(indices)
			for i := minIdx; i < maxIdx; i++ {
				if canInsertBetween(children, i) {
					overlapCounts[i]++
				}
			}
		}
	}

	return collectRanges(overlapCounts)
}

func eligibleForSeparation(d *dag.DAG, parent *dag.Node, targetRow int) bool {
	children := d.ChildrenInRow(parent.ID, targetRow)
	if len(children) < 2 || len(children) != len(d.Children(parent.ID)) {
		return false
	}
	for _, childID := range children {
		if n, ok := d.Node(childID); ok && n.IsSubdivider() {
			return false
		}
	}
	return true
}

func childPositions(childIDs []string, posMap map[string]int) []int {
	var indices []int
	for _, id := range childIDs {
		if pos, ok := posMap[id]; ok {
			indices = append(indices, pos)
		}
	}
	return indices
}

// canInsertBetween reports whether a separator range may cover the gap
// between children[i] and children[i+1]. Gaps adjacent to a subdivider are
// excluded: ranges are built only from gaps that pass this check, which
// guarantees no subdivider node ever falls inside an affected range and gets
// rerouted through a separator (breaking its vertical column).
func canInsertBetween(children []*dag.Node, i int) bool {
	if i < 0 || i+1 >= len(children) {
		return true
	}
	return !children[i].IsSubdivider() && !children[i+1].IsSubdivider()
}

func collectRanges(overlapCounts []int) []span {
	var ranges []span
	for i := 0; i < len(overlapCounts); i++ {
		if overlapCounts[i] >= 2 {
			start := i
			for i < len(overlapCounts) && overlapCounts[i] >= 2 {
				i++
			}
			ranges = append(ranges, span{start, i})
			i--
		}
	}
	return ranges
}

func shiftRowsDown(d *dag.DAG, fromRow int) {
	nodes := d.Nodes()
	newRows := make(map[string]int, len(nodes))
	for _, n := range nodes {
		row := n.Row
		if row >= fromRow {
			row++
		}
		newRows[n.ID] = row
	}
	d.SetRows(newRows)
}

func insertSeparator(d *dag.DAG, row int, children []*dag.Node, r span, usedIDs map[string]struct{}) error {
	separatorID := uniqueID(row, children[r.lo].ID, children[r.hi].ID, usedIDs)
	if err := d.AddNode(dag.Node{
		ID:   separatorID,
		Row:  row,
		Kind: dag.NodeKindAuxiliary,
	}); err != nil {
		return fmt.Errorf("add separator node %q: %w", separatorID, err)
	}

	affectedChildren := make(map[string]struct{}, r.hi-r.lo+1)
	for i := r.lo; i <= r.hi; i++ {
		affectedChildren[children[i].ID] = struct{}{}
	}

	// Collect metadata from edges being rerouted so it can be preserved
	// on the separator→child edges.
	childMeta := make(map[string]dag.Metadata)
	parents := make(map[string]struct{})
	for _, e := range d.Edges() {
		if src, ok := d.Node(e.From); ok && src.Row == row-1 {
			if _, affected := affectedChildren[e.To]; affected {
				parents[e.From] = struct{}{}
				if len(e.Meta) > 0 {
					childMeta[e.To] = e.Meta
				}
				d.RemoveEdge(e.From, e.To)
			}
		}
	}

	for parent := range parents {
		if err := d.AddEdge(dag.Edge{From: parent, To: separatorID}); err != nil {
			return fmt.Errorf("add separator edge %q -> %q: %w", parent, separatorID, err)
		}
	}

	for child := range affectedChildren {
		if err := d.AddEdge(dag.Edge{From: separatorID, To: child, Meta: childMeta[child]}); err != nil {
			return fmt.Errorf("add separator edge %q -> %q: %w", separatorID, child, err)
		}
	}
	return nil
}

func uniqueID(row int, firstChild, lastChild string, usedIDs map[string]struct{}) string {
	clean := func(s string) string { return strings.ReplaceAll(s, "_", "") }
	base := fmt.Sprintf("Sep_%d_%s_%s", row, clean(firstChild), clean(lastChild))

	id := base
	for i := 1; ; i++ {
		if _, exists := usedIDs[id]; !exists {
			usedIDs[id] = struct{}{}
			return id
		}
		id = fmt.Sprintf("%s__%d", base, i)
	}
}

func nodeIDSet(nodes []*dag.Node) map[string]struct{} {
	m := make(map[string]struct{}, len(nodes))
	for _, n := range nodes {
		m[n.ID] = struct{}{}
	}
	return m
}
