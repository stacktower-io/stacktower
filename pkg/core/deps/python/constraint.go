package python

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/contriboss/pubgrub-go"

	"github.com/matzehuels/stacktower/pkg/core/deps"
)

// PEP440Matcher implements constraint matching for Python's PEP 440 version specifiers.
// It supports common operators: ==, !=, <, <=, >, >=, ~=
// Multiple constraints can be combined with commas (e.g., ">=1.0,<2.0").
type PEP440Matcher struct{}

// Ensure PEP440Matcher implements ConstraintParser
var _ deps.ConstraintParser = PEP440Matcher{}

// BestMatch finds the highest version from candidates that satisfies the constraint.
// Returns empty string if no version matches or constraint is empty/invalid.
func (PEP440Matcher) BestMatch(constraint string, candidates []string) string {
	if constraint == "" || len(candidates) == 0 {
		return ""
	}

	// Parse constraint into individual specifiers
	specs := parseConstraint(constraint)
	if len(specs) == 0 {
		return ""
	}

	// Filter candidates to only valid versions (exclude pre-releases, dev, etc.)
	var validVersions []parsedVersion
	for _, v := range candidates {
		pv := parseVersion(v)
		if pv.valid && !pv.prerelease {
			validVersions = append(validVersions, pv)
		}
	}

	// Sort versions descending (highest first)
	sort.Slice(validVersions, func(i, j int) bool {
		return compareVersions(validVersions[i], validVersions[j]) > 0
	})

	// Find the first (highest) version that satisfies all constraints
	for _, pv := range validVersions {
		if satisfiesAll(pv, specs) {
			return pv.original
		}
	}

	return ""
}

// specifier represents a single version constraint like ">=1.0"
type specifier struct {
	op      string        // ==, !=, <, <=, >, >=, ~=
	version parsedVersion // The version to compare against
}

// parsedVersion holds a parsed semver-like version
type parsedVersion struct {
	original   string
	major      int
	minor      int
	patch      int
	prerelease bool
	valid      bool
}

var (
	// Matches constraint operators and version
	specRE = regexp.MustCompile(`^\s*(~=|===?|!=|<=?|>=?)\s*([^\s,]+)\s*$`)

	// Matches version components
	versionRE = regexp.MustCompile(`^(\d+)(?:\.(\d+))?(?:\.(\d+))?(?:[-._]?(a|alpha|b|beta|c|rc|pre|dev|post).*)?$`)
)

// parseConstraint splits a constraint string into individual specifiers.
// Example: ">=1.0,<2.0" -> [specifier{op:">=", version:1.0}, specifier{op:"<", version:2.0}]
func parseConstraint(constraint string) []specifier {
	var specs []specifier
	parts := strings.Split(constraint, ",")

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		m := specRE.FindStringSubmatch(part)
		if m == nil {
			// Invalid specifier, skip
			continue
		}

		op := m[1]
		verStr := m[2]

		pv := parseVersion(verStr)
		if !pv.valid {
			continue
		}

		specs = append(specs, specifier{op: op, version: pv})
	}

	return specs
}

// parseVersion parses a version string into components.
func parseVersion(v string) parsedVersion {
	pv := parsedVersion{original: v}

	// Normalize: remove leading 'v', convert underscores
	v = strings.TrimPrefix(v, "v")
	v = strings.TrimPrefix(v, "V")
	v = strings.ToLower(v)

	m := versionRE.FindStringSubmatch(v)
	if m == nil {
		return pv
	}

	pv.valid = true
	pv.major, _ = strconv.Atoi(m[1])
	if m[2] != "" {
		pv.minor, _ = strconv.Atoi(m[2])
	}
	if m[3] != "" {
		pv.patch, _ = strconv.Atoi(m[3])
	}
	if m[4] != "" {
		pv.prerelease = true
	}

	return pv
}

// compareVersions compares two versions.
// Returns: >0 if a > b, <0 if a < b, 0 if equal
func compareVersions(a, b parsedVersion) int {
	if a.major != b.major {
		return a.major - b.major
	}
	if a.minor != b.minor {
		return a.minor - b.minor
	}
	return a.patch - b.patch
}

// satisfiesAll checks if a version satisfies all specifiers.
func satisfiesAll(v parsedVersion, specs []specifier) bool {
	for _, s := range specs {
		if !satisfies(v, s) {
			return false
		}
	}
	return true
}

// satisfies checks if a version satisfies a single specifier.
func satisfies(v parsedVersion, s specifier) bool {
	cmp := compareVersions(v, s.version)

	switch s.op {
	case "==", "===":
		return cmp == 0
	case "!=":
		return cmp != 0
	case "<":
		return cmp < 0
	case "<=":
		return cmp <= 0
	case ">":
		return cmp > 0
	case ">=":
		return cmp >= 0
	case "~=":
		// Compatible release: ~=X.Y.Z means >=X.Y.Z,<X.(Y+1).0
		// ~=X.Y means >=X.Y,<(X+1).0
		if cmp < 0 {
			return false
		}
		// Upper bound: increment the second-to-last component
		if s.version.patch > 0 || versionHasPatch(s.version.original) {
			// ~=1.4.2 means >=1.4.2,<1.5.0
			return v.major == s.version.major && v.minor == s.version.minor
		}
		// ~=1.4 means >=1.4,<2.0
		return v.major == s.version.major
	default:
		return false
	}
}

// versionHasPatch checks if the original version string has a patch component.
func versionHasPatch(v string) bool {
	parts := strings.Split(v, ".")
	return len(parts) >= 3
}

// =============================================================================
// PubGrub ConstraintParser implementation
// =============================================================================

// pep440Version is a PubGrub Version that preserves the original version string
// so that registry fetches use the exact form returned by ListVersions (e.g.
// "5.0.0a2" rather than the stripped "5.0.0"). Sort() applies PEP 440 ordering:
// pre-release versions come before the corresponding stable release.
type pep440Version struct {
	original string
	parsed   parsedVersion
}

func (v pep440Version) String() string { return v.original }

func (v pep440Version) Sort(other pubgrub.Version) int {
	var op parsedVersion
	if o, ok := other.(pep440Version); ok {
		op = o.parsed
	} else {
		op = parseVersion(other.String())
	}
	if cmp := compareVersions(v.parsed, op); cmp != 0 {
		return cmp
	}
	// Same numeric components: pre-release < stable (PEP 440 §6)
	if v.parsed.prerelease == op.prerelease {
		return 0
	}
	if v.parsed.prerelease {
		return -1
	}
	return 1
}

// makePEP440Version builds a pep440Version from an already-parsed parsedVersion.
func makePEP440Version(pv parsedVersion) pep440Version {
	return pep440Version{original: pv.original, parsed: pv}
}

// ParseVersion converts a Python version string to a PubGrub Version.
// The original string is preserved so downstream fetches use the exact registry form.
func (PEP440Matcher) ParseVersion(version string) pubgrub.Version {
	pv := parseVersion(version)
	if !pv.valid {
		return nil
	}
	return makePEP440Version(pv)
}

// ParseConstraint converts a PEP 440 constraint to a PubGrub Condition.
// Returns nil if the constraint is empty or cannot be parsed.
// Version set bounds are built from pep440Version values so that range
// containment checks use the same Sort() logic as the candidate versions.
func (PEP440Matcher) ParseConstraint(constraint string) pubgrub.Condition {
	if constraint == "" {
		return nil
	}

	specs := parseConstraint(constraint)
	if len(specs) == 0 {
		return nil
	}

	combined := pubgrub.FullVersionSet()
	for _, s := range specs {
		v := makePEP440Version(s.version)
		var specSet pubgrub.VersionSet

		switch s.op {
		case "==", "===":
			specSet = pubgrub.NewVersionRangeSet(v, true, v, true)
		case "!=":
			specSet = pubgrub.NewVersionRangeSet(v, true, v, true).Complement()
		case ">=":
			specSet = pubgrub.NewLowerBoundVersionSet(v, true)
		case ">":
			specSet = pubgrub.NewLowerBoundVersionSet(v, false)
		case "<=":
			specSet = pubgrub.NewUpperBoundVersionSet(v, true)
		case "<":
			// When bound is a stable version (e.g., <1.0.0), exclude prereleases
			// of that version too. Users expect <1.0.0 to mean "before the 1.0
			// release series", not "anything that sorts before 1.0.0".
			// Achieve this by using a synthetic dev0 prerelease as the bound.
			if !s.version.prerelease {
				devBound := pep440Version{
					original: fmt.Sprintf("%d.%d.%d.dev0", s.version.major, s.version.minor, s.version.patch),
					parsed: parsedVersion{
						major:      s.version.major,
						minor:      s.version.minor,
						patch:      s.version.patch,
						prerelease: true,
						valid:      true,
					},
				}
				specSet = pubgrub.NewUpperBoundVersionSet(devBound, false)
			} else {
				specSet = pubgrub.NewUpperBoundVersionSet(v, false)
			}
		case "~=":
			// ~=X.Y.Z → >=X.Y.Z, <X.(Y+1).0
			// ~=X.Y   → >=X.Y.0, <(X+1).0.0
			var ceil pep440Version
			if s.version.patch > 0 || versionHasPatch(s.version.original) {
				ceil = pep440Version{
					original: fmt.Sprintf("%d.%d.0", s.version.major, s.version.minor+1),
					parsed: parsedVersion{
						major: s.version.major, minor: s.version.minor + 1, valid: true,
					},
				}
			} else {
				ceil = pep440Version{
					original: fmt.Sprintf("%d.0.0", s.version.major+1),
					parsed: parsedVersion{
						major: s.version.major + 1, valid: true,
					},
				}
			}
			specSet = pubgrub.NewVersionRangeSet(v, true, ceil, false)
		default:
			continue
		}

		combined = combined.Intersection(specSet)
	}

	return pubgrub.NewVersionSetCondition(combined)
}
