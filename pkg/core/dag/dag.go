package dag

import (
	"errors"
	"maps"
	"slices"
)

var (
	// ErrInvalidNodeID is returned by [DAG.AddNode] and [DAG.RenameNode] when
	// the node ID is empty. All nodes must have non-empty identifiers.
	ErrInvalidNodeID = errors.New("node ID must not be empty")

	// ErrDuplicateNodeID is returned by [DAG.AddNode] and [DAG.RenameNode] when
	// a node with the same ID already exists in the graph. Node IDs must be unique.
	ErrDuplicateNodeID = errors.New("duplicate node ID")

	// ErrUnknownSourceNode is returned by [DAG.AddEdge] when the From node
	// does not exist, or by [DAG.RenameNode] when the old ID is not found.
	ErrUnknownSourceNode = errors.New("unknown source node")

	// ErrUnknownTargetNode is returned by [DAG.AddEdge] when the To node
	// does not exist in the graph.
	ErrUnknownTargetNode = errors.New("unknown target node")

	// ErrInvalidEdgeEndpoint is returned by [DAG.Validate] when an edge
	// references a node that doesn't exist. This indicates graph corruption.
	ErrInvalidEdgeEndpoint = errors.New("invalid edge endpoint")

	// ErrNonConsecutiveRows is returned by [DAG.Validate] when an edge
	// connects nodes that are not in adjacent rows (From.Row+1 != To.Row).
	// All edges must connect consecutive rows for layered layouts.
	ErrNonConsecutiveRows = errors.New("edges must connect consecutive rows")

	// ErrGraphHasCycle is returned by [DAG.Validate] when a cycle is detected.
	// This indicates the graph is not a valid DAG. Cycles are detected using
	// depth-first search with white/gray/black coloring.
	ErrGraphHasCycle = errors.New("graph contains a cycle")
)

// Metadata stores arbitrary key-value pairs attached to nodes or the graph.
// It is commonly used to store package metadata (version, description, repo URL)
// or rendering options (style, seed). Metadata maps are never nil - they are
// automatically initialized to empty maps when needed.
type Metadata map[string]any

// NodeKind distinguishes between original and synthetic nodes created during
// graph transformation.
type NodeKind int

const (
	// NodeKindRegular represents an original graph node from dependency data.
	NodeKindRegular NodeKind = iota
	// NodeKindSubdivider represents a synthetic node inserted to subdivide a long edge.
	// Subdividers maintain a MasterID linking to their origin node.
	NodeKindSubdivider
	// NodeKindAuxiliary represents a helper node for layout (e.g., separator beams).
	// Auxiliary nodes resolve impossible crossing patterns by providing intermediate points.
	NodeKindAuxiliary
)

// Node represents a vertex in the dependency graph with an assigned row (layer).
// Nodes can be original vertices from dependency data (NodeKindRegular) or synthetic
// nodes created during transformation (NodeKindSubdivider, NodeKindAuxiliary).
//
// The zero value is not usable - ID and Row must be set before adding to a DAG.
type Node struct {
	ID   string   // Unique identifier (also used as display label)
	Row  int      // Layer assignment (0 = root/top, increasing downward)
	Meta Metadata // Arbitrary key-value metadata (never nil after AddNode)

	// Kind indicates whether this is an original or synthetic node.
	Kind NodeKind
	// MasterID links subdivider chains back to their origin node.
	// For subdividers, EffectiveID() returns MasterID instead of ID.
	MasterID string
}

// IsSubdivider reports whether the node was inserted to break a long edge.
// Subdivider nodes are synthetic and maintain a MasterID linking to their origin.
func (n Node) IsSubdivider() bool { return n.Kind == NodeKindSubdivider }

// IsAuxiliary reports whether the node is a helper for layout (e.g., separator beam).
// Auxiliary nodes are synthetic and resolve impossible crossing patterns.
func (n Node) IsAuxiliary() bool { return n.Kind == NodeKindAuxiliary }

// IsSynthetic reports whether the node was created during graph transformation
// (subdivider or auxiliary), as opposed to an original graph vertex.
func (n Node) IsSynthetic() bool { return n.Kind != NodeKindRegular }

// EffectiveID returns MasterID if set (for subdividers), otherwise the node's ID.
// This allows subdivider chains to be treated as a single logical entity during
// rendering, where they appear as continuous vertical blocks.
func (n Node) EffectiveID() string {
	if n.MasterID != "" {
		return n.MasterID
	}
	return n.ID
}

// Edge represents a directed connection between two nodes in consecutive rows.
// For a valid edge, the target must be exactly one row below the source:
// dst.Row == src.Row + 1. This constraint is enforced by Validate.
type Edge struct {
	From string   // Source node ID
	To   string   // Target node ID
	Meta Metadata // Arbitrary key-value metadata (never nil after AddEdge)
}

// DAG is a directed acyclic graph optimized for row-based layered layouts.
// Nodes are organized into horizontal rows (layers), and edges can only connect
// nodes in consecutive rows. This structure enables efficient crossing reduction
// algorithms for tower visualizations.
//
// The zero value is not usable - use New to create a valid DAG instance.
// DAG is not safe for concurrent use without external synchronization.
type DAG struct {
	nodes    map[string]*Node
	edges    []Edge
	outgoing map[string][]string // nodeID -> children IDs
	incoming map[string][]string // nodeID -> parent IDs
	rows     map[int][]*Node     // row -> nodes in that row
	meta     Metadata
}

// New creates an empty DAG with optional graph-level metadata.
// The metadata parameter can be nil, in which case an empty map is created.
// Graph-level metadata is typically used to store rendering options.
func New(meta Metadata) *DAG {
	if meta == nil {
		meta = Metadata{}
	}
	return &DAG{
		nodes:    make(map[string]*Node),
		outgoing: make(map[string][]string),
		incoming: make(map[string][]string),
		rows:     make(map[int][]*Node),
		meta:     meta,
	}
}

// Meta returns the graph-level metadata map.
// The returned map is never nil and can be safely modified.
func (d *DAG) Meta() Metadata { return d.meta }

// AddNode adds a node to the graph and automatically indexes it by its Row.
// Returns ErrInvalidNodeID if the node ID is empty, or ErrDuplicateNodeID
// if a node with the same ID already exists. The node's Meta field is
// automatically initialized to an empty map if nil.
//
// Node IDs must be unique across the entire graph, regardless of row assignment.
func (d *DAG) AddNode(n Node) error {
	if n.ID == "" {
		return ErrInvalidNodeID
	}
	if _, exists := d.nodes[n.ID]; exists {
		return ErrDuplicateNodeID
	}
	if n.Meta == nil {
		n.Meta = Metadata{}
	}
	node := &n
	d.nodes[node.ID] = node
	d.rows[node.Row] = append(d.rows[node.Row], node)
	return nil
}

// SetRows updates the row assignments for nodes and rebuilds the row index.
// Nodes not present in the rows map retain their current row assignment.
// This is typically used after layer assignment algorithms compute optimal depths.
//
// The row index (used by NodesInRow) is completely rebuilt, so this operation
// is O(N) where N is the total number of nodes.
func (d *DAG) SetRows(rows map[string]int) {
	d.rows = make(map[int][]*Node)
	for _, n := range d.nodes {
		if newRow, ok := rows[n.ID]; ok {
			n.Row = newRow
		}
		d.rows[n.Row] = append(d.rows[n.Row], n)
	}
}

// AddEdge adds a directed edge between two existing nodes.
// Returns ErrUnknownSourceNode if the From node doesn't exist, or
// ErrUnknownTargetNode if the To node doesn't exist. The edge's Meta
// field is automatically initialized to an empty map if nil.
//
// AddEdge does not validate that From.Row+1 == To.Row - use Validate
// to check this constraint after building the graph. Multiple edges
// between the same nodes are allowed (though unusual in dependency graphs).
func (d *DAG) AddEdge(e Edge) error {
	if _, ok := d.nodes[e.From]; !ok {
		return ErrUnknownSourceNode
	}
	if _, ok := d.nodes[e.To]; !ok {
		return ErrUnknownTargetNode
	}
	if e.Meta == nil {
		e.Meta = Metadata{}
	}
	d.edges = append(d.edges, e)
	d.outgoing[e.From] = append(d.outgoing[e.From], e.To)
	d.incoming[e.To] = append(d.incoming[e.To], e.From)
	return nil
}

// RemoveEdge removes the edge from→to if it exists.
// No error is returned if the edge does not exist. If multiple edges
// exist between the same nodes, only the first is removed.
func (d *DAG) RemoveEdge(from, to string) {
	d.edges = slices.DeleteFunc(d.edges, func(e Edge) bool { return e.From == from && e.To == to })
	d.outgoing[from] = slices.DeleteFunc(d.outgoing[from], func(s string) bool { return s == to })
	d.incoming[to] = slices.DeleteFunc(d.incoming[to], func(s string) bool { return s == from })
}

// RenameNode changes a node's ID, updating all edges and indices.
// Returns ErrInvalidNodeID if newID is empty, ErrUnknownSourceNode if
// oldID doesn't exist, or ErrDuplicateNodeID if newID is already in use.
//
// This is an O(N+E) operation where N is the number of nodes and E is
// the number of edges, as all adjacency lists must be updated.
func (d *DAG) RenameNode(oldID, newID string) error {
	if newID == "" {
		return ErrInvalidNodeID
	}
	node, ok := d.nodes[oldID]
	if !ok {
		return ErrUnknownSourceNode
	}
	if _, exists := d.nodes[newID]; exists {
		return ErrDuplicateNodeID
	}

	node.ID = newID
	delete(d.nodes, oldID)
	d.nodes[newID] = node

	for i := range d.edges {
		if d.edges[i].From == oldID {
			d.edges[i].From = newID
		}
		if d.edges[i].To == oldID {
			d.edges[i].To = newID
		}
	}

	d.outgoing[newID] = d.outgoing[oldID]
	delete(d.outgoing, oldID)
	for id, targets := range d.outgoing {
		for i, t := range targets {
			if t == oldID {
				d.outgoing[id][i] = newID
			}
		}
	}

	d.incoming[newID] = d.incoming[oldID]
	delete(d.incoming, oldID)
	for id, sources := range d.incoming {
		for i, s := range sources {
			if s == oldID {
				d.incoming[id][i] = newID
			}
		}
	}

	return nil
}

// Nodes returns all nodes in the graph.
// The order is not guaranteed. The returned slice contains pointers to
// the actual node structs, so modifications affect the graph.
func (d *DAG) Nodes() []*Node {
	nodes := make([]*Node, 0, len(d.nodes))
	for _, n := range d.nodes {
		nodes = append(nodes, n)
	}
	return nodes
}

// Edges returns a copy of all edges in the graph.
// The order matches insertion order. Modifications to the returned
// slice or its edge structs do not affect the graph.
func (d *DAG) Edges() []Edge { return slices.Clone(d.edges) }

// EdgesIter returns the edges slice for read-only iteration without copying.
// The returned slice must not be modified. Use Edges() if you need a mutable copy.
func (d *DAG) EdgesIter() []Edge { return d.edges }

// NodeCount returns the number of nodes in the graph.
func (d *DAG) NodeCount() int { return len(d.nodes) }

// EdgeCount returns the number of edges in the graph.
func (d *DAG) EdgeCount() int { return len(d.edges) }

// Children returns the IDs of nodes that this node has edges to (dependencies).
// Returns nil if the node has no children or doesn't exist. The returned slice
// should not be modified - use it as a read-only view.
func (d *DAG) Children(id string) []string { return d.outgoing[id] }

// Parents returns the IDs of nodes that have edges to this node (dependents).
// Returns nil if the node has no parents or doesn't exist. The returned slice
// should not be modified - use it as a read-only view.
func (d *DAG) Parents(id string) []string { return d.incoming[id] }

// OutDegree returns the number of outgoing edges from the node.
// Returns 0 if the node doesn't exist.
func (d *DAG) OutDegree(id string) int { return len(d.outgoing[id]) }

// InDegree returns the number of incoming edges to the node.
// Returns 0 if the node doesn't exist.
func (d *DAG) InDegree(id string) int { return len(d.incoming[id]) }

// Node returns the node with the given ID and true, or nil and false if not found.
// The returned node pointer refers to the actual node in the graph, so modifications
// affect the graph (except for ID changes - use RenameNode instead).
func (d *DAG) Node(id string) (*Node, bool) {
	n, ok := d.nodes[id]
	return n, ok
}

// ChildrenInRow returns children of the node that are in the specified row.
// This is useful for row-by-row traversals in layered layouts. Returns nil
// if the node has no children in that row or doesn't exist.
func (d *DAG) ChildrenInRow(id string, row int) []string {
	var result []string
	for _, c := range d.outgoing[id] {
		if n, ok := d.nodes[c]; ok && n.Row == row {
			result = append(result, c)
		}
	}
	return result
}

// ParentsInRow returns parents of the node that are in the specified row.
// This is useful for row-by-row traversals in layered layouts. Returns nil
// if the node has no parents in that row or doesn't exist.
func (d *DAG) ParentsInRow(id string, row int) []string {
	var result []string
	for _, p := range d.incoming[id] {
		if n, ok := d.nodes[p]; ok && n.Row == row {
			result = append(result, p)
		}
	}
	return result
}

// NodesInRow returns all nodes assigned to the given row.
// Returns nil if the row is empty or doesn't exist. The returned slice
// contains pointers to the actual nodes, so modifications affect the graph.
// The order is insertion order (order in which AddNode or SetRows added them).
func (d *DAG) NodesInRow(row int) []*Node { return d.rows[row] }

// RowCount returns the number of distinct rows (layers) in the graph.
// Returns 0 for an empty graph. Rows don't need to be consecutive -
// a graph with nodes in rows 0 and 5 has RowCount() == 2.
func (d *DAG) RowCount() int { return len(d.rows) }

// RowIDs returns all row indices in sorted ascending order.
// Returns an empty slice for an empty graph. Use this to iterate
// through rows from top to bottom.
func (d *DAG) RowIDs() []int {
	return slices.Sorted(maps.Keys(d.rows))
}

// MaxRow returns the highest row index, or 0 if the graph is empty.
// For a non-empty graph, this is the bottom-most layer.
func (d *DAG) MaxRow() int {
	if len(d.rows) == 0 {
		return 0
	}
	rowIDs := d.RowIDs()
	return rowIDs[len(rowIDs)-1]
}

// Sources returns nodes with no incoming edges (roots/entry points).
// These are typically application entry points or top-level packages.
// The order is not guaranteed. Returns nil for an empty graph.
func (d *DAG) Sources() []*Node {
	var sources []*Node
	for _, n := range d.nodes {
		if len(d.incoming[n.ID]) == 0 {
			sources = append(sources, n)
		}
	}
	return sources
}

// Sinks returns nodes with no outgoing edges (leaves/terminals).
// These are typically low-level libraries with no dependencies.
// The order is not guaranteed. Returns nil for an empty graph.
func (d *DAG) Sinks() []*Node {
	var sinks []*Node
	for _, n := range d.nodes {
		if len(d.outgoing[n.ID]) == 0 {
			sinks = append(sinks, n)
		}
	}
	return sinks
}

// Validate checks graph integrity and returns nil if valid.
// It verifies two constraints:
//
//  1. All edges connect existing nodes in consecutive rows (From.Row+1 == To.Row)
//  2. The graph is acyclic (no directed cycles exist)
//
// Returns ErrInvalidEdgeEndpoint if an edge references a missing node,
// ErrNonConsecutiveRows if edges don't connect adjacent rows, or
// ErrGraphHasCycle if a cycle is detected. Use this before rendering
// or applying transformations that assume a valid DAG.
//
// Cycle detection runs in O(N+E) time using depth-first search.
func (d *DAG) Validate() error {
	if err := d.validateEdgeConsistency(); err != nil {
		return err
	}
	return d.detectCycles()
}

func (d *DAG) validateEdgeConsistency() error {
	for _, e := range d.edges {
		src, okS := d.nodes[e.From]
		dst, okD := d.nodes[e.To]
		if !okS || !okD {
			return ErrInvalidEdgeEndpoint
		}
		if dst.Row != src.Row+1 {
			return ErrNonConsecutiveRows
		}
	}
	return nil
}

func (d *DAG) detectCycles() error {
	const (
		white = iota
		gray
		black
	)

	color := make(map[string]int, len(d.nodes))
	var hasCycle bool

	var dfs func(id string)
	dfs = func(id string) {
		color[id] = gray
		for _, child := range d.outgoing[id] {
			switch color[child] {
			case white:
				dfs(child)
			case gray:
				hasCycle = true
				return
			}
		}
		color[id] = black
	}

	for id := range d.nodes {
		if color[id] == white {
			dfs(id)
			if hasCycle {
				return ErrGraphHasCycle
			}
		}
	}
	return nil
}

// PosMap creates a position lookup map from a slice of node IDs.
// The returned map maps each ID to its index in the slice.
// This is commonly used to convert node orderings into fast position lookups
// for crossing calculations. Returns an empty map for a nil or empty slice.
func PosMap(ids []string) map[string]int {
	m := make(map[string]int, len(ids))
	for i, id := range ids {
		m[id] = i
	}
	return m
}

// NodePosMap creates a position lookup map from a slice of nodes.
// The returned map maps each node ID to its index in the slice.
// This is a convenience wrapper around PosMap for node slices.
// Returns an empty map for a nil or empty slice.
func NodePosMap(nodes []*Node) map[string]int {
	m := make(map[string]int, len(nodes))
	for i, n := range nodes {
		m[n.ID] = i
	}
	return m
}

// NodeIDs extracts the ID from each node in a slice.
// Returns a new slice containing the IDs in the same order as the input.
// Returns an empty slice for a nil or empty input.
func NodeIDs(nodes []*Node) []string {
	ids := make([]string, len(nodes))
	for i, n := range nodes {
		ids[i] = n.ID
	}
	return ids
}

// Clone creates a deep copy of the DAG, including all nodes, edges, and metadata.
// The returned DAG is completely independent - modifications to it will not affect
// the original graph. This is useful when graph transformations (like normalization)
// need to be applied without mutating the original graph.
//
// Clone preserves:
//   - All nodes with their row assignments, metadata, kind, and master IDs
//   - All edges with their metadata
//   - Graph-level metadata
//   - Row index structure
//
// Time complexity: O(N + E) where N is node count and E is edge count.
func (d *DAG) Clone() *DAG {
	// Clone graph-level metadata
	meta := make(Metadata, len(d.meta))
	for k, v := range d.meta {
		meta[k] = v
	}

	clone := New(meta)

	// Clone all nodes with their metadata
	for _, n := range d.nodes {
		nodeMeta := make(Metadata, len(n.Meta))
		for k, v := range n.Meta {
			nodeMeta[k] = v
		}
		clone.AddNode(Node{
			ID:       n.ID,
			Row:      n.Row,
			Meta:     nodeMeta,
			Kind:     n.Kind,
			MasterID: n.MasterID,
		})
	}

	// Clone all edges with their metadata
	for _, e := range d.edges {
		edgeMeta := make(Metadata, len(e.Meta))
		for k, v := range e.Meta {
			edgeMeta[k] = v
		}
		clone.AddEdge(Edge{
			From: e.From,
			To:   e.To,
			Meta: edgeMeta,
		})
	}

	return clone
}
