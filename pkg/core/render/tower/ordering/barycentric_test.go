package ordering

import (
	"slices"
	"testing"

	"github.com/stacktower-io/stacktower/pkg/core/dag"
)

func TestBarycentric_Diamond(t *testing.T) {
	g := dag.New(nil)
	g.AddNode(dag.Node{ID: "A", Row: 0})
	g.AddNode(dag.Node{ID: "B", Row: 1})
	g.AddNode(dag.Node{ID: "C", Row: 1})
	g.AddNode(dag.Node{ID: "D", Row: 2})
	g.AddEdge(dag.Edge{From: "A", To: "B"})
	g.AddEdge(dag.Edge{From: "A", To: "C"})
	g.AddEdge(dag.Edge{From: "B", To: "D"})
	g.AddEdge(dag.Edge{From: "C", To: "D"})

	got := Barycentric{}.OrderRows(g)

	if len(got) != 3 {
		t.Fatalf("want 3 rows, got %d", len(got))
	}
	if got[0][0] != "A" {
		t.Errorf("row 0: want [A], got %v", got[0])
	}
	if len(got[1]) != 2 {
		t.Errorf("row 1: want 2 nodes, got %d", len(got[1]))
	}
	if got[2][0] != "D" {
		t.Errorf("row 2: want [D], got %v", got[2])
	}
}

func TestBarycentric_Empty(t *testing.T) {
	got := Barycentric{}.OrderRows(dag.New(nil))
	if got != nil {
		t.Errorf("want nil, got %v", got)
	}
}

func TestBarycentric_SingleNode(t *testing.T) {
	g := dag.New(nil)
	g.AddNode(dag.Node{ID: "A", Row: 0})

	got := Barycentric{}.OrderRows(g)

	if !slices.Equal(got[0], []string{"A"}) {
		t.Errorf("want [A], got %v", got[0])
	}
}

func TestBarycentric_CrossingReduction(t *testing.T) {
	g := dag.New(nil)
	g.AddNode(dag.Node{ID: "P1", Row: 0})
	g.AddNode(dag.Node{ID: "P2", Row: 0})
	g.AddNode(dag.Node{ID: "C1", Row: 1})
	g.AddNode(dag.Node{ID: "C2", Row: 1})
	g.AddEdge(dag.Edge{From: "P1", To: "C1"})
	g.AddEdge(dag.Edge{From: "P2", To: "C2"})

	got := Barycentric{}.OrderRows(g)

	p1 := slices.Index(got[0], "P1")
	p2 := slices.Index(got[0], "P2")
	c1 := slices.Index(got[1], "C1")
	c2 := slices.Index(got[1], "C2")

	if (p1 < p2) != (c1 < c2) {
		t.Errorf("should minimize crossings: parents=%v children=%v", got[0], got[1])
	}
}

func TestBarycentric_SubdividerAlignment(t *testing.T) {
	g := dag.New(nil)
	g.AddNode(dag.Node{ID: "A", Row: 0})
	g.AddNode(dag.Node{ID: "A_sub", Row: 1, Kind: dag.NodeKindSubdivider, MasterID: "A"})
	g.AddNode(dag.Node{ID: "C", Row: 1})
	g.AddNode(dag.Node{ID: "B", Row: 2})
	g.AddEdge(dag.Edge{From: "A", To: "A_sub"})
	g.AddEdge(dag.Edge{From: "A", To: "C"})
	g.AddEdge(dag.Edge{From: "A_sub", To: "B"})
	g.AddEdge(dag.Edge{From: "C", To: "B"})

	got := Barycentric{}.OrderRows(g)

	if len(got[1]) != 2 {
		t.Fatalf("row 1: want 2 nodes, got %d", len(got[1]))
	}
}

func TestBarycentric_Passes(t *testing.T) {
	g := dag.New(nil)
	g.AddNode(dag.Node{ID: "A", Row: 0})
	g.AddNode(dag.Node{ID: "B", Row: 1})
	g.AddEdge(dag.Edge{From: "A", To: "B"})

	got := Barycentric{Passes: 5}.OrderRows(g)

	if len(got) != 2 {
		t.Errorf("want 2 rows, got %d", len(got))
	}
}

func TestBarycentric_TieBreaking(t *testing.T) {
	g := dag.New(nil)
	g.AddNode(dag.Node{ID: "Z", Row: 0})
	g.AddNode(dag.Node{ID: "Y", Row: 0})
	g.AddNode(dag.Node{ID: "X", Row: 0})

	got := Barycentric{}.OrderRows(g)
	want := []string{"X", "Y", "Z"}

	if !slices.Equal(got[0], want) {
		t.Errorf("want %v, got %v", want, got[0])
	}
}

func TestBarycentric_FanOut(t *testing.T) {
	g := dag.New(nil)
	g.AddNode(dag.Node{ID: "A", Row: 0})
	g.AddNode(dag.Node{ID: "B", Row: 1})
	g.AddNode(dag.Node{ID: "C", Row: 1})
	g.AddNode(dag.Node{ID: "D", Row: 1})
	g.AddEdge(dag.Edge{From: "A", To: "B"})
	g.AddEdge(dag.Edge{From: "A", To: "C"})
	g.AddEdge(dag.Edge{From: "A", To: "D"})

	got := Barycentric{}.OrderRows(g)

	if len(got[1]) != 3 {
		t.Fatalf("row 1: want 3 nodes, got %d", len(got[1]))
	}
}

func TestBarycentric_WPattern(t *testing.T) {
	g := dag.New(nil)
	g.AddNode(dag.Node{ID: "A", Row: 0})
	g.AddNode(dag.Node{ID: "B", Row: 0})
	g.AddNode(dag.Node{ID: "C", Row: 1})
	g.AddNode(dag.Node{ID: "D", Row: 1})
	g.AddNode(dag.Node{ID: "E", Row: 1})
	g.AddNode(dag.Node{ID: "F", Row: 2})
	g.AddNode(dag.Node{ID: "G", Row: 2})
	g.AddEdge(dag.Edge{From: "A", To: "C"})
	g.AddEdge(dag.Edge{From: "A", To: "D"})
	g.AddEdge(dag.Edge{From: "B", To: "D"})
	g.AddEdge(dag.Edge{From: "B", To: "E"})
	g.AddEdge(dag.Edge{From: "C", To: "F"})
	g.AddEdge(dag.Edge{From: "D", To: "F"})
	g.AddEdge(dag.Edge{From: "D", To: "G"})
	g.AddEdge(dag.Edge{From: "E", To: "G"})

	got := Barycentric{}.OrderRows(g)

	if len(got) != 3 {
		t.Errorf("want 3 rows, got %d", len(got))
	}
	if len(got[1]) != 3 {
		t.Errorf("row 1: want 3 nodes, got %d", len(got[1]))
	}
}

func TestWeightedMedian(t *testing.T) {
	tests := []struct {
		name      string
		neighbors []string
		positions map[string]int
		wantMed   float64
		wantHas   bool
	}{
		{
			name:      "two neighbors",
			neighbors: []string{"P1", "P2"},
			positions: map[string]int{"P1": 0, "P2": 2},
			wantMed:   1, // mean of the two middle positions for even count
			wantHas:   true,
		},
		{
			name:      "three neighbors",
			neighbors: []string{"P1", "P2", "P3"},
			positions: map[string]int{"P1": 0, "P2": 1, "P3": 4},
			wantMed:   1, // middle for odd count
			wantHas:   true,
		},
		{
			name:      "single neighbor",
			neighbors: []string{"P1"},
			positions: map[string]int{"P1": 5},
			wantMed:   5,
			wantHas:   true,
		},
		{
			name:      "no neighbors",
			neighbors: nil,
			positions: map[string]int{},
			wantMed:   0,
			wantHas:   false,
		},
		{
			name:      "neighbors not in positions",
			neighbors: []string{"X", "Y"},
			positions: map[string]int{"A": 0, "B": 1},
			wantMed:   0,
			wantHas:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotMed, gotHas := weightedMedian(tt.neighbors, tt.positions)
			if gotMed != tt.wantMed {
				t.Errorf("median: want %v, got %v", tt.wantMed, gotMed)
			}
			if gotHas != tt.wantHas {
				t.Errorf("hasEdges: want %t, got %t", tt.wantHas, gotHas)
			}
		})
	}
}

func TestCountPairCrossings(t *testing.T) {
	g := dag.New(nil)
	g.AddNode(dag.Node{ID: "A", Row: 0})
	g.AddNode(dag.Node{ID: "B", Row: 0})
	g.AddNode(dag.Node{ID: "C", Row: 1})
	g.AddNode(dag.Node{ID: "D", Row: 1})
	g.AddEdge(dag.Edge{From: "A", To: "D"})
	g.AddEdge(dag.Edge{From: "B", To: "C"})

	// Parents: A at pos 0, B at pos 1
	// Edges: A→D, B→C
	// If C is left of D:
	// - C's parent B is at pos 1
	// - D's parent A is at pos 0
	// C-D pair: left(C)'s parent pos (1) > right(D)'s parent pos (0) → 1 crossing
	// D-C pair: left(D)'s parent pos (0) < right(C)'s parent pos (1) → 0 crossings

	crossCD := dag.CountPairCrossings(g, "C", "D", []string{"A", "B"}, true)
	crossDC := dag.CountPairCrossings(g, "D", "C", []string{"A", "B"}, true)

	if crossCD != 1 {
		t.Errorf("C-D should have 1 crossing, got %d", crossCD)
	}
	if crossDC != 0 {
		t.Errorf("D-C should have 0 crossings, got %d", crossDC)
	}
}

func TestBarycentric_CrossingElimination(t *testing.T) {
	g := dag.New(nil)
	g.AddNode(dag.Node{ID: "A", Row: 0})
	g.AddNode(dag.Node{ID: "B", Row: 0})
	g.AddNode(dag.Node{ID: "C", Row: 1})
	g.AddNode(dag.Node{ID: "D", Row: 1})
	g.AddEdge(dag.Edge{From: "A", To: "D"})
	g.AddEdge(dag.Edge{From: "B", To: "C"})

	got := Barycentric{}.OrderRows(g)
	crossings := dag.CountCrossings(g, got)

	if crossings != 0 {
		t.Errorf("want 0 crossings, got %d with ordering %v", crossings, got)
	}
}

func TestBarycentric_ComplexCrossings(t *testing.T) {
	g := dag.New(nil)
	g.AddNode(dag.Node{ID: "A", Row: 0})
	g.AddNode(dag.Node{ID: "B", Row: 0})
	g.AddNode(dag.Node{ID: "C", Row: 1})
	g.AddNode(dag.Node{ID: "D", Row: 1})
	g.AddNode(dag.Node{ID: "E", Row: 1})
	g.AddEdge(dag.Edge{From: "A", To: "C"})
	g.AddEdge(dag.Edge{From: "A", To: "D"})
	g.AddEdge(dag.Edge{From: "A", To: "E"})
	g.AddEdge(dag.Edge{From: "B", To: "C"})
	g.AddEdge(dag.Edge{From: "B", To: "D"})
	g.AddEdge(dag.Edge{From: "B", To: "E"})

	got := Barycentric{}.OrderRows(g)
	crossings := dag.CountCrossings(g, got)

	if crossings > 3 {
		t.Errorf("want at most 3 crossings for K2,3, got %d with ordering %v", crossings, got)
	}
}

func TestBarycentric_AvoidableCrossings(t *testing.T) {
	g := dag.New(nil)
	g.AddNode(dag.Node{ID: "A", Row: 0})
	g.AddNode(dag.Node{ID: "B", Row: 0})
	g.AddNode(dag.Node{ID: "C", Row: 0})
	g.AddNode(dag.Node{ID: "D", Row: 1})
	g.AddNode(dag.Node{ID: "E", Row: 1})
	g.AddNode(dag.Node{ID: "F", Row: 1})
	g.AddEdge(dag.Edge{From: "A", To: "D"})
	g.AddEdge(dag.Edge{From: "B", To: "E"})
	g.AddEdge(dag.Edge{From: "C", To: "F"})

	got := Barycentric{}.OrderRows(g)
	crossings := dag.CountCrossings(g, got)

	if crossings != 0 {
		t.Errorf("want 0 crossings, got %d with ordering %v", crossings, got)
	}
}

func TestBarycentric_NodesWithoutEdges(t *testing.T) {
	// Test that isolated nodes don't break the algorithm
	g := dag.New(nil)
	g.AddNode(dag.Node{ID: "A", Row: 0})
	g.AddNode(dag.Node{ID: "B", Row: 0})
	g.AddNode(dag.Node{ID: "C", Row: 1}) // no edges to C
	g.AddNode(dag.Node{ID: "D", Row: 1})
	g.AddEdge(dag.Edge{From: "A", To: "D"})
	g.AddEdge(dag.Edge{From: "B", To: "D"})

	got := Barycentric{}.OrderRows(g)

	if len(got[1]) != 2 {
		t.Errorf("row 1: want 2 nodes, got %d", len(got[1]))
	}
}

func TestTranspose_ReducesCrossings(t *testing.T) {
	g := dag.New(nil)
	g.AddNode(dag.Node{ID: "P1", Row: 0})
	g.AddNode(dag.Node{ID: "P2", Row: 0})
	g.AddNode(dag.Node{ID: "C1", Row: 1})
	g.AddNode(dag.Node{ID: "C2", Row: 1})
	// Crossing pattern: P1->C2, P2->C1
	g.AddEdge(dag.Edge{From: "P1", To: "C2"})
	g.AddEdge(dag.Edge{From: "P2", To: "C1"})

	orders := map[int][]string{
		0: {"P1", "P2"},
		1: {"C1", "C2"}, // Bad order - creates crossing
	}

	before := dag.CountCrossings(g, orders)
	transpose(g, orders, 1, 0, true)
	after := dag.CountCrossings(g, orders)

	if after >= before {
		t.Errorf("transpose should reduce crossings: before=%d after=%d order=%v", before, after, orders[1])
	}
}
