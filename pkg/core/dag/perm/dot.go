package perm

import (
	"bytes"
	"context"
	"fmt"
	"sync"

	"github.com/goccy/go-graphviz"
)

// graphvizMu serializes access to the graphviz WASM runtime.
// The go-graphviz WASM backend is NOT thread-safe.
var graphvizMu sync.Mutex

// ToDOT returns a Graphviz DOT representation of the tree structure.
//
// The DOT format can be rendered with Graphviz tools (dot, neato, etc.) or
// programmatically with RenderSVG. The output is a complete DOT digraph with
// styling suitable for documentation and debugging.
//
// Node representation:
//   - P-nodes: labeled "P", ellipse shape
//   - Q-nodes: labeled "Q", box shape
//   - Leaf nodes: labeled with element value or label, rounded box shape
//
// The labels parameter works the same as in StringWithLabels: if labels[i]
// exists, element i is shown as labels[i], otherwise as a numeric index.
// Pass nil to use default numeric labels.
//
// The labels slice is not modified.
//
// Example:
//
//	tree := perm.NewPQTree(3)
//	tree.Reduce([]int{0, 1})
//	dot := tree.ToDOT([]string{"A", "B", "C"})
//	// Use 'dot' command or RenderSVG to visualize
func (t *PQTree) ToDOT(labels []string) string {
	var buf bytes.Buffer
	buf.WriteString("digraph PQTree {\n")
	buf.WriteString("  rankdir=TB;\n")
	buf.WriteString("  bgcolor=\"transparent\";\n")
	buf.WriteString("  node [fontname=\"SF Mono, Menlo, monospace\", fontsize=14, style=filled, fillcolor=white];\n")
	buf.WriteString("  edge [arrowhead=none];\n\n")

	if t.root != nil {
		t.writeDOTNode(&buf, t.root, 0, labels)
	}

	buf.WriteString("}\n")
	return buf.String()
}

func (t *PQTree) writeDOTNode(buf *bytes.Buffer, n *pqNode, id int, labels []string) int {
	nodeID := fmt.Sprintf("n%d", id)
	next := id + 1

	switch n.kind {
	case leafNode:
		label := t.nodeString(n, labels)
		fmt.Fprintf(buf, "  %s [label=%q, shape=box, style=\"filled,rounded\"];\n", nodeID, label)

	case pNode:
		fmt.Fprintf(buf, "  %s [label=\"P\", shape=ellipse];\n", nodeID)
		for _, c := range n.children {
			fmt.Fprintf(buf, "  %s -> n%d;\n", nodeID, next)
			next = t.writeDOTNode(buf, c, next, labels)
		}

	case qNode:
		fmt.Fprintf(buf, "  %s [label=\"Q\", shape=box];\n", nodeID)
		for _, c := range n.children {
			fmt.Fprintf(buf, "  %s -> n%d;\n", nodeID, next)
			next = t.writeDOTNode(buf, c, next, labels)
		}
	}

	return next
}

// RenderSVG renders the tree structure as an SVG image.
//
// RenderSVG generates a DOT representation via ToDOT, then uses Graphviz to
// render it to SVG format. The returned bytes are a complete SVG document
// suitable for embedding in HTML or saving to a file.
//
// The labels parameter is passed to ToDOT and works identically. Pass nil for
// default numeric labels.
//
// RenderSVG requires the Graphviz library (github.com/goccy/go-graphviz) and
// its C dependencies to be installed. Errors are returned if Graphviz cannot
// initialize, the DOT is malformed, or rendering fails.
//
// All errors are wrapped with context using fmt.Errorf with %w, suitable for
// unwrapping with errors.Unwrap or errors.Is.
//
// The labels slice is not modified.
//
// Example:
//
//	tree := perm.NewPQTree(4)
//	tree.Reduce([]int{1, 2})
//	svg, err := tree.RenderSVG([]string{"app", "auth", "cache", "db"})
//	if err != nil {
//		log.Fatal(err)
//	}
//	os.WriteFile("tree.svg", svg, 0644)
func (t *PQTree) RenderSVG(labels []string) ([]byte, error) {
	dot := t.ToDOT(labels)

	// Serialize all graphviz WASM calls to prevent memory corruption
	graphvizMu.Lock()
	defer graphvizMu.Unlock()

	gv, err := graphviz.New(context.Background())
	if err != nil {
		return nil, fmt.Errorf("init graphviz: %w", err)
	}
	defer gv.Close()

	g, err := graphviz.ParseBytes([]byte(dot))
	if err != nil {
		return nil, fmt.Errorf("parse DOT: %w", err)
	}
	defer g.Close()

	var buf bytes.Buffer
	if err := gv.Render(context.Background(), g, graphviz.SVG, &buf); err != nil {
		return nil, fmt.Errorf("render: %w", err)
	}
	return buf.Bytes(), nil
}
