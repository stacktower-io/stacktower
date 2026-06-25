package transform

import (
	"math"
	"testing"

	"github.com/stacktower-io/stacktower/pkg/core/dag"
	"github.com/stacktower-io/stacktower/pkg/core/render/tower/layout"
)

// TestEnsureMinimumOverlap_MergedColumn reproduces the "pydantic" bug: a
// master whose subdivider segments were merged into a single column keyed by
// the master ID. The overlap repair must resolve subdivider edge endpoints
// through EffectiveID, and a distant column must not block the repair.
func TestEnsureMinimumOverlap_MergedColumn(t *testing.T) {
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "pyd", Row: 1})
	_ = g.AddNode(dag.Node{ID: "pyd_sub_2", Row: 2, Kind: dag.NodeKindSubdivider, MasterID: "pyd"})
	_ = g.AddNode(dag.Node{ID: "tinsp", Row: 3})
	_ = g.AddNode(dag.Node{ID: "tall", Row: 1}) // merged column spanning all rows
	_ = g.AddEdge(dag.Edge{From: "pyd", To: "pyd_sub_2"})
	_ = g.AddEdge(dag.Edge{From: "pyd_sub_2", To: "tinsp"})

	// Post-merge, post-shrink state: pyd's column (rows 1-2) no longer
	// reaches tinsp (row 3). "tall" is a full-height column far to the
	// left; "far" is a column next to tinsp that blocks tinsp from moving
	// but not pyd from expanding.
	blocks := map[string]layout.Block{
		"pyd":   {NodeID: "pyd", Left: 678, Right: 708, Bottom: 200, Top: 400},
		"tinsp": {NodeID: "tinsp", Left: 732, Right: 762, Bottom: 100, Top: 200},
		"tall":  {NodeID: "tall", Left: 0, Right: 100, Bottom: 0, Top: 400},
		"far@x": {NodeID: "far", Left: 710, Right: 730, Bottom: 0, Top: 200},
	}

	ensureMinimumOverlap(g, blocks, 10)

	p, c := blocks["pyd"], blocks["tinsp"]
	if ov := calcOverlap(p.Left, p.Right, c.Left, c.Right); ov < 10 {
		t.Errorf("merged column should overlap its child: overlap %.1f < 10 (pyd [%.1f,%.1f], tinsp [%.1f,%.1f])",
			ov, p.Left, p.Right, c.Left, c.Right)
	}
}

// TestEnsureMinimumOverlap_NoIntrusionIntoOtherRows reproduces the beam
// overlap bug: a tall merged column expanding to reach its child must not
// intrude into a block of an intermediate row it passes through (e.g. the
// separator beam behind pydantic's column).
func TestEnsureMinimumOverlap_NoIntrusionIntoOtherRows(t *testing.T) {
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "pyd", Row: 1})
	_ = g.AddNode(dag.Node{ID: "pyd_sub_2", Row: 2, Kind: dag.NodeKindSubdivider, MasterID: "pyd"})
	_ = g.AddNode(dag.Node{ID: "kid", Row: 3})
	_ = g.AddNode(dag.Node{ID: "beam", Row: 2, Kind: dag.NodeKindAuxiliary})
	_ = g.AddEdge(dag.Edge{From: "pyd", To: "pyd_sub_2"})
	_ = g.AddEdge(dag.Edge{From: "pyd_sub_2", To: "kid"})

	// pyd's merged column spans rows 1-2 (y 0..200); the beam occupies row 2
	// (y 100..200) directly to its left; the child sits below, far left.
	blocks := map[string]layout.Block{
		"pyd":  {NodeID: "pyd", Left: 620, Right: 700, Bottom: 0, Top: 200},
		"beam": {NodeID: "beam", Left: 360, Right: 620, Bottom: 100, Top: 200},
		"kid":  {NodeID: "kid", Left: 540, Right: 580, Bottom: 200, Top: 300},
	}

	ensureMinimumOverlap(g, blocks, 10)

	p, b := blocks["pyd"], blocks["beam"]
	if ov := calcOverlap(p.Left, p.Right, b.Left, b.Right); ov > 1 {
		t.Errorf("column must not intrude into the beam: overlap %.1f (pyd [%.1f,%.1f], beam [%.1f,%.1f])",
			ov, p.Left, p.Right, b.Left, b.Right)
	}
}

func TestRandomize_Deterministic(t *testing.T) {
	layout := buildTestLayout()

	result1 := Randomize(layout, buildTestDAG(), 12345, nil)
	result2 := Randomize(layout, buildTestDAG(), 12345, nil)

	for id := range layout.Blocks {
		b1, b2 := result1.Blocks[id], result2.Blocks[id]
		if b1.Left != b2.Left || b1.Right != b2.Right {
			t.Errorf("block %s not deterministic: (%.2f, %.2f) vs (%.2f, %.2f)",
				id, b1.Left, b1.Right, b2.Left, b2.Right)
		}
	}
}

func TestRandomize_WidthShrinks(t *testing.T) {
	layout := buildTestLayout()
	g := buildTestDAG()
	result := Randomize(layout, g, 42, nil)

	slotWidth := layout.Blocks["A"].Width()

	blockA := result.Blocks["A"]
	if blockA.Width() != slotWidth {
		t.Errorf("root block A should not shrink, got width %.2f, want %.2f",
			blockA.Width(), slotWidth)
	}

	blockB := result.Blocks["B"]
	blockC := result.Blocks["C"]

	if blockB.Width() >= slotWidth {
		t.Errorf("block B should shrink, got width %.2f == slot %.2f", blockB.Width(), slotWidth)
	}
	if blockC.Width() >= slotWidth {
		t.Errorf("block C should shrink, got width %.2f == slot %.2f", blockC.Width(), slotWidth)
	}

	for id, original := range layout.Blocks {
		if id == "A" {
			continue
		}
		randomized := result.Blocks[id]
		if randomized.Width() > original.Width()+0.01 {
			t.Errorf("block %s width increased: %.2f > %.2f", id, randomized.Width(), original.Width())
		}
		shrinkRatio := (original.Width() - randomized.Width()) / original.Width()
		if shrinkRatio > 0.60 {
			t.Errorf("block %s shrink ratio = %.2f%%, want <= 60%%", id, shrinkRatio*100)
		}
	}
}

func TestRandomize_DoesNotShrinkAuxiliaryBeams(t *testing.T) {
	l := layout.Layout{
		Blocks: map[string]layout.Block{
			"root": {NodeID: "root", Left: 0, Right: 100, Bottom: 50, Top: 100},
			"sep":  {NodeID: "sep", Left: 10, Right: 90, Bottom: 25, Top: 50},
			"leaf": {NodeID: "leaf", Left: 0, Right: 100, Bottom: 0, Top: 25},
		},
		RowOrders: map[int][]string{
			0: {"root"},
			1: {"sep"},
			2: {"leaf"},
		},
	}
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "root", Row: 0})
	_ = g.AddNode(dag.Node{ID: "sep", Row: 1, Kind: dag.NodeKindAuxiliary})
	_ = g.AddNode(dag.Node{ID: "leaf", Row: 2})

	result := Randomize(l, g, 42, &Options{
		WidthShrink:   1,
		MinBlockWidth: 1,
		MinGap:        5,
		MinOverlap:    0,
	})

	if got, want := result.Blocks["sep"], l.Blocks["sep"]; got.Left != want.Left || got.Right != want.Right {
		t.Fatalf("auxiliary beam changed: got %.1f..%.1f, want %.1f..%.1f", got.Left, got.Right, want.Left, want.Right)
	}
	if got, want := result.Blocks["leaf"].Width(), l.Blocks["leaf"].Width(); got >= want {
		t.Fatalf("non-auxiliary block did not shrink: got %.1f, want < %.1f", got, want)
	}
}

func TestRandomize_StaysInBounds(t *testing.T) {
	layout := buildTestLayout()
	result := Randomize(layout, buildTestDAG(), 99, nil)

	for id, original := range layout.Blocks {
		b := result.Blocks[id]
		if b.Left < original.Left || b.Right > original.Right {
			t.Errorf("block %s bounds [%.2f, %.2f] outside slot [%.2f, %.2f]",
				id, b.Left, b.Right, original.Left, original.Right)
		}
	}
}

func TestRandomize_PreservesAllBlocks(t *testing.T) {
	layout := buildTestLayout()
	result := Randomize(layout, buildTestDAG(), 123, nil)

	if got, want := len(result.Blocks), len(layout.Blocks); got != want {
		t.Fatalf("block count = %d, want %d", got, want)
	}

	for id := range layout.Blocks {
		if _, ok := result.Blocks[id]; !ok {
			t.Errorf("missing block %s", id)
		}
	}
}

func TestRandomize_ZeroVariation(t *testing.T) {
	layout := buildTestLayout()
	result := Randomize(layout, buildTestDAG(), 42, &Options{
		WidthShrink: 0,
	})

	for id, original := range layout.Blocks {
		randomized := result.Blocks[id]
		if randomized.Left != original.Left || randomized.Right != original.Right {
			t.Errorf("block %s changed with zero variation: (%.2f, %.2f) vs (%.2f, %.2f)",
				id, randomized.Left, randomized.Right, original.Left, original.Right)
		}
	}
}

func TestRandomize_CustomParameters(t *testing.T) {
	layout := buildTestLayout()
	g := buildTestDAG()
	result := Randomize(layout, g, 77, &Options{
		WidthShrink:   0.25,
		MinBlockWidth: 20.0,
	})

	slotWidth := layout.Blocks["A"].Width()

	blockA := result.Blocks["A"]
	if blockA.Width() != slotWidth {
		t.Errorf("root block A should not shrink, got width %.2f, want %.2f",
			blockA.Width(), slotWidth)
	}

	for id, original := range layout.Blocks {
		if id == "A" {
			continue
		}
		randomized := result.Blocks[id]
		shrinkRatio := (original.Width() - randomized.Width()) / original.Width()
		if shrinkRatio > 0.25 {
			t.Errorf("block %s shrink ratio = %.2f%%, want <= 25%%", id, shrinkRatio*100)
		}
	}
}

func TestRandomize_PreservesVertical(t *testing.T) {
	layout := buildTestLayout()
	result := Randomize(layout, buildTestDAG(), 11, nil)

	for id, original := range layout.Blocks {
		randomized := result.Blocks[id]
		if randomized.Bottom != original.Bottom || randomized.Top != original.Top {
			t.Errorf("block %s vertical = (%.2f, %.2f), want (%.2f, %.2f)",
				id, randomized.Bottom, randomized.Top, original.Bottom, original.Top)
		}
	}
}

func TestRandomize_PreservesLayoutMetadata(t *testing.T) {
	layout := buildTestLayout()
	result := Randomize(layout, buildTestDAG(), 55, nil)

	if got, want := result.FrameWidth, layout.FrameWidth; got != want {
		t.Errorf("FrameWidth = %.2f, want %.2f", got, want)
	}
	if got, want := result.FrameHeight, layout.FrameHeight; got != want {
		t.Errorf("FrameHeight = %.2f, want %.2f", got, want)
	}
	if got, want := result.MarginX, layout.MarginX; got != want {
		t.Errorf("MarginX = %.2f, want %.2f", got, want)
	}
	if got, want := result.MarginY, layout.MarginY; got != want {
		t.Errorf("MarginY = %.2f, want %.2f", got, want)
	}
}

func TestRandomize_NoHorizontalOverlap(t *testing.T) {
	layout := buildMultiColumnLayout()
	result := Randomize(layout, buildMultiColumnDAG(), 123, nil)

	for row, ids := range result.RowOrders {
		for i := 0; i < len(ids)-1; i++ {
			curr, next := result.Blocks[ids[i]], result.Blocks[ids[i+1]]
			if curr.Right > next.Left {
				t.Errorf("row %d: block %s (right=%.2f) overlaps %s (left=%.2f)",
					row, ids[i], curr.Right, ids[i+1], next.Left)
			}
		}
	}
}

func TestRandomize_ClampsToValidRange(t *testing.T) {
	layout := buildTestLayout()
	result := Randomize(layout, buildTestDAG(), 42, &Options{
		WidthShrink:   5.0,
		MinBlockWidth: 20.0,
	})

	for id := range layout.Blocks {
		if _, ok := result.Blocks[id]; !ok {
			t.Errorf("missing block %s after out-of-range parameters", id)
		}
	}
}

func TestRandomize_MinimumOverlap(t *testing.T) {
	layout := buildTestLayout()
	g := buildTestDAG()
	minOverlap := 40.0

	result := Randomize(layout, g, 999, &Options{
		WidthShrink:   0.8,
		MinBlockWidth: 20.0,
		MinGap:        5.0,
		MinOverlap:    minOverlap,
	})

	for _, edge := range g.Edges() {
		parent := result.Blocks[edge.From]
		child := result.Blocks[edge.To]

		overlap := math.Max(0, math.Min(parent.Right, child.Right)-math.Max(parent.Left, child.Left))
		if overlap < minOverlap-0.01 {
			t.Errorf("edge %s->%s: overlap %.2f < min %.2f",
				edge.From, edge.To, overlap, minOverlap)
		}
	}
}

func TestRandomize_MinimumOverlapComplex(t *testing.T) {
	layout := buildMultiColumnLayout()
	g := buildMultiColumnDAG()
	minOverlap := 50.0

	result := Randomize(layout, g, 777, &Options{
		WidthShrink:   0.9,
		MinBlockWidth: 20.0,
		MinGap:        5.0,
		MinOverlap:    minOverlap,
	})

	for _, edge := range g.Edges() {
		parent := result.Blocks[edge.From]
		child := result.Blocks[edge.To]

		overlap := math.Max(0, math.Min(parent.Right, child.Right)-math.Max(parent.Left, child.Left))
		if overlap < minOverlap-0.01 {
			t.Errorf("edge %s->%s: overlap %.2f < min %.2f (parent=[%.2f,%.2f], child=[%.2f,%.2f])",
				edge.From, edge.To, overlap, minOverlap,
				parent.Left, parent.Right, child.Left, child.Right)
		}
	}
}

func buildMultiColumnLayout() layout.Layout {
	return layout.Layout{
		FrameWidth:  800,
		FrameHeight: 400,
		MarginX:     20,
		MarginY:     20,
		Blocks: map[string]layout.Block{
			"A": {NodeID: "A", Left: 20, Right: 420, Bottom: 20, Top: 220},
			"B": {NodeID: "B", Left: 420, Right: 780, Bottom: 20, Top: 220},
			"C": {NodeID: "C", Left: 20, Right: 420, Bottom: 220, Top: 420},
			"D": {NodeID: "D", Left: 420, Right: 780, Bottom: 220, Top: 420},
		},
		RowOrders: map[int][]string{
			0: {"A", "B"},
			1: {"C", "D"},
		},
	}
}

func buildTestLayout() layout.Layout {
	return layout.Layout{
		FrameWidth:  800,
		FrameHeight: 600,
		MarginX:     20,
		MarginY:     20,
		Blocks: map[string]layout.Block{
			"A": {NodeID: "A", Left: 20, Right: 780, Bottom: 20, Top: 220},
			"B": {NodeID: "B", Left: 20, Right: 780, Bottom: 220, Top: 420},
			"C": {NodeID: "C", Left: 20, Right: 780, Bottom: 420, Top: 620},
		},
		RowOrders: map[int][]string{
			0: {"A"},
			1: {"B"},
			2: {"C"},
		},
	}
}

func buildTestDAG() *dag.DAG {
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "A", Row: 0})
	_ = g.AddNode(dag.Node{ID: "B", Row: 1})
	_ = g.AddNode(dag.Node{ID: "C", Row: 2})
	_ = g.AddEdge(dag.Edge{From: "A", To: "B"})
	_ = g.AddEdge(dag.Edge{From: "B", To: "C"})
	return g
}

func buildMultiColumnDAG() *dag.DAG {
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "A", Row: 0})
	_ = g.AddNode(dag.Node{ID: "B", Row: 0})
	_ = g.AddNode(dag.Node{ID: "C", Row: 1})
	_ = g.AddNode(dag.Node{ID: "D", Row: 1})
	_ = g.AddEdge(dag.Edge{From: "A", To: "C"})
	_ = g.AddEdge(dag.Edge{From: "B", To: "D"})
	return g
}
