package python

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/matzehuels/stacktower/pkg/core/deps"
)

func TestUVLock_Supports(t *testing.T) {
	parser := &UVLock{}

	tests := []struct {
		name     string
		filename string
		want     bool
	}{
		{"uv.lock", "uv.lock", true},
		{"poetry.lock", "poetry.lock", false},
		{"requirements.txt", "requirements.txt", false},
		{"pyproject.toml", "pyproject.toml", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parser.Supports(tt.filename); got != tt.want {
				t.Errorf("Supports(%q) = %v, want %v", tt.filename, got, tt.want)
			}
		})
	}
}

func TestUVLock_Type(t *testing.T) {
	parser := &UVLock{}
	if got := parser.Type(); got != "uv.lock" {
		t.Errorf("Type() = %q, want %q", got, "uv.lock")
	}
}

func TestUVLock_IncludesTransitive(t *testing.T) {
	parser := &UVLock{}
	if got := parser.IncludesTransitive(); !got {
		t.Errorf("IncludesTransitive() = %v, want true", got)
	}
}

func TestUVLock_Parse(t *testing.T) {
	// Create a temporary uv.lock file
	content := `version = 1
requires-python = ">=3.9"

[[package]]
name = "requests"
version = "2.31.0"
source = { registry = "https://pypi.org/simple" }
dependencies = [
    { name = "certifi" },
    { name = "charset-normalizer", specifier = ">=2,<4" },
    { name = "idna", specifier = ">=2.5,<4" },
    { name = "urllib3", specifier = ">=1.21.1,<3" },
]
dev-dependencies = [{ name = "pytest", specifier = ">=8" }]

[[package]]
name = "certifi"
version = "2024.2.2"
source = { registry = "https://pypi.org/simple" }

[[package]]
name = "charset-normalizer"
version = "3.3.2"
source = { registry = "https://pypi.org/simple" }

[[package]]
name = "idna"
version = "3.6"
source = { registry = "https://pypi.org/simple" }

[[package]]
name = "urllib3"
version = "2.2.0"
source = { registry = "https://pypi.org/simple" }

[[package]]
name = "pytest"
version = "8.3.0"
source = { registry = "https://pypi.org/simple" }
`

	tmpDir := t.TempDir()
	lockPath := filepath.Join(tmpDir, "uv.lock")
	if err := os.WriteFile(lockPath, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	parser := &UVLock{}
	result, err := parser.Parse(lockPath, deps.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if result == nil {
		t.Fatal("Parse() returned nil result")
	}

	if result.Type != "uv.lock" {
		t.Errorf("result.Type = %q, want %q", result.Type, "uv.lock")
	}

	if !result.IncludesTransitive {
		t.Error("result.IncludesTransitive = false, want true")
	}

	g := result.Graph

	// Check that all packages are present
	expectedPkgs := []string{"requests", "certifi", "charset-normalizer", "idna", "urllib3"}
	for _, pkg := range expectedPkgs {
		if _, ok := g.Node(pkg); !ok {
			t.Errorf("Expected package %q not found in graph", pkg)
		}
	}

	// Check that requests has correct version
	if node, ok := g.Node("requests"); ok {
		if v, ok := node.Meta["version"].(string); !ok || v != "2.31.0" {
			t.Errorf("requests version = %v, want %q", node.Meta["version"], "2.31.0")
		}
	}

	// Check that edges exist from requests to its dependencies
	edges := g.Edges()
	hasEdge := func(from, to string) bool {
		for _, e := range edges {
			if e.From == from && e.To == to {
				return true
			}
		}
		return false
	}

	requestsDeps := []string{"certifi", "charset-normalizer", "idna", "urllib3"}
	for _, dep := range requestsDeps {
		if !hasEdge("requests", dep) {
			t.Errorf("Expected edge from requests to %s not found", dep)
		}
	}
	if _, ok := g.Node("pytest"); ok {
		t.Error("did not expect dev-only pytest package in prod_only scope")
	}
}

func TestUVLock_Parse_V2DevDepsGroupFormat(t *testing.T) {
	// uv.lock v2 uses [package.dev-dependencies] with named groups
	content := `version = 2
requires-python = ">=3.9"

[[package]]
name = "myapp"
version = "0.1.0"
source = { virtual = "." }

[package.dev-dependencies]
dev = [
    { name = "pytest", specifier = ">=8" },
]

[[package]]
name = "pytest"
version = "8.3.0"
source = { registry = "https://pypi.org/simple" }
`
	tmpDir := t.TempDir()
	lockPath := tmpDir + "/uv.lock"
	if err := os.WriteFile(lockPath, []byte(content), 0644); err != nil {
		t.Fatalf("write lock file: %v", err)
	}

	parser := &UVLock{}
	result, err := parser.Parse(lockPath, deps.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	// prod_only scope: pytest is dev-only and must be absent
	if _, ok := result.Graph.Node("pytest"); ok {
		t.Error("pytest should not be present in prod_only scope (v2 group format)")
	}

	// all scope: pytest must appear
	resultAll, err := parser.Parse(lockPath, deps.Options{DependencyScope: deps.DependencyScopeAll})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if _, ok := resultAll.Graph.Node("pytest"); !ok {
		t.Error("pytest should be present in all scope (v2 group format)")
	}
}

func TestBuildUVGraph_VirtualRootUsesProjectRootID(t *testing.T) {
	// The virtual/editable package (project root) must be stored under
	// deps.ProjectRootNodeID so that the caller can rename it to the repo name without
	// conflicting with a same-named regular package node.
	packages := []uvLockPkg{
		{
			Name:    "myapp",
			Version: "0.1.0",
			Source:  uvSource{Virtual: "."},
			Dependencies: []uvDependency{
				{Name: "requests"},
			},
		},
		{
			Name:    "requests",
			Version: "2.31.0",
		},
	}

	g := buildUVGraph(context.Background(), packages, deps.DependencyScopeProdOnly)

	// Root must be __project__, not "myapp"
	if _, ok := g.Node(deps.ProjectRootNodeID); !ok {
		t.Fatal("expected __project__ node for virtual root package")
	}
	if _, ok := g.Node("myapp"); ok {
		t.Error("myapp should not exist as a separate node (it IS __project__)")
	}

	// __project__ must have an edge to requests
	found := false
	for _, e := range g.Edges() {
		if e.From == deps.ProjectRootNodeID && e.To == "requests" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected edge from __project__ to requests")
	}

	// Version metadata must survive on the root node
	root, _ := g.Node(deps.ProjectRootNodeID)
	if v, _ := root.Meta["version"].(string); v != "0.1.0" {
		t.Errorf("__project__ version = %q, want %q", v, "0.1.0")
	}
}

func TestBuildUVGraph(t *testing.T) {
	packages := []uvLockPkg{
		{
			Name:    "parent",
			Version: "1.0.0",
			Dependencies: []uvDependency{
				{Name: "child", Specifier: ">=1.0"},
			},
		},
		{
			Name:    "child",
			Version: "1.2.0",
		},
	}

	g := buildUVGraph(context.Background(), packages, deps.DependencyScopeProdOnly)

	// Both packages should be nodes
	if _, ok := g.Node("parent"); !ok {
		t.Error("Expected 'parent' node not found")
	}
	if _, ok := g.Node("child"); !ok {
		t.Error("Expected 'child' node not found")
	}

	// Should have edge from parent to child
	edges := g.Edges()
	found := false
	for _, e := range edges {
		if e.From == "parent" && e.To == "child" {
			found = true
			if c, ok := e.Meta["constraint"].(string); !ok || c != ">=1.0" {
				t.Errorf("Edge constraint = %v, want %q", e.Meta["constraint"], ">=1.0")
			}
			break
		}
	}
	if !found {
		t.Error("Expected edge from parent to child not found")
	}

	// child should have incoming edge, so only parent connects to __project__
	for _, e := range edges {
		if e.From == deps.ProjectRootNodeID && e.To == "child" {
			t.Error("child should not connect directly to __project__")
		}
	}
}

func TestBuildUVGraph_AllScopeIncludesDevDeps(t *testing.T) {
	packages := []uvLockPkg{
		{
			Name:    "app",
			Version: "1.0.0",
			DevDeps: uvDevDeps{Groups: map[string][]uvDependency{"dev": {{Name: "pytest", Specifier: ">=8"}}}},
		},
		{
			Name:    "pytest",
			Version: "8.3.0",
		},
	}
	g := buildUVGraph(context.Background(), packages, deps.DependencyScopeAll)
	if _, ok := g.Node("pytest"); !ok {
		t.Fatal("expected pytest node in all scope")
	}
	found := false
	for _, e := range g.Edges() {
		if e.From == "app" && e.To == "pytest" {
			found = true
			if dev, _ := e.Meta["dev"].(bool); !dev {
				t.Error("expected edge to be marked as dev")
			}
		}
	}
	if !found {
		t.Error("expected app -> pytest edge")
	}
}
