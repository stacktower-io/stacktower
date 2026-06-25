package layout

import (
	"math"
	"testing"

	"github.com/stacktower-io/stacktower/pkg/core/dag"
	"github.com/stacktower-io/stacktower/pkg/core/dag/transform"
)

// TestAssembleBlocks_PillarAlignment reproduces the "two colorama blocks"
// artifact: a leaf with a sink-extension pillar whose row-local packing put
// the leaf and its pillar segment at different x positions. The pinned sweep
// must give both the exact same horizontal extent so MergeSubdividers fuses
// them into one column.
func TestAssembleBlocks_PillarAlignment(t *testing.T) {
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "a", Row: 1})
	_ = g.AddNode(dag.Node{ID: "b", Row: 1})
	_ = g.AddNode(dag.Node{ID: "e", Row: 1})
	_ = g.AddNode(dag.Node{ID: "c", Row: 2})
	_ = g.AddNode(dag.Node{ID: "c2", Row: 2})
	_ = g.AddNode(dag.Node{ID: "d", Row: 2})
	_ = g.AddNode(dag.Node{ID: "b_sub_2", Row: 2, Kind: dag.NodeKindSubdivider, MasterID: "b"})
	_ = g.AddEdge(dag.Edge{From: "a", To: "c"})
	_ = g.AddEdge(dag.Edge{From: "a", To: "c2"})
	_ = g.AddEdge(dag.Edge{From: "e", To: "c2"})
	_ = g.AddEdge(dag.Edge{From: "e", To: "d"})
	_ = g.AddEdge(dag.Edge{From: "b", To: "b_sub_2"})

	orders := map[int][]string{
		1: {"a", "b", "e"},
		2: {"c", "c2", "b_sub_2", "d"},
	}
	// Bottom-up widths: the shared child c2 splits between a and e, so the
	// naive prefix sums of row 1 (a=45, b=30) put b at [45,75] while its
	// pillar segment sits at [60,90].
	widths := map[string]float64{
		"a": 45, "b": 30, "e": 45,
		"c": 30, "c2": 30, "b_sub_2": 30, "d": 30,
	}
	heights := map[int]float64{1: 10, 2: 10}
	bottoms := map[int]float64{1: 0, 2: 10}

	blocks := assembleBlocks(g, orders, widths, heights, bottoms, 0, 0, 120)

	leaf, pillar := blocks["b"], blocks["b_sub_2"]
	if leaf.Left != pillar.Left || leaf.Right != pillar.Right {
		t.Errorf("pillar misaligned: b=[%.1f,%.1f], b_sub_2=[%.1f,%.1f]",
			leaf.Left, leaf.Right, pillar.Left, pillar.Right)
	}

	// Rows must remain contiguous partitions of [0, 120].
	for row, ids := range orders {
		x := 0.0
		for _, id := range ids {
			b := blocks[id]
			if math.Abs(b.Left-x) > 1e-6 {
				t.Errorf("row %d: %s starts at %.2f, want %.2f", row, id, b.Left, x)
			}
			x = b.Right
		}
		if math.Abs(x-120) > 1e-6 {
			t.Errorf("row %d ends at %.2f, want 120", row, x)
		}
	}
}

// TestAssembleBlocks_PipeAlignment checks that interior segments of a
// long-edge pipe share one extent, while the master block itself (which has
// real children) keeps its full width and is NOT collapsed onto the pipe.
func TestAssembleBlocks_PipeAlignment(t *testing.T) {
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "m", Row: 0})
	_ = g.AddNode(dag.Node{ID: "x", Row: 1})
	_ = g.AddNode(dag.Node{ID: "m_sub_1", Row: 1, Kind: dag.NodeKindSubdivider, MasterID: "m"})
	_ = g.AddNode(dag.Node{ID: "y", Row: 2})
	_ = g.AddNode(dag.Node{ID: "m_sub_2", Row: 2, Kind: dag.NodeKindSubdivider, MasterID: "m"})
	_ = g.AddNode(dag.Node{ID: "z", Row: 3})
	_ = g.AddNode(dag.Node{ID: "w", Row: 3})
	_ = g.AddEdge(dag.Edge{From: "m", To: "x"})
	_ = g.AddEdge(dag.Edge{From: "m", To: "m_sub_1"})
	_ = g.AddEdge(dag.Edge{From: "m_sub_1", To: "m_sub_2"})
	_ = g.AddEdge(dag.Edge{From: "m_sub_2", To: "z"})
	_ = g.AddEdge(dag.Edge{From: "x", To: "y"})
	_ = g.AddEdge(dag.Edge{From: "y", To: "z"})
	_ = g.AddEdge(dag.Edge{From: "y", To: "w"})

	orders := map[int][]string{
		0: {"m"},
		1: {"x", "m_sub_1"},
		2: {"y", "m_sub_2"},
		3: {"w", "z"},
	}
	widths := map[string]float64{
		"m": 100,
		"x": 70, "m_sub_1": 30,
		"y": 80, "m_sub_2": 20,
		"w": 50, "z": 50,
	}
	heights := map[int]float64{0: 10, 1: 10, 2: 10, 3: 10}
	bottoms := map[int]float64{0: 0, 1: 10, 2: 20, 3: 30}

	blocks := assembleBlocks(g, orders, widths, heights, bottoms, 0, 0, 100)

	s1, s2 := blocks["m_sub_1"], blocks["m_sub_2"]
	if s1.Left != s2.Left || s1.Right != s2.Right {
		t.Errorf("pipe misaligned: m_sub_1=[%.1f,%.1f], m_sub_2=[%.1f,%.1f]",
			s1.Left, s1.Right, s2.Left, s2.Right)
	}

	// The master keeps its full row width; it must not shrink to pipe width.
	if m := blocks["m"]; m.Width() != 100 {
		t.Errorf("master m width = %.1f, want 100 (must not be pinned to its pipe)", m.Width())
	}
}

// TestBuild_AlignsSinkExtension runs the full pipeline (layering, subdivision,
// Build) and asserts a leaf column reaches the bottom row at one position.
func TestBuild_AlignsSinkExtension(t *testing.T) {
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "root", Row: 0})
	_ = g.AddNode(dag.Node{ID: "leaf", Row: 1})
	_ = g.AddNode(dag.Node{ID: "mid", Row: 1})
	_ = g.AddNode(dag.Node{ID: "deep", Row: 2})
	_ = g.AddEdge(dag.Edge{From: "root", To: "leaf"})
	_ = g.AddEdge(dag.Edge{From: "root", To: "mid"})
	_ = g.AddEdge(dag.Edge{From: "mid", To: "deep"})

	transform.Subdivide(g)

	l := Build(g, 100, 100)

	leaf := l.Blocks["leaf"]
	var segments int
	for _, n := range g.Nodes() {
		if n.IsSubdivider() && n.MasterID == "leaf" {
			segments++
			b, ok := l.Blocks[n.ID]
			if !ok {
				t.Fatalf("no block for pillar segment %s", n.ID)
			}
			if b.Left != leaf.Left || b.Right != leaf.Right {
				t.Errorf("segment %s=[%.2f,%.2f] not aligned with leaf=[%.2f,%.2f]",
					n.ID, b.Left, b.Right, leaf.Left, leaf.Right)
			}
		}
	}
	if segments == 0 {
		t.Fatal("expected sink-extension segments for leaf")
	}
}
