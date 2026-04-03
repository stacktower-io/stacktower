package java

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/matzehuels/stacktower/pkg/core/deps"
)

func TestPOMParser_Supports(t *testing.T) {
	parser := &POMParser{}

	tests := []struct {
		filename string
		want     bool
	}{
		{"pom.xml", true},
		{"Pom.xml", false},
		{"build.gradle", false},
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

func TestPOMParser_Parse(t *testing.T) {
	dir := t.TempDir()
	pomFile := filepath.Join(dir, "pom.xml")
	content := `<?xml version="1.0" encoding="UTF-8"?>
<project>
  <groupId>com.example</groupId>
  <artifactId>my-app</artifactId>
  <version>1.0.0</version>
  
  <dependencies>
    <dependency>
      <groupId>org.springframework</groupId>
      <artifactId>spring-core</artifactId>
      <version>5.3.0</version>
    </dependency>
    <dependency>
      <groupId>com.google.guava</groupId>
      <artifactId>guava</artifactId>
      <version>31.0-jre</version>
    </dependency>
    <dependency>
      <groupId>junit</groupId>
      <artifactId>junit</artifactId>
      <version>4.13</version>
      <scope>test</scope>
    </dependency>
    <dependency>
      <groupId>org.projectlombok</groupId>
      <artifactId>lombok</artifactId>
      <version>1.18.0</version>
      <scope>provided</scope>
    </dependency>
    <dependency>
      <groupId>org.optional</groupId>
      <artifactId>optional-dep</artifactId>
      <optional>true</optional>
    </dependency>
  </dependencies>
</project>`

	if err := os.WriteFile(pomFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	parser := &POMParser{} // No resolver = shallow parse
	result, err := parser.Parse(pomFile, deps.Options{})
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	g := result.Graph

	// Should have project root + 2 compile dependencies
	if got := g.NodeCount(); got != 3 {
		t.Errorf("NodeCount = %d, want 3", got)
	}

	// Check that compile deps are included
	for _, dep := range []string{"org.springframework:spring-core", "com.google.guava:guava"} {
		if _, ok := g.Node(dep); !ok {
			t.Errorf("expected node %q not found", dep)
		}
	}

	// Check that test/provided/optional deps are excluded
	for _, dep := range []string{"junit:junit", "org.projectlombok:lombok", "org.optional:optional-dep"} {
		if _, ok := g.Node(dep); ok {
			t.Errorf("unexpected node %q found (should be filtered)", dep)
		}
	}

	// Verify root package
	if result.RootPackage != "com.example:my-app" {
		t.Errorf("RootPackage = %q, want %q", result.RootPackage, "com.example:my-app")
	}
}

func TestPOMParser_Type(t *testing.T) {
	parser := &POMParser{}
	if got := parser.Type(); got != "pom.xml" {
		t.Errorf("Type() = %q, want %q", got, "pom.xml")
	}
}

func TestPOMParser_IncludesTransitive(t *testing.T) {
	parser := &POMParser{}
	if parser.IncludesTransitive() {
		t.Error("IncludesTransitive() = true, want false (no resolver)")
	}
}

func TestExtractDependencies(t *testing.T) {
	pom := &pomProject{
		Dependencies: []pomDependency{
			{GroupID: "org.apache", ArtifactID: "commons-lang"},
			{GroupID: "org.apache", ArtifactID: "commons-lang"}, // duplicate
			{GroupID: "junit", ArtifactID: "junit", Scope: "test"},
			{GroupID: "${project.groupId}", ArtifactID: "internal"},
		},
	}

	deps := extractDependencies(pom)
	if len(deps) != 1 {
		t.Errorf("expected 1 dep, got %d: %v", len(deps), deps)
	}
}

func TestNormalizeJavaVersion(t *testing.T) {
	tests := []struct {
		version string
		want    string
	}{
		{"1.8", "8"},
		{"1.7", "7"},
		{"11", "11"},
		{"17", "17"},
		{"21", "21"},
		{"1.8.0", "8.0"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			if got := normalizeJavaVersion(tt.version); got != tt.want {
				t.Errorf("normalizeJavaVersion(%q) = %q, want %q", tt.version, got, tt.want)
			}
		})
	}
}

func TestPOMParser_RuntimeVersion(t *testing.T) {
	dir := t.TempDir()
	pomFile := filepath.Join(dir, "pom.xml")
	content := `<?xml version="1.0" encoding="UTF-8"?>
<project>
  <groupId>com.example</groupId>
  <artifactId>my-app</artifactId>
  <version>1.0.0</version>
  
  <properties>
    <maven.compiler.source>17</maven.compiler.source>
    <maven.compiler.target>17</maven.compiler.target>
  </properties>
  
  <dependencies>
    <dependency>
      <groupId>org.springframework</groupId>
      <artifactId>spring-core</artifactId>
      <version>5.3.0</version>
    </dependency>
  </dependencies>
</project>`

	if err := os.WriteFile(pomFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	parser := &POMParser{}
	result, err := parser.Parse(pomFile, deps.Options{})
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if result.RuntimeVersion != "17" {
		t.Errorf("RuntimeVersion = %q, want %q", result.RuntimeVersion, "17")
	}
	if result.RuntimeConstraint != ">=17" {
		t.Errorf("RuntimeConstraint = %q, want %q", result.RuntimeConstraint, ">=17")
	}
}

func TestPOMParser_RuntimeVersion_Legacy18(t *testing.T) {
	dir := t.TempDir()
	pomFile := filepath.Join(dir, "pom.xml")
	content := `<?xml version="1.0" encoding="UTF-8"?>
<project>
  <groupId>com.example</groupId>
  <artifactId>my-app</artifactId>
  <version>1.0.0</version>
  
  <properties>
    <maven.compiler.source>1.8</maven.compiler.source>
    <maven.compiler.target>1.8</maven.compiler.target>
  </properties>
  
  <dependencies/>
</project>`

	if err := os.WriteFile(pomFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	parser := &POMParser{}
	result, err := parser.Parse(pomFile, deps.Options{})
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if result.RuntimeVersion != "8" {
		t.Errorf("RuntimeVersion = %q, want %q (1.8 should normalize to 8)", result.RuntimeVersion, "8")
	}
	if result.RuntimeConstraint != ">=8" {
		t.Errorf("RuntimeConstraint = %q, want %q", result.RuntimeConstraint, ">=8")
	}
}

func TestPOMParser_Parse_InvalidXML(t *testing.T) {
	dir := t.TempDir()
	pomFile := filepath.Join(dir, "pom.xml")
	content := `<project><groupId>com.example</groupId>`
	if err := os.WriteFile(pomFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	parser := &POMParser{}
	if _, err := parser.Parse(pomFile, deps.Options{}); err == nil {
		t.Fatal("expected parse error for malformed XML")
	}
}
