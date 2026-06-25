package transform

import (
	"testing"

	"github.com/stacktower-io/stacktower/pkg/core/dag"
)

func hasEdge(g *dag.DAG, from, to string) bool {
	for _, e := range g.Edges() {
		if e.From == from && e.To == to {
			return true
		}
	}
	return false
}

func TestTransitiveReduction_SingleNode(t *testing.T) {
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "a", Row: 0})
	TransitiveReduction(g)

	if g.NodeCount() != 1 {
		t.Errorf("expected 1 node, got %d", g.NodeCount())
	}
	if g.EdgeCount() != 0 {
		t.Errorf("expected 0 edges, got %d", g.EdgeCount())
	}
}

func TestTransitiveReduction_TwoNodesNoEdge(t *testing.T) {
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "a", Row: 0})
	_ = g.AddNode(dag.Node{ID: "b", Row: 1})
	TransitiveReduction(g)

	if g.NodeCount() != 2 {
		t.Errorf("expected 2 nodes, got %d", g.NodeCount())
	}
	if g.EdgeCount() != 0 {
		t.Errorf("expected 0 edges, got %d", g.EdgeCount())
	}
}

func TestTransitiveReduction_Triangle(t *testing.T) {
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "a", Row: 0})
	_ = g.AddNode(dag.Node{ID: "b", Row: 1})
	_ = g.AddNode(dag.Node{ID: "c", Row: 2})
	_ = g.AddEdge(dag.Edge{From: "a", To: "b"})
	_ = g.AddEdge(dag.Edge{From: "b", To: "c"})
	_ = g.AddEdge(dag.Edge{From: "a", To: "c"})

	TransitiveReduction(g)

	if !hasEdge(g, "a", "b") {
		t.Error("direct edge a->b should be preserved")
	}
	if !hasEdge(g, "b", "c") {
		t.Error("direct edge b->c should be preserved")
	}
	if hasEdge(g, "a", "c") {
		t.Error("transitive edge a->c should be removed")
	}
}

func TestTransitiveReduction_Square(t *testing.T) {
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "a", Row: 0})
	_ = g.AddNode(dag.Node{ID: "b", Row: 1})
	_ = g.AddNode(dag.Node{ID: "c", Row: 1})
	_ = g.AddNode(dag.Node{ID: "d", Row: 2})
	_ = g.AddEdge(dag.Edge{From: "a", To: "b"})
	_ = g.AddEdge(dag.Edge{From: "a", To: "c"})
	_ = g.AddEdge(dag.Edge{From: "b", To: "d"})
	_ = g.AddEdge(dag.Edge{From: "c", To: "d"})

	TransitiveReduction(g)

	if g.EdgeCount() != 4 {
		t.Errorf("expected 4 edges (no reduction in diamond), got %d", g.EdgeCount())
	}
}

func TestTransitiveReduction_LongChainWithShortcut(t *testing.T) {
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "a", Row: 0})
	_ = g.AddNode(dag.Node{ID: "b", Row: 1})
	_ = g.AddNode(dag.Node{ID: "c", Row: 2})
	_ = g.AddNode(dag.Node{ID: "d", Row: 3})
	_ = g.AddNode(dag.Node{ID: "e", Row: 4})

	_ = g.AddEdge(dag.Edge{From: "a", To: "b"})
	_ = g.AddEdge(dag.Edge{From: "b", To: "c"})
	_ = g.AddEdge(dag.Edge{From: "c", To: "d"})
	_ = g.AddEdge(dag.Edge{From: "d", To: "e"})
	_ = g.AddEdge(dag.Edge{From: "a", To: "e"})

	TransitiveReduction(g)

	if hasEdge(g, "a", "e") {
		t.Error("long transitive edge a->e should be removed")
	}
	if g.EdgeCount() != 4 {
		t.Errorf("expected 4 edges (chain only), got %d", g.EdgeCount())
	}
}

func TestTransitiveReduction_MultipleRedundancies(t *testing.T) {
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "a", Row: 0})
	_ = g.AddNode(dag.Node{ID: "b", Row: 1})
	_ = g.AddNode(dag.Node{ID: "c", Row: 1})
	_ = g.AddNode(dag.Node{ID: "d", Row: 2})

	_ = g.AddEdge(dag.Edge{From: "a", To: "b"})
	_ = g.AddEdge(dag.Edge{From: "a", To: "c"})
	_ = g.AddEdge(dag.Edge{From: "b", To: "d"})
	_ = g.AddEdge(dag.Edge{From: "c", To: "d"})
	_ = g.AddEdge(dag.Edge{From: "a", To: "d"})

	TransitiveReduction(g)

	if hasEdge(g, "a", "d") {
		t.Error("transitive edge a->d should be removed")
	}
	if g.EdgeCount() != 4 {
		t.Errorf("expected 4 edges, got %d", g.EdgeCount())
	}
}

func TestTransitiveReduction_PreservesEdgeMetadata(t *testing.T) {
	meta := dag.Metadata{"important": "data"}
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "a", Row: 0})
	_ = g.AddNode(dag.Node{ID: "b", Row: 1})
	_ = g.AddEdge(dag.Edge{From: "a", To: "b", Meta: meta})

	TransitiveReduction(g)

	edges := g.Edges()
	if len(edges) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(edges))
	}
	if edges[0].Meta["important"] != "data" {
		t.Error("edge metadata should be preserved")
	}
}

func TestTransitiveReduction_NoModificationOnIrreducible(t *testing.T) {
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "a", Row: 0})
	_ = g.AddNode(dag.Node{ID: "b", Row: 1})
	_ = g.AddNode(dag.Node{ID: "c", Row: 1})
	_ = g.AddNode(dag.Node{ID: "d", Row: 2})
	_ = g.AddNode(dag.Node{ID: "e", Row: 2})

	_ = g.AddEdge(dag.Edge{From: "a", To: "b"})
	_ = g.AddEdge(dag.Edge{From: "a", To: "c"})
	_ = g.AddEdge(dag.Edge{From: "b", To: "d"})
	_ = g.AddEdge(dag.Edge{From: "b", To: "e"})
	_ = g.AddEdge(dag.Edge{From: "c", To: "d"})
	_ = g.AddEdge(dag.Edge{From: "c", To: "e"})

	edgesBefore := g.EdgeCount()
	TransitiveReduction(g)
	edgesAfter := g.EdgeCount()

	if edgesBefore != edgesAfter {
		t.Errorf("irreducible graph should not change: had %d edges, now %d", edgesBefore, edgesAfter)
	}
}

// TestTransitiveReduction_DeepSkipEdge verifies that redundancy is detected
// across long alternate paths, not just paths through a single intermediate.
func TestTransitiveReduction_DeepSkipEdge(t *testing.T) {
	g := dag.New(nil)
	for _, id := range []string{"a", "b", "c", "d", "e"} {
		_ = g.AddNode(dag.Node{ID: id})
	}
	// Chain a→b→c→d→e plus a skip edge a→e (redundant via the chain).
	_ = g.AddEdge(dag.Edge{From: "a", To: "b"})
	_ = g.AddEdge(dag.Edge{From: "b", To: "c"})
	_ = g.AddEdge(dag.Edge{From: "c", To: "d"})
	_ = g.AddEdge(dag.Edge{From: "d", To: "e"})
	_ = g.AddEdge(dag.Edge{From: "a", To: "e"})

	TransitiveReduction(g)

	hasEdge := func(from, to string) bool {
		for _, e := range g.Edges() {
			if e.From == from && e.To == to {
				return true
			}
		}
		return false
	}

	if hasEdge("a", "e") {
		t.Error("a→e should be removed: a reaches e via the chain")
	}
	for _, e := range [][2]string{{"a", "b"}, {"b", "c"}, {"c", "d"}, {"d", "e"}} {
		if !hasEdge(e[0], e[1]) {
			t.Errorf("chain edge %s→%s should be kept", e[0], e[1])
		}
	}
}
