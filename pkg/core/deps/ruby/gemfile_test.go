package ruby

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stacktower-io/stacktower/pkg/core/deps"
)

func TestGemfile_Supports(t *testing.T) {
	parser := &Gemfile{}

	tests := []struct {
		filename string
		want     bool
	}{
		{"Gemfile", true},
		{"gemfile", false},
		{"Gemfile.lock", false},
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

func TestGemfile_Parse(t *testing.T) {
	dir := t.TempDir()
	gemfile := filepath.Join(dir, "Gemfile")
	content := `source 'https://rubygems.org'

# Web framework
gem 'rails', '~> 7.0'
gem 'puma', '>= 5.0'

group :development, :test do
  gem 'rspec-rails'
  gem 'factory_bot_rails'
end
`

	if err := os.WriteFile(gemfile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	parser := &Gemfile{}
	result, err := parser.Parse(gemfile, deps.Options{})
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	g := result.Graph

	// project root + 2 runtime gems
	if got := g.NodeCount(); got != 3 {
		t.Errorf("NodeCount = %d, want 3", got)
	}

	for _, dep := range []string{"rails", "puma"} {
		if _, ok := g.Node(dep); !ok {
			t.Errorf("expected node %q not found", dep)
		}
	}
	for _, dep := range []string{"rspec-rails", "factory_bot_rails"} {
		if _, ok := g.Node(dep); ok {
			t.Errorf("did not expect dev/test gem %q in prod_only scope", dep)
		}
	}
}

func TestGemfile_Parse_MultipleVersionConstraints(t *testing.T) {
	dir := t.TempDir()
	gemfile := filepath.Join(dir, "Gemfile")
	content := `source 'https://rubygems.org'

gem 'rack', '>= 1.0', '< 2.0', '< 3.0'
gem 'rails', '~> 7.0'
gem 'puma', '>= 5.0', '< 7'
gem 'sidekiq', '>= 6.0', require: false
`
	if err := os.WriteFile(gemfile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	f, err := os.Open(gemfile)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	result, _ := parseGemfileWithVersions(f, deps.DependencyScopeProdOnly)
	got := make(map[string]string, len(result))
	for _, dep := range result {
		got[dep.Name] = dep.Constraint
	}

	tests := []struct {
		gem            string
		wantConstraint string
	}{
		// All three constraints captured, not just the first two
		{"rack", ">= 1.0, < 2.0, < 3.0"},
		{"rails", "~> 7.0"},
		{"puma", ">= 5.0, < 7"},
		// Constraint extraction stops at hash options like require:
		{"sidekiq", ">= 6.0"},
	}
	for _, tt := range tests {
		constraint, ok := got[tt.gem]
		if !ok {
			t.Errorf("gem %q missing from result", tt.gem)
			continue
		}
		if constraint != tt.wantConstraint {
			t.Errorf("gem %q constraint = %q, want %q", tt.gem, constraint, tt.wantConstraint)
		}
	}
}

func TestGemfile_Parse_GitAndPathSourceGems(t *testing.T) {
	// Gems declared with git:/github:/path: sources still produce a
	// dependency node (with no version constraint) instead of being dropped.
	dir := t.TempDir()
	gemfile := filepath.Join(dir, "Gemfile")
	content := `source 'https://rubygems.org'

gem 'rails', github: 'rails/rails'
gem 'my_engine', path: '../my_engine'
gem 'custom', git: 'https://github.com/user/custom.git', branch: 'main'
gem 'pinned', '~> 1.0', github: 'user/pinned'
`
	if err := os.WriteFile(gemfile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	f, err := os.Open(gemfile)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	result, _ := parseGemfileWithVersions(f, deps.DependencyScopeProdOnly)
	got := make(map[string]string, len(result))
	for _, dep := range result {
		got[dep.Name] = dep.Constraint
	}

	for _, gem := range []string{"rails", "my_engine", "custom"} {
		constraint, ok := got[gem]
		if !ok {
			t.Errorf("gem %q with git/path source missing from result", gem)
			continue
		}
		if constraint != "" {
			t.Errorf("gem %q constraint = %q, want empty (source option is not a constraint)", gem, constraint)
		}
	}
	// Version constraints before source options are still captured
	if got["pinned"] != "~> 1.0" {
		t.Errorf("gem %q constraint = %q, want %q", "pinned", got["pinned"], "~> 1.0")
	}
}

func TestExtractGemConstraints(t *testing.T) {
	tests := []struct {
		rest string
		want []string
	}{
		{", '~> 5.0'", []string{"~> 5.0"}},
		{", '>= 1.0', '< 2.0', '< 3.0'", []string{">= 1.0", "< 2.0", "< 3.0"}},
		{", '>= 1.0', require: false", []string{">= 1.0"}},
		{", github: 'rails/rails'", nil},
		{", tag: '1.2.3'", nil}, // quoted hash value is not a constraint
		{"", nil},
		{", '1.0.0'", []string{"1.0.0"}}, // bare version
	}
	for _, tt := range tests {
		t.Run(tt.rest, func(t *testing.T) {
			got := extractGemConstraints(tt.rest)
			if len(got) != len(tt.want) {
				t.Fatalf("extractGemConstraints(%q) = %v, want %v", tt.rest, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("extractGemConstraints(%q)[%d] = %q, want %q", tt.rest, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestGemfile_Type(t *testing.T) {
	parser := &Gemfile{}
	if got := parser.Type(); got != "Gemfile" {
		t.Errorf("Type() = %q, want %q", got, "Gemfile")
	}
}

func TestGemfile_IncludesTransitive(t *testing.T) {
	parser := &Gemfile{}
	if parser.IncludesTransitive() {
		t.Error("IncludesTransitive() = true, want false (no resolver)")
	}
}

func TestParseGemfile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Gemfile")
	content := `gem 'rails'
gem "puma"
gem 'rails'  # duplicate should be ignored
# gem 'commented_out'
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	f, _ := os.Open(path)
	defer f.Close()

	gems := parseGemfile(f)

	if len(gems) != 2 {
		t.Errorf("expected 2 gems, got %d: %v", len(gems), gems)
	}
}

func TestExtractRubyVersion(t *testing.T) {
	tests := []struct {
		constraint string
		want       string
	}{
		{"3.2.0", "3.2.0"},
		{"~> 3.0", "3.0"},
		{">= 2.7", "2.7"},
		{"~> 3.1.0", "3.1.0"},
		{">= 2.7.0, < 4.0", "2.7.0"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.constraint, func(t *testing.T) {
			if got := extractRubyVersion(tt.constraint); got != tt.want {
				t.Errorf("extractRubyVersion(%q) = %q, want %q", tt.constraint, got, tt.want)
			}
		})
	}
}

func TestGemfile_RuntimeVersion(t *testing.T) {
	dir := t.TempDir()
	gemfile := filepath.Join(dir, "Gemfile")
	content := `source 'https://rubygems.org'

ruby '3.2.0'

gem 'rails', '~> 7.0'
gem 'puma', '>= 5.0'
`

	if err := os.WriteFile(gemfile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	parser := &Gemfile{}
	result, err := parser.Parse(gemfile, deps.Options{})
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if result.RuntimeVersion != "3.2.0" {
		t.Errorf("RuntimeVersion = %q, want %q", result.RuntimeVersion, "3.2.0")
	}
	if result.RuntimeConstraint != "3.2.0" {
		t.Errorf("RuntimeConstraint = %q, want %q", result.RuntimeConstraint, "3.2.0")
	}
}

func TestGemfile_RuntimeVersion_Constraint(t *testing.T) {
	dir := t.TempDir()
	gemfile := filepath.Join(dir, "Gemfile")
	content := `source 'https://rubygems.org'

ruby '>= 3.0'

gem 'rails'
`

	if err := os.WriteFile(gemfile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	parser := &Gemfile{}
	result, err := parser.Parse(gemfile, deps.Options{})
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if result.RuntimeVersion != "3.0" {
		t.Errorf("RuntimeVersion = %q, want %q", result.RuntimeVersion, "3.0")
	}
	if result.RuntimeConstraint != ">= 3.0" {
		t.Errorf("RuntimeConstraint = %q, want %q", result.RuntimeConstraint, ">= 3.0")
	}
}
