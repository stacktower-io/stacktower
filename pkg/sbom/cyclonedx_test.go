package sbom

import (
	"encoding/json"
	"encoding/xml"
	"testing"

	"github.com/stacktower-io/stacktower/pkg/core/dag"
	"github.com/stacktower-io/stacktower/pkg/security"
)

func buildTestGraph() *dag.DAG {
	g := dag.New(dag.Metadata{"language": "python"})
	g.AddNode(dag.Node{ID: "flask", Row: 0, Meta: dag.Metadata{"version": "3.1.0"}})
	g.AddNode(dag.Node{ID: "werkzeug", Row: 1, Meta: dag.Metadata{
		"version":  "3.1.0",
		"license":  "BSD-3-Clause",
		"repo_url": "https://github.com/pallets/werkzeug",
	}})
	g.AddNode(dag.Node{ID: "markupsafe", Row: 2, Meta: dag.Metadata{
		"version": "3.0.2",
		"license": "BSD-3-Clause",
	}})
	g.AddEdge(dag.Edge{From: "flask", To: "werkzeug"})
	g.AddEdge(dag.Edge{From: "werkzeug", To: "markupsafe"})
	return g
}

func TestGenerateCycloneDX_JSON(t *testing.T) {
	g := buildTestGraph()
	data, err := GenerateCycloneDX(g, Options{
		Format:      FormatCycloneDX,
		Encoding:    EncodingJSON,
		Language:    "python",
		ToolName:    "stacktower",
		ToolVersion: "1.0.0",
	})
	if err != nil {
		t.Fatal(err)
	}

	var bom map[string]any
	if err := json.Unmarshal(data, &bom); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if bom["bomFormat"] != "CycloneDX" {
		t.Errorf("bomFormat: got %v", bom["bomFormat"])
	}
	if bom["specVersion"] != "1.6" {
		t.Errorf("specVersion: got %v", bom["specVersion"])
	}

	components, ok := bom["components"].([]any)
	if !ok {
		t.Fatal("missing components")
	}
	if len(components) != 2 {
		t.Errorf("expected 2 components, got %d", len(components))
	}

	// Check first component has purl
	comp0 := components[0].(map[string]any)
	if purl, ok := comp0["purl"].(string); !ok || purl == "" {
		t.Error("first component missing purl")
	}

	deps, ok := bom["dependencies"].([]any)
	if !ok {
		t.Fatal("missing dependencies")
	}
	if len(deps) < 2 {
		t.Errorf("expected at least 2 dependency entries, got %d", len(deps))
	}
}

func TestGenerateCycloneDX_XML(t *testing.T) {
	g := buildTestGraph()
	data, err := GenerateCycloneDX(g, Options{
		Encoding: EncodingXML,
		Language: "python",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Verify valid XML
	var v any
	if err := xml.Unmarshal(data, &v); err != nil {
		t.Fatalf("invalid XML: %v\n%s", err, string(data))
	}
}

func TestGenerateCycloneDX_WithVulns(t *testing.T) {
	g := buildTestGraph()
	report := &security.Report{
		Findings: []security.Finding{
			{
				ID:       "GHSA-1234",
				Package:  "werkzeug",
				Version:  "3.1.0",
				Severity: security.SeverityHigh,
			},
		},
	}

	data, err := GenerateCycloneDX(g, Options{
		Language:   "python",
		VulnReport: report,
	})
	if err != nil {
		t.Fatal(err)
	}

	var bom map[string]any
	if err := json.Unmarshal(data, &bom); err != nil {
		t.Fatal(err)
	}

	vulns, ok := bom["vulnerabilities"].([]any)
	if !ok || len(vulns) != 1 {
		t.Fatalf("expected 1 vulnerability, got %v", bom["vulnerabilities"])
	}
}

func TestGenerateCycloneDX_LanguageFromMeta(t *testing.T) {
	g := buildTestGraph() // has language=python in meta
	data, err := GenerateCycloneDX(g, Options{})
	if err != nil {
		t.Fatal(err)
	}

	var bom map[string]any
	json.Unmarshal(data, &bom)
	components := bom["components"].([]any)
	comp := components[0].(map[string]any)
	purl, _ := comp["purl"].(string)
	if purl == "" {
		t.Error("expected purl to be generated from graph language metadata")
	}
}
