package transform

import (
	"maps"
	"math/rand/v2"
	"slices"

	"github.com/stacktower-io/stacktower/pkg/core/dag"
	"github.com/stacktower-io/stacktower/pkg/core/render/tower/layout"
)

// Options configures the randomization behavior for [Randomize].
type Options struct {
	// WidthShrink is the maximum shrink factor applied to blocks (0-1).
	// Higher values create more width variation. Default: 0.85.
	WidthShrink float64

	// MinBlockWidth is the minimum allowed block width in pixels.
	// Blocks will not shrink below this size. Default: 30.
	MinBlockWidth float64

	// MinGap is the minimum gap between adjacent blocks in pixels. Default: 5.
	MinGap float64

	// MinOverlap is the minimum horizontal overlap required between connected
	// blocks (parent-child pairs). Blocks are expanded if needed. Default: 10.
	MinOverlap float64
}

var defaultOpts = Options{
	WidthShrink:   0.85,
	MinBlockWidth: 30.0,
	MinGap:        5.0,
	MinOverlap:    10.0,
}

// Randomize applies controlled random variation to block widths.
// It creates a checkerboard pattern by shrinking alternating rows, which
// mimics hand-drawn diagrams and adds visual interest.
//
// The seed ensures reproducible randomness—the same seed produces identical
// layouts. Pass nil for opts to use defaults.
//
// After shrinking, the function ensures connected blocks maintain minimum
// overlap so dependency edges remain visually clear.
func Randomize(l layout.Layout, g *dag.DAG, seed uint64, opts *Options) layout.Layout {
	if opts == nil {
		opts = &defaultOpts
	}
	if shrink := max(0.0, min(opts.WidthShrink, 1.0)); shrink == 0 {
		return l
	}

	blocks := maps.Clone(l.Blocks)
	rows := sortedRows(l.RowOrders)
	rng := rand.New(rand.NewPCG(seed, seed^0xdeadbeef))

	shrinkCheckerboard(g, l.RowOrders, blocks, rows, rng, opts)
	ensureMinimumOverlap(g, blocks, opts.MinOverlap)

	l.Blocks = blocks
	return l
}

func shrinkCheckerboard(g *dag.DAG, orders map[int][]string, blocks map[string]layout.Block, rows []int, rng *rand.Rand, opts *Options) {
	shrink := max(0, min(opts.WidthShrink, 1))
	for rowIdx, row := range rows {
		if rowIdx == 0 {
			continue
		}
		for _, nodeID := range orders[row] {
			if g != nil {
				if n, ok := g.Node(nodeID); ok && n.IsAuxiliary() {
					continue
				}
			}
			node := blocks[nodeID]
			center := (node.Left + node.Right) / 2
			width := node.Right - node.Left - 2*opts.MinGap
			if rowIdx%2 == 1 {
				width *= 1 - rng.Float64()*shrink
			}
			width = max(width, opts.MinBlockWidth)
			node.Left = center - width/2
			node.Right = center + width/2
			blocks[nodeID] = node
		}
	}
}

func sortedRows(orders map[int][]string) []int {
	rows := slices.Collect(maps.Keys(orders))
	slices.Sort(rows)
	return rows
}

func ensureMinimumOverlap(g *dag.DAG, blocks map[string]layout.Block, minOverlap float64) {
	edges := g.Edges()

	// Merged subdivider columns are keyed by their master ID, so edges that
	// reference a subdivider segment (e.g. "pydantic_sub_2") must be resolved
	// through EffectiveID or every contact constraint along a merged column
	// would be silently skipped, letting shrinking break parent-child contact.
	resolve := func(id string) (string, layout.Block, bool) {
		if b, ok := blocks[id]; ok {
			return id, b, true
		}
		if n, ok := g.Node(id); ok {
			if b, ok := blocks[n.EffectiveID()]; ok {
				return n.EffectiveID(), b, true
			}
		}
		return "", layout.Block{}, false
	}

	for range 10 {
		changed := false
		for _, edge := range edges {
			fromID, parent, okP := resolve(edge.From)
			toID, child, okC := resolve(edge.To)
			if !okP || !okC || fromID == toID {
				// fromID == toID: both endpoints merged into the same
				// column (e.g. master -> its own subdivider segment).
				continue
			}

			currentOverlap := calcOverlap(parent.Left, parent.Right, child.Left, child.Right)
			if currentOverlap >= minOverlap {
				continue
			}

			// Each candidate is clamped to reach exactly minOverlap against
			// the other block's current position, so a single-sided move is
			// always sufficient on its own. Expanding further (e.g. fully
			// covering the child) would intrude into unrelated blocks, such
			// as a separator beam behind a tall merged column.
			newParent, newChild := parent, child
			parentCenter := (parent.Left + parent.Right) / 2
			childCenter := (child.Left + child.Right) / 2

			if parentCenter < childCenter {
				newParent.Right = max(parent.Right, child.Left+minOverlap)
				newChild.Left = min(child.Left, parent.Right-minOverlap)
			} else {
				newParent.Left = min(parent.Left, child.Right-minOverlap)
				newChild.Right = max(child.Right, parent.Left+minOverlap)
			}

			parentCollides := wouldCollide(fromID, newParent, blocks)
			childCollides := wouldCollide(toID, newChild, blocks)

			switch {
			case !parentCollides && !childCollides:
				blocks[fromID] = newParent
				blocks[toID] = newChild
				changed = true
			case !parentCollides:
				blocks[fromID] = newParent
				changed = true
			case !childCollides:
				blocks[toID] = newChild
				changed = true
			}
		}
		if !changed {
			break
		}
	}
}

// wouldCollide reports whether the proposed bounds would intrude into any
// other block. Blocks are compared geometrically over their full extents, so
// merged columns spanning several rows are respected everywhere they exist —
// not just in the row that lists them. Blocks in adjacent rows merely touch
// vertically and never register as collisions; the ±1 tolerances allow
// blocks to share edges.
func wouldCollide(id string, newBounds layout.Block, blocks map[string]layout.Block) bool {
	for otherID, other := range blocks {
		if otherID == id {
			continue
		}
		if newBounds.Top > other.Bottom+1 && newBounds.Bottom < other.Top-1 &&
			newBounds.Right > other.Left+1 && newBounds.Left < other.Right-1 {
			return true
		}
	}
	return false
}

func calcOverlap(a1, a2, b1, b2 float64) float64 {
	return max(0, min(a2, b2)-max(a1, b1))
}
