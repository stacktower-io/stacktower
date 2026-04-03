package deps

import (
	"regexp"
	"strings"
)

// SourceRange identifies a section of a manifest file where dependencies are defined.
// This is used to highlight the relevant sections in the frontend viewer.
type SourceRange struct {
	// StartLine is the 1-indexed line number where the section starts.
	StartLine int `json:"start_line"`

	// EndLine is the 1-indexed line number where the section ends (inclusive).
	EndLine int `json:"end_line"`

	// Section is a human-readable identifier for the section (e.g., "project.dependencies").
	Section string `json:"section"`
}

// SectionDetector detects dependency sections in manifest content.
// Each language implements this interface to find where dependencies are declared.
type SectionDetector interface {
	// DetectSections scans the manifest content and returns the line ranges
	// where dependencies are defined.
	DetectSections(content string) []SourceRange

	// Supports reports whether this detector handles the given filename.
	Supports(filename string) bool
}

// DetectDependencySections finds dependency sections in a manifest file.
// It tries each detector in order and returns results from the first match.
// Returns nil if no detector supports the filename.
func DetectDependencySections(content, filename string, detectors ...SectionDetector) []SourceRange {
	for _, d := range detectors {
		if d.Supports(filename) {
			return d.DetectSections(content)
		}
	}
	return nil
}

// =============================================================================
// Python: pyproject.toml
// =============================================================================

// PyprojectDetector detects dependency sections in pyproject.toml files.
type PyprojectDetector struct{}

func (d *PyprojectDetector) Supports(filename string) bool {
	return filename == "pyproject.toml"
}

func (d *PyprojectDetector) DetectSections(content string) []SourceRange {
	var ranges []SourceRange
	lines := strings.Split(content, "\n")

	// Track state for multi-line arrays
	var currentSection string
	var startLine int
	inArray := false
	bracketDepth := 0

	// Track multi-line strings (TOML triple-quoted strings)
	inMultiLineString := false
	multiLineStringDelim := ""

	// Patterns for section headers
	sectionHeaderRE := regexp.MustCompile(`^\s*\[([^\]]+)\]`)

	// Patterns for inline dependency declarations
	// PEP 621: dependencies = [...]
	pep621DepsRE := regexp.MustCompile(`^\s*dependencies\s*=\s*\[`)
	// PEP 621 optional-dependencies: key = [...] inside [project.optional-dependencies]
	optionalDepsEntryRE := regexp.MustCompile(`^\s*([a-zA-Z][-a-zA-Z0-9._]*)\s*=\s*\[`)
	// Flit: requires = [...]
	flitRequiresRE := regexp.MustCompile(`^\s*requires\s*=\s*\[`)

	// Triple-quote patterns for TOML multi-line strings
	tripleQuoteRE := regexp.MustCompile(`'''|"""`)

	currentTableSection := ""

	for i, line := range lines {
		lineNum := i + 1

		// Handle multi-line strings (skip content inside them)
		if inMultiLineString {
			if strings.Contains(line, multiLineStringDelim) {
				inMultiLineString = false
				multiLineStringDelim = ""
			}
			continue
		}

		// Check for start of multi-line string
		if matches := tripleQuoteRE.FindAllString(line, -1); len(matches) > 0 {
			// If odd number of triple quotes on this line, we're entering a multi-line string
			if len(matches)%2 == 1 {
				inMultiLineString = true
				multiLineStringDelim = matches[0]
				continue
			}
		}

		// Check for section headers like [project] or [tool.poetry.dependencies]
		if m := sectionHeaderRE.FindStringSubmatch(line); m != nil {
			// If we were tracking a section, close it
			if inArray && currentSection != "" {
				ranges = append(ranges, SourceRange{
					StartLine: startLine,
					EndLine:   lineNum - 1,
					Section:   currentSection,
				})
				inArray = false
				currentSection = ""
			}

			currentTableSection = m[1]

			// Check if this is a dependency section we care about
			switch currentTableSection {
			case "tool.poetry.dependencies":
				// Start tracking Poetry dependencies section
				currentSection = "tool.poetry.dependencies"
				startLine = lineNum
				inArray = true // Not really an array, but we're tracking entries
				bracketDepth = 0
			case "tool.poetry.dev-dependencies", "tool.poetry.group.dev.dependencies":
				// Start tracking Poetry dev dependencies section
				currentSection = currentTableSection
				startLine = lineNum
				inArray = true
				bracketDepth = 0
			case "tool.flit.metadata":
				// We'll look for requires = [...] inside this section
			case "project.optional-dependencies", "tool.flit.metadata.requires-extra":
				// We'll look for named arrays inside this section (dev = [...], test = [...], etc.)
			}
			continue
		}

		// Check for PEP 621 dependencies in [project] section
		if currentTableSection == "project" && pep621DepsRE.MatchString(line) {
			currentSection = "project.dependencies"
			startLine = lineNum
			inArray = true
			bracketDepth = countBrackets(line)

			// Check if it's a single-line array
			if bracketDepth == 0 && strings.Contains(line, "]") {
				ranges = append(ranges, SourceRange{
					StartLine: startLine,
					EndLine:   lineNum,
					Section:   currentSection,
				})
				inArray = false
				currentSection = ""
			}
			continue
		}

		// Check for optional-dependencies entries in [project.optional-dependencies] or [tool.flit.metadata.requires-extra]
		// These are named arrays like: dev = [...], test = [...], etc.
		if currentTableSection == "project.optional-dependencies" || currentTableSection == "tool.flit.metadata.requires-extra" {
			if m := optionalDepsEntryRE.FindStringSubmatch(line); m != nil {
				// If we were tracking a previous entry, close it
				if inArray && currentSection != "" {
					ranges = append(ranges, SourceRange{
						StartLine: startLine,
						EndLine:   lineNum - 1,
						Section:   currentSection,
					})
				}

				groupName := m[1]
				currentSection = currentTableSection + "." + groupName
				startLine = lineNum
				inArray = true
				bracketDepth = countBrackets(line)

				// Check if it's a single-line array
				if bracketDepth == 0 && strings.Contains(line, "]") {
					ranges = append(ranges, SourceRange{
						StartLine: startLine,
						EndLine:   lineNum,
						Section:   currentSection,
					})
					inArray = false
					currentSection = ""
				}
				continue
			}
		}

		// Check for Flit requires in [tool.flit.metadata] section
		if currentTableSection == "tool.flit.metadata" && flitRequiresRE.MatchString(line) {
			currentSection = "tool.flit.metadata.requires"
			startLine = lineNum
			inArray = true
			bracketDepth = countBrackets(line)

			if bracketDepth == 0 && strings.Contains(line, "]") {
				ranges = append(ranges, SourceRange{
					StartLine: startLine,
					EndLine:   lineNum,
					Section:   currentSection,
				})
				inArray = false
				currentSection = ""
			}
			continue
		}

		// If we're inside a multi-line array, track bracket depth
		if inArray && !strings.HasPrefix(currentSection, "tool.poetry.") {
			bracketDepth += countBrackets(line)
			if bracketDepth <= 0 {
				ranges = append(ranges, SourceRange{
					StartLine: startLine,
					EndLine:   lineNum,
					Section:   currentSection,
				})
				inArray = false
				currentSection = ""
				bracketDepth = 0
			}
			continue
		}

		// For Poetry dependencies sections, track until we hit a new section or end of content
		if strings.HasPrefix(currentSection, "tool.poetry.") {
			// If we hit an empty line after some content, or a new section header, end
			trimmed := strings.TrimSpace(line)
			if trimmed == "" || strings.HasPrefix(trimmed, "[") {
				if startLine < lineNum-1 {
					ranges = append(ranges, SourceRange{
						StartLine: startLine,
						EndLine:   lineNum - 1,
						Section:   currentSection,
					})
				}
				inArray = false
				currentSection = ""
				// Re-process this line if it's a section header
				if strings.HasPrefix(trimmed, "[") {
					if m := sectionHeaderRE.FindStringSubmatch(line); m != nil {
						currentTableSection = m[1]
					}
				}
			}
		}
	}

	// Close any open section at end of file
	if inArray && currentSection != "" {
		ranges = append(ranges, SourceRange{
			StartLine: startLine,
			EndLine:   len(lines),
			Section:   currentSection,
		})
	}

	return ranges
}

// countBrackets returns the net bracket count (open minus close) for a line.
// Handles single and double quoted strings, and detects triple-quoted multi-line strings.
func countBrackets(line string) int {
	count := 0
	inString := false
	var stringChar rune
	prev := ' '
	prevPrev := ' '

	runes := []rune(line)
	for i, c := range runes {
		if inString {
			if c == stringChar && prev != '\\' {
				inString = false
			}
		} else {
			switch c {
			case '"', '\'':
				// Check for triple-quoted string (multi-line) - skip the whole line
				if i+2 < len(runes) && runes[i+1] == c && runes[i+2] == c {
					// Triple quote starts a multi-line string - skip to end of line
					// We can't properly count brackets in multi-line strings without more context
					return count
				}
				inString = true
				stringChar = c
			case '[':
				count++
			case ']':
				count--
			}
		}
		prevPrev = prev
		prev = c
		_ = prevPrev // suppress unused warning
	}
	return count
}

// =============================================================================
// JavaScript: package.json
// =============================================================================

// PackageJSONDetector detects dependency sections in package.json files.
type PackageJSONDetector struct{}

func (d *PackageJSONDetector) Supports(filename string) bool {
	return filename == "package.json"
}

func (d *PackageJSONDetector) DetectSections(content string) []SourceRange {
	var ranges []SourceRange
	lines := strings.Split(content, "\n")

	// Look for "dependencies": { ... } and "devDependencies": { ... }
	depsRE := regexp.MustCompile(`^\s*"(dependencies|devDependencies|peerDependencies|optionalDependencies)"\s*:\s*\{`)

	bracketDepth := 0
	var currentSection string
	var startLine int
	inSection := false

	for i, line := range lines {
		lineNum := i + 1

		if !inSection {
			if m := depsRE.FindStringSubmatch(line); m != nil {
				currentSection = m[1]
				startLine = lineNum
				inSection = true
				bracketDepth = countBraces(line)

				// Check if single line
				if bracketDepth == 0 {
					ranges = append(ranges, SourceRange{
						StartLine: startLine,
						EndLine:   lineNum,
						Section:   currentSection,
					})
					inSection = false
					currentSection = ""
				}
			}
		} else {
			bracketDepth += countBraces(line)
			if bracketDepth <= 0 {
				ranges = append(ranges, SourceRange{
					StartLine: startLine,
					EndLine:   lineNum,
					Section:   currentSection,
				})
				inSection = false
				currentSection = ""
				bracketDepth = 0
			}
		}
	}

	return ranges
}

// countBraces returns the net brace count (open minus close) for a line.
func countBraces(line string) int {
	count := 0
	inString := false
	prev := ' '

	for _, c := range line {
		if inString {
			if c == '"' && prev != '\\' {
				inString = false
			}
		} else {
			switch c {
			case '"':
				inString = true
			case '{':
				count++
			case '}':
				count--
			}
		}
		prev = c
	}
	return count
}

// =============================================================================
// Go: go.mod
// =============================================================================

// GoModDetector detects dependency sections in go.mod files.
type GoModDetector struct{}

func (d *GoModDetector) Supports(filename string) bool {
	return filename == "go.mod"
}

func (d *GoModDetector) DetectSections(content string) []SourceRange {
	var ranges []SourceRange
	lines := strings.Split(content, "\n")

	// Look for require ( ... ) blocks and single require statements
	requireBlockRE := regexp.MustCompile(`^\s*require\s*\(`)
	requireSingleRE := regexp.MustCompile(`^\s*require\s+[^\(]`)

	inBlock := false
	var startLine int

	for i, line := range lines {
		lineNum := i + 1

		if !inBlock {
			if requireBlockRE.MatchString(line) {
				startLine = lineNum
				inBlock = true
			} else if requireSingleRE.MatchString(line) {
				ranges = append(ranges, SourceRange{
					StartLine: lineNum,
					EndLine:   lineNum,
					Section:   "require",
				})
			}
		} else {
			if strings.Contains(line, ")") {
				ranges = append(ranges, SourceRange{
					StartLine: startLine,
					EndLine:   lineNum,
					Section:   "require",
				})
				inBlock = false
			}
		}
	}

	return ranges
}

// =============================================================================
// Rust: Cargo.toml
// =============================================================================

// CargoDetector detects dependency sections in Cargo.toml files.
type CargoDetector struct{}

func (d *CargoDetector) Supports(filename string) bool {
	return filename == "Cargo.toml"
}

func (d *CargoDetector) DetectSections(content string) []SourceRange {
	var ranges []SourceRange
	lines := strings.Split(content, "\n")

	sectionHeaderRE := regexp.MustCompile(`^\s*\[([^\]]+)\]`)

	var currentSection string
	var startLine int
	inDepsSection := false

	depsSections := map[string]bool{
		"dependencies":       true,
		"dev-dependencies":   true,
		"build-dependencies": true,
		"target":             false, // Target-specific deps need special handling
	}

	for i, line := range lines {
		lineNum := i + 1

		if m := sectionHeaderRE.FindStringSubmatch(line); m != nil {
			// Close previous section
			if inDepsSection {
				ranges = append(ranges, SourceRange{
					StartLine: startLine,
					EndLine:   lineNum - 1,
					Section:   currentSection,
				})
				inDepsSection = false
			}

			section := m[1]
			// Check if it's a deps section or target-specific deps
			if depsSections[section] || strings.HasSuffix(section, ".dependencies") {
				currentSection = section
				startLine = lineNum
				inDepsSection = true
			}
		}
	}

	// Close final section
	if inDepsSection {
		ranges = append(ranges, SourceRange{
			StartLine: startLine,
			EndLine:   len(lines),
			Section:   currentSection,
		})
	}

	return ranges
}

// =============================================================================
// Ruby: Gemfile
// =============================================================================

// GemfileDetector detects dependency sections in Gemfile files.
type GemfileDetector struct{}

func (d *GemfileDetector) Supports(filename string) bool {
	return filename == "Gemfile"
}

func (d *GemfileDetector) DetectSections(content string) []SourceRange {
	var ranges []SourceRange
	lines := strings.Split(content, "\n")

	// Look for gem 'name' statements and group blocks
	gemRE := regexp.MustCompile(`^\s*gem\s+['"]`)
	// Match group :name or group :name, :other, ... do
	groupRE := regexp.MustCompile(`^\s*group\s+(.+?)\s+do`)
	// Match symbol names like :development, :test
	symbolRE := regexp.MustCompile(`:([a-zA-Z_][a-zA-Z0-9_]*)`)

	inGroup := false
	var groupStart int
	var currentGroup string

	for i, line := range lines {
		lineNum := i + 1

		if m := groupRE.FindStringSubmatch(line); m != nil {
			groupStart = lineNum
			inGroup = true
			// Extract group names from the match (e.g., ":development, :test")
			symbols := symbolRE.FindAllStringSubmatch(m[1], -1)
			if len(symbols) > 0 {
				// Use the first group name as the section name
				// e.g., "group.development" or "group.test"
				currentGroup = "group." + symbols[0][1]
			} else {
				currentGroup = "group"
			}
		} else if inGroup && strings.TrimSpace(line) == "end" {
			ranges = append(ranges, SourceRange{
				StartLine: groupStart,
				EndLine:   lineNum,
				Section:   currentGroup,
			})
			inGroup = false
		} else if !inGroup && gemRE.MatchString(line) {
			ranges = append(ranges, SourceRange{
				StartLine: lineNum,
				EndLine:   lineNum,
				Section:   "gem",
			})
		}
	}

	return ranges
}

// =============================================================================
// PHP: composer.json
// =============================================================================

// ComposerDetector detects dependency sections in composer.json files.
type ComposerDetector struct{}

func (d *ComposerDetector) Supports(filename string) bool {
	return filename == "composer.json"
}

func (d *ComposerDetector) DetectSections(content string) []SourceRange {
	var ranges []SourceRange
	lines := strings.Split(content, "\n")

	// Look for "require": { ... } and "require-dev": { ... }
	depsRE := regexp.MustCompile(`^\s*"(require|require-dev)"\s*:\s*\{`)

	bracketDepth := 0
	var currentSection string
	var startLine int
	inSection := false

	for i, line := range lines {
		lineNum := i + 1

		if !inSection {
			if m := depsRE.FindStringSubmatch(line); m != nil {
				currentSection = m[1]
				startLine = lineNum
				inSection = true
				bracketDepth = countBraces(line)

				if bracketDepth == 0 {
					ranges = append(ranges, SourceRange{
						StartLine: startLine,
						EndLine:   lineNum,
						Section:   currentSection,
					})
					inSection = false
					currentSection = ""
				}
			}
		} else {
			bracketDepth += countBraces(line)
			if bracketDepth <= 0 {
				ranges = append(ranges, SourceRange{
					StartLine: startLine,
					EndLine:   lineNum,
					Section:   currentSection,
				})
				inSection = false
				currentSection = ""
				bracketDepth = 0
			}
		}
	}

	return ranges
}

// =============================================================================
// Python: requirements.txt
// =============================================================================

// RequirementsDetector detects dependency lines in requirements.txt files.
type RequirementsDetector struct{}

func (d *RequirementsDetector) Supports(filename string) bool {
	return filename == "requirements.txt" ||
		strings.HasPrefix(filename, "requirements") && strings.HasSuffix(filename, ".txt")
}

func (d *RequirementsDetector) DetectSections(content string) []SourceRange {
	var ranges []SourceRange
	lines := strings.Split(content, "\n")

	// Match dependency lines (not comments or blank)
	depRE := regexp.MustCompile(`^\s*[a-zA-Z][-a-zA-Z0-9._]*`)

	var startLine, endLine int
	inRange := false

	for i, line := range lines {
		lineNum := i + 1
		trimmed := strings.TrimSpace(line)

		isDep := depRE.MatchString(trimmed) && !strings.HasPrefix(trimmed, "#") && !strings.HasPrefix(trimmed, "-")

		if isDep {
			if !inRange {
				startLine = lineNum
				inRange = true
			}
			endLine = lineNum
		} else if inRange && (trimmed == "" || strings.HasPrefix(trimmed, "#")) {
			// Allow comments and blank lines within a range
			// but if we hit something else, close the range
		} else if inRange {
			ranges = append(ranges, SourceRange{
				StartLine: startLine,
				EndLine:   endLine,
				Section:   "dependencies",
			})
			inRange = false
		}
	}

	// Close final range
	if inRange {
		ranges = append(ranges, SourceRange{
			StartLine: startLine,
			EndLine:   endLine,
			Section:   "dependencies",
		})
	}

	// For requirements.txt, often the whole file is dependencies
	// If we found no ranges but have content, mark the whole file
	if len(ranges) == 0 && strings.TrimSpace(content) != "" {
		ranges = append(ranges, SourceRange{
			StartLine: 1,
			EndLine:   len(lines),
			Section:   "dependencies",
		})
	}

	return ranges
}

// =============================================================================
// Python: uv.lock
// =============================================================================

// UvLockDetector detects package sections in uv.lock files.
type UvLockDetector struct{}

func (d *UvLockDetector) Supports(filename string) bool {
	return filename == "uv.lock"
}

func (d *UvLockDetector) DetectSections(content string) []SourceRange {
	var ranges []SourceRange
	lines := strings.Split(content, "\n")

	// uv.lock uses [[package]] sections for each dependency
	packageHeaderRE := regexp.MustCompile(`^\s*\[\[package\]\]`)

	var startLine int
	inPackage := false

	for i, line := range lines {
		lineNum := i + 1

		if packageHeaderRE.MatchString(line) {
			// Close previous package section
			if inPackage {
				ranges = append(ranges, SourceRange{
					StartLine: startLine,
					EndLine:   lineNum - 1,
					Section:   "package",
				})
			}
			startLine = lineNum
			inPackage = true
		}
	}

	// Close final package section
	if inPackage {
		ranges = append(ranges, SourceRange{
			StartLine: startLine,
			EndLine:   len(lines),
			Section:   "package",
		})
	}

	return ranges
}

// =============================================================================
// Python: poetry.lock
// =============================================================================

// PoetryLockDetector detects package sections in poetry.lock files.
type PoetryLockDetector struct{}

func (d *PoetryLockDetector) Supports(filename string) bool {
	return filename == "poetry.lock"
}

func (d *PoetryLockDetector) DetectSections(content string) []SourceRange {
	var ranges []SourceRange
	lines := strings.Split(content, "\n")

	// poetry.lock uses [[package]] sections for each dependency
	packageHeaderRE := regexp.MustCompile(`^\s*\[\[package\]\]`)

	var startLine int
	inPackage := false

	for i, line := range lines {
		lineNum := i + 1

		if packageHeaderRE.MatchString(line) {
			// Close previous package section
			if inPackage {
				ranges = append(ranges, SourceRange{
					StartLine: startLine,
					EndLine:   lineNum - 1,
					Section:   "package",
				})
			}
			startLine = lineNum
			inPackage = true
		}
	}

	// Close final package section
	if inPackage {
		ranges = append(ranges, SourceRange{
			StartLine: startLine,
			EndLine:   len(lines),
			Section:   "package",
		})
	}

	return ranges
}

// =============================================================================
// JavaScript: package-lock.json
// =============================================================================

// PackageLockJSONDetector detects package entries in package-lock.json files.
type PackageLockJSONDetector struct{}

func (d *PackageLockJSONDetector) Supports(filename string) bool {
	return filename == "package-lock.json"
}

func (d *PackageLockJSONDetector) DetectSections(content string) []SourceRange {
	var ranges []SourceRange
	lines := strings.Split(content, "\n")

	// npm v3 format: "node_modules/packagename": { ... }
	// npm v2/v1 format: dependencies object with "packagename": { ... }
	nodeModulesRE := regexp.MustCompile(`^\s*"node_modules/[^"]+"\s*:\s*\{`)
	// For npm v1/v2, look for top-level "dependencies": { and entries inside
	dependenciesKeyRE := regexp.MustCompile(`^\s*"dependencies"\s*:\s*\{`)
	packageEntryRE := regexp.MustCompile(`^\s*"[^"]+"\s*:\s*\{`)

	var startLine int
	braceDepth := 0
	inPackage := false
	inDependencies := false
	dependenciesBraceDepth := 0

	for i, line := range lines {
		lineNum := i + 1

		// Count braces (simplified, not handling strings perfectly but good enough for JSON)
		openBraces := strings.Count(line, "{")
		closeBraces := strings.Count(line, "}")

		// Check for node_modules entries (npm v3)
		if nodeModulesRE.MatchString(line) {
			if inPackage {
				// Close previous package
				ranges = append(ranges, SourceRange{
					StartLine: startLine,
					EndLine:   lineNum - 1,
					Section:   "packages",
				})
			}
			startLine = lineNum
			inPackage = true
			braceDepth = openBraces - closeBraces
			if braceDepth <= 0 {
				// Single line entry
				ranges = append(ranges, SourceRange{
					StartLine: startLine,
					EndLine:   lineNum,
					Section:   "packages",
				})
				inPackage = false
			}
			continue
		}

		// Check for "dependencies" key (npm v1/v2)
		if dependenciesKeyRE.MatchString(line) && !inDependencies {
			inDependencies = true
			dependenciesBraceDepth = openBraces - closeBraces
			continue
		}

		// Inside dependencies object, look for package entries
		if inDependencies && !inPackage && packageEntryRE.MatchString(line) {
			startLine = lineNum
			inPackage = true
			braceDepth = openBraces - closeBraces
			if braceDepth <= 0 {
				ranges = append(ranges, SourceRange{
					StartLine: startLine,
					EndLine:   lineNum,
					Section:   "dependencies",
				})
				inPackage = false
			}
			continue
		}

		// Track brace depth for current package
		if inPackage {
			braceDepth += openBraces - closeBraces
			if braceDepth <= 0 {
				ranges = append(ranges, SourceRange{
					StartLine: startLine,
					EndLine:   lineNum,
					Section:   "packages",
				})
				inPackage = false
			}
		}

		// Track dependencies section depth
		if inDependencies && !inPackage {
			dependenciesBraceDepth += openBraces - closeBraces
			if dependenciesBraceDepth <= 0 {
				inDependencies = false
			}
		}
	}

	// Close any open package at end
	if inPackage {
		ranges = append(ranges, SourceRange{
			StartLine: startLine,
			EndLine:   len(lines),
			Section:   "packages",
		})
	}

	return ranges
}

// =============================================================================
// Rust: Cargo.lock
// =============================================================================

// CargoLockDetector detects package sections in Cargo.lock files.
type CargoLockDetector struct{}

func (d *CargoLockDetector) Supports(filename string) bool {
	return filename == "Cargo.lock"
}

func (d *CargoLockDetector) DetectSections(content string) []SourceRange {
	var ranges []SourceRange
	lines := strings.Split(content, "\n")

	// Cargo.lock uses [[package]] sections for each dependency
	packageHeaderRE := regexp.MustCompile(`^\s*\[\[package\]\]`)

	var startLine int
	inPackage := false

	for i, line := range lines {
		lineNum := i + 1

		if packageHeaderRE.MatchString(line) {
			if inPackage {
				ranges = append(ranges, SourceRange{
					StartLine: startLine,
					EndLine:   lineNum - 1,
					Section:   "package",
				})
			}
			startLine = lineNum
			inPackage = true
		}
	}

	if inPackage {
		ranges = append(ranges, SourceRange{
			StartLine: startLine,
			EndLine:   len(lines),
			Section:   "package",
		})
	}

	return ranges
}

// =============================================================================
// JavaScript: yarn.lock
// =============================================================================

// YarnLockDetector detects package entries in yarn.lock files.
type YarnLockDetector struct{}

func (d *YarnLockDetector) Supports(filename string) bool {
	return filename == "yarn.lock"
}

func (d *YarnLockDetector) DetectSections(content string) []SourceRange {
	var ranges []SourceRange
	lines := strings.Split(content, "\n")

	// yarn.lock format:
	// "packagename@version":
	//   version "x.y.z"
	//   resolved "..."
	//   ...
	// Or yarn v1: packagename@version:
	packageHeaderRE := regexp.MustCompile(`^"?[^"\s]+@[^"\s]+"?\s*[:,]?\s*$`)

	var startLine int
	inPackage := false

	for i, line := range lines {
		lineNum := i + 1

		// Skip comments and empty lines at start
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			if inPackage && trimmed == "" {
				// Empty line might end a package section
				ranges = append(ranges, SourceRange{
					StartLine: startLine,
					EndLine:   lineNum - 1,
					Section:   "package",
				})
				inPackage = false
			}
			continue
		}

		// Check for package header (starts at column 0, not indented)
		if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") && packageHeaderRE.MatchString(line) {
			if inPackage {
				ranges = append(ranges, SourceRange{
					StartLine: startLine,
					EndLine:   lineNum - 1,
					Section:   "package",
				})
			}
			startLine = lineNum
			inPackage = true
		}
	}

	// Close final package
	if inPackage {
		ranges = append(ranges, SourceRange{
			StartLine: startLine,
			EndLine:   len(lines),
			Section:   "package",
		})
	}

	return ranges
}

// =============================================================================
// JavaScript: pnpm-lock.yaml
// =============================================================================

// PnpmLockDetector detects package entries in pnpm-lock.yaml files.
type PnpmLockDetector struct{}

func (d *PnpmLockDetector) Supports(filename string) bool {
	return filename == "pnpm-lock.yaml"
}

func (d *PnpmLockDetector) DetectSections(content string) []SourceRange {
	var ranges []SourceRange
	lines := strings.Split(content, "\n")

	// pnpm-lock.yaml format (v6+):
	// packages:
	//   /packagename@version:
	//     ...
	// Or snapshots section with similar entries
	packageEntryRE := regexp.MustCompile(`^\s{2}[/'"]?[^:]+@[^:]+['":]`)
	packagesHeaderRE := regexp.MustCompile(`^packages:`)
	snapshotsHeaderRE := regexp.MustCompile(`^snapshots:`)

	var startLine int
	inPackages := false
	inPackage := false

	for i, line := range lines {
		lineNum := i + 1

		// Check for packages: or snapshots: section header
		if packagesHeaderRE.MatchString(line) || snapshotsHeaderRE.MatchString(line) {
			if inPackage {
				ranges = append(ranges, SourceRange{
					StartLine: startLine,
					EndLine:   lineNum - 1,
					Section:   "package",
				})
				inPackage = false
			}
			inPackages = true
			continue
		}

		// Check for new top-level section (not packages/snapshots)
		if inPackages && len(line) > 0 && line[0] != ' ' && line[0] != '\t' {
			if inPackage {
				ranges = append(ranges, SourceRange{
					StartLine: startLine,
					EndLine:   lineNum - 1,
					Section:   "package",
				})
				inPackage = false
			}
			inPackages = false
			continue
		}

		// Inside packages section, look for package entries
		if inPackages && packageEntryRE.MatchString(line) {
			if inPackage {
				ranges = append(ranges, SourceRange{
					StartLine: startLine,
					EndLine:   lineNum - 1,
					Section:   "package",
				})
			}
			startLine = lineNum
			inPackage = true
		}
	}

	// Close final package
	if inPackage {
		ranges = append(ranges, SourceRange{
			StartLine: startLine,
			EndLine:   len(lines),
			Section:   "package",
		})
	}

	return ranges
}

// =============================================================================
// Default Detectors
// =============================================================================

// DefaultDetectors returns the standard set of section detectors for all supported languages.
func DefaultDetectors() []SectionDetector {
	return []SectionDetector{
		&PyprojectDetector{},
		&PackageJSONDetector{},
		&GoModDetector{},
		&CargoDetector{},
		&CargoLockDetector{},
		&GemfileDetector{},
		&ComposerDetector{},
		&RequirementsDetector{},
		&UvLockDetector{},
		&PoetryLockDetector{},
		&PackageLockJSONDetector{},
		&YarnLockDetector{},
		&PnpmLockDetector{},
	}
}
