package transform

import (
	"testing"

	"github.com/stacktower-io/stacktower/pkg/core/dag"
)

func TestAssignLayers_SimpleChain(t *testing.T) {
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "a"})
	_ = g.AddNode(dag.Node{ID: "b"})
	_ = g.AddNode(dag.Node{ID: "c"})
	_ = g.AddEdge(dag.Edge{From: "a", To: "b"})
	_ = g.AddEdge(dag.Edge{From: "b", To: "c"})

	AssignLayers(g)

	checkRow(t, g, "a", 0)
	checkRow(t, g, "b", 1)
	checkRow(t, g, "c", 2)
}

func TestAssignLayers_Diamond(t *testing.T) {
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "a"})
	_ = g.AddNode(dag.Node{ID: "b"})
	_ = g.AddNode(dag.Node{ID: "c"})
	_ = g.AddNode(dag.Node{ID: "d"})

	_ = g.AddEdge(dag.Edge{From: "a", To: "b"})
	_ = g.AddEdge(dag.Edge{From: "a", To: "c"})
	_ = g.AddEdge(dag.Edge{From: "b", To: "d"})
	_ = g.AddEdge(dag.Edge{From: "c", To: "d"})

	AssignLayers(g)

	checkRow(t, g, "a", 0)
	checkRow(t, g, "b", 1)
	checkRow(t, g, "c", 1)
	checkRow(t, g, "d", 2)
}

func checkRow(t *testing.T, g *dag.DAG, id string, expected int) {
	t.Helper()
	n, ok := g.Node(id)
	if !ok {
		t.Fatalf("node %s not found", id)
	}
	if n.Row != expected {
		t.Errorf("node %s: expected row %d, got %d", id, expected, n.Row)
	}
}

func TestAssignLayers_EmptyGraph(t *testing.T) {
	g := dag.New(nil)
	AssignLayers(g)
	if g.NodeCount() != 0 {
		t.Errorf("expected 0 nodes, got %d", g.NodeCount())
	}
}

func TestAssignLayers_SingleNode(t *testing.T) {
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "a"})
	AssignLayers(g)
	checkRow(t, g, "a", 0)
}

func TestAssignLayers_DisconnectedNodes(t *testing.T) {
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "a"})
	_ = g.AddNode(dag.Node{ID: "b"})
	_ = g.AddNode(dag.Node{ID: "c"})

	AssignLayers(g)

	checkRow(t, g, "a", 0)
}

func TestAssignLayers_MultipleRoots(t *testing.T) {
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "a"})
	_ = g.AddNode(dag.Node{ID: "b"})
	_ = g.AddNode(dag.Node{ID: "c"})
	_ = g.AddNode(dag.Node{ID: "d"})

	_ = g.AddEdge(dag.Edge{From: "a", To: "c"})
	_ = g.AddEdge(dag.Edge{From: "b", To: "d"})

	AssignLayers(g)

	checkRow(t, g, "a", 0)
	checkRow(t, g, "b", 0)
	checkRow(t, g, "c", 1)
	checkRow(t, g, "d", 1)
}

func TestAssignLayers_LongestPath(t *testing.T) {
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "a"})
	_ = g.AddNode(dag.Node{ID: "b"})
	_ = g.AddNode(dag.Node{ID: "c"})
	_ = g.AddNode(dag.Node{ID: "d"})

	_ = g.AddEdge(dag.Edge{From: "a", To: "b"})
	_ = g.AddEdge(dag.Edge{From: "a", To: "d"})
	_ = g.AddEdge(dag.Edge{From: "b", To: "c"})
	_ = g.AddEdge(dag.Edge{From: "c", To: "d"})

	AssignLayers(g)

	checkRow(t, g, "a", 0)
	checkRow(t, g, "b", 1)
	checkRow(t, g, "c", 2)
	checkRow(t, g, "d", 3)
}

func TestAssignLayers_ComplexDAG(t *testing.T) {
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "a"})
	_ = g.AddNode(dag.Node{ID: "b"})
	_ = g.AddNode(dag.Node{ID: "c"})
	_ = g.AddNode(dag.Node{ID: "d"})
	_ = g.AddNode(dag.Node{ID: "e"})
	_ = g.AddNode(dag.Node{ID: "f"})

	_ = g.AddEdge(dag.Edge{From: "a", To: "b"})
	_ = g.AddEdge(dag.Edge{From: "a", To: "c"})
	_ = g.AddEdge(dag.Edge{From: "b", To: "d"})
	_ = g.AddEdge(dag.Edge{From: "c", To: "d"})
	_ = g.AddEdge(dag.Edge{From: "c", To: "e"})
	_ = g.AddEdge(dag.Edge{From: "d", To: "f"})
	_ = g.AddEdge(dag.Edge{From: "e", To: "f"})

	AssignLayers(g)

	checkRow(t, g, "a", 0)
	checkRow(t, g, "b", 1)
	checkRow(t, g, "c", 1)
	checkRow(t, g, "d", 2)
	checkRow(t, g, "e", 2)
	checkRow(t, g, "f", 3)
}

func TestAssignLayers_PreservesTopologicalOrder(t *testing.T) {
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "a"})
	_ = g.AddNode(dag.Node{ID: "b"})
	_ = g.AddNode(dag.Node{ID: "c"})
	_ = g.AddEdge(dag.Edge{From: "a", To: "b"})
	_ = g.AddEdge(dag.Edge{From: "b", To: "c"})

	AssignLayers(g)

	nodeA, _ := g.Node("a")
	nodeB, _ := g.Node("b")
	nodeC, _ := g.Node("c")

	if nodeA.Row >= nodeB.Row {
		t.Errorf("parent a (row %d) should be before child b (row %d)", nodeA.Row, nodeB.Row)
	}
	if nodeB.Row >= nodeC.Row {
		t.Errorf("parent b (row %d) should be before child c (row %d)", nodeB.Row, nodeC.Row)
	}
}

func TestAssignLayers_OverwritesStaleRows(t *testing.T) {
	// Regression test: nodes created with stale Row values (e.g. loaded from
	// a graph file) must be re-assigned, including sources and disconnected
	// nodes that the BFS never visits with a row > 0.
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "root", Row: 7})     // stale source row
	_ = g.AddNode(dag.Node{ID: "dep", Row: 3})      // stale child row
	_ = g.AddNode(dag.Node{ID: "isolated", Row: 9}) // disconnected node
	_ = g.AddEdge(dag.Edge{From: "root", To: "dep"})

	AssignLayers(g)

	checkRow(t, g, "root", 0)
	checkRow(t, g, "dep", 1)
	checkRow(t, g, "isolated", 0)

	// The row index must agree with the node rows.
	if got := len(g.NodesInRow(0)); got != 2 {
		t.Errorf("NodesInRow(0) = %d nodes, want 2", got)
	}
	if got := len(g.NodesInRow(7)); got != 0 {
		t.Errorf("NodesInRow(7) = %d nodes, want 0 (stale row)", got)
	}
}

func TestIsLayered(t *testing.T) {
	g := dag.New(nil)
	if !IsLayered(g) {
		t.Error("empty graph should be layered")
	}

	_ = g.AddNode(dag.Node{ID: "a"})
	_ = g.AddNode(dag.Node{ID: "b"})
	_ = g.AddEdge(dag.Edge{From: "a", To: "b"})

	// All rows zero with an edge → not layered (edge within a row).
	if IsLayered(g) {
		t.Error("unlayered graph with edges should not be layered")
	}

	AssignLayers(g)
	if !IsLayered(g) {
		t.Error("graph should be layered after AssignLayers")
	}

	// Stale rows: edge pointing upward.
	g2 := dag.New(nil)
	_ = g2.AddNode(dag.Node{ID: "a", Row: 2})
	_ = g2.AddNode(dag.Node{ID: "b", Row: 1})
	_ = g2.AddNode(dag.Node{ID: "c", Row: 0})
	_ = g2.AddEdge(dag.Edge{From: "a", To: "b"})
	if IsLayered(g2) {
		t.Error("graph with upward edge should not be layered")
	}

	// No node on row 0.
	g3 := dag.New(nil)
	_ = g3.AddNode(dag.Node{ID: "a", Row: 1})
	_ = g3.AddNode(dag.Node{ID: "b", Row: 2})
	_ = g3.AddEdge(dag.Edge{From: "a", To: "b"})
	if IsLayered(g3) {
		t.Error("graph without row-0 nodes should not be layered")
	}

	// Edges spanning multiple rows are fine (pre-subdivision).
	g4 := dag.New(nil)
	_ = g4.AddNode(dag.Node{ID: "a", Row: 0})
	_ = g4.AddNode(dag.Node{ID: "b", Row: 2})
	_ = g4.AddEdge(dag.Edge{From: "a", To: "b"})
	if !IsLayered(g4) {
		t.Error("multi-row edge spans should still count as layered")
	}
}

func TestAssignLayers_FanInFanOut(t *testing.T) {
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "a"})
	_ = g.AddNode(dag.Node{ID: "b"})
	_ = g.AddNode(dag.Node{ID: "c"})
	_ = g.AddNode(dag.Node{ID: "d"})
	_ = g.AddNode(dag.Node{ID: "e"})
	_ = g.AddNode(dag.Node{ID: "f"})

	_ = g.AddEdge(dag.Edge{From: "a", To: "c"})
	_ = g.AddEdge(dag.Edge{From: "b", To: "c"})
	_ = g.AddEdge(dag.Edge{From: "c", To: "d"})
	_ = g.AddEdge(dag.Edge{From: "c", To: "e"})
	_ = g.AddEdge(dag.Edge{From: "d", To: "f"})
	_ = g.AddEdge(dag.Edge{From: "e", To: "f"})

	AssignLayers(g)

	checkRow(t, g, "a", 0)
	checkRow(t, g, "b", 0)
	checkRow(t, g, "c", 1)
	checkRow(t, g, "d", 2)
	checkRow(t, g, "e", 2)
	checkRow(t, g, "f", 3)
}
