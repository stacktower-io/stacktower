package javascript

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/matzehuels/stacktower/pkg/core/deps"
)

func TestPackageLock_Supports(t *testing.T) {
	p := &PackageLock{}

	tests := []struct {
		name     string
		filename string
		want     bool
	}{
		{"exact match", "package-lock.json", true},
		{"case insensitive", "Package-Lock.JSON", true},
		{"package.json", "package.json", false},
		{"other file", "yarn.lock", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := p.Supports(tt.filename); got != tt.want {
				t.Errorf("Supports(%q) = %v, want %v", tt.filename, got, tt.want)
			}
		})
	}
}

func TestPackageLock_Type(t *testing.T) {
	p := &PackageLock{}
	if got := p.Type(); got != "package-lock.json" {
		t.Errorf("Type() = %q, want %q", got, "package-lock.json")
	}
}

func TestPackageLock_IncludesTransitive(t *testing.T) {
	p := &PackageLock{}
	if !p.IncludesTransitive() {
		t.Error("IncludesTransitive() = false, want true")
	}
}

func TestPackageLock_ParseV3(t *testing.T) {
	// Create a temp file with v3 format
	content := `{
  "name": "my-app",
  "version": "1.0.0",
  "lockfileVersion": 3,
  "packages": {
    "": {
      "name": "my-app",
      "version": "1.0.0",
      "dependencies": {
        "lodash": "^4.17.21"
      }
    },
    "node_modules/lodash": {
      "version": "4.17.21",
      "resolved": "https://registry.npmjs.org/lodash/-/lodash-4.17.21.tgz",
      "license": "MIT"
    },
    "node_modules/express": {
      "version": "4.18.2",
      "resolved": "https://registry.npmjs.org/express/-/express-4.18.2.tgz",
      "license": "MIT",
      "dependencies": {
        "body-parser": "1.20.1"
      }
    },
    "node_modules/body-parser": {
      "version": "1.20.1",
      "resolved": "https://registry.npmjs.org/body-parser/-/body-parser-1.20.1.tgz",
      "license": "MIT"
    }
  }
}`

	tmpDir := t.TempDir()
	lockPath := filepath.Join(tmpDir, "package-lock.json")
	if err := os.WriteFile(lockPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	p := &PackageLock{}
	result, err := p.Parse(lockPath, deps.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if result.Type != "package-lock.json" {
		t.Errorf("Type = %q, want %q", result.Type, "package-lock.json")
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
		{"lodash", "4.17.21"},
		{"express", "4.18.2"},
		{"body-parser", "1.20.1"},
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

	// Check that express -> body-parser edge exists
	children := g.Children("express")
	found := false
	for _, child := range children {
		if child == "body-parser" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Edge express -> body-parser not found")
	}
}

func TestPackageLock_ParseV1(t *testing.T) {
	// Create a temp file with v1 format
	content := `{
  "name": "my-app",
  "version": "1.0.0",
  "lockfileVersion": 1,
  "dependencies": {
    "lodash": {
      "version": "4.17.21",
      "resolved": "https://registry.npmjs.org/lodash/-/lodash-4.17.21.tgz"
    },
    "express": {
      "version": "4.18.2",
      "resolved": "https://registry.npmjs.org/express/-/express-4.18.2.tgz",
      "requires": {
        "body-parser": "1.20.1"
      }
    },
    "body-parser": {
      "version": "1.20.1",
      "resolved": "https://registry.npmjs.org/body-parser/-/body-parser-1.20.1.tgz"
    }
  }
}`

	tmpDir := t.TempDir()
	lockPath := filepath.Join(tmpDir, "package-lock.json")
	if err := os.WriteFile(lockPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	p := &PackageLock{}
	result, err := p.Parse(lockPath, deps.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	g := result.Graph

	// Check nodes exist with correct versions
	testCases := []struct {
		name    string
		version string
	}{
		{"lodash", "4.17.21"},
		{"express", "4.18.2"},
		{"body-parser", "1.20.1"},
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

	// Check that express -> body-parser edge exists
	children := g.Children("express")
	found := false
	for _, child := range children {
		if child == "body-parser" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Edge express -> body-parser not found")
	}
}

func TestPackageLock_ScopedPackages(t *testing.T) {
	content := `{
  "name": "my-app",
  "version": "1.0.0",
  "lockfileVersion": 3,
  "packages": {
    "": {
      "name": "my-app",
      "version": "1.0.0",
      "dependencies": {
        "@types/node": "^18.0.0"
      }
    },
    "node_modules/@types/node": {
      "version": "18.15.0",
      "resolved": "https://registry.npmjs.org/@types/node/-/node-18.15.0.tgz",
      "license": "MIT"
    }
  }
}`

	tmpDir := t.TempDir()
	lockPath := filepath.Join(tmpDir, "package-lock.json")
	if err := os.WriteFile(lockPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	p := &PackageLock{}
	result, err := p.Parse(lockPath, deps.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	g := result.Graph

	// Check scoped package exists
	node, ok := g.Node("@types/node")
	if !ok {
		t.Error("Scoped package @types/node not found")
		return
	}
	if v, _ := node.Meta["version"].(string); v != "18.15.0" {
		t.Errorf("Version = %q, want %q", v, "18.15.0")
	}
}

func TestExtractPackageName(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"node_modules/lodash", "lodash"},
		{"node_modules/@types/node", "@types/node"},
		{"node_modules/foo/node_modules/bar", "bar"},
		{"node_modules/@scope/pkg/node_modules/@other/lib", "@other/lib"},
		{"not-node-modules/foo", ""},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := extractPackageName(tt.path); got != tt.want {
				t.Errorf("extractPackageName(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestPackageLock_ProdOnlyExcludesDevNodes(t *testing.T) {
	content := `{
  "name": "my-app",
  "lockfileVersion": 3,
  "packages": {
    "": { "name": "my-app", "dependencies": { "lodash": "^4.17.21" } },
    "node_modules/lodash": { "version": "4.17.21" },
    "node_modules/jest": { "version": "29.0.0", "dev": true }
  }
}`
	tmpDir := t.TempDir()
	lockPath := filepath.Join(tmpDir, "package-lock.json")
	if err := os.WriteFile(lockPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	p := &PackageLock{}
	result, err := p.Parse(lockPath, deps.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if _, ok := result.Graph.Node("jest"); ok {
		t.Error("did not expect dev dependency jest in prod_only scope")
	}
}

func TestPackageLock_RuntimeVersion(t *testing.T) {
	lockContent := `{
  "name": "my-app",
  "lockfileVersion": 3,
  "packages": {
    "": { "name": "my-app", "dependencies": { "lodash": "^4.17.21" } },
    "node_modules/lodash": { "version": "4.17.21" }
  }
}`
	packageContent := `{
  "name": "my-app",
  "version": "1.0.0",
  "engines": {
    "node": ">=18.0.0"
  },
  "dependencies": {
    "lodash": "^4.17.21"
  }
}`
	tmpDir := t.TempDir()
	lockPath := filepath.Join(tmpDir, "package-lock.json")
	if err := os.WriteFile(lockPath, []byte(lockContent), 0644); err != nil {
		t.Fatal(err)
	}
	pkgPath := filepath.Join(tmpDir, "package.json")
	if err := os.WriteFile(pkgPath, []byte(packageContent), 0644); err != nil {
		t.Fatal(err)
	}

	p := &PackageLock{}
	result, err := p.Parse(lockPath, deps.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if result.RootPackage != "my-app" {
		t.Errorf("RootPackage = %q, want %q", result.RootPackage, "my-app")
	}
	if result.RuntimeVersion != "18.0.0" {
		t.Errorf("RuntimeVersion = %q, want %q", result.RuntimeVersion, "18.0.0")
	}
	if result.RuntimeConstraint != ">=18.0.0" {
		t.Errorf("RuntimeConstraint = %q, want %q", result.RuntimeConstraint, ">=18.0.0")
	}
}

func TestPackageLock_RuntimeVersion_NoPackageJSON(t *testing.T) {
	lockContent := `{
  "name": "my-app",
  "lockfileVersion": 3,
  "packages": {
    "": { "name": "my-app" },
    "node_modules/lodash": { "version": "4.17.21" }
  }
}`
	tmpDir := t.TempDir()
	lockPath := filepath.Join(tmpDir, "package-lock.json")
	if err := os.WriteFile(lockPath, []byte(lockContent), 0644); err != nil {
		t.Fatal(err)
	}

	p := &PackageLock{}
	result, err := p.Parse(lockPath, deps.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	// Without package.json, runtime fields should be empty
	if result.RuntimeVersion != "" {
		t.Errorf("RuntimeVersion = %q, want empty", result.RuntimeVersion)
	}
	if result.RuntimeConstraint != "" {
		t.Errorf("RuntimeConstraint = %q, want empty", result.RuntimeConstraint)
	}
}
