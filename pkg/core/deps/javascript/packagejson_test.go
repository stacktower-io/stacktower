package javascript

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/matzehuels/stacktower/pkg/core/deps"
)

func TestPackageJSON_Supports(t *testing.T) {
	parser := &PackageJSON{}

	tests := []struct {
		filename string
		want     bool
	}{
		{"package.json", true},
		{"Package.json", true},
		{"PACKAGE.JSON", true},
		{"Cargo.toml", false},
	}

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			if got := parser.Supports(tt.filename); got != tt.want {
				t.Errorf("Supports(%q) = %v, want %v", tt.filename, got, tt.want)
			}
		})
	}
}

func TestPackageJSON_Parse(t *testing.T) {
	dir := t.TempDir()
	pkgFile := filepath.Join(dir, "package.json")
	content := `{
  "name": "my-package",
  "version": "1.0.0",
  "dependencies": {
    "express": "^4.18.0",
    "lodash": "^4.17.21"
  },
  "devDependencies": {
    "jest": "^29.0.0"
  }
}`

	if err := os.WriteFile(pkgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	parser := &PackageJSON{}
	result, err := parser.Parse(pkgFile, deps.Options{})
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	g := result.Graph

	if got := g.NodeCount(); got != 3 {
		t.Errorf("NodeCount = %d, want 3", got)
	}

	for _, dep := range []string{"express", "lodash"} {
		if _, ok := g.Node(dep); !ok {
			t.Errorf("expected node %q not found", dep)
		}
	}
	if _, ok := g.Node("jest"); ok {
		t.Error("did not expect dev dependency jest in prod_only scope")
	}

	if result.RootPackage != "my-package" {
		t.Errorf("RootPackage = %q, want %q", result.RootPackage, "my-package")
	}

	if root, ok := g.Node("__project__"); ok {
		if root.Meta["version"] != "1.0.0" {
			t.Errorf("root node version = %v, want 1.0.0", root.Meta["version"])
		}
	} else {
		t.Error("__project__ node not found")
	}
}

func TestPackageJSON_Parse_AllScopeIncludesDevDependencies(t *testing.T) {
	dir := t.TempDir()
	pkgFile := filepath.Join(dir, "package.json")
	content := `{
  "name": "my-package",
  "dependencies": {"express": "^4.18.0"},
  "devDependencies": {"jest": "^29.0.0"}
}`
	if err := os.WriteFile(pkgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	parser := &PackageJSON{}
	result, err := parser.Parse(pkgFile, deps.Options{DependencyScope: deps.DependencyScopeAll})
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if _, ok := result.Graph.Node("jest"); !ok {
		t.Error("expected dev dependency jest in all scope")
	}
}

func TestPackageJSON_Type(t *testing.T) {
	parser := &PackageJSON{}
	if got := parser.Type(); got != "package.json" {
		t.Errorf("Type() = %q, want %q", got, "package.json")
	}
}

func TestPackageJSON_IncludesTransitive(t *testing.T) {
	parser := &PackageJSON{}
	if parser.IncludesTransitive() {
		t.Error("IncludesTransitive() = true, want false (no resolver)")
	}
}

func TestExtractNodeVersion(t *testing.T) {
	tests := []struct {
		constraint string
		want       string
	}{
		{">=18", "18"},
		{">=18.0.0", "18.0.0"},
		{"^20", "20"},
		{">=18 <21", "18"},
		{"v18", "18"},
		{"18", "18"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.constraint, func(t *testing.T) {
			if got := extractNodeVersion(tt.constraint); got != tt.want {
				t.Errorf("extractNodeVersion(%q) = %q, want %q", tt.constraint, got, tt.want)
			}
		})
	}
}

func TestPackageJSON_RuntimeVersion(t *testing.T) {
	dir := t.TempDir()
	pkgFile := filepath.Join(dir, "package.json")
	content := `{
  "name": "my-package",
  "version": "1.0.0",
  "engines": {
    "node": ">=18"
  },
  "dependencies": {
    "express": "^4.18.0"
  }
}`

	if err := os.WriteFile(pkgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	parser := &PackageJSON{}
	result, err := parser.Parse(pkgFile, deps.Options{})
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if result.RuntimeVersion != "18" {
		t.Errorf("RuntimeVersion = %q, want %q", result.RuntimeVersion, "18")
	}
	if result.RuntimeConstraint != ">=18" {
		t.Errorf("RuntimeConstraint = %q, want %q", result.RuntimeConstraint, ">=18")
	}
}

func TestPackageJSON_Parse_ToleratesMalformedEngines(t *testing.T) {
	dir := t.TempDir()
	pkgFile := filepath.Join(dir, "package.json")
	content := `{
  "name": "my-package",
  "version": "1.0.0",
  "engines": ["node >=18"],
  "dependencies": {
    "express": "^4.18.0"
  }
}`

	if err := os.WriteFile(pkgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	parser := &PackageJSON{}
	result, err := parser.Parse(pkgFile, deps.Options{})
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	// Malformed engines should be ignored, not fail parsing.
	if result.RuntimeVersion != "" {
		t.Errorf("RuntimeVersion = %q, want empty", result.RuntimeVersion)
	}
	if result.RuntimeConstraint != "" {
		t.Errorf("RuntimeConstraint = %q, want empty", result.RuntimeConstraint)
	}
}
