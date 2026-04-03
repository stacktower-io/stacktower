package transform_test

import (
	"fmt"

	"github.com/matzehuels/stacktower/pkg/core/dag"
	"github.com/matzehuels/stacktower/pkg/core/dag/transform"
)

func ExampleNormalize() {
	// Build a raw dependency graph (not yet normalized)
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "app"})
	_ = g.AddNode(dag.Node{ID: "auth"})
	_ = g.AddNode(dag.Node{ID: "cache"})
	_ = g.AddNode(dag.Node{ID: "db"})

	// Dependencies: app → auth → db, app → cache → db, app → db (transitive)
	_ = g.AddEdge(dag.Edge{From: "app", To: "auth"})
	_ = g.AddEdge(dag.Edge{From: "app", To: "cache"})
	_ = g.AddEdge(dag.Edge{From: "app", To: "db"}) // Transitive - will be removed
	_ = g.AddEdge(dag.Edge{From: "auth", To: "db"})
	_ = g.AddEdge(dag.Edge{From: "cache", To: "db"})

	fmt.Println("Before normalize:")
	fmt.Println("  Nodes:", g.NodeCount())
	fmt.Println("  Edges:", g.EdgeCount())

	// Normalize: assigns layers, removes transitive edges, subdivides long edges
	result, err := transform.Normalize(g)
	if err != nil {
		fmt.Println("error:", err)
		return
	}

	fmt.Println("After normalize:")
	fmt.Println("  Nodes:", g.NodeCount())
	fmt.Println("  Edges:", g.EdgeCount())
	fmt.Println("  Rows:", g.RowCount())
	fmt.Println("Transformation metrics:")
	fmt.Println("  Cycles removed:", result.CyclesRemoved)
	fmt.Println("  Transitive edges removed:", result.TransitiveEdgesRemoved)
	fmt.Println("  Subdividers added:", result.SubdividersAdded)
	// Output:
	// Before normalize:
	//   Nodes: 4
	//   Edges: 5
	// After normalize:
	//   Nodes: 4
	//   Edges: 4
	//   Rows: 3
	// Transformation metrics:
	//   Cycles removed: 0
	//   Transitive edges removed: 1
	//   Subdividers added: 0
}

func ExampleTransitiveReduction() {
	// A → B → C with transitive edge A → C
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "A", Row: 0})
	_ = g.AddNode(dag.Node{ID: "B", Row: 1})
	_ = g.AddNode(dag.Node{ID: "C", Row: 2})
	_ = g.AddEdge(dag.Edge{From: "A", To: "B"})
	_ = g.AddEdge(dag.Edge{From: "B", To: "C"})
	_ = g.AddEdge(dag.Edge{From: "A", To: "C"}) // Redundant

	fmt.Println("Before reduction:", g.EdgeCount(), "edges")
	transform.TransitiveReduction(g)
	fmt.Println("After reduction:", g.EdgeCount(), "edges")
	// Output:
	// Before reduction: 3 edges
	// After reduction: 2 edges
}

func ExampleAssignLayers() {
	// Create graph without layer assignments
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "app"})  // Will be row 0
	_ = g.AddNode(dag.Node{ID: "lib"})  // Will be row 1
	_ = g.AddNode(dag.Node{ID: "core"}) // Will be row 2
	_ = g.AddEdge(dag.Edge{From: "app", To: "lib"})
	_ = g.AddEdge(dag.Edge{From: "lib", To: "core"})

	transform.AssignLayers(g)

	app, _ := g.Node("app")
	lib, _ := g.Node("lib")
	core, _ := g.Node("core")

	fmt.Println("app row:", app.Row)
	fmt.Println("lib row:", lib.Row)
	fmt.Println("core row:", core.Row)
	// Output:
	// app row: 0
	// lib row: 1
	// core row: 2
}

func ExampleSubdivide() {
	// Create graph with a long edge spanning multiple rows
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "app", Row: 0})
	_ = g.AddNode(dag.Node{ID: "deep", Row: 3}) // 3 rows below app
	_ = g.AddEdge(dag.Edge{From: "app", To: "deep"})

	fmt.Println("Before subdivide:")
	fmt.Println("  Nodes:", g.NodeCount())

	transform.Subdivide(g)

	fmt.Println("After subdivide:")
	fmt.Println("  Nodes:", g.NodeCount())

	// Check that subdivider nodes were created
	subdividers := 0
	for _, n := range g.Nodes() {
		if n.IsSubdivider() {
			subdividers++
		}
	}
	fmt.Println("  Subdividers:", subdividers)
	// Output:
	// Before subdivide:
	//   Nodes: 2
	// After subdivide:
	//   Nodes: 4
	//   Subdividers: 2
}

func ExampleBreakCycles() {
	// Create a graph with a cycle (which shouldn't happen in deps, but might)
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "A"})
	_ = g.AddNode(dag.Node{ID: "B"})
	_ = g.AddNode(dag.Node{ID: "C"})
	_ = g.AddEdge(dag.Edge{From: "A", To: "B"})
	_ = g.AddEdge(dag.Edge{From: "B", To: "C"})
	_ = g.AddEdge(dag.Edge{From: "C", To: "A"}) // Creates cycle

	fmt.Println("Edges before:", g.EdgeCount())
	removed := transform.BreakCycles(g)
	fmt.Println("Edges after:", g.EdgeCount())
	fmt.Println("Removed:", removed)
	// Output:
	// Edges before: 3
	// Edges after: 2
	// Removed: 1
}

func ExampleResolveSpanOverlaps() {
	// Create a complete bipartite graph K(2,2) - the classic crossing pattern
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "auth", Row: 0})
	_ = g.AddNode(dag.Node{ID: "api", Row: 0})
	_ = g.AddNode(dag.Node{ID: "logging", Row: 1})
	_ = g.AddNode(dag.Node{ID: "metrics", Row: 1})

	// Both parents connect to both children (guaranteed crossing)
	_ = g.AddEdge(dag.Edge{From: "auth", To: "logging"})
	_ = g.AddEdge(dag.Edge{From: "auth", To: "metrics"})
	_ = g.AddEdge(dag.Edge{From: "api", To: "logging"})
	_ = g.AddEdge(dag.Edge{From: "api", To: "metrics"})

	fmt.Println("Before resolution:")
	fmt.Println("  Nodes:", g.NodeCount())
	fmt.Println("  Edges:", g.EdgeCount())

	transform.ResolveSpanOverlaps(g)

	fmt.Println("After resolution:")
	fmt.Println("  Nodes:", g.NodeCount())
	fmt.Println("  Edges:", g.EdgeCount())

	// Check for separator nodes
	hasSeparator := false
	for _, n := range g.Nodes() {
		if n.IsAuxiliary() {
			hasSeparator = true
			break
		}
	}
	fmt.Println("  Separator inserted:", hasSeparator)
	// Output:
	// Before resolution:
	//   Nodes: 4
	//   Edges: 4
	// After resolution:
	//   Nodes: 5
	//   Edges: 4
	//   Separator inserted: true
}

func ExampleNormalizeWithOptions() {
	// Build a graph that we know is already acyclic
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "api"})
	_ = g.AddNode(dag.Node{ID: "auth"})
	_ = g.AddNode(dag.Node{ID: "db"})
	_ = g.AddEdge(dag.Edge{From: "api", To: "auth"})
	_ = g.AddEdge(dag.Edge{From: "api", To: "db"}) // Transitive
	_ = g.AddEdge(dag.Edge{From: "auth", To: "db"})

	// Skip cycle breaking (we know it's acyclic) but keep transitive reduction
	result, err := transform.NormalizeWithOptions(g, transform.NormalizeOptions{
		SkipCycleBreaking: true,
	})
	if err != nil {
		fmt.Println("error:", err)
		return
	}

	fmt.Println("Cycles removed:", result.CyclesRemoved)
	fmt.Println("Transitive edges removed:", result.TransitiveEdgesRemoved)
	fmt.Println("Final edge count:", g.EdgeCount())
	// Output:
	// Cycles removed: 0
	// Transitive edges removed: 1
	// Final edge count: 2
}

func ExampleNormalizeWithOptions_preserveTransitive() {
	// Sometimes you want to preserve all edges even if redundant
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "A"})
	_ = g.AddNode(dag.Node{ID: "B"})
	_ = g.AddNode(dag.Node{ID: "C"})
	_ = g.AddEdge(dag.Edge{From: "A", To: "B"})
	_ = g.AddEdge(dag.Edge{From: "B", To: "C"})
	_ = g.AddEdge(dag.Edge{From: "A", To: "C"}) // Keep this transitive edge

	result, err := transform.NormalizeWithOptions(g, transform.NormalizeOptions{
		SkipTransitiveReduction: true,
	})
	if err != nil {
		fmt.Println("error:", err)
		return
	}

	fmt.Println("Transitive edges removed:", result.TransitiveEdgesRemoved)
	fmt.Println("Subdividers added:", result.SubdividersAdded)
	// Note: Edge count increases due to subdividers for A→C (0→2 requires subdivider)
	fmt.Println("Final edge count:", g.EdgeCount())
	// Output:
	// Transitive edges removed: 0
	// Subdividers added: 1
	// Final edge count: 4
}

func ExampleNormalizeWithOptions_skipSeparators() {
	// Accept edge crossings instead of inserting separator beams
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "auth"})
	_ = g.AddNode(dag.Node{ID: "api"})
	_ = g.AddNode(dag.Node{ID: "log"})
	_ = g.AddNode(dag.Node{ID: "metrics"})
	_ = g.AddEdge(dag.Edge{From: "auth", To: "log"})
	_ = g.AddEdge(dag.Edge{From: "auth", To: "metrics"})
	_ = g.AddEdge(dag.Edge{From: "api", To: "log"})
	_ = g.AddEdge(dag.Edge{From: "api", To: "metrics"})

	result, err := transform.NormalizeWithOptions(g, transform.NormalizeOptions{
		SkipSeparators: true,
	})
	if err != nil {
		fmt.Println("error:", err)
		return
	}

	fmt.Println("Separators added:", result.SeparatorsAdded)
	fmt.Println("Node count unchanged:", g.NodeCount() == 4)
	// Output:
	// Separators added: 0
	// Node count unchanged: true
}
