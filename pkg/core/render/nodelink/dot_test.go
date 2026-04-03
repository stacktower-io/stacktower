package nodelink

import (
	"strings"
	"testing"

	"github.com/matzehuels/stacktower/pkg/core/dag"
	"github.com/matzehuels/stacktower/pkg/security"
)

func TestToDOT_Basic(t *testing.T) {
	g := dag.New(nil)
	g.AddNode(dag.Node{ID: "a", Row: 0})
	g.AddNode(dag.Node{ID: "b", Row: 1})
	g.AddEdge(dag.Edge{From: "a", To: "b"})

	dot := ToDOT(g, Options{})

	if !strings.Contains(dot, "digraph G") {
		t.Error("ToDOT() output missing digraph declaration")
	}
	if !strings.Contains(dot, `"a"`) {
		t.Error("ToDOT() output missing node a")
	}
	if !strings.Contains(dot, `"b"`) {
		t.Error("ToDOT() output missing node b")
	}
	if !strings.Contains(dot, `"a" -> "b"`) {
		t.Error("ToDOT() output missing edge")
	}
}

func TestToDOT_Detailed(t *testing.T) {
	g := dag.New(nil)
	g.AddNode(dag.Node{
		ID:   "pkg",
		Row:  1,
		Meta: dag.Metadata{"version": "1.0.0"},
	})

	dot := ToDOT(g, Options{Detailed: true})

	if !strings.Contains(dot, "row: 1") {
		t.Error("ToDOT() detailed output missing row info")
	}
	if !strings.Contains(dot, "version: 1.0.0") {
		t.Error("ToDOT() detailed output missing metadata")
	}
}

func TestToDOT_Subdivider(t *testing.T) {
	g := dag.New(nil)
	g.AddNode(dag.Node{
		ID:   "sub#1",
		Row:  1,
		Kind: dag.NodeKindSubdivider,
	})

	dot := ToDOT(g, Options{})

	if !strings.Contains(dot, "dashed") {
		t.Error("ToDOT() subdivider missing dashed style")
	}
	if !strings.Contains(dot, "lightgrey") {
		t.Error("ToDOT() subdivider missing lightgrey fill")
	}
}

func TestFmtLabel_Simple(t *testing.T) {
	n := dag.Node{ID: "test-node", Row: 0}
	label := fmtLabel(n, false)

	if label != "test-node" {
		t.Errorf("fmtLabel() simple mode = %q, want %q", label, "test-node")
	}
}

func TestFmtLabel_Detailed(t *testing.T) {
	n := dag.Node{
		ID:   "test-node",
		Row:  2,
		Meta: dag.Metadata{"key": "value"},
	}
	label := fmtLabel(n, true)

	if !strings.HasPrefix(label, "test-node\n") {
		t.Errorf("fmtLabel() detailed should start with ID: %q", label)
	}
	if !strings.Contains(label, "row: 2") {
		t.Errorf("fmtLabel() detailed missing row: %q", label)
	}
	if !strings.Contains(label, "key: value") {
		t.Errorf("fmtLabel() detailed missing metadata: %q", label)
	}
}

func TestFmtAttrs_Regular(t *testing.T) {
	n := dag.Node{ID: "regular", Kind: dag.NodeKindRegular}
	attrs := fmtAttrs(n, "test-label")

	if len(attrs) != 1 {
		t.Errorf("fmtAttrs() regular node should have 1 attr, got %d", len(attrs))
	}
	if !strings.Contains(attrs[0], "label=") {
		t.Errorf("fmtAttrs() regular node missing label attr: %v", attrs)
	}
}

func TestFmtAttrs_Subdivider(t *testing.T) {
	n := dag.Node{ID: "sub", Kind: dag.NodeKindSubdivider}
	attrs := fmtAttrs(n, "sub-label")

	if len(attrs) != 4 {
		t.Errorf("fmtAttrs() subdivider should have 4 attrs, got %d: %v", len(attrs), attrs)
	}

	joined := strings.Join(attrs, " ")
	if !strings.Contains(joined, "dashed") {
		t.Error("fmtAttrs() subdivider missing dashed style")
	}
	if !strings.Contains(joined, "lightgrey") {
		t.Error("fmtAttrs() subdivider missing lightgrey fill")
	}
}

func TestFmtAttrs_VulnerabilityUsesBackgroundColor(t *testing.T) {
	n := dag.Node{
		ID:   "vuln-node",
		Kind: dag.NodeKindRegular,
		Meta: dag.Metadata{
			security.MetaVulnSeverity: "high",
		},
	}

	attrs := fmtAttrs(n, "pkg-a")
	joined := strings.Join(attrs, " ")
	if strings.Contains(joined, "⚑") {
		t.Fatalf("fmtAttrs() should not add flag label for vulnerabilities: %v", attrs)
	}
	if !strings.Contains(joined, `fillcolor="#c2410c"`) {
		t.Fatalf("fmtAttrs() vuln node missing dark orange background: %v", attrs)
	}
	if !strings.Contains(joined, `fontcolor="white"`) {
		t.Fatalf("fmtAttrs() vuln node missing readable text color: %v", attrs)
	}
}

func TestFmtAttrs_LicenseRiskUsesBackgroundColor(t *testing.T) {
	n := dag.Node{
		ID:   "license-node",
		Kind: dag.NodeKindRegular,
		Meta: dag.Metadata{
			security.MetaLicenseRisk: string(security.LicenseRiskCopyleft),
		},
	}

	attrs := fmtAttrs(n, "pkg-b")
	joined := strings.Join(attrs, " ")
	if strings.Contains(joined, "⚑") {
		t.Fatalf("fmtAttrs() should not add flag label for license risk: %v", attrs)
	}
	if !strings.Contains(joined, `fillcolor="#9333ea"`) {
		t.Fatalf("fmtAttrs() license-risk node missing purple background: %v", attrs)
	}
	if !strings.Contains(joined, `fontcolor="white"`) {
		t.Fatalf("fmtAttrs() license-risk node missing readable text color: %v", attrs)
	}
}

func TestNormalizeViewBox(t *testing.T) {
	tests := []struct {
		name string
		svg  string
		want string
	}{
		{
			name: "with viewBox",
			svg:  `<svg viewBox="10 20 800 600" xmlns="http://www.w3.org/2000/svg">content</svg>`,
			want: `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 800.00 600.00" width="800" height="600">content</svg>`,
		},
		{
			name: "no viewBox",
			svg:  `<svg xmlns="http://www.w3.org/2000/svg">content</svg>`,
			want: `<svg xmlns="http://www.w3.org/2000/svg">content</svg>`,
		},
		{
			name: "zero dimensions",
			svg:  `<svg viewBox="0 0 0 0">content</svg>`,
			want: `<svg viewBox="0 0 0 0">content</svg>`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeViewBox([]byte(tt.svg))
			if string(got) != tt.want {
				t.Errorf("normalizeViewBox() = %q, want %q", string(got), tt.want)
			}
		})
	}
}

func TestRenderSVG(t *testing.T) {
	// Simple DOT that should render
	dot := `digraph G { a -> b; }`
	svg, err := RenderSVG(dot)
	if err != nil {
		t.Fatalf("RenderSVG() error: %v", err)
	}

	if !strings.Contains(string(svg), "<svg") {
		t.Error("RenderSVG() output missing <svg> tag")
	}
}

func TestRenderSVG_InvalidDOT(t *testing.T) {
	// Invalid DOT syntax
	dot := `not valid DOT {{{`
	_, err := RenderSVG(dot)
	if err == nil {
		t.Error("RenderSVG() should return error for invalid DOT")
	}
}
