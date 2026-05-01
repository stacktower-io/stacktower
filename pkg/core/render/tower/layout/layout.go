package layout

import (
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

// EnsureLayered ensures the graph has row assignments for tower layout.
// If the graph has no rows assigned (MaxRow == 0), this assigns layers.
// This modifies the graph in place.
func EnsureLayered(g *dag.DAG) {
	if g.MaxRow() == 0 && g.EdgeCount() > 0 {
		transform.AssignLayers(g)
	}
}

// Build computes a physical layout for the given DAG within the specified
// width and height constraints. It applies row ordering, width computation,
// and coordinate assignment.
//
// Build requires that the graph has row assignments. If the graph was loaded
// from a file without normalization, call EnsureLayered first, or the caller
// should handle layer assignment.
func Build(g *dag.DAG, width, height float64, opts ...Option) Layout {
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
	blocks := assembleBlocks(g, orders, widths, heights, bottoms, marginX, marginY)

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

	maxRow := 0
	for r := range heights {
		maxRow = max(maxRow, r)
	}

	bottoms := make(map[int]float64, len(heights))
	y := 0.0
	for r := range maxRow + 1 {
		if h, ok := heights[r]; ok {
			bottoms[r] = y
			y += h
		}
	}
	return bottoms
}

func assembleBlocks(g *dag.DAG, orders map[int][]string, widths map[string]float64, heights, bottoms map[int]float64, marginX, marginY float64) map[string]Block {
	blocks := make(map[string]Block, g.NodeCount())
	for row, ids := range orders {
		x := marginX
		y := bottoms[row] + marginY
		h := heights[row]

		for _, id := range ids {
			w := widths[id]
			blocks[id] = Block{
				NodeID: id,
				Left:   x,
				Right:  x + w,
				Bottom: y,
				Top:    y + h,
			}
			x += w
		}
	}
	return blocks
}
