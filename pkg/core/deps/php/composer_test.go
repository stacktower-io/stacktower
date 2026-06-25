package php

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stacktower-io/stacktower/pkg/core/dag"
	"github.com/stacktower-io/stacktower/pkg/core/deps"
)

func TestComposerJSON_Supports(t *testing.T) {
	parser := &ComposerJSON{}

	tests := []struct {
		filename string
		want     bool
	}{
		{"composer.json", true},
		{"Composer.json", true},
		{"COMPOSER.JSON", true},
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

func TestComposerJSON_Parse(t *testing.T) {
	dir := t.TempDir()
	composerFile := filepath.Join(dir, "composer.json")
	content := `{
  "name": "vendor/my-package",
  "version": "1.0.0",
  "require": {
    "php": "^8.1",
    "ext-json": "*",
    "monolog/monolog": "^3.0",
    "symfony/console": "^6.0"
  },
  "require-dev": {
    "phpunit/phpunit": "^10.0"
  }
}`

	if err := os.WriteFile(composerFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	parser := &ComposerJSON{}
	result, err := parser.Parse(composerFile, deps.Options{})
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	g := result.Graph

	if got := g.NodeCount(); got != 3 {
		t.Errorf("NodeCount = %d, want 3", got)
	}

	for _, dep := range []string{"monolog/monolog", "symfony/console"} {
		if _, ok := g.Node(dep); !ok {
			t.Errorf("expected node %q not found", dep)
		}
	}
	if _, ok := g.Node("phpunit/phpunit"); ok {
		t.Error("did not expect require-dev dependency phpunit/phpunit in prod_only scope")
	}

	if _, ok := g.Node("php"); ok {
		t.Error("unexpected node 'php' found (should be filtered)")
	}
	if _, ok := g.Node("ext-json"); ok {
		t.Error("unexpected node 'ext-json' found (should be filtered)")
	}

	if result.RootPackage != "vendor/my-package" {
		t.Errorf("RootPackage = %q, want %q", result.RootPackage, "vendor/my-package")
	}

	if root, ok := g.Node("__project__"); ok {
		if root.Meta["version"] != "1.0.0" {
			t.Errorf("root node version = %v, want 1.0.0", root.Meta["version"])
		}
	} else {
		t.Error("__project__ node not found")
	}
}

func TestComposerJSON_Type(t *testing.T) {
	parser := &ComposerJSON{}
	if got := parser.Type(); got != "composer.json" {
		t.Errorf("Type() = %q, want %q", got, "composer.json")
	}
}

func TestComposerJSON_IncludesTransitive(t *testing.T) {
	parser := &ComposerJSON{}
	if parser.IncludesTransitive() {
		t.Error("IncludesTransitive() = true, want false (no resolver)")
	}
}

func TestExtractPHPVersion(t *testing.T) {
	tests := []struct {
		constraint string
		want       string
	}{
		{">=8.1", "8.1"},
		{"^8.0", "8.0"},
		{"~8.2", "8.2"},
		{">=8.1,<9.0", "8.1"},
		{">=8.1 <9.0", "8.1"},
		{"8.2", "8.2"},
		{"8.2.0", "8.2.0"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.constraint, func(t *testing.T) {
			if got := extractPHPVersion(tt.constraint); got != tt.want {
				t.Errorf("extractPHPVersion(%q) = %q, want %q", tt.constraint, got, tt.want)
			}
		})
	}
}

func TestComposerJSON_RuntimeVersion(t *testing.T) {
	dir := t.TempDir()
	composerFile := filepath.Join(dir, "composer.json")
	content := `{
  "name": "vendor/my-package",
  "require": {
    "php": ">=8.1",
    "monolog/monolog": "^3.0"
  }
}`

	if err := os.WriteFile(composerFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	parser := &ComposerJSON{}
	result, err := parser.Parse(composerFile, deps.Options{})
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if result.RuntimeVersion != "8.1" {
		t.Errorf("RuntimeVersion = %q, want %q", result.RuntimeVersion, "8.1")
	}
	if result.RuntimeConstraint != ">=8.1" {
		t.Errorf("RuntimeConstraint = %q, want %q", result.RuntimeConstraint, ">=8.1")
	}
}

// cancelledResolver simulates a resolver that encounters a cancelled context.
type cancelledResolver struct{}

func (f *cancelledResolver) Resolve(ctx context.Context, _ string, _ deps.Options) (*dag.DAG, error) {
	return nil, context.Canceled
}

func (f *cancelledResolver) Name() string { return "test" }

func TestComposerJSON_FallbackOnResolutionFailure(t *testing.T) {
	dir := t.TempDir()
	composerFile := filepath.Join(dir, "composer.json")
	content := `{
  "name": "composer/composer",
  "require": {
    "php": "^7.2.5 || ^8.0",
    "composer/ca-bundle": "^1.5",
    "composer/semver": "^3.3",
    "symfony/console": "^5.4.47 || ^6.4.25 || ^7.1.10 || ^8.0",
    "psr/log": "^1.0 || ^2.0 || ^3.0"
  }
}`
	if err := os.WriteFile(composerFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// Use a cancelled context to trigger the fallback path
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	parser := &ComposerJSON{resolver: &cancelledResolver{}}
	result, err := parser.Parse(composerFile, deps.Options{Ctx: ctx})
	if err != nil {
		t.Fatalf("Parse should not error on resolution failure, got: %v", err)
	}

	if result.IncludesTransitive {
		t.Error("IncludesTransitive should be false when resolution failed")
	}

	g := result.Graph
	nodes := g.Nodes()
	// 4 non-php deps + __project__ root = 5 nodes
	if len(nodes) < 5 {
		t.Errorf("Expected at least 5 nodes (direct deps + root), got %d", len(nodes))
	}

	// Verify direct deps are present as nodes
	for _, name := range []string{"composer/ca-bundle", "composer/semver", "symfony/console", "psr/log"} {
		if _, ok := g.Node(name); !ok {
			t.Errorf("Expected direct dep %q as node in fallback graph", name)
		}
	}

	// PHP should not appear as a node
	if _, ok := g.Node("php"); ok {
		t.Error("php should not be a node in the graph")
	}
}
