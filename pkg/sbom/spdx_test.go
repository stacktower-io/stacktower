package sbom

import (
	"encoding/json"
	"testing"

	"github.com/stacktower-io/stacktower/pkg/core/dag"
)

func TestGenerateSPDX(t *testing.T) {
	g := dag.New(dag.Metadata{"language": "python"})
	g.AddNode(dag.Node{ID: "flask", Row: 0, Meta: dag.Metadata{"version": "3.1.0"}})
	g.AddNode(dag.Node{ID: "werkzeug", Row: 1, Meta: dag.Metadata{
		"version": "3.1.0",
		"license": "BSD-3-Clause",
	}})
	g.AddNode(dag.Node{ID: "markupsafe", Row: 2, Meta: dag.Metadata{
		"version": "3.0.2",
	}})
	g.AddEdge(dag.Edge{From: "flask", To: "werkzeug"})
	g.AddEdge(dag.Edge{From: "werkzeug", To: "markupsafe"})

	data, err := GenerateSPDX(g, Options{Language: "python", ToolVersion: "1.0.0"})
	if err != nil {
		t.Fatal(err)
	}

	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if doc["spdxVersion"] != "SPDX-2.3" {
		t.Errorf("spdxVersion: got %v", doc["spdxVersion"])
	}
	if doc["dataLicense"] != "CC0-1.0" {
		t.Errorf("dataLicense: got %v", doc["dataLicense"])
	}

	packages, ok := doc["packages"].([]any)
	if !ok {
		t.Fatal("missing packages")
	}
	// root + 2 deps = 3 packages
	if len(packages) != 3 {
		t.Errorf("expected 3 packages, got %d", len(packages))
	}

	relationships, ok := doc["relationships"].([]any)
	if !ok {
		t.Fatal("missing relationships")
	}
	// 1 DESCRIBES + 2 DEPENDS_ON = 3
	if len(relationships) < 3 {
		t.Errorf("expected at least 3 relationships, got %d", len(relationships))
	}

	// Check that werkzeug has purl and license
	for _, p := range packages {
		pkg := p.(map[string]any)
		if pkg["name"] == "werkzeug" {
			if pkg["licenseConcluded"] != "BSD-3-Clause" {
				t.Errorf("werkzeug license: got %v", pkg["licenseConcluded"])
			}
			refs, ok := pkg["externalRefs"].([]any)
			if !ok || len(refs) == 0 {
				t.Error("werkzeug missing externalRefs (purl)")
			}
		}
	}
}

func TestSPDXID(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"flask", "SPDXRef-flask"},
		{"@angular/core", "SPDXRef--angular-core"},
		{"golang.org/x/sync", "SPDXRef-golang.org-x-sync"},
	}
	for _, tt := range tests {
		got := spdxID(tt.input)
		if got != tt.want {
			t.Errorf("spdxID(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
