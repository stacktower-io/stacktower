package python

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/matzehuels/stacktower/pkg/core/deps"
)

func TestPoetryLock_Supports(t *testing.T) {
	parser := &PoetryLock{}

	tests := []struct {
		filename string
		want     bool
	}{
		{"poetry.lock", true},
		{"Poetry.lock", false},
		{"requirements.txt", false},
		{"pyproject.toml", false},
	}

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			if got := parser.Supports(tt.filename); got != tt.want {
				t.Errorf("Supports(%q) = %v, want %v", tt.filename, got, tt.want)
			}
		})
	}
}

func TestPoetryLock_Parse(t *testing.T) {
	dir := t.TempDir()
	lockFile := filepath.Join(dir, "poetry.lock")
	content := `[[package]]
name = "requests"
version = "2.31.0"
description = "Python HTTP for Humans."
category = "main"
optional = false
python-versions = ">=3.7"

[package.dependencies]
certifi = ">=2017.4.17"
urllib3 = ">=1.21.1,<3"

[[package]]
name = "certifi"
version = "2024.2.2"
description = "Python package for providing Mozilla's CA Bundle."
category = "main"
optional = false
python-versions = ">=3.6"

[[package]]
name = "urllib3"
version = "2.2.1"
description = "HTTP library with thread-safe connection pooling."
category = "main"
optional = false
python-versions = ">=3.8"

[[package]]
name = "pytest"
version = "8.3.0"
description = "pytest dev dependency"
category = "dev"
optional = false
python-versions = ">=3.8"

[metadata]
lock-version = "2.0"
python-versions = "^3.10"
content-hash = "abc123"
`
	if err := os.WriteFile(lockFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	parser := &PoetryLock{}
	result, err := parser.Parse(lockFile, deps.Options{})
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	g := result.Graph

	if got := g.NodeCount(); got != 4 {
		t.Errorf("NodeCount = %d, want 4", got)
	}

	reqNode, ok := g.Node("requests")
	if !ok {
		t.Fatal("requests node not found")
	}
	if v := reqNode.Meta["version"]; v != "2.31.0" {
		t.Errorf("requests version = %v, want 2.31.0", v)
	}

	children := g.Children("requests")
	if len(children) != 2 {
		t.Errorf("requests has %d children, want 2", len(children))
	}

	projectChildren := g.Children("__project__")
	if len(projectChildren) != 1 {
		t.Errorf("__project__ has %d children, want 1", len(projectChildren))
	}
	if _, ok := g.Node("pytest"); ok {
		t.Error("did not expect dev dependency pytest in prod_only scope")
	}
}

func TestPoetryLock_Type(t *testing.T) {
	parser := &PoetryLock{}
	if got := parser.Type(); got != "poetry.lock" {
		t.Errorf("Type() = %q, want %q", got, "poetry.lock")
	}
}

func TestPoetryLock_IncludesTransitive(t *testing.T) {
	parser := &PoetryLock{}
	if !parser.IncludesTransitive() {
		t.Error("IncludesTransitive() = false, want true")
	}
}

func TestPoetryLock_AllScopeIncludesDevPackages(t *testing.T) {
	dir := t.TempDir()
	lockFile := filepath.Join(dir, "poetry.lock")
	content := `[[package]]
name = "pytest"
version = "8.3.0"
category = "dev"
`
	if err := os.WriteFile(lockFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	parser := &PoetryLock{}
	result, err := parser.Parse(lockFile, deps.Options{DependencyScope: deps.DependencyScopeAll})
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if _, ok := result.Graph.Node("pytest"); !ok {
		t.Error("expected pytest in all dependency scope")
	}
}
