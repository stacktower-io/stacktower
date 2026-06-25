package transform

import (
	"testing"

	"github.com/stacktower-io/stacktower/pkg/core/dag"
)

func TestSubdivide_NoEdges(t *testing.T) {
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "a", Row: 0})
	_ = g.AddNode(dag.Node{ID: "b", Row: 0})

	Subdivide(g)

	if g.EdgeCount() != 0 {
		t.Errorf("expected 0 edges (no connections), got %d", g.EdgeCount())
	}
}

func TestSubdivide_AllConsecutiveRows(t *testing.T) {
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "a", Row: 0})
	_ = g.AddNode(dag.Node{ID: "b", Row: 1})
	_ = g.AddNode(dag.Node{ID: "c", Row: 2})
	_ = g.AddNode(dag.Node{ID: "d", Row: 3})
	_ = g.AddEdge(dag.Edge{From: "a", To: "b"})
	_ = g.AddEdge(dag.Edge{From: "b", To: "c"})
	_ = g.AddEdge(dag.Edge{From: "c", To: "d"})

	Subdivide(g)

	if g.NodeCount() != 4 {
		t.Errorf("expected 4 nodes (no subdivision needed), got %d", g.NodeCount())
	}
	if g.EdgeCount() != 3 {
		t.Errorf("expected 3 edges, got %d", g.EdgeCount())
	}
}

func TestSubdivide_VeryLongEdge(t *testing.T) {
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "a", Row: 0})
	_ = g.AddNode(dag.Node{ID: "b", Row: 10})
	_ = g.AddEdge(dag.Edge{From: "a", To: "b"})

	Subdivide(g)

	expectedSubdividers := 9
	subdividerCount := 0
	for _, n := range g.Nodes() {
		if n.IsSubdivider() {
			subdividerCount++
		}
	}

	if subdividerCount != expectedSubdividers {
		t.Errorf("expected %d subdividers, got %d", expectedSubdividers, subdividerCount)
	}

	expectedEdges := 10
	if g.EdgeCount() != expectedEdges {
		t.Errorf("expected %d edges, got %d", expectedEdges, g.EdgeCount())
	}
}

// TestSubdivide_SharedChainPerSource verifies that multiple long edges from
// the same source share one subdivider column instead of one chain per edge
// (the "duplicate pydantic blocks" rendering bug).
func TestSubdivide_SharedChainPerSource(t *testing.T) {
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "pyd", Row: 1})
	_ = g.AddNode(dag.Node{ID: "x", Row: 3})
	_ = g.AddNode(dag.Node{ID: "y", Row: 3})
	_ = g.AddNode(dag.Node{ID: "z", Row: 4})
	// Sinks at the bottom row so sink extension doesn't add chains for x/y.
	_ = g.AddEdge(dag.Edge{From: "pyd", To: "x"}) // spans row 2
	_ = g.AddEdge(dag.Edge{From: "pyd", To: "y"}) // spans row 2
	_ = g.AddEdge(dag.Edge{From: "pyd", To: "z"}) // spans rows 2-3
	_ = g.AddEdge(dag.Edge{From: "x", To: "z"})
	_ = g.AddEdge(dag.Edge{From: "y", To: "z"})

	Subdivide(g)

	perRow := make(map[int][]string)
	for _, n := range g.Nodes() {
		if n.IsSubdivider() {
			if n.MasterID != "pyd" {
				t.Errorf("subdivider %s: MasterID = %q, want %q", n.ID, n.MasterID, "pyd")
			}
			perRow[n.Row] = append(perRow[n.Row], n.ID)
		}
	}

	// All three long edges pass row 2; they must share ONE subdivider there.
	if len(perRow[2]) != 1 {
		t.Fatalf("row 2: want 1 shared subdivider, got %d (%v)", len(perRow[2]), perRow[2])
	}
	// Only pyd→z continues through row 3.
	if len(perRow[3]) != 1 {
		t.Fatalf("row 3: want 1 subdivider, got %d (%v)", len(perRow[3]), perRow[3])
	}

	sub2, sub3 := perRow[2][0], perRow[3][0]
	// The shared segment diverges to x, y, and the row-3 segment.
	gotChildren := g.Children(sub2)
	if len(gotChildren) != 3 {
		t.Errorf("shared subdivider children = %v, want x, y and %s", gotChildren, sub3)
	}
	if kids := g.Children(sub3); len(kids) != 1 || kids[0] != "z" {
		t.Errorf("row-3 segment children = %v, want [z]", kids)
	}
}

func TestSubdivide_MixedEdgeLengths(t *testing.T) {
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "a", Row: 0})
	_ = g.AddNode(dag.Node{ID: "b", Row: 1})
	_ = g.AddNode(dag.Node{ID: "c", Row: 5})
	_ = g.AddEdge(dag.Edge{From: "a", To: "b"})
	_ = g.AddEdge(dag.Edge{From: "b", To: "c"})

	edgesBefore := g.EdgeCount()
	Subdivide(g)

	if g.EdgeCount() <= edgesBefore {
		t.Errorf("expected more edges after subdivision, had %d, got %d", edgesBefore, g.EdgeCount())
	}
}

func TestSubdivide_SinkExtension_SingleSink(t *testing.T) {
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "a", Row: 0})
	_ = g.AddNode(dag.Node{ID: "b", Row: 1})
	_ = g.AddNode(dag.Node{ID: "c", Row: 5})
	_ = g.AddEdge(dag.Edge{From: "a", To: "b"})
	_ = g.AddEdge(dag.Edge{From: "a", To: "c"})

	Subdivide(g)

	maxRow := 0
	for _, n := range g.Nodes() {
		if n.Row > maxRow {
			maxRow = n.Row
		}
	}

	sinkNode, ok := g.Node("b")
	if !ok {
		t.Fatal("sink node b not found")
	}

	descendants := findDescendants(g, "b")
	hasDescendantAtMaxRow := false
	for _, desc := range descendants {
		if desc.Row == maxRow {
			hasDescendantAtMaxRow = true
			break
		}
	}

	if !hasDescendantAtMaxRow {
		t.Errorf("sink node b (row %d) should extend to max row %d", sinkNode.Row, maxRow)
	}
}

func TestSubdivide_SinkExtension_MultipleSinksAtDifferentLevels(t *testing.T) {
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "a", Row: 0})
	_ = g.AddNode(dag.Node{ID: "b", Row: 2})
	_ = g.AddNode(dag.Node{ID: "c", Row: 5})
	_ = g.AddEdge(dag.Edge{From: "a", To: "b"})
	_ = g.AddEdge(dag.Edge{From: "a", To: "c"})

	Subdivide(g)

	maxRow := 0
	for _, n := range g.Nodes() {
		if n.Row > maxRow {
			maxRow = n.Row
		}
	}

	sinkCount := 0
	for _, n := range g.Nodes() {
		if g.OutDegree(n.ID) == 0 {
			sinkCount++
			if n.Row != maxRow {
				t.Errorf("sink %s at row %d should be at max row %d", n.ID, n.Row, maxRow)
			}
		}
	}

	if sinkCount < 2 {
		t.Errorf("expected at least 2 sinks, got %d", sinkCount)
	}
}

func TestSubdivide_PreservesOriginalNodeProperties(t *testing.T) {
	meta := dag.Metadata{"version": "1.0"}
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "a", Row: 0, Meta: meta})
	_ = g.AddNode(dag.Node{ID: "b", Row: 3})
	_ = g.AddEdge(dag.Edge{From: "a", To: "b"})

	Subdivide(g)

	nodeA, ok := g.Node("a")
	if !ok {
		t.Fatal("original node a not found")
	}
	if nodeA.Meta["version"] != "1.0" {
		t.Error("original node metadata should be preserved")
	}
}

func TestSubdivide_SubdividerNaming(t *testing.T) {
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "parent", Row: 0})
	_ = g.AddNode(dag.Node{ID: "child", Row: 3})
	_ = g.AddEdge(dag.Edge{From: "parent", To: "child"})

	Subdivide(g)

	subdividers := make([]*dag.Node, 0)
	for _, n := range g.Nodes() {
		if n.IsSubdivider() {
			subdividers = append(subdividers, n)
		}
	}

	if len(subdividers) != 2 {
		t.Fatalf("expected 2 subdividers, got %d", len(subdividers))
	}

	for _, sub := range subdividers {
		if sub.MasterID != "parent" {
			t.Errorf("subdivider %s should have MasterID 'parent', got '%s'", sub.ID, sub.MasterID)
		}
	}
}

func TestSubdivide_ResubdivisionKeepsOriginalMaster(t *testing.T) {
	// Simulates the post-separator-insertion state: an existing subdivider
	// whose outgoing edge spans multiple rows (separator insertion shifted
	// the rows below it). Re-subdividing must link the new segment to the
	// ORIGINAL master, not the intermediate subdivider, or MergeSubdividers
	// can never reassemble the column.
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "a", Row: 0})
	_ = g.AddNode(dag.Node{ID: "a_sub_1", Row: 1, Kind: dag.NodeKindSubdivider, MasterID: "a"})
	_ = g.AddNode(dag.Node{ID: "w", Row: 3})
	_ = g.AddEdge(dag.Edge{From: "a", To: "a_sub_1"})
	_ = g.AddEdge(dag.Edge{From: "a_sub_1", To: "w"}) // spans rows 1 -> 3

	Subdivide(g)

	for _, n := range g.Nodes() {
		if !n.IsSubdivider() {
			continue
		}
		if got := n.EffectiveID(); got != "a" {
			t.Errorf("subdivider %s: EffectiveID = %q, want %q (column would split on merge)", n.ID, got, "a")
		}
	}
}

func TestNormalize_SeparatorMidChainKeepsMaster(t *testing.T) {
	// End-to-end: a K2,2 tangle (p,q -> x,y) forces a separator beam in the
	// middle of the long-edge chain a -> w. The re-subdivision after
	// separator insertion must keep the whole chain under master "a".
	g := dag.New(nil)
	for _, id := range []string{"a", "b", "p", "q", "x", "y", "c", "d", "w"} {
		_ = g.AddNode(dag.Node{ID: id, Row: 0})
	}
	_ = g.AddEdge(dag.Edge{From: "a", To: "p"})
	_ = g.AddEdge(dag.Edge{From: "a", To: "q"})
	_ = g.AddEdge(dag.Edge{From: "p", To: "x"})
	_ = g.AddEdge(dag.Edge{From: "p", To: "y"})
	_ = g.AddEdge(dag.Edge{From: "q", To: "x"})
	_ = g.AddEdge(dag.Edge{From: "q", To: "y"})
	_ = g.AddEdge(dag.Edge{From: "b", To: "c"})
	_ = g.AddEdge(dag.Edge{From: "c", To: "d"})
	_ = g.AddEdge(dag.Edge{From: "d", To: "w"})
	_ = g.AddEdge(dag.Edge{From: "a", To: "w"}) // long edge through the tangle

	res, err := Normalize(g)
	if err != nil {
		t.Fatal(err)
	}
	if res.SeparatorsAdded == 0 {
		t.Fatal("expected a separator to be inserted for the K2,2 tangle")
	}

	for _, n := range g.Nodes() {
		if !n.IsSubdivider() {
			continue
		}
		if master, ok := g.Node(n.MasterID); !ok {
			t.Errorf("subdivider %s: MasterID %q does not exist", n.ID, n.MasterID)
		} else if master.IsSubdivider() {
			t.Errorf("subdivider %s: MasterID %q is itself a subdivider; chain segments would never merge", n.ID, n.MasterID)
		}
	}
}

func TestSubdivide_IDGenerator_HandlesCollisions(t *testing.T) {
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "a", Row: 0})
	_ = g.AddNode(dag.Node{ID: "a_sub_1", Row: 1})
	_ = g.AddNode(dag.Node{ID: "b", Row: 3})
	_ = g.AddEdge(dag.Edge{From: "a", To: "b"})

	Subdivide(g)

	ids := make(map[string]bool)
	for _, n := range g.Nodes() {
		if ids[n.ID] {
			t.Errorf("duplicate ID found: %s", n.ID)
		}
		ids[n.ID] = true
	}
}

func TestSubdivide_ComplexGraph(t *testing.T) {
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "a", Row: 0})
	_ = g.AddNode(dag.Node{ID: "b", Row: 0})
	_ = g.AddNode(dag.Node{ID: "c", Row: 2})
	_ = g.AddNode(dag.Node{ID: "d", Row: 5})
	_ = g.AddNode(dag.Node{ID: "e", Row: 3})

	_ = g.AddEdge(dag.Edge{From: "a", To: "c"})
	_ = g.AddEdge(dag.Edge{From: "b", To: "d"})
	_ = g.AddEdge(dag.Edge{From: "c", To: "e"})

	nodesBefore := g.NodeCount()
	Subdivide(g)
	nodesAfter := g.NodeCount()

	if nodesAfter <= nodesBefore {
		t.Errorf("expected more nodes after subdivision, had %d, got %d", nodesBefore, nodesAfter)
	}

	maxRow := 0
	for _, n := range g.Nodes() {
		if n.Row > maxRow {
			maxRow = n.Row
		}
	}

	allSinksAtMaxRow := true
	for _, n := range g.Nodes() {
		if g.OutDegree(n.ID) == 0 && n.Row != maxRow {
			allSinksAtMaxRow = false
			break
		}
	}

	if !allSinksAtMaxRow {
		t.Error("all sinks should be extended to max row")
	}
}

func TestSubdivide_MaintainsConnectivity(t *testing.T) {
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "a", Row: 0})
	_ = g.AddNode(dag.Node{ID: "b", Row: 5})
	_ = g.AddEdge(dag.Edge{From: "a", To: "b"})

	Subdivide(g)

	path := findPath(g, "a", "b")
	if len(path) == 0 {
		t.Error("should maintain path from a to b after subdivision")
	}
}

func findDescendants(g *dag.DAG, nodeID string) []*dag.Node {
	descendants := make([]*dag.Node, 0)
	visited := make(map[string]bool)
	queue := []string{nodeID}

	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]

		if visited[curr] {
			continue
		}
		visited[curr] = true

		children := g.Children(curr)
		for _, child := range children {
			if n, ok := g.Node(child); ok {
				descendants = append(descendants, n)
				queue = append(queue, child)
			}
		}
	}

	return descendants
}

func findPath(g *dag.DAG, from, to string) []string {
	if from == to {
		return []string{from}
	}

	visited := make(map[string]bool)
	parent := make(map[string]string)
	queue := []string{from}
	visited[from] = true

	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]

		if curr == to {
			path := make([]string, 0)
			for node := to; node != ""; node = parent[node] {
				path = append([]string{node}, path...)
				if node == from {
					break
				}
			}
			return path
		}

		for _, child := range g.Children(curr) {
			if !visited[child] {
				visited[child] = true
				parent[child] = curr
				queue = append(queue, child)
			}
		}
	}

	return nil
}
