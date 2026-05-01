package transform

import (
	"fmt"

	"github.com/stacktower-io/stacktower/pkg/core/dag"
)

// Subdivide breaks edges that span multiple rows into sequences of single-row
// edges connected by synthetic subdivider nodes.
//
// Subdivide ensures every edge in the graph connects nodes in consecutive rows
// (parent.Row + 1 == child.Row). Any edge spanning multiple rows is replaced
// by a chain of [dag.NodeKindSubdivider] nodes. For example:
//
//	Before: app (row 0) → core (row 3)  [spans 3 rows]
//	After:  app → app_sub_1 → app_sub_2 → core  [3 single-row edges]
//
// Each subdivider maintains a MasterID field linking back to the original
// source node, allowing renderers to visually merge subdividers into
// continuous vertical blocks.
//
// # Sink Extension
//
// Subdivide also extends all sink nodes (nodes with out-degree 0) to the
// bottom row of the graph by appending subdivider chains. This ensures tower
// layouts have a flat foundation where all columns reach the bottom.
//
// # Node IDs
//
// Subdivider nodes are assigned unique IDs of the form "master_sub_row" (e.g.,
// "app_sub_1"). If a collision occurs, a numeric suffix is appended
// ("app_sub_1__2"). All generated IDs are tracked to guarantee uniqueness.
//
// # Edge Metadata
//
// Subdivide preserves edge metadata only on the final edge in each subdivided
// chain (the edge entering the original target). Intermediate subdivider edges
// have no metadata.
//
// # Nil Handling
//
// Subdivide panics if g is nil. If g is empty (zero nodes), the function
// returns immediately.
//
// # Performance
//
// Time complexity is O(V·D) where V is nodes and D is the maximum depth (row
// count), as each node may spawn subdividers equal to the depth. Space
// complexity is O(V) for tracking used IDs.
func Subdivide(g *dag.DAG) {
	_ = subdivide(g)
}

func subdivide(g *dag.DAG) error {
	gen := newIDGen(g.Nodes())
	if err := subdivideLongEdges(g, gen); err != nil {
		return err
	}
	if err := extendSinksToBottom(g, gen); err != nil {
		return err
	}
	return nil
}

func subdivideLongEdges(g *dag.DAG, gen *idGen) error {
	var toRemove []dag.Edge
	for _, e := range g.Edges() {
		src, srcOK := g.Node(e.From)
		dst, dstOK := g.Node(e.To)
		if !srcOK || !dstOK || dst.Row <= src.Row+1 {
			continue
		}

		toRemove = append(toRemove, e)
		prevID := src.ID
		for row := src.Row + 1; row < dst.Row; row++ {
			nextID, err := addSubdivider(g, gen, prevID, src.ID, row)
			if err != nil {
				return err
			}
			prevID = nextID
		}
		if err := g.AddEdge(dag.Edge{From: prevID, To: dst.ID, Meta: e.Meta}); err != nil {
			return fmt.Errorf("add subdivided edge %q -> %q: %w", prevID, dst.ID, err)
		}
	}

	for _, e := range toRemove {
		g.RemoveEdge(e.From, e.To)
	}
	return nil
}

func addSubdivider(g *dag.DAG, gen *idGen, from, master string, row int) (string, error) {
	id := gen.next(master, row)
	if err := g.AddNode(dag.Node{
		ID:       id,
		Row:      row,
		Kind:     dag.NodeKindSubdivider,
		MasterID: master,
	}); err != nil {
		return "", fmt.Errorf("add subdivider node %q: %w", id, err)
	}
	if err := g.AddEdge(dag.Edge{From: from, To: id}); err != nil {
		return "", fmt.Errorf("add subdivider edge %q -> %q: %w", from, id, err)
	}
	return id, nil
}

func extendSinksToBottom(g *dag.DAG, gen *idGen) error {
	maxRow := g.MaxRow()
	for _, n := range g.Nodes() {
		if g.OutDegree(n.ID) > 0 || n.Row >= maxRow {
			continue
		}
		prevID := n.ID
		for row := n.Row + 1; row <= maxRow; row++ {
			nextID, err := addSubdivider(g, gen, prevID, n.EffectiveID(), row)
			if err != nil {
				return err
			}
			prevID = nextID
		}
	}
	return nil
}

type idGen struct {
	used map[string]struct{}
}

func newIDGen(nodes []*dag.Node) *idGen {
	m := make(map[string]struct{}, len(nodes)*2)
	for _, n := range nodes {
		m[n.ID] = struct{}{}
	}
	return &idGen{used: m}
}

func (gen *idGen) next(base string, row int) string {
	prefix := fmt.Sprintf("%s_sub_%d", base, row)
	id := prefix
	for i := 1; ; i++ {
		if _, exists := gen.used[id]; !exists {
			gen.used[id] = struct{}{}
			return id
		}
		id = fmt.Sprintf("%s__%d", prefix, i)
	}
}
