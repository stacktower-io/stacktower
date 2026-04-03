package sink

import (
	"strings"
	"testing"

	"github.com/matzehuels/stacktower/pkg/core/dag"
	"github.com/matzehuels/stacktower/pkg/core/render/tower/layout"
)

func TestRenderSVG_Simple(t *testing.T) {
	g := dag.New(nil)
	g.AddNode(dag.Node{ID: "A", Row: 0})
	g.AddNode(dag.Node{ID: "B", Row: 1})

	l := layout.Build(g, 100, 100)
	svg := RenderSVG(l)

	svgStr := string(svg)

	if !strings.Contains(svgStr, "<svg") {
		t.Error("SVG should contain <svg tag")
	}
	if !strings.Contains(svgStr, "</svg>") {
		t.Error("SVG should contain closing </svg> tag")
	}
	if !strings.Contains(svgStr, "<rect") {
		t.Error("SVG should contain rect elements")
	}
	if !strings.Contains(svgStr, "<text") {
		t.Error("SVG should contain text elements")
	}
	if !strings.Contains(svgStr, ">A</text>") {
		t.Error("SVG should contain node A label")
	}
	if !strings.Contains(svgStr, ">B</text>") {
		t.Error("SVG should contain node B label")
	}
}

func TestRenderSVG_WithEdges(t *testing.T) {
	g := dag.New(nil)
	g.AddNode(dag.Node{ID: "A", Row: 0})
	g.AddNode(dag.Node{ID: "B", Row: 1})
	g.AddEdge(dag.Edge{From: "A", To: "B"})

	l := layout.Build(g, 100, 100)
	svg := RenderSVG(l, WithGraph(g), WithEdges())

	svgStr := string(svg)

	if !strings.Contains(svgStr, "<line") {
		t.Error("SVG with edges should contain line elements")
	}

	if !strings.Contains(svgStr, "<rect") {
		t.Error("SVG should contain rect elements")
	}
}

func TestRenderSVG_WithoutEdges(t *testing.T) {
	g := dag.New(nil)
	g.AddNode(dag.Node{ID: "A", Row: 0})
	g.AddNode(dag.Node{ID: "B", Row: 1})
	g.AddEdge(dag.Edge{From: "A", To: "B"})

	l := layout.Build(g, 100, 100)
	svg := RenderSVG(l)

	svgStr := string(svg)

	if strings.Contains(svgStr, "<line") {
		t.Error("SVG without edges option should not contain line elements")
	}
}

func TestRenderSVG_Diamond(t *testing.T) {
	g := dag.New(nil)
	g.AddNode(dag.Node{ID: "A", Row: 0})
	g.AddNode(dag.Node{ID: "B", Row: 1})
	g.AddNode(dag.Node{ID: "C", Row: 1})
	g.AddNode(dag.Node{ID: "D", Row: 2})
	g.AddEdge(dag.Edge{From: "A", To: "B"})
	g.AddEdge(dag.Edge{From: "A", To: "C"})
	g.AddEdge(dag.Edge{From: "B", To: "D"})
	g.AddEdge(dag.Edge{From: "C", To: "D"})

	l := layout.Build(g, 400, 300)
	svg := RenderSVG(l, WithGraph(g), WithEdges())

	svgStr := string(svg)

	// 4 blocks + 4 text backgrounds = 8 rect elements
	blockCount := strings.Count(svgStr, "<rect")
	if blockCount != 8 {
		t.Errorf("Expected 8 rect elements (4 blocks + 4 text backgrounds), got %d", blockCount)
	}

	lineCount := strings.Count(svgStr, "<line")
	if lineCount != 4 {
		t.Errorf("Expected 4 line elements, got %d", lineCount)
	}

	for _, id := range []string{"A", "B", "C", "D"} {
		if !strings.Contains(svgStr, ">"+id+"</text>") {
			t.Errorf("SVG should contain label for node %s", id)
		}
	}
}

func TestRenderSVG_WithSubdividersShowsAllEdges(t *testing.T) {
	g := dag.New(nil)
	g.AddNode(dag.Node{ID: "A", Row: 0})
	g.AddNode(dag.Node{ID: "A_sub_1", Row: 1, Kind: dag.NodeKindSubdivider, MasterID: "A"})
	g.AddNode(dag.Node{ID: "A_sub_2", Row: 2, Kind: dag.NodeKindSubdivider, MasterID: "A"})
	g.AddNode(dag.Node{ID: "B", Row: 3})
	g.AddEdge(dag.Edge{From: "A", To: "A_sub_1"})
	g.AddEdge(dag.Edge{From: "A_sub_1", To: "A_sub_2"})
	g.AddEdge(dag.Edge{From: "A_sub_2", To: "B"})

	l := layout.Build(g, 100, 100)
	svg := RenderSVG(l, WithGraph(g), WithEdges())
	svgStr := string(svg)

	lineCount := strings.Count(svgStr, "<line")
	if lineCount != 3 {
		t.Errorf("Expected 3 edges (all DAG edges), got %d", lineCount)
	}
}

func TestRenderSVG_MergedSkipsInternalSubdividerEdges(t *testing.T) {
	g := dag.New(nil)
	g.AddNode(dag.Node{ID: "A", Row: 0})
	g.AddNode(dag.Node{ID: "A_sub_1", Row: 1, Kind: dag.NodeKindSubdivider, MasterID: "A"})
	g.AddNode(dag.Node{ID: "A_sub_2", Row: 2, Kind: dag.NodeKindSubdivider, MasterID: "A"})
	g.AddNode(dag.Node{ID: "B", Row: 3})
	g.AddEdge(dag.Edge{From: "A", To: "A_sub_1"})
	g.AddEdge(dag.Edge{From: "A_sub_1", To: "A_sub_2"})
	g.AddEdge(dag.Edge{From: "A_sub_2", To: "B"})

	l := layout.Build(g, 100, 100)
	svg := RenderSVG(l, WithGraph(g), WithEdges(), WithMerged())
	svgStr := string(svg)

	lineCount := strings.Count(svgStr, "<line")
	if lineCount != 1 {
		t.Errorf("Expected 1 edge (A→B only), got %d", lineCount)
	}
}

func TestRenderSVG_MergedDeduplicatesEdgesToSameMaster(t *testing.T) {
	g := dag.New(nil)
	g.AddNode(dag.Node{ID: "A", Row: 0})
	g.AddNode(dag.Node{ID: "B", Row: 0})
	g.AddNode(dag.Node{ID: "C", Row: 1})
	g.AddNode(dag.Node{ID: "C_sub_2", Row: 2, Kind: dag.NodeKindSubdivider, MasterID: "C"})
	g.AddNode(dag.Node{ID: "D", Row: 3})
	g.AddEdge(dag.Edge{From: "A", To: "C"})
	g.AddEdge(dag.Edge{From: "B", To: "C"})
	g.AddEdge(dag.Edge{From: "C", To: "C_sub_2"})
	g.AddEdge(dag.Edge{From: "C_sub_2", To: "D"})

	l := layout.Build(g, 100, 100)
	svg := RenderSVG(l, WithGraph(g), WithEdges(), WithMerged())
	svgStr := string(svg)

	lineCount := strings.Count(svgStr, "<line")
	if lineCount != 3 {
		t.Errorf("Expected 3 edges (A→C, B→C, C→D), got %d", lineCount)
	}
}

func TestExtractPopupData_FallsBackToNodeIDDescription(t *testing.T) {
	n := &dag.Node{ID: "stacktower", Meta: dag.Metadata{"virtual": true}}
	p := extractPopupData(n)
	if p == nil {
		t.Fatal("expected popup data")
	}
	if p.Description != "stacktower" {
		t.Fatalf("popup description = %q, want %q", p.Description, "stacktower")
	}
}
