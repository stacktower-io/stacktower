package python

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/matzehuels/stacktower/pkg/core/deps"
)

func TestPyProject_Supports(t *testing.T) {
	parser := &PyProject{}

	tests := []struct {
		name     string
		filename string
		want     bool
	}{
		{"pyproject.toml", "pyproject.toml", true},
		{"poetry.lock", "poetry.lock", false},
		{"requirements.txt", "requirements.txt", false},
		{"uv.lock", "uv.lock", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parser.Supports(tt.filename); got != tt.want {
				t.Errorf("Supports(%q) = %v, want %v", tt.filename, got, tt.want)
			}
		})
	}
}

func TestPyProject_Type(t *testing.T) {
	parser := &PyProject{}
	if got := parser.Type(); got != "pyproject.toml" {
		t.Errorf("Type() = %q, want %q", got, "pyproject.toml")
	}
}

func TestPyProject_IncludesTransitive(t *testing.T) {
	t.Run("without resolver", func(t *testing.T) {
		parser := &PyProject{}
		if got := parser.IncludesTransitive(); got {
			t.Errorf("IncludesTransitive() = %v, want false", got)
		}
	})
}

func TestPyProject_Parse_PEP621(t *testing.T) {
	// PEP 621 format with [project.dependencies]
	content := `[project]
name = "myproject"
version = "1.0.0"
dependencies = [
    "requests>=2.28.0",
    "numpy==1.24.0",
    "flask~=2.0",
]

[project.optional-dependencies]
dev = ["pytest>=7.0"]
docs = ["sphinx"]
`

	tmpDir := t.TempDir()
	pyprojectPath := filepath.Join(tmpDir, "pyproject.toml")
	if err := os.WriteFile(pyprojectPath, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	parser := &PyProject{}
	result, err := parser.Parse(pyprojectPath, deps.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if result.RootPackage != "myproject" {
		t.Errorf("result.RootPackage = %q, want %q", result.RootPackage, "myproject")
	}

	g := result.Graph

	// Check production dependencies are present
	expectedPkgs := []string{"requests", "numpy", "flask"}
	for _, pkg := range expectedPkgs {
		if _, ok := g.Node(pkg); !ok {
			t.Errorf("Expected package %q not found in graph", pkg)
		}
	}

	// Optional dependencies should NOT be included
	unexpectedPkgs := []string{"pytest", "sphinx"}
	for _, pkg := range unexpectedPkgs {
		if _, ok := g.Node(pkg); ok {
			t.Errorf("Unexpected dev/optional package %q found in graph", pkg)
		}
	}
}

func TestPyProject_Parse_Poetry(t *testing.T) {
	// Poetry format with [tool.poetry.dependencies]
	content := `[tool.poetry]
name = "mypoetryproject"
version = "0.1.0"

[tool.poetry.dependencies]
python = "^3.9"
requests = "^2.28"
numpy = {version = "^1.24", optional = true}

[tool.poetry.dev-dependencies]
pytest = "^7.0"

[tool.poetry.group.docs.dependencies]
sphinx = "^5.0"
`

	tmpDir := t.TempDir()
	pyprojectPath := filepath.Join(tmpDir, "pyproject.toml")
	if err := os.WriteFile(pyprojectPath, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	parser := &PyProject{}
	result, err := parser.Parse(pyprojectPath, deps.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if result.RootPackage != "mypoetryproject" {
		t.Errorf("result.RootPackage = %q, want %q", result.RootPackage, "mypoetryproject")
	}

	g := result.Graph

	// Check production dependencies are present
	expectedPkgs := []string{"requests", "numpy"}
	for _, pkg := range expectedPkgs {
		if _, ok := g.Node(pkg); !ok {
			t.Errorf("Expected package %q not found in graph", pkg)
		}
	}

	// Python should NOT be in the graph
	if _, ok := g.Node("python"); ok {
		t.Error("python should not be in the dependency graph")
	}

	// Dev dependencies and groups should NOT be included
	unexpectedPkgs := []string{"pytest", "sphinx"}
	for _, pkg := range unexpectedPkgs {
		if _, ok := g.Node(pkg); ok {
			t.Errorf("Unexpected dev package %q found in graph", pkg)
		}
	}
}

func TestPyProject_Parse_DependencyGroups(t *testing.T) {
	// PEP 735 format with [dependency-groups]
	content := `[project]
name = "myproject"
version = "1.0.0"
dependencies = ["requests"]

[dependency-groups]
test = ["pytest>=7.0", "coverage"]
lint = ["ruff", "mypy"]
`

	tmpDir := t.TempDir()
	pyprojectPath := filepath.Join(tmpDir, "pyproject.toml")
	if err := os.WriteFile(pyprojectPath, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	parser := &PyProject{}
	result, err := parser.Parse(pyprojectPath, deps.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	g := result.Graph

	// Check production dependencies are present
	expectedPkgs := []string{"requests"}
	for _, pkg := range expectedPkgs {
		if _, ok := g.Node(pkg); !ok {
			t.Errorf("Expected package %q not found in graph", pkg)
		}
	}

	// Dependency groups (test, lint) should NOT be included
	unexpectedPkgs := []string{"pytest", "coverage", "ruff", "mypy"}
	for _, pkg := range unexpectedPkgs {
		if _, ok := g.Node(pkg); ok {
			t.Errorf("Unexpected dependency-group package %q found in graph", pkg)
		}
	}
}

func TestPyProject_Parse_Flit(t *testing.T) {
	content := `
[build-system]
requires = ["flit"]
build-backend = "flit.buildapi"

[tool.flit.metadata]
module = "fastapi"
author = "Test Author"
requires = [
    "starlette ==0.14.2",
    "pydantic >=1.6.2,<2.0.0"
]

[tool.flit.metadata.requires-extra]
test = [
    "pytest ==5.4.3",
    "requests >=2.24.0,<3.0.0"
]
dev = [
    "uvicorn[standard] >=0.12.0,<0.14.0"
]
`

	tmpDir := t.TempDir()
	pyprojectPath := filepath.Join(tmpDir, "pyproject.toml")
	if err := os.WriteFile(pyprojectPath, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	parser := &PyProject{}
	result, err := parser.Parse(pyprojectPath, deps.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	// Check root package name
	if result.RootPackage != "fastapi" {
		t.Errorf("RootPackage = %q, want %q", result.RootPackage, "fastapi")
	}

	g := result.Graph

	// Check production dependencies are present
	expectedPkgs := []string{"starlette", "pydantic"}
	for _, pkg := range expectedPkgs {
		if _, ok := g.Node(pkg); !ok {
			t.Errorf("Expected package %q not found in graph", pkg)
		}
	}

	// requires-extra (test, dev) should NOT be included
	unexpectedPkgs := []string{"pytest", "requests", "uvicorn"}
	for _, pkg := range unexpectedPkgs {
		if _, ok := g.Node(pkg); ok {
			t.Errorf("Unexpected requires-extra package %q found in graph", pkg)
		}
	}
}

func TestParsePEP508(t *testing.T) {
	tests := []struct {
		input          string
		wantName       string
		wantConstraint string
	}{
		{"requests", "requests", ""},
		{"requests>=2.28.0", "requests", ">=2.28.0"},
		{"numpy==1.24.0", "numpy", "==1.24.0"},
		{"flask~=2.0", "flask", "~=2.0"},
		{"pytest>=7.0,<8.0", "pytest", ">=7.0,<8.0"},
		{"Django>=3.0; python_version >= '3.8'", "django", ">=3.0"},
		{"importlib-metadata; python_version < '3.8'", "importlib-metadata", ""},
		{"package[extra]>=1.0", "package", ">=1.0"},
		{"my_package-name>=1.0", "my-package-name", ">=1.0"},
		{"", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			d := parsePEP508(tt.input)
			if d.Name != tt.wantName {
				t.Errorf("parsePEP508(%q).Name = %q, want %q", tt.input, d.Name, tt.wantName)
			}
			if d.Constraint != tt.wantConstraint {
				t.Errorf("parsePEP508(%q).Constraint = %q, want %q", tt.input, d.Constraint, tt.wantConstraint)
			}
		})
	}
}

func TestExtractConstraint(t *testing.T) {
	tests := []struct {
		name  string
		input any
		want  string
	}{
		{"string constraint", "^2.28", "^2.28"},
		{"map with version", map[string]any{"version": "^1.24", "optional": true}, "^1.24"},
		{"map without version", map[string]any{"git": "https://github.com/user/repo"}, ""},
		{"nil", nil, ""},
		{"integer", 42, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extractConstraint(tt.input); got != tt.want {
				t.Errorf("extractConstraint(%v) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestExtractPyprojectDependencies(t *testing.T) {
	// Test with mixed sources
	pyproject := &pyprojectFile{
		Project: pyprojectProject{
			Name:         "test",
			Dependencies: []string{"requests>=2.0", "numpy"},
			OptionalDependencies: map[string][]string{
				"dev": {"pytest"},
			},
		},
		Tool: pyprojectTool{
			Poetry: pyprojectPoetry{
				Dependencies: map[string]any{
					"python": "^3.9", // Should be excluded
					"flask":  "^2.0",
				},
			},
		},
	}

	deps := extractPyprojectDependencies(pyproject)

	// Convert to map for easier checking
	depMap := make(map[string]string)
	for _, d := range deps {
		depMap[d.Name] = d.Constraint
	}

	// Check production deps are present
	if _, ok := depMap["requests"]; !ok {
		t.Error("Expected 'requests' in dependencies")
	}
	if _, ok := depMap["numpy"]; !ok {
		t.Error("Expected 'numpy' in dependencies")
	}
	if _, ok := depMap["flask"]; !ok {
		t.Error("Expected 'flask' in dependencies")
	}

	// Optional dependencies should NOT be included
	if _, ok := depMap["pytest"]; ok {
		t.Error("'pytest' (optional-dependency) should not be in dependencies")
	}

	// Python should be excluded
	if _, ok := depMap["python"]; ok {
		t.Error("'python' should not be in dependencies")
	}
}
