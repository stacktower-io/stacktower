package ordering

import (
	"context"
	"slices"
	"testing"

	"github.com/stacktower-io/stacktower/pkg/core/dag"
)

// TestRefineColumnPlacement_MovesPillarOutOfBeam reproduces the search blind
// spot: a foundation pillar sits between two nodes connected by an edge that
// must pass over it. Row-local search can't fix this (the move only pays off
// when applied to both rows at once), but column refinement slides the whole
// pillar out of the corridor.
func TestRefineColumnPlacement_MovesPillarOutOfBeam(t *testing.T) {
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "u", Row: 0})
	_ = g.AddNode(dag.Node{ID: "c", Row: 0})
	_ = g.AddNode(dag.Node{ID: "w", Row: 0})
	_ = g.AddNode(dag.Node{ID: "u1", Row: 1})
	_ = g.AddNode(dag.Node{ID: "c_sub_1", Row: 1, Kind: dag.NodeKindSubdivider, MasterID: "c"})
	_ = g.AddNode(dag.Node{ID: "w1", Row: 1})
	_ = g.AddEdge(dag.Edge{From: "u", To: "u1"})
	_ = g.AddEdge(dag.Edge{From: "u", To: "w1"}) // spans over the pillar
	_ = g.AddEdge(dag.Edge{From: "w", To: "w1"})
	_ = g.AddEdge(dag.Edge{From: "c", To: "c_sub_1"})

	orders := map[int][]string{
		0: {"u", "c", "w"},
		1: {"u1", "c_sub_1", "w1"},
	}
	if before := dag.CountCrossings(g, orders); before != 1 {
		t.Fatalf("setup: want 1 crossing before refinement, got %d", before)
	}

	after := RefinePlacement(context.Background(), g, orders)
	if after != 0 {
		t.Errorf("want 0 crossings after refinement, got %d", after)
	}

	// The pillar must have moved as one unit: master and segment at the
	// same position in their respective rows.
	p0 := slices.Index(orders[0], "c")
	p1 := slices.Index(orders[1], "c_sub_1")
	if p0 != p1 {
		t.Errorf("column split: c at %d, c_sub_1 at %d", p0, p1)
	}
}

// TestRefineColumnPlacement_NoImprovementKeepsOrder verifies refinement
// leaves an already-optimal ordering untouched.
func TestRefineColumnPlacement_NoImprovementKeepsOrder(t *testing.T) {
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "a", Row: 0})
	_ = g.AddNode(dag.Node{ID: "b", Row: 0})
	_ = g.AddNode(dag.Node{ID: "a1", Row: 1})
	_ = g.AddNode(dag.Node{ID: "b_sub_1", Row: 1, Kind: dag.NodeKindSubdivider, MasterID: "b"})
	_ = g.AddEdge(dag.Edge{From: "a", To: "a1"})
	_ = g.AddEdge(dag.Edge{From: "b", To: "b_sub_1"})

	orders := map[int][]string{
		0: {"a", "b"},
		1: {"a1", "b_sub_1"},
	}
	want := map[int][]string{
		0: slices.Clone(orders[0]),
		1: slices.Clone(orders[1]),
	}

	if got := RefinePlacement(context.Background(), g, orders); got != 0 {
		t.Errorf("want 0 crossings, got %d", got)
	}
	for r, ids := range want {
		if !slices.Equal(orders[r], ids) {
			t.Errorf("row %d changed: got %v, want %v", r, orders[r], ids)
		}
	}
}

// TestBuildEntities_PipeExcludesMaster checks that a master with real
// children does not move with its long-edge pipe (it becomes its own
// single-row entity), while a leaf master is included in its pillar column.
func TestBuildEntities_PipeExcludesMaster(t *testing.T) {
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "m", Row: 0})
	_ = g.AddNode(dag.Node{ID: "kid", Row: 1})
	_ = g.AddNode(dag.Node{ID: "m_sub_1", Row: 1, Kind: dag.NodeKindSubdivider, MasterID: "m"})
	_ = g.AddNode(dag.Node{ID: "m_sub_2", Row: 2, Kind: dag.NodeKindSubdivider, MasterID: "m"})
	_ = g.AddNode(dag.Node{ID: "leaf", Row: 1})
	_ = g.AddNode(dag.Node{ID: "leaf_sub_2", Row: 2, Kind: dag.NodeKindSubdivider, MasterID: "leaf"})
	_ = g.AddEdge(dag.Edge{From: "m", To: "kid"})
	_ = g.AddEdge(dag.Edge{From: "m", To: "m_sub_1"})
	_ = g.AddEdge(dag.Edge{From: "m_sub_1", To: "m_sub_2"})
	_ = g.AddEdge(dag.Edge{From: "leaf", To: "leaf_sub_2"})

	ents := buildEntities(g)
	byTop := make(map[string]entity)
	for _, e := range ents {
		byTop[e.ids[0]] = e
	}

	if e, ok := byTop["m_sub_1"]; !ok || len(e.ids) != 2 {
		t.Errorf("pipe column should start at m_sub_1 with 2 segments, got %+v", byTop)
	}
	if e, ok := byTop["m"]; !ok || len(e.ids) != 1 {
		t.Error("master m has real children and must be its own single-row entity")
	} else if e.rows[0] != 0 {
		t.Errorf("entity m in row %d, want 0", e.rows[0])
	}
	if e, ok := byTop["leaf"]; !ok || len(e.ids) != 2 {
		t.Errorf("leaf pillar should include its master, got %+v", byTop)
	}

	// Every node must belong to exactly one entity.
	seen := make(map[string]int)
	for _, e := range ents {
		for _, id := range e.ids {
			seen[id]++
		}
	}
	for _, n := range g.Nodes() {
		if seen[n.ID] != 1 {
			t.Errorf("node %s in %d entities, want 1", n.ID, seen[n.ID])
		}
	}
}

// TestRefinePlacement_SiftsSingleNode verifies single-row entities are
// sifted too: a node stuck at a bad position that the row search could in
// principle fix, but a capped candidate set might miss.
func TestRefinePlacement_SiftsSingleNode(t *testing.T) {
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "a", Row: 0})
	_ = g.AddNode(dag.Node{ID: "b", Row: 0})
	_ = g.AddNode(dag.Node{ID: "c", Row: 0})
	_ = g.AddNode(dag.Node{ID: "a1", Row: 1})
	_ = g.AddNode(dag.Node{ID: "b1", Row: 1})
	_ = g.AddNode(dag.Node{ID: "c1", Row: 1})
	_ = g.AddEdge(dag.Edge{From: "a", To: "a1"})
	_ = g.AddEdge(dag.Edge{From: "b", To: "b1"})
	_ = g.AddEdge(dag.Edge{From: "c", To: "c1"})

	// c is leftmost on top but its child is rightmost below: 2 crossings,
	// fixable by sifting c (or c1) alone.
	orders := map[int][]string{
		0: {"c", "a", "b"},
		1: {"a1", "b1", "c1"},
	}
	if before := dag.CountCrossings(g, orders); before != 2 {
		t.Fatalf("setup: want 2 crossings, got %d", before)
	}

	if after := RefinePlacement(context.Background(), g, orders); after != 0 {
		t.Errorf("want 0 crossings after sifting, got %d", after)
	}
}
