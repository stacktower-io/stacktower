package transform

import (
	"testing"

	"github.com/stacktower-io/stacktower/pkg/core/dag"
)

func TestResolveSpanOverlaps_NoOverlaps(t *testing.T) {
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "a", Row: 0})
	_ = g.AddNode(dag.Node{ID: "b", Row: 1})
	_ = g.AddNode(dag.Node{ID: "c", Row: 1})
	_ = g.AddEdge(dag.Edge{From: "a", To: "b"})

	nodesBefore := g.NodeCount()
	ResolveSpanOverlaps(g)

	if g.NodeCount() != nodesBefore {
		t.Errorf("expected no separators for single-child parent, got %d nodes (was %d)", g.NodeCount(), nodesBefore)
	}
}

func TestResolveSpanOverlaps_SimpleOverlap(t *testing.T) {
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "p1", Row: 0})
	_ = g.AddNode(dag.Node{ID: "p2", Row: 0})
	_ = g.AddNode(dag.Node{ID: "c1", Row: 1})
	_ = g.AddNode(dag.Node{ID: "c2", Row: 1})
	_ = g.AddNode(dag.Node{ID: "c3", Row: 1})

	_ = g.AddEdge(dag.Edge{From: "p1", To: "c1"})
	_ = g.AddEdge(dag.Edge{From: "p1", To: "c3"})
	_ = g.AddEdge(dag.Edge{From: "p2", To: "c2"})
	_ = g.AddEdge(dag.Edge{From: "p2", To: "c3"})

	nodesBefore := g.NodeCount()
	ResolveSpanOverlaps(g)

	if g.NodeCount() <= nodesBefore {
		t.Errorf("expected separators to be inserted, got %d nodes (was %d)", g.NodeCount(), nodesBefore)
	}

	separatorCount := 0
	for _, n := range g.Nodes() {
		if n.IsAuxiliary() {
			separatorCount++
		}
	}

	if separatorCount == 0 {
		t.Error("expected at least one separator node for overlapping spans")
	}
}

func TestResolveSpanOverlaps_MixedRowWithSubdividers(t *testing.T) {
	// Regression test: a row containing subdivider pass-throughs used to be
	// skipped entirely, leaving K(2,2) tangles among the regular nodes
	// unresolved. The subdivider column itself must stay intact.
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "p1", Row: 0})
	_ = g.AddNode(dag.Node{ID: "p2", Row: 0})
	_ = g.AddNode(dag.Node{ID: "other", Row: 0})
	_ = g.AddNode(dag.Node{ID: "c1", Row: 1})
	_ = g.AddNode(dag.Node{ID: "c2", Row: 1})
	_ = g.AddNode(dag.Node{ID: "sub", Row: 1, Kind: dag.NodeKindSubdivider, MasterID: "other"})
	_ = g.AddNode(dag.Node{ID: "target", Row: 2})

	// K(2,2) tangle between {p1,p2} and {c1,c2}.
	_ = g.AddEdge(dag.Edge{From: "p1", To: "c1"})
	_ = g.AddEdge(dag.Edge{From: "p1", To: "c2"})
	_ = g.AddEdge(dag.Edge{From: "p2", To: "c1"})
	_ = g.AddEdge(dag.Edge{From: "p2", To: "c2"})
	// Subdivider column passing through row 1.
	_ = g.AddEdge(dag.Edge{From: "other", To: "sub"})
	_ = g.AddEdge(dag.Edge{From: "sub", To: "target"})

	ResolveSpanOverlaps(g)

	separatorCount := 0
	for _, n := range g.Nodes() {
		if n.IsAuxiliary() {
			separatorCount++
		}
	}
	if separatorCount == 0 {
		t.Error("expected a separator despite subdividers elsewhere in the row")
	}

	// The subdivider column must not be rerouted through a separator.
	sub, _ := g.Node("sub")
	if sub == nil {
		t.Fatal("subdivider missing")
	}
	for _, parent := range g.Parents("sub") {
		if n, ok := g.Node(parent); ok && n.IsAuxiliary() {
			t.Errorf("subdivider parent %q is a separator; column was broken", parent)
		}
	}
	if kids := g.Children("sub"); len(kids) != 1 || kids[0] != "target" {
		t.Errorf("subdivider children = %v, want [target]", kids)
	}
}

func TestResolveSpanOverlaps_ThreeParentsOverlap(t *testing.T) {
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "p1", Row: 0})
	_ = g.AddNode(dag.Node{ID: "p2", Row: 0})
	_ = g.AddNode(dag.Node{ID: "p3", Row: 0})
	_ = g.AddNode(dag.Node{ID: "c1", Row: 1})
	_ = g.AddNode(dag.Node{ID: "c2", Row: 1})
	_ = g.AddNode(dag.Node{ID: "c3", Row: 1})
	_ = g.AddNode(dag.Node{ID: "c4", Row: 1})

	_ = g.AddEdge(dag.Edge{From: "p1", To: "c1"})
	_ = g.AddEdge(dag.Edge{From: "p1", To: "c3"})
	_ = g.AddEdge(dag.Edge{From: "p2", To: "c1"})
	_ = g.AddEdge(dag.Edge{From: "p2", To: "c4"})
	_ = g.AddEdge(dag.Edge{From: "p3", To: "c2"})
	_ = g.AddEdge(dag.Edge{From: "p3", To: "c4"})

	ResolveSpanOverlaps(g)

	separatorCount := 0
	for _, n := range g.Nodes() {
		if n.IsAuxiliary() {
			separatorCount++
		}
	}

	if separatorCount == 0 {
		t.Error("expected separators for three overlapping parent spans")
	}
}

func TestResolveSpanOverlaps_MultiLevel(t *testing.T) {
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "a", Row: 0})
	_ = g.AddNode(dag.Node{ID: "b", Row: 0})
	_ = g.AddNode(dag.Node{ID: "c1", Row: 1})
	_ = g.AddNode(dag.Node{ID: "c2", Row: 1})
	_ = g.AddNode(dag.Node{ID: "d1", Row: 2})
	_ = g.AddNode(dag.Node{ID: "d2", Row: 2})

	_ = g.AddEdge(dag.Edge{From: "a", To: "c1"})
	_ = g.AddEdge(dag.Edge{From: "a", To: "c2"})
	_ = g.AddEdge(dag.Edge{From: "b", To: "c1"})
	_ = g.AddEdge(dag.Edge{From: "b", To: "c2"})
	_ = g.AddEdge(dag.Edge{From: "c1", To: "d1"})
	_ = g.AddEdge(dag.Edge{From: "c1", To: "d2"})
	_ = g.AddEdge(dag.Edge{From: "c2", To: "d1"})
	_ = g.AddEdge(dag.Edge{From: "c2", To: "d2"})

	nodesBefore := g.NodeCount()
	ResolveSpanOverlaps(g)

	if g.NodeCount() <= nodesBefore {
		t.Error("expected separators for multi-level overlaps")
	}
}

func TestResolveSpanOverlaps_SeparatorPlacement(t *testing.T) {
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "p1", Row: 0})
	_ = g.AddNode(dag.Node{ID: "p2", Row: 0})
	_ = g.AddNode(dag.Node{ID: "c1", Row: 1})
	_ = g.AddNode(dag.Node{ID: "c2", Row: 1})

	_ = g.AddEdge(dag.Edge{From: "p1", To: "c1"})
	_ = g.AddEdge(dag.Edge{From: "p1", To: "c2"})
	_ = g.AddEdge(dag.Edge{From: "p2", To: "c1"})
	_ = g.AddEdge(dag.Edge{From: "p2", To: "c2"})

	ResolveSpanOverlaps(g)

	var separator *dag.Node
	for _, n := range g.Nodes() {
		if n.IsAuxiliary() {
			separator = n
			break
		}
	}

	if separator == nil {
		t.Fatal("expected a separator node")
		return
	}

	// Separator should be in intermediate row between parents (0) and children (now at 2)
	if separator.Row != 1 {
		t.Errorf("separator should be at intermediate row 1, got row %d", separator.Row)
	}

	// Verify parents are still at row 0
	p1, _ := g.Node("p1")
	if p1.Row != 0 {
		t.Errorf("parent should remain at row 0, got %d", p1.Row)
	}

	// Verify children are now at row 2
	c1, _ := g.Node("c1")
	if c1.Row != 2 {
		t.Errorf("children should be shifted to row 2, got %d", c1.Row)
	}

	children := g.Children(separator.ID)
	if len(children) < 2 {
		t.Errorf("separator should have multiple children, got %d", len(children))
	}
}

func TestResolveSpanOverlaps_EdgeRedirection(t *testing.T) {
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "p1", Row: 0})
	_ = g.AddNode(dag.Node{ID: "p2", Row: 0})
	_ = g.AddNode(dag.Node{ID: "c1", Row: 1})
	_ = g.AddNode(dag.Node{ID: "c2", Row: 1})
	_ = g.AddNode(dag.Node{ID: "c3", Row: 1})

	_ = g.AddEdge(dag.Edge{From: "p1", To: "c1"})
	_ = g.AddEdge(dag.Edge{From: "p1", To: "c3"})
	_ = g.AddEdge(dag.Edge{From: "p2", To: "c2"})
	_ = g.AddEdge(dag.Edge{From: "p2", To: "c3"})

	ResolveSpanOverlaps(g)

	var separator *dag.Node
	for _, n := range g.Nodes() {
		if n.IsAuxiliary() {
			separator = n
			break
		}
	}

	if separator == nil {
		t.Fatal("expected a separator node")
	}

	edgeCount := 0
	for _, e := range g.Edges() {
		if e.From == "p1" && e.To == separator.ID {
			edgeCount++
		}
		if e.From == "p2" && e.To == separator.ID {
			edgeCount++
		}
	}

	if edgeCount != 2 {
		t.Errorf("expected 2 edges redirected to separator (one from each parent), got %d", edgeCount)
	}

	directToC2 := 0
	directToC3 := 0
	for _, e := range g.Edges() {
		src, _ := g.Node(e.From)
		if src != nil && src.Row == 0 {
			if e.To == "c2" {
				directToC2++
			}
			if e.To == "c3" {
				directToC3++
			}
		}
	}

	if directToC2 > 0 || directToC3 > 0 {
		t.Errorf("expected no direct edges from parents to contested children c2 and c3, got %d to c2 and %d to c3", directToC2, directToC3)
	}
}

func TestResolveSpanOverlaps_SingleChildSpanNoSeparator(t *testing.T) {
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "p1", Row: 0})
	_ = g.AddNode(dag.Node{ID: "p2", Row: 0})
	_ = g.AddNode(dag.Node{ID: "c1", Row: 1})
	_ = g.AddNode(dag.Node{ID: "c2", Row: 1})
	_ = g.AddNode(dag.Node{ID: "c3", Row: 1})

	_ = g.AddEdge(dag.Edge{From: "p1", To: "c1"})
	_ = g.AddEdge(dag.Edge{From: "p2", To: "c3"})

	nodesBefore := g.NodeCount()
	ResolveSpanOverlaps(g)

	if g.NodeCount() != nodesBefore {
		t.Error("expected no separators when parent spans don't overlap")
	}
}

func TestResolveSpanOverlaps_PartialOverlap(t *testing.T) {
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "p1", Row: 0})
	_ = g.AddNode(dag.Node{ID: "p2", Row: 0})
	_ = g.AddNode(dag.Node{ID: "c1", Row: 1})
	_ = g.AddNode(dag.Node{ID: "c2", Row: 1})
	_ = g.AddNode(dag.Node{ID: "c3", Row: 1})
	_ = g.AddNode(dag.Node{ID: "c4", Row: 1})

	_ = g.AddEdge(dag.Edge{From: "p1", To: "c1"})
	_ = g.AddEdge(dag.Edge{From: "p1", To: "c2"})
	_ = g.AddEdge(dag.Edge{From: "p2", To: "c3"})
	_ = g.AddEdge(dag.Edge{From: "p2", To: "c4"})

	nodesBefore := g.NodeCount()
	ResolveSpanOverlaps(g)

	if g.NodeCount() != nodesBefore {
		t.Error("expected no separators when spans don't overlap")
	}
}

func TestResolveSpanOverlaps_IDCollisionHandling(t *testing.T) {
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "p1", Row: 0})
	_ = g.AddNode(dag.Node{ID: "p2", Row: 0})
	_ = g.AddNode(dag.Node{ID: "c1", Row: 1})
	_ = g.AddNode(dag.Node{ID: "c2", Row: 1})
	_ = g.AddNode(dag.Node{ID: "Sep_1_c1_c2", Row: 1})

	_ = g.AddEdge(dag.Edge{From: "p1", To: "c1"})
	_ = g.AddEdge(dag.Edge{From: "p1", To: "c2"})
	_ = g.AddEdge(dag.Edge{From: "p2", To: "c1"})
	_ = g.AddEdge(dag.Edge{From: "p2", To: "c2"})

	ResolveSpanOverlaps(g)

	ids := make(map[string]bool)
	for _, n := range g.Nodes() {
		if ids[n.ID] {
			t.Errorf("duplicate ID found: %s", n.ID)
		}
		ids[n.ID] = true
	}
}

func TestResolveSpanOverlaps_Deterministic(t *testing.T) {
	buildGraph := func() *dag.DAG {
		g := dag.New(nil)
		_ = g.AddNode(dag.Node{ID: "p1", Row: 0})
		_ = g.AddNode(dag.Node{ID: "p2", Row: 0})
		_ = g.AddNode(dag.Node{ID: "c1", Row: 1})
		_ = g.AddNode(dag.Node{ID: "c2", Row: 1})
		_ = g.AddNode(dag.Node{ID: "c3", Row: 1})

		_ = g.AddEdge(dag.Edge{From: "p1", To: "c1"})
		_ = g.AddEdge(dag.Edge{From: "p1", To: "c2"})
		_ = g.AddEdge(dag.Edge{From: "p2", To: "c2"})
		_ = g.AddEdge(dag.Edge{From: "p2", To: "c3"})
		return g
	}

	g1 := buildGraph()
	ResolveSpanOverlaps(g1)

	g2 := buildGraph()
	ResolveSpanOverlaps(g2)

	if g1.NodeCount() != g2.NodeCount() {
		t.Errorf("expected deterministic results, got %d and %d nodes", g1.NodeCount(), g2.NodeCount())
	}

	seps1 := collectSeparatorIDs(g1)
	seps2 := collectSeparatorIDs(g2)

	if len(seps1) != len(seps2) {
		t.Errorf("expected same separator count, got %d and %d", len(seps1), len(seps2))
	}

	for i := range seps1 {
		if seps1[i] != seps2[i] {
			t.Errorf("separator IDs differ at index %d: %s vs %s", i, seps1[i], seps2[i])
		}
	}
}

func TestResolveSpanOverlaps_PreservesOriginalNodes(t *testing.T) {
	meta := dag.Metadata{"version": "1.0"}
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "p1", Row: 0, Meta: meta})
	_ = g.AddNode(dag.Node{ID: "p2", Row: 0})
	_ = g.AddNode(dag.Node{ID: "c1", Row: 1})
	_ = g.AddNode(dag.Node{ID: "c2", Row: 1})

	_ = g.AddEdge(dag.Edge{From: "p1", To: "c1"})
	_ = g.AddEdge(dag.Edge{From: "p1", To: "c2"})
	_ = g.AddEdge(dag.Edge{From: "p2", To: "c1"})
	_ = g.AddEdge(dag.Edge{From: "p2", To: "c2"})

	ResolveSpanOverlaps(g)

	p1, ok := g.Node("p1")
	if !ok {
		t.Fatal("original node p1 not found")
	}
	if p1.Meta["version"] != "1.0" {
		t.Error("original node metadata should be preserved")
	}

	c1, ok := g.Node("c1")
	if !ok {
		t.Fatal("original node c1 not found")
	}

	// After separator insertion, child nodes are shifted down by 1
	// (separator takes an intermediate row between parents and children)
	if c1.Row != 2 {
		t.Errorf("child node should be at row 2 after separator insertion, got %d", c1.Row)
	}

	// Verify parent row is unchanged
	if p1.Row != 0 {
		t.Errorf("parent node row should remain 0, got %d", p1.Row)
	}
}

func TestResolveSpanOverlaps_ComplexOverlap(t *testing.T) {
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "p1", Row: 0})
	_ = g.AddNode(dag.Node{ID: "p2", Row: 0})
	_ = g.AddNode(dag.Node{ID: "p3", Row: 0})
	_ = g.AddNode(dag.Node{ID: "c1", Row: 1})
	_ = g.AddNode(dag.Node{ID: "c2", Row: 1})
	_ = g.AddNode(dag.Node{ID: "c3", Row: 1})
	_ = g.AddNode(dag.Node{ID: "c4", Row: 1})
	_ = g.AddNode(dag.Node{ID: "c5", Row: 1})

	_ = g.AddEdge(dag.Edge{From: "p1", To: "c1"})
	_ = g.AddEdge(dag.Edge{From: "p1", To: "c3"})
	_ = g.AddEdge(dag.Edge{From: "p2", To: "c2"})
	_ = g.AddEdge(dag.Edge{From: "p2", To: "c4"})
	_ = g.AddEdge(dag.Edge{From: "p3", To: "c1"})
	_ = g.AddEdge(dag.Edge{From: "p3", To: "c5"})

	ResolveSpanOverlaps(g)

	separatorCount := 0
	for _, n := range g.Nodes() {
		if n.IsAuxiliary() {
			separatorCount++
		}
	}

	if separatorCount == 0 {
		t.Error("expected separators for complex overlapping spans")
	}
}

func TestResolveSpanOverlaps_EmptyGraph(t *testing.T) {
	g := dag.New(nil)
	ResolveSpanOverlaps(g)

	if g.NodeCount() != 0 {
		t.Errorf("expected empty graph to remain empty, got %d nodes", g.NodeCount())
	}
}

func TestResolveSpanOverlaps_SingleNode(t *testing.T) {
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "a", Row: 0})

	nodesBefore := g.NodeCount()
	ResolveSpanOverlaps(g)

	if g.NodeCount() != nodesBefore {
		t.Errorf("expected no changes for single node, got %d nodes (was %d)", g.NodeCount(), nodesBefore)
	}
}

func collectSeparatorIDs(g *dag.DAG) []string {
	var ids []string
	for _, n := range g.Nodes() {
		if n.IsAuxiliary() {
			ids = append(ids, n.ID)
		}
	}
	return ids
}
