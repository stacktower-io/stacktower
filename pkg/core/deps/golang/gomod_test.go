package golang

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/matzehuels/stacktower/pkg/core/deps"
)

func TestGoModParser_Supports(t *testing.T) {
	parser := &GoModParser{}

	tests := []struct {
		filename string
		want     bool
	}{
		{"go.mod", true},
		{"Go.mod", false},
		{"go.sum", false},
		{"package.json", false},
	}

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			if got := parser.Supports(tt.filename); got != tt.want {
				t.Errorf("Supports(%q) = %v, want %v", tt.filename, got, tt.want)
			}
		})
	}
}

func TestGoModParser_Parse(t *testing.T) {
	dir := t.TempDir()
	goModFile := filepath.Join(dir, "go.mod")
	content := `module github.com/example/myapp

go 1.21

require (
	github.com/gin-gonic/gin v1.9.0
	github.com/spf13/cobra v1.7.0
	golang.org/x/sync v0.3.0 // indirect
)

require github.com/stretchr/testify v1.8.0
`

	if err := os.WriteFile(goModFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	parser := &GoModParser{} // No resolver = includes indirect deps from go.mod
	result, err := parser.Parse(goModFile, deps.Options{})
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	g := result.Graph

	// Should have project root + 3 direct dependencies + 1 indirect dependency
	if got := g.NodeCount(); got != 5 {
		t.Errorf("NodeCount = %d, want 5", got)
	}

	// Check that direct deps are included
	for _, dep := range []string{"github.com/gin-gonic/gin", "github.com/spf13/cobra", "github.com/stretchr/testify"} {
		if _, ok := g.Node(dep); !ok {
			t.Errorf("expected node %q not found", dep)
		}
	}

	// Check that indirect deps are now included (with indirect marker)
	if node, ok := g.Node("golang.org/x/sync"); ok {
		if indirect, _ := node.Meta["indirect"].(bool); !indirect {
			t.Error("golang.org/x/sync should be marked as indirect")
		}
	} else {
		t.Error("expected indirect dependency golang.org/x/sync not found")
	}

	// Verify indirect deps are connected to root (so they appear in visualization)
	indirectEdgeFound := false
	for _, child := range g.Children(deps.ProjectRootNodeID) {
		if child == "golang.org/x/sync" {
			indirectEdgeFound = true
			break
		}
	}
	if !indirectEdgeFound {
		t.Error("indirect dependency golang.org/x/sync should be connected to project root")
	}

	// When we have indirect deps, IncludesTransitive should be true
	if !result.IncludesTransitive {
		t.Error("IncludesTransitive = false, want true (has indirect deps)")
	}

	// Verify root package
	if result.RootPackage != "github.com/example/myapp" {
		t.Errorf("RootPackage = %q, want %q", result.RootPackage, "github.com/example/myapp")
	}
}

func TestGoModParser_Type(t *testing.T) {
	parser := &GoModParser{}
	if got := parser.Type(); got != "go.mod" {
		t.Errorf("Type() = %q, want %q", got, "go.mod")
	}
}

func TestGoModParser_IncludesTransitive(t *testing.T) {
	// Without resolver, IncludesTransitive returns false initially
	// (but may become true after parsing if go.mod has indirect deps)
	parser := &GoModParser{}
	if parser.IncludesTransitive() {
		t.Error("IncludesTransitive() = true, want false (no resolver set)")
	}
}

func TestParseGoModFileComplete(t *testing.T) {
	content := `module github.com/example/app

go 1.21

require (
	github.com/gin-gonic/gin v1.9.0
	github.com/gin-gonic/gin v1.9.0
	golang.org/x/sync v0.3.0 // indirect
)
`
	dir := t.TempDir()
	path := filepath.Join(dir, "go.mod")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	f, _ := os.Open(path)
	defer f.Close()

	result := parseGoModFileComplete(f)

	if result.moduleName != "github.com/example/app" {
		t.Errorf("module = %q, want github.com/example/app", result.moduleName)
	}

	// Should dedupe; indirect deps go to indirectDeps
	if len(result.directDeps) != 1 {
		t.Errorf("expected 1 direct dep, got %d: %v", len(result.directDeps), result.directDeps)
	}
	if len(result.indirectDeps) != 1 {
		t.Errorf("expected 1 indirect dep, got %d: %v", len(result.indirectDeps), result.indirectDeps)
	}
}

func TestParseRequireLineComplete_Variations(t *testing.T) {
	tests := []struct {
		line       string
		wantName   string
		wantPinned string
		wantIndir  bool
	}{
		{"github.com/pkg/errors v0.9.1", "github.com/pkg/errors", "v0.9.1", false},
		{"golang.org/x/sync v0.3.0 // indirect", "golang.org/x/sync", "v0.3.0", true},
		{"  github.com/spf13/cobra v1.7.0  ", "github.com/spf13/cobra", "v1.7.0", false},
		{"github.com/example/pkg v1.0.0 // some other comment", "github.com/example/pkg", "v1.0.0", false},
		{"", "", "", false},
		{"   ", "", "", false},
	}

	for _, tt := range tests {
		name := tt.line
		if name == "" {
			name = "empty"
		} else if strings.TrimSpace(name) == "" {
			name = "whitespace"
		}
		t.Run(name, func(t *testing.T) {
			dep, isIndirect := parseRequireLineComplete(tt.line)
			if dep.Name != tt.wantName {
				t.Errorf("parseRequireLineComplete(%q).Name = %q, want %q", tt.line, dep.Name, tt.wantName)
			}
			if dep.Pinned != tt.wantPinned {
				t.Errorf("parseRequireLineComplete(%q).Pinned = %q, want %q", tt.line, dep.Pinned, tt.wantPinned)
			}
			if isIndirect != tt.wantIndir {
				t.Errorf("parseRequireLineComplete(%q) indirect = %v, want %v", tt.line, isIndirect, tt.wantIndir)
			}
		})
	}
}

func TestFilterGoModRuntimeDeps_UsesRuntimeModuleSet(t *testing.T) {
	orig := runtimeGoModulesFn
	runtimeGoModulesFn = func(context.Context, string) (map[string]bool, error) {
		return map[string]bool{
			"github.com/gin-gonic/gin": true,
		}, nil
	}
	defer func() { runtimeGoModulesFn = orig }()

	direct := []deps.Dependency{
		{Name: "github.com/gin-gonic/gin"},
		{Name: "github.com/stretchr/testify"},
	}
	filteredDirect, filteredIndirect := filterGoModRuntimeDeps("/tmp/go.mod", direct, nil, deps.Options{})
	if len(filteredIndirect) != 0 {
		t.Fatalf("expected no indirect deps, got %d", len(filteredIndirect))
	}
	if len(filteredDirect) != 1 || filteredDirect[0].Name != "github.com/gin-gonic/gin" {
		t.Fatalf("unexpected filtered direct deps: %#v", filteredDirect)
	}
}

func TestFilterGoModRuntimeDeps_FallbackOnGoListError(t *testing.T) {
	orig := runtimeGoModulesFn
	runtimeGoModulesFn = func(context.Context, string) (map[string]bool, error) {
		return nil, errors.New("go list failed")
	}
	defer func() { runtimeGoModulesFn = orig }()

	direct := []deps.Dependency{{Name: "github.com/gin-gonic/gin"}}
	indirect := []deps.Dependency{{Name: "golang.org/x/sync"}}
	filteredDirect, filteredIndirect := filterGoModRuntimeDeps("/tmp/go.mod", direct, indirect, deps.Options{})
	if len(filteredDirect) != len(direct) || len(filteredIndirect) != len(indirect) {
		t.Fatal("expected fallback to keep original dependency sets")
	}
}

func TestGoModParser_RuntimeVersion(t *testing.T) {
	dir := t.TempDir()
	goModFile := filepath.Join(dir, "go.mod")
	content := `module github.com/example/myapp

go 1.21

require github.com/gin-gonic/gin v1.9.0
`

	if err := os.WriteFile(goModFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	parser := &GoModParser{}
	result, err := parser.Parse(goModFile, deps.Options{})
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if result.RootPackage != "github.com/example/myapp" {
		t.Errorf("RootPackage = %q, want %q", result.RootPackage, "github.com/example/myapp")
	}
	if result.RuntimeVersion != "1.21" {
		t.Errorf("RuntimeVersion = %q, want %q", result.RuntimeVersion, "1.21")
	}
	if result.RuntimeConstraint != ">=1.21" {
		t.Errorf("RuntimeConstraint = %q, want %q", result.RuntimeConstraint, ">=1.21")
	}
}

func TestGoModParser_RuntimeVersion_NotSpecified(t *testing.T) {
	dir := t.TempDir()
	goModFile := filepath.Join(dir, "go.mod")
	content := `module github.com/example/myapp

require github.com/gin-gonic/gin v1.9.0
`

	if err := os.WriteFile(goModFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	parser := &GoModParser{}
	result, err := parser.Parse(goModFile, deps.Options{})
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if result.RootPackage != "github.com/example/myapp" {
		t.Errorf("RootPackage = %q, want %q", result.RootPackage, "github.com/example/myapp")
	}
	// When go directive is not specified, RuntimeVersion should be empty
	if result.RuntimeVersion != "" {
		t.Errorf("RuntimeVersion = %q, want empty (no go directive)", result.RuntimeVersion)
	}
}
