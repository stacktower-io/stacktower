package ruby

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/matzehuels/stacktower/pkg/core/deps"
)

func TestGemfileLock_Supports(t *testing.T) {
	g := &GemfileLock{}

	tests := []struct {
		name     string
		filename string
		want     bool
	}{
		{"exact match", "Gemfile.lock", true},
		{"Gemfile", "Gemfile", false},
		{"other file", "package.json", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := g.Supports(tt.filename); got != tt.want {
				t.Errorf("Supports(%q) = %v, want %v", tt.filename, got, tt.want)
			}
		})
	}
}

func TestGemfileLock_Type(t *testing.T) {
	g := &GemfileLock{}
	if got := g.Type(); got != "Gemfile.lock" {
		t.Errorf("Type() = %q, want %q", got, "Gemfile.lock")
	}
}

func TestGemfileLock_IncludesTransitive(t *testing.T) {
	g := &GemfileLock{}
	if !g.IncludesTransitive() {
		t.Error("IncludesTransitive() = false, want true")
	}
}

func TestGemfileLock_Parse(t *testing.T) {
	// Create a temp file with Gemfile.lock content
	content := `GEM
  remote: https://rubygems.org/
  specs:
    actioncable (7.0.8)
      actionpack (= 7.0.8)
      activesupport (= 7.0.8)
      nio4r (~> 2.0)
      websocket-driver (>= 0.6.1)
    actionpack (7.0.8)
      activesupport (= 7.0.8)
      rack (~> 2.2, >= 2.2.4)
    activesupport (7.0.8)
      concurrent-ruby (~> 1.0, >= 1.0.2)
      i18n (>= 1.6, < 2)
    concurrent-ruby (1.2.2)
    i18n (1.14.1)
      concurrent-ruby (~> 1.0)
    nio4r (2.5.9)
    rack (2.2.8)
    websocket-driver (0.7.6)
      websocket-extensions (>= 0.1.0)
    websocket-extensions (0.1.5)

PLATFORMS
  ruby
  x86_64-linux

DEPENDENCIES
  actioncable (~> 7.0)
  rack

BUNDLED WITH
   2.4.10
`

	tmpDir := t.TempDir()
	lockPath := filepath.Join(tmpDir, "Gemfile.lock")
	if err := os.WriteFile(lockPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	gl := &GemfileLock{}
	result, err := gl.Parse(lockPath, deps.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if result.Type != "Gemfile.lock" {
		t.Errorf("Type = %q, want %q", result.Type, "Gemfile.lock")
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
		{"actioncable", "7.0.8"},
		{"actionpack", "7.0.8"},
		{"activesupport", "7.0.8"},
		{"concurrent-ruby", "1.2.2"},
		{"i18n", "1.14.1"},
		{"nio4r", "2.5.9"},
		{"rack", "2.2.8"},
		{"websocket-driver", "0.7.6"},
		{"websocket-extensions", "0.1.5"},
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
	// actioncable -> actionpack
	children := g.Children("actioncable")
	hasActionpack := false
	hasActivesupport := false
	for _, child := range children {
		if child == "actionpack" {
			hasActionpack = true
		}
		if child == "activesupport" {
			hasActivesupport = true
		}
	}
	if !hasActionpack {
		t.Error("Edge actioncable -> actionpack not found")
	}
	if !hasActivesupport {
		t.Error("Edge actioncable -> activesupport not found")
	}

	// Check direct dependencies are connected to root
	rootChildren := g.Children(deps.ProjectRootNodeID)
	hasActioncable := false
	hasRack := false
	for _, child := range rootChildren {
		if child == "actioncable" {
			hasActioncable = true
		}
		if child == "rack" {
			hasRack = true
		}
	}
	if !hasActioncable {
		t.Error("Direct dependency actioncable not connected to root")
	}
	if !hasRack {
		t.Error("Direct dependency rack not connected to root")
	}
}

func TestGemfileLock_ParseGitSource(t *testing.T) {
	// Test parsing a lockfile with GIT source
	content := `GIT
  remote: https://github.com/rails/rails.git
  revision: abc123
  branch: main
  specs:
    actionpack (7.1.0)
      activesupport (= 7.1.0)

GEM
  remote: https://rubygems.org/
  specs:
    activesupport (7.1.0)

PLATFORMS
  ruby

DEPENDENCIES
  rails!

BUNDLED WITH
   2.4.10
`

	tmpDir := t.TempDir()
	lockPath := filepath.Join(tmpDir, "Gemfile.lock")
	if err := os.WriteFile(lockPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	gl := &GemfileLock{}
	result, err := gl.Parse(lockPath, deps.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	g := result.Graph

	// Check that gems from both GIT and GEM sources are parsed
	node, ok := g.Node("actionpack")
	if !ok {
		t.Error("Node 'actionpack' from GIT source not found")
	} else if v, _ := node.Meta["version"].(string); v != "7.1.0" {
		t.Errorf("actionpack version = %q, want %q", v, "7.1.0")
	}

	_, ok = g.Node("activesupport")
	if !ok {
		t.Error("Node 'activesupport' from GEM source not found")
	}

	// Check edge actionpack -> activesupport
	children := g.Children("actionpack")
	found := false
	for _, child := range children {
		if child == "activesupport" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Edge actionpack -> activesupport not found")
	}
}
