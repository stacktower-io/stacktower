package python

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/matzehuels/stacktower/pkg/core/deps"
)

func TestRequirements_Supports(t *testing.T) {
	parser := &Requirements{}

	tests := []struct {
		filename string
		want     bool
	}{
		{"requirements.txt", true},
		{"requirements-dev.txt", true},
		{"requirements_prod.txt", true},
		{"requirements-test.txt", true},
		{"pyproject.toml", false},
		{"poetry.lock", false},
		{"Pipfile", false},
	}

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			if got := parser.Supports(tt.filename); got != tt.want {
				t.Errorf("Supports(%q) = %v, want %v", tt.filename, got, tt.want)
			}
		})
	}
}

func TestRequirements_Parse(t *testing.T) {
	dir := t.TempDir()
	reqFile := filepath.Join(dir, "requirements.txt")
	content := `# Test requirements
requests>=2.28.0
click==8.1.0
pydantic>=2.0
# Comment line
httpx

# Empty lines above

-e ./local-package  # editable, should be skipped
git+https://github.com/user/repo.git  # git URL, should be skipped
`
	if err := os.WriteFile(reqFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	parser := &Requirements{}
	result, err := parser.Parse(reqFile, deps.Options{})
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	g := result.Graph

	if got := g.NodeCount(); got != 5 {
		t.Errorf("NodeCount = %d, want 5", got)
	}

	for _, pkg := range []string{"requests", "click", "pydantic", "httpx"} {
		if _, ok := g.Node(pkg); !ok {
			t.Errorf("expected node %q not found", pkg)
		}
	}

	edges := g.Children("__project__")
	if len(edges) != 4 {
		t.Errorf("__project__ has %d children, want 4", len(edges))
	}
}

func TestRequirements_Type(t *testing.T) {
	parser := &Requirements{}
	if got := parser.Type(); got != "requirements.txt" {
		t.Errorf("Type() = %q, want %q", got, "requirements.txt")
	}
}

func TestRequirements_IncludesTransitive(t *testing.T) {
	parser := &Requirements{}
	if parser.IncludesTransitive() {
		t.Error("IncludesTransitive() = true, want false")
	}
}
