package rust

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/BurntSushi/toml"

	"github.com/stacktower-io/stacktower/pkg/core/deps"
)

func TestCargoToml_Supports(t *testing.T) {
	parser := &CargoToml{}

	tests := []struct {
		filename string
		want     bool
	}{
		{"Cargo.toml", true},
		{"cargo.toml", true},
		{"CARGO.TOML", true},
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

func TestCargoToml_Parse(t *testing.T) {
	dir := t.TempDir()
	cargoFile := filepath.Join(dir, "Cargo.toml")
	content := `[package]
name = "my-crate"
version = "0.1.0"

[dependencies]
serde = "1.0"
tokio = { version = "1.0", features = ["full"] }

[dev-dependencies]
pretty_assertions = "1.0"
`

	if err := os.WriteFile(cargoFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	parser := &CargoToml{}
	result, err := parser.Parse(cargoFile, deps.Options{})
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	g := result.Graph

	if got := g.NodeCount(); got != 3 {
		t.Errorf("NodeCount = %d, want 3", got)
	}

	for _, dep := range []string{"serde", "tokio"} {
		if _, ok := g.Node(dep); !ok {
			t.Errorf("expected node %q not found", dep)
		}
	}
	if _, ok := g.Node("pretty_assertions"); ok {
		t.Error("did not expect dev dependency pretty_assertions in prod_only scope")
	}

	if result.RootPackage != "my-crate" {
		t.Errorf("RootPackage = %q, want %q", result.RootPackage, "my-crate")
	}

	// Verify version metadata is on the root node
	if root, ok := g.Node("__project__"); ok {
		if root.Meta["version"] != "0.1.0" {
			t.Errorf("root node version = %v, want 0.1.0", root.Meta["version"])
		}
	} else {
		t.Error("__project__ node not found")
	}
}

func TestCargoToml_Parse_TargetDependencies(t *testing.T) {
	// Platform-specific deps under [target.'cfg(...)'.dependencies] are
	// merged into the regular dependency set.
	dir := t.TempDir()
	cargoFile := filepath.Join(dir, "Cargo.toml")
	content := `[package]
name = "my-crate"
version = "0.1.0"

[dependencies]
serde = "1.0"

[target.'cfg(unix)'.dependencies]
libc = "0.2"

[target.'cfg(windows)'.dependencies]
winapi = { version = "0.3", features = ["winuser"] }

[target.'cfg(unix)'.dev-dependencies]
nix-test-helper = "0.1"
`

	if err := os.WriteFile(cargoFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	parser := &CargoToml{}
	result, err := parser.Parse(cargoFile, deps.Options{})
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	g := result.Graph
	for _, dep := range []string{"serde", "libc", "winapi"} {
		if _, ok := g.Node(dep); !ok {
			t.Errorf("expected node %q not found", dep)
		}
	}
	// Target dev-dependencies follow dev scoping rules (excluded in prod_only)
	if _, ok := g.Node("nix-test-helper"); ok {
		t.Error("did not expect target dev dependency nix-test-helper in prod_only scope")
	}
}

func TestCargoToml_Parse_WorkspaceDependencies(t *testing.T) {
	// `workspace = true` deps inherit their constraint from
	// [workspace.dependencies] in the same Cargo.toml.
	dir := t.TempDir()
	cargoPath := filepath.Join(dir, "Cargo.toml")
	content := `[package]
name = "my-crate"
version = "0.1.0"

[workspace]
members = ["."]

[workspace.dependencies]
serde = "1.0.190"
tokio = { version = "1.35", features = ["rt"] }

[dependencies]
serde = { workspace = true }
tokio = { workspace = true, features = ["macros"] }
anyhow = "1.0"
`

	if err := os.WriteFile(cargoPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(cargoPath)
	if err != nil {
		t.Fatal(err)
	}
	var cargo cargoFile
	if err := toml.Unmarshal(data, &cargo); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	result := extractCargoDepsWithVersions(cargo, deps.DependencyScopeProdOnly)
	got := make(map[string]string, len(result))
	for _, dep := range result {
		got[dep.Name] = dep.Constraint
	}

	tests := []struct {
		name           string
		wantConstraint string
	}{
		{"serde", "1.0.190"}, // inherited from string-form workspace dep
		{"tokio", "1.35"},    // inherited from table-form workspace dep
		{"anyhow", "1.0"},    // regular dep unaffected
	}
	for _, tt := range tests {
		constraint, ok := got[tt.name]
		if !ok {
			t.Errorf("dependency %q missing from result", tt.name)
			continue
		}
		if constraint != tt.wantConstraint {
			t.Errorf("dependency %q constraint = %q, want %q", tt.name, constraint, tt.wantConstraint)
		}
	}
}

func TestCargoToml_Type(t *testing.T) {
	parser := &CargoToml{}
	if got := parser.Type(); got != "Cargo.toml" {
		t.Errorf("Type() = %q, want %q", got, "Cargo.toml")
	}
}

func TestCargoToml_IncludesTransitive(t *testing.T) {
	parser := &CargoToml{}
	if parser.IncludesTransitive() {
		t.Error("IncludesTransitive() = true, want false (no resolver)")
	}
}

func TestCargoToml_RuntimeVersion(t *testing.T) {
	dir := t.TempDir()
	cargoFile := filepath.Join(dir, "Cargo.toml")
	content := `[package]
name = "my-crate"
version = "0.1.0"
rust-version = "1.70.0"

[dependencies]
serde = "1.0"
`

	if err := os.WriteFile(cargoFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	parser := &CargoToml{}
	result, err := parser.Parse(cargoFile, deps.Options{})
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if result.RuntimeVersion != "1.70.0" {
		t.Errorf("RuntimeVersion = %q, want %q", result.RuntimeVersion, "1.70.0")
	}
	if result.RuntimeConstraint != ">=1.70.0" {
		t.Errorf("RuntimeConstraint = %q, want %q", result.RuntimeConstraint, ">=1.70.0")
	}
}

func TestCargoToml_RuntimeVersion_Empty(t *testing.T) {
	dir := t.TempDir()
	cargoFile := filepath.Join(dir, "Cargo.toml")
	content := `[package]
name = "my-crate"
version = "0.1.0"

[dependencies]
serde = "1.0"
`

	if err := os.WriteFile(cargoFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	parser := &CargoToml{}
	result, err := parser.Parse(cargoFile, deps.Options{})
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if result.RuntimeVersion != "" {
		t.Errorf("RuntimeVersion = %q, want empty (no rust-version specified)", result.RuntimeVersion)
	}
}

func TestCargoToml_Parse_InvalidTOML(t *testing.T) {
	dir := t.TempDir()
	cargoFile := filepath.Join(dir, "Cargo.toml")
	content := `[package
name = "broken"`
	if err := os.WriteFile(cargoFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	parser := &CargoToml{}
	if _, err := parser.Parse(cargoFile, deps.Options{}); err == nil {
		t.Fatal("expected parse error for malformed TOML")
	}
}
