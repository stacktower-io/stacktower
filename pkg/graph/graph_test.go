package graph

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/matzehuels/stacktower/pkg/core/dag"
)

func TestMarshalGraph(t *testing.T) {
	tests := []struct {
		name      string
		build     func() *dag.DAG
		wantNodes int
		wantEdges int
		check     func(t *testing.T, g Graph)
	}{
		{
			name:      "Empty",
			build:     func() *dag.DAG { return dag.New(nil) },
			wantNodes: 0,
			wantEdges: 0,
		},
		{
			name: "Simple",
			build: func() *dag.DAG {
				g := dag.New(nil)
				g.AddNode(dag.Node{ID: "a", Meta: dag.Metadata{"version": "1.0"}})
				g.AddNode(dag.Node{ID: "b", Meta: dag.Metadata{"version": "2.0"}})
				g.AddEdge(dag.Edge{From: "a", To: "b"})
				return g
			},
			wantNodes: 2,
			wantEdges: 1,
		},
		{
			name: "PreservesMetadata",
			build: func() *dag.DAG {
				g := dag.New(nil)
				g.AddNode(dag.Node{
					ID: "test",
					Meta: dag.Metadata{
						"version": "1.0",
						"author":  "test-author",
					},
				})
				return g
			},
			wantNodes: 1,
			wantEdges: 0,
			check: func(t *testing.T, g Graph) {
				if g.Nodes[0].Meta["version"] != "1.0" {
					t.Errorf("version = %v, want 1.0", g.Nodes[0].Meta["version"])
				}
				if g.Nodes[0].Meta["author"] != "test-author" {
					t.Errorf("author = %v, want test-author", g.Nodes[0].Meta["author"])
				}
			},
		},
		{
			name: "Diamond",
			build: func() *dag.DAG {
				g := dag.New(nil)
				g.AddNode(dag.Node{ID: "a"})
				g.AddNode(dag.Node{ID: "b"})
				g.AddNode(dag.Node{ID: "c"})
				g.AddNode(dag.Node{ID: "d"})
				g.AddEdge(dag.Edge{From: "a", To: "b"})
				g.AddEdge(dag.Edge{From: "a", To: "c"})
				g.AddEdge(dag.Edge{From: "b", To: "d"})
				g.AddEdge(dag.Edge{From: "c", To: "d"})
				return g
			},
			wantNodes: 4,
			wantEdges: 4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := tt.build()

			data, err := MarshalGraph(g)
			if err != nil {
				t.Fatalf("MarshalGraph: %v", err)
			}

			var result Graph
			if err := json.Unmarshal(data, &result); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}

			if got := len(result.Nodes); got != tt.wantNodes {
				t.Errorf("nodes = %d, want %d", got, tt.wantNodes)
			}
			if got := len(result.Edges); got != tt.wantEdges {
				t.Errorf("edges = %d, want %d", got, tt.wantEdges)
			}

			if tt.check != nil {
				tt.check(t, result)
			}
		})
	}
}

func TestReadGraph(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantNodes int
		wantEdges int
		wantErr   bool
		check     func(t *testing.T, g *dag.DAG)
	}{
		{
			name: "Valid",
			input: `{
				"nodes": [
					{"id": "A", "meta": {"version": "1.0"}},
					{"id": "B"}
				],
				"edges": [
					{"from": "A", "to": "B"}
				]
			}`,
			wantNodes: 2,
			wantEdges: 1,
			check: func(t *testing.T, g *dag.DAG) {
				n, ok := g.Node("A")
				if !ok {
					t.Fatal("node A not found")
				}
				if n.Meta["version"] != "1.0" {
					t.Errorf("version = %v, want 1.0", n.Meta["version"])
				}
			},
		},
		{
			name: "Empty",
			input: `{
				"nodes": [],
				"edges": []
			}`,
			wantNodes: 0,
			wantEdges: 0,
		},
		{
			name:    "Invalid",
			input:   `{invalid json}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := strings.NewReader(tt.input)
			g, err := ReadGraph(r)

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("ReadGraph: %v", err)
			}

			if got := g.NodeCount(); got != tt.wantNodes {
				t.Errorf("nodes = %d, want %d", got, tt.wantNodes)
			}
			if got := g.EdgeCount(); got != tt.wantEdges {
				t.Errorf("edges = %d, want %d", got, tt.wantEdges)
			}

			if tt.check != nil {
				tt.check(t, g)
			}
		})
	}
}

func TestReadGraphFile(t *testing.T) {
	content := `{
		"nodes": [{"id": "A"}],
		"edges": []
	}`

	dir := t.TempDir()
	path := filepath.Join(dir, "test.json")

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	g, err := ReadGraphFile(path)
	if err != nil {
		t.Fatalf("ReadGraphFile: %v", err)
	}

	if g.NodeCount() != 1 {
		t.Errorf("nodes = %d, want 1", g.NodeCount())
	}
}

func TestReadGraphFileNotFound(t *testing.T) {
	_, err := ReadGraphFile("nonexistent.json")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestWriteGraph(t *testing.T) {
	g := dag.New(nil)
	g.AddNode(dag.Node{ID: "a"})
	g.AddNode(dag.Node{ID: "b"})
	g.AddEdge(dag.Edge{From: "a", To: "b"})

	var buf bytes.Buffer
	if err := WriteGraph(g, &buf); err != nil {
		t.Fatalf("WriteGraph: %v", err)
	}

	var result Graph
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(result.Nodes) != 2 {
		t.Errorf("nodes = %d, want 2", len(result.Nodes))
	}
}

func TestReadGraph_MalformedJSON(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"truncated", `{"nodes": [`},
		{"unclosed brace", `{"nodes": []`},
		{"invalid escape", `{"nodes": [{"id": "\x"}]}`},
		{"deeply nested", generateDeeplyNestedJSON(100)},
		{"null nodes", `{"nodes": null, "edges": []}`},
		{"wrong type nodes", `{"nodes": "not an array", "edges": []}`},
		{"wrong type edges", `{"nodes": [], "edges": "not an array"}`},
		{"extra fields", `{"nodes": [], "edges": [], "extra": "field"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := strings.NewReader(tt.input)
			_, err := ReadGraph(r)
			// We're just checking it doesn't panic
			_ = err
		})
	}
}

func TestReadGraph_DAGValidation(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name: "self loop allowed",
			input: `{
				"nodes": [{"id": "A"}],
				"edges": [{"from": "A", "to": "A"}]
			}`,
			wantErr: false, // Self-loops handled later by transform.BreakCycles, not at read time
		},
		{
			name: "duplicate node IDs rejected",
			input: `{
				"nodes": [{"id": "A"}, {"id": "A"}],
				"edges": []
			}`,
			wantErr: true, // Duplicate IDs are properly rejected
		},
		{
			name: "edge to nonexistent node",
			input: `{
				"nodes": [{"id": "A"}],
				"edges": [{"from": "A", "to": "B"}]
			}`,
			wantErr: true,
		},
		{
			name: "cycle allowed by reader",
			input: `{
				"nodes": [{"id": "A"}, {"id": "B"}, {"id": "C"}],
				"edges": [
					{"from": "A", "to": "B"},
					{"from": "B", "to": "C"},
					{"from": "C", "to": "A"}
				]
			}`,
			wantErr: false, // Cycles handled later by transform.BreakCycles, not at read time
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := strings.NewReader(tt.input)
			_, err := ReadGraph(r)
			if (err != nil) != tt.wantErr {
				t.Errorf("ReadGraph() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestReadGraph_SpecialCharactersInIDs(t *testing.T) {
	tests := []struct {
		name  string
		id    string
		valid bool
	}{
		{"unicode", "パッケージ", true},
		{"emoji", "📦", true},
		{"spaces", "my package", true},
		{"slashes", "org/pkg", true},
		{"at sign", "@scope/pkg", true},
		{"empty string", "", false},
		{"very long id", strings.Repeat("a", 10000), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := `{"nodes": [{"id": ` + jsonString(tt.id) + `}], "edges": []}`
			r := strings.NewReader(input)
			g, err := ReadGraph(r)

			if tt.valid {
				if err != nil {
					t.Errorf("ReadGraph() unexpected error = %v", err)
					return
				}
				if g.NodeCount() != 1 {
					t.Errorf("expected 1 node, got %d", g.NodeCount())
				}
			}
		})
	}
}

func TestWriteGraphFile_Permissions(t *testing.T) {
	g := dag.New(nil)
	g.AddNode(dag.Node{ID: "a"})

	dir := t.TempDir()
	path := filepath.Join(dir, "output.json")

	if err := WriteGraphFile(g, path); err != nil {
		t.Fatalf("WriteGraphFile: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}

	mode := info.Mode().Perm()
	// File should be readable (at least by owner)
	if mode&0o400 == 0 {
		t.Errorf("file not readable, mode = %o", mode)
	}
}

func TestWriteGraphFile_OverwriteExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "output.json")

	// Write first version
	g1 := dag.New(nil)
	g1.AddNode(dag.Node{ID: "original"})
	if err := WriteGraphFile(g1, path); err != nil {
		t.Fatalf("first WriteGraphFile: %v", err)
	}

	// Write second version (should overwrite)
	g2 := dag.New(nil)
	g2.AddNode(dag.Node{ID: "updated"})
	if err := WriteGraphFile(g2, path); err != nil {
		t.Fatalf("second WriteGraphFile: %v", err)
	}

	// Verify overwrite worked
	g, err := ReadGraphFile(path)
	if err != nil {
		t.Fatalf("ReadGraphFile: %v", err)
	}
	if _, ok := g.Node("updated"); !ok {
		t.Error("expected 'updated' node after overwrite")
	}
	if _, ok := g.Node("original"); ok {
		t.Error("'original' node should have been overwritten")
	}
}

func TestRoundTrip(t *testing.T) {
	original := dag.New(nil)
	original.AddNode(dag.Node{
		ID: "root",
		Meta: dag.Metadata{
			"version":     "1.2.3",
			"stars":       float64(1000),
			"description": "A test package\nwith newlines",
		},
	})
	original.AddNode(dag.Node{ID: "dep1"})
	original.AddNode(dag.Node{ID: "dep2"})
	original.AddEdge(dag.Edge{From: "root", To: "dep1"})
	original.AddEdge(dag.Edge{From: "root", To: "dep2"})
	original.AddEdge(dag.Edge{From: "dep1", To: "dep2"})

	// Marshal
	data, err := MarshalGraph(original)
	if err != nil {
		t.Fatalf("MarshalGraph: %v", err)
	}

	// Unmarshal
	restored, err := ReadGraph(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("ReadGraph: %v", err)
	}

	// Verify structure
	if restored.NodeCount() != original.NodeCount() {
		t.Errorf("nodes = %d, want %d", restored.NodeCount(), original.NodeCount())
	}
	if restored.EdgeCount() != original.EdgeCount() {
		t.Errorf("edges = %d, want %d", restored.EdgeCount(), original.EdgeCount())
	}

	// Verify metadata preserved
	root, ok := restored.Node("root")
	if !ok {
		t.Fatal("root node not found")
	}
	if root.Meta["version"] != "1.2.3" {
		t.Errorf("version = %v, want 1.2.3", root.Meta["version"])
	}
}

func generateDeeplyNestedJSON(depth int) string {
	var sb strings.Builder
	sb.WriteString(`{"nodes": [{"id": "a", "meta": {`)
	for i := 0; i < depth; i++ {
		sb.WriteString(`"nested": {`)
	}
	sb.WriteString(`"value": 1`)
	for i := 0; i < depth; i++ {
		sb.WriteString(`}`)
	}
	sb.WriteString(`}}], "edges": []}`)
	return sb.String()
}

func jsonString(s string) string {
	data, _ := json.Marshal(s)
	return string(data)
}
