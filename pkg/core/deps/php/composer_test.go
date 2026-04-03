package php

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/matzehuels/stacktower/pkg/core/deps"
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
