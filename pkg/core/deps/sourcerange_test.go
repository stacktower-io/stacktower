package deps

import (
	"testing"
)

func TestPyprojectDetector_PEP621(t *testing.T) {
	content := `[project]
name = "myproject"
version = "1.0.0"
dependencies = [
    "requests>=2.28.0",
    "flask>=2.0.0",
    "sqlalchemy>=1.4.0",
]

[project.optional-dependencies]
dev = ["pytest", "black"]
`
	d := &PyprojectDetector{}
	ranges := d.DetectSections(content)

	// Should detect both dependencies and optional-dependencies sections
	if len(ranges) != 2 {
		t.Fatalf("expected 2 ranges, got %d: %+v", len(ranges), ranges)
	}

	// First range: project.dependencies
	r := ranges[0]
	if r.Section != "project.dependencies" {
		t.Errorf("expected section 'project.dependencies', got %q", r.Section)
	}
	if r.StartLine != 4 {
		t.Errorf("expected start line 4, got %d", r.StartLine)
	}
	if r.EndLine != 8 {
		t.Errorf("expected end line 8, got %d", r.EndLine)
	}

	// Second range: project.optional-dependencies.dev
	r = ranges[1]
	if r.Section != "project.optional-dependencies.dev" {
		t.Errorf("expected section 'project.optional-dependencies.dev', got %q", r.Section)
	}
	if r.StartLine != 11 {
		t.Errorf("expected start line 11, got %d", r.StartLine)
	}
	if r.EndLine != 11 {
		t.Errorf("expected end line 11, got %d", r.EndLine)
	}
}

func TestPyprojectDetector_Poetry(t *testing.T) {
	content := `[tool.poetry]
name = "myproject"
version = "1.0.0"

[tool.poetry.dependencies]
python = "^3.9"
requests = "^2.28.0"
flask = "^2.0.0"

[tool.poetry.dev-dependencies]
pytest = "^7.0.0"
`
	d := &PyprojectDetector{}
	ranges := d.DetectSections(content)

	// Should detect both dependencies and dev-dependencies sections
	if len(ranges) != 2 {
		t.Fatalf("expected 2 ranges, got %d: %+v", len(ranges), ranges)
	}

	// First range: tool.poetry.dependencies
	r := ranges[0]
	if r.Section != "tool.poetry.dependencies" {
		t.Errorf("expected section 'tool.poetry.dependencies', got %q", r.Section)
	}
	if r.StartLine != 5 {
		t.Errorf("expected start line 5, got %d", r.StartLine)
	}
	if r.EndLine != 8 {
		t.Errorf("expected end line 8, got %d", r.EndLine)
	}

	// Second range: tool.poetry.dev-dependencies
	r = ranges[1]
	if r.Section != "tool.poetry.dev-dependencies" {
		t.Errorf("expected section 'tool.poetry.dev-dependencies', got %q", r.Section)
	}
	if r.StartLine != 10 {
		t.Errorf("expected start line 10, got %d", r.StartLine)
	}
	if r.EndLine != 11 {
		t.Errorf("expected end line 11, got %d", r.EndLine)
	}
}

func TestPyprojectDetector_SingleLine(t *testing.T) {
	content := `[project]
name = "simple"
dependencies = ["requests"]
`
	d := &PyprojectDetector{}
	ranges := d.DetectSections(content)

	if len(ranges) != 1 {
		t.Fatalf("expected 1 range, got %d: %+v", len(ranges), ranges)
	}

	r := ranges[0]
	if r.StartLine != 3 || r.EndLine != 3 {
		t.Errorf("expected single line range 3-3, got %d-%d", r.StartLine, r.EndLine)
	}
}

func TestPyprojectDetector_FlitRequiresExtra(t *testing.T) {
	content := `[tool.flit.metadata]
module = "myproject"
requires = ["requests>=2.0"]

[tool.flit.metadata.requires-extra]
test = [
    "pytest>=5.0",
    "pytest-cov>=2.0",
]
dev = [
    "flake8>=3.0",
    "black>=20.0",
]
`
	d := &PyprojectDetector{}
	ranges := d.DetectSections(content)

	// Should detect requires, test extras, and dev extras
	if len(ranges) != 3 {
		t.Fatalf("expected 3 ranges, got %d: %+v", len(ranges), ranges)
	}

	// First range: tool.flit.metadata.requires
	r := ranges[0]
	if r.Section != "tool.flit.metadata.requires" {
		t.Errorf("expected section 'tool.flit.metadata.requires', got %q", r.Section)
	}
	if r.StartLine != 3 {
		t.Errorf("expected start line 3, got %d", r.StartLine)
	}

	// Second range: tool.flit.metadata.requires-extra.test
	r = ranges[1]
	if r.Section != "tool.flit.metadata.requires-extra.test" {
		t.Errorf("expected section 'tool.flit.metadata.requires-extra.test', got %q", r.Section)
	}
	if r.StartLine != 6 {
		t.Errorf("expected start line 6, got %d", r.StartLine)
	}
	if r.EndLine != 9 {
		t.Errorf("expected end line 9, got %d", r.EndLine)
	}

	// Third range: tool.flit.metadata.requires-extra.dev
	r = ranges[2]
	if r.Section != "tool.flit.metadata.requires-extra.dev" {
		t.Errorf("expected section 'tool.flit.metadata.requires-extra.dev', got %q", r.Section)
	}
	if r.StartLine != 10 {
		t.Errorf("expected start line 10, got %d", r.StartLine)
	}
	if r.EndLine != 13 {
		t.Errorf("expected end line 13, got %d", r.EndLine)
	}
}

func TestPackageJSONDetector(t *testing.T) {
	content := `{
  "name": "myproject",
  "version": "1.0.0",
  "dependencies": {
    "lodash": "^4.17.0",
    "express": "^4.18.0"
  },
  "devDependencies": {
    "jest": "^29.0.0"
  }
}`
	d := &PackageJSONDetector{}
	ranges := d.DetectSections(content)

	if len(ranges) != 2 {
		t.Fatalf("expected 2 ranges, got %d: %+v", len(ranges), ranges)
	}

	// Check dependencies section
	if ranges[0].Section != "dependencies" {
		t.Errorf("expected first section 'dependencies', got %q", ranges[0].Section)
	}
	if ranges[0].StartLine != 4 || ranges[0].EndLine != 7 {
		t.Errorf("expected dependencies range 4-7, got %d-%d", ranges[0].StartLine, ranges[0].EndLine)
	}

	// Check devDependencies section
	if ranges[1].Section != "devDependencies" {
		t.Errorf("expected second section 'devDependencies', got %q", ranges[1].Section)
	}
}

func TestGoModDetector(t *testing.T) {
	content := `module github.com/example/project

go 1.21

require (
	github.com/pkg/errors v0.9.1
	github.com/stretchr/testify v1.8.4
)

require github.com/davecgh/go-spew v1.1.1 // indirect
`
	d := &GoModDetector{}
	ranges := d.DetectSections(content)

	if len(ranges) != 2 {
		t.Fatalf("expected 2 ranges, got %d: %+v", len(ranges), ranges)
	}

	// Block require
	if ranges[0].Section != "require" {
		t.Errorf("expected section 'require', got %q", ranges[0].Section)
	}
	if ranges[0].StartLine != 5 || ranges[0].EndLine != 8 {
		t.Errorf("expected require block range 5-8, got %d-%d", ranges[0].StartLine, ranges[0].EndLine)
	}

	// Single require
	if ranges[1].StartLine != 10 || ranges[1].EndLine != 10 {
		t.Errorf("expected single require range 10-10, got %d-%d", ranges[1].StartLine, ranges[1].EndLine)
	}
}

func TestCargoDetector(t *testing.T) {
	content := `[package]
name = "myproject"
version = "1.0.0"

[dependencies]
serde = "1.0"
tokio = { version = "1.0", features = ["full"] }

[dev-dependencies]
criterion = "0.5"
`
	d := &CargoDetector{}
	ranges := d.DetectSections(content)

	if len(ranges) != 2 {
		t.Fatalf("expected 2 ranges, got %d: %+v", len(ranges), ranges)
	}

	if ranges[0].Section != "dependencies" {
		t.Errorf("expected first section 'dependencies', got %q", ranges[0].Section)
	}
	if ranges[1].Section != "dev-dependencies" {
		t.Errorf("expected second section 'dev-dependencies', got %q", ranges[1].Section)
	}
}

func TestGemfileDetector(t *testing.T) {
	content := `source 'https://rubygems.org'

gem 'rails', '~> 7.0'
gem 'pg', '~> 1.4'

group :development, :test do
  gem 'rspec-rails'
  gem 'factory_bot_rails'
end

group :development do
  gem 'rubocop'
end

group :test do
  gem 'capybara'
end
`
	d := &GemfileDetector{}
	ranges := d.DetectSections(content)

	// Should detect: 2 top-level gems + 3 groups
	if len(ranges) != 5 {
		t.Fatalf("expected 5 ranges, got %d: %+v", len(ranges), ranges)
	}

	// First two should be top-level gems
	if ranges[0].Section != "gem" {
		t.Errorf("expected section 'gem', got %q", ranges[0].Section)
	}
	if ranges[1].Section != "gem" {
		t.Errorf("expected section 'gem', got %q", ranges[1].Section)
	}

	// Third should be group.development (first group listed)
	if ranges[2].Section != "group.development" {
		t.Errorf("expected section 'group.development', got %q", ranges[2].Section)
	}

	// Fourth should be group.development
	if ranges[3].Section != "group.development" {
		t.Errorf("expected section 'group.development', got %q", ranges[3].Section)
	}

	// Fifth should be group.test
	if ranges[4].Section != "group.test" {
		t.Errorf("expected section 'group.test', got %q", ranges[4].Section)
	}
}

func TestRequirementsDetector(t *testing.T) {
	content := `# Production dependencies
requests>=2.28.0
flask>=2.0.0
sqlalchemy>=1.4.0

# Another comment
numpy==1.24.0
`
	d := &RequirementsDetector{}
	ranges := d.DetectSections(content)

	// Should merge into one range since it's all dependencies
	if len(ranges) == 0 {
		t.Fatal("expected at least 1 range")
	}

	// First range should cover the first block
	if ranges[0].Section != "dependencies" {
		t.Errorf("expected section 'dependencies', got %q", ranges[0].Section)
	}
}

func TestDetectDependencySections_Integration(t *testing.T) {
	tests := []struct {
		filename string
		content  string
		wantLen  int
	}{
		{
			filename: "pyproject.toml",
			content:  "[project]\ndependencies = [\"requests\"]\n",
			wantLen:  1,
		},
		{
			filename: "package.json",
			content:  "{\n  \"dependencies\": {\n    \"lodash\": \"^4.0.0\"\n  }\n}",
			wantLen:  1,
		},
		{
			filename: "go.mod",
			content:  "module test\nrequire github.com/pkg/errors v0.9.1\n",
			wantLen:  1,
		},
		{
			filename: "unknown.xyz",
			content:  "some content",
			wantLen:  0,
		},
	}

	detectors := DefaultDetectors()

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			ranges := DetectDependencySections(tt.content, tt.filename, detectors...)
			if len(ranges) != tt.wantLen {
				t.Errorf("expected %d ranges for %s, got %d: %+v", tt.wantLen, tt.filename, len(ranges), ranges)
			}
		})
	}
}

func TestCountBrackets(t *testing.T) {
	tests := []struct {
		line string
		want int
	}{
		{"[", 1},
		{"]", -1},
		{"[]", 0},
		{`["hello"]`, 0},
		{`["hello", "world"]`, 0},
		{`dependencies = [`, 1},
		{`    "requests>=2.0",`, 0},
		{`]`, -1},
		{`["foo[bar]"]`, 0}, // brackets inside string
	}

	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			got := countBrackets(tt.line)
			if got != tt.want {
				t.Errorf("countBrackets(%q) = %d, want %d", tt.line, got, tt.want)
			}
		})
	}
}

func TestUvLockDetector(t *testing.T) {
	detector := &UvLockDetector{}

	// Test Supports
	if !detector.Supports("uv.lock") {
		t.Error("UvLockDetector should support uv.lock")
	}
	if detector.Supports("pyproject.toml") {
		t.Error("UvLockDetector should not support pyproject.toml")
	}

	// Test DetectSections
	content := `version = 1
revision = 2
requires-python = ">=3.11"

[[package]]
name = "aiohappyeyeballs"
version = "2.6.1"
source = { registry = "https://pypi.org/simple" }

[[package]]
name = "aiohttp"
version = "3.13.2"
source = { registry = "https://pypi.org/simple" }
dependencies = [
    { name = "aiohappyeyeballs" },
]

[[package]]
name = "requests"
version = "2.31.0"
`

	ranges := detector.DetectSections(content)

	if len(ranges) != 3 {
		t.Fatalf("expected 3 ranges, got %d", len(ranges))
	}

	// First package: starts at line 5
	if ranges[0].Section != "package" {
		t.Errorf("range 0: expected section 'package', got %q", ranges[0].Section)
	}
	if ranges[0].StartLine != 5 {
		t.Errorf("range 0: expected start line 5, got %d", ranges[0].StartLine)
	}

	// Second package: starts at line 10
	if ranges[1].Section != "package" {
		t.Errorf("range 1: expected section 'package', got %q", ranges[1].Section)
	}
	if ranges[1].StartLine != 10 {
		t.Errorf("range 1: expected start line 10, got %d", ranges[1].StartLine)
	}

	// Third package: starts at line 18
	if ranges[2].Section != "package" {
		t.Errorf("range 2: expected section 'package', got %q", ranges[2].Section)
	}
	if ranges[2].StartLine != 18 {
		t.Errorf("range 2: expected start line 18, got %d", ranges[2].StartLine)
	}
}

func TestPoetryLockDetector(t *testing.T) {
	detector := &PoetryLockDetector{}

	// Test Supports
	if !detector.Supports("poetry.lock") {
		t.Error("PoetryLockDetector should support poetry.lock")
	}
	if detector.Supports("pyproject.toml") {
		t.Error("PoetryLockDetector should not support pyproject.toml")
	}

	// Test DetectSections
	content := `[[package]]
name = "certifi"
version = "2023.7.22"
description = "Python package for providing Mozilla's CA Bundle."
optional = false
python-versions = ">=3.6"

[[package]]
name = "charset-normalizer"
version = "3.2.0"
`

	ranges := detector.DetectSections(content)

	if len(ranges) != 2 {
		t.Fatalf("expected 2 ranges, got %d", len(ranges))
	}

	if ranges[0].Section != "package" {
		t.Errorf("range 0: expected section 'package', got %q", ranges[0].Section)
	}
	if ranges[0].StartLine != 1 {
		t.Errorf("range 0: expected start line 1, got %d", ranges[0].StartLine)
	}
}

func TestPackageLockJSONDetector(t *testing.T) {
	detector := &PackageLockJSONDetector{}

	// Test Supports
	if !detector.Supports("package-lock.json") {
		t.Error("PackageLockJSONDetector should support package-lock.json")
	}
	if detector.Supports("package.json") {
		t.Error("PackageLockJSONDetector should not support package.json")
	}

	// Test DetectSections with npm v3 format (node_modules paths)
	content := `{
  "name": "my-project",
  "version": "1.0.0",
  "lockfileVersion": 3,
  "packages": {
    "": {
      "name": "my-project",
      "dependencies": {
        "lodash": "^4.17.21"
      }
    },
    "node_modules/lodash": {
      "version": "4.17.21",
      "resolved": "https://registry.npmjs.org/lodash/-/lodash-4.17.21.tgz"
    },
    "node_modules/express": {
      "version": "4.18.2",
      "resolved": "https://registry.npmjs.org/express/-/express-4.18.2.tgz",
      "dependencies": {
        "body-parser": "1.20.1"
      }
    }
  }
}`

	ranges := detector.DetectSections(content)

	if len(ranges) < 2 {
		t.Fatalf("expected at least 2 ranges for node_modules entries, got %d", len(ranges))
	}

	// All ranges should be "packages" section
	for i, r := range ranges {
		if r.Section != "packages" {
			t.Errorf("range %d: expected section 'packages', got %q", i, r.Section)
		}
	}
}
