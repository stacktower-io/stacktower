package dag

import "testing"

func TestComputeStats_Chain(t *testing.T) {
	// A -> B -> C -> D
	g := New(nil)
	for _, id := range []string{"A", "B", "C", "D"} {
		g.AddNode(Node{ID: id, Row: 0})
	}
	g.AddEdge(Edge{From: "A", To: "B"})
	g.AddEdge(Edge{From: "B", To: "C"})
	g.AddEdge(Edge{From: "C", To: "D"})

	s := ComputeStats(g)
	if s.NodeCount != 4 {
		t.Errorf("NodeCount: got %d, want 4", s.NodeCount)
	}
	if s.EdgeCount != 3 {
		t.Errorf("EdgeCount: got %d, want 3", s.EdgeCount)
	}
	if s.MaxDepth != 3 {
		t.Errorf("MaxDepth: got %d, want 3", s.MaxDepth)
	}
	if s.DirectDeps != 1 {
		t.Errorf("DirectDeps: got %d, want 1", s.DirectDeps)
	}
	if s.TransitiveDeps != 2 {
		t.Errorf("TransitiveDeps: got %d, want 2", s.TransitiveDeps)
	}
}

func TestComputeStats_Wide(t *testing.T) {
	// A -> B, A -> C, A -> D
	g := New(nil)
	for _, id := range []string{"A", "B", "C", "D"} {
		g.AddNode(Node{ID: id, Row: 0})
	}
	g.AddEdge(Edge{From: "A", To: "B"})
	g.AddEdge(Edge{From: "A", To: "C"})
	g.AddEdge(Edge{From: "A", To: "D"})

	s := ComputeStats(g)
	if s.MaxDepth != 1 {
		t.Errorf("MaxDepth: got %d, want 1", s.MaxDepth)
	}
	if s.DirectDeps != 3 {
		t.Errorf("DirectDeps: got %d, want 3", s.DirectDeps)
	}
	if s.TransitiveDeps != 0 {
		t.Errorf("TransitiveDeps: got %d, want 0", s.TransitiveDeps)
	}
}

func TestComputeStats_Diamond(t *testing.T) {
	//   A
	//  / \
	// B   C
	//  \ /
	//   D
	g := New(nil)
	for _, id := range []string{"A", "B", "C", "D"} {
		g.AddNode(Node{ID: id, Row: 0})
	}
	g.AddEdge(Edge{From: "A", To: "B"})
	g.AddEdge(Edge{From: "A", To: "C"})
	g.AddEdge(Edge{From: "B", To: "D"})
	g.AddEdge(Edge{From: "C", To: "D"})

	s := ComputeStats(g)
	if s.MaxDepth != 2 {
		t.Errorf("MaxDepth: got %d, want 2", s.MaxDepth)
	}
	if s.DirectDeps != 2 {
		t.Errorf("DirectDeps: got %d, want 2", s.DirectDeps)
	}
	if s.TransitiveDeps != 1 {
		t.Errorf("TransitiveDeps: got %d, want 1", s.TransitiveDeps)
	}

	// D should be the most load-bearing (both B and C depend on it)
	if len(s.LoadBearing) == 0 {
		t.Fatal("expected load-bearing nodes")
	}
	if s.LoadBearing[0].ID != "D" {
		t.Errorf("top load-bearing: got %s, want D", s.LoadBearing[0].ID)
	}
	if s.LoadBearing[0].ReverseDeps != 2 {
		t.Errorf("D reverse deps: got %d, want 2", s.LoadBearing[0].ReverseDeps)
	}
}

func TestComputeStats_SingleNode(t *testing.T) {
	g := New(nil)
	g.AddNode(Node{ID: "A", Row: 0})

	s := ComputeStats(g)
	if s.NodeCount != 1 {
		t.Errorf("NodeCount: got %d, want 1", s.NodeCount)
	}
	if s.MaxDepth != 0 {
		t.Errorf("MaxDepth: got %d, want 0", s.MaxDepth)
	}
	if s.DirectDeps != 0 {
		t.Errorf("DirectDeps: got %d, want 0", s.DirectDeps)
	}
}

func TestComputeStats_SkipsSynthetic(t *testing.T) {
	g := New(nil)
	g.AddNode(Node{ID: "A", Row: 0})
	g.AddNode(Node{ID: "sub_1", Row: 0, Kind: NodeKindSubdivider, MasterID: "A"})
	g.AddNode(Node{ID: "B", Row: 0})
	g.AddEdge(Edge{From: "A", To: "sub_1"})
	g.AddEdge(Edge{From: "sub_1", To: "B"})

	s := ComputeStats(g)
	if s.DirectDeps != 1 {
		t.Errorf("DirectDeps: got %d, want 1 (sub_1 is synthetic)", s.DirectDeps)
	}
}
