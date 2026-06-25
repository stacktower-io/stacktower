package ordering

import (
	"context"
	"slices"
	"testing"

	"github.com/stacktower-io/stacktower/pkg/core/dag"
)

// beamGraph builds the coordinated-move scenario: a pillar column sits
// between two nodes connected by an edge spanning over it. Fixing it
// requires moving the column in both rows at once.
func beamGraph(t *testing.T) (*dag.DAG, map[int][]string) {
	t.Helper()
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "u", Row: 0})
	_ = g.AddNode(dag.Node{ID: "c", Row: 0})
	_ = g.AddNode(dag.Node{ID: "w", Row: 0})
	_ = g.AddNode(dag.Node{ID: "u1", Row: 1})
	_ = g.AddNode(dag.Node{ID: "c_sub_1", Row: 1, Kind: dag.NodeKindSubdivider, MasterID: "c"})
	_ = g.AddNode(dag.Node{ID: "w1", Row: 1})
	_ = g.AddEdge(dag.Edge{From: "u", To: "u1"})
	_ = g.AddEdge(dag.Edge{From: "u", To: "w1"})
	_ = g.AddEdge(dag.Edge{From: "w", To: "w1"})
	_ = g.AddEdge(dag.Edge{From: "c", To: "c_sub_1"})

	seed := map[int][]string{
		0: {"u", "c", "w"},
		1: {"u1", "c_sub_1", "w1"},
	}
	return g, seed
}

func TestSearchEntities_FindsZeroCrossingLayout(t *testing.T) {
	g, seed := beamGraph(t)
	budget := dag.CountCrossings(g, seed)
	if budget != 1 {
		t.Fatalf("setup: want 1 crossing, got %d", budget)
	}

	orders, score, exhausted := searchEntities(context.Background(), g, seed, budget)
	if orders == nil {
		t.Fatal("expected an improved ordering")
	}
	if score != 0 {
		t.Errorf("score = %d, want 0", score)
	}
	if !exhausted {
		t.Error("zero-crossing result should report the search as exhausted")
	}
	if got := dag.CountCrossings(g, orders); got != score {
		t.Errorf("reported score %d != recount %d", score, got)
	}

	// Row contents must be preserved (same nodes, new order).
	for r, ids := range seed {
		got := slices.Clone(orders[r])
		want := slices.Clone(ids)
		slices.Sort(got)
		slices.Sort(want)
		if !slices.Equal(got, want) {
			t.Errorf("row %d contents changed: got %v, want %v", r, orders[r], ids)
		}
	}

	// The column must stay consistent: c and c_sub_1 at the same index.
	if p0, p1 := slices.Index(orders[0], "c"), slices.Index(orders[1], "c_sub_1"); p0 != p1 {
		t.Errorf("column split across rows: c at %d, c_sub_1 at %d", p0, p1)
	}
}

func TestSearchEntities_Deterministic(t *testing.T) {
	g, seed := beamGraph(t)
	budget := dag.CountCrossings(g, seed)

	o1, s1, _ := searchEntities(context.Background(), g, seed, budget)
	o2, s2, _ := searchEntities(context.Background(), g, seed, budget)
	if s1 != s2 {
		t.Fatalf("scores differ: %d vs %d", s1, s2)
	}
	for r := range o1 {
		if !slices.Equal(o1[r], o2[r]) {
			t.Errorf("row %d differs: %v vs %v", r, o1[r], o2[r])
		}
	}
}

func TestSearchEntities_NoImprovementReturnsNil(t *testing.T) {
	// Already optimal seed: budget 0 cannot be beaten.
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "a", Row: 0})
	_ = g.AddNode(dag.Node{ID: "b", Row: 0})
	_ = g.AddNode(dag.Node{ID: "a1", Row: 1})
	_ = g.AddNode(dag.Node{ID: "b1", Row: 1})
	_ = g.AddEdge(dag.Edge{From: "a", To: "a1"})
	_ = g.AddEdge(dag.Edge{From: "b", To: "b1"})

	seed := map[int][]string{0: {"a", "b"}, 1: {"a1", "b1"}}
	orders, score, _ := searchEntities(context.Background(), g, seed, 0)
	if orders != nil {
		t.Errorf("expected nil orders when budget is already 0, got %v (score %d)", orders, score)
	}
}

func TestSearchEntities_RespectsCancellation(t *testing.T) {
	g, seed := beamGraph(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	orders, _, exhausted := searchEntities(ctx, g, seed, dag.CountCrossings(g, seed))
	if exhausted {
		t.Error("cancelled search must not report exhaustion")
	}
	_ = orders // a cancelled search may or may not have found something
}
