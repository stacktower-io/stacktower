package cli

import (
	"errors"
	"strings"
	"testing"

	"github.com/stacktower-io/stacktower/pkg/core/dag"
	"github.com/stacktower-io/stacktower/pkg/core/deps/languages"
)

func TestLooksLikeFile(t *testing.T) {
	// Test cases based on actual language definitions in pkg/core/deps.
	// Only manifests defined in Language.ManifestAliases are recognized.
	tests := []struct {
		name string
		arg  string
		want bool
	}{
		// Recognized manifest files (from language definitions)
		{"requirements.txt", "requirements.txt", true},
		{"poetry.lock", "poetry.lock", true},
		{"pyproject.toml", "pyproject.toml", true},
		{"uv.lock", "uv.lock", true},
		{"pom.xml", "pom.xml", true},
		{"go.mod", "go.mod", true},
		{"package.json", "package.json", true},
		{"Cargo.toml", "Cargo.toml", true},
		{"cargo.toml lowercase", "cargo.toml", true},
		{"Gemfile", "Gemfile", true},
		{"composer.json", "composer.json", true},

		// Not in language definitions
		// Case sensitivity: go.mod is defined, but GO.MOD is not
		{"GO.MOD uppercase not matched", "GO.MOD", false},

		// Package names (not files)
		{"package name", "requests", false},
		{"package with version", "requests==2.0", false},
		{"package with dash", "my-package", false},
		{"package with underscore", "my_package", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := looksLikeFile(tt.arg); got != tt.want {
				t.Errorf("looksLikeFile(%q) = %v, want %v", tt.arg, got, tt.want)
			}
		})
	}
}

func TestLanguagesRegistered(t *testing.T) {
	if len(languages.All) == 0 {
		t.Error("languages.All slice should not be empty")
	}

	// Check that all expected languages are present
	expectedLangs := []string{"python", "rust", "javascript", "ruby", "php", "java", "go"}
	langNames := make(map[string]bool)
	for _, lang := range languages.All {
		langNames[lang.Name] = true
	}

	for _, expected := range expectedLangs {
		if !langNames[expected] {
			t.Errorf("languages.All missing %q", expected)
		}
	}
}

func TestParsePackageVersion(t *testing.T) {
	tests := []struct {
		name        string
		arg         string
		wantPkg     string
		wantVersion string
	}{
		// Simple packages
		{"no version", "requests", "requests", ""},
		{"with version", "requests@2.31.0", "requests", "2.31.0"},
		{"complex version", "django@4.2.0a1", "django", "4.2.0a1"},

		// Scoped npm packages
		{"scoped no version", "@angular/core", "@angular/core", ""},
		{"scoped with version", "@angular/core@17.0.0", "@angular/core", "17.0.0"},
		{"scoped nested", "@types/node@20.10.0", "@types/node", "20.10.0"},

		// Edge cases
		{"empty", "", "", ""},
		{"just @", "@", "@", ""},
		{"trailing @", "pkg@", "pkg", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotPkg, gotVersion := parsePackageVersion(tt.arg)
			if gotPkg != tt.wantPkg {
				t.Errorf("parsePackageVersion(%q) pkg = %q, want %q", tt.arg, gotPkg, tt.wantPkg)
			}
			if gotVersion != tt.wantVersion {
				t.Errorf("parsePackageVersion(%q) version = %q, want %q", tt.arg, gotVersion, tt.wantVersion)
			}
		})
	}
}

func TestValidateFlags(t *testing.T) {
	tests := []struct {
		name      string
		maxDepth  int
		maxNodes  int
		wantErr   bool
		errKind   ErrorKind
		errSubstr string
	}{
		// Valid inputs
		{"valid defaults", 10, 5000, false, "", ""},
		{"min depth", 1, 100, false, "", ""},
		{"max depth", 100, 50000, false, "", ""},

		// Invalid maxDepth
		{"depth zero", 0, 5000, true, ErrorKindUser, "max-depth"},
		{"depth negative", -1, 5000, true, ErrorKindUser, "max-depth"},
		{"depth too large", 101, 5000, true, ErrorKindUser, "max-depth"},
		{"depth way too large", 1000, 5000, true, ErrorKindUser, "max-depth"},

		// Invalid maxNodes
		{"nodes zero", 10, 0, true, ErrorKindUser, "max-nodes"},
		{"nodes negative", 10, -1, true, ErrorKindUser, "max-nodes"},
		{"nodes too large", 10, 50001, true, ErrorKindUser, "max-nodes"},
		{"nodes way too large", 10, 1000000, true, ErrorKindUser, "max-nodes"},

		// Both invalid (depth checked first)
		{"both invalid", 0, 0, true, ErrorKindUser, "max-depth"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateFlags(tt.maxDepth, tt.maxNodes)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateFlags(%d, %d) error = %v, wantErr %v", tt.maxDepth, tt.maxNodes, err, tt.wantErr)
				return
			}
			if err != nil && tt.errKind != "" {
				var cliErr *CLIError
				if !errors.As(err, &cliErr) {
					t.Errorf("expected CLIError, got %T", err)
					return
				}
				if cliErr.Kind != tt.errKind {
					t.Errorf("error kind = %v, want %v", cliErr.Kind, tt.errKind)
				}
			}
		})
	}
}

func TestValidatePackageName(t *testing.T) {
	tests := []struct {
		name    string
		pkgName string
		wantErr bool
		errMsg  string
	}{
		// Valid package names
		{"simple", "requests", false, ""},
		{"with dash", "my-package", false, ""},
		{"with underscore", "my_package", false, ""},
		{"with numbers", "pkg123", false, ""},
		{"scoped npm", "@angular/core", false, ""},
		{"with dots", "com.example.pkg", false, ""},
		{"unicode allowed", "пакет", false, ""},

		// Empty/length errors
		{"empty", "", true, "empty"},
		{"too long", strings.Repeat("a", 257), true, "too long"},
		{"exactly 256 chars", strings.Repeat("a", 256), false, ""},

		// Path traversal attempts
		{"parent dir", "../etc/passwd", true, ".."},
		{"parent dir middle", "foo/../bar", true, ".."},
		{"double slash", "foo//bar", true, "//"},
		{"backslash", "foo\\bar", true, "\\"},

		// Control characters (null byte is explicitly rejected)
		{"null byte", "foo\x00bar", true, "\\x00"},
		{"newline", "foo\nbar", true, "control"},
		{"carriage return", "foo\rbar", true, "control"},
		{"tab", "foo\tbar", true, "control"},
		{"bell", "foo\x07bar", true, "control"},
		{"escape", "foo\x1bbar", true, "control"},
		{"del char", "foo\x7fbar", true, "control"},

		// Edge cases
		{"single char", "a", false, ""},
		{"single dot", ".", false, ""},
		{"single at", "@", false, ""},
		{"starts with dot", ".hidden", false, ""},
		{"ends with dot", "pkg.", false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePackageName(tt.pkgName)
			if (err != nil) != tt.wantErr {
				t.Errorf("validatePackageName(%q) error = %v, wantErr %v", tt.pkgName, err, tt.wantErr)
				return
			}
			if err != nil && tt.errMsg != "" {
				if !containsSubstring(err.Error(), tt.errMsg) {
					t.Errorf("error = %q, should contain %q", err.Error(), tt.errMsg)
				}
			}
		})
	}
}

func TestValidatePackageName_SecurityBoundaries(t *testing.T) {
	// Test specific security attack vectors
	attacks := []struct {
		name    string
		pkgName string
	}{
		{"shell injection semicolon", "pkg;rm -rf /"},
		{"shell injection pipe", "pkg|cat /etc/passwd"},
		{"shell injection backtick", "pkg`id`"},
		{"shell injection dollar", "pkg$(id)"},
		{"URL encoding attempt", "pkg%2e%2e%2fetc"},
	}

	// These should NOT cause validation failures (they're caught by other means)
	// but verify they don't crash or panic
	for _, tt := range attacks {
		t.Run(tt.name, func(t *testing.T) {
			// Just ensure it doesn't panic
			_ = validatePackageName(tt.pkgName)
		})
	}
}

func TestSuggestOutputName(t *testing.T) {
	tests := []struct {
		name    string
		rootID  string
		version string
		ref     string
		want    string
	}{
		{"registry with version", "flask", "3.1.0", "", "flask-3.1.0.json"},
		{"github with ref tag", "myrepo", "", "v2.0.0", "myrepo-v2.0.0.json"},
		{"github with branch", "myrepo", "", "main", "myrepo-main.json"},
		{"github with slash in ref", "myrepo", "", "feature/foo", "myrepo-feature-foo.json"},
		{"no version or ref", "flask", "", "", "flask.json"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := dag.New(nil)
			meta := dag.Metadata{}
			if tt.version != "" {
				meta["version"] = tt.version
			}
			g.AddNode(dag.Node{ID: tt.rootID, Row: 0, Meta: meta})

			got := suggestOutputName(g, tt.ref)
			if got != tt.want {
				t.Errorf("suggestOutputName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSanitizeFilenameSegment(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"v2.0.0", "v2.0.0"},
		{"feature/branch", "feature-branch"},
		{"refs/tags/v1", "refs-tags-v1"},
		{"some:thing", "some-thing"},
	}
	for _, tt := range tests {
		got := sanitizeFilenameSegment(tt.input)
		if got != tt.want {
			t.Errorf("sanitizeFilenameSegment(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
