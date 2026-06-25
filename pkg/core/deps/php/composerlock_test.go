package php

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stacktower-io/stacktower/pkg/core/deps"
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

func TestComposerLock_ComposerComposer(t *testing.T) {
	lockContent := `{
    "packages": [
        {
            "name": "composer/ca-bundle",
            "version": "1.5.12",
            "description": "Lets you find a path to the system CA bundle",
            "license": ["MIT"],
            "require": {}
        },
        {
            "name": "composer/class-map-generator",
            "version": "1.6.2",
            "description": "Utilities to scan PHP namespaces",
            "license": ["MIT"],
            "require": {"composer/pcre": "^2.3 || ^3.3"}
        },
        {
            "name": "composer/metadata-minifier",
            "version": "1.0.0",
            "description": "Small utility library that handles metadata minification and expansion",
            "license": ["MIT"],
            "require": {}
        },
        {
            "name": "composer/pcre",
            "version": "3.4.0",
            "description": "PCRE wrapping library that offers type-safe preg_* replacements",
            "license": ["MIT"],
            "require": {}
        },
        {
            "name": "composer/semver",
            "version": "3.4.4",
            "description": "Semver library to handle versioning",
            "license": ["MIT"],
            "require": {}
        },
        {
            "name": "composer/spdx-licenses",
            "version": "1.6.0",
            "description": "SPDX licenses list and validation library",
            "license": ["MIT"],
            "require": {}
        },
        {
            "name": "composer/xdebug-handler",
            "version": "3.0.5",
            "description": "Restarts a process without Xdebug",
            "license": ["MIT"],
            "require": {"composer/pcre": "^1 || ^2 || ^3", "psr/log": "^1 || ^2 || ^3"}
        },
        {
            "name": "justinrainbow/json-schema",
            "version": "v6.10.0",
            "description": "A library to validate a json schema",
            "license": ["MIT"],
            "require": {}
        },
        {
            "name": "psr/log",
            "version": "3.0.2",
            "description": "Common interface for logging libraries",
            "license": ["MIT"],
            "require": {}
        },
        {
            "name": "react/promise",
            "version": "v3.3.0",
            "description": "A lightweight implementation of CommonJS Promises/A for PHP",
            "license": ["MIT"],
            "require": {}
        },
        {
            "name": "seld/jsonlint",
            "version": "1.12.1",
            "description": "JSON Linter",
            "license": ["MIT"],
            "require": {}
        },
        {
            "name": "seld/phar-utils",
            "version": "1.2.1",
            "description": "PHAR file format utilities",
            "license": ["MIT"],
            "require": {}
        },
        {
            "name": "seld/signal-handler",
            "version": "2.0.2",
            "description": "Simple unix and windows signal handler",
            "license": ["MIT"],
            "require": {"psr/log": "^1 || ^2 || ^3"}
        },
        {
            "name": "symfony/console",
            "version": "v7.2.3",
            "description": "Eases the creation of beautiful and testable command line interfaces",
            "license": ["MIT"],
            "require": {
                "symfony/string": "^6.4 || ^7.0",
                "symfony/service-contracts": "^2.5 || ^3"
            }
        },
        {
            "name": "symfony/filesystem",
            "version": "v7.2.0",
            "description": "Provides basic utilities for the filesystem",
            "license": ["MIT"],
            "require": {"symfony/polyfill-ctype": "~1.8"}
        },
        {
            "name": "symfony/finder",
            "version": "v7.2.2",
            "description": "Finds files and directories via an intuitive fluent interface",
            "license": ["MIT"],
            "require": {}
        },
        {
            "name": "symfony/polyfill-ctype",
            "version": "v1.37.0",
            "description": "Symfony polyfill for ctype functions",
            "license": ["MIT"],
            "require": {}
        },
        {
            "name": "symfony/polyfill-php73",
            "version": "v1.37.0",
            "description": "Symfony polyfill backporting some PHP 7.3+ features",
            "license": ["MIT"],
            "require": {}
        },
        {
            "name": "symfony/polyfill-php80",
            "version": "v1.37.0",
            "description": "Symfony polyfill backporting some PHP 8.0+ features",
            "license": ["MIT"],
            "require": {}
        },
        {
            "name": "symfony/polyfill-php81",
            "version": "v1.38.1",
            "description": "Symfony polyfill backporting some PHP 8.1+ features",
            "license": ["MIT"],
            "require": {}
        },
        {
            "name": "symfony/polyfill-php84",
            "version": "v1.38.1",
            "description": "Symfony polyfill backporting some PHP 8.4+ features",
            "license": ["MIT"],
            "require": {}
        },
        {
            "name": "symfony/process",
            "version": "v7.2.3",
            "description": "Executes commands in sub-processes",
            "license": ["MIT"],
            "require": {}
        },
        {
            "name": "symfony/service-contracts",
            "version": "v3.7.0",
            "description": "Generic abstractions related to writing services",
            "license": ["MIT"],
            "require": {}
        },
        {
            "name": "symfony/string",
            "version": "v7.2.0",
            "description": "Provides an object-oriented API to strings",
            "license": ["MIT"],
            "require": {"symfony/polyfill-ctype": "~1.8"}
        }
    ],
    "packages-dev": [],
    "platform": {"php": "^7.2.5 || ^8.0"}
}`

	composerJSON := `{"name": "composer/composer", "require": {"php": "^7.2.5 || ^8.0"}}`

	tmpDir := t.TempDir()
	lockPath := filepath.Join(tmpDir, "composer.lock")
	if err := os.WriteFile(lockPath, []byte(lockContent), 0644); err != nil {
		t.Fatal(err)
	}
	jsonPath := filepath.Join(tmpDir, "composer.json")
	if err := os.WriteFile(jsonPath, []byte(composerJSON), 0644); err != nil {
		t.Fatal(err)
	}

	c := &ComposerLock{}
	result, err := c.Parse(lockPath, deps.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if !result.IncludesTransitive {
		t.Error("IncludesTransitive should be true for lockfile")
	}
	if result.RootPackage != "composer/composer" {
		t.Errorf("RootPackage = %q, want %q", result.RootPackage, "composer/composer")
	}

	g := result.Graph
	nodes := g.Nodes()
	// Expect 24 packages + __project__ root = 25 nodes
	if len(nodes) < 20 {
		t.Errorf("Expected at least 20 nodes, got %d", len(nodes))
	}

	expectedPackages := []string{
		"composer/ca-bundle",
		"composer/class-map-generator",
		"composer/pcre",
		"composer/semver",
		"symfony/console",
		"symfony/filesystem",
		"symfony/finder",
		"symfony/process",
		"react/promise",
		"psr/log",
		"seld/jsonlint",
	}
	for _, name := range expectedPackages {
		if _, ok := g.Node(name); !ok {
			t.Errorf("Expected node %q not found", name)
		}
	}

	// Verify edges for transitive deps
	consoleChildren := g.Children("symfony/console")
	hasString := false
	for _, c := range consoleChildren {
		if c == "symfony/string" {
			hasString = true
		}
	}
	if !hasString {
		t.Error("Expected edge symfony/console -> symfony/string")
	}

	// Verify version normalization
	if node, ok := g.Node("symfony/console"); ok {
		if v, _ := node.Meta["version"].(string); v != "7.2.3" {
			t.Errorf("symfony/console version = %q, want %q", v, "7.2.3")
		}
	}

	// Verify runtime info
	if result.RuntimeConstraint != "^7.2.5 || ^8.0" {
		t.Errorf("RuntimeConstraint = %q, want %q", result.RuntimeConstraint, "^7.2.5 || ^8.0")
	}
}
