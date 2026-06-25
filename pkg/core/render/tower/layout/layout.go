package layout

import (
	"log/slog"
	"slices"
	"time"

	"github.com/stacktower-io/stacktower/pkg/core/dag"
	"github.com/stacktower-io/stacktower/pkg/core/dag/transform"
	"github.com/stacktower-io/stacktower/pkg/core/render/tower/feature"
	"github.com/stacktower-io/stacktower/pkg/core/render/tower/ordering"
)

const (
	defaultAuxRatio       = 0.2
	defaultMarginRatio    = 0.05
	defaultOrdererTimeout = 60 * time.Second
)

// defaultOrderer is the default ordering algorithm used by Build.
// It uses OptimalSearch with a 60-second timeout to find minimum-crossing layouts.
// Callers that need a different orderer should use WithOrderer.
var defaultOrderer = ordering.OptimalSearch{Timeout: defaultOrdererTimeout}

// Layout represents the computed physical positions and dimensions of all
// blocks in a tower visualization, along with rendering metadata.
//
// This is the internal representation used during layout computation and rendering.
// For serialization (JSON files, API responses, caching), convert to graph.Layout
// using the Export() method. Use Parse() to convert back from serialized form.
//
// The key difference from graph.Layout:
//   - This type: optimized for computation (map-based block lookup, computed values)
//   - graph.Layout: optimized for serialization (slice-based, JSON-friendly)
type Layout struct {
	FrameWidth  float64
	FrameHeight float64
	Blocks      map[string]Block
	RowOrders   map[int][]string
	MarginX     float64
	MarginY     float64

	// Metadata fields for rendering configuration
	Style     string // Render style: "simple", "handdrawn"
	Seed      uint64 // Random seed for reproducible rendering
	Randomize bool   // Whether block widths were randomized
	Merged    bool   // Whether subdividers were merged

	// Nebraska contains maintainer ranking data (computed during layout)
	Nebraska []feature.NebraskaRanking
}

// Option configures the layout generation process.
type Option func(*config)

type config struct {
	orderer     ordering.Orderer
	auxRatio    float64
	marginRatio float64
	topDownFlow bool
}

// WithOrderer sets the algorithm used to determine the horizontal ordering
// of blocks in each row. Defaults to [ordering.OptimalSearch] with a 60-second timeout.
func WithOrderer(o ordering.Orderer) Option {
	return func(c *config) { c.orderer = o }
}

// WithAuxiliaryRatio sets the height of auxiliary rows (separator beams)
// relative to regular rows. Defaults to 0.2.
func WithAuxiliaryRatio(r float64) Option {
	return func(c *config) { c.auxRatio = r }
}

// WithMarginRatio sets the outer margin of the tower relative to the total
// frame size. Defaults to 0.05.
func WithMarginRatio(r float64) Option {
	return func(c *config) { c.marginRatio = r }
}

// WithTopDownWidths configures width computation to flow from parents to
// children (top-down). The default is bottom-up, where blocks are sized
// to support what is above them.
func WithTopDownWidths() Option {
	return func(c *config) { c.topDownFlow = true }
}

// NeedsLayering reports whether the graph requires (re-)layering before a
// tower layout can be computed. This catches both unlayered graphs (all rows
// zero) and graphs with stale or inconsistent row metadata, e.g. loaded from
// a hand-edited graph.json.
func NeedsLayering(g *dag.DAG) bool {
	return g.EdgeCount() > 0 && !transform.IsLayered(g)
}

// EnsureLayered ensures the graph has consistent row assignments for tower
// layout, re-running layer assignment when they are missing or stale.
// This modifies the graph in place.
func EnsureLayered(g *dag.DAG) {
	if NeedsLayering(g) {
		transform.AssignLayers(g)
	}
}

// Build computes a physical layout for the given DAG within the specified
// width and height constraints. It applies row ordering, width computation,
// and coordinate assignment.
//
// Build requires that the graph has been subdivided so every edge spans
// exactly one row and every column reaches the bottom. Passing an
// un-normalized graph (multi-row edges or mid-row sinks) will produce a
// layout with zero-width blocks and incorrect crossing counts. Call
// [transform.Normalize] or at minimum [EnsureLayered] + [transform.Subdivide]
// before Build to satisfy these invariants.
func Build(g *dag.DAG, width, height float64, opts ...Option) Layout {
	if g.EdgeCount() > 0 {
		for _, e := range g.EdgesIter() {
			src, _ := g.Node(e.From)
			dst, _ := g.Node(e.To)
			if src != nil && dst != nil && dst.Row != src.Row+1 {
				slog.Warn("layout.Build: graph has multi-row edges; results may be incorrect — call transform.Normalize first",
					"from", e.From, "to", e.To, "fromRow", src.Row, "toRow", dst.Row)
				break
			}
		}
	}
	cfg := config{
		orderer:     defaultOrderer,
		auxRatio:    defaultAuxRatio,
		marginRatio: defaultMarginRatio,
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	marginX := width * cfg.marginRatio
	marginY := height * cfg.marginRatio

	orders := cfg.orderer.OrderRows(g)
	var widths map[string]float64
	if cfg.topDownFlow {
		widths = ComputeWidths(g, orders, width-2*marginX)
	} else {
		widths = ComputeWidthsBottomUp(g, orders, width-2*marginX)
	}
	heights := computeRowHeights(g, height-2*marginY, cfg.auxRatio)
	bottoms := computeRowBottoms(heights)
	blocks := assembleBlocks(g, orders, widths, heights, bottoms, marginX, marginY, width-2*marginX)

	return Layout{
		FrameWidth:  width,
		FrameHeight: height,
		Blocks:      blocks,
		RowOrders:   orders,
		MarginX:     marginX,
		MarginY:     marginY,
	}
}

func computeRowHeights(g *dag.DAG, totalHeight, auxRatio float64) map[int]float64 {
	rows := g.RowIDs()
	if len(rows) == 0 {
		return nil
	}

	isAux := make([]bool, len(rows))
	auxCount := 0
	for i, r := range rows {
		nodes := g.NodesInRow(r)
		allAuxiliary := len(nodes) > 0 && !slices.ContainsFunc(nodes, func(n *dag.Node) bool {
			return !n.IsAuxiliary()
		})
		isAux[i] = allAuxiliary
		if allAuxiliary {
			auxCount++
		}
	}

	regularCount := float64(len(rows) - auxCount)
	unit := totalHeight / (regularCount + float64(auxCount)*auxRatio)

	heights := make(map[int]float64, len(rows))
	for i, r := range rows {
		if isAux[i] {
			heights[r] = unit * auxRatio
		} else {
			heights[r] = unit
		}
	}
	return heights
}

func computeRowBottoms(heights map[int]float64) map[int]float64 {
	if len(heights) == 0 {
		return nil
	}

	// Stack rows in ascending row order. Iterating the sorted keys (rather
	// than 0..maxRow) guarantees every row with a height gets a bottom, even
	// if row IDs don't start at 0 or have gaps — a missed assignment would
	// silently place that row's blocks at y=0, overlapping other rows.
	rows := make([]int, 0, len(heights))
	for r := range heights {
		rows = append(rows, r)
	}
	slices.Sort(rows)

	bottoms := make(map[int]float64, len(heights))
	y := 0.0
	for _, r := range rows {
		bottoms[r] = y
		y += heights[r]
	}
	return bottoms
}

// assembleBlocks places blocks row by row from the bottom up. Within a row,
// blocks are normally packed edge-to-edge in order, but members of vertical
// subdivider chains (long-edge pipes and foundation pillars) are pinned to
// the exact horizontal extent of their segment in the row below, so a column
// keeps one width and one position across all of its rows instead of
// wobbling with each row's independent packing. Flexible blocks between
// pinned columns share the remaining span proportionally to their computed
// widths, keeping every row a contiguous partition of the frame.
func assembleBlocks(g *dag.DAG, orders map[int][]string, widths map[string]float64, heights, bottoms map[int]float64, marginX, marginY, innerWidth float64) map[string]Block {
	rows := make([]int, 0, len(orders))
	for r := range orders {
		rows = append(rows, r)
	}
	slices.Sort(rows)

	mates := chainMatesBelow(g)
	blocks := make(map[string]Block, g.NodeCount())
	rightEdge := marginX + innerWidth

	for i := len(rows) - 1; i >= 0; i-- {
		row := rows[i]
		ids := orders[row]
		y := bottoms[row] + marginY
		h := heights[row]

		pins := selectPins(ids, mates, blocks, marginX, rightEdge)

		cursor := marginX
		prevIdx := -1
		for p := 0; p <= len(pins); p++ {
			segEnd := rightEdge
			pinIdx := len(ids)
			if p < len(pins) {
				segEnd = pins[p].left
				pinIdx = pins[p].idx
			}

			flex := ids[prevIdx+1 : pinIdx]
			switch {
			case len(flex) > 0:
				placeFlexible(blocks, flex, widths, cursor, segEnd, y, h)
			case segEnd > cursor+eps && p > 0:
				// Nothing flexible fills this span: stretch the left
				// flanking pinned column for this row only. The column gets
				// a step here, which the merge pass treats as a boundary.
				stretched := blocks[ids[pins[p-1].idx]]
				stretched.Right = segEnd
				blocks[ids[pins[p-1].idx]] = stretched
			case segEnd > cursor+eps:
				// Gap before the first pin with nothing to fill it: extend
				// the pinned column to the left frame edge.
				pins[p].left = cursor
			}

			if p < len(pins) {
				id := ids[pinIdx]
				blocks[id] = Block{NodeID: id, Left: pins[p].left, Right: pins[p].right, Bottom: y, Top: y + h}
				cursor = pins[p].right
				prevIdx = pinIdx
			}
		}
	}
	return blocks
}
