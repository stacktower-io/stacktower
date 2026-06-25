package layout

import (
	"github.com/stacktower-io/stacktower/pkg/core/dag"
)

// minFlexWidth is the minimum width reserved for each flexible block squeezed
// between pinned columns. A pin that would compress its flexible neighbors
// below this is dropped, letting the column bend at that row instead of
// producing unreadable slivers.
const minFlexWidth = 1.0

// chainMatesBelow computes, for every node that anchors a rigid vertical
// column, the ID of the subdivider segment directly below it in that column.
//
// A node anchors a rigid column when it is:
//   - a subdivider segment with another segment of the same master in the
//     row directly below (the interior of a pipe or foundation pillar), or
//   - a regular leaf whose only child is its own sink-extension subdivider
//     (the visible top of a foundation pillar).
//
// Masters with real children are deliberately NOT chained to their pipe: the
// pipe carries only the long edges, while the master block keeps the full
// width computed from its subtree.
//
// Masters with more than one subdivider in a single row (parallel chains in
// graphs predating shared subdivision) are skipped entirely — there is no
// unambiguous column to align.
func chainMatesBelow(g *dag.DAG) map[string]string {
	type slot struct {
		id  string
		dup bool
	}
	below := make(map[string]map[int]slot)
	for _, n := range g.Nodes() {
		if !n.IsSubdivider() || n.MasterID == "" {
			continue
		}
		rows := below[n.MasterID]
		if rows == nil {
			rows = make(map[int]slot)
			below[n.MasterID] = rows
		}
		s := rows[n.Row]
		if s.id != "" {
			s.dup = true
		} else {
			s.id = n.ID
		}
		rows[n.Row] = s
	}

	mates := make(map[string]string)
	for _, n := range g.Nodes() {
		rows := below[n.EffectiveID()]
		if rows == nil {
			continue
		}
		mate, ok := rows[n.Row+1]
		if !ok || mate.dup {
			continue
		}
		if !n.IsSubdivider() {
			children := g.Children(n.ID)
			if len(children) != 1 || children[0] != mate.id {
				continue
			}
		}
		mates[n.ID] = mate.id
	}
	return mates
}

// pinnedBlock is a block in the current row whose horizontal extent is fixed
// to its chain mate's extent in the row below.
type pinnedBlock struct {
	idx         int // position within the row's order
	left, right float64
}

// selectPins returns the usable pins for a row, in row order. A pin is only
// usable when it stays in row order relative to already accepted pins and
// leaves at least minFlexWidth per flexible block between them; otherwise it
// is dropped and the column bends at this row.
func selectPins(ids []string, mates map[string]string, blocks map[string]Block, marginX, rightEdge float64) []pinnedBlock {
	var pins []pinnedBlock
	lastRight := marginX
	lastIdx := -1
	for j, id := range ids {
		mate, ok := mates[id]
		if !ok {
			continue
		}
		b, ok := blocks[mate]
		if !ok {
			continue
		}
		need := float64(j-lastIdx-1) * minFlexWidth
		if b.Left+eps < lastRight+need || b.Right > rightEdge+eps {
			continue
		}
		pins = append(pins, pinnedBlock{idx: j, left: b.Left, right: b.Right})
		lastRight = b.Right
		lastIdx = j
	}

	// Flexible blocks after the last pin need room up to the right frame
	// edge too; drop pins from the right until they fit.
	for len(pins) > 0 {
		last := pins[len(pins)-1]
		need := float64(len(ids)-last.idx-1) * minFlexWidth
		if last.right+need <= rightEdge+eps {
			break
		}
		pins = pins[:len(pins)-1]
	}
	return pins
}

// placeFlexible packs the given blocks edge-to-edge into [lo, hi], scaling
// their computed widths proportionally to fill the span exactly.
func placeFlexible(blocks map[string]Block, ids []string, widths map[string]float64, lo, hi, y, h float64) {
	var sum float64
	for _, id := range ids {
		sum += widths[id]
	}
	space := hi - lo
	x := lo
	for k, id := range ids {
		var w float64
		if sum > eps {
			w = widths[id] / sum * space
		} else {
			w = space / float64(len(ids))
		}
		right := x + w
		if k == len(ids)-1 {
			right = hi // absorb FP drift so the next pin stays exact
		}
		blocks[id] = Block{NodeID: id, Left: x, Right: right, Bottom: y, Top: y + h}
		x = right
	}
}
