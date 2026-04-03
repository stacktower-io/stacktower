package ui

import (
	"bytes"
	"strings"
	"testing"

	"github.com/matzehuels/stacktower/pkg/core/dag"
	"github.com/matzehuels/stacktower/pkg/core/deps"
	"github.com/matzehuels/stacktower/pkg/pipeline"
)

func TestGraphDepth_PrefersProjectRoot(t *testing.T) {
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: deps.ProjectRootNodeID})
	_ = g.AddNode(dag.Node{ID: "a"})
	_ = g.AddNode(dag.Node{ID: "b"})
	_ = g.AddEdge(dag.Edge{From: deps.ProjectRootNodeID, To: "a"})
	_ = g.AddEdge(dag.Edge{From: "a", To: "b"})

	// Disconnected deeper component should not control CLI depth reporting.
	_ = g.AddNode(dag.Node{ID: "z1"})
	_ = g.AddNode(dag.Node{ID: "z2"})
	_ = g.AddNode(dag.Node{ID: "z3"})
	_ = g.AddNode(dag.Node{ID: "z4"})
	_ = g.AddEdge(dag.Edge{From: "z1", To: "z2"})
	_ = g.AddEdge(dag.Edge{From: "z2", To: "z3"})
	_ = g.AddEdge(dag.Edge{From: "z3", To: "z4"})

	if got := GraphDepth(g); got != 2 {
		t.Fatalf("GraphDepth = %d, want 2", got)
	}
}

func TestGraphDepth_PrefersRenamedVirtualRoot(t *testing.T) {
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "stacktower", Meta: dag.Metadata{"virtual": true}})
	_ = g.AddNode(dag.Node{ID: "a"})
	_ = g.AddNode(dag.Node{ID: "b"})
	_ = g.AddEdge(dag.Edge{From: "stacktower", To: "a"})
	_ = g.AddEdge(dag.Edge{From: "a", To: "b"})

	_ = g.AddNode(dag.Node{ID: "z1"})
	_ = g.AddNode(dag.Node{ID: "z2"})
	_ = g.AddNode(dag.Node{ID: "z3"})
	_ = g.AddNode(dag.Node{ID: "z4"})
	_ = g.AddEdge(dag.Edge{From: "z1", To: "z2"})
	_ = g.AddEdge(dag.Edge{From: "z2", To: "z3"})
	_ = g.AddEdge(dag.Edge{From: "z3", To: "z4"})

	if got := GraphDepth(g); got != 2 {
		t.Fatalf("GraphDepth = %d, want 2", got)
	}
}

func TestBuildResolveResult(t *testing.T) {
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: deps.ProjectRootNodeID, Meta: dag.Metadata{"virtual": true}})
	_ = g.AddNode(dag.Node{ID: "requests", Meta: dag.Metadata{"version": "2.31.0"}})
	_ = g.AddNode(dag.Node{ID: "urllib3", Meta: dag.Metadata{"version": "2.0.4"}})
	_ = g.AddNode(dag.Node{ID: "certifi", Meta: dag.Metadata{"version": "2023.7.22"}})

	_ = g.AddEdge(dag.Edge{From: deps.ProjectRootNodeID, To: "requests", Meta: dag.Metadata{"constraint": ">=2.28"}})
	_ = g.AddEdge(dag.Edge{From: "requests", To: "urllib3", Meta: dag.Metadata{"constraint": ">=1.21,<3"}})
	_ = g.AddEdge(dag.Edge{From: "requests", To: "certifi", Meta: dag.Metadata{"constraint": ">=2017.4.17"}})

	result := pipeline.BuildResolveResult(g, deps.ProjectRootNodeID)

	if result.DirectCnt != 1 {
		t.Errorf("DirectCnt = %d, want 1", result.DirectCnt)
	}
	if result.TransCnt != 2 {
		t.Errorf("TransCnt = %d, want 2", result.TransCnt)
	}

	// Direct dep should be first
	if len(result.Entries) != 3 {
		t.Fatalf("Entries = %d, want 3", len(result.Entries))
	}
	if result.Entries[0].Package != "requests" {
		t.Errorf("First entry = %q, want requests", result.Entries[0].Package)
	}
	if !result.Entries[0].IsDirect {
		t.Error("requests should be marked as direct")
	}
	if result.Entries[0].Version != "2.31.0" {
		t.Errorf("requests version = %q, want 2.31.0", result.Entries[0].Version)
	}

	// Check constraints captured
	var urllib3Entry *ResolveEntry
	for i := range result.Entries {
		if result.Entries[i].Package == "urllib3" {
			urllib3Entry = &result.Entries[i]
			break
		}
	}
	if urllib3Entry == nil {
		t.Fatal("urllib3 entry not found")
	}
	if len(urllib3Entry.Constraints) != 1 || urllib3Entry.Constraints[0] != ">=1.21,<3" {
		t.Errorf("urllib3 constraints = %v, want [>=1.21,<3]", urllib3Entry.Constraints)
	}
	if len(urllib3Entry.RequiredBy) != 1 || urllib3Entry.RequiredBy[0] != "requests" {
		t.Errorf("urllib3 requiredBy = %v, want [requests]", urllib3Entry.RequiredBy)
	}
}

func TestWriteResolveOutput(t *testing.T) {
	result := ResolveResult{
		RootName:  "myproject",
		DirectCnt: 1,
		TransCnt:  2,
		Entries: []ResolveEntry{
			{Package: "requests", Version: "2.31.0", Constraints: []string{">=2.28"}, IsDirect: true},
			{Package: "certifi", Version: "2023.7.22", Constraints: []string{">=2017.4.17"}, RequiredBy: []string{"requests"}, IsDirect: false},
			{Package: "urllib3", Version: "2.0.4", Constraints: []string{">=1.21,<3"}, RequiredBy: []string{"requests"}, IsDirect: false},
		},
	}

	var buf bytes.Buffer
	WriteResolveOutput(&buf, result, false)
	output := buf.String()

	// Check header is present
	if !strings.Contains(output, "Package") {
		t.Error("output missing Package header")
	}
	if !strings.Contains(output, "Resolved") {
		t.Error("output missing Resolved header")
	}
	if !strings.Contains(output, "Constraint") {
		t.Error("output missing Constraint header")
	}
	if !strings.Contains(output, "Required By") {
		t.Error("output missing Required By header")
	}

	// Check sections
	if !strings.Contains(output, "Direct dependencies") {
		t.Error("output missing Direct dependencies section")
	}
	if !strings.Contains(output, "Transitive dependencies") {
		t.Error("output missing Transitive dependencies section")
	}

	// Check packages appear
	if !strings.Contains(output, "requests") {
		t.Error("output missing requests package")
	}
	if !strings.Contains(output, "urllib3") {
		t.Error("output missing urllib3 package")
	}
	if !strings.Contains(output, "2.31.0") {
		t.Error("output missing version 2.31.0")
	}
}

func TestFormatConstraints(t *testing.T) {
	tests := []struct {
		input    []string
		expected string
	}{
		{nil, "*"},
		{[]string{}, "*"},
		{[]string{">=1.0"}, ">=1.0"},
		{[]string{">=1.0", "<2.0"}, "<2.0, >=1.0"},
		{[]string{"<2.0", ">=1.0"}, "<2.0, >=1.0"}, // sorted alphabetically
	}

	for _, tc := range tests {
		got := FormatConstraints(tc.input)
		if got != tc.expected {
			t.Errorf("FormatConstraints(%v) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

func TestFormatRequiredBy(t *testing.T) {
	tests := []struct {
		parents  []string
		isDirect bool
		rootName string
		expected string
	}{
		{nil, true, "myproject", "myproject (direct)"},
		{nil, false, "myproject", "-"},
		{[]string{"a"}, false, "myproject", "a"},
		{[]string{"b", "a"}, false, "myproject", "a, b"},
		{[]string{"a", "b", "c", "d"}, false, "myproject", "a, b, c +1 more"},
	}

	for _, tc := range tests {
		got := FormatRequiredBy(tc.parents, tc.isDirect, tc.rootName)
		if got != tc.expected {
			t.Errorf("FormatRequiredBy(%v, %v, %q) = %q, want %q",
				tc.parents, tc.isDirect, tc.rootName, got, tc.expected)
		}
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		input    string
		maxLen   int
		expected string
	}{
		{"short", 10, "short"},
		{"exactly10!", 10, "exactly10!"},
		{"this is too long", 10, "this is..."},
		{"ab", 3, "ab"},
		{"abc", 3, "abc"},
		{"abcd", 3, "abc"},
	}

	for _, tc := range tests {
		got := Truncate(tc.input, tc.maxLen)
		if got != tc.expected {
			t.Errorf("Truncate(%q, %d) = %q, want %q", tc.input, tc.maxLen, got, tc.expected)
		}
	}
}
