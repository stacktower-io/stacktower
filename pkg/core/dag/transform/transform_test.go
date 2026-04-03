package transform

import (
	"testing"

	"github.com/matzehuels/stacktower/pkg/core/dag"
)

func buildSimpleDAG() *dag.DAG {
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "a", Row: 0})
	_ = g.AddNode(dag.Node{ID: "b", Row: 1})
	_ = g.AddNode(dag.Node{ID: "c", Row: 2})
	_ = g.AddEdge(dag.Edge{From: "a", To: "b"})
	_ = g.AddEdge(dag.Edge{From: "b", To: "c"})
	return g
}

func buildDiamondDAG() *dag.DAG {
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "a", Row: 0})
	_ = g.AddNode(dag.Node{ID: "b", Row: 1})
	_ = g.AddNode(dag.Node{ID: "c", Row: 1})
	_ = g.AddNode(dag.Node{ID: "d", Row: 2})
	_ = g.AddEdge(dag.Edge{From: "a", To: "b"})
	_ = g.AddEdge(dag.Edge{From: "a", To: "c"})
	_ = g.AddEdge(dag.Edge{From: "b", To: "d"})
	_ = g.AddEdge(dag.Edge{From: "c", To: "d"})
	return g
}

func TestNormalize_EmptyGraph_ReturnsEmpty(t *testing.T) {
	g := dag.New(nil)
	result, err := Normalize(g)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if g.NodeCount() != 0 {
		t.Errorf("expected 0 nodes, got %d", g.NodeCount())
	}
	if result.CyclesRemoved != 0 || result.TransitiveEdgesRemoved != 0 || result.SubdividersAdded != 0 {
		t.Errorf("expected zero metrics for empty graph, got %+v", result)
	}
}

func TestNormalize_SimpleGraph_AppliesPipeline(t *testing.T) {
	g := buildSimpleDAG()
	result, err := Normalize(g)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if g.NodeCount() == 0 {
		t.Error("expected non-empty result")
	}
	if result == nil {
		t.Error("expected non-nil result")
	}
}

func TestTransitiveReduction_EmptyGraph_Noop(t *testing.T) {
	g := dag.New(nil)
	TransitiveReduction(g)
	if g.NodeCount() != 0 {
		t.Errorf("expected 0 nodes, got %d", g.NodeCount())
	}
}

func TestTransitiveReduction_NoRedundantEdges_Unchanged(t *testing.T) {
	g := buildSimpleDAG()
	TransitiveReduction(g)

	if g.NodeCount() != 3 {
		t.Errorf("expected %d nodes, got %d", 3, g.NodeCount())
	}
	if g.EdgeCount() != 2 {
		t.Errorf("expected %d edges, got %d", 2, g.EdgeCount())
	}
}

func TestTransitiveReduction_RedundantEdge_Removed(t *testing.T) {
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "a", Row: 0})
	_ = g.AddNode(dag.Node{ID: "b", Row: 1})
	_ = g.AddNode(dag.Node{ID: "c", Row: 2})
	_ = g.AddEdge(dag.Edge{From: "a", To: "b"})
	_ = g.AddEdge(dag.Edge{From: "b", To: "c"})
	_ = g.AddEdge(dag.Edge{From: "a", To: "c"})

	TransitiveReduction(g)

	if g.NodeCount() != 3 {
		t.Errorf("expected 3 nodes, got %d", g.NodeCount())
	}
	if g.EdgeCount() != 2 {
		t.Errorf("expected 2 edges (redundant removed), got %d", g.EdgeCount())
	}
}

func TestTransitiveReduction_Diamond_PreservesStructure(t *testing.T) {
	g := buildDiamondDAG()
	TransitiveReduction(g)

	if g.NodeCount() != 4 {
		t.Errorf("expected 4 nodes, got %d", g.NodeCount())
	}
	if g.EdgeCount() != 4 {
		t.Errorf("expected 4 edges (no reduction), got %d", g.EdgeCount())
	}
}

func TestResolveSpanOverlaps_NoopForNow(t *testing.T) {
	g := buildSimpleDAG()
	beforeNodes, beforeEdges := g.NodeCount(), g.EdgeCount()
	ResolveSpanOverlaps(g)

	if g.NodeCount() != beforeNodes {
		t.Errorf("expected %d nodes, got %d", beforeNodes, g.NodeCount())
	}
	if g.EdgeCount() != beforeEdges {
		t.Errorf("expected %d edges, got %d", beforeEdges, g.EdgeCount())
	}
}

func TestSubdivide_EmptyGraph_Noop(t *testing.T) {
	g := dag.New(nil)
	Subdivide(g)
	if g.NodeCount() != 0 {
		t.Errorf("expected 0 nodes, got %d", g.NodeCount())
	}
}

func TestSubdivide_ConsecutiveRows_NoSubdivision(t *testing.T) {
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "a", Row: 0})
	_ = g.AddNode(dag.Node{ID: "b", Row: 1})
	_ = g.AddEdge(dag.Edge{From: "a", To: "b"})

	Subdivide(g)

	if g.NodeCount() != 2 {
		t.Errorf("expected 2 nodes (no subdivision), got %d", g.NodeCount())
	}
	if g.EdgeCount() != 1 {
		t.Errorf("expected 1 edge, got %d", g.EdgeCount())
	}
}

func TestSubdivide_LongEdge_InsertsSubdividers(t *testing.T) {
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "a", Row: 0})
	_ = g.AddNode(dag.Node{ID: "b", Row: 3})
	_ = g.AddEdge(dag.Edge{From: "a", To: "b"})

	Subdivide(g)

	expectedNodes := 4
	if g.NodeCount() != expectedNodes {
		t.Errorf("expected %d nodes (2 original + 2 subdividers), got %d", expectedNodes, g.NodeCount())
	}

	expectedEdges := 3
	if g.EdgeCount() != expectedEdges {
		t.Errorf("expected %d edges, got %d", expectedEdges, g.EdgeCount())
	}

	nodes := g.Nodes()
	subdividerCount := 0
	for _, n := range nodes {
		if n.IsSubdivider() {
			subdividerCount++
			if n.MasterID != "a" {
				t.Errorf("expected MasterID 'a', got '%s'", n.MasterID)
			}
		}
	}
	if subdividerCount != 2 {
		t.Errorf("expected 2 subdivider nodes, got %d", subdividerCount)
	}
}

func TestSubdivide_MultipleEdges_SubdividesAll(t *testing.T) {
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "a", Row: 0})
	_ = g.AddNode(dag.Node{ID: "b", Row: 0})
	_ = g.AddNode(dag.Node{ID: "c", Row: 3})
	_ = g.AddEdge(dag.Edge{From: "a", To: "c"})
	_ = g.AddEdge(dag.Edge{From: "b", To: "c"})

	Subdivide(g)

	expectedNodes := 7
	if g.NodeCount() != expectedNodes {
		t.Errorf("expected %d nodes (3 original + 4 subdividers), got %d", expectedNodes, g.NodeCount())
	}
}

func TestSubdivide_PreservesMetadata(t *testing.T) {
	meta := dag.Metadata{"key": "value"}
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "a", Row: 0, Meta: meta})
	_ = g.AddNode(dag.Node{ID: "b", Row: 2})
	_ = g.AddEdge(dag.Edge{From: "a", To: "b", Meta: meta})

	Subdivide(g)

	edges := g.Edges()
	finalEdge := edges[len(edges)-1]
	if finalEdge.Meta["key"] != "value" {
		t.Error("expected metadata preserved on final edge")
	}
}

func TestSubdivide_SubdividerIDsAreUnique(t *testing.T) {
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "a", Row: 0})
	_ = g.AddNode(dag.Node{ID: "b", Row: 5})
	_ = g.AddEdge(dag.Edge{From: "a", To: "b"})

	Subdivide(g)

	nodes := g.Nodes()
	ids := make(map[string]bool)
	for _, n := range nodes {
		if ids[n.ID] {
			t.Errorf("duplicate node ID: %s", n.ID)
		}
		ids[n.ID] = true
	}
}

func TestSubdivide_ExtendsSinksToMaxRow(t *testing.T) {
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "a", Row: 0})
	_ = g.AddNode(dag.Node{ID: "b", Row: 1})
	_ = g.AddNode(dag.Node{ID: "c", Row: 0})
	_ = g.AddNode(dag.Node{ID: "d", Row: 3})
	_ = g.AddEdge(dag.Edge{From: "a", To: "b"})
	_ = g.AddEdge(dag.Edge{From: "c", To: "d"})

	Subdivide(g)

	nodes := g.Nodes()
	maxRow := 0
	for _, n := range nodes {
		if n.Row > maxRow {
			maxRow = n.Row
		}
	}

	for _, n := range nodes {
		if g.OutDegree(n.ID) == 0 && n.Row != maxRow {
			t.Errorf("sink node %s not extended to max row %d, got row %d", n.ID, maxRow, n.Row)
		}
	}
}

func TestSubdivide_HandlesMultipleSinks(t *testing.T) {
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "a", Row: 0})
	_ = g.AddNode(dag.Node{ID: "b", Row: 0})
	_ = g.AddNode(dag.Node{ID: "c", Row: 1})
	_ = g.AddNode(dag.Node{ID: "d", Row: 1})
	_ = g.AddEdge(dag.Edge{From: "a", To: "c"})
	_ = g.AddEdge(dag.Edge{From: "b", To: "d"})

	Subdivide(g)

	sinkCount := 0
	for _, n := range g.Nodes() {
		if g.OutDegree(n.ID) == 0 {
			sinkCount++
		}
	}

	if sinkCount != 2 {
		t.Errorf("expected 2 sinks, got %d", sinkCount)
	}
}

func TestTransitiveReduction_ComplexGraph(t *testing.T) {
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "a", Row: 0})
	_ = g.AddNode(dag.Node{ID: "b", Row: 1})
	_ = g.AddNode(dag.Node{ID: "c", Row: 1})
	_ = g.AddNode(dag.Node{ID: "d", Row: 2})
	_ = g.AddNode(dag.Node{ID: "e", Row: 3})

	_ = g.AddEdge(dag.Edge{From: "a", To: "b"})
	_ = g.AddEdge(dag.Edge{From: "a", To: "c"})
	_ = g.AddEdge(dag.Edge{From: "b", To: "d"})
	_ = g.AddEdge(dag.Edge{From: "c", To: "d"})
	_ = g.AddEdge(dag.Edge{From: "d", To: "e"})
	_ = g.AddEdge(dag.Edge{From: "a", To: "d"})
	_ = g.AddEdge(dag.Edge{From: "b", To: "e"})

	TransitiveReduction(g)

	if g.EdgeCount() > 5 {
		t.Errorf("expected <= 5 edges after reduction, got %d", g.EdgeCount())
	}

	hasEdge := func(from, to string) bool {
		for _, e := range g.Edges() {
			if e.From == from && e.To == to {
				return true
			}
		}
		return false
	}

	if hasEdge("a", "d") {
		t.Error("redundant edge a->d should be removed")
	}
	if hasEdge("b", "e") {
		t.Error("redundant edge b->e should be removed")
	}
}

func TestTransitiveReduction_MultipleRedundantPaths(t *testing.T) {
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "a", Row: 0})
	_ = g.AddNode(dag.Node{ID: "b", Row: 1})
	_ = g.AddNode(dag.Node{ID: "c", Row: 2})
	_ = g.AddNode(dag.Node{ID: "d", Row: 3})

	_ = g.AddEdge(dag.Edge{From: "a", To: "b"})
	_ = g.AddEdge(dag.Edge{From: "b", To: "c"})
	_ = g.AddEdge(dag.Edge{From: "c", To: "d"})
	_ = g.AddEdge(dag.Edge{From: "a", To: "c"})
	_ = g.AddEdge(dag.Edge{From: "a", To: "d"})
	_ = g.AddEdge(dag.Edge{From: "b", To: "d"})

	TransitiveReduction(g)

	if g.EdgeCount() != 3 {
		t.Errorf("expected 3 edges after reduction (only direct paths), got %d", g.EdgeCount())
	}
}

func TestTransitiveReduction_PreservesDirectEdges(t *testing.T) {
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "a", Row: 0})
	_ = g.AddNode(dag.Node{ID: "b", Row: 1})
	_ = g.AddNode(dag.Node{ID: "c", Row: 1})
	_ = g.AddEdge(dag.Edge{From: "a", To: "b"})
	_ = g.AddEdge(dag.Edge{From: "a", To: "c"})

	TransitiveReduction(g)

	if g.EdgeCount() != 2 {
		t.Errorf("expected 2 edges (no reduction for parallel edges), got %d", g.EdgeCount())
	}
}

func TestNormalize_CompleteWorkflow(t *testing.T) {
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "a"})
	_ = g.AddNode(dag.Node{ID: "b"})
	_ = g.AddNode(dag.Node{ID: "c"})
	_ = g.AddNode(dag.Node{ID: "d"})

	_ = g.AddEdge(dag.Edge{From: "a", To: "b"})
	_ = g.AddEdge(dag.Edge{From: "b", To: "c"})
	_ = g.AddEdge(dag.Edge{From: "a", To: "c"})
	_ = g.AddEdge(dag.Edge{From: "c", To: "d"})

	result, err := Normalize(g)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if g.EdgeCount() != 3 {
		t.Errorf("expected 3 edges after reduction, got %d", g.EdgeCount())
	}

	nodeA, _ := g.Node("a")
	if nodeA.Row != 0 {
		t.Errorf("expected node a at row 0, got %d", nodeA.Row)
	}

	allNodesHaveRows := true
	for _, n := range g.Nodes() {
		if !n.IsSubdivider() {
			if n.Row < 0 {
				allNodesHaveRows = false
				break
			}
		}
	}
	if !allNodesHaveRows {
		t.Error("not all nodes have valid rows assigned")
	}

	if result.TransitiveEdgesRemoved != 1 {
		t.Errorf("expected 1 transitive edge removed, got %d", result.TransitiveEdgesRemoved)
	}
}

func TestNormalize_ReturnsResult(t *testing.T) {
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "a"})

	result, err := Normalize(g)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result == nil {
		t.Error("Normalize should return a non-nil TransformResult")
	}
	if g.NodeCount() != 1 {
		t.Error("Normalize should modify DAG in-place")
	}
}

func TestNormalize_IntegrationWithSubdivision(t *testing.T) {
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "a"})
	_ = g.AddNode(dag.Node{ID: "b"})
	_ = g.AddNode(dag.Node{ID: "c"})
	_ = g.AddEdge(dag.Edge{From: "a", To: "c"})
	_ = g.AddEdge(dag.Edge{From: "b", To: "c"})

	result, err := Normalize(g)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	nodeA, _ := g.Node("a")
	nodeB, _ := g.Node("b")
	nodeC, _ := g.Node("c")

	if nodeA.Row != 0 || nodeB.Row != 0 {
		t.Error("roots should be at row 0")
	}
	if nodeC.Row != 1 {
		t.Errorf("node c should be at row 1, got %d", nodeC.Row)
	}

	for _, n := range g.Nodes() {
		if n.Row < 0 {
			t.Errorf("node %s has invalid row %d", n.ID, n.Row)
		}
	}

	if result == nil {
		t.Error("expected non-nil result")
	}
}
