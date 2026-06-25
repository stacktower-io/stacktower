package python

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stacktower-io/stacktower/pkg/core/deps"
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

func TestRequirements_Parse_LineContinuations(t *testing.T) {
	dir := t.TempDir()
	reqFile := filepath.Join(dir, "requirements.txt")
	content := `requests \
    >=2.28.0, \
    <3.0
click==8.1.0
`
	if err := os.WriteFile(reqFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	dependencies, err := parseRequirementsFile(reqFile)
	if err != nil {
		t.Fatalf("parseRequirementsFile failed: %v", err)
	}

	got := make(map[string]string, len(dependencies))
	for _, dep := range dependencies {
		got[dep.Name] = dep.Constraint
	}
	if constraint, ok := got["requests"]; !ok {
		t.Error("expected requests from continued line")
	} else if constraint != ">=2.28.0, <3.0" {
		t.Errorf("requests constraint = %q, want %q", constraint, ">=2.28.0, <3.0")
	}
	if _, ok := got["click"]; !ok {
		t.Error("expected click after continued line")
	}
}

func TestRequirements_Parse_Includes(t *testing.T) {
	dir := t.TempDir()
	baseFile := filepath.Join(dir, "base.txt")
	if err := os.WriteFile(baseFile, []byte("flask>=2.0\nrequests==2.28.0\n"), 0644); err != nil {
		t.Fatal(err)
	}
	reqFile := filepath.Join(dir, "requirements.txt")
	content := `-r base.txt
click==8.1.0
--requirement base.txt
`
	if err := os.WriteFile(reqFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	dependencies, err := parseRequirementsFile(reqFile)
	if err != nil {
		t.Fatalf("parseRequirementsFile failed: %v", err)
	}

	names := make(map[string]bool, len(dependencies))
	for _, dep := range dependencies {
		if names[dep.Name] {
			t.Errorf("duplicate dependency %q (include followed twice without dedup)", dep.Name)
		}
		names[dep.Name] = true
	}
	for _, want := range []string{"flask", "requests", "click"} {
		if !names[want] {
			t.Errorf("expected dependency %q not found", want)
		}
	}
}

func TestRequirements_Parse_IncludeCycle(t *testing.T) {
	dir := t.TempDir()
	aFile := filepath.Join(dir, "a.txt")
	bFile := filepath.Join(dir, "b.txt")
	if err := os.WriteFile(aFile, []byte("-r b.txt\nflask>=2.0\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bFile, []byte("-r a.txt\nclick==8.1.0\n"), 0644); err != nil {
		t.Fatal(err)
	}

	dependencies, err := parseRequirementsFile(aFile)
	if err != nil {
		t.Fatalf("parseRequirementsFile failed (cycle not handled?): %v", err)
	}

	names := make(map[string]bool, len(dependencies))
	for _, dep := range dependencies {
		names[dep.Name] = true
	}
	for _, want := range []string{"flask", "click"} {
		if !names[want] {
			t.Errorf("expected dependency %q not found", want)
		}
	}
}

func TestRequirements_Parse_MissingIncludeIgnored(t *testing.T) {
	dir := t.TempDir()
	reqFile := filepath.Join(dir, "requirements.txt")
	if err := os.WriteFile(reqFile, []byte("-r missing.txt\nclick==8.1.0\n"), 0644); err != nil {
		t.Fatal(err)
	}

	dependencies, err := parseRequirementsFile(reqFile)
	if err != nil {
		t.Fatalf("parseRequirementsFile failed: %v", err)
	}
	if len(dependencies) != 1 || dependencies[0].Name != "click" {
		t.Errorf("dependencies = %v, want just click", dependencies)
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
