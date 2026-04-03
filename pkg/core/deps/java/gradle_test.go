package java

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/matzehuels/stacktower/pkg/core/deps"
)

func TestGradleParser_Supports(t *testing.T) {
	parser := &GradleParser{}
	tests := []struct {
		name string
		want bool
	}{
		{"build.gradle", true},
		{"build.gradle.kts", true},
		{"pom.xml", false},
		{"package.json", false},
		{"settings.gradle", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parser.Supports(tt.name); got != tt.want {
				t.Errorf("Supports(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func TestGradleParser_Type(t *testing.T) {
	parser := &GradleParser{}
	if got := parser.Type(); got != "build.gradle" {
		t.Errorf("Type() = %q, want %q", got, "build.gradle")
	}
}

func TestGradleParser_IncludesTransitive(t *testing.T) {
	// Without resolver
	parser := &GradleParser{}
	if parser.IncludesTransitive() {
		t.Error("IncludesTransitive() = true, want false (no resolver)")
	}
}

func TestGradleParser_Parse_GroovyDSL(t *testing.T) {
	content := `plugins {
    id 'java'
    id 'org.springframework.boot' version '3.0.0'
}

dependencies {
    implementation 'org.springframework.boot:spring-boot-starter-web:3.0.0'
    implementation 'com.google.guava:guava:31.1-jre'
    runtimeOnly 'org.postgresql:postgresql:42.5.0'
    compileOnly 'org.projectlombok:lombok:1.18.24'
    annotationProcessor 'org.projectlombok:lombok:1.18.24'
    
    // Test dependencies (should be skipped)
    testImplementation 'org.springframework.boot:spring-boot-starter-test:3.0.0'
    testRuntimeOnly 'org.junit.platform:junit-platform-launcher'
}
`
	tmpDir := t.TempDir()
	gradleFile := filepath.Join(tmpDir, "build.gradle")
	if err := os.WriteFile(gradleFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	parser := &GradleParser{}
	result, err := parser.Parse(gradleFile, deps.Options{})
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	g := result.Graph

	// Should have project root + 5 production dependencies
	expectedDeps := map[string]bool{
		"org.springframework.boot:spring-boot-starter-web": true,
		"com.google.guava:guava":                           true,
		"org.postgresql:postgresql":                        true,
		"org.projectlombok:lombok":                         true,
	}

	for dep := range expectedDeps {
		if _, ok := g.Node(dep); !ok {
			t.Errorf("expected node %q not found", dep)
		}
	}

	// Test dependencies should not be included
	testDeps := []string{
		"org.springframework.boot:spring-boot-starter-test",
		"org.junit.platform:junit-platform-launcher",
	}
	for _, dep := range testDeps {
		if _, ok := g.Node(dep); ok {
			t.Errorf("unexpected test dependency %q found (should be excluded)", dep)
		}
	}
}

func TestGradleParser_Parse_KotlinDSL(t *testing.T) {
	content := `plugins {
    kotlin("jvm") version "1.9.0"
}

dependencies {
    implementation("org.jetbrains.kotlin:kotlin-stdlib:1.9.0")
    implementation("io.ktor:ktor-server-core:2.3.0")
    implementation("io.ktor:ktor-server-netty:2.3.0")
    
    testImplementation("io.ktor:ktor-server-test-host:2.3.0")
    testImplementation("org.jetbrains.kotlin:kotlin-test:1.9.0")
}
`
	tmpDir := t.TempDir()
	gradleFile := filepath.Join(tmpDir, "build.gradle.kts")
	if err := os.WriteFile(gradleFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	parser := &GradleParser{}
	result, err := parser.Parse(gradleFile, deps.Options{})
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	g := result.Graph

	expectedDeps := []string{
		"org.jetbrains.kotlin:kotlin-stdlib",
		"io.ktor:ktor-server-core",
		"io.ktor:ktor-server-netty",
	}

	for _, dep := range expectedDeps {
		if _, ok := g.Node(dep); !ok {
			t.Errorf("expected node %q not found", dep)
		}
	}
}

func TestGradleParser_Parse_MapNotation(t *testing.T) {
	content := `dependencies {
    implementation group: 'com.google.guava', name: 'guava', version: '31.1-jre'
    implementation group: 'org.apache.commons', name: 'commons-lang3', version: '3.12.0'
}
`
	tmpDir := t.TempDir()
	gradleFile := filepath.Join(tmpDir, "build.gradle")
	if err := os.WriteFile(gradleFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	parser := &GradleParser{}
	result, err := parser.Parse(gradleFile, deps.Options{})
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	g := result.Graph

	expectedDeps := []string{
		"com.google.guava:guava",
		"org.apache.commons:commons-lang3",
	}

	for _, dep := range expectedDeps {
		if _, ok := g.Node(dep); !ok {
			t.Errorf("expected node %q not found", dep)
		}
	}
}

func TestGradleParser_Parse_SkipsProjectDeps(t *testing.T) {
	content := `dependencies {
    implementation project(':core')
    implementation project(':api')
    implementation 'com.google.guava:guava:31.1-jre'
}
`
	tmpDir := t.TempDir()
	gradleFile := filepath.Join(tmpDir, "build.gradle")
	if err := os.WriteFile(gradleFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	parser := &GradleParser{}
	result, err := parser.Parse(gradleFile, deps.Options{})
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	g := result.Graph

	// Should only have guava, not project dependencies
	if got := g.NodeCount(); got != 2 { // root + guava
		t.Errorf("NodeCount = %d, want 2 (root + guava only)", got)
	}

	if _, ok := g.Node("com.google.guava:guava"); !ok {
		t.Error("expected guava dependency not found")
	}
}

func TestGradleParser_Parse_VersionWithClassifier(t *testing.T) {
	content := `dependencies {
    implementation 'com.google.android.material:material:1.11.0@aar'
    runtimeOnly 'net.sf.docbook:docbook-xsl:1.75.2:resources@zip'
}
`
	tmpDir := t.TempDir()
	gradleFile := filepath.Join(tmpDir, "build.gradle")
	if err := os.WriteFile(gradleFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	parser := &GradleParser{}
	result, err := parser.Parse(gradleFile, deps.Options{})
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	g := result.Graph

	// Classifiers and extensions should be stripped
	expectedDeps := []string{
		"com.google.android.material:material",
		"net.sf.docbook:docbook-xsl",
	}

	for _, dep := range expectedDeps {
		if _, ok := g.Node(dep); !ok {
			t.Errorf("expected node %q not found", dep)
		}
	}
}

func TestParseGradleCoordinate(t *testing.T) {
	tests := []struct {
		coord    string
		wantName string
		wantVer  string
	}{
		{"com.google.guava:guava:31.1-jre", "com.google.guava:guava", "31.1-jre"},
		{"org.springframework:spring-core:5.3.8", "org.springframework:spring-core", "5.3.8"},
		{"com.google.android.material:material:1.11.0@aar", "com.google.android.material:material", "1.11.0"},
		{"net.sf.docbook:docbook-xsl:1.75.2:resources@zip", "net.sf.docbook:docbook-xsl", "1.75.2"},
		{"com.example:lib", "com.example:lib", ""},
		{"invalid", "", ""},
		{"com.example:$variable:1.0", "", ""}, // Should skip variable references
	}

	for _, tt := range tests {
		t.Run(tt.coord, func(t *testing.T) {
			dep := parseGradleCoordinate(tt.coord)
			if dep.Name != tt.wantName {
				t.Errorf("parseGradleCoordinate(%q).Name = %q, want %q", tt.coord, dep.Name, tt.wantName)
			}
			if dep.Pinned != tt.wantVer {
				t.Errorf("parseGradleCoordinate(%q).Pinned = %q, want %q", tt.coord, dep.Pinned, tt.wantVer)
			}
		})
	}
}

func TestGradleParser_Parse_AndroidProject(t *testing.T) {
	content := `plugins {
    id 'com.android.application'
    id 'kotlin-android'
}

android {
    compileSdk 34
}

dependencies {
    implementation 'androidx.core:core-ktx:1.12.0'
    implementation 'androidx.appcompat:appcompat:1.6.1'
    implementation 'com.google.android.material:material:1.11.0'
    
    kapt 'com.google.dagger:dagger-compiler:2.48'
    
    // Debug only
    debugImplementation 'com.squareup.leakcanary:leakcanary-android:2.12'
    
    testImplementation 'junit:junit:4.13.2'
    androidTestImplementation 'androidx.test.ext:junit:1.1.5'
}
`
	tmpDir := t.TempDir()
	gradleFile := filepath.Join(tmpDir, "build.gradle")
	if err := os.WriteFile(gradleFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	parser := &GradleParser{}
	result, err := parser.Parse(gradleFile, deps.Options{})
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	g := result.Graph

	// Production dependencies should be present
	prodDeps := []string{
		"androidx.core:core-ktx",
		"androidx.appcompat:appcompat",
		"com.google.android.material:material",
		"com.google.dagger:dagger-compiler",
	}

	for _, dep := range prodDeps {
		if _, ok := g.Node(dep); !ok {
			t.Errorf("expected production dependency %q not found", dep)
		}
	}
}

func TestExtractGradleJavaVersion(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name: "sourceCompatibility_equals",
			content: `sourceCompatibility = 17
targetCompatibility = 17`,
			want: "17",
		},
		{
			name:    "sourceCompatibility_JavaVersion",
			content: `sourceCompatibility = JavaVersion.VERSION_11`,
			want:    "11",
		},
		{
			name: "toolchain_languageVersion_of",
			content: `java {
    toolchain {
        languageVersion.set(JavaLanguageVersion.of(21))
    }
}`,
			want: "21",
		},
		{
			name: "toolchain_languageVersion_shorthand",
			content: `java {
    toolchain {
        languageVersion = JavaLanguageVersion.of(17)
    }
}`,
			want: "17",
		},
		{
			name:    "no_java_version",
			content: `dependencies { implementation 'com.example:lib:1.0' }`,
			want:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extractGradleJavaVersion(tt.content); got != tt.want {
				t.Errorf("extractGradleJavaVersion() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGradleParser_RuntimeVersion(t *testing.T) {
	content := `java {
    toolchain {
        languageVersion.set(JavaLanguageVersion.of(17))
    }
}

dependencies {
    implementation 'com.google.guava:guava:31.1-jre'
}
`
	tmpDir := t.TempDir()
	gradleFile := filepath.Join(tmpDir, "build.gradle")
	if err := os.WriteFile(gradleFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	parser := &GradleParser{}
	result, err := parser.Parse(gradleFile, deps.Options{})
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

func TestGradleParser_RuntimeVersion_SourceCompatibility(t *testing.T) {
	content := `sourceCompatibility = 11

dependencies {
    implementation 'org.springframework:spring-core:5.3.0'
}
`
	tmpDir := t.TempDir()
	gradleFile := filepath.Join(tmpDir, "build.gradle")
	if err := os.WriteFile(gradleFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	parser := &GradleParser{}
	result, err := parser.Parse(gradleFile, deps.Options{})
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if result.RuntimeVersion != "11" {
		t.Errorf("RuntimeVersion = %q, want %q", result.RuntimeVersion, "11")
	}
}
