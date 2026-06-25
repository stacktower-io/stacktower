package transform

import (
	"testing"

	"github.com/stacktower-io/stacktower/pkg/core/dag"
	"github.com/stacktower-io/stacktower/pkg/core/render/tower/layout"
)

func TestMergeSubdividers_NoSubdividers(t *testing.T) {
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "a", Row: 0})
	_ = g.AddNode(dag.Node{ID: "b", Row: 1})
	_ = g.AddEdge(dag.Edge{From: "a", To: "b"})

	layout := layout.Layout{
		FrameWidth:  100,
		FrameHeight: 100,
		Blocks: map[string]layout.Block{
			"a": {NodeID: "a", Left: 0, Right: 50, Bottom: 50, Top: 100},
			"b": {NodeID: "b", Left: 0, Right: 50, Bottom: 0, Top: 50},
		},
	}

	merged := MergeSubdividers(layout, g)

	if got, want := len(merged.Blocks), 2; got != want {
		t.Errorf("block count = %d, want %d", got, want)
	}
	if _, ok := merged.Blocks["a"]; !ok {
		t.Error("missing block 'a'")
	}
	if _, ok := merged.Blocks["b"]; !ok {
		t.Error("missing block 'b'")
	}
}

func TestMergeSubdividers_SingleChain(t *testing.T) {
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "a", Row: 0})
	_ = g.AddNode(dag.Node{ID: "a_sub_1", Row: 1, Kind: dag.NodeKindSubdivider, MasterID: "a"})
	_ = g.AddNode(dag.Node{ID: "a_sub_2", Row: 2, Kind: dag.NodeKindSubdivider, MasterID: "a"})
	_ = g.AddNode(dag.Node{ID: "b", Row: 3})
	_ = g.AddEdge(dag.Edge{From: "a", To: "a_sub_1"})
	_ = g.AddEdge(dag.Edge{From: "a_sub_1", To: "a_sub_2"})
	_ = g.AddEdge(dag.Edge{From: "a_sub_2", To: "b"})

	layout := layout.Layout{
		FrameWidth:  100,
		FrameHeight: 100,
		Blocks: map[string]layout.Block{
			"a":       {NodeID: "a", Left: 0, Right: 50, Bottom: 75, Top: 100},
			"a_sub_1": {NodeID: "a_sub_1", Left: 0, Right: 50, Bottom: 50, Top: 75},
			"a_sub_2": {NodeID: "a_sub_2", Left: 0, Right: 50, Bottom: 25, Top: 50},
			"b":       {NodeID: "b", Left: 0, Right: 50, Bottom: 0, Top: 25},
		},
	}

	merged := MergeSubdividers(layout, g)

	if got, want := len(merged.Blocks), 2; got != want {
		t.Fatalf("block count = %d, want %d", got, want)
	}

	blockA, ok := merged.Blocks["a"]
	if !ok {
		t.Fatal("missing merged block 'a'")
	}

	if got, want := blockA.Bottom, 25.0; got != want {
		t.Errorf("block 'a' bottom = %f, want %f", got, want)
	}
	if got, want := blockA.Top, 100.0; got != want {
		t.Errorf("block 'a' top = %f, want %f", got, want)
	}

	if _, ok := merged.Blocks["a_sub_1"]; ok {
		t.Error("subdivider 'a_sub_1' should not exist")
	}
	if _, ok := merged.Blocks["a_sub_2"]; ok {
		t.Error("subdivider 'a_sub_2' should not exist")
	}
}

func TestMergeSubdividers_MultipleChains(t *testing.T) {
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "a", Row: 0})
	_ = g.AddNode(dag.Node{ID: "a_sub_1", Row: 1, Kind: dag.NodeKindSubdivider, MasterID: "a"})
	_ = g.AddNode(dag.Node{ID: "b", Row: 0})
	_ = g.AddNode(dag.Node{ID: "b_sub_1", Row: 1, Kind: dag.NodeKindSubdivider, MasterID: "b"})
	_ = g.AddNode(dag.Node{ID: "c", Row: 2})

	layout := layout.Layout{
		FrameWidth:  100,
		FrameHeight: 100,
		Blocks: map[string]layout.Block{
			"a":       {NodeID: "a", Left: 0, Right: 25, Bottom: 66, Top: 100},
			"a_sub_1": {NodeID: "a_sub_1", Left: 0, Right: 25, Bottom: 33, Top: 66},
			"b":       {NodeID: "b", Left: 25, Right: 50, Bottom: 66, Top: 100},
			"b_sub_1": {NodeID: "b_sub_1", Left: 25, Right: 50, Bottom: 33, Top: 66},
			"c":       {NodeID: "c", Left: 0, Right: 50, Bottom: 0, Top: 33},
		},
	}

	merged := MergeSubdividers(layout, g)

	if got, want := len(merged.Blocks), 3; got != want {
		t.Fatalf("block count = %d, want %d", got, want)
	}

	if blockA := merged.Blocks["a"]; blockA.Bottom != 33 || blockA.Top != 100 {
		t.Errorf("block 'a' bounds = (%.0f, %.0f), want (33, 100)", blockA.Bottom, blockA.Top)
	}

	if blockB := merged.Blocks["b"]; blockB.Bottom != 33 || blockB.Top != 100 {
		t.Errorf("block 'b' bounds = (%.0f, %.0f), want (33, 100)", blockB.Bottom, blockB.Top)
	}
}

func TestMergeSubdividers_PreservesLayoutMetadata(t *testing.T) {
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "a", Row: 0})

	layout := layout.Layout{
		FrameWidth:  800,
		FrameHeight: 600,
		MarginX:     40,
		MarginY:     30,
		RowOrders:   map[int][]string{0: {"a"}},
		Blocks: map[string]layout.Block{
			"a": {NodeID: "a", Left: 40, Right: 760, Bottom: 30, Top: 570},
		},
	}

	merged := MergeSubdividers(layout, g)

	if got, want := merged.FrameWidth, 800.0; got != want {
		t.Errorf("FrameWidth = %f, want %f", got, want)
	}
	if got, want := merged.FrameHeight, 600.0; got != want {
		t.Errorf("FrameHeight = %f, want %f", got, want)
	}
	if got, want := merged.MarginX, 40.0; got != want {
		t.Errorf("MarginX = %f, want %f", got, want)
	}
	if got, want := merged.MarginY, 30.0; got != want {
		t.Errorf("MarginY = %f, want %f", got, want)
	}
}

func TestMergeSubdividers_ConsistentPositions_Merges(t *testing.T) {
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "a", Row: 0})
	_ = g.AddNode(dag.Node{ID: "a_sub_1", Row: 1, Kind: dag.NodeKindSubdivider, MasterID: "a"})

	layout := layout.Layout{
		Blocks: map[string]layout.Block{
			"a":       {NodeID: "a", Left: 10, Right: 40, Bottom: 50, Top: 100},
			"a_sub_1": {NodeID: "a_sub_1", Left: 10, Right: 40, Bottom: 0, Top: 50},
		},
	}

	merged := MergeSubdividers(layout, g)

	// Should merge into single block
	if len(merged.Blocks) != 1 {
		t.Errorf("expected 1 merged block, got %d", len(merged.Blocks))
	}
	blockA := merged.Blocks["a"]
	if got, want := blockA.Left, 10.0; got != want {
		t.Errorf("Left = %f, want %f", got, want)
	}
	if got, want := blockA.Right, 40.0; got != want {
		t.Errorf("Right = %f, want %f", got, want)
	}
	if got, want := blockA.Bottom, 0.0; got != want {
		t.Errorf("Bottom = %f, want %f (min)", got, want)
	}
	if got, want := blockA.Top, 100.0; got != want {
		t.Errorf("Top = %f, want %f (max)", got, want)
	}
}

func TestMergeSubdividers_InconsistentPositions_MergesSubgroups(t *testing.T) {
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "a", Row: 0})
	_ = g.AddNode(dag.Node{ID: "a_sub_1", Row: 1, Kind: dag.NodeKindSubdivider, MasterID: "a"})
	_ = g.AddNode(dag.Node{ID: "a_sub_2", Row: 2, Kind: dag.NodeKindSubdivider, MasterID: "a"})

	layout := layout.Layout{
		Blocks: map[string]layout.Block{
			"a":       {NodeID: "a", Left: 10, Right: 40, Bottom: 75, Top: 100},
			"a_sub_1": {NodeID: "a_sub_1", Left: 50, Right: 80, Bottom: 25, Top: 75},
			"a_sub_2": {NodeID: "a_sub_2", Left: 50, Right: 80, Bottom: 0, Top: 25},
		},
	}

	merged := MergeSubdividers(layout, g)

	// Should create 2 blocks: master alone + subdividers merged together
	if len(merged.Blocks) != 2 {
		t.Errorf("expected 2 merged blocks, got %d", len(merged.Blocks))
	}

	// One block should be the master
	var hasMasterBlock, hasSubBlock bool
	for id, b := range merged.Blocks {
		if b.Left == 10 && b.Right == 40 {
			hasMasterBlock = true
			if b.Bottom != 75 || b.Top != 100 {
				t.Errorf("master block %s has wrong vertical extent", id)
			}
		}
		if b.Left == 50 && b.Right == 80 {
			hasSubBlock = true
			// Subdividers should be merged vertically
			if b.Bottom != 0 || b.Top != 75 {
				t.Errorf("subdivider block %s has wrong vertical extent: got Bottom=%.1f Top=%.1f, want Bottom=0 Top=75", id, b.Bottom, b.Top)
			}
		}
	}
	if !hasMasterBlock {
		t.Error("expected master block at position 10-40")
	}
	if !hasSubBlock {
		t.Error("expected merged subdivider block at position 50-80")
	}
}

func TestMergeSubdividers_SubgroupsWithSameLeftDoNotOverwrite(t *testing.T) {
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "a", Row: 0})
	_ = g.AddNode(dag.Node{ID: "a_sub_wide_1", Row: 1, Kind: dag.NodeKindSubdivider, MasterID: "a"})
	_ = g.AddNode(dag.Node{ID: "a_sub_wide_2", Row: 2, Kind: dag.NodeKindSubdivider, MasterID: "a"})
	_ = g.AddNode(dag.Node{ID: "a_sub_narrow", Row: 3, Kind: dag.NodeKindSubdivider, MasterID: "a"})

	layout := layout.Layout{
		Blocks: map[string]layout.Block{
			"a":            {NodeID: "a", Left: 10, Right: 40, Bottom: 75, Top: 100},
			"a_sub_wide_1": {NodeID: "a_sub_wide_1", Left: 50, Right: 90, Bottom: 50, Top: 75},
			"a_sub_wide_2": {NodeID: "a_sub_wide_2", Left: 50, Right: 90, Bottom: 25, Top: 50},
			"a_sub_narrow": {NodeID: "a_sub_narrow", Left: 50, Right: 60, Bottom: 0, Top: 25},
		},
	}

	merged := MergeSubdividers(layout, g)

	if got, want := len(merged.Blocks), 3; got != want {
		t.Fatalf("block count = %d, want %d; blocks=%v", got, want, merged.Blocks)
	}
	if _, ok := merged.Blocks["a@50-90"]; !ok {
		t.Fatalf("missing wide subgroup keyed by full span; blocks=%v", merged.Blocks)
	}
	if _, ok := merged.Blocks["a@50-60"]; !ok {
		t.Fatalf("missing narrow subgroup keyed by full span; blocks=%v", merged.Blocks)
	}
	if b := merged.Blocks["a@50-90"]; b.Bottom != 25 || b.Top != 75 {
		t.Errorf("wide subgroup vertical span = %.0f..%.0f, want 25..75", b.Bottom, b.Top)
	}
	if b := merged.Blocks["a@50-60"]; b.Bottom != 0 || b.Top != 25 {
		t.Errorf("narrow subgroup vertical span = %.0f..%.0f, want 0..25", b.Bottom, b.Top)
	}
}
