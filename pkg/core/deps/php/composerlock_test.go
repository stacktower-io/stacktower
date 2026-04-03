package php

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/matzehuels/stacktower/pkg/core/deps"
)

func TestComposerLock_Supports(t *testing.T) {
	c := &ComposerLock{}

	tests := []struct {
		name     string
		filename string
		want     bool
	}{
		{"exact match", "composer.lock", true},
		{"case insensitive", "Composer.Lock", true},
		{"composer.json", "composer.json", false},
		{"other file", "package.json", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := c.Supports(tt.filename); got != tt.want {
				t.Errorf("Supports(%q) = %v, want %v", tt.filename, got, tt.want)
			}
		})
	}
}

func TestComposerLock_Type(t *testing.T) {
	c := &ComposerLock{}
	if got := c.Type(); got != "composer.lock" {
		t.Errorf("Type() = %q, want %q", got, "composer.lock")
	}
}

func TestComposerLock_IncludesTransitive(t *testing.T) {
	c := &ComposerLock{}
	if !c.IncludesTransitive() {
		t.Error("IncludesTransitive() = false, want true")
	}
}

func TestComposerLock_Parse(t *testing.T) {
	// Create a temp file with composer.lock content
	content := `{
    "_readme": [
        "This file locks the dependencies of your project",
        "@generated automatically"
    ],
    "packages": [
        {
            "name": "monolog/monolog",
            "version": "3.5.0",
            "description": "Sends your logs to files, sockets, etc.",
            "license": ["MIT"],
            "require": {
                "php": ">=8.1",
                "psr/log": "^2.0 || ^3.0"
            }
        },
        {
            "name": "psr/log",
            "version": "3.0.0",
            "description": "Common interface for logging libraries",
            "license": ["MIT"],
            "require": {
                "php": ">=8.0.0"
            }
        },
        {
            "name": "symfony/console",
            "version": "v6.4.3",
            "description": "Eases the creation of command line interfaces",
            "license": ["MIT"],
            "require": {
                "php": ">=8.1",
                "symfony/polyfill-mbstring": "~1.0"
            }
        },
        {
            "name": "symfony/polyfill-mbstring",
            "version": "v1.28.0",
            "description": "Symfony polyfill for mbstring",
            "license": ["MIT"],
            "require": {
                "php": ">=7.1"
            }
        }
    ],
    "packages-dev": [
        {
            "name": "phpunit/phpunit",
            "version": "10.5.9",
            "description": "The PHP Unit Testing framework",
            "license": ["BSD-3-Clause"],
            "require": {
                "php": ">=8.1"
            }
        }
    ]
}`

	tmpDir := t.TempDir()
	lockPath := filepath.Join(tmpDir, "composer.lock")
	if err := os.WriteFile(lockPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	c := &ComposerLock{}
	result, err := c.Parse(lockPath, deps.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if result.Type != "composer.lock" {
		t.Errorf("Type = %q, want %q", result.Type, "composer.lock")
	}
	if !result.IncludesTransitive {
		t.Error("IncludesTransitive = false, want true")
	}

	g := result.Graph

	// Check nodes exist with correct versions
	testCases := []struct {
		name    string
		version string
	}{
		{"monolog/monolog", "3.5.0"},
		{"psr/log", "3.0.0"},
		{"symfony/console", "6.4.3"},            // 'v' prefix should be stripped
		{"symfony/polyfill-mbstring", "1.28.0"}, // 'v' prefix should be stripped
	}

	for _, tc := range testCases {
		node, ok := g.Node(tc.name)
		if !ok {
			t.Errorf("Node %q not found", tc.name)
			continue
		}
		if v, _ := node.Meta["version"].(string); v != tc.version {
			t.Errorf("Node %q version = %q, want %q", tc.name, v, tc.version)
		}
	}

	// Check edges
	// monolog/monolog -> psr/log
	children := g.Children("monolog/monolog")
	hasPsrLog := false
	for _, child := range children {
		if child == "psr/log" {
			hasPsrLog = true
		}
	}
	if !hasPsrLog {
		t.Error("Edge monolog/monolog -> psr/log not found")
	}

	// symfony/console -> symfony/polyfill-mbstring
	children = g.Children("symfony/console")
	hasPolyfill := false
	for _, child := range children {
		if child == "symfony/polyfill-mbstring" {
			hasPolyfill = true
		}
	}
	if !hasPolyfill {
		t.Error("Edge symfony/console -> symfony/polyfill-mbstring not found")
	}

	// Dev packages are excluded in prod_only mode
	if _, ok := g.Node("phpunit/phpunit"); ok {
		t.Error("did not expect phpunit/phpunit in prod_only scope")
	}
}

func TestComposerLock_SkipsPHPRequirements(t *testing.T) {
	content := `{
    "packages": [
        {
            "name": "some/package",
            "version": "1.0.0",
            "require": {
                "php": ">=8.0",
                "ext-json": "*",
                "ext-mbstring": "*",
                "other/package": "^1.0"
            }
        },
        {
            "name": "other/package",
            "version": "1.0.0",
            "require": {
                "php-64bit": ">=8.0"
            }
        }
    ],
    "packages-dev": []
}`

	tmpDir := t.TempDir()
	lockPath := filepath.Join(tmpDir, "composer.lock")
	if err := os.WriteFile(lockPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	c := &ComposerLock{}
	result, err := c.Parse(lockPath, deps.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	g := result.Graph

	// Should NOT have nodes for php, ext-json, ext-mbstring
	for _, name := range []string{"php", "ext-json", "ext-mbstring", "php-64bit"} {
		if _, ok := g.Node(name); ok {
			t.Errorf("Should not have node for %q", name)
		}
	}

	// Should have edge some/package -> other/package
	children := g.Children("some/package")
	hasOther := false
	for _, child := range children {
		if child == "other/package" {
			hasOther = true
		}
	}
	if !hasOther {
		t.Error("Edge some/package -> other/package not found")
	}
}

func TestNormalizeComposerVersion(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"1.2.3", "1.2.3"},
		{"v1.2.3", "1.2.3"},
		{"v6.4.3", "6.4.3"},
		{"dev-main", "dev-main"},
		{"vdev-main", "vdev-main"}, // Don't strip 'v' when not followed by digit
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := normalizeComposerVersion(tt.input); got != tt.want {
				t.Errorf("normalizeComposerVersion(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestComposerLock_AllScopeIncludesDevPackages(t *testing.T) {
	content := `{
    "packages": [{"name":"prod/pkg","version":"1.0.0","require":{}}],
    "packages-dev": [{"name":"dev/pkg","version":"2.0.0","require":{}}]
}`
	tmpDir := t.TempDir()
	lockPath := filepath.Join(tmpDir, "composer.lock")
	if err := os.WriteFile(lockPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	c := &ComposerLock{}
	result, err := c.Parse(lockPath, deps.Options{DependencyScope: deps.DependencyScopeAll})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if _, ok := result.Graph.Node("dev/pkg"); !ok {
		t.Error("expected dev/pkg in all dependency scope")
	}
}
